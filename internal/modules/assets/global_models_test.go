package assets_test

import (
	"fmt"
	"testing"
)

func TestGlobalModelVisibilityAndScopeChanges(t *testing.T) {
	f := newFixture(t, nil)
	shop := id(f.req(t, "POST", "/api/shops", obj{"name": "乙店", "platform": "taobao", "language": "zh-CN"}, 201)["shop"])
	global := id(f.req(t, "POST", "/api/models", obj{"name": "全局"}, 201)["model"])
	local := modelCreate(t, f, 1)
	other := modelCreate(t, f, shop)
	f.user(t, "a-model-user", 2, 1)
	f.user(t, "b-model-user", 2, shop)
	f.login(t, "a-model-user")
	all := f.req(t, "GET", "/api/models", nil, 200)["items"].([]any)
	if len(all) != 2 {
		t.Fatal(all)
	}
	f.req(t, "GET", fmt.Sprintf("/api/models/%d", other), nil, 404)
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", other), obj{"shop_id": nil}, 404)
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", global), obj{"shop_id": shop}, 404)
	for _, raw := range all {
		if id(raw) == global && raw.(obj)["shop_id"] != nil {
			t.Fatal(raw)
		}
	}
	filtered := f.req(t, "GET", "/api/models?shop_id=1", nil, 200)["items"].([]any)
	if len(filtered) != 2 {
		t.Fatal(filtered)
	}
	// 只管一个店铺的角色不能把模特设为全局，也不能把全局模特收回到自己店铺
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", local), obj{"shop_id": nil}, 403)
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", global), obj{"shop_id": 1}, 403)
	f.login(t, "admin")
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", local), obj{"shop_id": nil}, 200)
	f.login(t, "b-model-user")
	f.req(t, "GET", fmt.Sprintf("/api/models/%d", local), nil, 200)
	f.req(t, "GET", fmt.Sprintf("/api/models/%d", global), nil, 200)
	f.login(t, "admin")
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", local), obj{"shop_id": 1}, 200)
	if _, err := f.c.DB.Exec("UPDATE shop_models SET id=id+100 WHERE id=?", global); err == nil {
		t.Fatal("model id changed")
	}
	rows, err := f.c.DB.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("broken model foreign keys")
	}
	f.assertAudit(t, "assets.model.create", "assets.model.update")
}
