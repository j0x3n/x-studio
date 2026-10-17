package studio

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

type DetailTemplate struct {
	Key       string      `json:"key"`
	Name      string      `json:"name"`
	File      string      `json:"file"`
	Fit       string      `json:"fit"`
	Optional  core.Object `json:"optional"`
	ThumbURL  string      `json:"thumb_url"`
	Disabled  bool        `json:"disabled"`
	IsDefault bool        `json:"is_default"`
}

func isValidDetailHTML(templateDir, file string) bool {
	if file == "" || filepath.Base(file) != file || strings.ContainsAny(file, "/\\:") || strings.Contains(file, "..") {
		return false
	}
	if !strings.EqualFold(filepath.Ext(file), ".html") {
		return false
	}
	st, e := os.Stat(filepath.Join(templateDir, "详情页模板", file))
	return e == nil && st.Mode().IsRegular()
}

func (a *Module) readManifest() (list []DetailTemplate, defaultKey string) {
	defaultKey = "navy"
	manifestPath := filepath.Join(a.Cfg.TemplateDir, "详情页模板", "模板清单.json")
	b, e := os.ReadFile(manifestPath)
	if e != nil {
		if isValidDetailHTML(a.Cfg.TemplateDir, "模板.html") {
			list = append(list, DetailTemplate{
				Key:      "navy",
				Name:     "深蓝烫金",
				File:     "模板.html",
				Optional: core.Object{},
			})
		}
		return
	}
	var raw struct {
		DefaultCN   string           `json:"默认"`
		DefaultEN   string           `json:"default"`
		TemplatesCN []DetailTemplate `json:"模板"`
		TemplatesEN []DetailTemplate `json:"templates"`
		Detail      []DetailTemplate `json:"detail"`
	}
	if e := json.Unmarshal(b, &raw); e != nil {
		log.Printf("解析模板清单失败：%v", e)
		return
	}
	if raw.DefaultCN != "" {
		defaultKey = raw.DefaultCN
	} else if raw.DefaultEN != "" {
		defaultKey = raw.DefaultEN
	}
	entries := raw.TemplatesCN
	if len(entries) == 0 {
		entries = raw.TemplatesEN
	}
	if len(entries) == 0 {
		entries = raw.Detail
	}
	seen := map[string]bool{}
	for _, item := range entries {
		item.Key = strings.TrimSpace(item.Key)
		item.File = strings.TrimSpace(item.File)
		if item.Key == "" || item.Name == "" || seen[item.Key] || !isValidDetailHTML(a.Cfg.TemplateDir, item.File) {
			log.Printf("跳过不合法的详情页模板条目：key=%q file=%q", item.Key, item.File)
			continue
		}
		seen[item.Key] = true
		if item.Optional == nil {
			item.Optional = core.Object{}
		}
		thumbPath := filepath.Join(a.Cfg.TemplateDir, "详情页模板", "缩略图", item.Key+".jpg")
		if st, err := os.Stat(thumbPath); err == nil && st.Mode().IsRegular() {
			item.ThumbURL = "/render/详情页模板/缩略图/" + item.Key + ".jpg"
		}
		list = append(list, item)
	}
	return
}

func (a *Module) shopTemplatePrefs(shopID int64, manifestDefault string) (defaultDetail string, disabled []string) {
	defaultDetail = manifestDefault
	if defaultDetail == "" {
		defaultDetail = "navy"
	}
	disabled = []string{}
	if shopID <= 0 {
		return
	}
	var def, disRaw string
	if a.DB.QueryRow("SELECT default_detail,disabled FROM template_prefs WHERE shop_id=?", shopID).Scan(&def, &disRaw) == nil {
		if def != "" {
			defaultDetail = def
		}
		_ = json.Unmarshal([]byte(disRaw), &disabled)
		if disabled == nil {
			disabled = []string{}
		}
	}
	return
}

func (a *Module) resolveListingDetailFile(listingID, shopID int64) (effectiveKey, file string) {
	var detailTpl string
	_ = a.DB.QueryRow("SELECT detail_template FROM listing_render WHERE listing_id=?", listingID).Scan(&detailTpl)
	list, manifestDef := a.readManifest()
	shopDef, disabled := a.shopTemplatePrefs(shopID, manifestDef)
	chosen := strings.TrimSpace(detailTpl)
	if chosen == "" {
		chosen = shopDef
	}
	if chosen == "" {
		chosen = "navy"
	}
	for _, t := range list {
		if t.Key == chosen && !slices.Contains(disabled, t.Key) {
			return t.Key, t.File
		}
	}
	return "navy", "模板.html"
}

func (a *Module) listTemplates(w http.ResponseWriter, r *http.Request) {
	list, manifestDef := a.readManifest()
	var shopID int64
	if s := strings.TrimSpace(r.URL.Query().Get("shop_id")); s != "" {
		sid, e := strconv.ParseInt(s, 10, 64)
		if e != nil || sid < 1 || !a.CanShop(core.CurrentUser(r), sid) {
			core.Fail(w, 404, "店铺不存在")
			return
		}
		shopID = sid
	}
	defaultKey, disabled := a.shopTemplatePrefs(shopID, manifestDef)
	for i := range list {
		list[i].Disabled = slices.Contains(disabled, list[i].Key)
		list[i].IsDefault = list[i].Key == defaultKey
	}
	mainTemplates := []core.Object{
		{"key": "default", "name": "默认主图", "file": "模板.html"},
	}
	core.WriteJSON(w, 200, core.Object{
		"detail": list,
		"main":   mainTemplates,
	})
}

func (a *Module) putTemplatePrefs(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	shopID := core.ID(r, "id")
	u := core.CurrentUser(r)
	if !a.CanShop(u, shopID) {
		core.Fail(w, 404, "店铺不存在")
		return
	}
	var in struct {
		DefaultDetail string   `json:"default_detail"`
		Disabled      []string `json:"disabled"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	in.DefaultDetail = strings.TrimSpace(in.DefaultDetail)
	if in.DefaultDetail == "" {
		in.DefaultDetail = "navy"
	}
	if in.Disabled == nil {
		in.Disabled = []string{}
	}
	list, _ := a.readManifest()
	validKeys := map[string]bool{}
	for _, t := range list {
		validKeys[t.Key] = true
	}
	if !validKeys[in.DefaultDetail] {
		core.Fail(w, 400, "默认模板不存在")
		return
	}
	dedupDisabled := []string{}
	seen := map[string]bool{}
	for _, k := range in.Disabled {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		if !validKeys[k] {
			core.Fail(w, 400, "停用的模板不存在："+k)
			return
		}
		if k == in.DefaultDetail {
			core.Fail(w, 400, "默认模板不能同时停用")
			return
		}
		seen[k] = true
		dedupDisabled = append(dedupDisabled, k)
	}
	ts := core.Now()
	_, e := a.DB.Exec(
		"INSERT INTO template_prefs(shop_id,default_detail,disabled,updated_by,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(shop_id) DO UPDATE SET default_detail=excluded.default_detail,disabled=excluded.disabled,updated_by=excluded.updated_by,updated_at=excluded.updated_at",
		shopID, in.DefaultDetail, core.JS(dedupDisabled), u.ID, ts,
	)
	if e != nil {
		core.Fail(w, 500, "保存店铺模板偏好失败")
		return
	}
	var updatedBy sql.NullInt64
	updatedBy.Valid = true
	updatedBy.Int64 = u.ID
	prefs := core.Object{
		"shop_id":        shopID,
		"default_detail": in.DefaultDetail,
		"disabled":       dedupDisabled,
		"updated_by":     updatedBy.Int64,
		"updated_at":     ts,
	}
	a.Audit(r, "studio.template_prefs", "shop", shopID, prefs)
	core.WriteJSON(w, 200, core.Object{
		"prefs":          prefs,
		"shop_id":        shopID,
		"default_detail": in.DefaultDetail,
		"disabled":       dedupDisabled,
		"updated_by":     updatedBy.Int64,
		"updated_at":     ts,
	})
}
