package pipeline

import (
	"context"
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
)

func (a *Module) check(ctx context.Context, p Product) error {
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
		im, ex, e := loadPhoto(filepath.Join(a.Dir(p.ID), filepath.FromSlash(ph.File)))
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
		if _, e = a.DB.Exec("UPDATE assets SET metrics=? WHERE id=?", js(metrics), ph.ID); e != nil {
			return e
		}
		if metrics["error"] != "" {
			return fmt.Errorf("照片 %s 无法检查", ph.OrigName)
		}
	}
	return nil
}
func (a *Module) retouch(ctx context.Context, p Product) error {
	photos, e := a.photos(p.ID)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(a.Dir(p.ID), 0700); e != nil {
		return e
	}
	tmp, e := os.MkdirTemp(a.Dir(p.ID), ".retouch-*")
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
		im, ex, e := loadPhoto(filepath.Join(a.Dir(p.ID), filepath.FromSlash(input)))
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
	if e = replaceDir(tmp, filepath.Join(a.Dir(p.ID), "照片")); e != nil {
		return e
	}
	for id, v := range logs {
		if _, e = a.DB.Exec("UPDATE assets SET retouch_log=? WHERE id=?", js(v), id); e != nil {
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
