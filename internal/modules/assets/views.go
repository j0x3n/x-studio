package assets

import (
	"database/sql"
	"fmt"
	"net/http"
	"slices"
	"xstudio/internal/core"
	"xstudio/internal/media"
)

func (a *Module) viewRoutes(r *core.Router) {
	r.Handle("GET /api/products/{id}/views", core.ProductView, a.productViews)
	r.Handle("POST /api/products/{id}/views/classify", core.AssetUpload, a.viewsClassify)
	r.Handle("POST /api/products/{id}/views/enhance", core.AssetUpscale, a.viewsEnhance)
	r.Handle("POST /api/assets/{pid}/make-current", core.AssetUpload, a.makeCurrent)
	r.Handle("POST /api/assets/{pid}/discard", core.AssetUpload, a.discard)
}

func viewCodeAllowed(p core.Product, code string) bool {
	for _, view := range media.ProductViews(p) {
		if view.Code == code {
			return true
		}
	}
	return false
}

func setCurrentTx(tx *sql.Tx, p Asset, code string) error {
	if code == "" || p.Media != "image" || p.Kind == "output" || p.Archived {
		return fmt.Errorf("这张照片不能设为槽位当前图")
	}
	if _, err := tx.Exec("UPDATE assets SET code='',updated_at=? WHERE product_id=? AND code=? AND id<>?", now(), p.ProductID, code, p.ID); err != nil {
		return err
	}
	_, err := tx.Exec("UPDATE assets SET code=?,view_code=?,suggest='{}',manual=1,updated_at=? WHERE id=?", code, code, now(), p.ID)
	return err
}

func (a *Module) makeCurrent(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	p, ok := a.photoForWrite(w, r)
	if !ok {
		return
	}
	if !a.CanUpload(currentUser(r), p.ProductID) {
		fail(w, 403, "此商品不允许修改素材")
		return
	}
	if p.ViewCode == "" {
		fail(w, 400, "照片没有可用的底图槽位归属")
		return
	}
	if sharedPipelineBusy(a.DB, p.ProductID) {
		fail(w, 409, "商品正在处理，请等待任务完成")
		return
	}
	photo := p.Photo
	photo.Code = p.ViewCode
	if err := validatePhoto(photo); err != nil {
		fail(w, 400, err.Error())
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		fail(w, 500, "保存当前图失败")
		return
	}
	defer tx.Rollback()
	if err = setCurrentTx(tx, p, p.ViewCode); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if core.AuditTx(tx, r, "assets.make_current", "asset", p.ID, Object{"code": p.ViewCode, "product_id": p.ProductID}) != nil || tx.Commit() != nil {
		fail(w, 500, "保存当前图失败")
		return
	}
	a.removePhotoOutputs(p.ProductID, p.ViewCode)
	p, _ = a.asset(p.ID)
	writeJSON(w, 200, Object{"asset": p})
}

func (a *Module) discard(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	p, ok := a.photoForWrite(w, r)
	if !ok {
		return
	}
	if !a.CanUpload(currentUser(r), p.ProductID) {
		fail(w, 403, "此商品不允许修改素材")
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		fail(w, 500, "丢弃照片失败")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE assets SET archived=1,code='',updated_at=? WHERE id=?", now(), p.ID); err != nil {
		fail(w, 500, "丢弃照片失败")
		return
	}
	if core.AuditTx(tx, r, "assets.discard", "asset", p.ID, Object{"product_id": p.ProductID}) != nil || tx.Commit() != nil {
		fail(w, 500, "丢弃照片失败")
		return
	}
	a.removePhotoOutputs(p.ProductID, p.Code)
	p, _ = a.asset(p.ID)
	writeJSON(w, 200, Object{"asset": p})
}

func (a *Module) productViews(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	if !a.RequireProduct(w, r, id, false) {
		return
	}
	p, err := a.Product(id)
	if err != nil {
		fail(w, 500, "读取商品失败")
		return
	}
	items, err := a.readAssets("SELECT "+assetColumns+" FROM assets WHERE product_id=? AND media='image' AND kind<>'output' AND archived=0 ORDER BY created_at DESC,id DESC", id)
	if err != nil {
		fail(w, 500, "读取底图失败")
		return
	}
	style, _ := p.Info["style"].(string)
	group := media.ViewGroup(style)
	views := []Object{}
	missing := []string{}
	known := map[string]bool{}
	for _, view := range media.ProductViews(p) {
		known[view.Code] = true
		entry := Object{"key": view.Key, "name": view.Name, "code": view.Code, "required": slices.Contains(view.Required, group), "hint": view.Hint, "current": nil}
		versions, pending := []Asset{}, []Asset{}
		for _, asset := range items {
			if asset.ViewCode != view.Code {
				continue
			}
			asset = a.decorate(asset, currentUser(r), items)
			versions = append(versions, asset)
			if asset.Code == view.Code {
				entry["current"] = asset
			}
			if asset.Code == "" && asset.EnhanceCheck["pending"] == true {
				pending = append(pending, asset)
			}
		}
		entry["versions"], entry["pending"] = versions, pending
		if entry["required"] == true && entry["current"] == nil {
			missing = append(missing, view.Key)
		}
		views = append(views, entry)
	}
	unassigned, others := []Asset{}, []Asset{}
	for _, asset := range items {
		asset = a.decorate(asset, currentUser(r), items)
		if asset.ViewCode == "" {
			unassigned = append(unassigned, asset)
		} else if !known[asset.ViewCode] {
			others = append(others, asset)
		}
	}
	writeJSON(w, 200, Object{"group": group, "views": views, "unassigned": unassigned, "others": others, "missing": missing, "ready": len(missing) == 0})
}
