package creative

import (
	"context"
	"fmt"
	"testing"
	"xstudio/internal/core"
)

func TestPlansCRUDAndEffectiveMerge(t *testing.T) {
	f := newFixture(t)
	f.exec(t, `INSERT INTO creative_templates(key,name,slot,updated_at) VALUES('plan_main','主图','main1',?),('plan_detail','详情','d1',?),('override_main','覆盖','main1',?)`, core.Now(), core.Now(), core.Now())
	model := f.request(t, "POST", "/api/models", core.Object{"name": "共用模特"}, 201)["model"].(map[string]any)
	mid := int64(number(model["id"]))
	created := f.request(t, "POST", "/api/creative/plans", core.Object{"name": "耳饰方案", "note": "初始", "categories": []string{"pearl"}, "template_set": map[string]string{"main1": "plan_main", "d1": "plan_detail"}, "models": map[string][]int64{"female_main": {mid}, "hand": {}}, "scene_id": nil}, 201)["plan"].(map[string]any)
	id := int64(number(created["id"]))
	path := fmt.Sprintf("/api/creative/plans/%d", id)
	if created["archived"] != false {
		t.Fatal(created)
	}
	f.request(t, "PATCH", fmt.Sprintf("/api/models/%d", mid), core.Object{"archived": true}, 200)
	f.request(t, "DELETE", fmt.Sprintf("/api/models/%d", mid), nil, 409)
	f.request(t, "PATCH", fmt.Sprintf("/api/models/%d", mid), core.Object{"archived": false}, 200)
	f.request(t, "GET", path, nil, 200)
	f.request(t, "PATCH", path, core.Object{"note": "新备注"}, 200)
	f.request(t, "POST", "/api/creative/plans", core.Object{"name": "坏模板", "template_set": map[string]string{"main1": "missing"}}, 400)
	f.request(t, "PATCH", path, core.Object{"models": map[string][]int64{"female_main": {9999}}}, 400)
	productPath := fmt.Sprintf("/api/products/%d", f.productID)
	f.request(t, "PATCH", productPath, core.Object{"plan_id": id, "plan_overrides": core.Object{"template_set": map[string]string{"main1": "override_main"}, "models": map[string][]int64{"hand": {mid}}, "detail_layout": ""}}, 200)
	f.request(t, "PATCH", productPath, core.Object{"plan_id": 9999}, 400)
	f.request(t, "PATCH", productPath, core.Object{"plan_overrides": core.Object{"models": map[string][]int64{"hand": {9999}}}}, 400)
	resolved, err := f.a.EffectivePlan(context.Background(), f.productID)
	if err != nil || resolved.Plan == nil || resolved.Effective.TemplateSet["main1"] != "override_main" || resolved.Effective.TemplateSet["d1"] != "plan_detail" || len(resolved.Effective.Models["female_main"]) != 1 || len(resolved.Effective.Models["hand"]) != 1 {
		t.Fatal(resolved, err)
	}
	f.request(t, "GET", productPath+"/plan", nil, 200)
	duplicate := f.request(t, "POST", productPath+"/duplicate", core.Object{"name": "按方案新建"}, 201)["product"].(map[string]any)
	if number(duplicate["plan_id"]) != float64(id) || duplicate["plan_overrides"].(map[string]any)["models"] == nil || len(duplicate["listings"].([]any)) != 0 {
		t.Fatal(duplicate)
	}
	deleted := f.request(t, "DELETE", path, nil, 200)
	if deleted["archived"] != true || deleted["deleted"] != false {
		t.Fatal(deleted)
	}
	resolved, err = f.a.EffectivePlan(context.Background(), f.productID)
	if err != nil || resolved.Plan == nil || !resolved.Plan.Archived {
		t.Fatal(resolved, err)
	}
	unused := f.request(t, "POST", "/api/creative/plans", core.Object{"name": "未使用"}, 201)["plan"].(map[string]any)
	unusedPath := fmt.Sprintf("/api/creative/plans/%d", int64(number(unused["id"])))
	if f.request(t, "DELETE", unusedPath, nil, 200)["deleted"] != true {
		t.Fatal("unused plan not deleted")
	}
	f.request(t, "GET", unusedPath, nil, 404)
	f.request(t, "PATCH", path, core.Object{"scene_id": 99999}, 200)
	resolved, err = f.a.EffectivePlan(context.Background(), f.productID)
	if err != nil || resolved.Effective.SceneID != nil {
		t.Fatal(resolved, err)
	}
}

func TestGlobalModelInCreativeShopSettings(t *testing.T) {
	f := newFixture(t)
	created := f.request(t, "POST", "/api/models", core.Object{"name": "共用模特"}, 201)["model"].(map[string]any)
	mid := int64(number(created["id"]))
	f.request(t, "PATCH", "/api/creative/shops/1/settings", core.Object{"models": core.Object{"female_main": mid}}, 200)
	f.exec(t, `INSERT INTO shops(name,platform,language,created_at) VALUES('别的店','taobao','zh-CN',?)`, core.Now())
	var other int64
	if err := f.c.DB.QueryRow("SELECT max(id) FROM shops").Scan(&other); err != nil {
		t.Fatal(err)
	}
	f.request(t, "PATCH", fmt.Sprintf("/api/models/%d", mid), core.Object{"shop_id": other}, 200)
	f.request(t, "PATCH", "/api/creative/shops/1/settings", core.Object{"models": core.Object{"female_main": mid}}, 404)
}
