package pipeline

import (
	"context"
	"fmt"
	"image"
	"math/bits"
	"os"
	"path/filepath"
	"strings"

	"xstudio/internal/core"
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
		if e = validatePhoto(ph, p.Category); e != nil {
			return e
		}
		count++
		if !ph.HasVerifiedEnhancement() {
			ph.EnhancedFile = ""
		}
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
		if ph.WhiteBG {
			if e = a.makeWhite(ctx, p, ph, filename, tmp, params); e != nil {
				return e
			}
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

func (a *Module) makeWhite(ctx context.Context, p Product, ph Photo, canonicalPath, tmp string, params Object) error {
	key := whiteParamKey(ph)
	params["white_key"] = key
	if a.reuseWhite(p.ID, ph, key, tmp, params) {
		return nil
	}
	canonical, _, e := loadPhoto(canonicalPath)
	if e != nil {
		return e
	}
	// AI 补的白底图（A1_AI）本身就是 GPT 出的纯白底，不再调一次 AI，只做成方图和透明图
	if ph.AIGenerated {
		white := fitWhiteSquare(canonical, 1200)
		if e = saveJPEG(filepath.Join(tmp, ph.Code+"_白底.jpg"), white, 92); e != nil {
			return e
		}
		if e = savePNG(filepath.Join(tmp, ph.Code+"_透明.png"), knockoutWhite(white)); e != nil {
			return e
		}
		params["white_source"] = "ai_fill"
		params["white_size"] = 1200
		return nil
	}
	doc, e := a.prompt("补图.md")
	var prompt string
	if e == nil {
		prompt, e = enhancementPrompt(enhancementSections(doc), "A1", p)
	}
	if e != nil {
		params["white_error"] = a.safeAIError(e.Error())
		return nil
	}
	if model := strings.TrimSpace(a.Model("fill", a.Cfg.ImageModel)); model != "" {
		params["white_model"] = model
	}
	data, e := a.EditImageSize(core.WithFeature(ctx, "fill"), prompt, []image.Image{canonical}, "1024x1024")
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		params["white_error"] = a.safeAIError(e.Error())
		return nil
	}
	white, transparent, returned, e := whiteOutputs(data)
	if e != nil {
		params["white_error"] = a.safeAIError(e.Error())
		return nil
	}
	if e = saveJPEG(filepath.Join(tmp, ph.Code+"_白底.jpg"), white, 92); e != nil {
		return e
	}
	if e = savePNG(filepath.Join(tmp, ph.Code+"_透明.png"), transparent); e != nil {
		return e
	}
	params["white_source"] = "ai"
	params["white_size"] = 1200
	params["white_returned"] = returned
	return nil
}

func (a *Module) reuseWhite(productID int64, ph Photo, key, tmp string, params Object) bool {
	old, _ := ph.RetouchLog["white_key"].(string)
	if old == "" || old != key {
		return false
	}
	dir := filepath.Join(a.Dir(productID), "照片")
	jpg := filepath.Join(dir, ph.Code+"_白底.jpg")
	if _, e := os.Stat(jpg); e != nil {
		return false
	}
	jpgDst := filepath.Join(tmp, ph.Code+"_白底.jpg")
	pngDst := filepath.Join(tmp, ph.Code+"_透明.png")
	if e := copyFile(jpg, jpgDst); e != nil {
		return false
	}
	pngSrc := filepath.Join(dir, ph.Code+"_透明.png")
	if _, e := os.Stat(pngSrc); e == nil {
		if e = copyFile(pngSrc, pngDst); e != nil {
			_ = os.Remove(jpgDst)
			return false
		}
	} else {
		im, _, e := loadPhoto(jpgDst)
		if e == nil {
			e = savePNG(pngDst, knockoutWhite(im))
		}
		if e != nil {
			_ = os.Remove(jpgDst)
			_ = os.Remove(pngDst)
			return false
		}
	}
	params["white_source"] = "ai"
	params["white_size"] = 1200
	if v, ok := ph.RetouchLog["white_returned"].(string); ok && v != "" {
		params["white_returned"] = v
	}
	if v, ok := ph.RetouchLog["white_model"].(string); ok && v != "" {
		params["white_model"] = v
	}
	return true
}
