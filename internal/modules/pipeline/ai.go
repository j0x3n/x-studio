package pipeline

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"xstudio/internal/core"
)

func (a *Module) prompt(name string) (string, error) {
	b, e := os.ReadFile(filepath.Join(a.Cfg.PromptDir, name))
	if e != nil {
		return "", fmt.Errorf("缺少提示词文件：%s，请检查 PROMPT_DIR", name)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return "", fmt.Errorf("提示词文件为空：%s", name)
	}
	return string(b), nil
}

type classification struct {
	PhotoID int64     `json:"photo_id"`
	Code    string    `json:"code"`
	Crop    []float64 `json:"crop"`
	Rotate  int       `json:"rotate"`
	WhiteBG bool      `json:"white_bg"`
	Note    string    `json:"note"`
}

func (a *Module) classify(ctx context.Context, p Product) error {
	prompt, e := a.prompt("分拣.md")
	if e != nil {
		return e
	}
	photos, e := a.photos(p.ID)
	if e != nil {
		return e
	}
	real := []Photo{}
	for _, ph := range photos {
		if !ph.AIGenerated {
			real = append(real, ph)
		}
	}
	photos = real
	if len(photos) == 0 {
		return fmt.Errorf("请先上传照片")
	}
	parts := []Object{{"type": "text", "text": js(Object{"name": p.Name, "info": p.Info, "slots": json.RawMessage(a.Slots), "说明": "照片已按 EXIF 转正。crop 是手动 rotate 顺时针旋转后的 [x,y,w,h] 比例。返回 [{photo_id,code,crop,rotate,white_bg,note}]；JSON 对象模式可用 {photos:[...]}。"})}}
	for _, ph := range photos {
		im, ex, e := loadPhoto(filepath.Join(a.Dir(p.ID), filepath.FromSlash(ph.File)))
		if e != nil {
			return fmt.Errorf("读取原图失败：%s", ph.OrigName)
		}
		im = resize(oriented(im, ex.Orientation), 1024)
		var buf bytes.Buffer
		_ = jpeg.Encode(&buf, im, &jpeg.Options{Quality: 85})
		parts = append(parts, Object{"type": "text", "text": fmt.Sprintf("photo_id=%d，员工已手动指定=%t，编号=%s，检查结果=%s", ph.ID, ph.Manual, ph.Code, js(ph.Metrics))}, Object{"type": "image_url", "image_url": Object{"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())}})
	}
	var raw json.RawMessage
	if e = a.aiJSON(core.WithFeature(ctx, "classify"), a.Cfg.VisionModel, prompt, parts, &raw); e != nil {
		return e
	}
	var result []classification
	if len(raw) > 0 && raw[0] == '{' {
		var wrap struct {
			Photos []classification `json:"photos"`
		}
		if e = json.Unmarshal(raw, &wrap); e != nil || wrap.Photos == nil {
			return fmt.Errorf("AI 分拣结果必须是照片数组")
		}
		result = wrap.Photos
	} else if e = json.Unmarshal(raw, &result); e != nil || result == nil {
		return fmt.Errorf("AI 分拣结果必须是照片数组")
	}
	known := map[int64]Photo{}
	used := map[string]bool{}
	for _, ph := range photos {
		known[ph.ID] = ph
		if ph.Manual && ph.Code != "" {
			used[ph.Code] = true
		}
	}
	updates := []Photo{}
	seen := map[int64]bool{}
	for _, v := range result {
		ph, ok := known[v.PhotoID]
		if !ok || ph.Manual || seen[v.PhotoID] {
			continue
		}
		seen[v.PhotoID] = true
		ph.Code = v.Code
		ph.Crop = v.Crop
		ph.Rotate = v.Rotate
		ph.WhiteBG = v.WhiteBG
		ph.AINote = v.Note
		if e := validatePhoto(ph); e != nil {
			ph.Code = ""
			ph.Crop = nil
			ph.Rotate = 0
			ph.WhiteBG = false
			ph.AINote += "；AI 设置无效：" + e.Error()
		}
		if ph.Code != "" {
			if used[ph.Code] {
				ph.Code = ""
				ph.AINote += "；编号重复，保留第一张（或员工手动指定的照片）"
			} else {
				used[ph.Code] = true
			}
		}
		updates = append(updates, ph)
	}
	// A transaction clears old automatic assignments first, so code swaps do not
	// trip the unique index. Manual assignments remain reserved throughout.
	tx, e := a.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("UPDATE assets SET code='',crop='null',rotate=0,white_bg=0,ai_note='',enhanced_file='',ai_enhanced=0 WHERE product_id=? AND manual=0 AND ai_generated=0", p.ID); e != nil {
		return e
	}
	invalidate := map[string]bool{}
	for _, ph := range photos {
		if !ph.Manual && !seen[ph.ID] {
			invalidate[ph.Code] = true
		}
	}
	for _, ph := range updates {
		if ph.Code != "" {
			result, err := tx.Exec("UPDATE assets SET code='' WHERE product_id=? AND ai_generated=1 AND code=?", p.ID, ph.Code)
			if err != nil {
				return err
			}
			if n, _ := result.RowsAffected(); n > 0 {
				invalidate[ph.Code] = true
			}
		}
		old := known[ph.ID]
		if ph.Code != old.Code {
			invalidate[old.Code] = true
			invalidate[ph.Code] = true
		}
		if ph.Rotate != old.Rotate || js(ph.Crop) != js(old.Crop) {
			invalidate[old.Code] = true
			invalidate[ph.Code] = true
			ph.EnhancedFile = ""
			ph.AIEnhanced = false
			delete(ph.RetouchLog, "upscale_error")
		}
		if _, e = tx.Exec("UPDATE assets SET code=?,crop=?,rotate=?,white_bg=?,ai_note=?,enhanced_file=?,ai_enhanced=?,retouch_log=? WHERE id=?", ph.Code, js(ph.Crop), ph.Rotate, ph.WhiteBG, ph.AINote, ph.EnhancedFile, ph.AIEnhanced, js(ph.RetouchLog), ph.ID); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	// Until retouch regenerates the file, an old AI JPEG must not appear under
	// a newly assigned real photo's code without its original provenance label.
	for code := range invalidate {
		a.removePhotoOutputs(p.ID, code)
	}
	return nil
}
func (a *Module) copyText(ctx context.Context, p Product) error {
	prompt, e := a.prompt("文案.md")
	if e != nil {
		return e
	}
	banned, e := a.prompt("违禁词.txt")
	if e != nil {
		return e
	}
	samples := Object{}
	for _, name := range []string{"内容.js", "主图.js"} {
		b, e := os.ReadFile(filepath.Join(a.Cfg.TemplateDir, "详情页模板", "商品", "示例款", name))
		if e != nil {
			return fmt.Errorf("缺少格式样例 %s，请先同步模板", name)
		}
		samples[name] = string(b)
	}
	photos, e := a.photos(p.ID)
	if e != nil {
		return e
	}
	notes := []Object{}
	for _, ph := range photos {
		if ph.Code != "" {
			notes = append(notes, Object{"code": ph.Code, "file": ph.Code + ".jpg", "note": ph.AINote, "ai_generated": ph.AIGenerated, "ai_enhanced": ph.AIEnhanced})
		}
	}
	entries, _ := os.ReadDir(filepath.Join(a.Dir(p.ID), "照片"))
	files := []string{}
	for _, f := range entries {
		if !f.IsDir() {
			files = append(files, f.Name())
		}
	}
	input := Object{"platform": p.Platform, "language": p.Language, "name": p.Name, "info": p.Info, "photos": notes, "files": files, "coverage": a.coverage(photos), "slots": json.RawMessage(a.Slots), "samples": samples, "要求": "返回 {content,main,listing} 三个 JSON 对象，按样例字段类型。不得虚构材质、承诺或照片。"}
	var result struct {
		Content Object `json:"content"`
		Main    Object `json:"main"`
		Listing Object `json:"listing"`
	}
	if e = a.aiJSON(core.WithFeature(ctx, "copy"), a.Cfg.TextModel, prompt+"\nAI 生成或增强的照片不得称为实拍、原相机、无滤镜或作为材质真伪的证据。", js(input), &result); e != nil {
		return e
	}
	if result.Content == nil || result.Main == nil || result.Listing == nil {
		return fmt.Errorf("AI 文案必须包含 content、main、listing 三个对象")
	}
	result.Main = cleanMain(result.Main, filepath.Join(a.Dir(p.ID), "照片"))
	if e = a.annotateAI(p.ID, result.Content, result.Main); e != nil {
		return e
	}
	if e = validateCopy(result.Content, result.Main, result.Listing); e != nil {
		return e
	}
	normalizeCopy(result.Content, result.Main, result.Listing)
	result.Content = cleanObject(result.Content, filepath.Join(a.Dir(p.ID), "照片"))
	result.Listing["compliance"] = appendCompliance(result.Content, result.Main, result.Listing, banned)
	_, e = a.DB.Exec("UPDATE listings SET content=?,main=?,meta=?,updated_at=? WHERE id=?", js(result.Content), js(result.Main), js(result.Listing), now(), p.ListingID)
	return e
}

func cleanObject(o Object, dir string) Object {
	v, ok := cleanPhotos(o, dir).(Object)
	if !ok {
		return Object{}
	}
	return v
}
func cleanPhotos(v any, dir string) any {
	switch x := v.(type) {
	case map[string]any:
		if f, exists := x["f"]; exists {
			s, ok := f.(string)
			if !ok || s == "" || !safePhotoFile(dir, s) {
				return nil
			}
		}
		o := Object{}
		for k, v := range x {
			c := cleanPhotos(v, dir)
			if c != nil {
				o[k] = c
			}
		}
		return o
	case []any:
		o := []any{}
		for _, v := range x {
			if c := cleanPhotos(v, dir); c != nil {
				o = append(o, c)
			}
		}
		return o
	default:
		return v
	}
}
func safePhotoFile(dir, name string) bool {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\:") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".jpg" && ext != ".png" && ext != ".webp" {
		return false
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return false
	}
	defer root.Close()
	f, e := root.Open(name)
	if e != nil {
		return false
	}
	defer f.Close()
	st, e := f.Stat()
	return e == nil && st.Mode().IsRegular()
}
func appendCompliance(content, main, listing Object, words string) []any {
	items := []any{}
	if prior, ok := listing["compliance"].([]any); ok {
		for _, v := range prior {
			if o, ok := v.(Object); ok && o["source"] == "local" {
				continue
			}
			items = append(items, v)
		}
	}
	listingText := Object{}
	for k, v := range listing {
		if k != "compliance" {
			listingText[k] = v
		}
	}
	all := js(Object{"content": content, "main": main, "listing": listingText})
	seen := map[string]bool{}
	for _, line := range strings.Split(words, "\n") {
		word := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if word == "" || hasPrefix(word, "#", "//") {
			continue
		}
		level, description := "error", ""
		columns := strings.SplitN(strings.ReplaceAll(word, `\t`, "\t"), "\t", 3)
		if len(columns) >= 2 && (columns[0] == "error" || columns[0] == "warn") {
			level = columns[0]
			word = strings.TrimSpace(columns[1])
			if len(columns) == 3 {
				description = columns[2]
			}
		}
		if word == "" {
			continue
		}
		if strings.Contains(all, word) && !seen[word] {
			text := "含违禁词：" + word
			if description != "" {
				text += "（" + description + "）"
			}
			items = append(items, Object{"level": level, "text": text, "source": "local"})
			seen[word] = true
		}
	}
	if title, ok := listing["title"].(string); ok && titleLength(title) > 60 {
		items = append(items, Object{"level": "error", "text": "标题超过 60 字符（汉字算 2），请缩短后再上架", "source": "local"})
	}
	return items
}
func titleLength(s string) int {
	n := 0
	for _, r := range s {
		if r <= 255 {
			n++
		} else {
			n += 2
		}
	}
	return n
}
