package creative

import (
	"net/http"
	"slices"
	"xstudio/internal/core"
)

func (a *Module) templatesList(w http.ResponseWriter, r *http.Request) {
	items, err := objects(r.Context(), a.DB, "SELECT * FROM creative_templates ORDER BY kind,slot,key")
	if err != nil {
		fail(w, err)
		return
	}
	out := []core.Object{}
	for _, item := range items {
		t, err := typed[Template](item)
		if err != nil {
			fail(w, err)
			return
		}
		if kind := r.URL.Query().Get("kind"); kind != "" && kind != t.Kind {
			continue
		}
		if category := r.URL.Query().Get("category"); category != "" && len(t.Categories) > 0 && !slices.Contains(t.Categories, category) {
			continue
		}
		out = append(out, item)
	}
	core.WriteJSON(w, 200, core.Object{"items": out, "total": len(out)})
}
func (a *Module) templateGet(w http.ResponseWriter, r *http.Request) {
	obj, err := a.one(r.Context(), "SELECT * FROM creative_templates WHERE key=?", r.PathValue("key"))
	if err != nil {
		core.Fail(w, 404, "模板不存在")
		return
	}
	core.WriteJSON(w, 200, obj)
}
func (a *Module) templatePatch(w http.ResponseWriter, r *http.Request) {
	var patch core.Object
	if !core.Decode(w, r, &patch) {
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	obj, err := a.one(r.Context(), "SELECT * FROM creative_templates WHERE key=?", r.PathValue("key"))
	if err != nil {
		core.Fail(w, 404, "模板不存在")
		return
	}
	version := int(number(obj["version"]))
	bump := false
	for key, value := range patch {
		if !slices.Contains([]string{"name", "kind", "slot", "categories", "purpose", "prompt", "style_refs", "texts", "photo_codes", "product_mode", "product_box", "gen_size", "export", "count", "wear", "enabled", "lock_shape", "size_compare", "hero_style"}, key) {
			core.Fail(w, 400, "不能修改字段："+key)
			return
		}
		if slices.Contains([]string{"prompt", "texts", "product_mode", "product_box", "gen_size", "lock_shape", "size_compare", "hero_style"}, key) && core.JS(obj[key]) != core.JS(value) {
			bump = true
		}
		obj[key] = value
	}
	t, err := typed[Template](obj)
	if err != nil {
		core.Fail(w, 400, "模板字段类型不正确")
		return
	}
	if err = validateTemplate(t); err != nil {
		fail(w, err)
		return
	}
	if bump {
		version++
	}
	t.Version = version
	if err = a.storeTemplate(r.Context(), t, core.CurrentUser(r).ID); err != nil {
		fail(w, err)
		return
	}
	a.Audit(r, "creative.template", "creative_template", t.ID, patch)
	a.templateGet(w, r)
}
func (a *Module) templateTrial(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ListingID int64 `json:"listing_id"`
	}
	if !core.Decode(w, r, &in) || !a.listingScope(w, r, in.ListingID) {
		return
	}
	t, err := a.template(r.Context(), r.PathValue("key"))
	if err != nil {
		fail(w, err)
		return
	}
	if !t.Enabled {
		core.Fail(w, 400, "模板已停用")
		return
	}
	product, err := a.PipelineProduct(in.ListingID)
	if err != nil {
		fail(w, err)
		return
	}
	if len(t.Categories) > 0 && !slices.Contains(t.Categories, product.Category.Key) {
		core.Fail(w, 400, "模板不适用于这个品类")
		return
	}
	uid := core.CurrentUser(r).ID
	if _, err = a.authorize(r.Context(), uid, in.ListingID, core.CreativeRun); err != nil {
		fail(w, err)
		return
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	item := Item{ListingID: in.ListingID, Slot: t.Slot, Seq: 1, TemplateKey: t.Key, TemplateVersion: t.Version, TemplateSnapshot: t, Attempt: 1, ProductMode: t.ProductMode, CreatedBy: uid}
	id, err := insertItem(r.Context(), tx, item)
	if err != nil {
		fail(w, err)
		return
	}
	job, err := enqueueTx(r.Context(), tx, "creative.item", "creative_item", id, uid, core.Object{"new": true})
	if err != nil {
		fail(w, err)
		return
	}
	if err = auditTx(r.Context(), tx, uid, "creative.trial", "creative_item", id, core.Object{"listing_id": in.ListingID}); err != nil {
		fail(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	a.Wake()
	obj, err := a.one(r.Context(), "SELECT * FROM creative_items WHERE id=?", id)
	if err == nil {
		err = a.enrichItem(r.Context(), obj)
	}
	if err != nil {
		fail(w, err)
		return
	}
	core.WriteJSON(w, 202, core.Object{"item": obj, "item_id": id, "job_id": job})
}
func (a *Module) shopScope(w http.ResponseWriter, r *http.Request) bool {
	var archived bool
	id := core.ID(r, "id")
	if !a.CanShop(core.CurrentUser(r), id) || a.DB.QueryRowContext(r.Context(), "SELECT archived FROM shops WHERE id=?", id).Scan(&archived) != nil || archived {
		core.Fail(w, 404, "店铺不存在")
		return false
	}
	return true
}
func (a *Module) shopGet(w http.ResponseWriter, r *http.Request) {
	if !a.shopScope(w, r) {
		return
	}
	obj, err := a.one(r.Context(), "SELECT * FROM creative_shop_settings WHERE shop_id=?", core.ID(r, "id"))
	if err != nil {
		obj = core.Object{"shop_id": core.ID(r, "id"), "models": core.Object{}, "template_set": core.Object{}}
	}
	core.WriteJSON(w, 200, obj)
}
func (a *Module) shopPut(w http.ResponseWriter, r *http.Request) {
	if !a.shopScope(w, r) {
		return
	}
	var in struct {
		Models      map[string]int64  `json:"models"`
		TemplateSet map[string]string `json:"template_set"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	id := core.ID(r, "id")
	for slot, mid := range in.Models {
		if !slices.Contains([]string{"female_main", "male", "hand"}, slot) {
			core.Fail(w, 400, "模特位不正确")
			return
		}
		if mid == 0 {
			continue
		}
		var count int
		err := a.DB.QueryRowContext(r.Context(), "SELECT count(*) FROM shop_models WHERE id=? AND shop_id=? AND archived=0", mid, id).Scan(&count)
		if err != nil {
			fail(w, err)
			return
		}
		if count == 0 {
			core.Fail(w, 404, "模特不属于这个店铺")
			return
		}
	}
	for slot, key := range in.TemplateSet {
		t, err := a.template(r.Context(), key)
		if err != nil {
			fail(w, err)
			return
		}
		if t.Slot != slot || !t.Enabled {
			core.Fail(w, 400, "模板位置不匹配或已停用")
			return
		}
	}
	_, err := a.DB.ExecContext(r.Context(), `INSERT INTO creative_shop_settings(shop_id,models,template_set,updated_by,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(shop_id) DO UPDATE SET models=excluded.models,template_set=excluded.template_set,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, id, core.JS(in.Models), core.JS(in.TemplateSet), core.CurrentUser(r).ID, core.Now())
	if err != nil {
		fail(w, err)
		return
	}
	a.Audit(r, "creative.shop_settings", "shop", id, in)
	a.shopGet(w, r)
}
