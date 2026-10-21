package orders

import (
	"testing"
	"xstudio/internal/core"
)

func TestReturnReasonAndCreativeSet(t *testing.T) {
	f := boot(t)
	item := f.item(t, "REASON")
	f.in(t, item, 5, 100, "")
	id := f.order(t, item, 2)
	lid := f.line(t, id)
	f.req(t, "POST", path(id)+"/ship", obj{"lines": []obj{{"line_id": lid, "warehouse_id": 1}}}, 200)

	f.req(t, "POST", path(id)+"/returns", obj{"reason": "尺寸", "lines": []obj{{"line_id": lid, "qty": 1, "action": "none", "reason_code": "smaller"}}}, 200)
	got := returnLine(t, f.req(t, "GET", path(id), nil, 200), 0)
	if got["reason_code"] != "smaller" {
		t.Fatal(got)
	}
	if v, ok := got["creative_set_id"]; !ok || v != nil {
		t.Fatal(got)
	}

	before := f.n(t, "SELECT count(*) FROM order_returns WHERE order_id=?", id)
	f.req(t, "POST", path(id)+"/returns", obj{"lines": []obj{{"line_id": lid, "qty": 1, "action": "none", "reason_code": "too_big"}}}, 400)
	if f.n(t, "SELECT count(*) FROM order_returns WHERE order_id=?", id) != before {
		t.Fatal("invalid reason wrote a return")
	}

	f.req(t, "POST", path(id)+"/returns", obj{"lines": []obj{{"line_id": lid, "qty": 1, "action": "scrap"}}}, 200)
	old := returnLine(t, f.req(t, "GET", path(id), nil, 200), 1)
	if old["reason_code"] != "" {
		t.Fatal(old)
	}
	if v, ok := old["creative_set_id"]; !ok || v != nil {
		t.Fatal(old)
	}

	uid := f.n(t, "SELECT id FROM users WHERE username='admin'")
	now := core.Now()
	pid := f.exec(t, "INSERT INTO products(name,created_by,created_at,updated_at) VALUES('珍珠',?,?,?)", uid, now, now)
	f.exec(t, "UPDATE inv_items SET product_id=? WHERE id=?", pid, item)
	listing := f.exec(t, "INSERT INTO listings(product_id,shop_id,created_by,created_at,updated_at) VALUES(?,1,?,?,?)", pid, uid, now, now)
	f.exec(t, "CREATE TABLE creative_sets(id INTEGER PRIMARY KEY, listing_id INTEGER, exported_at TEXT)")
	id2 := num(f.req(t, "POST", "/api/orders", obj{"shop_id": 1, "paid_at": "2026-10-20T08:00:00Z", "lines": []obj{{"item_id": item, "qty": 1, "price": 100}}}, 200)["id"])
	paidAt := f.req(t, "GET", path(id2), nil, 200)["paid_at"].(string)
	f.exec(t, "INSERT INTO creative_sets(id,listing_id,exported_at) VALUES(11,?,?)", listing, "2026-10-19T08:00:00.000000000Z")
	f.exec(t, "INSERT INTO creative_sets(id,listing_id,exported_at) VALUES(22,?,?)", listing, paidAt)
	f.exec(t, "INSERT INTO creative_sets(id,listing_id,exported_at) VALUES(33,?,?)", listing, "2026-10-21T08:00:00.000000000Z")
	lid2 := f.line(t, id2)
	f.req(t, "POST", path(id2)+"/ship", obj{"lines": []obj{{"line_id": lid2, "warehouse_id": 1}}}, 200)
	f.req(t, "POST", path(id2)+"/returns", obj{"lines": []obj{{"line_id": lid2, "qty": 1, "action": "none", "reason_code": "color"}}}, 200)
	linked := returnLine(t, f.req(t, "GET", path(id2), nil, 200), 0)
	if linked["reason_code"] != "color" || num(linked["creative_set_id"]) != 22 {
		t.Fatal(linked, paidAt)
	}
}

func returnLine(t *testing.T, detail obj, index int) obj {
	t.Helper()
	returns, ok := detail["returns"].([]any)
	if !ok || index >= len(returns) {
		t.Fatal(detail)
	}
	lines, ok := returns[index].(obj)["lines"].([]any)
	if !ok || len(lines) == 0 {
		t.Fatal(detail)
	}
	line, ok := lines[0].(obj)
	if !ok {
		t.Fatal(detail)
	}
	return line
}
