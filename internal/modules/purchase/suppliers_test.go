package purchase

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/inventory"
	"xstudio/internal/modules/products"
)

type object = map[string]any

type fixture struct {
	c      *core.Core
	m      *Module
	s      *httptest.Server
	client *http.Client
	dir    string
}

var (
	hashOnce sync.Once
	hashPass []byte
)

func passwordHash() []byte {
	hashOnce.Do(func() {
		var err error
		hashPass, err = bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
		if err != nil {
			panic(err)
		}
	})
	return hashPass
}

func boot(t *testing.T, dir string, withPurchase bool) *fixture {
	t.Helper()
	if dir == "" {
		dir = t.TempDir()
	}
	cfg := core.Config{
		DataDir:       filepath.Join(dir, "data"),
		PromptDir:     filepath.Join(dir, "prompts"),
		TemplateDir:   filepath.Join(dir, "templates"),
		AdminUser:     "admin",
		AdminPassword: "password123",
		SessionSecret: strings.Repeat("s", 32),
		Workers:       1,
	}
	if err := os.MkdirAll(cfg.PromptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	c, err := core.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := New(c)
	mods := []core.Module{accounts.New(c), products.New(c)}
	if withPurchase {
		mods = append(mods, inventory.New(c), m)
	}
	if err = c.Start(mods...); err != nil {
		c.Close()
		t.Fatal(err)
	}
	s := httptest.NewServer(c.Handler())
	jar, _ := cookiejar.New(nil)
	f := &fixture{c: c, m: m, s: s, client: &http.Client{Jar: jar}, dir: dir}
	t.Cleanup(f.close)
	return f
}

func (f *fixture) close() {
	if f.s != nil {
		f.s.Close()
		f.s = nil
	}
	if f.c != nil {
		_ = f.c.Close()
		f.c = nil
	}
}

func (f *fixture) req(t *testing.T, method, path string, body any, status int) object {
	t.Helper()
	b := f.raw(t, method, path, body, status)
	if len(b) == 0 {
		return object{}
	}
	var out object
	if json.Unmarshal(b, &out) != nil {
		t.Fatalf("不是对象：%s", b)
	}
	return out
}

func (f *fixture) raw(t *testing.T, method, path string, body any, status int) []byte {
	t.Helper()
	var input io.Reader
	if body != nil {
		input = strings.NewReader(core.JS(body))
	}
	req, err := http.NewRequest(method, f.s.URL+path, input)
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
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != status {
		t.Fatalf("%s %s：得到 %d，想要 %d：%s", method, path, res.StatusCode, status, b)
	}
	return b
}

func (f *fixture) login(t *testing.T, name string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	f.client.Jar = jar
	f.req(t, "POST", "/api/login", object{"username": name, "password": "password123"}, 200)
}

func (f *fixture) exec(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	res, err := f.c.DB.Exec(q, args...)
	if err != nil {
		t.Fatalf("%s：%v", q, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (f *fixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := f.c.DB.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) user(t *testing.T, username, display, role string) int64 {
	t.Helper()
	return f.exec(t, "INSERT INTO users(username,password_hash,display_name,role_id,created_at) SELECT ?,?,?,id,? FROM roles WHERE key=?", username, passwordHash(), display, core.Now(), role)
}

func num(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}

func TestSupplierPermissions(t *testing.T) {
	f := boot(t, "", true)
	f.user(t, "op", "运营", "operator")          // has purchase.manage & inventory.view
	f.user(t, "fin", "财务", "finance")          // has inventory.view, no purchase.manage
	f.user(t, "des", "美工", "designer")         // neither purchase.manage nor inventory.view
	f.user(t, "collab", "协作者", "collaborator") // external collaborator (scope=shares)

	// Custom shares role that accidentally has inventory.view & purchase.manage must still get 403
	f.exec(t, "INSERT INTO roles(key,name,builtin,scope,perms,created_at) VALUES('shares_plus','外协扩展',0,'shares',?,?)",
		core.JS([]string{core.InventoryView, core.PurchaseManage}), core.Now())
	f.user(t, "collab_plus", "外协扩展", "shares_plus")

	f.login(t, "op")
	created := f.req(t, "POST", "/api/suppliers", object{"name": "诸暨珍珠厂"}, 201)["supplier"].(object)
	sid := num(created["id"])

	// Only inventory.view: can list and get, cannot create or patch (403)
	f.login(t, "fin")
	list := f.req(t, "GET", "/api/suppliers", nil, 200)
	if num(list["total"]) != 1 {
		t.Fatalf("财务应能看到 1 条供应商，实际 %v", list)
	}
	f.req(t, "GET", fmt.Sprintf("/api/suppliers/%d", sid), nil, 200)
	f.req(t, "POST", "/api/suppliers", object{"name": "新供应商"}, 403)
	f.req(t, "PATCH", fmt.Sprintf("/api/suppliers/%d", sid), object{"contact": "李四"}, 403)

	// Neither purchase.manage nor inventory.view: 403 on all
	f.login(t, "des")
	f.req(t, "GET", "/api/suppliers", nil, 403)
	f.req(t, "GET", fmt.Sprintf("/api/suppliers/%d", sid), nil, 403)
	f.req(t, "POST", "/api/suppliers", object{"name": "新供应商"}, 403)
	f.req(t, "PATCH", fmt.Sprintf("/api/suppliers/%d", sid), object{"contact": "李四"}, 403)

	// External collaborators: 403 on all
	for _, u := range []string{"collab", "collab_plus"} {
		f.login(t, u)
		f.req(t, "GET", "/api/suppliers", nil, 403)
		f.req(t, "GET", fmt.Sprintf("/api/suppliers/%d", sid), nil, 403)
		f.req(t, "POST", "/api/suppliers", object{"name": "新供应商"}, 403)
		f.req(t, "PATCH", fmt.Sprintf("/api/suppliers/%d", sid), object{"contact": "李四"}, 403)
	}
}

func TestSupplierCRUDValidationArchiveAndAudit(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")

	// Create supplier with trimmed name and defaults
	res1 := f.req(t, "POST", "/api/suppliers", object{
		"name":     "  山下湖 · 老陈  ",
		"contact":  "陈老板",
		"phone":    "13800001111",
		"products": "淡水散珠、巴洛克",
	}, 201)
	s1 := res1["supplier"].(object)
	id1 := num(s1["id"])
	if s1["name"] != "山下湖 · 老陈" || s1["settlement"] != "cash" || num(s1["payment_days"]) != 0 || s1["archived"] != false {
		t.Fatalf("新建默认值或去空格不正确：%v", s1)
	}

	// Create second supplier with monthly settlement
	res2 := f.req(t, "POST", "/api/suppliers", object{
		"name":         "东海 · 小林水晶",
		"contact":      "小林",
		"wechat":       "lin_crystal",
		"address":      "连云港东海县水晶城",
		"products":     "白水晶、紫水晶原石",
		"settlement":   "monthly",
		"payment_days": 30,
		"note":         "老合作商，品质稳定",
	}, 201)
	s2 := res2["supplier"].(object)
	id2 := num(s2["id"])
	if s2["settlement"] != "monthly" || num(s2["payment_days"]) != 30 {
		t.Fatalf("新建月结供应商不正确：%v", s2)
	}

	// Duplicate name on POST -> 409 "已有同名供应商"
	dup := f.req(t, "POST", "/api/suppliers", object{"name": " 山下湖 · 老陈 "}, 409)
	if dup["error"] != "已有同名供应商" {
		t.Fatalf("重名错误消息不正确：%v", dup)
	}

	// Duplicate name on PATCH -> 409 "已有同名供应商"
	dupPatch := f.req(t, "PATCH", fmt.Sprintf("/api/suppliers/%d", id2), object{"name": "山下湖 · 老陈"}, 409)
	if dupPatch["error"] != "已有同名供应商" {
		t.Fatalf("修改重名错误消息不正确：%v", dupPatch)
	}

	// Validation: settlement invalid -> 400
	f.req(t, "POST", "/api/suppliers", object{"name": "测试A", "settlement": "weekly"}, 400)
	f.req(t, "PATCH", fmt.Sprintf("/api/suppliers/%d", id1), object{"settlement": "invalid"}, 400)
	f.req(t, "PATCH", fmt.Sprintf("/api/suppliers/%d", id1), object{"settlement": ""}, 400)

	// Validation: name length 1-50, payment_days 0-365, other text <= 200, note <= 1000
	f.req(t, "POST", "/api/suppliers", object{"name": "   "}, 400)
	f.req(t, "POST", "/api/suppliers", object{"name": strings.Repeat("甲", 51)}, 400)
	f.req(t, "POST", "/api/suppliers", object{"name": "测试B", "payment_days": -1}, 400)
	f.req(t, "POST", "/api/suppliers", object{"name": "测试B", "payment_days": 366}, 400)
	f.req(t, "POST", "/api/suppliers", object{"name": "测试B", "contact": strings.Repeat("a", 201)}, 400)
	f.req(t, "POST", "/api/suppliers", object{"name": "测试B", "products": strings.Repeat("珠", 201)}, 400)
	f.req(t, "POST", "/api/suppliers", object{"name": "测试B", "note": strings.Repeat("备", 1001)}, 400)

	// Partial update via PATCH preserves unmodified fields
	patched := f.req(t, "PATCH", fmt.Sprintf("/api/suppliers/%d", id1), object{
		"phone":        "13999998888",
		"settlement":   "monthly",
		"payment_days": 15,
	}, 200)["supplier"].(object)
	if patched["name"] != "山下湖 · 老陈" || patched["contact"] != "陈老板" || patched["phone"] != "13999998888" || patched["settlement"] != "monthly" || num(patched["payment_days"]) != 15 {
		t.Fatalf("部分更新结果不正确：%v", patched)
	}

	// GET detail and 404
	got := f.req(t, "GET", fmt.Sprintf("/api/suppliers/%d", id1), nil, 200)["supplier"].(object)
	if got["phone"] != "13999998888" {
		t.Fatalf("详情返回不正确：%v", got)
	}
	f.req(t, "GET", "/api/suppliers/9999", nil, 404)
	f.req(t, "PATCH", "/api/suppliers/9999", object{"note": "x"}, 404)

	// Search by q (name, contact, products)
	byContact := f.req(t, "GET", "/api/suppliers?q=陈老板", nil, 200)
	if num(byContact["total"]) != 1 {
		t.Fatalf("按联系人搜索失败：%v", byContact)
	}
	byProducts := f.req(t, "GET", "/api/suppliers?q=紫水晶", nil, 200)
	if num(byProducts["total"]) != 1 {
		t.Fatalf("按主营搜索失败：%v", byProducts)
	}

	// Archive hides from default list, visible with archived=1
	f.req(t, "PATCH", fmt.Sprintf("/api/suppliers/%d", id1), object{"archived": true}, 200)
	defList := f.req(t, "GET", "/api/suppliers", nil, 200)
	if num(defList["total"]) != 1 {
		t.Fatalf("归档后默认列表应只有 1 条，实际：%v", defList)
	}
	allList := f.req(t, "GET", "/api/suppliers?archived=1", nil, 200)
	if num(allList["total"]) != 2 {
		t.Fatalf("archived=1 应能看到归档供应商，实际：%v", allList)
	}

	// Audit log check
	if n := f.count(t, "SELECT count(*) FROM audit_log WHERE action='supplier.create' AND target_type='supplier'"); n != 2 {
		t.Fatalf("supplier.create 审计日志条数不对：得到 %d，想要 2", n)
	}
	if n := f.count(t, "SELECT count(*) FROM audit_log WHERE action='supplier.update' AND target_type='supplier' AND target_id=?", id1); n != 2 {
		t.Fatalf("supplier.update 审计日志条数不对：得到 %d，想要 2", n)
	}
}

func TestMigrationImportsSuppliersFromProducts(t *testing.T) {
	dir := t.TempDir()

	// Step 1: boot without purchase module and seed 3 products
	f1 := boot(t, dir, false)
	now := core.Now()
	f1.exec(t, "INSERT INTO products(name,created_by,created_at,updated_at,supplier) VALUES(?,1,?,?,?)",
		"商品1", now, now, `{"name":"诸暨 · 阿强"}`)
	f1.exec(t, "INSERT INTO products(name,created_by,created_at,updated_at,supplier) VALUES(?,1,?,?,?)",
		"商品2", now, now, `{"name":" 诸暨 · 阿强 "}`)
	f1.exec(t, "INSERT INTO products(name,created_by,created_at,updated_at,supplier) VALUES(?,1,?,?,?)",
		"商品3", now, now, `{}`)
	f1.close()

	// Step 2: boot with purchase module on the same data directory
	f2 := boot(t, dir, true)
	f2.login(t, "admin")
	res := f2.req(t, "GET", "/api/suppliers", nil, 200)
	if num(res["total"]) != 1 {
		t.Fatalf("迁移后应只导入 1 条供应商，实际：%v", res)
	}
	items := res["items"].([]any)
	sup := items[0].(object)
	if sup["name"] != "诸暨 · 阿强" || sup["settlement"] != "cash" || num(sup["payment_days"]) != 0 || sup["archived"] != false {
		t.Fatalf("迁移导入的供应商字段不正确：%v", sup)
	}
	// Ensure products table was not modified
	if n := f2.count(t, "SELECT count(*) FROM products"); n != 3 {
		t.Fatalf("products 表不应被修改，条数：%d", n)
	}
}
