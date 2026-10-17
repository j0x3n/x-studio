package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxGeneratedPhotos = 4

func allowedFillCode(code string) bool {
	switch code {
	case "A1", "E1", "E2", "E3", "D4":
		return true
	}
	return false
}
func codeMatches(code, candidate string) bool {
	return code != "" && (code == candidate || strings.HasPrefix(code, candidate+"_"))
}

func (a *App) slotsList() []slot {
	var input map[string]json.RawMessage
	_ = json.Unmarshal(a.slots, &input)
	out := []slot{}
	for _, key := range []string{"主图", "详情页"} {
		var group []slot
		_ = json.Unmarshal(input[key], &group)
		out = append(out, group...)
	}
	return out
}
func slotHits(s slot, photos []Photo) []Photo {
	out := []Photo{}
	for _, candidate := range s.Candidates {
		for _, ph := range photos {
			if ph.AIGenerated && !allowedFillCode(ph.Code) {
				continue
			}
			if codeMatches(ph.Code, candidate) {
				out = append(out, ph)
			}
		}
	}
	return out
}
func slotAllowsFill(s slot) bool {
	for _, candidate := range s.Candidates {
		if allowedFillCode(candidate) {
			return true
		}
	}
	return false
}
func (a *App) usedCode(code string) bool {
	for _, s := range a.slotsList() {
		for _, candidate := range s.Candidates {
			if codeMatches(code, candidate) {
				return true
			}
		}
	}
	return false
}
func boolOption(info Object, name string, def bool) bool {
	if v, ok := info[name].(bool); ok {
		return v
	}
	return def
}
func validateInfo(info Object) error {
	for _, key := range []string{"auto_upscale", "auto_fill"} {
		if v, exists := info[key]; exists {
			if _, ok := v.(bool); !ok {
				return fmt.Errorf("info.%s 必须是布尔值", key)
			}
		}
	}
	return nil
}

func (a *App) fillTargets(photos []Photo) []string {
	white, hero := false, false
	for _, s := range a.slotsList() {
		if !s.Required || len(slotHits(s, photos)) > 0 {
			continue
		}
		for _, candidate := range s.Candidates {
			if candidate == "A1" {
				white = true
			}
			if candidate == "E1" {
				hero = true
			}
		}
	}
	out := []string{}
	if white {
		out = append(out, "A1")
	}
	if hero {
		out = append(out, "E1")
	}
	hasD := false
	for _, ph := range photos {
		if len(ph.Code) >= 2 && ph.Code[0] == 'D' {
			hasD = true
		}
	}
	if !hasD {
		out = append(out, "D4")
	}
	return out
}

func selectReferences(photos []Photo, code string) []Photo {
	priority := []string{"A1", "A2", "F2", "E4", "B2", "B3"}
	real := []Photo{}
	for _, ph := range photos {
		if !ph.AIGenerated && ph.Code != "" {
			real = append(real, ph)
		}
	}
	rank := func(ph Photo) int {
		for i, c := range priority {
			if codeMatches(ph.Code, c) {
				return i
			}
		}
		return len(priority)
	}
	sort.SliceStable(real, func(i, j int) bool { return rank(real[i]) < rank(real[j]) })
	out := []Photo{}
	for _, ph := range real {
		if len(out) < 3 {
			out = append(out, ph)
		}
	}
	if code == "D4" {
		for _, ph := range real {
			if codeMatches(ph.Code, "D1") || codeMatches(ph.Code, "D2") || codeMatches(ph.Code, "D3") {
				found := false
				for _, used := range out {
					if used.ID == ph.ID {
						found = true
					}
				}
				if !found {
					if len(out) == 3 {
						out = out[:2]
					}
					out = append(out, ph)
				}
				break
			}
		}
	}
	return out
}
func (a *App) beforeRetouch(ph Photo) (*image.NRGBA, error) {
	if ph.EnhancedFile != "" {
		im, _, err := loadPhoto(filepath.Join(a.dir(ph.ProductID), filepath.FromSlash(ph.EnhancedFile)))
		return im, err
	}
	im, ex, err := loadPhoto(filepath.Join(a.dir(ph.ProductID), filepath.FromSlash(ph.File)))
	if err != nil {
		return nil, err
	}
	return Geometry(im, ex.Orientation, ph.Rotate, ph.Crop), nil
}

func (a *App) enhance(ctx context.Context, p Product) ([]string, error) {
	up, fill := boolOption(p.Info, "auto_upscale", a.cfg.AutoUpscale), boolOption(p.Info, "auto_fill", a.cfg.AutoFill)
	if !up && !fill {
		return nil, nil
	}
	photos, err := a.photos(p.ID)
	if err != nil {
		return nil, err
	}
	doc, promptErr := a.prompt("补图.md")
	parts := enhancementSections(doc)
	warnings := []string{}
	if up {
		for _, ph := range photos {
			if ctx.Err() != nil {
				return warnings, ctx.Err()
			}
			if ph.AIGenerated || !a.usedCode(ph.Code) {
				continue
			}
			if ph.EnhancedFile != "" {
				if _, err := os.Stat(filepath.Join(a.dir(p.ID), filepath.FromSlash(ph.EnhancedFile))); err == nil {
					continue
				}
				ph.EnhancedFile = ""
				ph.AIEnhanced = false
				if _, err = a.db.Exec("UPDATE photos SET enhanced_file='',ai_enhanced=0 WHERE id=?", ph.ID); err != nil {
					return warnings, err
				}
			}
			im, ex, loadErr := loadPhoto(filepath.Join(a.dir(p.ID), filepath.FromSlash(ph.File)))
			if loadErr == nil {
				im = Geometry(im, ex.Orientation, ph.Rotate, ph.Crop)
				if min(im.Bounds().Dx(), im.Bounds().Dy()) >= a.cfg.UpscaleMinSide {
					continue
				}
			}
			var data []byte
			upErr := loadErr
			if upErr == nil {
				upErr = promptErr
			}
			if upErr == nil {
				var prompt string
				prompt, upErr = enhancementPrompt(parts, "高清化", p)
				if upErr == nil {
					data, upErr = a.editImage(ctx, prompt, []image.Image{im})
				}
			}
			if upErr == nil {
				file := fmt.Sprintf("原图/ai_up_%d.png", ph.ID)
				upErr = writeAIFile(filepath.Join(a.dir(p.ID), filepath.FromSlash(file)), data)
				if upErr == nil {
					delete(ph.RetouchLog, "upscale_error")
					_, err = a.db.Exec("UPDATE photos SET enhanced_file=?,ai_enhanced=1,retouch_log=? WHERE id=?", file, js(ph.RetouchLog), ph.ID)
					if err != nil {
						return warnings, err
					}
				}
			}
			if upErr != nil {
				message := fmt.Sprintf("照片 #%d 高清化失败：%s", ph.ID, a.safeAIError(upErr.Error()))
				warnings = append(warnings, message)
				ph.RetouchLog["upscale_error"] = message
				if _, err = a.db.Exec("UPDATE photos SET retouch_log=? WHERE id=?", js(ph.RetouchLog), ph.ID); err != nil {
					return warnings, err
				}
			}
		}
	}
	if !fill {
		return warnings, nil
	}
	photos, err = a.photos(p.ID)
	if err != nil {
		return warnings, err
	}
	targets := a.fillTargets(photos)
	for _, code := range targets {
		if ctx.Err() != nil {
			return warnings, ctx.Err()
		}
		if !allowedFillCode(code) {
			continue
		}
		photos, err = a.photos(p.ID)
		if err != nil {
			return warnings, err
		}
		count := 0
		reuseID := int64(0)
		file := "原图/ai_" + code + ".png"
		for _, ph := range photos {
			if ph.AIGenerated {
				count++
				if ph.File == file {
					reuseID = ph.ID
				}
			}
		}
		if count >= maxGeneratedPhotos && reuseID == 0 {
			warnings = append(warnings, "补图已达每款 4 张上限，跳过 "+code)
			continue
		}
		refs := selectReferences(photos, code)
		images := []image.Image{}
		refIDs := []string{}
		for _, ref := range refs {
			im, err := a.beforeRetouch(ref)
			if err != nil {
				continue
			}
			images = append(images, im)
			refIDs = append(refIDs, fmt.Sprintf("#%d", ref.ID))
		}
		fillErr := promptErr
		var data []byte
		if fillErr == nil && len(images) == 0 {
			fillErr = fmt.Errorf("没有可用的实拍参考图")
		}
		if fillErr == nil {
			var prompt string
			prompt, fillErr = enhancementPrompt(parts, code, p)
			if fillErr == nil {
				data, fillErr = a.editImage(ctx, prompt, images)
			}
		}
		if fillErr != nil {
			warnings = append(warnings, "补图 "+code+" 失败："+a.safeAIError(fillErr.Error()))
			continue
		}
		if err = writeAIFile(filepath.Join(a.dir(p.ID), filepath.FromSlash(file)), data); err != nil {
			warnings = append(warnings, "保存补图 "+code+" 失败")
			continue
		}
		cfg, _, _ := image.DecodeConfig(bytes.NewReader(data))
		note := "AI 生成（参考 " + strings.Join(refIDs, "、") + "）"
		if reuseID != 0 {
			_, err = a.db.Exec("UPDATE photos SET code=?,width=?,height=?,ai_note=?,white_bg=?,crop='null',rotate=0,enhanced_file='',ai_enhanced=0,manual=0 WHERE id=?", code, cfg.Width, cfg.Height, note, code == "A1", reuseID)
		} else {
			_, err = a.db.Exec("INSERT INTO photos(product_id,file,orig_name,width,height,code,white_bg,ai_note,ai_generated,source) VALUES(?,?,?,?,?,?,?,?,1,'ai')", p.ID, file, "ai_"+code+".png", cfg.Width, cfg.Height, code, code == "A1", note)
		}
		if err != nil {
			return warnings, fmt.Errorf("保存补图记录失败：%w", err)
		}
	}
	return warnings, nil
}
