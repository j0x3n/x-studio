package products

import (
	"fmt"
	"testing"
	"xstudio/internal/core"
)

func TestDuplicateProductScopeAndBasedOn(t *testing.T) {
	f := newCategoryFixture(t)
	created := f.req(t, "POST", "/api/products", core.Object{"name": "源商品", "info": core.Object{"style": "耳钉", "note": "照样复制"}, "cost": core.Object{"price": 12}, "supplier": core.Object{"name": "货源"}, "shop_ids": []int64{1}}, 201)["product"].(map[string]any)
	source := int64(created["id"].(float64))
	f.req(t, "PATCH", fmt.Sprintf("/api/products/%d", source), core.Object{"status": "listed"}, 200)
	copy := f.req(t, "POST", fmt.Sprintf("/api/products/%d/duplicate", source), core.Object{"name": "新商品"}, 201)["product"].(map[string]any)
	if copy["status"] != "sampling" || copy["name"] != "新商品" || copy["based_on_id"] != float64(source) || copy["cost"].(map[string]any)["price"] != float64(12) || len(copy["listings"].([]any)) != 0 || copy["asset_count"] != float64(0) {
		t.Fatal(copy)
	}
	id := int64(copy["id"].(float64))
	get := f.req(t, "GET", fmt.Sprintf("/api/products/%d", id), nil, 200)["product"].(map[string]any)
	if get["based_on"].(map[string]any)["name"] != "源商品" || get["info"].(map[string]any)["note"] != "照样复制" {
		t.Fatal(get)
	}
	if _, err := f.c.DB.Exec("UPDATE roles SET perms=? WHERE id=2", core.JS([]string{core.ProductView, core.ProductEdit})); err != nil {
		t.Fatal(err)
	}
	f.req(t, "POST", "/api/users", core.Object{"username": "no-cost", "password": "password123", "role_id": 2, "shop_ids": []int64{1}}, 201)
	f.req(t, "POST", "/api/login", core.Object{"username": "no-cost", "password": "password123"}, 200)
	limited := f.req(t, "POST", fmt.Sprintf("/api/products/%d/duplicate", source), core.Object{"name": "无成本复制"}, 201)["product"].(map[string]any)
	var cost, supplier string
	if err := f.c.DB.QueryRow("SELECT cost,supplier FROM products WHERE id=?", int64(limited["id"].(float64))).Scan(&cost, &supplier); err != nil || cost != "{}" || supplier != "{}" {
		t.Fatal(cost, supplier, err)
	}
	f.req(t, "POST", "/api/login", core.Object{"username": "admin", "password": "password123"}, 200)
	f.req(t, "DELETE", fmt.Sprintf("/api/products/%d", source), nil, 200)
	get = f.req(t, "GET", fmt.Sprintf("/api/products/%d", id), nil, 200)["product"].(map[string]any)
	if get["based_on"] != nil || get["based_on_id"] != nil {
		t.Fatal(get)
	}
}
