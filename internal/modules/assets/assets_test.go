package assets_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/pipeline"
	"xstudio/internal/modules/products"
)

type obj = map[string]any
type fixture struct {
	c      *core.Core
	s      *httptest.Server
	client *http.Client
}

func newFixture(t *testing.T, change func(*core.Config)) *fixture {
	t.Helper()
	root := t.TempDir()
	cfg := core.Config{DataDir: filepath.Join(root, "data"), PromptDir: filepath.Join(root, "prompts"), TemplateDir: filepath.Join(root, "templates"), AdminUser: "admin", AdminPassword: "password123", SessionSecret: strings.Repeat("s", 32), Workers: 2}
	if change != nil {
		change(&cfg)
	}
	c, err := core.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(accounts.New(c), products.New(c), assets.New(c), pipeline.New(c)); err != nil {
		c.Close()
		t.Fatal(err)
	}
	s := httptest.NewServer(c.Handler())
	c.SetBaseURL(s.URL)
	jar, _ := cookiejar.New(nil)
	f := &fixture{c, s, &http.Client{Jar: jar}}
	t.Cleanup(func() { s.Close(); c.Close() })
	f.login(t, "admin")
	return f
}
func (f *fixture) req(t *testing.T, method, path string, body any, status int) obj {
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
		t.Fatalf("%s %s：%d，期望 %d：%s", method, path, res.StatusCode, status, b)
	}
	var out obj
	if json.Unmarshal(b, &out) != nil {
		t.Fatalf("无效 JSON：%s", b)
	}
	return out
}
func (f *fixture) login(t *testing.T, name string) {
	t.Helper()
	f.req(t, "POST", "/api/login", obj{"username": name, "password": "password123"}, 200)
}
func (f *fixture) product(t *testing.T, name string, shops ...int64) int64 {
	t.Helper()
	o := f.req(t, "POST", "/api/products", obj{"name": name, "shop_ids": shops}, 201)
	return id(o["product"])
}
func (f *fixture) user(t *testing.T, name string, role int64, shops ...int64) int64 {
	t.Helper()
	return id(f.req(t, "POST", "/api/users", obj{"username": name, "password": "password123", "role_id": role, "shop_ids": shops}, 201)["user"])
}
func id(v any) int64 { return int64(v.(map[string]any)["id"].(float64)) }
func imageBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(30 + x*100/max(1, w)), uint8(80 + y*60/max(1, h)), 100, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func (f *fixture) upload(t *testing.T, pid int64, b []byte) int64 {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	p, _ := mw.CreateFormFile("files", "实拍.png")
	p.Write(b)
	mw.Close()
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/products/%d/assets", f.s.URL, pid), &buf)
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var o obj
	json.NewDecoder(res.Body).Decode(&o)
	if res.StatusCode != 201 {
		t.Fatalf("上传失败：%d %v", res.StatusCode, o)
	}
	return id(o["items"].([]any)[0])
}
func (f *fixture) wait(t *testing.T, jid int64) core.Job {
	t.Helper()
	until := time.Now().Add(15 * time.Second)
	for time.Now().Before(until) {
		j, err := core.ScanJob(f.c.DB.QueryRow("SELECT "+core.JobColumns+" FROM jobs WHERE id=?", jid))
		if err != nil {
			t.Fatal(err)
		}
		if j.Status == "done" || j.Status == "failed" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("任务超时")
	return core.Job{}
}

func TestLibraryScopeFiltersVersionsAndPatch(t *testing.T) {
	f := newFixture(t, nil)
	p1 := f.product(t, "珍珠耳饰", 1)
	shop2 := id(f.req(t, "POST", "/api/shops", obj{"name": "另一店", "platform": "other", "language": "en"}, 201)["shop"])
	p2 := f.product(t, "隐藏商品", shop2)
	a1 := f.upload(t, p1, imageBytes(t, 40, 30))
	a2 := f.upload(t, p2, imageBytes(t, 40, 30))
	f.user(t, "operator", 2, 1)
	guest := f.user(t, "guest", 5)
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", a1), obj{"title": "蝴蝶结", "note": "礼物", "tags": []string{"耳钉", "白色"}, "source_override": "ai_edited"}, 200)
	if _, err := f.c.DB.Exec("INSERT INTO assets(product_id,parent_id,file,orig_name,width,height,source,created_by,created_at,archived) SELECT product_id,id,file,orig_name,width,height,'ai_enhanced',created_by,created_at,1 FROM assets WHERE id=?", a1); err != nil {
		t.Fatal(err)
	}
	f.req(t, "POST", fmt.Sprintf("/api/products/%d/shares", p1), obj{"user_id": guest, "can_upload": true}, 201)
	f.login(t, "operator")
	o := f.req(t, "GET", "/api/assets?tag=耳钉,白色&source=ai_edited,original&q=礼物&root=1", nil, 200)
	if o["total"] != float64(1) {
		t.Fatal(o)
	}
	item := o["items"].([]any)[0].(obj)
	if item["version_count"] != float64(2) || len(item["phash"].(string)) != 16 || item["effective_source"] != "ai_edited" {
		t.Fatal(item)
	}
	f.req(t, "GET", fmt.Sprintf("/api/assets/%d", a2), nil, 404)
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", a2), obj{"note": "不能改"}, 404)
	detail := f.req(t, "GET", fmt.Sprintf("/api/assets/%d", a1), nil, 200)
	if len(detail["versions"].([]any)) != 2 || id(detail["versions"].([]any)[0]) != a1 {
		t.Fatal(detail)
	}
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", a1), obj{"archived": true, "title": "新显示名"}, 200)
	if f.req(t, "GET", "/api/assets", nil, 200)["total"] != float64(0) {
		t.Fatal("归档素材仍默认显示")
	}
	if f.req(t, "GET", "/api/assets?archived=1&page_size=1", nil, 200)["total"] != float64(2) {
		t.Fatal("归档筛选失败")
	}
	f.login(t, "guest")
	if f.req(t, "GET", "/api/assets?archived=1", nil, 200)["total"] != float64(2) {
		t.Fatal("协作范围错误")
	}
	f.req(t, "GET", fmt.Sprintf("/api/assets/%d", a2), nil, 404)
	if f.req(t, "GET", "/api/assets?shop_id=1&archived=1", nil, 200)["total"] != float64(0) {
		t.Fatal("分享角色泄漏店铺关联")
	}
}
func (f *fixture) assertAudit(t *testing.T, actions ...string) {
	t.Helper()
	for _, action := range actions {
		var count int
		if err := f.c.DB.QueryRow("SELECT count(*) FROM audit_log WHERE action=?", action).Scan(&count); err != nil || count == 0 {
			t.Fatalf("缺少操作日志 %s：%v", action, err)
		}
	}
}

func TestMigrationV2AndBackgroundHashPreservesOriginal(t *testing.T) {
	f := newFixture(t, nil)
	pid := f.product(t, "旧素材", 1)
	b := imageBytes(t, 30, 20)
	aid := f.upload(t, pid, b)
	var file, created string
	if err := f.c.DB.QueryRow("SELECT file,created_at FROM assets WHERE id=?", aid).Scan(&file, &created); err != nil {
		t.Fatal(err)
	}
	// Recreate the actual v1 shape, then apply only the v2 migration in a transaction.
	f.c.Mu.Lock()
	locked := true
	defer func() {
		if locked {
			f.c.Mu.Unlock()
		}
	}()
	if _, err := f.c.DB.Exec("DROP TRIGGER assets_updated_at_insert;DROP TABLE asset_presets;ALTER TABLE assets DROP COLUMN title;ALTER TABLE assets DROP COLUMN note;ALTER TABLE assets DROP COLUMN archived;ALTER TABLE assets DROP COLUMN updated_at;ALTER TABLE assets DROP COLUMN phash;"); err != nil {
		t.Fatal(err)
	}
	tx, err := f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	m := assets.New(f.c).Migrations()
	if err = m[1].Up(tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var hash, updated string
	f.c.DB.QueryRow("SELECT phash,updated_at FROM assets WHERE id=?", aid).Scan(&hash, &updated)
	if hash != "" || updated != created {
		t.Fatal("迁移改动了旧素材")
	}
	if f.req(t, "GET", fmt.Sprintf("/api/assets/%d", aid), nil, 200)["asset"].(obj)["phash"] != "" {
		t.Fatal("详情不应同步补算哈希")
	}
	if f.req(t, "GET", "/api/assets", nil, 200)["items"].([]any)[0].(obj)["phash"] != "" {
		t.Fatal("列表不应同步补算哈希")
	}
	f.c.Mu.Unlock()
	locked = false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.c.DB.QueryRow("SELECT phash FROM assets WHERE id=?", aid).Scan(&hash)
		if len(hash) == 16 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(hash) != 16 {
		t.Fatal("后台未补算哈希", hash)
	}
	data, err := os.ReadFile(filepath.Join(f.c.Dir(pid), filepath.FromSlash(file)))
	if err != nil || !bytes.Equal(data, b) {
		t.Fatal("迁移改变了原文件", err)
	}
}

func TestLibrarySQLScopePagination(t *testing.T) {
	f := newFixture(t, nil)
	operator := f.user(t, "operator", 2, 1)
	guest := f.user(t, "guest", 5)
	shop2 := id(f.req(t, "POST", "/api/shops", obj{"name": "隐藏店", "platform": "other", "language": "en"}, 201)["shop"])
	p1, p2, p3 := f.product(t, "分配店商品", 1), f.product(t, "本人商品"), f.product(t, "隐藏商品", shop2)
	if _, err := f.c.DB.Exec("UPDATE products SET created_by=? WHERE id=?", operator, p2); err != nil {
		t.Fatal(err)
	}
	f.req(t, "POST", fmt.Sprintf("/api/products/%d/shares", p2), obj{"user_id": guest, "can_upload": false}, 201)
	visible, shared := []int64{}, []int64{}
	for i := 0; i < 25; i++ {
		pid := p3
		if i < 21 {
			pid = p1
			if i%2 == 1 {
				pid = p2
			}
		}
		aid := f.upload(t, pid, imageBytes(t, 8, 8))
		if pid != p3 {
			visible = append(visible, aid)
		}
		if pid == p2 {
			shared = append(shared, aid)
		}
	}
	if _, err := f.c.DB.Exec("UPDATE assets SET created_at='2026-10-18T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	f.login(t, "operator")
	for _, sort := range []string{"new", "old"} {
		for page := 1; page <= 3; page++ {
			o := f.req(t, "GET", fmt.Sprintf("/api/assets?page_size=10&page=%d&sort=%s", page, sort), nil, 200)
			items := o["items"].([]any)
			want := 10
			if page == 3 {
				want = 1
			}
			if o["total"] != float64(21) || len(items) != want {
				t.Fatal(page, o)
			}
			for i, item := range items {
				offset := (page-1)*10 + i
				idx := offset
				if sort == "new" {
					idx = len(visible) - 1 - offset
				}
				if id(item) != visible[idx] || item.(obj)["product_id"] == float64(p3) {
					t.Fatal("分页顺序或范围错误", sort, page, item)
				}
			}
		}
	}
	f.login(t, "guest")
	o := f.req(t, "GET", "/api/assets?page_size=10", nil, 200)
	if o["total"] != float64(len(shared)) {
		t.Fatal(o)
	}
	for i, item := range o["items"].([]any) {
		if id(item) != shared[len(shared)-1-i] {
			t.Fatal("协作范围错误", item)
		}
	}
}
