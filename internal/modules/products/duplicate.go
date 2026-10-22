package products

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
)

func (a *Module) validateProductPlan(r *http.Request, in map[string]json.RawMessage) error {
	if raw, ok := in["plan_id"]; ok && string(raw) != "null" {
		var id int64
		if json.Unmarshal(raw, &id) != nil || id < 1 {
			return fmt.Errorf("plan_id 必须是正整数或 null")
		}
		var templates, models, layout string
		var scene *int64
		if a.DB.QueryRowContext(r.Context(), "SELECT template_set,models,scene_id,detail_layout FROM creative_plans WHERE id=? AND archived=0", id).Scan(&templates, &models, &scene, &layout) != nil {
			return fmt.Errorf("方案不存在或已归档")
		}
		fields := map[string]json.RawMessage{"template_set": json.RawMessage(templates), "models": json.RawMessage(models), "detail_layout": json.RawMessage(core.JS(layout))}
		if err := a.ValidatePlanFields(r.Context(), core.CurrentUser(r), fields); err != nil {
			return err
		}
	}
	if raw, ok := in["plan_overrides"]; ok {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return fmt.Errorf("plan_overrides 必须是对象")
		}
		return a.ValidatePlanFields(r.Context(), core.CurrentUser(r), fields)
	}
	return nil
}

func (a *Module) duplicate(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	source := core.ID(r, "id")
	if !a.RequireProduct(w, r, source, false) {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 120 {
		core.Fail(w, 400, "商品名称须为 1–120 字")
		return
	}
	u := core.CurrentUser(r)
	var category int64
	var info, cost, supplier, overrides string
	var plan *int64
	if err := a.DB.QueryRowContext(r.Context(), "SELECT category_id,info,cost,supplier,plan_id,plan_overrides FROM products WHERE id=?", source).Scan(&category, &info, &cost, &supplier, &plan, &overrides); err != nil {
		core.Fail(w, 500, "读取源商品失败")
		return
	}
	if !u.Has(core.ProductCost) {
		cost, supplier = "{}", "{}"
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		core.Fail(w, 500, "复制商品失败")
		return
	}
	defer tx.Rollback()
	t := core.Now()
	res, err := tx.ExecContext(r.Context(), `INSERT INTO products(name,category_id,info,cost,supplier,plan_id,plan_overrides,based_on_id,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, in.Name, category, info, cost, supplier, plan, overrides, source, u.ID, t, t)
	if err != nil {
		core.Fail(w, 500, "复制商品失败")
		return
	}
	id, _ := res.LastInsertId()
	if core.AuditTx(tx, r, "product.duplicate", "product", id, core.Object{"based_on_id": source}) != nil || tx.Commit() != nil {
		core.Fail(w, 500, "复制商品失败")
		return
	}
	p, err := a.productJSON(id, u)
	if err != nil {
		core.Fail(w, 500, "读取新商品失败")
		return
	}
	core.WriteJSON(w, 201, core.Object{"product": p})
}
