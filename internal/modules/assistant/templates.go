package assistant

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type detailEntry struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	File string `json:"file"`
}

// Keep manifest selection and preference semantics aligned with studio. This
// reader deliberately never calls ensureListingRender (which inserts rows).
func (a *Module) manifest() ([]detailEntry, string, error) {
	valid := func(file string) bool {
		if file == "" || filepath.Base(file) != file || strings.ContainsAny(file, "/\\:") || strings.Contains(file, "..") || !strings.EqualFold(filepath.Ext(file), ".html") {
			return false
		}
		st, err := os.Stat(filepath.Join(a.Cfg.TemplateDir, "详情页模板", file))
		return err == nil && st.Mode().IsRegular()
	}
	entries := []detailEntry{}
	def := "navy"
	b, err := os.ReadFile(filepath.Join(a.Cfg.TemplateDir, "详情页模板", "模板清单.json"))
	if err != nil {
		if valid("模板.html") {
			entries = append(entries, detailEntry{"navy", "深蓝烫金", "模板.html"})
		}
		return entries, def, nil
	}
	var raw struct {
		DefaultCN string        `json:"默认"`
		DefaultEN string        `json:"default"`
		CN        []detailEntry `json:"模板"`
		EN        []detailEntry `json:"templates"`
		Detail    []detailEntry `json:"detail"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return nil, "", fmt.Errorf("模板清单格式不正确")
	}
	if raw.DefaultCN != "" {
		def = raw.DefaultCN
	} else if raw.DefaultEN != "" {
		def = raw.DefaultEN
	}
	list := raw.CN
	if len(list) == 0 {
		list = raw.EN
	}
	if len(list) == 0 {
		list = raw.Detail
	}
	seen := map[string]bool{}
	for _, v := range list {
		v.Key = strings.TrimSpace(v.Key)
		v.File = strings.TrimSpace(v.File)
		if v.Key != "" && v.Name != "" && !seen[v.Key] && valid(v.File) {
			seen[v.Key] = true
			entries = append(entries, v)
		}
	}
	return entries, def, nil
}
func (a *Module) templatePrefs(shop int64, def string) (string, []string) {
	var chosen, raw string
	disabled := []string{}
	if shop > 0 && a.DB.QueryRow("SELECT default_detail,disabled FROM template_prefs WHERE shop_id=?", shop).Scan(&chosen, &raw) == nil {
		if chosen != "" {
			def = chosen
		}
		_ = json.Unmarshal([]byte(raw), &disabled)
	}
	return def, disabled
}
func (a *Module) templates(shop int64) (object, error) {
	entries, def, err := a.manifest()
	if err != nil {
		return nil, err
	}
	def, disabled := a.templatePrefs(shop, def)
	detail := []object{}
	for _, v := range entries {
		detail = append(detail, object{"key": v.Key, "name": v.Name, "is_default": v.Key == def, "disabled": slices.Contains(disabled, v.Key)})
	}
	return object{"detail": detail, "main": []object{{"key": "default", "name": "默认主图", "is_default": true, "disabled": false}}}, nil
}
func (a *Module) detailTemplate(listing, shop int64) (string, string, error) {
	entries, def, err := a.manifest()
	if err != nil {
		return "", "", err
	}
	def, disabled := a.templatePrefs(shop, def)
	var chosen string
	_ = a.DB.QueryRow("SELECT detail_template FROM listing_render WHERE listing_id=?", listing).Scan(&chosen)
	chosen = strings.TrimSpace(chosen)
	if chosen == "" {
		chosen = def
	}
	for _, v := range entries {
		if v.Key == chosen && !slices.Contains(disabled, v.Key) {
			return v.Key, v.File, nil
		}
	}
	return "navy", "模板.html", nil
}
func (a *Module) outputs(id int64) object {
	out := object{"main34": []string{}, "main11": []string{}, "detail": []string{}, "white": "", "zip": ""}
	for key, sub := range map[string]string{"main34": "主图/3x4", "main11": "主图/1x1", "detail": "详情页"} {
		files, _ := filepath.Glob(filepath.Join(a.ListingDir(id), "导出", filepath.FromSlash(sub), "*.jpg"))
		urls := []string{}
		for _, f := range files {
			urls = append(urls, fmt.Sprintf("/files/listings/%d/导出/%s/%s", id, sub, filepath.Base(f)))
		}
		out[key] = urls
	}
	if _, err := os.Stat(filepath.Join(a.ListingDir(id), "导出", "白底.jpg")); err == nil {
		out["white"] = fmt.Sprintf("/files/listings/%d/导出/白底.jpg", id)
	}
	if _, err := os.Stat(filepath.Join(a.ListingDir(id), "导出.zip")); err == nil {
		out["zip"] = fmt.Sprintf("/api/listings/%d/download", id)
	}
	return out
}
