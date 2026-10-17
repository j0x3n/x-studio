package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func intString(id int64) string { return strconv.FormatInt(id, 10) }
func writePrompts(t *testing.T, a *Module) {
	t.Helper()
	if e := os.MkdirAll(a.Cfg.PromptDir, 0700); e != nil {
		t.Fatal(e)
	}
	for name, body := range map[string]string{"分拣.md": "按编号分拣，输出 JSON", "文案.md": "按样例输出 content main listing JSON", "违禁词.txt": "# 一行一个词\n保证招财\n疗愈\n"} {
		if e := os.WriteFile(filepath.Join(a.Cfg.PromptDir, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
}
func aiReply(w http.ResponseWriter, raw string) {
	writeJSON(w, 200, Object{"choices": []Object{{"message": Object{"content": raw}}}})
}
func TestClassifyCopyWithFakeAI(t *testing.T) {
	var reply string
	var calls atomic.Int32
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("wrong AI request")
		}
		var body Object
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body["response_format"].(Object)["type"] != "json_object" {
			t.Error("missing JSON mode")
		}
		aiReply(w, reply)
	}))
	defer ai.Close()
	f := newFixture(t, func(c *Config) {
		c.AIKey = "test-key"
		c.AIBaseURL = ai.URL + "/v1"
		c.VisionModel = "vision"
		c.TextModel = "text"
	})
	writePrompts(t, f.a)
	f.login(t, "admin")
	id := f.product(t)
	ids := []int64{}
	for i := 0; i < 4; i++ {
		up := f.upload(t, id, fmt.Sprintf("%d.png", i), photoBytes(t, 64, 80), 201)
		ids = append(ids, int64(up["photos"].([]any)[0].(Object)["id"].(float64)))
	}
	f.req(t, "PATCH", "/api/assets/"+intString(ids[3]), Object{"code": "B1"}, 200)
	reply = "```json\n" + js([]Object{{"photo_id": ids[0], "code": "A1", "white_bg": true}, {"photo_id": ids[1], "code": "A1"}, {"photo_id": ids[2], "code": "B1"}, {"photo_id": ids[3], "code": "E1"}, {"photo_id": 99999, "code": "C1"}, {"photo_id": ids[0], "code": "D4"}}) + "\n```"
	p, _ := f.a.product(id)
	if e := f.a.classify(testActor(context.Background()), p); e != nil {
		t.Fatal(e)
	}
	photos, _ := f.a.photos(id)
	if photos[0].Code != "A1" || photos[1].Code != "" || !strings.Contains(photos[1].AINote, "重复") || photos[2].Code != "" || photos[3].Code != "B1" || !photos[3].Manual {
		t.Fatal(photos)
	}
	if e := f.a.retouch(testActor(context.Background()), p); e != nil {
		t.Fatal(e)
	}
	reply = js(Object{"content": Object{"name": "粉珍珠", "photos": Object{"hero": Object{"f": "A1.jpg"}, "pack": Object{"f": "does-not-exist.jpg"}}, "details": []Object{{"photo": Object{"f": "../../app.db"}, "title": "非法照片"}, {"photo": Object{"f": "B1.jpg"}, "title": "光泽"}}, "oneLiner": "保证招财"}, "main": Object{"imgs": []Object{{"n": 1, "layout": "photo", "f": "A1.jpg"}, {"n": 2, "f": "missing.jpg"}}}, "listing": Object{"title": "真淡水珍珠耳钉", "attrs": [][]string{{"材质", "淡水养殖珍珠"}}, "sku": []Object{{"name": "粉色", "price": 69}}}})
	if e := f.a.copyText(testActor(context.Background()), p); e != nil {
		t.Fatal(e)
	}
	p, _ = f.a.product(id)
	photoSlots := p.Content["photos"].(Object)
	if _, ok := photoSlots["pack"]; ok {
		t.Fatal("nonexistent photo retained")
	}
	if photoSlots["hero"].(Object)["f"] != "A1.jpg" {
		t.Fatal(p.Content)
	}
	if len(p.Main["imgs"].([]any)) != 1 {
		t.Fatal("missing main photo retained")
	}
	if len(p.Listing["compliance"].([]any)) != 1 {
		t.Fatal("banned word not detected")
	}
	// Both generated JS files contain JSON, not model-supplied executable JS.
	f.req(t, "PATCH", fmt.Sprintf("/api/listings/%d", id), Object{"content": Object{"tags": "bad type"}}, 400)
	if calls.Load() != 2 {
		t.Fatal("unexpected retries", calls.Load())
	}
}
func TestCopyCleansGroupedMain(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aiReply(w, js(Object{
			"content": Object{}, "listing": Object{}, "main": Object{"imgs": []Object{
				{"n": 1, "layout": "photo", "f": "missing.jpg"},
				{"n": 2, "layout": "variants", "items": []Object{{"f": "A3_白_白底.jpg"}, {"f": "A3_粉_白底.jpg"}}},
				{"n": 3, "layout": "sizes", "items": []Object{{"f": "a.jpg"}, {"f": "missing.jpg"}}},
				{"n": 4, "layout": "photo", "f": "a.jpg"},
				{"n": 5, "layout": "variants", "items": []Object{{"f": "a.jpg"}, {"f": "b.jpg"}, {"f": "missing.jpg"}}},
			}},
		}))
	}))
	defer ai.Close()
	f := newFixture(t, func(c *Config) { c.AIKey = "test-key"; c.AIBaseURL = ai.URL + "/v1"; c.TextModel = "text" })
	writePrompts(t, f.a)
	f.login(t, "admin")
	id := f.product(t)
	dir := filepath.Join(f.a.Dir(id), "照片")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.jpg", "b.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), photoBytes(t, 8, 8), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := f.a.product(id)
	if err := f.a.copyText(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	p, _ = f.a.product(id)
	imgs := p.Main["imgs"].([]any)
	if len(imgs) != 2 || imgs[0].(Object)["n"] != 1.0 || imgs[1].(Object)["n"] != 2.0 || len(imgs[1].(Object)["items"].([]any)) != 2 {
		t.Fatal(js(p.Main))
	}
}

func TestAIRetryTruncationAndMissingPrompts(t *testing.T) {
	var count atomic.Int32
	raw := "```json\n" + strings.Repeat("错误", 600) + "test-key"
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count.Add(1); aiReply(w, raw) }))
	defer ai.Close()
	f := newFixture(t, func(c *Config) { c.AIKey = "test-key"; c.AIBaseURL = ai.URL + "/v1"; c.VisionModel = "vision" })
	var out Object
	e := f.a.aiJSON(testActor(context.Background()), "vision", "test", "hello", &out)
	if e == nil || !strings.Contains(e.Error(), "前 500 字") || strings.Contains(e.Error(), "test-key") {
		t.Fatal(e)
	}
	if count.Load() != 2 {
		t.Fatal("must retry exactly once")
	}
	if len([]rune(e.Error())) > 550 {
		t.Fatal("reply not truncated")
	}
	p := Product{ID: 1}
	if e = f.a.classify(testActor(context.Background()), p); e == nil || !strings.Contains(e.Error(), "分拣.md") {
		t.Fatal(e)
	}
	if e = validateCopy(Object{"real": Object{"rows": []any{12.0}}}, Object{}, Object{}); e == nil {
		t.Fatal("bad nested type accepted")
	}
	if e = validateCopy(Object{}, Object{"imgs": []any{Object{"n": 1.0}, Object{"n": 1.0}}}, Object{}); e == nil {
		t.Fatal("duplicate main image numbers accepted")
	}
	// Transient transport/status failure succeeds on the second attempt.
	count.Store(0)
	okAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		aiReply(w, "```json\n{\"ok\":true}\n```")
	}))
	defer okAI.Close()
	f.a.Cfg.AIBaseURL = okAI.URL + "/v1"
	if e = f.a.aiJSON(testActor(context.Background()), "vision", "test", "hello", &out); e != nil || out["ok"] != true || count.Load() != 2 {
		t.Fatal(e, out, count.Load())
	}
}
func TestWorkerPoolAndSameProductExclusion(t *testing.T) {
	entered := make(chan struct{}, 10)
	release := make(chan struct{})
	var active, maxActive atomic.Int32
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maxActive.Load(); n > old; old = maxActive.Load() {
			if maxActive.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		aiReply(w, `{"photos":[]}`)
	}))
	defer func() { close(release); ai.Close() }()
	f := newFixture(t, func(c *Config) {
		c.AIKey = "key"
		c.AIBaseURL = ai.URL + "/v1"
		c.VisionModel = "vision"
		c.Workers = 2
	})
	writePrompts(t, f.a)
	f.login(t, "admin")
	ids := []int64{}
	jobs := []int64{}
	for i := 0; i < 3; i++ {
		id := f.product(t)
		ids = append(ids, id)
		f.upload(t, id, "x.png", photoBytes(t, 24, 24), 201)
		j := f.req(t, "POST", fmt.Sprintf("/api/listings/%d/run?step=classify", id), nil, 202)["job"].(Object)
		jobs = append(jobs, int64(j["id"].(float64)))
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("workers did not start")
		}
	}
	select {
	case <-entered:
		t.Fatal("more than two workers running")
	case <-time.After(100 * time.Millisecond):
	}
	f.req(t, "POST", fmt.Sprintf("/api/listings/%d/run", ids[0]), nil, 409)
	f.req(t, "PATCH", fmt.Sprintf("/api/products/%d", ids[0]), Object{"name": "busy"}, 409)
	// Send three permits without closing, keeping cleanup order deterministic.
	for i := 0; i < 3; i++ {
		release <- struct{}{}
	}
	for _, id := range jobs {
		j := waitJob(t, f.a, id)
		if j.Status != "done" {
			t.Fatal(j)
		}
	}
	if maxActive.Load() != 2 {
		t.Fatal(maxActive.Load())
	}
}
