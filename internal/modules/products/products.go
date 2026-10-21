package products

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
	"xstudio/internal/media"
)

type Module struct{ *core.Core }

func New(c *core.Core) *Module                   { return &Module{c} }
func (*Module) Name() string                     { return "products" }
func (*Module) Jobs() map[string]core.JobHandler { return nil }
func (a *Module) Routes(r *core.Router) {
	r.Handle("GET /api/categories", core.LoggedIn, a.listCategories)
	r.Handle("POST /api/categories", core.AdminSettings, a.createCategory)
	r.Handle("PATCH /api/categories/{id}", core.AdminSettings, a.patchCategory)
	r.Handle("GET /api/products", core.ProductView, a.list)
	r.Handle("POST /api/products", core.ProductEdit, a.create)
	r.Handle("GET /api/products/{id}", core.ProductView, a.get)
	r.Handle("PATCH /api/products/{id}", core.ProductEdit, a.patch)
	r.Handle("DELETE /api/products/{id}", core.Scoped, a.delete)
	r.Handle("POST /api/products/{id}/listings", core.ProductEdit, a.addListing)
	r.Handle("GET /api/listings/{id}", core.ProductView, a.getListing)
	r.Handle("PATCH /api/listings/{id}", core.ProductEdit, a.patchListing)
	r.Handle("DELETE /api/listings/{id}", core.ProductEdit, a.deleteListing)
}
func (a *Module) productJSON(id int64, u core.User) (core.Object, error) {
	rows, e := core.QueryObjects(a.DB, "SELECT * FROM products WHERE id=?", id)
	if e != nil {
		return nil, e
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	o := rows[0]
	c, e := a.ProductCategory(id)
	if e != nil {
		return nil, e
	}
	o["category_id"] = c.ID
	o["category"] = core.Object{"id": c.ID, "key": c.Key, "name": c.Name}
	var owner string
	_ = a.DB.QueryRow("SELECT coalesce(nullif(display_name,''),username) FROM users WHERE id=?", o["created_by"]).Scan(&owner)
	o["created_by_name"] = owner
	if !u.Has(core.ProductCost) {
		delete(o, "cost")
		delete(o, "supplier")
	}
	listings, e := a.listings(id, u)
	if e != nil {
		return nil, e
	}
	o["listings"] = listings
	var count int
	var cover string
	if e = a.DB.QueryRow("SELECT count(*),coalesce(min(CASE WHEN media='image' THEN file END),'') FROM assets WHERE product_id=? AND kind='photo'", id).Scan(&count, &cover); e != nil {
		return nil, e
	}
	o["asset_count"] = count
	o["cover_url"] = ""
	if cover != "" {
		o["cover_url"] = fmt.Sprintf("/files/products/%d/%s?w=240", id, cover)
	}
	return o, nil
}
func (a *Module) listings(id int64, u core.User) ([]core.Object, error) {
	q := "SELECT * FROM listings WHERE product_id=?"
	args := []any{id}
	if u.Role.Key != "admin" {
		q += " AND shop_id IN (SELECT shop_id FROM user_shops WHERE user_id=?)"
		args = append(args, u.ID)
		if u.Role.Scope == "shares" {
			q += " AND 0"
		}
	}
	items, e := core.QueryObjects(a.DB, q+" ORDER BY id", args...)
	for _, v := range items {
		if !u.Has(core.CopyView) {
			delete(v, "content")
			delete(v, "main")
			delete(v, "meta")
		}
	}
	return items, e
}
func (a *Module) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := "SELECT id FROM products WHERE 1=1"
	args := []any{}
	for _, key := range []string{"status"} {
		if s := q.Get(key); s != "" {
			query += " AND " + key + "=?"
			args = append(args, s)
		}
	}
	if s := q.Get("category"); s != "" {
		query += " AND category_id=?"
		args = append(args, s)
	}
	if s := q.Get("q"); s != "" {
		query += " AND instr(name,?)>0"
		args = append(args, s)
	}
	if q.Get("mine") == "1" {
		query += " AND created_by=?"
		args = append(args, core.CurrentUser(r).ID)
	}
	if s := q.Get("shop_id"); s != "" {
		if core.CurrentUser(r).Role.Scope == "shares" {
			core.PageItems(w, r, []core.Object{})
			return
		}
		query += " AND EXISTS(SELECT 1 FROM listings l WHERE l.product_id=products.id AND l.shop_id=?)"
		args = append(args, s)
	}
	ids, e := core.QueryObjects(a.DB, query+" ORDER BY updated_at DESC,id DESC", args...)
	if e != nil {
		core.Fail(w, 500, "读取商品失败")
		return
	}
	items := []core.Object{}
	u := core.CurrentUser(r)
	for _, v := range ids {
		id := v["id"].(int64)
		if !a.CanProduct(u, id) {
			continue
		}
		if shop := q.Get("shop_id"); shop != "" {
			var sid int64
			_ = a.DB.QueryRow("SELECT id FROM shops WHERE id=?", shop).Scan(&sid)
			if !a.CanShop(u, sid) {
				continue
			}
		}
		o, e := a.productJSON(id, u)
		if e != nil {
			core.Fail(w, 500, "读取商品失败")
			return
		}
		items = append(items, o)
	}
	core.PageItems(w, r, items)
}
func (a *Module) get(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !a.RequireProduct(w, r, id, false) {
		return
	}
	o, e := a.productJSON(id, core.CurrentUser(r))
	if e != nil {
		core.Fail(w, 500, "读取商品失败")
		return
	}
	photos, e := a.AllPhotos(id)
	if e != nil {
		core.Fail(w, 500, "读取素材失败")
		return
	}
	response := core.Object{"product": o, "assets": photos}
	if a.ProductCoverage != nil {
		current, e := a.Photos(id)
		if e != nil {
			core.Fail(w, 500, "读取素材覆盖情况失败")
			return
		}
		cov, e := a.ProductCoverage(id, current)
		if e != nil {
			core.Fail(w, 500, "读取品类覆盖情况失败")
			return
		}
		response["coverage"] = cov
	}
	core.WriteJSON(w, 200, response)
}
func (a *Module) create(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	var raw map[string]json.RawMessage
	if !core.Decode(w, r, &raw) {
		return
	}
	u := core.CurrentUser(r)
	if !u.Has(core.ProductCost) && (raw["cost"] != nil || raw["supplier"] != nil) {
		core.Fail(w, 403, "没有查看或修改成本和货源的权限")
		return
	}
	var in struct {
		CategoryID *int64      `json:"category_id"`
		Name       string      `json:"name"`
		Info       core.Object `json:"info"`
		Cost       core.Object `json:"cost"`
		Supplier   core.Object `json:"supplier"`
		ShopIDs    []int64     `json:"shop_ids"`
	}
	b, _ := json.Marshal(raw)
	if json.Unmarshal(b, &in) != nil || strings.TrimSpace(in.Name) == "" || utf8.RuneCountInString(in.Name) > 120 {
		core.Fail(w, 400, "商品名称或字段类型不正确")
		return
	}
	if in.Info == nil {
		in.Info = core.Object{}
	}
	if in.Cost == nil {
		in.Cost = core.Object{}
	}
	if in.Supplier == nil {
		in.Supplier = core.Object{}
	}
	if e := media.ValidateInfo(in.Info); e != nil {
		core.Fail(w, 400, e.Error())
		return
	}
	if raw["category_id"] != nil && in.CategoryID == nil {
		core.Fail(w, 400, "category_id 格式不正确")
		return
	}
	c, e := a.productCategoryInput(in.CategoryID, true)
	if e != nil {
		core.Fail(w, 400, e.Error())
		return
	}
	if e = validateCategoryInfo(in.Info, c); e != nil {
		core.Fail(w, 400, e.Error())
		return
	}
	seen := map[int64]bool{}
	for _, id := range in.ShopIDs {
		if seen[id] {
			core.Fail(w, 400, "店铺不能重复")
			return
		}
		seen[id] = true
		if !a.CanShop(u, id) {
			core.Fail(w, 404, "店铺不存在")
			return
		}
	}
	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "创建商品失败")
		return
	}
	defer tx.Rollback()
	t := core.Now()
	res, e := tx.Exec("INSERT INTO products(name,created_by,created_at,updated_at,info,cost,supplier,category_id) VALUES(?,?,?,?,?,?,?,?)", strings.TrimSpace(in.Name), u.ID, t, t, core.JS(in.Info), core.JS(in.Cost), core.JS(in.Supplier), c.ID)
	if e != nil {
		core.Fail(w, 500, "创建商品失败")
		return
	}
	id, _ := res.LastInsertId()
	for _, sid := range in.ShopIDs {
		if _, e = a.insertListing(tx, r, id, sid); e != nil {
			core.Fail(w, 400, e.Error())
			return
		}
	}
	if core.AuditTx(tx, r, "product.create", "product", id, core.Object{"name": in.Name}) != nil || tx.Commit() != nil {
		core.Fail(w, 500, "创建商品失败")
		return
	}
	o, _ := a.productJSON(id, u)
	core.WriteJSON(w, 201, core.Object{"product": o})
}
func (a *Module) insertListing(tx *sql.Tx, r *http.Request, pid, sid int64) (int64, error) {
	var archived bool
	if tx.QueryRow("SELECT archived FROM shops WHERE id=?", sid).Scan(&archived) != nil || archived {
		return 0, fmt.Errorf("店铺不存在或已归档")
	}
	t := core.Now()
	res, e := tx.Exec("INSERT INTO listings(product_id,shop_id,created_by,created_at,updated_at) VALUES(?,?,?,?,?)", pid, sid, core.CurrentUser(r).ID, t, t)
	if e != nil {
		return 0, fmt.Errorf("此商品已有该店铺的上架版本")
	}
	id, _ := res.LastInsertId()
	return id, core.AuditTx(tx, r, "listing.create", "listing", id, core.Object{"product_id": pid, "shop_id": sid})
}
func (a *Module) patch(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	if !a.RequireProduct(w, r, id, true) {
		return
	}
	u := core.CurrentUser(r)
	var in map[string]json.RawMessage
	if !core.Decode(w, r, &in) {
		return
	}
	if !u.Has(core.ProductCost) && (in["cost"] != nil || in["supplier"] != nil) {
		core.Fail(w, 403, "没有修改成本和货源的权限")
		return
	}
	if len(in) == 0 {
		core.Fail(w, 400, "没有要更新的字段")
		return
	}
	c, e := a.ProductCategory(id)
	if e != nil {
		core.Fail(w, 500, "读取品类失败")
		return
	}
	if raw, ok := in["category_id"]; ok {
		var cid int64
		if string(raw) == "null" || json.Unmarshal(raw, &cid) != nil || cid < 1 {
			core.Fail(w, 400, "category_id 格式不正确")
			return
		}
		c, e = a.productCategoryInput(&cid, cid != c.ID)
		if e != nil {
			core.Fail(w, 400, e.Error())
			return
		}
	}
	sets := []string{}
	args := []any{}
	for k, v := range in {
		var value any
		switch k {
		case "category_id":
			value = c.ID
		case "name", "status":
			var s string
			if json.Unmarshal(v, &s) != nil {
				core.Fail(w, 400, "商品字段类型不正确")
				return
			}
			if k == "name" && (strings.TrimSpace(s) == "" || len([]rune(s)) > 120) {
				core.Fail(w, 400, "商品名称不正确")
				return
			}
			if k == "status" && s != "sampling" && s != "pending" && s != "listed" && s != "archived" {
				core.Fail(w, 400, "商品状态不正确")
				return
			}
			value = s
		case "info", "cost", "supplier":
			var o core.Object
			if json.Unmarshal(v, &o) != nil || o == nil {
				core.Fail(w, 400, "商品字段必须是 JSON 对象")
				return
			}
			if k == "info" {
				if e := validateCategoryInfo(o, c); e != nil {
					core.Fail(w, 400, e.Error())
					return
				}
				if e := media.ValidateInfo(o); e != nil {
					core.Fail(w, 400, e.Error())
					return
				}
			}
			value = core.JS(o)
		default:
			core.Fail(w, 400, "不支持的商品字段："+k)
			return
		}
		sets = append(sets, k+"=?")
		args = append(args, value)
	}
	args = append(args, core.Now(), id)
	if _, e := a.DB.Exec("UPDATE products SET "+strings.Join(sets, ",")+",updated_at=? WHERE id=?", args...); e != nil {
		core.Fail(w, 500, "保存商品失败")
		return
	}
	a.Audit(r, "product.update", "product", id, core.Object{"fields": mapKeys(in)})
	o, _ := a.productJSON(id, u)
	core.WriteJSON(w, 200, core.Object{"product": o})
}
func mapKeys(in map[string]json.RawMessage) []string {
	ks := []string{}
	for k := range in {
		ks = append(ks, k)
	}
	return ks
}
func (a *Module) delete(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	if !a.RequireProduct(w, r, id, true) {
		return
	}
	p, e := a.Product(id)
	if e != nil {
		core.Fail(w, 404, "商品不存在")
		return
	}
	u := core.CurrentUser(r)
	if !u.Has(core.ProductDeleteAny) && !(u.Has(core.ProductDeleteOwn) && p.OwnerID == u.ID) {
		core.Fail(w, 403, "只能删除自己创建的商品")
		return
	}
	ls, e := core.QueryObjects(a.DB, "SELECT id FROM listings WHERE product_id=?", id)
	if e != nil {
		core.Fail(w, 500, "删除商品失败")
		return
	}
	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "删除商品失败")
		return
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM jobs WHERE target_type='listing' AND target_id IN (SELECT id FROM listings WHERE product_id=?)", id); e == nil {
		_, e = tx.Exec("DELETE FROM products WHERE id=?", id)
	}
	if e == nil {
		e = core.AuditTx(tx, r, "product.delete", "product", id, core.Object{"name": p.Name})
	}
	if e != nil || tx.Commit() != nil {
		core.Fail(w, 500, "删除商品失败")
		return
	}
	if e = os.RemoveAll(a.Dir(id)); e != nil {
		core.Fail(w, 500, "商品已删除，但文件清理失败")
		return
	}
	for _, l := range ls {
		if e = os.RemoveAll(a.ListingDir(l["id"].(int64))); e != nil {
			core.Fail(w, 500, "商品已删除，但导出清理失败")
			return
		}
	}
	core.WriteJSON(w, 200, core.Object{})
}
func (a *Module) productCategoryInput(id *int64, rejectDisabled bool) (*core.Category, error) {
	var c *core.Category
	var e error
	if id == nil {
		list, err := a.Categories(false)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, fmt.Errorf("没有启用的品类")
		}
		c = &list[0]
	} else {
		if *id < 1 {
			return nil, fmt.Errorf("品类不存在")
		}
		c, e = a.Category(*id)
		if e != nil {
			return nil, fmt.Errorf("品类不存在")
		}
	}
	if rejectDisabled && c.Disabled {
		return nil, fmt.Errorf("这个品类已停用")
	}
	return c, nil
}
