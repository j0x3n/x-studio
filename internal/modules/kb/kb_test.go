package kb

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
	"time"

	"golang.org/x/crypto/bcrypt"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/products"
)

type object = map[string]any
type fixture struct {
	c      *core.Core
	m      *Module
	s      *httptest.Server
	client *http.Client
}

func boot(t *testing.T, configure ...func(*core.Config)) *fixture {
	t.Helper()
	dir := t.TempDir()
	cfg := core.Config{DataDir: filepath.Join(dir, "data"), PromptDir: filepath.Join(dir, "prompts"), TemplateDir: filepath.Join(dir, "templates"), AdminUser: "admin", AdminPassword: "password123", SessionSecret: strings.Repeat("s", 32), Workers: 1, AIKey: "test-secret-key", TextModel: "fake-model"}
	for _, fn := range configure {
		fn(&cfg)
	}
	if err := os.MkdirAll(cfg.PromptDir, 0700); err != nil {
		t.Fatal(err)
	}
	c, err := core.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := New(c)
	if err = c.Start(accounts.New(c), products.New(c), m); err != nil {
		c.Close()
		t.Fatal(err)
	}
	s := httptest.NewServer(c.Handler())
	jar, _ := cookiejar.New(nil)
	f := &fixture{c: c, m: m, s: s, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
	t.Cleanup(func() {
		s.Close()
		if f.c != nil {
			if err := f.c.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	f.login(t, "admin")
	return f
}
func (f *fixture) login(t *testing.T, name string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	f.client.Jar = jar
	f.req(t, "POST", "/api/login", object{"username": name, "password": "password123"}, 200)
}
func (f *fixture) response(t *testing.T, method, path string, body any, status int) []byte {
	t.Helper()
	var input io.Reader
	if body != nil {
		input = strings.NewReader(core.JS(body))
	}
	r, err := http.NewRequest(method, f.s.URL+path, input)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-Requested-With", "fetch")
	r.Header.Set("Content-Type", "application/json")
	res, err := f.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, res.StatusCode, status, b)
	}
	return b
}
func (f *fixture) req(t *testing.T, method, path string, body any, status int) object {
	t.Helper()
	b := f.response(t, method, path, body, status)
	var out object
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return out
}
func (f *fixture) array(t *testing.T, path string) []object {
	t.Helper()
	var out []object
	b := f.response(t, "GET", path, nil, 200)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func (f *fixture) exec(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	res, err := f.c.DB.Exec(q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
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
func (f *fixture) user(t *testing.T, name, scope string, perms ...string) int64 {
	t.Helper()
	rid := f.exec(t, "INSERT INTO roles(key,name,scope,perms,created_at) VALUES(?,?,?,?,?)", name, name, scope, core.JS(perms), core.Now())
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return f.exec(t, "INSERT INTO users(username,password_hash,display_name,role_id,created_at) VALUES(?,?,?,?,?)", name, hash, name, rid, core.Now())
}
func (f *fixture) article(t *testing.T, title, body string, extra object) object {
	t.Helper()
	in := object{"title": title, "body": body, "folder_id": 1}
	for k, v := range extra {
		in[k] = v
	}
	return f.req(t, "POST", "/api/kb/articles", in, 201)
}
func articlePath(v object) string { return fmt.Sprintf("/api/kb/articles/%.0f", v["id"]) }
func items(v object) []any        { return v["items"].([]any) }
func assertField(t *testing.T, v object, key string, want any) {
	t.Helper()
	if core.JS(v[key]) != core.JS(want) {
		t.Fatalf("%s: got %s want %s in %v", key, core.JS(v[key]), core.JS(want), v)
	}
}

func TestPermissions(t *testing.T) {
	f := boot(t)
	v := f.article(t, "权限", "正文", nil)
	p := articlePath(v)
	f.user(t, "none", "shops")
	f.user(t, "editOnly", "shops", core.KBEdit)
	f.user(t, "external", "shares", core.KBView, core.KBEdit, core.AdminSettings)
	f.user(t, "viewer", "shops", core.KBView)
	f.user(t, "editor", "shops", core.KBView, core.KBEdit)
	read := []string{"/api/kb/folders", "/api/kb/articles", p, p + "/versions/1", "/api/kb/tags", "/api/kb/search?q=权限", "/api/kb/trash", "/files/kb/2026-10/missing.png"}
	writes := []struct{ method, path string }{{"POST", "/api/kb/folders"}, {"PATCH", "/api/kb/folders/1"}, {"DELETE", "/api/kb/folders/1"}, {"POST", "/api/kb/articles"}, {"PATCH", p}, {"DELETE", p}, {"POST", p + "/restore"}, {"POST", p + "/revert"}, {"POST", "/api/kb/files"}, {"POST", "/api/kb/organize"}}
	for _, user := range []string{"none", "editOnly", "external"} {
		f.login(t, user)
		for _, path := range read {
			f.response(t, "GET", path, nil, 403)
		}
		for _, route := range writes {
			f.response(t, route.method, route.path, object{}, 403)
		}
	}
	f.login(t, "viewer")
	for _, path := range read[:6] {
		f.response(t, "GET", path, nil, 200)
	}
	for _, route := range writes {
		f.response(t, route.method, route.path, object{}, 403)
	}
	f.response(t, "GET", "/api/kb/trash", nil, 403)
	f.login(t, "editor")
	f.response(t, "GET", "/api/kb/trash", nil, 403)
	f.response(t, "POST", p+"/restore", nil, 403)
	jar, _ := cookiejar.New(nil)
	f.client.Jar = jar
	f.response(t, "GET", "/api/kb/articles", nil, 401)
	f.response(t, "GET", "/files/kb/test.png", nil, 401)
	f.login(t, "admin")
	r, _ := http.NewRequest("POST", f.s.URL+"/api/kb/articles", strings.NewReader(`{}`))
	res, err := f.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("missing CSRF header accepted")
	}
}

func TestArticleVersionsConflictAndRevert(t *testing.T) {
	f := boot(t)
	v := f.article(t, "旧标题", "# 旧正文", object{"tags": []string{"旧标签"}, "note": "第一版"})
	p := articlePath(v)
	assertField(t, v, "version", 1)
	if len(v["versions"].([]any)) != 1 {
		t.Fatal(v)
	}
	v = f.req(t, "PATCH", p, object{"base_version": 1, "body": "新正文", "note": "修改正文"}, 200)
	assertField(t, v, "version", 2)
	v = f.req(t, "PATCH", p, object{"base_version": 2, "pinned": true}, 200)
	assertField(t, v, "version", 2)
	v = f.req(t, "PATCH", p, object{"base_version": 2, "links": []object{{"type": "category", "id": 1}}, "summary": "摘要"}, 200)
	assertField(t, v, "version", 2)
	f.req(t, "PATCH", p, object{"body": "未带版本"}, 400)
	conf := f.req(t, "PATCH", p, object{"base_version": 1, "body": "覆盖"}, 409)
	assertField(t, conf, "version", 2)
	if !strings.Contains(conf["error"].(string), "admin") {
		t.Fatal(conf)
	}
	v = f.req(t, "POST", p+"/revert", object{"version": 1, "base_version": 2}, 200)
	assertField(t, v, "version", 3)
	assertField(t, v, "body", "# 旧正文")
	assertField(t, v, "tags", []string{"旧标签"})
	assertField(t, v, "pinned", true)
	vs := v["versions"].([]any)
	if len(vs) != 3 {
		t.Fatal(vs)
	}
	assertField(t, vs[0].(object), "note", "回退到第 1 版")
	if _, ok := vs[0].(object)["body"]; ok {
		t.Fatal("version summary leaked body")
	}
	v1 := f.req(t, "GET", p+"/versions/1", nil, 200)
	assertField(t, v1, "body", "# 旧正文")
	f.req(t, "POST", p+"/revert", object{"version": 999, "base_version": 3}, 404)
	f.req(t, "POST", p+"/revert", object{"version": 1, "base_version": 2}, 409)
	for _, change := range []object{{"title": "新标题"}, {"folder_id": 2}, {"tags": []string{"新标签"}}} {
		change["base_version"] = v["version"]
		v = f.req(t, "PATCH", p, change, 200)
	}
	assertField(t, v, "version", 6)
	if f.count(t, "SELECT count(*) FROM audit_log WHERE action LIKE 'kb.article.%'") < 7 {
		t.Fatal("missing audits")
	}
}

func TestConcurrentEdits(t *testing.T) {
	f := boot(t)
	v := f.article(t, "并发", "原文", nil)
	p := articlePath(v)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	errs := make(chan error, 2)
	for _, body := range []string{"甲", "乙"} {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			r, _ := http.NewRequest("PATCH", f.s.URL+p, strings.NewReader(core.JS(object{"body": body, "base_version": 1})))
			r.Header.Set("X-Requested-With", "fetch")
			res, err := f.client.Do(r)
			if err != nil {
				errs <- err
				return
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			codes <- res.StatusCode
		}(body)
	}
	wg.Wait()
	close(codes)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	if f.count(t, "SELECT count(*) FROM kb_versions") != 2 {
		t.Fatal("concurrent version history")
	}
}

func TestArticleValidation(t *testing.T) {
	f := boot(t)
	for _, extra := range []object{{"title": ""}, {"title": strings.Repeat("文", 101)}, {"folder_id": 999}, {"body": strings.Repeat("文", 100001)}, {"summary": strings.Repeat("文", 201)}, {"note": strings.Repeat("文", 101)}, {"tags": []string{strings.Repeat("文", 21)}}, {"tags": []string{"同", "同"}}, {"tags": []string{""}}, {"tags": strings.Fields("a b c d e f g h i j k")}} {
		in := object{"title": "验证", "folder_id": 1}
		for k, v := range extra {
			in[k] = v
		}
		f.req(t, "POST", "/api/kb/articles", in, 400)
	}
	v := f.article(t, strings.Repeat("文", 100), strings.Repeat("文", 100000), object{"summary": strings.Repeat("文", 200), "note": strings.Repeat("文", 100)})
	assertField(t, v, "version", 1)
	list := f.req(t, "GET", "/api/kb/articles?page=1&page_size=1", nil, 200)
	if len(items(list)) != 1 {
		t.Fatal(list)
	}
	if _, ok := items(list)[0].(object)["body"]; ok {
		t.Fatal("list includes body")
	}
}

func TestLinksAndProductScope(t *testing.T) {
	f := boot(t)
	now := core.Now()
	pid := f.exec(t, "INSERT INTO products(name,created_by,created_at,updated_at) VALUES('隐藏商品',1,?,?)", now, now)
	badInput := object{"folder_id": 1, "title": "物料", "links": []object{{"type": "item", "id": 1}}}
	err := f.req(t, "POST", "/api/kb/articles", badInput, 400)
	assertField(t, err, "error", "库存模块还没上线")
	if f.count(t, "SELECT count(*) FROM kb_articles") != 0 {
		t.Fatal("failed create persisted")
	}
	f.exec(t, "CREATE TABLE inv_items(id INTEGER PRIMARY KEY,code TEXT,name TEXT)")
	f.exec(t, "INSERT INTO inv_items VALUES(1,'MAT-1','珍珠')")
	links := []object{{"type": "category", "id": 1}, {"type": "product", "id": pid}, {"type": "item", "id": 1}}
	v := f.article(t, "三种关联", "全文", object{"links": links})
	p := articlePath(v)
	for _, l := range links {
		list := f.req(t, "GET", fmt.Sprintf("/api/kb/articles?target_type=%s&target_id=%v", l["type"], l["id"]), nil, 200)
		assertField(t, list, "total", 1)
	}
	got := v["links"].([]any)
	for _, l := range got {
		ll := l.(object)
		if ll["type"] == "item" {
			assertField(t, ll, "label", "MAT-1 珍珠")
			assertField(t, ll, "path", "/inventory/items/1")
		}
	}
	uid := f.user(t, "employee", "shops", core.KBView, core.KBEdit)
	f.login(t, "employee")
	v = f.req(t, "GET", p, nil, 200)
	for _, l := range v["links"].([]any) {
		ll := l.(object)
		if ll["type"] == "product" {
			assertField(t, ll, "label", "无权查看")
			assertField(t, ll, "path", "")
		}
	}
	f.req(t, "PATCH", p, object{"base_version": 1, "links": links}, 400)
	// Existing hidden links do not prevent unrelated edits.
	f.req(t, "PATCH", p, object{"base_version": 1, "pinned": true}, 200)
	f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(?,1)", uid)
	f.exec(t, "INSERT INTO listings(product_id,shop_id,created_by,created_at,updated_at) VALUES(?,1,1,?,?)", pid, now, now)
	v = f.req(t, "PATCH", p, object{"base_version": 1, "links": links}, 200)
	for _, l := range v["links"].([]any) {
		ll := l.(object)
		if ll["type"] == "product" {
			assertField(t, ll, "label", "隐藏商品")
			assertField(t, ll, "path", fmt.Sprintf("/products/%d", pid))
		}
	}
	for _, link := range []object{{"type": "category", "id": 999}, {"type": "product", "id": 999}, {"type": "item", "id": 999}, {"type": "bad", "id": 1}} {
		f.req(t, "PATCH", p, object{"base_version": 1, "links": []object{link}}, 400)
	}
	f.req(t, "GET", "/api/kb/articles?target_type=item", nil, 400)
	f.login(t, "admin")
	f.req(t, "DELETE", p, nil, 200)
	for _, l := range links {
		list := f.req(t, "GET", fmt.Sprintf("/api/kb/articles?target_type=%s&target_id=%v", l["type"], l["id"]), nil, 200)
		assertField(t, list, "total", 0)
	}
}

func TestFoldersTrashAndRestore(t *testing.T) {
	f := boot(t)
	roots := f.array(t, "/api/kb/folders")
	if len(roots) != 6 {
		t.Fatal(roots)
	}
	for i, name := range []string{"鉴别", "工艺做法", "拍摄规范", "平台规则", "客服话术", "供应商资料"} {
		assertField(t, roots[i], "name", name)
	}
	f.req(t, "POST", "/api/kb/folders", object{"name": "鉴别"}, 409)
	for _, name := range []string{"", strings.Repeat("文", 31)} {
		f.req(t, "POST", "/api/kb/folders", object{"name": name}, 400)
	}
	child := f.req(t, "POST", "/api/kb/folders", object{"name": "子目录", "parent_id": 1}, 201)
	cp := fmt.Sprintf("/api/kb/folders/%.0f", child["id"])
	f.req(t, "POST", "/api/kb/folders", object{"name": "子目录", "parent_id": 1}, 409)
	f.req(t, "POST", "/api/kb/folders", object{"name": "第三层", "parent_id": child["id"]}, 400)
	f.req(t, "PATCH", "/api/kb/folders/1", object{"parent_id": child["id"]}, 400)
	f.req(t, "PATCH", "/api/kb/folders/1", object{"parent_id": 2}, 400)
	f.req(t, "DELETE", "/api/kb/folders/1", nil, 409)
	f.req(t, "PATCH", cp, object{"name": "根目录", "parent_id": nil, "sort": -1}, 200)
	assertField(t, f.array(t, "/api/kb/folders")[0], "name", "根目录")
	f.req(t, "DELETE", cp, nil, 200)
	v := f.article(t, "删除恢复", "正文", object{"tags": []string{"标签"}})
	p := articlePath(v)
	assertField(t, f.array(t, "/api/kb/folders")[0], "article_count", 1)
	f.req(t, "DELETE", "/api/kb/folders/1", nil, 409)
	f.req(t, "DELETE", p, nil, 200)
	f.req(t, "GET", p, nil, 404)
	f.req(t, "GET", p+"/versions/1", nil, 404)
	assertField(t, f.req(t, "GET", "/api/kb/articles", nil, 200), "total", 0)
	trash := f.req(t, "GET", "/api/kb/trash", nil, 200)
	assertField(t, trash, "total", 1)
	if len(f.array(t, "/api/kb/tags")) != 0 {
		t.Fatal("trash tags visible")
	}
	assertField(t, f.array(t, "/api/kb/folders")[0], "article_count", 0)
	f.req(t, "DELETE", "/api/kb/folders/1", nil, 409)
	f.req(t, "POST", p+"/restore", nil, 200)
	v = f.req(t, "GET", p, nil, 200)
	assertField(t, v, "version", 1)
	assertField(t, f.array(t, "/api/kb/tags")[0], "count", 1)
	assertField(t, f.req(t, "GET", "/api/kb/trash", nil, 200), "total", 0)
}

func TestSearchRankingANDSnippetAndLimits(t *testing.T) {
	f := boot(t)
	first := f.article(t, "珍珠 鉴别", "# "+strings.Repeat("前", 70)+"**鉴别**"+strings.Repeat("后", 70), object{"tags": []string{"规范"}})
	second := f.article(t, "护理", "珍珠 鉴别", object{"pinned": true})
	f.article(t, "只含珍珠", "护理", nil)
	hits, err := Search(f.c.DB, "珍珠 鉴别", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].ID != int64(first["id"].(float64)) || hits[1].ID != int64(second["id"].(float64)) {
		t.Fatal(hits)
	}
	if hits[0].Snippet != strings.Repeat("前", 60)+"鉴别"+strings.Repeat("后", 60) {
		t.Fatal(hits[0].Snippet)
	}
	api := f.array(t, "/api/kb/search?q=珍珠%20鉴别")
	var exported []object
	if err := decodeMap(hits, &exported); err != nil {
		t.Fatal(err)
	}
	if core.JS(exported) != core.JS(api) {
		t.Fatalf("API and exported search differ: %v %v", hits, api)
	}
	list := f.req(t, "GET", "/api/kb/articles?q=珍珠%20鉴别&tag=规范&folder_id=1&page_size=1", nil, 200)
	assertField(t, list, "total", 1)
	assertField(t, items(list)[0].(object), "id", first["id"])
	if strings.Contains(items(list)[0].(object)["summary"].(string), "#") {
		t.Fatal("summary Markdown retained")
	}
	f.req(t, "DELETE", articlePath(first), nil, 200)
	hits, err = Search(f.c.DB, "珍珠 鉴别", 5)
	if err != nil || len(hits) != 1 {
		t.Fatal(hits, err)
	}
	// LIKE wildcard characters are search text, not a pattern supplied by callers.
	f.article(t, "100%_literal", "a\\b", nil)
	for _, q := range []string{"%", "_", `a\b`} {
		hits, err = Search(f.c.DB, q, 5)
		if err != nil || len(hits) != 1 {
			t.Fatal(q, hits, err)
		}
	}
	for i := 0; i < 22; i++ {
		f.article(t, fmt.Sprintf("上限 %d", i), "正文", nil)
	}
	for _, test := range []struct{ limit, want int }{{0, 5}, {1, 1}, {999, 20}} {
		hits, err = Search(f.c.DB, "上限", test.limit)
		if err != nil || len(hits) != test.want {
			t.Fatal(test, hits, err)
		}
	}
	if got := snippet("# 开头 **内容** [文字](https://example.test)", "不存在"); got != "开头 内容 文字" {
		t.Fatal(got)
	}
}

func TestOrganizeWithFakeAI(t *testing.T) {
	var mu sync.Mutex
	prompts := []string{}
	fail := false
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			w.WriteHeader(500)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-secret-key" {
			t.Error("missing auth")
		}
		var in object
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		prompts = append(prompts, in["messages"].([]any)[0].(object)["content"].(string))
		out := object{"title": strings.Repeat("文", 120), "body": "# 整理结果", "summary": "摘要", "tags": strings.Fields("a b c d e f g h i j k l"), "folder_id": 999}
		if len(prompts) == 2 {
			out["folder_id"] = 2
		}
		json.NewEncoder(w).Encode(object{"choices": []object{{"message": object{"content": core.JS(out)}}}})
	}))
	defer ai.Close()
	f := boot(t, func(c *core.Config) { c.AIBaseURL = ai.URL })
	path := filepath.Join(f.c.Cfg.PromptDir, "知识库整理.md")
	for i := 0; i < 2; i++ {
		prompt := fmt.Sprintf("PROMPT%d {{folders}} {{category_name}} {{category_rules}} {{codes}} {{banned_words}}", i)
		if err := os.WriteFile(path, []byte(prompt), 0600); err != nil {
			t.Fatal(err)
		}
		v := f.req(t, "POST", "/api/kb/organize", object{"text": "零散笔记", "folder_id": 1}, 200)
		assertField(t, v, "title", strings.Repeat("文", 100))
		if len(v["tags"].([]any)) != 10 {
			t.Fatal(v)
		}
		wantFolder := 1
		if i == 1 {
			wantFolder = 2
		}
		assertField(t, v, "folder_id", wantFolder)
		mu.Lock()
		got := prompts[i]
		mu.Unlock()
		if !strings.Contains(got, fmt.Sprintf("PROMPT%d", i)) || !strings.Contains(got, "1 鉴别") || strings.Contains(got, "{{") {
			t.Fatal(got)
		}
	}
	if f.count(t, "SELECT count(*) FROM kb_articles") != 0 {
		t.Fatal("organize saved article")
	}
	if f.count(t, "SELECT count(*) FROM ai_usage WHERE feature='kb.organize' AND user_id=1") != 2 {
		t.Fatal("missing usage")
	}
	for _, in := range []object{{"text": "", "folder_id": 1}, {"text": strings.Repeat("文", 20001), "folder_id": 1}, {"text": "笔记", "folder_id": 999}} {
		f.req(t, "POST", "/api/kb/organize", in, 400)
	}
	mu.Lock()
	fail = true
	mu.Unlock()
	f.req(t, "POST", "/api/kb/organize", object{"text": "笔记", "folder_id": 1}, 502)
	f.exec(t, "INSERT INTO quotas(user_id,month_text) VALUES(1,2)")
	v := f.req(t, "POST", "/api/kb/organize", object{"text": "笔记", "folder_id": 1}, 502)
	if !strings.Contains(v["error"].(string), "额度") {
		t.Fatal(v)
	}
	if f.count(t, "SELECT count(*) FROM ai_usage") != 2 {
		t.Fatal("failed AI charged")
	}
}
