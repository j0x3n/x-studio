package assets

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strconv"
	"time"
)

func parseWorkbenchTime(s string) time.Time { t, _ := time.Parse(time.RFC3339Nano, s); return t }
func (a *Module) startWorkbenchCleanup() {
	a.cleanupOnce.Do(func() {
		done := make(chan struct{})
		a.OnClose(func() { <-done })
		go func() {
			defer close(done)
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for {
				if err := a.cleanupWorkbench(a.Context(), time.Now()); err != nil && a.Context().Err() == nil {
					log.Printf("处理台清理失败：%v", err)
				}
				select {
				case <-a.Context().Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}
func (a *Module) cleanupWorkbench(ctx context.Context, at time.Time) error {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	base, err := os.OpenRoot(a.Cfg.DataDir)
	if err != nil {
		return err
	}
	defer base.Close()
	dir, err := base.OpenRoot("workbench")
	if os.IsNotExist(err) {
		return a.pruneExpiredWorkbenchRows(ctx, at)
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := fs.ReadDir(dir.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return err
		}
		uid, e := strconv.ParseInt(entry.Name(), 10, 64)
		if e != nil || uid <= 0 || !entry.IsDir() {
			continue
		}
		root, e := dir.OpenRoot(entry.Name())
		if e != nil {
			return e
		}
		e = a.cleanupWorkbenchUser(ctx, root, uid, at)
		root.Close()
		if e != nil {
			return e
		}
	}
	return a.pruneExpiredWorkbenchRows(ctx, at)
}

// Files can have been removed outside the app, including an entire user folder.
func (a *Module) pruneExpiredWorkbenchRows(ctx context.Context, at time.Time) error {
	_, err := a.DB.ExecContext(ctx, "DELETE FROM workbench_items WHERE julianday(created_at)<julianday(?)", at.Add(-7*24*time.Hour).UTC().Format(time.RFC3339Nano))
	return err
}
func (a *Module) cleanupWorkbenchUser(ctx context.Context, root *os.Root, uid int64, at time.Time) error {
	rows, err := a.DB.QueryContext(ctx, "SELECT "+workbenchColumns+" FROM workbench_items WHERE user_id=?", uid)
	if err != nil {
		return err
	}
	items := []WorkbenchItem{}
	for rows.Next() {
		v, e := scanWorkbench(rows)
		if e != nil {
			rows.Close()
			return e
		}
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for _, v := range items {
		if err = ctx.Err(); err != nil {
			return err
		}
		if !parseWorkbenchTime(v.CreatedAt).Before(at.Add(-7 * 24 * time.Hour)) {
			live[v.File] = true
			for _, width := range []int{240, 480, 960} {
				live[workbenchThumbPath(v.File, width)] = true
			}
			continue
		}
		if !validAssetFile(v.File) {
			return fmt.Errorf("处理台图片 %d 的文件路径不合法", v.ID)
		}
		err = func() error {
			a.workbenchFileMu.Lock()
			defer a.workbenchFileMu.Unlock()
			if err := root.Remove(v.File); err != nil && !os.IsNotExist(err) {
				return err
			}
			if err := removeWorkbenchThumbs(root, v.File); err != nil {
				return err
			}
			_, err := a.DB.ExecContext(ctx, "DELETE FROM workbench_items WHERE id=? AND user_id=?", v.ID, uid)
			return err
		}()
		if err != nil {
			return err
		}
	}
	return fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			if os.IsNotExist(e) {
				return nil
			}
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if d.IsDir() || path == "." || live[path] {
			return nil
		}
		if !validAssetFile(path) {
			return fmt.Errorf("处理台文件路径不合法")
		}
		// Writers register unique paths before creating them and release only after
		// closing and publishing/removing. No global mutex is required for writes.
		if _, active := a.workbenchFiles.Load(fmt.Sprintf("%d/%s", uid, path)); active {
			return nil
		}
		info, err := root.Stat(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.ModTime().Before(at.Add(-time.Hour)) {
			return nil
		}
		err = root.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	})
}
