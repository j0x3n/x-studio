package products

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
)

func TestViewsSeedAndPatch(t *testing.T) {
	f := newCategoryFixture(t)
	body := `{"key":"pearl","name":"珍珠","views":[{"key":"front","name":"正面","code":"A1","groups":["small"],"required":["small"],"hint":"原始"}]}`
	writeCategoryFile(t, f.c, "pearl.json", body)
	a := New(f.c)
	if err := a.AfterMigrate(); err != nil {
		t.Fatal(err)
	}
	p, err := f.c.CategoryByKey("pearl")
	if err != nil || len(p.Views) != 1 || p.Views[0].Hint != "原始" {
		t.Fatal(p, err)
	}
	path := fmt.Sprintf("/api/categories/%d", p.ID)
	view := core.Object{"key": "front", "name": "用户正面", "code": "A1", "groups": []string{"small"}, "required": []string{"small"}, "hint": "用户修改"}
	f.req(t, "PATCH", path, core.Object{"views": []core.Object{view}}, 200)
	if err = a.AfterMigrate(); err != nil {
		t.Fatal(err)
	}
	p, err = f.c.CategoryByKey("pearl")
	if err != nil || p.Views[0].Hint != "用户修改" {
		t.Fatal(p, err)
	}
	f.req(t, "PATCH", path, core.Object{"views": []core.Object{view, view}}, 400)
	view["code"] = "Z9"
	f.req(t, "PATCH", path, core.Object{"views": []core.Object{view}}, 400)
	view["code"] = "A1"
	view["required"] = []string{"strand"}
	f.req(t, "PATCH", path, core.Object{"views": []core.Object{view}}, 400)
	items := f.req(t, "GET", "/api/categories", nil, 200)["items"].([]any)
	if len(items[0].(map[string]any)["views"].([]any)) != 1 {
		t.Fatal(items)
	}
}

func TestViewsRepositorySeeds(t *testing.T) {
	c := categoryCore(t)
	c.Cfg.TemplateDir = filepath.Join("..", "..", "..", "templates")
	for _, name := range []string{"pearl", "crystal"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "prompts", "品类", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		writeCategoryFile(t, c, name+".json", string(data))
	}
	if err := c.Start(accounts.New(c), New(c), assets.New(c)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"pearl", "crystal"} {
		category, err := c.CategoryByKey(key)
		if err != nil || len(category.Views) != 9 {
			t.Fatal(key, category, err)
		}
		if category.Views[0].Code != "A1" || category.Views[1].Code != "A1" || category.Views[2].Code != "A2" || category.Views[8].Code != "F2" {
			t.Fatal(category.Views)
		}
	}
}
