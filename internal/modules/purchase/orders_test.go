package purchase

import (
	"fmt"
	"math"
	"testing"
	"xstudio/internal/core"
)

func (f *fixture) item(t *testing.T, code string) int64 {
	return num(f.req(t, "POST", "/api/inventory/items", object{"code": code, "name": code, "kind": "sku", "unit": "个"}, 200)["id"])
}
func poPath(id int64) string { return fmt.Sprintf("/api/purchase-orders/%d", id) }
func (f *fixture) po(t *testing.T, shipping int64, lines []object) int64 {
	sid := num(f.req(t, "POST", "/api/suppliers", object{"name": fmt.Sprintf("供应商-%d", f.count(t, "SELECT count(*) FROM suppliers"))}, 201)["supplier"].(object)["id"])
	return num(f.req(t, "POST", "/api/purchase-orders", object{"supplier_id": sid, "shipping_fee": shipping, "lines": lines}, 200)["id"])
}
func TestPurchaseReceiptCostsRemaindersGradesAndReturns(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	a, b := f.item(t, "A"), f.item(t, "B")
	id := f.po(t, 1, []object{{"item_id": a, "qty": 3, "amount": 100}, {"item_id": b, "qty": 3, "amount": 100}})
	d := f.req(t, "GET", poPath(id), nil, 200)
	lines := d["lines"].([]any)
	la, lb := num(lines[0].(object)["id"]), num(lines[1].(object)["id"])
	if num(lines[0].(object)["shipping_share"])+num(lines[1].(object)["shipping_share"]) != 1 {
		t.Fatal(d)
	}
	f.req(t, "POST", poPath(id)+"/receipts", object{"warehouse_id": 1, "lines": []object{{"line_id": la, "qty": 1}}}, 409)
	f.req(t, "POST", poPath(id)+"/order", nil, 200)
	f.req(t, "PATCH", poPath(id), object{"shipping_fee": 3}, 409)
	f.req(t, "PATCH", poPath(id), object{"note": "分批到货", "expected_at": "2026-10-20"}, 200)
	// A's final accepted/rejected cost consumes the remainder of its 101 cents.
	f.req(t, "POST", poPath(id)+"/receipts", object{"warehouse_id": 1, "lines": []object{{"line_id": la, "qty": 1, "grades": []object{{"grade": "AAA", "qty": 1}}}}}, 200)
	result := f.req(t, "POST", poPath(id)+"/receipts", object{"warehouse_id": 1, "lines": []object{{"line_id": la, "qty": 2, "rejected": 1, "grades": []object{{"grade": "AA", "qty": 1}}}, {"line_id": lb, "qty": 4, "grades": []object{{"grade": "AA", "qty": 1}, {"grade": "A", "qty": 3}}}}}, 200)
	if result["status"] != "received" {
		t.Fatal(result)
	}
	if f.count(t, "SELECT sum(cost_in) FROM inv_batches")+f.count(t, "SELECT sum(amount) FROM purchase_returns") != 201 {
		t.Fatal("receipt valuation lost cents")
	}
	d = f.req(t, "GET", poPath(id), nil, 200)
	ret := d["returns"].([]any)[0].(object)
	if num(ret["amount"]) != 34 || num(ret["qty"]) != 1 {
		t.Fatal(ret)
	}
	f.req(t, "PATCH", fmt.Sprintf("/api/purchase-returns/%d", num(ret["id"])), object{"status": "done"}, 200)
	f.req(t, "PATCH", fmt.Sprintf("/api/purchase-returns/%d", num(ret["id"])), object{"status": "done"}, 409)
	f.req(t, "POST", poPath(id)+"/finish", nil, 200)
	f.req(t, "POST", poPath(id)+"/finish", nil, 409)
	if f.count(t, "SELECT sum(qty_left) FROM inv_batches") != 6 {
		t.Fatal(d)
	}
	audit, err := one(f.c.DB, "SELECT detail FROM audit_log WHERE action='purchase.receive' ORDER BY id DESC LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	payload := core.JSONObj(audit["detail"].(string))
	if num(payload["po_id"]) != id || payload["no"] != d["no"] || payload["supplier"] == "" || len(payload["lines"].([]any)) != 2 {
		t.Fatal(payload)
	}
}
func TestPurchaseReceiptAtomicValidationAndStateMachine(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	item := f.item(t, "ATOMIC")
	id := f.po(t, 0, []object{{"item_id": item, "qty": 3, "amount": 100}})
	path := poPath(id)
	f.req(t, "PATCH", path, object{"lines": []object{{"item_id": item, "qty": 3, "amount": 100}}}, 200)
	d := f.req(t, "GET", path, nil, 200)
	lid := num(d["lines"].([]any)[0].(object)["id"])
	f.req(t, "POST", path+"/order", nil, 200)
	f.req(t, "POST", path+"/order", nil, 409)
	for _, lines := range [][]object{
		{{"line_id": lid, "qty": 1}, {"line_id": 99999, "qty": 1}},
		{{"line_id": lid, "qty": 1}, {"line_id": lid, "qty": 1}},
		{{"line_id": lid, "qty": 2, "rejected": 3}},
		{{"line_id": lid, "qty": 2, "grades": []object{{"grade": "AA", "qty": 1}}}},
	} {
		status := 400
		if num(lines[len(lines)-1]["line_id"]) == 99999 {
			status = 404
		}
		f.req(t, "POST", path+"/receipts", object{"warehouse_id": 1, "lines": lines}, status)
		if f.count(t, "SELECT count(*) FROM purchase_receipts") != 0 || f.count(t, "SELECT count(*) FROM inv_batches") != 0 || f.count(t, "SELECT sum(received_qty) FROM purchase_lines") != 0 {
			t.Fatal("partial receipt committed")
		}
	}
	f.req(t, "POST", path+"/receipts", object{"warehouse_id": 1, "lines": []object{{"line_id": lid, "qty": 1, "rejected": 1}}}, 200)
	f.req(t, "POST", path+"/cancel", nil, 409)
	f.req(t, "POST", path+"/finish", nil, 200)
	f.req(t, "POST", path+"/receipts", object{"warehouse_id": 1, "lines": []object{{"line_id": lid, "qty": 1}}}, 409)
	for _, ordered := range []bool{false, true} {
		id = f.po(t, 0, []object{{"item_id": item, "qty": 1, "amount": 0}})
		if ordered {
			f.req(t, "POST", poPath(id)+"/order", nil, 200)
		}
		f.req(t, "POST", poPath(id)+"/cancel", nil, 200)
		f.req(t, "POST", poPath(id)+"/order", nil, 409)
	}
}
func TestPurchasePermissionsFiltersAndOverflow(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	item := f.item(t, "FILTER")
	id := f.po(t, 0, []object{{"item_id": item, "qty": 1, "amount": 1}})
	f.req(t, "PATCH", poPath(id), object{"is_sample": true}, 200)
	d := f.req(t, "GET", poPath(id), nil, 200)
	list := f.req(t, "GET", fmt.Sprintf("/api/purchase-orders?status=draft&supplier_id=%d&item_id=%d&is_sample=1&q=%s&page_size=1", num(d["supplier_id"]), item, d["no"]), nil, 200)
	if num(list["total"]) != 1 {
		t.Fatal(list)
	}
	f.req(t, "POST", "/api/purchase-orders", object{"supplier_id": d["supplier_id"], "shipping_fee": 1, "lines": []object{{"item_id": item, "qty": 1, "amount": int64(math.MaxInt64)}}}, 400)
	for _, role := range []string{"designer", "finance", "collaborator"} {
		f.user(t, role, role, role)
		f.login(t, role)
		f.req(t, "GET", "/api/purchase-orders", nil, 403)
		f.req(t, "GET", poPath(id), nil, 403)
		f.req(t, "POST", poPath(id)+"/order", nil, 403)
	}
	f.login(t, "admin")
	f.exec(t, "UPDATE roles SET perms=? WHERE key='collaborator'", core.JS([]string{core.PurchaseManage}))
	f.login(t, "collaborator")
	f.req(t, "GET", poPath(id), nil, 403)
}
