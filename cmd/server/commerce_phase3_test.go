package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assembly"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/finance"
	"xstudio/internal/modules/inventory"
	"xstudio/internal/modules/kb"
	"xstudio/internal/modules/orders"
	"xstudio/internal/modules/products"
	"xstudio/internal/modules/purchase"
	"xstudio/internal/modules/tasks"
)

// One isolated business flow checks transaction boundaries that a UI build
// cannot verify. No real AI, browser, production database, or network provider.
func TestPhase3BusinessTransactions(t *testing.T) {
	dir := t.TempDir()
	c, err := core.New(core.Config{DataDir: filepath.Join(dir, "data"), PromptDir: filepath.Join(dir, "prompts"), TemplateDir: filepath.Join(dir, "templates"), AdminUser: "admin", AdminPassword: "phase3-password", SessionSecret: strings.Repeat("s", 32), Workers: 1}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(accounts.New(c), products.New(c), assets.New(c), tasks.New(c), inventory.New(c), kb.New(c), purchase.New(c), orders.New(c), assembly.New(c), finance.New(c)); err != nil {
		c.Close()
		t.Fatal(err)
	}
	defer c.Close()
	s := httptest.NewServer(c.Handler())
	defer s.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 8 * time.Second}
	call := func(method, path string, body any, success bool) core.Object {
		t.Helper()
		req, e := http.NewRequest(method, s.URL+path, bytes.NewBufferString(core.JS(body)))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "fetch")
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var out core.Object
		if e = json.NewDecoder(res.Body).Decode(&out); e != nil {
			t.Fatal(path, e)
		}
		if ok := res.StatusCode >= 200 && res.StatusCode < 300; ok != success {
			t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, core.JS(out))
		}
		return out
	}
	number := func(o core.Object, key string) int64 {
		t.Helper()
		if n, ok := o[key].(float64); ok {
			return int64(n)
		}
		t.Fatalf("missing %s: %s", key, core.JS(o))
		return 0
	}
	scalar := func(query string, args ...any) int64 {
		t.Helper()
		var n int64
		if e := c.DB.QueryRow(query, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	idText := func(id int64) string { return core.JS(id) }
	call("POST", "/api/login", core.Object{"username": "admin", "password": "phase3-password"}, true)
	raw := number(call("POST", "/api/inventory/items", core.Object{"code": "P3-RAW", "name": "原料", "kind": "material", "unit": "颗", "safety_stock": 5}, true), "id")
	finished := number(call("POST", "/api/inventory/items", core.Object{"code": "P3-FINISHED", "name": "成品", "kind": "sku", "unit": "条"}, true), "id")
	tx, err := c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = inventory.StockIn(tx, inventory.In{ItemID: raw, WarehouseID: 1, Qty: 10, Cost: 100, Currency: "CNY", Kind: "other_in", RefType: "manual", UserID: 1}); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	recipe := call("POST", "/api/assembly/recipes", core.Object{"name": "两条一组", "output_item_id": finished, "output_qty": 2, "currency": "CNY", "labor_cost": 5, "lines": []core.Object{{"item_id": raw, "qty": 3, "loss_bps": 1000}}}, true)
	rid := number(recipe["recipe"].(map[string]any), "id")
	assemblyOrder := call("POST", "/api/assembly/orders", core.Object{"recipe_id": rid, "runs": 2, "warehouse_id": 1}, true)
	aid := number(assemblyOrder["order"].(map[string]any), "id")
	path := "/api/assembly/orders/" + idText(aid) + "/complete"
	call("POST", path, core.Object{}, true)
	call("POST", path, core.Object{}, true)
	if scalar("SELECT sum(qty_left) FROM inv_batches WHERE item_id=?", raw) != 3 || scalar("SELECT sum(qty_left) FROM inv_batches WHERE item_id=?", finished) != 4 || scalar("SELECT sum(cost_left) FROM inv_batches WHERE item_id=?", finished) != 80 {
		t.Fatal("组装损耗、实际成本或重复完成不正确")
	}
	failed := call("POST", "/api/assembly/orders", core.Object{"recipe_id": rid, "runs": 2, "warehouse_id": 1}, true)
	fid := number(failed["order"].(map[string]any), "id")
	call("POST", "/api/assembly/orders/"+idText(fid)+"/complete", core.Object{}, false)
	if scalar("SELECT sum(qty_left) FROM inv_batches WHERE item_id=?", raw) != 3 || scalar("SELECT sum(qty_left) FROM inv_batches WHERE item_id=?", finished) != 4 {
		t.Fatal("失败组装未完整回滚")
	}
	supplier := call("POST", "/api/suppliers", core.Object{"name": "三期供应商"}, true)
	sid := number(supplier["supplier"].(map[string]any), "id")
	po := number(call("POST", "/api/purchase-orders", core.Object{"supplier_id": sid, "lines": []core.Object{{"item_id": raw, "qty": 10, "amount": 1000}}}, true), "id")
	call("POST", "/api/purchase-orders/"+idText(po)+"/order", core.Object{}, true)
	accountResult := call("POST", "/api/finance/accounts", core.Object{"name": "隔离测试账户", "kind": "cash", "currency": "CNY", "opening_balance": 10000}, true)
	account := number(accountResult["account"].(map[string]any), "id")
	paymentPath := "/api/finance/payables/" + idText(po) + "/payments"
	payment := core.Object{"account_id": account, "amount": 400, "request_key": "phase3-payment-one"}
	call("POST", paymentPath, payment, true)
	call("POST", paymentPath, payment, true)
	call("POST", paymentPath, core.Object{"account_id": account, "amount": 700, "request_key": "phase3-payment-over"}, false)
	call("POST", paymentPath, core.Object{"account_id": account, "amount": 401, "request_key": "phase3-payment-one"}, false)
	accountsResult := call("GET", "/api/finance/accounts", nil, true)
	items, ok := accountsResult["items"].([]any)
	if !ok || len(items) != 1 || number(items[0].(map[string]any), "balance") != 9600 {
		t.Fatal("重复付款/超付改变了账户余额", accountsResult)
	}
	call("GET", "/api/inventory/replenishment", nil, true)
	call("GET", "/api/push/key", nil, true)
	shopAccount := call("POST", "/api/finance/accounts", core.Object{"name": "店铺测试账户", "kind": "cash", "currency": "CNY", "shop_id": 1, "opening_balance": 1000}, true)
	shopAccountID := number(shopAccount["account"].(map[string]any), "id")
	financeRole := scalar("SELECT id FROM roles WHERE key='finance'")
	call("POST", "/api/users", core.Object{"username": "phase3-finance", "display_name": "财务测试", "password": "phase3-password", "role_id": financeRole, "shop_ids": []int{1}}, true)
	call("POST", "/api/logout", core.Object{}, true)
	call("POST", "/api/login", core.Object{"username": "phase3-finance", "password": "phase3-password"}, true)
	call("POST", "/api/finance/entries", core.Object{"account_id": shopAccountID, "direction": "out", "amount": 100, "category": "包装"}, true)
	expense := core.Object{"account_id": shopAccountID, "shop_id": 1, "amount": 50, "category": "包装", "request_key": "phase3-expense-one"}
	call("POST", "/api/finance/expenses", expense, true)
	call("POST", "/api/finance/expenses", expense, true)
	call("POST", "/api/finance/entries", core.Object{"account_id": account, "direction": "out", "amount": 100, "category": "越权"}, false)
	visibleAccounts := call("GET", "/api/finance/accounts", nil, true)["items"].([]any)
	if len(visibleAccounts) != 1 || number(visibleAccounts[0].(map[string]any), "id") != shopAccountID {
		t.Fatal("财务看到了未授权账户")
	}
	if number(visibleAccounts[0].(map[string]any), "balance") != 850 {
		t.Fatal("重复费用扣款或店铺记账错误")
	}
	call("POST", "/api/logout", core.Object{}, true)
	call("POST", "/api/login", core.Object{"username": "admin", "password": "phase3-password"}, true)
	warehouseRole := scalar("SELECT id FROM roles WHERE key='warehouse'")
	call("POST", "/api/users", core.Object{"username": "phase3-warehouse", "display_name": "仓管测试", "password": "phase3-password", "role_id": warehouseRole, "shop_ids": []int{1}}, true)
	call("POST", "/api/logout", core.Object{}, true)
	call("POST", "/api/login", core.Object{"username": "phase3-warehouse", "password": "phase3-password"}, true)
	warehouseOrder := call("GET", "/api/assembly/orders/"+idText(aid), nil, true)
	if strings.Contains(core.JS(warehouseOrder), "labor_cost") || strings.Contains(core.JS(warehouseOrder), "actual_cost") || strings.Contains(core.JS(warehouseOrder), "total_cost") {
		t.Fatal("仓管响应泄漏成本")
	}
	call("POST", "/api/logout", core.Object{}, true)
	call("POST", "/api/login", core.Object{"username": "admin", "password": "phase3-password"}, true)
	sale := number(call("POST", "/api/orders", core.Object{"shop_id": 1, "paid_at": "2026-08-31T23:59:00+08:00", "lines": []core.Object{{"item_id": finished, "qty": 1, "price": 200}}}, true), "id")
	saleLine := scalar("SELECT id FROM order_lines WHERE order_id=?", sale)
	call("POST", "/api/orders/"+idText(sale)+"/ship", core.Object{"lines": []core.Object{{"line_id": saleLine, "warehouse_id": 1}}}, true)
	if _, err = c.DB.Exec("UPDATE orders SET shipped_at='2026-09-01T00:01:00.000000000Z' WHERE id=?", sale); err != nil {
		t.Fatal(err)
	}
	report := call("GET", "/api/finance/reports?month=2026-09&shop_id=1", nil, true)
	totals := report["totals"].(map[string]any)
	if number(totals, "cny_revenue") != 200 || number(totals, "cny_cost") != 20 || number(totals, "cny_profit") != 180 {
		t.Fatal("跨月发货收入/成本未同时确认", core.JS(report))
	}
	call("POST", "/api/orders/"+idText(sale)+"/returns", core.Object{"refund_amount": 200, "lines": []core.Object{{"line_id": saleLine, "qty": 1, "action": "restock", "warehouse_id": 1}}}, true)
	if _, err = c.DB.Exec("UPDATE order_returns SET created_at='2026-11-01T00:01:00.000000000Z' WHERE order_id=?", sale); err != nil {
		t.Fatal(err)
	}
	report = call("GET", "/api/finance/reports?month=2026-11&shop_id=1", nil, true)
	totals = report["totals"].(map[string]any)
	if number(totals, "cny_refund") != 200 || number(totals, "cny_cost") != -20 || number(totals, "cny_profit") != -180 {
		t.Fatal("跨月退款回仓未冲回实际成本", core.JS(report))
	}
	// Check actual relational integrity after migrations and business writes.
	rows, err := c.DB.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("三期迁移或业务写入破坏外键")
	}
}
