package products

import (
	"encoding/json"
	"net/http"
	"os"
	"xstudio/internal/core"
)

func (a *Module) addListing(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	if !a.RequireProduct(w, r, id, true) {
		return
	}
	var in struct {
		ShopID int64 `json:"shop_id"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if !a.CanShop(core.CurrentUser(r), in.ShopID) {
		core.Fail(w, 404, "店铺不存在")
		return
	}
	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "创建上架版本失败")
		return
	}
	defer tx.Rollback()
	lid, e := a.insertListing(tx, r, id, in.ShopID)
	if e != nil {
		core.Fail(w, 409, e.Error())
		return
	}
	if tx.Commit() != nil {
		core.Fail(w, 500, "创建上架版本失败")
		return
	}
	items, _ := core.QueryObjects(a.DB, "SELECT * FROM listings WHERE id=?", lid)
	core.WriteJSON(w, 201, core.Object{"listing": items[0]})
}
func (a *Module) getListing(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, false) {
		return
	}
	items, e := core.QueryObjects(a.DB, "SELECT * FROM listings WHERE id=?", id)
	if e != nil {
		core.Fail(w, 500, "读取上架版本失败")
		return
	}
	o := items[0]
	if !core.CurrentUser(r).Has(core.CopyView) {
		delete(o, "content")
		delete(o, "main")
		delete(o, "meta")
	}
	core.WriteJSON(w, 200, core.Object{"listing": o, "job": a.LatestJob(id), "outputs": a.outputs(id)})
}
func (a *Module) patchListing(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, true) {
		return
	}
	var in map[string]json.RawMessage
	if !core.Decode(w, r, &in) {
		return
	}
	p, e := a.PipelineProduct(id)
	if e != nil {
		core.Fail(w, 404, "上架版本不存在")
		return
	}
	var status, url string
	if e = a.DB.QueryRow("SELECT status,url FROM listings WHERE id=?", id).Scan(&status, &url); e != nil {
		core.Fail(w, 500, "读取上架版本失败")
		return
	}
	for k, v := range in {
		switch k {
		case "status":
			if json.Unmarshal(v, &status) != nil || (status != "draft" && status != "ready" && status != "listed") {
				core.Fail(w, 400, "上架版本状态不正确")
				return
			}
		case "url":
			if json.Unmarshal(v, &url) != nil || len(url) > 4096 {
				core.Fail(w, 400, "链接不正确")
				return
			}
		case "content", "main", "meta":
			if !core.CurrentUser(r).Has(core.CopyEdit) {
				core.Fail(w, 403, "没有编辑文案的权限")
				return
			}
			var o core.Object
			if json.Unmarshal(v, &o) != nil || o == nil {
				core.Fail(w, 400, "文案字段必须是 JSON 对象")
				return
			}
			switch k {
			case "content":
				p.Content = o
			case "main":
				p.Main = o
			case "meta":
				p.Listing = o
			}
		default:
			core.Fail(w, 400, "不支持的上架版本字段："+k)
			return
		}
	}
	if a.PrepareListing != nil {
		p, e = a.PrepareListing(p)
	}
	if e != nil {
		core.Fail(w, 400, e.Error())
		return
	}
	if _, e = a.DB.Exec("UPDATE listings SET status=?,url=?,content=?,main=?,meta=?,pipeline_error='',updated_at=? WHERE id=?", status, url, core.JS(p.Content), core.JS(p.Main), core.JS(p.Listing), core.Now(), id); e != nil {
		core.Fail(w, 500, "保存上架版本失败")
		return
	}
	a.Audit(r, "listing.update", "listing", id, core.Object{"fields": mapKeys(in)})
	a.getListing(w, r)
}
func (a *Module) deleteListing(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, true) {
		return
	}
	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "删除上架版本失败")
		return
	}
	defer tx.Rollback()
	_, e = tx.Exec("DELETE FROM jobs WHERE target_type='listing' AND target_id=?", id)
	if e == nil {
		_, e = tx.Exec("DELETE FROM listings WHERE id=?", id)
	}
	if e == nil {
		e = core.AuditTx(tx, r, "listing.delete", "listing", id, core.Object{})
	}
	if e != nil || tx.Commit() != nil {
		core.Fail(w, 500, "删除上架版本失败")
		return
	}
	if os.RemoveAll(a.ListingDir(id)) != nil {
		core.Fail(w, 500, "版本已删除，但导出文件清理失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{})
}
