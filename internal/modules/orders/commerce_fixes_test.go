package orders

import (
	"fmt"
	"strings"
	"testing"
)

func TestForcedFiniteDropshipShipmentRollsBackWholeOrder(t *testing.T) {
	f := boot(t)
	own, drop := f.item(t, "OWN-FIRST"), f.item(t, "DROP-SECOND")
	f.in(t, own, 1, 50, "")
	wid := num(f.req(t, "POST", "/api/inventory/warehouses", obj{"name": "有限代发", "type": "dropship"}, 200)["id"])
	dp := fmt.Sprintf("/api/inventory/dropship/%d/%d", wid, drop)
	f.req(t, "PUT", dp, obj{"qty": 1, "unit_cost": 20}, 200)
	a := f.order(t, drop, 1)
	b := num(f.req(t, "POST", "/api/orders", obj{"shop_id": 1, "allow_short": true, "lines": []obj{{"item_id": own, "qty": 1, "price": 100}, {"item_id": drop, "qty": 1, "price": 100}}}, 200)["id"])
	lines := f.req(t, "GET", path(b), nil, 200)["lines"].([]any)
	shipment := obj{"lines": []obj{{"line_id": lines[0].(obj)["id"], "warehouse_id": 1}, {"line_id": lines[1].(obj)["id"], "warehouse_id": wid}}}
	f.req(t, "POST", path(b)+"/ship", shipment, 409)
	if f.n(t, "SELECT sum(qty_left) FROM inv_batches") != 1 || f.n(t, "SELECT qty FROM inv_dropship WHERE item_id=?", drop) != 1 || f.n(t, "SELECT sum(qty) FROM inv_reservations") != 2 || f.n(t, "SELECT count(*) FROM order_picks") != 0 || f.n(t, "SELECT count(*) FROM inv_ledger WHERE kind='sale_out'") != 0 {
		t.Fatal("failed forced shipment consumed stock or reservations")
	}
	if detail := f.req(t, "GET", path(b), nil, 200); detail["status"] != "paid" || detail["short"] != true {
		t.Fatal(detail)
	}
	f.req(t, "POST", path(a)+"/ship", obj{"lines": []obj{{"line_id": f.line(t, a), "warehouse_id": wid}}}, 200)
	f.req(t, "PUT", dp, obj{"qty": 1}, 200)
	f.req(t, "POST", path(b)+"/ship", shipment, 200)
	if f.n(t, "SELECT count(*) FROM inv_reservations") != 0 || f.n(t, "SELECT qty FROM inv_dropship WHERE item_id=?", drop) != 0 {
		t.Fatal("normal release or replenished shipment failed")
	}
}

func TestOrderTitlesAcceptInventoryJSONSpecificationValues(t *testing.T) {
	f := boot(t)
	iid := f.item(t, "SCALAR-SPEC")
	f.req(t, "PATCH", fmt.Sprintf("/api/inventory/items/%d", iid), obj{"spec": obj{"尺寸": 8, "可调": false, "倍率": 1.25, "颜色": "白", "备注": nil}}, 200)
	f.in(t, iid, 2, 100, "")
	id := f.order(t, iid, 1)
	check := func() {
		t.Helper()
		title := f.req(t, "GET", path(id), nil, 200)["lines"].([]any)[0].(obj)["title"].(string)
		for _, text := range []string{"尺寸：8", "可调：false", "倍率：1.25", "颜色：白", "备注：null"} {
			if !strings.Contains(title, text) {
				t.Fatalf("missing %q in %q", text, title)
			}
		}
	}
	check()
	f.req(t, "PATCH", path(id), obj{"lines": []obj{{"item_id": iid, "qty": 2, "price": 100}}}, 200)
	check()
}

func TestPatchShortTracksDynamicStockWithoutChangingReservations(t *testing.T) {
	f := boot(t)
	iid := f.item(t, "DYNAMIC-SHORT")
	f.in(t, iid, 1, 10, "")
	id := num(f.req(t, "POST", "/api/orders", obj{"shop_id": 1, "allow_short": true, "lines": []obj{{"item_id": iid, "qty": 2, "price": 100}}}, 200)["id"])
	check := func(body obj, want bool) {
		t.Helper()
		patch := f.req(t, "PATCH", path(id), body, 200)
		detail := f.req(t, "GET", path(id), nil, 200)
		if patch["short"] != want || detail["short"] != want || f.n(t, "SELECT qty FROM inv_reservations WHERE ref_type='order' AND ref_id=?", id) != 1 {
			t.Fatal("PATCH/GET mismatch or lost reservation", patch, detail)
		}
	}
	check(obj{"seller_note": "等补货"}, true)
	f.in(t, iid, 1, 10, "")
	check(obj{"seller_note": "已补货"}, false)
	check(obj{"shipping_fee": 10}, false)
	f.order(t, iid, 1) // Free stock is now reserved by another order.
	check(obj{"buyer_note": "再次缺货"}, true)
}
