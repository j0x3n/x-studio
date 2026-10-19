package orders

import (
	"fmt"
	"strings"
	"testing"
	"xstudio/internal/core"
)

func TestInventorySourceLinksRequireDocumentPermissionsAndShopAccess(t *testing.T) {
	f := boot(t)
	iid := f.item(t, "SOURCE-LINKS")
	sid := num(f.req(t, "POST", "/api/suppliers", obj{"name": "来源供应商"}, 201)["supplier"].(obj)["id"])
	po := num(f.req(t, "POST", "/api/purchase-orders", obj{"supplier_id": sid, "lines": []obj{{"item_id": iid, "qty": 3, "amount": 100}}}, 200)["id"])
	poPath := fmt.Sprintf("/api/purchase-orders/%d", po)
	poDetail := f.req(t, "GET", poPath, nil, 200)
	poNo := poDetail["no"].(string)
	poLine := poDetail["lines"].([]any)[0].(obj)["id"]
	f.req(t, "POST", poPath+"/order", nil, 200)
	for i := 0; i < 2; i++ {
		f.req(t, "POST", poPath+"/receipts", obj{"warehouse_id": 1, "lines": []obj{{"line_id": poLine, "qty": 1}}}, 200)
	}
	// Deliberately make receipt/return IDs differ from their parent document IDs.
	dummy := f.order(t, iid, 1)
	f.req(t, "POST", path(dummy)+"/cancel", nil, 200)
	order := f.order(t, iid, 1)
	orderNo := f.req(t, "GET", path(order), nil, 200)["no"].(string)
	lid := f.line(t, order)
	f.req(t, "POST", path(order)+"/ship", obj{"lines": []obj{{"line_id": lid, "warehouse_id": 1}}}, 200)
	f.req(t, "POST", path(order)+"/returns", obj{"lines": []obj{{"line_id": lid, "qty": 1, "action": "restock", "warehouse_id": 1}}}, 200)
	pending := f.order(t, iid, 1)
	pendingNo := f.req(t, "GET", path(pending), nil, 200)["no"].(string)
	count := num(f.req(t, "POST", "/api/inventory/counts", obj{"warehouse_id": 1, "item_ids": []int64{iid}}, 200)["id"])
	countPath := fmt.Sprintf("/api/inventory/counts/%d", count)
	f.req(t, "PATCH", countPath, obj{"lines": []obj{{"item_id": iid, "counted_qty": 3}}}, 200)
	f.req(t, "POST", countPath+"/finish", nil, 200)
	itemPath := fmt.Sprintf("/api/inventory/items/%d", iid)
	check := func(purchaseAllowed, ordersAllowed bool) {
		t.Helper()
		detail := f.req(t, "GET", itemPath, nil, 200)
		groups := [][]any{
			detail["ledger"].([]any), detail["batches"].([]any), detail["reservations"].([]any),
			f.req(t, "GET", "/api/inventory/ledger", nil, 200)["items"].([]any),
			f.req(t, "GET", "/api/inventory/batches", nil, 200)["items"].([]any),
		}
		seen := map[string]bool{}
		for _, group := range groups {
			for _, raw := range group {
				row := raw.(obj)
				kind := row["ref_type"].(string)
				seen[kind] = true
				var label, link string
				switch kind {
				case "purchase_receipt":
					label = "采购到货"
					if purchaseAllowed {
						label += " " + poNo
						link = fmt.Sprintf("/purchase-orders/%d", po)
					}
				case "order", "order_return":
					label = "订单"
					if kind == "order_return" {
						label = "售后退货"
					}
					if ordersAllowed {
						id, no := order, orderNo
						if kind == "order" && num(row["ref_id"]) == pending {
							id, no = pending, pendingNo
						}
						label += " " + no
						link = fmt.Sprintf("/orders/%d", id)
					}
				case "count":
					label, link = fmt.Sprintf("盘点 #%d", count), fmt.Sprintf("/inventory/counts/%d", count)
				default:
					t.Fatalf("unexpected source %s", kind)
				}
				if row["ref_label"] != label || row["ref_path"] != link {
					t.Fatalf("source %s: got %v; want %s %s", kind, row, label, link)
				}
			}
		}
		for _, kind := range []string{"purchase_receipt", "order", "order_return", "count"} {
			if !seen[kind] {
				t.Fatal("source not exercised", kind)
			}
		}
	}
	check(true, true) // Admin bypasses assignment checks.
	uid := f.user(t, "stock-reader", "shops", []string{core.InventoryView})
	f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(?,1)", uid)
	f.login(t, "stock-reader")
	check(false, false) // Shop membership without order.view grants no links.
	setPerms := func(perms ...string) {
		f.exec(t, "UPDATE roles SET perms=? WHERE key='test_stock-reader'", core.JS(append([]string{core.InventoryView}, perms...)))
	}
	setPerms(core.OrderView)
	f.exec(t, "DELETE FROM user_shops WHERE user_id=?", uid)
	other := f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES('其他店','taobao','zh-CN',?)", core.Now())
	f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(?,?)", uid, other)
	check(false, false) // order.view alone cannot reveal another shop's order.
	f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(?,1)", uid)
	check(false, true)
	setPerms(core.PurchaseManage)
	check(true, false)
	setPerms(core.PurchaseManage, core.OrderView)
	check(true, true)
	// Cost permission does not grant either document's navigation rights.
	setPerms(core.ProductCost)
	check(false, false)
	for _, key := range []string{"ledger", "batches", "reservations"} {
		for _, raw := range f.req(t, "GET", itemPath, nil, 200)[key].([]any) {
			label := raw.(obj)["ref_label"].(string)
			if strings.Contains(label, poNo) || strings.Contains(label, orderNo) || strings.Contains(label, pendingNo) {
				t.Fatal("document number leaked", label)
			}
		}
	}
}
