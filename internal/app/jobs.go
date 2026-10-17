package app

import (
	"context"
	"fmt"
	"math/bits"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var steps = []string{"check", "classify", "enhance", "retouch", "copy", "render"}

func (a *App) run(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := idParam(r, "id")
	if _, ok := a.authorizedProduct(w, r, id, true); !ok {
		return
	}
	q := r.URL.Query()
	step, from := q.Get("step"), q.Get("from")
	if step != "" && from != "" {
		fail(w, 400, "step 和 from 不能同时指定")
		return
	}
	selected := steps
	kind := "run_all"
	target := step
	if target == "" {
		target = from
	}
	if target != "" {
		index := -1
		for i, s := range steps {
			if s == target {
				index = i
			}
		}
		if index < 0 {
			fail(w, 400, "未知处理步骤")
			return
		}
		if step != "" {
			selected = []string{step}
			kind = step
		} else {
			selected = steps[index:]
		}
	}
	tx, e := a.db.Begin()
	if e != nil {
		fail(w, 500, "创建任务失败")
		return
	}
	defer tx.Rollback()
	res, e := tx.Exec("INSERT INTO jobs(product_id,kind,status,created_at,steps) VALUES(?,?,'queued',?,?)", id, kind, now(), strings.Join(selected, ","))
	if e != nil {
		fail(w, 409, "此款已有正在排队或运行的任务")
		return
	}
	jid, _ := res.LastInsertId()
	if _, e = tx.Exec("UPDATE products SET status='processing',error='',updated_at=? WHERE id=?", now(), id); e != nil {
		fail(w, 500, "保存任务失败")
		return
	}
	if e = tx.Commit(); e != nil {
		fail(w, 500, "保存任务失败")
		return
	}
	j, _ := scanJob(a.db.QueryRow("SELECT * FROM jobs WHERE id=?", jid))
	select {
	case a.wake <- struct{}{}:
	default:
	}
	writeJSON(w, 202, Object{"job": j})
}
func (a *App) claim() *Job {
	a.mu.Lock()
	defer a.mu.Unlock()
	j, e := scanJob(a.db.QueryRow("SELECT * FROM jobs WHERE status='queued' ORDER BY id LIMIT 1"))
	if e != nil {
		return nil
	}
	res, e := a.db.Exec("UPDATE jobs SET status='running',message='开始处理' WHERE id=? AND status='queued'", j.ID)
	if e != nil {
		return nil
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil
	}
	j.Status = "running"
	return &j
}
func (a *App) worker() {
	defer a.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if a.ctx.Err() != nil {
			return
		}
		if a.getBaseURL() != "" {
			if j := a.claim(); j != nil {
				a.execute(*j)
				continue
			}
		}
		select {
		case <-a.ctx.Done():
			return
		case <-a.wake:
		case <-ticker.C:
		}
	}
}
func (a *App) execute(j Job) {
	var err error
	warnings := []string{}
	withWarnings := func(message string) string {
		if len(warnings) > 0 {
			return message + "；" + strings.Join(warnings, "；")
		}
		return message
	}
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("处理异常，请检查照片与文案格式")
		}
		if a.ctx.Err() != nil {
			err = fmt.Errorf("服务重启中断")
		}
		if err != nil {
			message := withWarnings(a.safeAIError(err.Error()))
			_, _ = a.db.Exec("UPDATE jobs SET status='failed',message=?,finished_at=? WHERE id=?", message, now(), j.ID)
			_, _ = a.db.Exec("UPDATE products SET status='failed',error=?,updated_at=? WHERE id=?", message, now(), j.ProductID)
		}
	}()
	selected := strings.Split(j.Steps, ",")
	for i, step := range selected {
		if a.ctx.Err() != nil {
			err = fmt.Errorf("服务重启中断")
			return
		}
		_, err = a.db.Exec("UPDATE jobs SET progress=?,message=? WHERE id=?", i*100/len(selected), withWarnings("正在执行："+step), j.ID)
		if err != nil {
			return
		}
		_, err = a.db.Exec("UPDATE products SET step=?,updated_at=? WHERE id=?", step, now(), j.ProductID)
		if err != nil {
			return
		}
		var p Product
		p, err = a.product(j.ProductID)
		if err != nil {
			return
		}
		switch step {
		case "check":
			err = a.check(a.ctx, p)
		case "classify":
			err = a.classify(a.ctx, p)
		case "enhance":
			var notes []string
			notes, err = a.enhance(a.ctx, p)
			warnings = append(warnings, notes...)
		case "retouch":
			err = a.retouch(a.ctx, p)
		case "copy":
			err = a.copyText(a.ctx, p)
		case "render":
			err = a.render(a.ctx, p)
		default:
			err = fmt.Errorf("未知任务步骤")
		}
		if err != nil {
			err = fmt.Errorf("%s 步骤失败：%w", step, err)
			return
		}
		_, err = a.db.Exec("UPDATE jobs SET progress=?,message=? WHERE id=?", (i+1)*100/len(selected), withWarnings("已完成："+step), j.ID)
		if err != nil {
			return
		}
	}
	status := "draft"
	if selected[len(selected)-1] == "render" {
		status = "ready"
	}
	_, err = a.db.Exec("UPDATE products SET status=?,error='',updated_at=? WHERE id=?", status, now(), j.ProductID)
	if err != nil {
		return
	}
	_, err = a.db.Exec("UPDATE jobs SET status='done',progress=100,message=?,finished_at=? WHERE id=?", withWarnings("处理完成"), now(), j.ID)
}
func (a *App) check(ctx context.Context, p Product) error {
	photos, e := a.photos(p.ID)
	if e != nil {
		return e
	}
	if len(photos) == 0 {
		return fmt.Errorf("请先上传照片")
	}
	hashes := map[int64]uint64{}
	for _, ph := range photos {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		im, ex, e := loadPhoto(filepath.Join(a.dir(p.ID), filepath.FromSlash(ph.File)))
		metrics := Object{}
		if e != nil {
			metrics["error"] = "照片无法读取或已损坏"
		} else {
			var hash uint64
			metrics, hash = Metrics(oriented(im, ex.Orientation), ex)
			for _, prev := range photos {
				if h, ok := hashes[prev.ID]; ok && bits.OnesCount64(h^hash) <= 5 {
					metrics["dup_of"] = prev.ID
					break
				}
			}
			hashes[ph.ID] = hash
		}
		if _, e = a.db.Exec("UPDATE photos SET metrics=? WHERE id=?", js(metrics), ph.ID); e != nil {
			return e
		}
		if metrics["error"] != "" {
			return fmt.Errorf("照片 %s 无法检查", ph.OrigName)
		}
	}
	return nil
}
func (a *App) retouch(ctx context.Context, p Product) error {
	photos, e := a.photos(p.ID)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(a.dir(p.ID), 0700); e != nil {
		return e
	}
	tmp, e := os.MkdirTemp(a.dir(p.ID), ".retouch-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	logs := map[int64]Object{}
	count := 0
	for _, ph := range photos {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if ph.Code == "" {
			continue
		}
		if e = validatePhoto(ph); e != nil {
			return e
		}
		count++
		input := ph.File
		if ph.EnhancedFile != "" {
			input = ph.EnhancedFile
		}
		im, ex, e := loadPhoto(filepath.Join(a.dir(p.ID), filepath.FromSlash(input)))
		if e != nil {
			return fmt.Errorf("无法读取原图：%s", ph.OrigName)
		}
		if ph.EnhancedFile == "" {
			im = Geometry(im, ex.Orientation, ph.Rotate, ph.Crop)
		}
		im, params := Retouch(im, ph.Code)
		params["orientation"] = ex.Orientation
		params["rotate"] = ph.Rotate
		params["crop"] = ph.Crop
		params["gps_removed"] = ex.GPS
		if ph.EnhancedFile != "" {
			params["enhanced_file"] = ph.EnhancedFile
			params["geometry_already_applied"] = true
		}
		if message, ok := ph.RetouchLog["upscale_error"]; ok {
			params["upscale_error"] = message
		}
		filename := filepath.Join(tmp, ph.Code+".jpg")
		if e = saveJPEG(filename, im, 92); e != nil {
			return e
		}
		if ph.WhiteBG { // Use the decoded saved JPG as the canonical retouched image.
			canonical, _, e := loadPhoto(filename)
			if e != nil {
				return e
			}
			mask, mode := a.mask(ctx, canonical)
			white, transparent, e := CompositeWhite(canonical, mask)
			if e != nil {
				return e
			}
			if e = saveJPEG(filepath.Join(tmp, ph.Code+"_白底.jpg"), white, 92); e != nil {
				return e
			}
			if e = savePNG(filepath.Join(tmp, ph.Code+"_透明.png"), transparent); e != nil {
				return e
			}
			params["cutout"] = mode
			params["white_size"] = 1200
			params["product_occupancy"] = .65
			params["opaque_pixels_preserved"] = true
		}
		logs[ph.ID] = params
	}
	if count == 0 {
		return fmt.Errorf("没有已编号的照片，请先分拣或手动指定拍摄编号")
	}
	if e = replaceDir(tmp, filepath.Join(a.dir(p.ID), "照片")); e != nil {
		return e
	}
	for id, v := range logs {
		if _, e = a.db.Exec("UPDATE photos SET retouch_log=? WHERE id=?", js(v), id); e != nil {
			return e
		}
	}
	return nil
}
func replaceDir(src, dst string) error {
	backup := dst + ".previous"
	if e := os.RemoveAll(backup); e != nil {
		return e
	}
	exists := false
	if _, e := os.Stat(dst); e == nil {
		if e = os.Rename(dst, backup); e != nil {
			return e
		}
		exists = true
	}
	if e := os.Rename(src, dst); e != nil {
		if exists {
			_ = os.Rename(backup, dst)
		}
		return e
	}
	return os.RemoveAll(backup)
}
