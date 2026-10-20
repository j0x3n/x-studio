package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
	"xstudio/internal/modules/kb"
)

func (a *Module) searchKB(u core.User, raw json.RawMessage) (any, error) {
	if u.Role.Scope == "shares" || !u.Has(core.KBView) {
		return nil, fmt.Errorf("没有知识库权限")
	}
	var in struct {
		Q     string `json:"q"`
		Limit *int   `json:"limit"`
	}
	if err := parseTaskArgs(raw, &in, "q", "limit"); err != nil {
		return nil, err
	}
	in.Q = strings.TrimSpace(in.Q)
	if n := utf8.RuneCountInString(in.Q); n < 1 || n > 200 {
		return nil, fmt.Errorf("查询需要 1–200 字")
	}
	limit, err := bounded(in.Limit, 5, 20)
	if err != nil {
		return nil, err
	}
	return kb.Search(a.DB, in.Q, limit)
}

func (a *Module) commerceRef(u core.User, typ string, id int64) (path, label string, ok bool) {
	if u.Role.Scope == "shares" {
		return
	}
	switch typ {
	case "inv_item":
		if !u.Has(core.InventoryView) {
			return
		}
		ok = a.DB.QueryRow("SELECT code || ' ' || name FROM inv_items WHERE id=?", id).Scan(&label) == nil
		path = fmt.Sprintf("/inventory/items/%d", id)
	case "purchase_order":
		if !u.Has(core.PurchaseManage) {
			return
		}
		ok = a.DB.QueryRow("SELECT no FROM purchase_orders WHERE id=?", id).Scan(&label) == nil
		path = fmt.Sprintf("/purchase-orders/%d", id)
	}
	return
}

// Keep the legacy task scope independent of optional modules. The additional
// guard is deliberately outside the participant OR, just as in tasks.
func (a *Module) taskCountScope(u core.User) (string, []any) {
	old, args := taskCountScope(u)
	clauses := []string{}
	for _, v := range []struct{ typ, table, perm string }{{"inv_item", "inv_items", core.InventoryView}, {"purchase_order", "purchase_orders", core.PurchaseManage}} {
		if u.Role.Scope == "shares" || !u.Has(v.perm) {
			continue
		}
		var n int
		if a.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", v.table).Scan(&n) != nil || n == 0 {
			continue
		}
		clauses = append(clauses, "(t.target_type='"+v.typ+"' AND EXISTS(SELECT 1 FROM "+v.table+" c WHERE c.id=t.target_id AND c.created_at=t.target_created_at))")
	}
	extra := "0=1"
	if len(clauses) > 0 {
		part := "1=1"
		if !u.Has(core.TaskManage) {
			id := fmt.Sprint(u.ID)
			part = "(t.assignee_id=" + id + " OR t.created_by=" + id + " OR EXISTS(SELECT 1 FROM task_watchers w WHERE w.task_id=t.id AND w.user_id=" + id + "))"
		}
		extra = "t.target_deleted=0 AND (" + strings.Join(clauses, " OR ") + ") AND (" + part + ")"
	}
	return "((coalesce(t.target_type,'') NOT IN ('inv_item','purchase_order') AND (" + old + ")) OR (" + extra + "))", args
}
