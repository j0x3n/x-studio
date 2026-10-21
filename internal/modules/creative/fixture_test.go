package creative

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/products"
)

type fakeAI struct {
	images  atomic.Int64
	texts   atomic.Int64
	reviews atomic.Int64
	mu      sync.Mutex
	read    []OCRText
	review  *Review
	before  func()
	orders  []string
}

func (fake *fakeAI) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/chat/completions" {
		var req struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Messages) != 2 {
			w.WriteHeader(400)
			return
		}
		var prompt string
		_ = json.Unmarshal(req.Messages[0].Content, &prompt)
		fake.mu.Lock()
		var response any
		if strings.Contains(prompt, "TEST_OCR") {
			fake.texts.Add(1)
			read := append([]OCRText{}, fake.read...)
			response = core.Object{"texts": read}
		} else {
			fake.reviews.Add(1)
			review := passingReview()
			if fake.review != nil {
				review = *fake.review
			}
			response = review
			fake.orders = append(fake.orders, prompt)
		}
		fake.mu.Unlock()
		core.WriteJSON(w, 200, core.Object{"choices": []core.Object{{"message": core.Object{"content": core.JS(response)}}}})
		return
	}
	fake.images.Add(1)
	size := ""
	if r.URL.Path == "/images/generations" {
		var req struct {
			Size string `json:"size"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		size = req.Size
	} else if r.URL.Path == "/images/edits" {
		reader, err := r.MultipartReader()
		if err != nil {
			w.WriteHeader(400)
			return
		}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				w.WriteHeader(400)
				return
			}
			data, _ := io.ReadAll(part)
			if part.FormName() == "size" {
				size = string(data)
			}
			part.Close()
		}
	} else {
		w.WriteHeader(404)
		return
	}
	fake.mu.Lock()
	before := fake.before
	fake.mu.Unlock()
	if before != nil {
		before()
	}
	width, height := imageSize(size)
	if width == 0 || height == 0 {
		w.WriteHeader(400)
		return
	}
	im := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(im, im.Rect, image.NewUniform(color.NRGBA{210, 20, 180, 255}), image.Point{}, draw.Src)
	data, _ := pngBytes(im)
	core.WriteJSON(w, 200, core.Object{"data": []core.Object{{"b64_json": base64.StdEncoding.EncodeToString(data)}}})
}
func passingReview() Review {
	return Review{Checks: map[string]string{"product": "pass", "size": "na", "body": "na", "placement": "na", "style": "pass"}, Score: 99, SizeMM: []float64{}, Issues: []Issue{}}
}

type fixture struct {
	c                    *core.Core
	a                    *Module
	server               *httptest.Server
	client               *http.Client
	fake                 *fakeAI
	productID, listingID int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	fake := &fakeAI{}
	provider := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(provider.Close)
	root := t.TempDir()
	cfg := core.Config{DataDir: filepath.Join(root, "store"), PromptDir: filepath.Join(root, "prompts"), TemplateDir: filepath.Join(root, "templates"), AdminUser: "admin", AdminPassword: "password123", SessionSecret: strings.Repeat("s", 32), Workers: 1, AIKey: "fake", AIBaseURL: provider.URL, ImageModel: "fake-image", VisionModel: "fake-vision", ImageTimeout: 5 * time.Second}
	if err := os.MkdirAll(cfg.PromptDir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"生图-主图.md": "## 通用要求\nTEST_MAIN {{category_name}}\n## pixel_lock 像素锁定\nLOCK\n## model 模特\nMODEL {佩戴位置}\n## redraw 重画\nREDRAW\n## none 不放商品\nNONE\n## 上一次的问题\nFIX {问题}\n## 规则（后端照做，不给模型看）\nNEVER_SEND_RULES", "生图-详情.md": "## 通用要求\nTEST_DETAIL {{category_name}}\n## 图上的字\nTEXT {文字}\n## 规则（后端照做，不给模型看）\nNEVER_SEND_RULES", "生图-核对.md": "TEST_REVIEW {{order}}", "生图-文字核对.md": "TEST_OCR", "生图-文字.md": "## 提示词\nTEST_RETEXT {{text}} OLD {{old_text}} {{hint}}\n## 规则（后端照做，不给模型看）\nNEVER_SEND_RULES", "违禁词.txt": "error\t绝对第一\t禁词"} {
		if err := os.WriteFile(filepath.Join(cfg.PromptDir, name), []byte(content), 0600); err != nil {
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
	f := &fixture{c: c, a: a, server: server, client: &http.Client{Jar: jar, Timeout: 15 * time.Second}, fake: fake}
	t.Cleanup(func() { server.Close(); c.Close() })
	f.request(t, "POST", "/api/login", core.Object{"username": "admin", "password": "password123"}, 200)
	created := f.request(t, "POST", "/api/products", core.Object{"name": "离线测试珍珠耳钉", "shop_ids": []int64{1}}, 201)
	f.productID = int64(number(created["product"].(map[string]any)["id"]))
	if err = c.DB.QueryRow("SELECT id FROM listings WHERE product_id=?", f.productID).Scan(&f.listingID); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "UPDATE products SET info=? WHERE id=?", core.JS(core.Object{"style": "珍珠耳钉", "size": "8mm"}), f.productID)
	f.settings(t, core.Object{"spot_check_rate": 0, "max_auto_redraw": 0, "final_glance": false})
	return f
}
func (f *fixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.c.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) settings(t *testing.T, values core.Object) {
	t.Helper()
	current := f.a.settings()
	for key, value := range values {
		current[key] = value
	}
	f.exec(t, "INSERT INTO settings(key,value,updated_at) VALUES('creative',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", core.JS(current), core.Now())
}
func (f *fixture) request(t *testing.T, method, path string, body any, status int) core.Object {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(core.JS(body))
	}
	req, _ := http.NewRequest(method, f.server.URL+path, reader)
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", "application/json")
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != status {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, res.StatusCode, status, data)
	}
	var out core.Object
	if json.Unmarshal(data, &out) != nil {
		t.Fatalf("invalid JSON: %s", data)
	}
	return out
}
func testTemplate(mode string) Template {
	return Template{Key: "test_" + mode, Name: "测试模板", Kind: "detail", Slot: "d1", Categories: []string{}, Purpose: "test", Prompt: "保持原样 {款名} {文字}", StyleRefs: []string{}, Texts: []TextSpec{}, PhotoCodes: []string{"A1"}, ProductMode: mode, ProductBox: []float64{0.2, 0.2, 0.6, 0.6}, GenSize: "1024x1024", Export: []ExportSize{{Name: "750", W: 750, H: 0}}, Count: 1, Version: 1, Enabled: true, Source: "custom"}
}
func (f *fixture) makeSet(t *testing.T, tpl Template, count int) (Set, []Item) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("SELECT coalesce(max(version),0)+1 FROM creative_sets WHERE listing_id=?", f.listingID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	result, err := tx.Exec("INSERT INTO creative_sets(listing_id,version,status,template_keys,params,parts,images_allowance,created_by,created_at,updated_at) VALUES(?,?,'generating',?,'{}','[\"detail\"]',40,1,?,?)", f.listingID, version, core.JS(map[string]string{tpl.Slot: tpl.Key}), core.Now(), core.Now())
	if err != nil {
		t.Fatal(err)
	}
	sid, _ := result.LastInsertId()
	for seq := 1; seq <= count; seq++ {
		_, err = insertItem(ctx, tx, Item{SetID: &sid, ListingID: f.listingID, Slot: tpl.Slot, Seq: seq, TemplateKey: tpl.Key, TemplateVersion: tpl.Version, TemplateSnapshot: tpl, Attempt: 1, ProductMode: tpl.ProductMode, CreatedBy: 1})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	set, err := f.a.set(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	items, err := f.a.currentItems(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	return set, items
}
func (f *fixture) run(t *testing.T, kind, target string, id, uid int64, params core.Object) error {
	t.Helper()
	tx, err := f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	jid, err := enqueueTx(context.Background(), tx, kind, target, id, uid, params)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return f.runJob(t, jid)
}
func (f *fixture) runJob(t *testing.T, id int64) error {
	t.Helper()
	job, err := core.ScanJob(f.c.DB.QueryRow("SELECT "+core.JobColumns+" FROM jobs WHERE id=?", id))
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, "UPDATE jobs SET status='running' WHERE id=?", id)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = f.a.execute(ctx, job)
	status := "done"
	if err != nil {
		status = "failed"
	}
	f.exec(t, "UPDATE jobs SET status=? WHERE id=?", status, id)
	return err
}
func (f *fixture) source(t *testing.T) int64 {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 160, 120))
	draw.Draw(im, im.Rect, image.White, image.Point{}, draw.Src)
	for y := 30; y < 90; y++ {
		for x := 40; x < 120; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(30 + x), 70, uint8(80 + y), 255})
		}
	}
	data, err := pngBytes(im)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(f.c.Dir(f.productID), "原图"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.c.Dir(f.productID), "原图", "source.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := f.c.DB.Exec("INSERT INTO assets(product_id,file,orig_name,width,height,code,created_by,created_at) VALUES(?,'原图/source.png','source',160,120,'A1',1,?)", f.productID, core.Now())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return id
}
func itemPath(id int64, action string) string {
	return fmt.Sprintf("/api/creative/items/%d/%s", id, action)
}
func setPath(id int64, action string) string {
	return fmt.Sprintf("/api/creative/sets/%d/%s", id, action)
}
