package inventory

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"xstudio/internal/core"
)

func fmtDropship(wid, id int64) string { return fmt.Sprintf("/api/inventory/dropship/%d/%d", wid, id) }
func assertNoCosts(t *testing.T, v any) {
	t.Helper()
	switch o := v.(type) {
	case map[string]any:
		for k, x := range o {
			switch k {
			case "cost", "cost_in", "cost_left", "cost_value", "unit_cost", "gain_cost", "loss_cost":
				t.Fatalf("secret field %q returned: %v", k, o)
			}
			assertNoCosts(t, x)
		}
	case []any:
		for _, x := range o {
			assertNoCosts(t, x)
		}
	}
}

// Acceptance 1: every route is guarded, including externally shared users who
// have accidentally been assigned inventory permissions in a custom role.
func TestRoutePermissionsAndExternalCollaborators(t *testing.T) {
	f := boot(t)
	id := f.item(t, "PERM")
	f.in(t, id, 1, 5, 100, "")
	wid := f.wh(t, "dropship")
	cid := f.count(t, 1, []int64{id})
	reads := []string{"/api/inventory/items", path(id), "/api/inventory/items/lookup?codes=PERM", "/api/inventory/warehouses", "/api/inventory/dropship", "/api/inventory/ledger", "/api/inventory/batches", "/api/inventory/counts", countPath(cid), "/api/inventory/export.csv"}
	writes := []struct{ method, path string }{
		{"POST", "/api/inventory/items"}, {"PATCH", path(id)}, {"POST", "/api/inventory/warehouses"}, {"PATCH", "/api/inventory/warehouses/1"},
		{"PUT", fmtDropship(wid, id)}, {"DELETE", fmtDropship(wid, id)}, {"POST", "/api/inventory/moves"}, {"POST", "/api/inventory/transfers"},
		{"POST", "/api/inventory/counts"}, {"PATCH", countPath(cid)}, {"POST", countPath(cid) + "/finish"}, {"POST", countPath(cid) + "/cancel"},
	}
	f.user(t, "designer", "designer")
	f.user(t, "operator", "operator")
	f.exec(t, "INSERT INTO roles(key,name,scope,perms,created_at) VALUES('external-test','外部测试','shares',?,?)", core.JS([]string{core.InventoryView, core.InventoryEdit, core.ProductCost}), core.Now())
	f.user(t, "external", "external-test")
	for _, name := range []string{"designer", "external"} {
		f.login(t, name)
		for _, p := range reads {
			f.raw(t, "GET", p, nil, 403)
		}
		for _, v := range writes {
			f.raw(t, v.method, v.path, obj{}, 403)
		}
	}
	f.login(t, "operator")
	for _, p := range reads {
		f.raw(t, "GET", p, nil, 200)
	}
	for _, v := range writes {
		f.raw(t, v.method, v.path, obj{}, 403)
	}
	f.login(t, "admin")
	r := httptest.NewRequest("POST", "/api/inventory/items", strings.NewReader(`{}`))
	r.AddCookie(f.cookie)
	w := httptest.NewRecorder()
	f.c.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("missing CSRF header allowed")
	}
}

func TestCostsOmittedEverywhereAndBothPermissionsGrantAccess(t *testing.T) {
	f := boot(t)
	id := f.item(t, "SECRET")
	f.in(t, id, 1, 5, 100, "")
	wid := f.wh(t, "dropship")
	dp := fmtDropship(wid, id)
	f.req(t, "PUT", dp, obj{"qty": 3, "unit_cost": 17}, 200)
	cid := f.count(t, 1, []int64{id})
	f.req(t, "PATCH", countPath(cid), obj{"lines": []obj{{"item_id": id, "counted_qty": 6, "unit_cost": 71}}}, 200)
	f.user(t, "warehouse", "warehouse")
	f.login(t, "warehouse")
	for _, p := range []string{"/api/inventory/items", path(id), "/api/inventory/items/lookup?codes=SECRET", "/api/inventory/batches", "/api/inventory/ledger", "/api/inventory/warehouses", "/api/inventory/dropship", countPath(cid)} {
		o := f.req(t, "GET", p, nil, 200)
		assertNoCosts(t, map[string]any(o))
	}
	// Forged cost inputs from warehouse staff must not set costs or overwrite existing secrets.
	f.req(t, "POST", "/api/inventory/moves", obj{"kind": "other_in", "warehouse_id": 1, "lines": []obj{{"item_id": id, "qty": 1, "cost": 999}}}, 200)
	if f.n(t, "SELECT cost_in FROM inv_batches WHERE item_id=? ORDER BY id DESC LIMIT 1", id) != 0 {
		t.Fatal("unauthorized input cost persisted")
	}
	f.req(t, "PUT", dp, obj{"qty": 5, "unit_cost": 999, "currency": "USD"}, 200)
	if f.n(t, "SELECT unit_cost FROM inv_dropship WHERE item_id=?", id) != 17 {
		t.Fatal("secret dropship cost overwritten")
	}
	f.req(t, "PATCH", countPath(cid), obj{"lines": []obj{{"item_id": id, "counted_qty": 7, "unit_cost": 999}}}, 200)
	if f.n(t, "SELECT unit_cost FROM inv_count_lines WHERE count_id=?", cid) != 71 {
		t.Fatal("secret count cost overwritten")
	}
	result := f.req(t, "POST", countPath(cid)+"/finish", nil, 200)
	assertNoCosts(t, map[string]any(result))
	for _, perm := range []string{core.ProductCost, core.FinanceView} {
		f.exec(t, "UPDATE roles SET perms=? WHERE key='warehouse'", core.JS([]string{core.InventoryView, core.InventoryEdit, perm}))
		detail := f.req(t, "GET", path(id), nil, 200)
		if _, ok := detail["cost_value"]; !ok {
			t.Fatal("cost missing for", perm)
		}
		if _, ok := rows(f.req(t, "GET", "/api/inventory/batches", nil, 200))[0].(map[string]any)["cost_left"]; !ok {
			t.Fatal("batch cost missing", perm)
		}
		if _, ok := rows(f.req(t, "GET", "/api/inventory/dropship", nil, 200))[0].(map[string]any)["unit_cost"]; !ok {
			t.Fatal("dropship cost missing", perm)
		}
	}
}

func TestLinkedProductAuthorizationAndHiddenProductName(t *testing.T) {
	f := boot(t)
	admin := f.n(t, "SELECT id FROM users WHERE username='admin'")
	pid := f.exec(t, "INSERT INTO products(name,created_by,created_at,updated_at,category_id) VALUES('保密商品',?,?,?,(SELECT id FROM categories LIMIT 1))", admin, core.Now(), core.Now())
	id := int64(f.req(t, "POST", "/api/inventory/items", obj{"code": "LINK", "name": "LINK", "kind": "sku", "unit": "个", "product_id": pid}, 200)["id"].(float64))
	f.user(t, "warehouse", "warehouse")
	f.login(t, "warehouse")
	detail := f.req(t, "GET", path(id), nil, 200)
	product := detail["product"].(map[string]any)
	if product["id"] != float64(pid) || product["name"] != "" {
		t.Fatal(product)
	}
	f.req(t, "POST", "/api/inventory/items", obj{"code": "BADLINK", "name": "bad", "kind": "sku", "unit": "个", "product_id": pid}, 404)
	f.req(t, "PATCH", path(id), obj{"product_id": pid}, 404)
	f.req(t, "PATCH", path(id), obj{"name": "允许修改普通字段"}, 200)
	f.login(t, "admin")
	f.req(t, "PATCH", path(id), obj{"kind": "material"}, 400)
	f.req(t, "PATCH", path(id), obj{"kind": "material", "product_id": nil}, 200)
}

func TestMalformedJSONHasSingleResponse(t *testing.T) {
	f := boot(t)
	for _, body := range []string{`{"bad":`, `{} {}`, `{"code": 123}`} {
		r := httptest.NewRequest("POST", "/api/inventory/items", strings.NewReader(body))
		r.Header.Set("X-Requested-With", "fetch")
		r.AddCookie(f.cookie)
		w := httptest.NewRecorder()
		f.c.Handler().ServeHTTP(w, r)
		if w.Code != 400 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}
