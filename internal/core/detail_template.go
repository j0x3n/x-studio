package core

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type DetailTemplate struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	File       string   `json:"file"`
	Fit        string   `json:"fit"`
	Optional   Object   `json:"optional"`
	ThumbURL   string   `json:"thumb_url"`
	Disabled   bool     `json:"disabled"`
	IsDefault  bool     `json:"is_default"`
	Categories []string `json:"categories"`
}

func (t DetailTemplate) Applies(key string) bool {
	return len(t.Categories) == 0 || slices.Contains(t.Categories, key)
}
func (a *Core) validDetailFile(file string) bool {
	if file == "" || filepath.Base(file) != file || strings.ContainsAny(file, "/\\:") || strings.Contains(file, "..") || !strings.EqualFold(filepath.Ext(file), ".html") {
		return false
	}
	st, e := os.Stat(filepath.Join(a.Cfg.TemplateDir, "详情页模板", file))
	return e == nil && st.Mode().IsRegular()
}
func (a *Core) DetailTemplates() ([]DetailTemplate, string) {
	list := []DetailTemplate{}
	def := "navy"
	b, e := os.ReadFile(filepath.Join(a.Cfg.TemplateDir, "详情页模板", "模板清单.json"))
	if e != nil {
		if a.validDetailFile("模板.html") {
			list = append(list, DetailTemplate{Key: "navy", Name: "深蓝烫金", File: "模板.html", Optional: Object{}, Categories: []string{}})
		}
		return list, def
	}
	var raw struct {
		DefaultCN string           `json:"默认"`
		DefaultEN string           `json:"default"`
		CN        []DetailTemplate `json:"模板"`
		EN        []DetailTemplate `json:"templates"`
		Detail    []DetailTemplate `json:"detail"`
	}
	if e = json.Unmarshal(b, &raw); e != nil {
		log.Printf("解析模板清单失败：%v", e)
		return list, def
	}
	if raw.DefaultCN != "" {
		def = raw.DefaultCN
	} else if raw.DefaultEN != "" {
		def = raw.DefaultEN
	}
	entries := raw.CN
	if len(entries) == 0 {
		entries = raw.EN
	}
	if len(entries) == 0 {
		entries = raw.Detail
	}
	seen := map[string]bool{}
	for _, v := range entries {
		v.Key = strings.TrimSpace(v.Key)
		v.File = strings.TrimSpace(v.File)
		if v.Key == "" || v.Name == "" || seen[v.Key] || !a.validDetailFile(v.File) {
			continue
		}
		seen[v.Key] = true
		if v.Optional == nil {
			v.Optional = Object{}
		}
		if v.Categories == nil {
			v.Categories = []string{}
		}
		if st, e := os.Stat(filepath.Join(a.Cfg.TemplateDir, "详情页模板", "缩略图", v.Key+".jpg")); e == nil && st.Mode().IsRegular() {
			v.ThumbURL = "/render/详情页模板/缩略图/" + v.Key + ".jpg"
		}
		list = append(list, v)
	}
	return list, def
}

// ResolveDetailTemplate is shared by preview and the actual render job.
func (a *Core) ResolveDetailTemplate(listingID, shopID int64) (file, key, note string, err error) {
	var chosen, shopDef, disabledRaw string
	if e := a.DB.QueryRow("SELECT detail_template FROM listing_render WHERE listing_id=?", listingID).Scan(&chosen); e != nil && e != sql.ErrNoRows {
		return "", "", "", e
	}
	if e := a.DB.QueryRow("SELECT default_detail,disabled FROM template_prefs WHERE shop_id=?", shopID).Scan(&shopDef, &disabledRaw); e != nil && e != sql.ErrNoRows {
		return "", "", "", e
	}
	var pid int64
	if err = a.DB.QueryRow("SELECT product_id FROM listings WHERE id=?", listingID).Scan(&pid); err != nil {
		return
	}
	c, e := a.ProductCategory(pid)
	if e != nil {
		err = e
		return
	}
	var disabled []string
	_ = json.Unmarshal([]byte(disabledRaw), &disabled)
	list, manifestDef := a.DetailTemplates()
	byKey := map[string]DetailTemplate{}
	for _, t := range list {
		byKey[t.Key] = t
	}
	requested := strings.TrimSpace(chosen)
	if requested == "" {
		requested = strings.TrimSpace(shopDef)
	}
	if requested != "" {
		t, exists := byKey[requested]
		switch {
		case !exists:
			note = "详情页模板 " + requested + " 不存在"
		case slices.Contains(disabled, requested):
			note = "详情页模板 " + requested + " 已禁用"
		case !t.Applies(c.Key):
			note = t.Name + "不适用于" + c.Name
		}
	}
	candidates := []string{chosen, shopDef, c.DefaultTemplate, manifestDef}
	for _, t := range list {
		candidates = append(candidates, t.Key)
	}
	for _, candidate := range candidates {
		if t, ok := byKey[strings.TrimSpace(candidate)]; ok && !slices.Contains(disabled, t.Key) && t.Applies(c.Key) {
			if note != "" {
				note += "，已回退到 " + t.File
			}
			return t.File, t.Key, note, nil
		}
	}
	if t, ok := byKey[manifestDef]; ok {
		return t.File, t.Key, fmt.Sprintf("没有适用于%s的详情页模板，先用了%s", c.Name, t.Name), nil
	}
	return "模板.html", "navy", fmt.Sprintf("没有适用于%s的详情页模板，先用了深蓝烫金", c.Name), nil
}
