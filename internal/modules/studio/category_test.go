package studio_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"xstudio/internal/core"
)

func TestCategoryTemplateFilterFallbackOrderAndReject(t *testing.T) {
	env := newTestEnv(t, func(c *core.Config) {
		dir := filepath.Join(c.TemplateDir, "详情页模板")
		templates := []Object{}
		for _, key := range []string{"chosen", "shop", "category", "manifest", "first", "pearl"} {
			categories := []string{"crystal"}
			if key == "pearl" {
				categories = []string{"pearl"}
			}
			templates = append(templates, Object{"key": key, "name": key + "模板", "file": key + ".html", "categories": categories})
			if e := os.WriteFile(filepath.Join(dir, key+".html"), []byte("<!doctype html><html>测试</html>"), 0600); e != nil {
				t.Fatal(e)
			}
		}
		templates = append(templates, Object{"key": "universal", "name": "通用", "file": "模板.html"})
		if e := os.WriteFile(filepath.Join(dir, "模板清单.json"), []byte(core.JS(Object{"default": "manifest", "templates": templates})), 0600); e != nil {
			t.Fatal(e)
		}
	})
	env.login(t, "admin", "AdminPassword123!")
	env.req(t, "POST", "/api/categories", Object{"key": "crystal", "name": "水晶", "default_template": "category"}, 201)
	p := env.req(t, "POST", "/api/products", Object{"name": "水晶", "category_id": 2, "shop_ids": []int64{1}}, 201)["product"].(Object)
	lid := int64(p["listings"].([]any)[0].(Object)["id"].(float64))
	for key, want := range map[string]int{"crystal": 6, "pearl": 2} {
		items := env.req(t, "GET", "/api/templates?category="+key, nil, 200)["detail"].([]any)
		if len(items) != want {
			t.Fatal(key, items)
		}
		for _, item := range items {
			if _, ok := item.(Object)["categories"]; !ok {
				t.Fatal(item)
			}
		}
	}
	path := fmt.Sprintf("/api/listings/%d/render", lid)
	out := env.req(t, "PUT", path, Object{"detail_template": "pearl"}, 400)
	if out["error"] != "pearl模板不适用于水晶" {
		t.Fatal(out)
	}
	env.req(t, "PUT", path, Object{"detail_template": "chosen"}, 200)
	cases := []struct {
		name, chosen, shop, category string
		disabled                     []string
		want, note                   string
	}{
		{"listing", "chosen", "shop", "category", nil, "chosen", ""},
		{"shop", "pearl", "shop", "category", nil, "shop", "不适用于水晶"},
		{"category", "missing", "shop", "category", []string{"shop"}, "category", "不存在"},
		{"manifest", "chosen", "shop", "category", []string{"chosen", "shop", "category"}, "manifest", "已禁用"},
		{"first", "chosen", "shop", "category", []string{"chosen", "shop", "category", "manifest"}, "first", "已禁用"},
		{"universal", "chosen", "shop", "category", []string{"chosen", "shop", "category", "manifest", "first"}, "universal", "已禁用"},
		{"emergency", "chosen", "shop", "category", []string{"chosen", "shop", "category", "manifest", "first", "universal"}, "manifest", "没有适用于水晶的详情页模板，先用了manifest模板"},
		{"empty_shop", "", "", "category", nil, "category", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := env.c.DB.Exec("UPDATE listing_render SET detail_template=? WHERE listing_id=?", tc.chosen, lid); e != nil {
				t.Fatal(e)
			}
			if _, e := env.c.DB.Exec("INSERT INTO template_prefs(shop_id,default_detail,disabled,updated_at) VALUES(1,?,?,?) ON CONFLICT(shop_id) DO UPDATE SET default_detail=excluded.default_detail,disabled=excluded.disabled", tc.shop, core.JS(tc.disabled), core.Now()); e != nil {
				t.Fatal(e)
			}
			if _, e := env.c.DB.Exec("UPDATE categories SET default_template=? WHERE id=2", tc.category); e != nil {
				t.Fatal(e)
			}
			file, key, note, e := env.c.ResolveDetailTemplate(lid, 1)
			if e != nil || key != tc.want || file != tc.want+".html" && tc.want != "universal" {
				t.Fatal(file, key, note, e)
			}
			if tc.note == "" && note != "" || tc.note != "" && !strings.Contains(note, tc.note) {
				t.Fatal(note)
			}
			state := env.req(t, "GET", path, nil, 200)
			if state["settings"].(Object)["effective_detail_template"] != tc.want || !strings.Contains(state["preview_url"].(string), file) {
				t.Fatal(state)
			}
		})
	}
	// A manifest entry whose HTML disappeared cannot win the selection.
	if e := os.Remove(filepath.Join(env.c.Cfg.TemplateDir, "详情页模板", "chosen.html")); e != nil {
		t.Fatal(e)
	}
	if _, e := env.c.DB.Exec("UPDATE listing_render SET detail_template='chosen' WHERE listing_id=?; UPDATE template_prefs SET default_detail='shop',disabled='[]' WHERE shop_id=1", lid); e != nil {
		t.Fatal(e)
	}
	_, key, note, e := env.c.ResolveDetailTemplate(lid, 1)
	if e != nil || key != "shop" || !strings.Contains(note, "不存在") {
		t.Fatal(key, note, e)
	}
}
