package studio

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

type DetailTemplate = core.DetailTemplate

func (a *Module) readManifest() ([]DetailTemplate, string) { return a.DetailTemplates() }

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

func (a *Module) resolveListingDetailFile(listingID, shopID int64) (string, string) {
	file, key, _, _ := a.ResolveDetailTemplate(listingID, shopID)
	return key, file
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
	filtered := []DetailTemplate{}
	for _, t := range list {
		if key := r.URL.Query().Get("category"); key == "" || t.Applies(key) {
			filtered = append(filtered, t)
		}
	}
	list = filtered
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
