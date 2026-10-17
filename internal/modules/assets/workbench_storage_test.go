package assets

import (
	"context"
	"io/fs"
	"os"
	"path"
	"testing"
	"time"
)

func TestWorkbenchCleanupThumbnailLifetimeAndOrphanGrace(t *testing.T) {
	a := algorithmModule(t)
	root, err := a.workbenchRoot(1)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	at := time.Now().UTC()
	write := func(file string, old bool) {
		t.Helper()
		if err := root.MkdirAll(path.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := root.WriteFile(file, []byte("file"), 0600); err != nil {
			t.Fatal(err)
		}
		if old {
			if err := root.Chtimes(file, at.Add(-2*time.Hour), at.Add(-2*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, v := range []struct {
		file    string
		created time.Time
	}{{"results/expired.png", at.Add(-8 * 24 * time.Hour)}, {"results/live.png", at}} {
		write(v.file, false)
		for _, width := range []int{240, 480, 960} {
			write(workbenchThumbPath(v.file, width), true)
		}
		if _, err := a.DB.Exec("INSERT INTO workbench_items(user_id,kind,file,orig_name,width,height,created_at) VALUES(1,'result',?,'x.png',1,1,?)", v.file, v.created.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	// The expired item's fresh thumbnail must be removed immediately too.
	write(workbenchThumbPath("results/expired.png", 240), false)
	orphanThumb := workbenchThumbPath("results/orphan.png", 240)
	write(orphanThumb, true)
	write("download_old.zip", true)
	write("download_new.zip", false)
	// Keep an actual open writer registered, with a deliberately old mtime.
	pending := "inputs/pending.png.tmp"
	if err = root.MkdirAll("inputs", 0700); err != nil {
		t.Fatal(err)
	}
	release := a.holdWorkbenchFile(1, pending)
	defer release()
	f, err := root.OpenFile(pending, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if err = root.Chtimes(pending, at.Add(-2*time.Hour), at.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = a.cleanupWorkbench(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	missing := func(file string) {
		t.Helper()
		if _, err := root.Stat(file); !os.IsNotExist(err) {
			t.Fatal("文件未清理", file, err)
		}
	}
	present := func(file string) {
		t.Helper()
		if _, err := root.Stat(file); err != nil {
			t.Fatal("文件被误删", file, err)
		}
	}
	missing("results/expired.png")
	missing(orphanThumb)
	missing("download_old.zip")
	for _, width := range []int{240, 480, 960} {
		missing(workbenchThumbPath("results/expired.png", width))
		present(workbenchThumbPath("results/live.png", width))
	}
	present("download_new.zip")
	present(pending)
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	release()
	if err = a.cleanupWorkbench(context.Background(), at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	missing(pending)
	missing("download_new.zip")
	for _, width := range []int{240, 480, 960} {
		present(workbenchThumbPath("results/live.png", width))
	}
}
func TestWorkbenchThumbnailPublicationAfterDeletionDoesNotRecreateCache(t *testing.T) {
	a := algorithmModule(t)
	root, err := a.workbenchRoot(1)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tx, err := a.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	v, err := insertWorkbench(tx, WorkbenchItem{UserID: 1, Kind: "input", File: "inputs/deleted.png", OrigName: "x.png", Width: 1, Height: 1, Source: "original", Op: Object{}, CreatedAt: now()})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = a.DB.Exec("DELETE FROM workbench_items WHERE id=?", v.ID); err != nil {
		t.Fatal(err)
	}
	if err = a.publishWorkbenchThumb(root, v, 240, []byte("already encoded")); !os.IsNotExist(err) {
		t.Fatal("删除后仍发布了缓存", err)
	}
	if err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			t.Errorf("删除后残留文件 %s", name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.workbenchFiles.Range(func(key, value any) bool { t.Errorf("残留临时文件登记 %v", key); return true })
}
