package orders

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/inventory"
	"xstudio/internal/modules/products"
	"xstudio/internal/modules/purchase"
)

type obj = map[string]any
type fixture struct {
	c      *core.Core
	s      *httptest.Server
	client *http.Client
}

func boot(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	prompts := filepath.Join(dir, "prompts")
	if err := os.MkdirAll(prompts, 0700); err != nil {
		t.Fatal(err)
	}
	c, err := core.New(core.Config{DataDir: filepath.Join(dir, "data"), PromptDir: prompts, TemplateDir: filepath.Join(dir, "templates"), AdminUser: "admin", AdminPassword: "password123", SessionSecret: strings.Repeat("s", 32), Workers: 1}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(accounts.New(c), products.New(c), inventory.New(c), purchase.New(c), New(c)); err != nil {
		c.Close()
		t.Fatal(err)
	}
	s := httptest.NewServer(c.Handler())
	jar, _ := cookiejar.New(nil)
	f := &fixture{c, s, &http.Client{Jar: jar, Timeout: 10 * time.Second}}
	t.Cleanup(func() { s.Close(); c.Close() })
	f.login(t, "admin")
	return f
}
func (f *fixture) req(t *testing.T, method, path string, body any, status int) obj {
	t.Helper()
	var data string
	if body != nil {
		data = core.JS(body)
	} else if method != "GET" {
		data = "{}"
	}
	req, err := http.NewRequest(method, f.s.URL+path, strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", "application/json")
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, res.StatusCode, status, b)
	}
	var out obj
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(string(b), err)
	}
	return out
}
func (f *fixture) login(t *testing.T, user string) {
	t.Helper()
	f.client.Jar, _ = cookiejar.New(nil)
	f.req(t, "POST", "/api/login", obj{"username": user, "password": "password123"}, 200)
}
func (f *fixture) exec(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	res, err := f.c.DB.Exec(q, args...)
	if err != nil {
		t.Fatal(q, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *fixture) n(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := f.c.DB.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func (f *fixture) item(t *testing.T, code string) int64 {
	return num(f.req(t, "POST", "/api/inventory/items", obj{"code": code, "name": code, "kind": "sku", "unit": "个", "spec": obj{"尺寸": "8mm"}}, 200)["id"])
}
func (f *fixture) in(t *testing.T, item, qty, cost int64, grade string) {
	t.Helper()
	f.req(t, "POST", "/api/inventory/moves", obj{"kind": "other_in", "warehouse_id": 1, "lines": []obj{{"item_id": item, "qty": qty, "cost": cost, "grade": grade}}}, 200)
}
func (f *fixture) order(t *testing.T, item, qty int64) int64 {
	t.Helper()
	return num(f.req(t, "POST", "/api/orders", obj{"shop_id": 1, "lines": []obj{{"item_id": item, "qty": qty, "price": 100}}}, 200)["id"])
}
func (f *fixture) line(t *testing.T, id int64) int64 {
	t.Helper()
	return num(f.req(t, "GET", path(id), nil, 200)["lines"].([]any)[0].(obj)["id"])
}
func path(id int64) string { return fmt.Sprintf("/api/orders/%d", id) }
func num(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	}
	return 0
}
func (f *fixture) user(t *testing.T, name, scope string, perms []string) int64 {
	t.Helper()
	role := f.exec(t, "INSERT INTO roles(key,name,scope,perms,created_at) VALUES(?,?,?,?,?)", "test_"+name, name, scope, core.JS(perms), core.Now())
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return f.exec(t, "INSERT INTO users(username,password_hash,role_id,created_at) VALUES(?,?,?,?)", name, hash, role, core.Now())
}
func TestOrderReserveEditCancelAndShort(t *testing.T) {
	f := boot(t)
	item := f.item(t, "RESERVE")
	f.in(t, item, 3, 100, "")
	id := f.order(t, item, 2)
	if f.n(t, "SELECT sum(qty) FROM inv_reservations") != 2 {
		t.Fatal("not reserved")
	}
	f.req(t, "POST", "/api/orders", obj{"shop_id": 1, "lines": []obj{{"item_id": item, "qty": 2, "price": 100}}}, 409)
	forced := f.req(t, "POST", "/api/orders", obj{"shop_id": 1, "allow_short": true, "lines": []obj{{"item_id": item, "qty": 2, "price": 100}}}, 200)
	fid := num(forced["id"])
	if forced["short"] != true || f.n(t, "SELECT sum(qty) FROM inv_reservations") != 3 {
		t.Fatal(forced)
	}
	shortList := f.req(t, "GET", "/api/orders?short=1", nil, 200)
	if num(shortList["total"]) != 1 {
		t.Fatal(shortList)
	}
	// A note edit must neither require new stock nor release the partial reservation.
	f.req(t, "PATCH", path(fid), obj{"seller_note": "等补货"}, 200)
	f.req(t, "PATCH", path(id), obj{"lines": []obj{{"item_id": item, "qty": 4, "price": 100}}}, 409)
	if f.n(t, "SELECT qty FROM inv_reservations WHERE ref_id=?", id) != 2 {
		t.Fatal("failed edit released reservation")
	}
	f.req(t, "PATCH", path(id), obj{"lines": []obj{{"item_id": item, "qty": 1, "price": 100}}, "shipping_fee": 10, "discount": 5}, 200)
	detail := f.req(t, "GET", path(id), nil, 200)
	if num(detail["paid_amount"]) != 105 {
		t.Fatal(detail)
	}
	if num(f.req(t, "GET", "/api/orders?short=1", nil, 200)["total"]) != 0 {
		t.Fatal("free stock did not clear short")
	}
	f.req(t, "POST", path(id)+"/cancel", nil, 200)
	f.req(t, "POST", path(id)+"/cancel", nil, 409)
	f.req(t, "POST", path(fid)+"/cancel", nil, 200)
	if f.n(t, "SELECT count(*) FROM inv_reservations") != 0 {
		t.Fatal("cancel did not release")
	}
}
func TestOrderShippingWholeTransactionAndRemainderReturns(t *testing.T) {
	f := boot(t)
	a, b := f.item(t, "SHIP-A"), f.item(t, "SHIP-B")
	f.in(t, a, 3, 100, "AAA")
	f.in(t, b, 1, 5, "AA")
	id := num(f.req(t, "POST", "/api/orders", obj{"shop_id": 1, "lines": []obj{{"item_id": a, "qty": 3, "price": 100}, {"item_id": b, "qty": 1, "price": 100}}}, 200)["id"])
	lines := f.req(t, "GET", path(id), nil, 200)["lines"].([]any)
	la, lb := num(lines[0].(obj)["id"]), num(lines[1].(obj)["id"])
	warehouse := num(f.req(t, "POST", "/api/inventory/warehouses", obj{"name": "空仓", "type": "own"}, 200)["id"])
	f.req(t, "POST", path(id)+"/ship", obj{"lines": []obj{{"line_id": la, "warehouse_id": 1}, {"line_id": lb, "warehouse_id": warehouse}}}, 409)
	if f.n(t, "SELECT sum(qty_left) FROM inv_batches") != 4 || f.n(t, "SELECT sum(qty) FROM inv_reservations") != 4 || f.n(t, "SELECT count(*) FROM order_picks") != 0 {
		t.Fatal("partial shipment committed")
	}
	f.req(t, "POST", path(id)+"/ship", obj{"lines": []obj{{"line_id": la, "warehouse_id": 1}, {"line_id": la, "warehouse_id": 1}}}, 400)
	f.req(t, "POST", path(id)+"/ship", obj{"carrier": "顺丰", "tracking_no": "SF123", "lines": []obj{{"line_id": la, "warehouse_id": 1}, {"line_id": lb, "warehouse_id": 1}}}, 200)
	f.req(t, "POST", path(id)+"/ship", obj{"lines": []obj{{"line_id": la, "warehouse_id": 1}}}, 409)
	f.req(t, "PATCH", path(id), obj{"discount": 1}, 409)
	f.req(t, "POST", path(id)+"/cancel", nil, 409)
	if f.n(t, "SELECT count(*) FROM inv_reservations") != 0 || f.n(t, "SELECT sum(cost) FROM order_picks") != 105 {
		t.Fatal("shipment picks")
	}
	for i := 0; i < 3; i++ {
		f.req(t, "POST", path(id)+"/returns", obj{"refund_amount": 100, "reason": "退回", "lines": []obj{{"line_id": la, "qty": 1, "action": "restock", "warehouse_id": 1}}}, 200)
	}
	if f.n(t, "SELECT sum(cost) FROM inv_ledger WHERE kind='return_in'") != 100 || f.n(t, "SELECT sum(qty_in) FROM inv_batches WHERE ref_type='order_return' AND grade='AAA'") != 3 {
		t.Fatal("return cost/grade lost")
	}
	f.req(t, "POST", path(id)+"/returns", obj{"lines": []obj{{"line_id": la, "qty": 1, "action": "restock", "warehouse_id": 1}}}, 409)
	f.req(t, "POST", path(id)+"/returns", obj{"refund_amount": 100, "lines": []obj{{"line_id": lb, "qty": 1, "action": "scrap"}}}, 200)
	f.req(t, "POST", path(id)+"/returns", obj{"refund_amount": 1}, 409)
	if f.n(t, "SELECT sum(qty_left) FROM inv_batches") != 3 || f.req(t, "GET", path(id), nil, 200)["status"] != "shipped" {
		t.Fatal("scrap restocked")
	}
}
func TestOrderShopIsolationPermissionsAndCostRedaction(t *testing.T) {
	f := boot(t)
	item := f.item(t, "PRIVATE")
	f.in(t, item, 2, 100, "")
	id := f.order(t, item, 1)
	lid := f.line(t, id)
	f.req(t, "POST", path(id)+"/ship", obj{"lines": []obj{{"line_id": lid, "warehouse_id": 1}}}, 200)
	other := f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES('其他店','taobao','zh-CN',?)", core.Now())
	uid := f.user(t, "warehouse", "shops", []string{core.OrderView, core.OrderManage})
	f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(?,?)", uid, other)
	f.login(t, "warehouse")
	if num(f.req(t, "GET", "/api/orders", nil, 200)["total"]) != 0 {
		t.Fatal("cross-shop list leak")
	}
	for _, p := range []string{path(id), path(id) + "/ship", path(id) + "/cancel", path(id) + "/returns"} {
		method := "POST"
		var body any = obj{"refund_amount": 1, "lines": []obj{{"line_id": lid, "qty": 1, "action": "none"}}}
		if p == path(id) {
			method = "GET"
			body = nil
		} else if strings.HasSuffix(p, "ship") {
			body = obj{"lines": []obj{{"line_id": lid, "warehouse_id": 1}}}
		}
		f.req(t, method, p, body, 404)
	}
	f.req(t, "PATCH", path(id), obj{"seller_note": "越权"}, 404)
	f.req(t, "POST", "/api/orders", obj{"shop_id": 1, "lines": []obj{{"item_id": item, "qty": 1, "price": 1}}}, 404)
	f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(?,1)", uid)
	d := f.req(t, "GET", path(id), nil, 200)
	pick := d["lines"].([]any)[0].(obj)["picks"].([]any)[0].(obj)
	if _, ok := pick["cost"]; ok {
		t.Fatal("cost leaked", pick)
	}
	if _, ok := pick["returned_cost"]; ok {
		t.Fatal("return cost leaked")
	}
	f.exec(t, "UPDATE roles SET perms=? WHERE key='test_warehouse'", core.JS([]string{core.OrderView, core.ProductCost}))
	if _, ok := f.req(t, "GET", path(id), nil, 200)["lines"].([]any)[0].(obj)["picks"].([]any)[0].(obj)["cost"]; !ok {
		t.Fatal("cost not visible")
	}
	f.req(t, "POST", path(id)+"/cancel", nil, 403)
	f.user(t, "external", "shares", []string{core.OrderView, core.OrderManage, core.ProductCost})
	f.login(t, "external")
	f.req(t, "GET", "/api/orders", nil, 403)
	f.req(t, "GET", path(id), nil, 403)
	f.user(t, "none", "shops", nil)
	f.login(t, "none")
	f.req(t, "GET", "/api/orders", nil, 403)
	f.req(t, "POST", "/api/orders", obj{}, 403)
}
func TestOrderPlatformUniquenessValidationAndRollback(t *testing.T) {
	f := boot(t)
	item := f.item(t, "UNIQUE")
	f.in(t, item, 10, 101, "")
	body := obj{"shop_id": 1, "platform_no": "TB1", "lines": []obj{{"item_id": item, "qty": 1, "price": 100}}}
	id := num(f.req(t, "POST", "/api/orders", body, 200)["id"])
	f.req(t, "POST", "/api/orders", body, 409)
	f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES('第二店','taobao','zh-CN',?)", core.Now())
	body["shop_id"] = 2
	f.req(t, "POST", "/api/orders", body, 200)
	for _, p := range []obj{{"qty": 0, "price": 1}, {"qty": 1, "price": -1}, {"qty": int64(math.MaxInt64), "price": 2}} {
		p["item_id"] = item
		f.req(t, "POST", "/api/orders", obj{"shop_id": 1, "lines": []obj{p}}, 400)
	}
	material := f.exec(t, "INSERT INTO inv_items(code,code_key,name,kind,unit,created_by,created_at,updated_at) VALUES('MAT','mat','原料','material','个',1,?,?)", core.Now(), core.Now())
	f.req(t, "POST", "/api/orders", obj{"shop_id": 1, "allow_short": true, "lines": []obj{{"item_id": item, "qty": 1, "price": 1}, {"item_id": material, "qty": 1, "price": 1}}}, 400)
	if f.n(t, "SELECT count(*) FROM orders") != 2 || f.n(t, "SELECT sum(qty) FROM inv_reservations") != 2 {
		t.Fatal("failed order leaked data")
	}
	f.req(t, "PATCH", path(id), obj{"status": "shipped"}, 400)
}
func TestOrderConcurrentReservationDoesNotOversell(t *testing.T) {
	f := boot(t)
	item := f.item(t, "CONCURRENT")
	f.in(t, item, 10, 100, "")
	statuses := make(chan int, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", f.s.URL+"/api/orders", strings.NewReader(core.JS(obj{"shop_id": 1, "lines": []obj{{"item_id": item, "qty": 1, "price": 1}}})))
			req.Header.Set("X-Requested-With", "fetch")
			res, err := f.client.Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			statuses <- res.StatusCode
		}()
	}
	wg.Wait()
	close(statuses)
	success := 0
	for s := range statuses {
		if s == 200 {
			success++
		} else if s != 409 {
			t.Fatalf("unexpected status %d", s)
		}
	}
	if success != 10 || f.n(t, "SELECT sum(qty) FROM inv_reservations") != 10 {
		t.Fatal("oversold", success)
	}
}
func TestOrderDropshipAndReturnRollback(t *testing.T) {
	f := boot(t)
	item := f.item(t, "DROPSHIP")
	wid := num(f.req(t, "POST", "/api/inventory/warehouses", obj{"name": "代发", "type": "dropship"}, 200)["id"])
	f.req(t, "PUT", fmt.Sprintf("/api/inventory/dropship/%d/%d", wid, item), obj{"qty": nil, "unit_cost": 33, "currency": "CNY"}, 200)
	id := f.order(t, item, 3)
	lid := f.line(t, id)
	f.req(t, "POST", path(id)+"/ship", obj{"lines": []obj{{"line_id": lid, "warehouse_id": wid}}}, 200)
	if f.n(t, "SELECT sum(cost) FROM order_picks") != 99 {
		t.Fatal("dropship cost")
	}
	// A failing return after an earlier restock must roll back the restock and refund.
	f.req(t, "POST", path(id)+"/returns", obj{"refund_amount": 10, "lines": []obj{{"line_id": lid, "qty": 1, "action": "restock", "warehouse_id": 1}, {"line_id": 999, "qty": 1, "action": "none"}}}, 404)
	if f.n(t, "SELECT count(*) FROM order_returns") != 0 || f.n(t, "SELECT count(*) FROM inv_batches") != 0 {
		t.Fatal("partial return committed")
	}
	f.req(t, "POST", path(id)+"/returns", obj{"lines": []obj{{"line_id": lid, "qty": 1, "action": "restock", "warehouse_id": wid}}}, 400)
	f.req(t, "POST", path(id)+"/returns", obj{"refund_amount": 100, "lines": []obj{{"line_id": lid, "qty": 1, "action": "none"}}}, 200)
	f.req(t, "POST", path(id)+"/returns", obj{"refund_amount": 200, "lines": []obj{{"line_id": lid, "qty": 2, "action": "restock", "warehouse_id": 1}}}, 200)
	if f.n(t, "SELECT sum(cost_in) FROM inv_batches") != 66 {
		t.Fatal("refund-only consumed incorrect cost")
	}
}
func TestPurchaseToShipmentToReturnIntegration(t *testing.T) {
	f := boot(t)
	item := f.item(t, "END-TO-END")
	sid := num(f.req(t, "POST", "/api/suppliers", obj{"name": "供应商"}, 201)["supplier"].(obj)["id"])
	po := num(f.req(t, "POST", "/api/purchase-orders", obj{"supplier_id": sid, "shipping_fee": 1, "lines": []obj{{"item_id": item, "qty": 3, "amount": 100}}}, 200)["id"])
	poPath := fmt.Sprintf("/api/purchase-orders/%d", po)
	lid := num(f.req(t, "GET", poPath, nil, 200)["lines"].([]any)[0].(obj)["id"])
	f.req(t, "POST", poPath+"/order", nil, 200)
	f.req(t, "POST", poPath+"/receipts", obj{"warehouse_id": 1, "lines": []obj{{"line_id": lid, "qty": 3, "grades": []obj{{"grade": "AA", "qty": 1}, {"grade": "AAA", "qty": 2}}}}}, 200)
	id := f.order(t, item, 3)
	line := f.line(t, id)
	f.req(t, "POST", path(id)+"/ship", obj{"lines": []obj{{"line_id": line, "warehouse_id": 1}}}, 200)
	if f.n(t, "SELECT sum(cost) FROM order_picks") != 101 {
		t.Fatal("procurement cost lost")
	}
	for i := 0; i < 3; i++ {
		f.req(t, "POST", path(id)+"/returns", obj{"lines": []obj{{"line_id": line, "qty": 1, "action": "restock", "warehouse_id": 1}}}, 200)
	}
	if f.n(t, "SELECT sum(cost_left) FROM inv_batches") != 101 || f.n(t, "SELECT sum(qty_left) FROM inv_batches") != 3 {
		t.Fatal("full return did not conserve cost")
	}
	if f.n(t, "SELECT count(*) FROM audit_log WHERE action IN ('order.create','order.ship','order.return')") != 5 {
		t.Fatal("audit missing")
	}
}

func TestReturnProratesWholeLineAcrossDifferentCostBatches(t *testing.T) {
	f := boot(t)
	item := f.item(t, "BLENDED")
	f.in(t, item, 1, 1, "AA")
	f.in(t, item, 2, 99, "AAA")
	id := f.order(t, item, 3)
	lid := f.line(t, id)
	f.req(t, "POST", path(id)+"/ship", obj{"lines": []obj{{"line_id": lid, "warehouse_id": 1}}}, 200)
	for i, want := range []int64{33, 67, 100} {
		f.req(t, "POST", path(id)+"/returns", obj{"lines": []obj{{"line_id": lid, "qty": 1, "action": "restock", "warehouse_id": 1}}}, 200)
		if got := f.n(t, "SELECT sum(cost) FROM inv_ledger WHERE kind='return_in'"); got != want {
			t.Fatalf("return %d cost=%d want=%d", i+1, got, want)
		}
	}
	if f.n(t, "SELECT sum(qty_in) FROM inv_batches WHERE ref_type='order_return' AND grade='AA'") != 1 {
		t.Fatal("grade not preserved")
	}
}

func TestMixedCostCurrencyShipmentRollsBack(t *testing.T) {
	f := boot(t)
	item := f.item(t, "MIXED")
	f.in(t, item, 1, 100, "")
	// The stock API preserves the batch currency independently of selling currency.
	tx, err := f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = inventory.StockIn(tx, inventory.In{ItemID: item, WarehouseID: 1, Qty: 1, Cost: 10, Currency: "USD", Kind: "other_in", RefType: "manual", RefID: 999, UserID: 1})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	id := f.order(t, item, 2)
	lid := f.line(t, id)
	f.req(t, "POST", path(id)+"/ship", obj{"lines": []obj{{"line_id": lid, "warehouse_id": 1}}}, 409)
	if f.n(t, "SELECT sum(qty_left) FROM inv_batches") != 2 || f.n(t, "SELECT sum(qty) FROM inv_reservations") != 2 {
		t.Fatal("mixed-currency shipment was not rolled back")
	}
}

// Compile-time guarantee: modules integrate with a caller-owned transaction.
var _ inventory.Querier = (*sql.Tx)(nil)
