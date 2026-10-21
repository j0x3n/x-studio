package scenes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
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
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/products"
)

type sceneFixture struct {
	c         *core.Core
	a         *Module
	server    *httptest.Server
	client    *http.Client
	listingID int64
}

func newSceneFixture(t *testing.T, withSeed bool) *sceneFixture {
	t.Helper()
	dir := t.TempDir()
	cfg := core.Config{DataDir: filepath.Join(dir, "store"), TemplateDir: filepath.Join(dir, "templates"), AdminUser: "admin", AdminPassword: "password123", SessionSecret: strings.Repeat("s", 32), Workers: 1}
	if withSeed {
		seedDir := filepath.Join(cfg.TemplateDir, "生图场景")
		if err := os.MkdirAll(seedDir, 0700); err != nil {
			t.Fatal(err)
		}
		var picture bytes.Buffer
		if err := png.Encode(&picture, image.NewNRGBA(image.Rect(0, 0, 12, 9))); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(seedDir, "参考.png"), picture.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		body := core.JS(core.Object{"key": "fallback", "name": "兜底桌面", "still": "米白桌面", "wear": "白色衬衫", "light": "自然光", "default": true, "refs": []core.Object{{"use": "still", "file": "参考.png"}}})
		if err := os.WriteFile(filepath.Join(seedDir, "default.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c, err := core.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(c)
	if err = c.Start(accounts.New(c), products.New(c), assets.New(c), a); err != nil {
		c.Close()
		t.Fatal(err)
	}
	server := httptest.NewServer(c.Handler())
	jar, _ := cookiejar.New(nil)
	f := &sceneFixture{c: c, a: a, server: server, client: &http.Client{Jar: jar}}
	t.Cleanup(func() { server.Close(); c.Close() })
	f.request(t, "POST", "/api/login", core.Object{"username": "admin", "password": "password123"}, 200)
	created := f.request(t, "POST", "/api/products", core.Object{"name": "场景测试珍珠", "shop_ids": []int64{1}}, 201)
	productID := int64(created["product"].(map[string]any)["id"].(float64))
	if err = c.DB.QueryRow("SELECT id FROM listings WHERE product_id=?", productID).Scan(&f.listingID); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *sceneFixture) request(t *testing.T, method, path string, body any, status int) core.Object {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(core.JS(body))
	}
	request, err := http.NewRequest(method, f.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Requested-With", "fetch")
	response, err := f.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, response.StatusCode, status, data)
	}
	var out core.Object
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err, string(data))
	}
	return out
}
func (f *sceneFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.c.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
func (f *sceneFixture) create(t *testing.T, name string, shopID any, categories []string) int64 {
	t.Helper()
	out := f.request(t, "POST", "/api/scenes", core.Object{"name": name, "shop_id": shopID, "still": "木桌", "categories": categories}, 201)
	return int64(out["scene"].(map[string]any)["id"].(float64))
}
func TestScenesCRUDSeedAndReferences(t *testing.T) {
	f := newSceneFixture(t, true)
	items := f.request(t, "GET", "/api/scenes", nil, 200)["items"].([]any)
	if len(items) != 1 {
		t.Fatal(items)
	}
	seed := items[0].(map[string]any)
	seedID := int64(seed["id"].(float64))
	if seed["is_default"] != true || len(seed["refs"].([]any)) != 1 {
		t.Fatal(seed)
	}
	seedPath := fmt.Sprintf("/api/scenes/%d", seedID)
	f.request(t, "PATCH", seedPath, core.Object{"name": "用户改过的兜底"}, 200)
	if err := f.a.AfterMigrate(); err != nil {
		t.Fatal(err)
	}
	if got := f.request(t, "GET", seedPath, nil, 200)["scene"].(map[string]any)["name"]; got != "用户改过的兜底" {
		t.Fatal(got)
	}
	f.request(t, "PATCH", seedPath, core.Object{"archived": true}, 200)
	f.request(t, "DELETE", seedPath, nil, 409)
	f.request(t, "PATCH", seedPath, core.Object{"archived": false}, 200)
	id := f.create(t, "新场景", 1, []string{})
	path := fmt.Sprintf("/api/scenes/%d", id)
	f.request(t, "DELETE", path, nil, 409)
	f.request(t, "PATCH", path, core.Object{"name": "修改场景", "wear": "亚麻衬衫"}, 200)
	for _, body := range []core.Object{{"name": strings.Repeat("中", 41), "still": "桌面"}, {"name": "空场景"}, {"name": "过长", "still": strings.Repeat("中", 601)}} {
		f.request(t, "POST", "/api/scenes", body, 400)
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewNRGBA(image.Rect(0, 0, 20, 10))); err != nil {
		t.Fatal(err)
	}
	upload := func(data []byte, status int) core.Object {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "伪装.jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(data); err != nil {
			t.Fatal(err)
		}
		if err = writer.WriteField("use", "still"); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		request, _ := http.NewRequest("POST", f.server.URL+path+"/refs", &body)
		request.Header.Set("X-Requested-With", "fetch")
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response, err := f.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var result core.Object
		if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status {
			t.Fatal(response.StatusCode, result)
		}
		return result
	}
	upload([]byte("MZ pretending to be an image"), 400)
	ref := upload(pngData.Bytes(), 201)["ref"].(map[string]any)
	response, err := f.client.Get(f.server.URL + ref["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	received, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Equal(received, pngData.Bytes()) {
		t.Fatal(response.StatusCode)
	}
	for index := 0; index < 3; index++ {
		upload(pngData.Bytes(), 201)
	}
	upload(pngData.Bytes(), 409)
	refID := int64(ref["id"].(float64))
	var savedFile string
	if err = f.c.DB.QueryRow("SELECT file FROM scene_refs WHERE id=?", refID).Scan(&savedFile); err != nil {
		t.Fatal(err)
	}
	f.request(t, "DELETE", fmt.Sprintf("%s/refs/%d", path, refID), nil, 200)
	if _, err = os.Stat(savedFile); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"shop_id": 1, "scene_id": id}, 200)
	f.request(t, "PATCH", path, core.Object{"archived": true}, 200)
	f.request(t, "DELETE", path, nil, 200)
	f.request(t, "GET", path, nil, 404)
	if _, err = os.Stat(filepath.Join(f.c.Cfg.DataDir, "scenes", fmt.Sprint(id))); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var count int
	if err = f.c.DB.QueryRow("SELECT count(*) FROM scene_bindings WHERE scene_id=?", id).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
func TestSceneBindingsResolutionAndValidation(t *testing.T) {
	f := newSceneFixture(t, true)
	ctx := context.Background()
	shopScene := f.create(t, "店铺场景", 1, []string{})
	listingScene := f.create(t, "单款场景", nil, []string{})
	lookup := func(wantID int64, wantSource string) {
		t.Helper()
		snapshot, source, err := resolve(ctx, f.c.DB, f.listingID, 0)
		if err != nil || snapshot.ID != wantID || source != wantSource {
			t.Fatal(snapshot, source, err)
		}
	}
	var defaultID int64
	if err := f.c.DB.QueryRow("SELECT id FROM scenes WHERE builtin_key='fallback'").Scan(&defaultID); err != nil {
		t.Fatal(err)
	}
	lookup(defaultID, "default")
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"shop_id": 1, "scene_id": shopScene}, 200)
	lookup(shopScene, "shop")
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"listing_ids": []int64{f.listingID}, "scene_id": listingScene}, 200)
	lookup(listingScene, "listing")
	bindings := f.request(t, "GET", fmt.Sprintf("/api/scenes/bindings?listing_ids=%d", f.listingID), nil, 200)
	if len(bindings["shops"].([]any)) != 1 || bindings["listings"].([]any)[0].(map[string]any)["source"] != "listing" {
		t.Fatal(bindings)
	}
	snapshot, err := Resolve(ctx, f.c.DB, f.listingID, shopScene)
	if err != nil || snapshot.ID != shopScene {
		t.Fatal(snapshot, err)
	}
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"listing_ids": []int64{f.listingID}, "scene_id": nil}, 200)
	lookup(shopScene, "shop")
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"shop_id": 1, "scene_id": nil}, 200)
	lookup(defaultID, "default")
	f.exec(t, "INSERT INTO shops(id,name,platform,language,created_at) VALUES(99,'另一店','taobao','zh-CN',?)", core.Now())
	foreign := f.create(t, "其他店场景", 99, []string{})
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"listing_ids": []int64{f.listingID}, "scene_id": foreign}, 400)
	f.request(t, "POST", "/api/categories", core.Object{"key": "other", "name": "其他品类", "copy_from": "pearl"}, 201)
	category := "other"
	mismatch := f.create(t, "其他品类", nil, []string{category})
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"listing_ids": []int64{f.listingID}, "scene_id": mismatch}, 400)
	if _, err = Resolve(ctx, f.c.DB, f.listingID, mismatch); err == nil {
		t.Fatal("品类不匹配仍可出图")
	}
	f.request(t, "PATCH", fmt.Sprintf("/api/scenes/%d", listingScene), core.Object{"archived": true}, 200)
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"shop_id": 1, "scene_id": listingScene}, 400)
	if _, err = Resolve(ctx, f.c.DB, f.listingID, listingScene); err == nil {
		t.Fatal("已归档仍可出图")
	}
	tooMany := make([]int64, 201)
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"listing_ids": tooMany, "scene_id": nil}, 400)
}
func TestScenePermissionsAndShopVisibility(t *testing.T) {
	f := newSceneFixture(t, false)
	f.exec(t, "INSERT INTO shops(id,name,platform,language,created_at) VALUES(99,'不可见店铺','taobao','zh-CN',?)", core.Now())
	common := f.create(t, "通用", nil, []string{})
	own := f.create(t, "本店", 1, []string{})
	foreign := f.create(t, "别店", 99, []string{})
	var password []byte
	if err := f.c.DB.QueryRow("SELECT password_hash FROM users WHERE id=1").Scan(&password); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "INSERT INTO roles(id,key,name,scope,perms,builtin,created_at) VALUES(99,'scene_reader','场景读者','shops',?,0,?)", core.JS([]string{core.ProductView}), core.Now())
	f.exec(t, "INSERT INTO users(id,username,password_hash,display_name,role_id,created_at) VALUES(99,'reader',?,'读者',99,?)", password, core.Now())
	f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(99,1)")
	jar, _ := cookiejar.New(nil)
	f.client = &http.Client{Jar: jar}
	f.request(t, "POST", "/api/login", core.Object{"username": "reader", "password": "password123"}, 200)
	items := f.request(t, "GET", "/api/scenes", nil, 200)["items"].([]any)
	if len(items) != 2 {
		t.Fatal(items)
	}
	f.request(t, "GET", fmt.Sprintf("/api/scenes/%d", common), nil, 200)
	f.request(t, "GET", fmt.Sprintf("/api/scenes/%d", own), nil, 200)
	f.request(t, "GET", fmt.Sprintf("/api/scenes/%d", foreign), nil, 404)
	f.request(t, "GET", "/api/scenes?shop_id=99", nil, 404)
	f.request(t, "POST", "/api/scenes", core.Object{"name": "无权", "still": "桌面"}, 403)
	f.request(t, "PATCH", fmt.Sprintf("/api/scenes/%d", own), core.Object{"name": "无权修改"}, 403)
	f.request(t, "DELETE", fmt.Sprintf("/api/scenes/%d", own), nil, 403)
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"shop_id": 1, "scene_id": common}, 403)
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"listing_ids": []int64{f.listingID}, "scene_id": common}, 403)
	f.exec(t, "UPDATE roles SET perms=? WHERE id=99", core.JS([]string{core.ProductView, core.CreativeRun}))
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"listing_ids": []int64{f.listingID}, "scene_id": common}, 200)
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"listing_ids": []int64{f.listingID}, "scene_id": foreign}, 404)
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"shop_id": 1, "scene_id": common}, 403)
	snapshot, err := Resolve(context.Background(), f.c.DB, f.listingID, 0)
	if err != nil || snapshot.ID != common {
		t.Fatal(snapshot, err)
	}
}
func TestSceneMissingDirectoryAndSeedValidation(t *testing.T) {
	f := newSceneFixture(t, false)
	snapshot, err := Resolve(context.Background(), f.c.DB, f.listingID, 0)
	if err != nil || snapshot.ID != 0 {
		t.Fatal(snapshot, err)
	}
	dir := filepath.Join(f.c.Cfg.TemplateDir, "生图场景")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "two"} {
		body := core.JS(core.Object{"key": name, "name": name, "still": "桌面", "default": true})
		if err = os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.a.AfterMigrate(); err == nil || !strings.Contains(err.Error(), "只能有一个") {
		t.Fatal(err)
	}
}
