package products

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
)

const testSlots = `{"说明":"旧槽位","主图":[{"位":1,"名":"首图","候选":["A1"],"必需":true}],"详情页":[]}`

func categoryCore(t *testing.T) *core.Core {
	t.Helper()
	root := t.TempDir()
	c, e := core.New(core.Config{DataDir: filepath.Join(root, "data"), PromptDir: filepath.Join(root, "prompts"), TemplateDir: filepath.Join(root, "templates"), AdminUser: "admin", AdminPassword: "password123", SessionSecret: strings.Repeat("s", 32)}, fstest.MapFS{"index.html": {Data: []byte("ok")}}, []byte(testSlots))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

type v1Products struct{ m *Module }

func (v v1Products) Name() string                     { return "products" }
func (v v1Products) Migrations() []core.Migration     { return v.m.Migrations()[:1] }
func (v v1Products) Routes(*core.Router)              {}
func (v v1Products) Jobs() map[string]core.JobHandler { return nil }
func TestCategoryMigrationFromV1(t *testing.T) {
	c := categoryCore(t)
	m := New(c)
	if e := c.Migrate([]core.Module{accounts.New(c), v1Products{m}, assets.New(c)}); e != nil {
		t.Fatal(e)
	}
	if _, e := c.DB.Exec(`INSERT INTO users(id,username,password_hash,role_id,created_at) VALUES(1,'old','hash',1,'old'); INSERT INTO products(name,created_by,created_at,updated_at,info) VALUES('a',1,'old','old','{"category":"耳钉","extra":1}'),('b',1,'old','old','{"category":"耳夹","style":"项链"}'),('c',1,'old','old','{}')`); e != nil {
		t.Fatal(e)
	}
	if e := c.Migrate([]core.Module{accounts.New(c), m, assets.New(c)}); e != nil {
		t.Fatal(e)
	}
	pearl, e := c.CategoryByKey("pearl")
	if e != nil {
		t.Fatal(e)
	}
	if string(pearl.Slots) != testSlots {
		t.Fatal(string(pearl.Slots))
	}
	rows, e := core.QueryObjects(c.DB, "SELECT category_id,info FROM products ORDER BY id")
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range rows {
		if p["category_id"] != pearl.ID {
			t.Fatal(p)
		}
	}
	first := rows[0]["info"].(map[string]any)
	if first["style"] != "耳钉" || first["category"] != nil || first["extra"] != float64(1) {
		t.Fatal(first)
	}
	second := rows[1]["info"].(map[string]any)
	if second["style"] != "项链" || second["category"] != "耳夹" {
		t.Fatal(second)
	}
	expected := map[string][]string{"A": {"一对平放正面", "一对立起 45°", "每个颜色或规格"}, "B": {"珍珠表面生长纹", "珍珠光泽和反光", "配件做工", "925 印记", "耳针或耳夹结构", "珍珠微瑕"}, "C": {"钢尺参照", "一元硬币参照", "指尖或手心", "同一只耳朵各尺寸上耳"}, "D": {"侧脸正对耳朵", "微侧 45°", "正脸半脸", "生活感上耳", "耳饰配颈链"}, "E": {"深蓝丝绒", "米白亚麻布", "大理石纹场景", "打开的包装盒", "手托托盘批量实拍（不校色）"}, "F": {"礼盒和品牌卡", "打开的礼盒", "打包好的快递"}}
	n := 0
	for _, code := range pearl.Codes {
		group := code.Code[:1]
		var i int
		fmt.Sscanf(code.Code[1:], "%d", &i)
		if i < 1 || i > len(expected[group]) || expected[group][i-1] != code.Name || code.Desc == "" {
			t.Fatal(code)
		}
		if (code.Code == "A3" || code.Code == "C4") && code.Suffix == "" {
			t.Fatal(code)
		}
		n++
	}
	if n != 26 {
		t.Fatal(n)
	}
	if e := c.Migrate([]core.Module{accounts.New(c), m, assets.New(c)}); e != nil {
		t.Fatal(e)
	}
}
func writeCategoryFile(t *testing.T, c *core.Core, name, body string) {
	t.Helper()
	dir := filepath.Join(c.Cfg.PromptDir, "品类")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestCategorySeedsFillOnlyEmptyAndSkipInvalid(t *testing.T) {
	c := categoryCore(t)
	m := New(c)
	templateDir := filepath.Join(c.Cfg.TemplateDir, "详情页模板")
	if e := os.MkdirAll(templateDir, 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"navy", "custom"} {
		if e := os.WriteFile(filepath.Join(templateDir, name+".html"), []byte("<html></html>"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(templateDir, "模板清单.json"), []byte(`{"templates":[{"key":"navy","name":"蓝","file":"navy.html"},{"key":"custom","name":"自定义","file":"custom.html"}]}`), 0600); e != nil {
		t.Fatal(e)
	}
	writeCategoryFile(t, c, "bad.json", `{`)
	writeCategoryFile(t, c, "invalid.json", `{"key":"broken","name":"坏","slots":[]}`)
	writeCategoryFile(t, c, "pearl.json", `{"key":"pearl","name":"不覆盖","sort":-10,"disabled":true,"attrs":[{"key":"style","label":"款式","type":"text"}],"codes":[{"code":"A1","name":"种子编号"}],"rules":"初始规则","default_template":"navy","banned":["禁词"],"slots":{"主图":[],"详情页":[]}}`)
	writeCategoryFile(t, c, "crystal.json", `{"key":"crystal","name":"水晶","rules":"水晶规则","codes":[{"code":"X1","name":"晶体"}],"slots":{"主图":[{"位":1,"名":"水晶","候选":["X1"]}],"详情页":[]}}`)
	if e := c.Start(accounts.New(c), m, assets.New(c)); e != nil {
		t.Fatal(e)
	}
	p, e := c.CategoryByKey("pearl")
	if e != nil {
		t.Fatal(e)
	}
	if p.Name != "珍珠" || p.Sort != 10 || p.Disabled || p.Rules != "初始规则" || p.DefaultTemplate != "navy" || len(p.Attrs) != 1 || len(p.Codes) != 26 || string(p.Slots) != testSlots {
		t.Fatal(p)
	}
	if _, e = c.CategoryByKey("crystal"); e != nil {
		t.Fatal(e)
	}
	if _, e = c.CategoryByKey("broken"); e != sql.ErrNoRows {
		t.Fatal(e)
	}
	if _, e = c.DB.Exec(`UPDATE categories SET default_template='custom',rules='管理员规则',attrs='[]',banned='[]',slots='{}',codes='[]' WHERE key='pearl'`); e != nil {
		t.Fatal(e)
	}
	if e = m.AfterMigrate(); e != nil {
		t.Fatal(e)
	}
	p, _ = c.CategoryByKey("pearl")
	if p.Rules != "管理员规则" || p.DefaultTemplate != "custom" || len(p.Attrs) != 1 || len(p.Banned) != 1 || len(p.Codes) != 1 || string(p.Slots) != `{"主图":[],"详情页":[]}` {
		t.Fatal(p)
	}
	if e = m.AfterMigrate(); e != nil {
		t.Fatal(e)
	}
	items, e := c.Categories(true)
	if e != nil || len(items) != 2 {
		t.Fatal(items, e)
	}
	if e = c.Close(); e != nil {
		t.Fatal(e)
	}
	restarted, e := core.New(c.Cfg, nil, []byte(testSlots))
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	if e = restarted.Start(accounts.New(restarted), New(restarted), assets.New(restarted)); e != nil {
		t.Fatal(e)
	}
	p, e = restarted.CategoryByKey("pearl")
	if e != nil || p.Rules != "管理员规则" || p.DefaultTemplate != "custom" {
		t.Fatal(p, e)
	}

}

type categoryFixture struct {
	c      *core.Core
	s      *httptest.Server
	client *http.Client
}

func newCategoryFixture(t *testing.T) *categoryFixture {
	t.Helper()
	c := categoryCore(t)
	if e := c.Start(accounts.New(c), New(c), assets.New(c)); e != nil {
		t.Fatal(e)
	}
	s := httptest.NewServer(c.Handler())
	t.Cleanup(s.Close)
	jar, _ := cookiejar.New(nil)
	f := &categoryFixture{c, s, &http.Client{Jar: jar}}
	f.req(t, "POST", "/api/login", core.Object{"username": "admin", "password": "password123"}, 200)
	return f
}
func (f *categoryFixture) req(t *testing.T, method, path string, body any, status int) core.Object {
	t.Helper()
	r, e := http.NewRequest(method, f.s.URL+path, strings.NewReader(core.JS(body)))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("X-Requested-With", "fetch")
	r.Header.Set("Content-Type", "application/json")
	res, e := f.client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var out core.Object
	if e = json.NewDecoder(res.Body).Decode(&out); e != nil {
		t.Fatal(e)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s status %d want %d: %v", method, path, res.StatusCode, status, out)
	}
	return out
}
func TestCategoryPermissionsValidationCopyAndAudit(t *testing.T) {
	f := newCategoryFixture(t)
	for _, body := range []core.Object{
		{"key": "X", "name": "错"}, {"key": "wrong", "name": ""}, {"key": "wrong", "name": strings.Repeat("长", 21)},
		{"key": "wrong", "name": "错", "attrs": core.Object{}}, {"key": "wrong", "name": "错", "attrs": []any{core.Object{"key": "aa", "label": "字段", "type": "unknown"}}},
		{"key": "wrong", "name": "错", "attrs": []any{core.Object{"key": "aa", "label": "字段", "type": "select"}}},
		{"key": "wrong", "name": "错", "attrs": []any{core.Object{"key": "aa", "label": "a", "type": "text"}, core.Object{"key": "aa", "label": "b", "type": "text"}}},
		{"key": "wrong", "name": "错", "codes": []any{core.Object{"code": "a1"}}}, {"key": "wrong", "name": "错", "codes": []any{core.Object{"code": "A1"}, core.Object{"code": "A1"}}},
		{"key": "wrong", "name": "错", "codes": core.Object{}}, {"key": "wrong", "name": "错", "slots": []any{}},
		{"key": "wrong", "name": "错", "slots": core.Object{"主图": "bad"}}, {"key": "wrong", "name": "错", "slots": core.Object{"主图": []any{core.Object{"名": "首图", "候选": []string{"Z9"}}}}},
		{"key": "wrong", "name": "错", "default_template": "missing"}, {"key": "wrong", "name": "错", "rules": strings.Repeat("长", 20001)},
	} {
		f.req(t, "POST", "/api/categories", body, 400)
	}
	for _, body := range []core.Object{{"disabled": true}, {"key": "newkey"}, {"slots": nil}} {
		f.req(t, "PATCH", "/api/categories/1", body, 400)
	}
	f.req(t, "PATCH", "/api/categories/1", core.Object{"rules": "管理员规则"}, 200)
	copied := f.req(t, "POST", "/api/categories", core.Object{"key": "crystal", "name": "水晶", "copy_from": "pearl", "sort": 0}, 201)["category"].(map[string]any)
	if copied["rules"] != "管理员规则" || len(copied["codes"].([]any)) != 26 {
		t.Fatal(copied)
	}
	f.req(t, "PATCH", "/api/categories/1", core.Object{"disabled": true}, 200)
	if got := f.req(t, "GET", "/api/categories", nil, 200)["items"].([]any); len(got) != 1 {
		t.Fatal(got)
	}
	if got := f.req(t, "GET", "/api/categories?all=1", nil, 200)["items"].([]any); len(got) != 2 {
		t.Fatal(got)
	}
	f.req(t, "PATCH", "/api/categories/2", core.Object{"disabled": true}, 400)
	var n int
	_ = f.c.DB.QueryRow("SELECT count(*) FROM audit_log WHERE action IN ('category.create','category.update') AND json_array_length(json_extract(detail,'$.fields'))>0").Scan(&n)
	if n != 3 {
		t.Fatal(n)
	}
	f.req(t, "POST", "/api/users", core.Object{"username": "operator", "password": "password123", "role_id": 2, "shop_ids": []int64{1}}, 201)
	f.req(t, "POST", "/api/login", core.Object{"username": "operator", "password": "password123"}, 200)
	f.req(t, "GET", "/api/categories", nil, 200)
	f.req(t, "POST", "/api/categories", core.Object{"key": "blocked", "name": "禁止"}, 403)
	f.req(t, "PATCH", "/api/categories/2", core.Object{"name": "禁止"}, 403)
	f.client.Jar, _ = cookiejar.New(nil)
	f.req(t, "GET", "/api/categories", nil, 401)
}
func TestCategoryProductsDefaultFilterInfoAndSwitch(t *testing.T) {
	f := newCategoryFixture(t)
	attrs := []core.CategoryAttr{{Key: "title", Label: "标题", Type: "text", Required: true}, {Key: "story", Label: "描述", Type: "textarea"}, {Key: "price", Label: "价格", Type: "number"}, {Key: "style", Label: "款式", Type: "select", Options: []string{"手链"}}, {Key: "colors", Label: "颜色", Type: "multi", Options: []string{"白"}}, {Key: "ready", Label: "就绪", Type: "bool"}, {Key: "custom", Label: "自选", Type: "select", Options: []string{"一"}, Free: true}}
	f.req(t, "POST", "/api/categories", core.Object{"key": "crystal", "name": "水晶", "sort": -1, "attrs": attrs}, 201)
	p := f.req(t, "POST", "/api/products", core.Object{"name": "水晶", "info": core.Object{"unknown": []any{"保持"}, "title": "", "price": nil, "custom": "其他", "colors": []any{"白"}}, "shop_ids": []int64{1}}, 201)["product"].(map[string]any)
	if p["category_id"] != float64(2) || p["category"].(map[string]any)["name"] != "水晶" {
		t.Fatal(p)
	}
	pid := int64(p["id"].(float64))
	path := fmt.Sprintf("/api/products/%d", pid)
	if !reflect.DeepEqual(p["info"].(map[string]any)["unknown"], []any{"保持"}) {
		t.Fatal(p)
	}
	for _, info := range []core.Object{{"price": "10"}, {"ready": 1}, {"style": "项链"}, {"colors": []string{"黑"}}, {"title": strings.Repeat("长", 201)}, {"story": strings.Repeat("长", 2001)}} {
		f.req(t, "POST", "/api/products", core.Object{"name": "错误", "category_id": 2, "info": info}, 400)
		f.req(t, "PATCH", path, core.Object{"info": info}, 400)
	}
	f.req(t, "PATCH", path, core.Object{"info": core.Object{"title": nil, "price": "", "style": "", "ready": false, "story": "描述", "price2": "保留"}}, 200)
	old := f.req(t, "POST", "/api/products", core.Object{"name": "珍珠", "category_id": 1, "info": core.Object{"style": "耳钉"}}, 201)["product"].(map[string]any)
	switched := f.req(t, "PATCH", fmt.Sprintf("/api/products/%.0f", old["id"].(float64)), core.Object{"category_id": 2}, 200)["product"].(map[string]any)
	if switched["info"].(map[string]any)["style"] != "耳钉" {
		t.Fatal(switched)
	}
	f.req(t, "PATCH", fmt.Sprintf("/api/products/%.0f", old["id"].(float64)), core.Object{"category_id": 1}, 200)

	if rows := f.req(t, "GET", "/api/products?category=2", nil, 200)["items"].([]any); len(rows) != 1 {
		t.Fatal(rows)
	}
	var aid int64
	res, e := f.c.DB.Exec("INSERT INTO assets(product_id,file,orig_name,width,height,code,created_by,created_at) VALUES(?,'原图/a.png','a.png',1,1,'X9',1,'now')", pid)
	if e != nil {
		t.Fatal(e)
	}
	aid, _ = res.LastInsertId()
	f.req(t, "PATCH", path, core.Object{"category_id": 1}, 200)
	var code string
	_ = f.c.DB.QueryRow("SELECT code FROM assets WHERE id=?", aid).Scan(&code)
	if code != "X9" {
		t.Fatal(code)
	}
	f.req(t, "PATCH", "/api/categories/2", core.Object{"disabled": true}, 200)
	out := f.req(t, "POST", "/api/products", core.Object{"name": "停用", "category_id": 2}, 400)
	if out["error"] != "这个品类已停用" {
		t.Fatal(out)
	}
	f.req(t, "PATCH", path, core.Object{"category_id": 2}, 400)
	f.req(t, "POST", "/api/products", core.Object{"name": "回退默认"}, 201)
	f.req(t, "GET", path, nil, 200)
	f.req(t, "PATCH", path, core.Object{"category_id": 9999}, 400)
}
