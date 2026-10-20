package pipeline

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
	"xstudio/internal/core"
	"xstudio/internal/media"
)

const maxGeneratedPhotos = 4

// allowedFillCode 只认带 _AI 后缀的补图编号；模特图（原来的 D4）改由 creative 模块出。
func allowedFillCode(code string) bool {
	_, ok := media.FillBase(code)
	return ok
}

// fillBaseCode 判断槽位候选编号能不能由 AI 补：A1、E1、E2、E3。
func fillBaseCode(code string) bool {
	_, ok := media.FillBase(code + media.AISuffix)
	return ok
}
func codeMatches(code, candidate string) bool {
	return code != "" && (code == candidate || strings.HasPrefix(code, candidate+"_"))
}

func (a *Module) slotsList(categories ...*core.Category) []slot {
	var input map[string]json.RawMessage
	raw := a.Slots
	if len(categories) > 0 && categories[0] != nil {
		raw = categories[0].Slots
	}
	_ = json.Unmarshal(raw, &input)
	out := []slot{}
	for _, key := range []string{"主图", "详情页"} {
		var group []slot
		_ = json.Unmarshal(input[key], &group)
		out = append(out, group...)
	}
	return out
}
// slotHits 按候选顺序找照片，同一编号实拍排在 AI 图前面。
// AI 生成或改过的图只能进可以补图的位置（首图、首屏、白底），实拍证据位只认实拍。
func slotHits(s slot, photos []Photo) []Photo {
	out := []Photo{}
	fillable := slotAllowsFill(s)
	for _, candidate := range s.Candidates {
		var ai []Photo
		for _, ph := range photos {
			if !codeMatches(ph.Code, candidate) {
				continue
			}
			if ph.AIGenerated && !allowedFillCode(ph.Code) {
				continue
			}
			if media.AIMade(ph) {
				if fillable {
					ai = append(ai, ph)
				}
				continue
			}
			out = append(out, ph)
		}
		out = append(out, ai...)
	}
	return out
}
func slotAllowsFill(s slot) bool {
	for _, candidate := range s.Candidates {
		if fillBaseCode(candidate) {
			return true
		}
	}
	return false
}
func (a *Module) usedCode(code string, categories ...*core.Category) bool {
	for _, s := range a.slotsList(categories...) {
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

func (a *Module) fillTargets(photos []Photo, categories ...*core.Category) []string {
	white, hero := false, false
	for _, s := range a.slotsList(categories...) {
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
		out = append(out, "A1"+media.AISuffix)
	}
	if hero {
		out = append(out, "E1"+media.AISuffix)
	}
	return out
}

func selectReferences(photos []Photo) []Photo {
	priority := []string{"A1", "A2", "F2", "E4", "B2", "B3"}
	real := []Photo{}
	for _, ph := range photos {
		if !media.AIMade(ph) && ph.Code != "" {
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
	return out
}
func (a *Module) beforeRetouch(ph Photo) (*image.NRGBA, error) {
	if ph.EnhancedFile != "" {
		im, _, err := loadPhoto(filepath.Join(a.Dir(ph.ProductID), filepath.FromSlash(ph.EnhancedFile)))
		return im, err
	}
	im, ex, err := loadPhoto(filepath.Join(a.Dir(ph.ProductID), filepath.FromSlash(ph.File)))
	if err != nil {
		return nil, err
	}
	return Geometry(im, ex.Orientation, ph.Rotate, ph.Crop), nil
}

func (a *Module) enhance(ctx context.Context, p Product) ([]string, error) {
	up, fill := boolOption(p.Info, "auto_upscale", a.Cfg.AutoUpscale), boolOption(p.Info, "auto_fill", a.Cfg.AutoFill)
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
			if ph.AIGenerated || !a.usedCode(ph.Code, p.Category) {
				continue
			}
			if ph.EnhancedFile != "" {
				if _, err := os.Stat(filepath.Join(a.Dir(p.ID), filepath.FromSlash(ph.EnhancedFile))); err == nil {
					continue
				}
				ph.EnhancedFile = ""
				ph.AIEnhanced = false
				if _, err = a.DB.Exec("UPDATE assets SET enhanced_file='',ai_enhanced=0 WHERE id=?", ph.ID); err != nil {
					return warnings, err
				}
			}
			im, ex, loadErr := loadPhoto(filepath.Join(a.Dir(p.ID), filepath.FromSlash(ph.File)))
			if loadErr == nil {
				im = Geometry(im, ex.Orientation, ph.Rotate, ph.Crop)
				if min(im.Bounds().Dx(), im.Bounds().Dy()) >= a.Cfg.UpscaleMinSide {
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
					data, upErr = a.editImage(core.WithFeature(ctx, "enhance"), prompt, []image.Image{im})
				}
			}
			if upErr == nil {
				file := fmt.Sprintf("原图/ai_up_%d.png", ph.ID)
				var versions int
				if err = a.DB.QueryRow("SELECT count(*) FROM assets WHERE parent_id=? AND source='ai_enhanced'", ph.ID).Scan(&versions); err != nil {
					return warnings, err
				}
				if versions > 0 {
					file = fmt.Sprintf("原图/ai_up_%d_v%d.png", ph.ID, versions+1)
				}
				upErr = writeAIFile(filepath.Join(a.Dir(p.ID), filepath.FromSlash(file)), data)
				if upErr == nil {
					delete(ph.RetouchLog, "upscale_error")
					// Preserve the input asset and also record the generated version.
					cfg, _, _ := image.DecodeConfig(bytes.NewReader(data))
					if err = a.recordEnhancement(ctx, p.ID, ph, file, cfg.Width, cfg.Height); err != nil {
						return warnings, err
					}
				}
			}
			if upErr != nil {
				message := fmt.Sprintf("照片 #%d 高清化失败：%s", ph.ID, a.safeAIError(upErr.Error()))
				warnings = append(warnings, message)
				ph.RetouchLog["upscale_error"] = message
				if _, err = a.DB.Exec("UPDATE assets SET retouch_log=? WHERE id=?", js(ph.RetouchLog), ph.ID); err != nil {
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
	targets := a.fillTargets(photos, p.Category)
	for _, code := range targets {
		if ctx.Err() != nil {
			return warnings, ctx.Err()
		}
		base, ok := media.FillBase(code)
		if !ok {
			continue
		}
		photos, err = a.photos(p.ID)
		if err != nil {
			return warnings, err
		}
		count := 0
		reuseID := int64(0)
		file := "原图/ai_" + base + ".png"
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
		refs := selectReferences(photos)
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
			prompt, fillErr = enhancementPrompt(parts, base, p)
			if fillErr == nil {
				data, fillErr = a.editImage(core.WithFeature(ctx, "fill"), prompt, images)
			}
		}
		if fillErr != nil {
			warnings = append(warnings, "补图 "+code+" 失败："+a.safeAIError(fillErr.Error()))
			continue
		}
		if err = writeAIFile(filepath.Join(a.Dir(p.ID), filepath.FromSlash(file)), data); err != nil {
			warnings = append(warnings, "保存补图 "+code+" 失败")
			continue
		}
		cfg, _, _ := image.DecodeConfig(bytes.NewReader(data))
		note := "AI 生成（参考 " + strings.Join(refIDs, "、") + "）"
		op := js(Object{"feature": "fill", "code": code, "references": refIDs})
		if reuseID != 0 {
			_, err = a.DB.Exec("UPDATE assets SET code=?,width=?,height=?,ai_note=?,white_bg=?,crop='null',rotate=0,enhanced_file='',ai_enhanced=0,manual=0,source='ai_generated',source_override='',parent_id=?,created_by=?,created_at=?,op=? WHERE id=?", code, cfg.Width, cfg.Height, note, base == "A1", refs[0].ID, core.Actor(ctx).UserID, now(), op, reuseID)
		} else {
			_, err = a.DB.Exec("INSERT INTO assets(product_id,file,orig_name,width,height,code,white_bg,ai_note,ai_generated,source,created_by,created_at,parent_id,op) VALUES(?,?,?,?,?,?,?,?,1,'ai_generated',?,?,?,?)", p.ID, file, "ai_"+base+".png", cfg.Width, cfg.Height, code, base == "A1", note, core.Actor(ctx).UserID, now(), refs[0].ID, op)
		}
		if err != nil {
			return warnings, fmt.Errorf("保存补图记录失败：%w", err)
		}
	}
	return warnings, nil
}
