package copywriting_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/copywriting"
	"xstudio/internal/modules/pipeline"
	"xstudio/internal/modules/products"
)

type Object = core.Object

type testEnv struct {
	c      *core.Core
	s      *httptest.Server
	client *http.Client
}

func newTestEnv(t *testing.T, cfgFn func(*core.Config)) *testEnv {
	t.Helper()
	dir := t.TempDir()
	cfg := core.Config{
		Listen:        "127.0.0.1:0",
		DataDir:       filepath.Join(dir, "data"),
		PromptDir:     filepath.Join(dir, "prompts"),
		TemplateDir:   filepath.Join(dir, "templates"),
		AdminUser:     "admin",
		AdminPassword: "AdminPassword123!",
		SessionSecret: "0123456789abcdef0123456789abcdef",
		AIKey:         "fake-key",
		VisionModel:   "vision",
		TextModel:     "text",
		Workers:       2,
	}
	if cfgFn != nil {
		cfgFn(&cfg)
	}
	writeTestPrompts(t, cfg.PromptDir)
	sampleDir := filepath.Join(cfg.TemplateDir, "详情页模板", "商品", "示例款")
	if err := os.MkdirAll(sampleDir, 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(sampleDir, "内容.js"), []byte("window.PRODUCT={};"), 0600)
	_ = os.WriteFile(filepath.Join(sampleDir, "主图.js"), []byte("window.MAIN={};"), 0600)
	web := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	slots := []byte(`{"A1":{"cat":"white","desc":"白底主图"},"B1":{"cat":"ear","desc":"上耳实拍"}}`)
	c, err := core.New(cfg, web, slots)
	if err != nil {
		t.Fatal(err)
	}
	cw := copywriting.New(c)
	cw.ScanInterval = 20 * time.Millisecond
	if err := c.Start(accounts.New(c), products.New(c), assets.New(c), pipeline.New(c), cw); err != nil {
		_ = c.Close()
		t.Fatal(err)
	}
	s := httptest.NewServer(c.Handler())
	c.SetBaseURL(s.URL)
	t.Cleanup(func() {
		s.Close()
		_ = c.Close()
	})
	jar, _ := cookiejar.New(nil)
	return &testEnv{
		c:      c,
		s:      s,
		client: &http.Client{Jar: jar, Timeout: 15 * time.Second},
	}
}

func writeTestPrompts(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"分拣.md":    "按编号分拣，输出 JSON",
		"文案.md":    "按要求生成 content/main/listing JSON",
		"文案-附加.md": "口吻：{{voice}}\n关键词：{{keywords}}\n禁用词：{{banned}}\n附加要求：{{instructions}}",
		"文案-改写.md": "你是电商文案编辑，负责在不破坏结构的前提下按指令局部改写已有文案。只输出合法 JSON。",
		"合规审查.md":  "审查以下文案 JSON 并返回 issues 数组",
		"违禁词.txt":  "# 违禁词\n保证招财\n疗愈\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *testEnv) req(t *testing.T, method, path string, body any, wantStatus int) Object {
	t.Helper()
	var rBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rBody = bytes.NewReader(b)
	} else {
		rBody = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, e.s.URL+path, rBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Requested-With", "fetch")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out Object
	_ = json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != wantStatus {
		t.Fatalf("%s %s: status=%d want=%d body=%v", method, path, res.StatusCode, wantStatus, out)
	}
	return out
}

func (e *testEnv) login(t *testing.T, username, password string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	e.client.Jar = jar
	e.req(t, "POST", "/api/login", Object{"username": username, "password": password}, 200)
}

func (e *testEnv) waitJob(t *testing.T, jobID int64) Object {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res := e.req(t, "GET", fmt.Sprintf("/api/jobs/%d", jobID), nil, 200)
		job, _ := res["job"].(Object)
		st, _ := job["status"].(string)
		if st == "done" || st == "failed" {
			return job
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("job %d timed out", jobID)
	return nil
}

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, color.NRGBA{220, 200, 180, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, im); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (e *testEnv) uploadPhoto(t *testing.T, productID int64, filename string) int64 {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("files", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(makePNG(t, 80, 80))
	_ = mw.Close()
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/products/%d/assets", e.s.URL, productID), &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 201 {
		t.Fatalf("upload status=%d", res.StatusCode)
	}
	var out Object
	_ = json.NewDecoder(res.Body).Decode(&out)
	photos := out["items"].([]any)
	return int64(photos[0].(Object)["id"].(float64))
}

func (e *testEnv) roleIDByKey(t *testing.T, key string) int64 {
	t.Helper()
	roles := e.req(t, "GET", "/api/roles", nil, 200)["items"].([]any)
	for _, item := range roles {
		m := item.(Object)
		if m["key"] == key {
			return int64(m["id"].(float64))
		}
	}
	t.Fatalf("role %q not found", key)
	return 0
}

func TestPermissionsAndShopScope(t *testing.T) {
	env := newTestEnv(t, nil)
	env.login(t, "admin", "AdminPassword123!")

	// Create shop 2
	shop2Res := env.req(t, "POST", "/api/shops", Object{"name": "二号店", "platform": "taobao", "language": "zh-CN"}, 201)
	shop2ID := int64(shop2Res["shop"].(Object)["id"].(float64))

	// Create product + listing in shop 1 and shop 2
	p1 := env.req(t, "POST", "/api/products", Object{"name": "一号店耳环", "shop_ids": []int64{1}}, 201)["product"].(Object)
	l1ID := int64(p1["listings"].([]any)[0].(Object)["id"].(float64))
	p2 := env.req(t, "POST", "/api/products", Object{"name": "二号店耳环", "shop_ids": []int64{shop2ID}}, 201)["product"].(Object)
	l2ID := int64(p2["listings"].([]any)[0].(Object)["id"].(float64))

	// Save a copy version in shop 1 and shop 2
	v1Res := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies", l1ID), Object{
		"content": Object{"name": "测试一"},
		"main":    Object{},
		"meta":    Object{"title": "一号店标题"},
		"note":    "v1",
	}, 201)
	c1ID := int64(v1Res["copy"].(Object)["id"].(float64))

	v2Res := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies", l2ID), Object{
		"content": Object{"name": "测试二"},
		"main":    Object{},
		"meta":    Object{"title": "二号店标题"},
		"note":    "v1",
	}, 201)
	c2ID := int64(v2Res["copy"].(Object)["id"].(float64))

	// Add keyword in shop 2
	kw2Res := env.req(t, "POST", "/api/keywords", Object{"shop_id": shop2ID, "word": "专属词", "grp": "品类词", "weight": 80}, 201)
	kw2ID := int64(kw2Res["keyword"].(Object)["id"].(float64))

	// Create users:
	// 1. photographer (no copy.view, no copy.edit)
	env.req(t, "POST", "/api/users", Object{
		"username": "photo_user",
		"password": "Password123!",
		"role_id":  env.roleIDByKey(t, "photographer"),
		"shop_ids": []int64{1},
	}, 201)
	// 2. designer (has copy.view, no copy.edit)
	env.req(t, "POST", "/api/users", Object{
		"username": "designer_user",
		"password": "Password123!",
		"role_id":  env.roleIDByKey(t, "designer"),
		"shop_ids": []int64{1},
	}, 201)
	// 3. operator assigned only to shop 1
	env.req(t, "POST", "/api/users", Object{
		"username": "op_shop1",
		"password": "Password123!",
		"role_id":  env.roleIDByKey(t, "operator"),
		"shop_ids": []int64{1},
	}, 201)

	// 1. User without copy.view -> 403
	env.login(t, "photo_user", "Password123!")
	env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", l1ID), nil, 403)
	env.req(t, "GET", fmt.Sprintf("/api/copies/%d", c1ID), nil, 403)
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/check", c1ID), Object{}, 403)
	env.req(t, "GET", "/api/keywords", nil, 403)
	env.req(t, "GET", "/api/shops/1/voice", nil, 403)

	// 2. User with copy.view but without copy.edit -> 200 on read/check, 403 on write
	env.login(t, "designer_user", "Password123!")
	env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", l1ID), nil, 200)
	env.req(t, "GET", fmt.Sprintf("/api/copies/%d", c1ID), nil, 200)
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/check?ai=0", c1ID), Object{}, 200)
	env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies", l1ID), Object{"content": Object{}, "main": Object{}, "meta": Object{}}, 403)
	env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies/generate", l1ID), Object{}, 403)
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/apply", c1ID), Object{}, 403)
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/rewrite", c1ID), Object{"scope": "title", "instructions": "改"}, 403)
	env.req(t, "POST", "/api/keywords", Object{"word": "测试词"}, 403)
	env.req(t, "POST", "/api/keywords/import", Object{"lines": "测试词"}, 403)
	env.req(t, "PATCH", fmt.Sprintf("/api/keywords/%d", kw2ID), Object{"weight": 60}, 403)
	env.req(t, "DELETE", fmt.Sprintf("/api/keywords/%d", kw2ID), nil, 403)
	env.req(t, "PUT", "/api/shops/1/voice", Object{"text": "温柔"}, 403)

	// 3. Shop 1 operator accessing Shop 2 resources -> 404
	env.login(t, "op_shop1", "Password123!")
	env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", l2ID), nil, 404)
	env.req(t, "GET", fmt.Sprintf("/api/copies/%d", c2ID), nil, 404)
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/apply", c2ID), Object{}, 404)
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/rewrite", c2ID), Object{"scope": "title", "instructions": "改"}, 404)
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/check", c2ID), Object{}, 404)
	env.req(t, "GET", fmt.Sprintf("/api/keywords?shop_id=%d", shop2ID), nil, 404)
	env.req(t, "POST", "/api/keywords", Object{"shop_id": shop2ID, "word": "越权词"}, 404)
	env.req(t, "POST", "/api/keywords/import", Object{"shop_id": shop2ID, "lines": "越权词"}, 404)
	env.req(t, "PATCH", fmt.Sprintf("/api/keywords/%d", kw2ID), Object{"weight": 10}, 404)
	env.req(t, "DELETE", fmt.Sprintf("/api/keywords/%d", kw2ID), nil, 404)
	env.req(t, "GET", fmt.Sprintf("/api/shops/%d/voice", shop2ID), nil, 404)
	env.req(t, "PUT", fmt.Sprintf("/api/shops/%d/voice", shop2ID), Object{"text": "越权"}, 404)
}

func TestCopyVersionFlowKeywordsVoiceAndPipelineSync(t *testing.T) {
	var lastPrompt atomic.Value
	var aiMode atomic.Value
	aiMode.Store("generate")
	blockCh := make(chan struct{})
	var shouldBlock atomic.Bool

	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Object
		_ = json.NewDecoder(r.Body).Decode(&req)
		msgs, _ := req["messages"].([]any)
		var promptText strings.Builder
		for _, m := range msgs {
			msg, _ := m.(Object)
			if s, ok := msg["content"].(string); ok {
				promptText.WriteString(s)
				promptText.WriteString("\n")
			} else if arr, ok := msg["content"].([]any); ok {
				for _, part := range arr {
					if pm, ok := part.(Object); ok {
						if tStr, ok := pm["text"].(string); ok {
							promptText.WriteString(tStr)
							promptText.WriteString("\n")
						}
					}
				}
			}
		}
		lastPrompt.Store(promptText.String())
		if shouldBlock.Load() {
			<-blockCh
		}
		mode, _ := aiMode.Load().(string)
		var reply Object
		switch mode {
		case "generate":
			reply = Object{
				"content": Object{
					"name":     "AI法式巴洛克珍珠",
					"oneLiner": "天然淡水珍珠原生态光泽",
					"details": []Object{
						{"photo": Object{"f": "B1.jpg"}, "title": "温润光泽", "body": "自然流淌的珠光"},
					},
					"photos": Object{"hero": Object{"f": "B1.jpg"}},
				},
				"main": Object{"imgs": []Object{{"n": 1, "layout": "photo", "f": "B1.jpg"}}},
				"listing": Object{
					"title":      "AI生成法式复古珍珠耳环",
					"title_alts": []string{"AI备选标题一"},
					"sku":        []Object{{"name": "925银针", "price": 129}},
				},
			}
		case "rewrite_title":
			// Even if AI tries to modify content.oneLiner or content.details, scope="title" must keep original content!
			reply = Object{
				"content": Object{
					"name":     "被AI擅自修改的名称",
					"oneLiner": "这不该生效的一句话卖点",
					"details": []Object{
						{"photo": Object{"f": "B1.jpg"}, "title": "被篡改卖点", "body": "这不该生效"},
					},
				},
				"main": Object{"imgs": []Object{{"n": 1, "layout": "photo", "f": "B1.jpg"}}},
				"listing": Object{
					"title":      "七夕限定法式巴洛克珍珠耳环",
					"title_alts": []string{"七夕备选标题"},
				},
			}
		case "pipeline_copy":
			reply = Object{
				"content": Object{
					"name":   "流水线珍珠耳环",
					"photos": Object{"hero": Object{"f": "B1.jpg"}},
				},
				"main": Object{"imgs": []Object{{"n": 1, "layout": "photo", "f": "B1.jpg"}}},
				"listing": Object{
					"title": "流水线自动生成标题",
					"sku":   []Object{{"name": "默认款", "price": 99}},
				},
			}
		}
		b, _ := json.Marshal(reply)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Object{
			"choices": []Object{{"message": Object{"content": string(b)}}},
		})
	}))
	defer ai.Close()

	env := newTestEnv(t, func(c *core.Config) {
		c.AIBaseURL = ai.URL + "/v1"
	})
	env.login(t, "admin", "AdminPassword123!")

	// 1. Keywords batch import, deduplication, and weight update
	imp1 := env.req(t, "POST", "/api/keywords/import", Object{
		"shop_id": 1,
		"lines":   "巴洛克珍珠\t品类词\t90\n复古耳环\t场景词\t70\n巴洛克珍珠\t品类词\t95\n奢华绝顶\t禁用词\t50",
	}, 200)
	if int(imp1["imported"].(float64)) != 4 {
		t.Fatalf("expected 4 processed lines, got %v", imp1)
	}
	kws := env.req(t, "GET", "/api/keywords?shop_id=1", nil, 200)["items"].([]any)
	if len(kws) != 3 {
		t.Fatalf("expected 3 unique keywords after deduplication, got %d", len(kws))
	}
	var baroqueWeight float64
	for _, item := range kws {
		m := item.(Object)
		if m["word"] == "巴洛克珍珠" {
			baroqueWeight = m["weight"].(float64)
		}
	}
	if baroqueWeight != 95 {
		t.Fatalf("expected updated weight 95, got %v", baroqueWeight)
	}

	// 2. Configure shop 1 voice profile
	env.req(t, "PUT", "/api/shops/1/voice", Object{
		"text":     "独立设计师高定珠宝，克制温润",
		"examples": []string{"每一颗都有自己的晨露光泽"},
	}, 200)

	// 3. Create product, upload photo, set code=B1, and run retouch so 照片/B1.jpg exists
	prod := env.req(t, "POST", "/api/products", Object{"name": "测试珍珠耳饰", "shop_ids": []int64{1}}, 201)["product"].(Object)
	pid := int64(prod["id"].(float64))
	lid := int64(prod["listings"].([]any)[0].(Object)["id"].(float64))
	photoID := env.uploadPhoto(t, pid, "b1.png")
	env.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", photoID), Object{"code": "B1"}, 200)
	retJob := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/run?step=retouch", lid), nil, 202)["job"].(Object)
	if j := env.waitJob(t, int64(retJob["id"].(float64))); j["status"] != "done" {
		t.Fatalf("retouch failed: %v", j)
	}

	// 4. Saving version referencing nonexistent photo -> 400
	env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies", lid), Object{
		"content": Object{"photos": Object{"hero": Object{"f": "nonexistent.jpg"}}},
		"main":    Object{},
		"meta":    Object{"title": "坏图测试"},
	}, 400)

	// 5. Manually save v1 and apply it
	v1Res := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies", lid), Object{
		"content": Object{
			"name":     "手动版珍珠",
			"oneLiner": "手动一句话卖点",
			"photos":   Object{"hero": Object{"f": "B1.jpg"}},
		},
		"main": Object{"imgs": []Object{{"n": 1, "layout": "photo", "f": "B1.jpg"}}},
		"meta": Object{"title": "手动版标题v1"},
		"note": "手改第一版",
	}, 201)["copy"].(Object)
	v1ID := int64(v1Res["id"].(float64))
	if v1Res["source"] != "manual" || v1Res["applied_at"] != "" {
		t.Fatalf("unexpected v1: %v", v1Res)
	}
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/apply", v1ID), nil, 200)

	// 6. AI generate v2 (saved as new version, NOT auto-applied)
	aiMode.Store("generate")
	genJob := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies/generate", lid), Object{
		"instructions": "突出法式通勤感",
	}, 202)["job"].(Object)
	if j := env.waitJob(t, int64(genJob["id"].(float64))); j["status"] != "done" {
		t.Fatalf("copy.generate failed: %v", j)
	}
	promptSent, _ := lastPrompt.Load().(string)
	for _, expected := range []string{"独立设计师高定珠宝", "每一颗都有自己的晨露光泽", "巴洛克珍珠", "奢华绝顶", "突出法式通勤感"} {
		if !strings.Contains(promptSent, expected) {
			t.Fatalf("AI prompt missing %q; prompt=%s", expected, promptSent)
		}
	}

	listResAfterGen := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", lid), nil, 200)
	listAfterGen := listResAfterGen["items"].([]any)
	if len(listAfterGen) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(listAfterGen))
	}
	// v2 was NOT auto-applied, so current is still v1ID
	if int64(listResAfterGen["current"].(float64)) != v1ID {
		t.Fatalf("expected current to remain v1 (%d), got %v", v1ID, listResAfterGen["current"])
	}
	v2Summary := listAfterGen[0].(Object)
	v2ID := int64(v2Summary["id"].(float64))
	if v2Summary["source"] != "ai_generate" {
		t.Fatalf("unexpected v2 summary: %v", v2Summary)
	}

	// 7. Partial rewrite v3 (scope="title"): verify title changes while selling fields are untouched
	aiMode.Store("rewrite_title")
	rewJob := env.req(t, "POST", fmt.Sprintf("/api/copies/%d/rewrite", v2ID), Object{
		"scope":        "title",
		"instructions": "改成七夕礼盒标题",
	}, 202)["job"].(Object)
	if j := env.waitJob(t, int64(rewJob["id"].(float64))); j["status"] != "done" {
		t.Fatalf("copy.rewrite failed: %v", j)
	}
	listAfterRew := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", lid), nil, 200)["items"].([]any)
	v3Summary := listAfterRew[0].(Object)
	v3ID := int64(v3Summary["id"].(float64))
	v3Detail := env.req(t, "GET", fmt.Sprintf("/api/copies/%d", v3ID), nil, 200)["copy"].(Object)
	if v3Detail["meta"].(Object)["title"] != "七夕限定法式巴洛克珍珠耳环" {
		t.Fatalf("rewrite title did not update: %v", v3Detail["meta"])
	}
	detailsArr := v3Detail["content"].(Object)["details"].([]any)
	if detailsArr[0].(Object)["title"] != "温润光泽" || v3Detail["content"].(Object)["oneLiner"] != "天然淡水珍珠原生态光泽" || v3Detail["content"].(Object)["name"] != "AI法式巴洛克珍珠" {
		t.Fatalf("scope=title leaked changes into content: %v", v3Detail["content"])
	}

	// 8. Apply v3 then apply v1 -> verify listings.content/main/meta and current ID
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/apply", v3ID), nil, 200)
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/apply", v1ID), nil, 200)
	listingNow := env.req(t, "GET", fmt.Sprintf("/api/listings/%d", lid), nil, 200)["listing"].(Object)
	if listingNow["meta"].(Object)["title"] != "手动版标题v1" || listingNow["content"].(Object)["name"] != "手动版珍珠" {
		t.Fatalf("apply v1 did not revert listing: %v", listingNow)
	}
	copiesNow := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", lid), nil, 200)
	if int64(copiesNow["current"].(float64)) != v1ID {
		t.Fatalf("expected current=%d, got %v", v1ID, copiesNow["current"])
	}

	// 9. Active job exclusion on apply -> 409
	shouldBlock.Store(true)
	aiMode.Store("generate")
	busyJob := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies/generate", lid), Object{}, 202)["job"].(Object)
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/apply", v2ID), nil, 409)
	shouldBlock.Store(false)
	close(blockCh)
	env.waitJob(t, int64(busyJob["id"].(float64)))

	// 10. Pipeline copy step auto-creates exactly one source='pipeline' version
	aiMode.Store("pipeline_copy")
	pipeJob := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/run?step=copy", lid), nil, 202)["job"].(Object)
	pipeJobID := int64(pipeJob["id"].(float64))
	if j := env.waitJob(t, pipeJobID); j["status"] != "done" {
		t.Fatalf("pipeline copy failed: %v", j)
	}
	copiesAfterPipe1 := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", lid), nil, 200)
	copiesAfterPipe2 := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", lid), nil, 200)
	items1 := copiesAfterPipe1["items"].([]any)
	items2 := copiesAfterPipe2["items"].([]any)
	if len(items1) != len(items2) {
		t.Fatalf("duplicate pipeline copy version created on repeated GET")
	}
	topCopy := items2[0].(Object)
	if topCopy["source"] != "pipeline" || int64(copiesAfterPipe2["current"].(float64)) != int64(topCopy["id"].(float64)) {
		t.Fatalf("expected top version to be current pipeline version, got %v", topCopy)
	}
}

func TestComplianceCheckAndAIFallback(t *testing.T) {
	var failAI atomic.Bool
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failAI.Load() {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("internal error"))
			return
		}
		reply := Object{
			"issues": []Object{
				{
					"level":   "warn",
					"field":   "content.oneLiner",
					"text":    "国家级精品",
					"reason":  "涉嫌使用极限用语",
					"suggest": "匠心臻品",
				},
			},
		}
		b, _ := json.Marshal(reply)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Object{
			"choices": []Object{{"message": Object{"content": string(b)}}},
		})
	}))
	defer ai.Close()

	env := newTestEnv(t, func(c *core.Config) {
		c.AIBaseURL = ai.URL + "/v1"
	})
	env.login(t, "admin", "AdminPassword123!")

	// Add shop-level banned word
	env.req(t, "POST", "/api/keywords", Object{
		"shop_id": 1,
		"word":    "包过检",
		"grp":     "禁用词",
		"weight":  100,
	}, 201)

	prod := env.req(t, "POST", "/api/products", Object{"name": "合规测试商品", "shop_ids": []int64{1}}, 201)["product"].(Object)
	lid := int64(prod["listings"].([]any)[0].(Object)["id"].(float64))

	vRes := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies", lid), Object{
		"content": Object{
			"name":     "合规耳环",
			"oneLiner": "国家级精品手作",
			"story":    "支持包过检认证",
		},
		"main": Object{},
		"meta": Object{"title": "保证招财好运珍珠耳环"},
	}, 201)["copy"].(Object)
	cid := int64(vRes["id"].(float64))

	// 1. Check with both local + shop banned words + AI risks
	chk1 := env.req(t, "POST", fmt.Sprintf("/api/copies/%d/check", cid), nil, 200)
	issues1 := chk1["issues"].([]any)
	fields := map[string]string{}
	for _, item := range issues1 {
		m := item.(Object)
		f, _ := m["field"].(string)
		txt, _ := m["text"].(string)
		fields[f] = txt
	}
	if fields["meta.title"] != "保证招财" {
		t.Fatalf("missing local banned word at meta.title: %v", issues1)
	}
	if fields["content.story"] != "包过检" {
		t.Fatalf("missing shop banned word at content.story: %v", issues1)
	}
	if fields["content.oneLiner"] != "国家级精品" {
		t.Fatalf("missing AI issue at content.oneLiner: %v", issues1)
	}

	// 2. When AI fails, endpoint still returns 200 with local results and warn issue "AI 审查暂不可用"
	failAI.Store(true)
	chk2 := env.req(t, "POST", fmt.Sprintf("/api/copies/%d/check", cid), nil, 200)
	issues2 := chk2["issues"].([]any)
	if len(issues2) < 3 {
		t.Fatalf("expected 2 local issues + 1 AI fallback warn issue, got %v", issues2)
	}
	foundWarn := false
	for _, item := range issues2 {
		m := item.(Object)
		if m["level"] == "warn" && m["reason"] == "AI 审查暂不可用" {
			foundWarn = true
		}
	}
	if !foundWarn {
		t.Fatalf("expected AI fallback warn issue, got %v", issues2)
	}
}

func TestRewriteScopesWhitelistMergeAndCompliance(t *testing.T) {
	var lastUserReq atomic.Value
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Object
		_ = json.NewDecoder(r.Body).Decode(&req)
		msgs, _ := req["messages"].([]any)
		if len(msgs) >= 2 {
			if m1, ok := msgs[1].(Object); ok {
				if s, ok := m1["content"].(string); ok {
					var parsed Object
					_ = json.Unmarshal([]byte(s), &parsed)
					lastUserReq.Store(parsed)
				}
			}
		}
		// Fake AI intentionally modifies both in-scope and out-of-scope fields across content, main, and meta,
		// adds extra array elements, adds nonexistent fields, and appends a warn compliance item.
		reply := Object{
			"content": Object{
				"name":     "AI新名称",
				"nameEn":   "AI NEW NAME",
				"oneLiner": "AI新卖点",
				"tags":     []string{"AI新标签"},
				"real": Object{
					"note": "AI新实拍说明",
					"lead": "被篡改实拍导语",
				},
				"detailsLead": "AI新细节导语",
				"details": []Object{
					{
						"photo": Object{"f": "HACKED.jpg", "need": "被篡改need"},
						"title": "AI新细节标题",
						"text":  "AI新细节正文",
					},
					{
						"title": "多余的第二个细节",
						"text":  "多余正文",
					},
				},
				"wear": Object{
					"lead": "AI新佩戴导语",
					"big":  Object{"f": "HACKED.jpg"},
				},
				"sizeLead":    "AI新尺码导语",
				"relatedLead": "AI新搭配导语",
				"story":       "被篡改的品牌故事",
				"storySign":   "被篡改的落款",
				"sticker":     "被新增的贴纸字段",
				"params":      [][]string{{"材质", "假黄金"}},
				"photos":      Object{"hero": Object{"f": "B1.jpg"}},
			},
			"main": Object{
				"imgs": []Object{
					{
						"n":      99,
						"layout": "hacked",
						"f":      "HACKED.jpg",
						"text":   "AI新主图文案",
						"label":  "AI新主图标签",
					},
					{
						"n":      2,
						"layout": "photo",
						"f":      "B1.jpg",
						"text":   "多余主图",
					},
				},
			},
			"meta": Object{
				"title":      "AI新标题",
				"title_alts": []string{"AI新备选1", "AI新备选2"},
				"attrs":      [][]string{{"材质", "被篡改材质"}},
				"sku":        []Object{{"name": "被篡改SKU", "price": 9999}},
				"compliance": []Object{
					{"level": "warn", "text": "部分夸张要求未照做，因违反广告法"},
				},
			},
		}
		b, _ := json.Marshal(reply)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Object{
			"choices": []Object{{"message": Object{"content": string(b)}}},
		})
	}))
	defer ai.Close()

	env := newTestEnv(t, func(c *core.Config) {
		c.AIBaseURL = ai.URL + "/v1"
	})
	env.login(t, "admin", "AdminPassword123!")

	// Configure shop 1 voice and keywords (including banned words)
	env.req(t, "PUT", "/api/shops/1/voice", Object{
		"text":     "法式克制温润",
		"examples": []string{"晨露般的光泽"},
	}, 200)
	env.req(t, "POST", "/api/keywords/import", Object{
		"shop_id": 1,
		"lines":   "巴洛克珍珠\t品类词\t90\n顶级皇家\t禁用词\t100",
	}, 200)

	// Create product + listing + retouched photo B1.jpg
	prod := env.req(t, "POST", "/api/products", Object{"name": "白名单改写测试耳环", "shop_ids": []int64{1}}, 201)["product"].(Object)
	pid := int64(prod["id"].(float64))
	lid := int64(prod["listings"].([]any)[0].(Object)["id"].(float64))
	photoID := env.uploadPhoto(t, pid, "b1.png")
	env.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", photoID), Object{"code": "B1"}, 200)
	retJob := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/run?step=retouch", lid), nil, 202)["job"].(Object)
	if j := env.waitJob(t, int64(retJob["id"].(float64))); j["status"] != "done" {
		t.Fatalf("retouch failed: %v", j)
	}

	// Create original version with existing whitelisted fields and out-of-scope fields
	origCreate := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies", lid), Object{
		"content": Object{
			"name":     "原名称",
			"nameEn":   "ORIG NAME",
			"oneLiner": "原一句话卖点",
			"tags":     []string{"原标签1", "原标签2"},
			"real": Object{
				"note": "原实拍说明",
				"lead": "原实拍导语",
			},
			"detailsLead": "原细节导语",
			"details": []Object{
				{
					"photo": Object{"f": "B1.jpg", "need": "原需求"},
					"title": "原细节标题",
					"text":  "原细节正文",
				},
			},
			"wear": Object{
				"lead": "原佩戴导语",
				"big":  Object{"f": "B1.jpg"},
			},
			"sizeLead":    "原尺码导语",
			"relatedLead": "原搭配导语",
			"story":       "原品牌故事",
			"storySign":   "原落款",
			"params":      [][]string{{"材质", "925银"}},
			"photos":      Object{"hero": Object{"f": "B1.jpg"}},
		},
		"main": Object{
			"imgs": []Object{
				{
					"n":      1,
					"layout": "photo",
					"f":      "B1.jpg",
					"text":   "原主图文案",
					"label":  "原主图标签",
				},
			},
		},
		"meta": Object{
			"title":      "原标题",
			"title_alts": []string{"原备选标题1"},
			"attrs":      [][]string{{"材质", "淡水珍珠"}},
			"sku":        []Object{{"name": "银针款", "price": 129}},
			"compliance": []Object{{"level": "info", "text": "原有提示"}},
		},
	}, 201)["copy"].(Object)
	origID := int64(origCreate["id"].(float64))
	origCopy := env.req(t, "GET", fmt.Sprintf("/api/copies/%d", origID), nil, 200)["copy"].(Object)

	checkReqPayload := func(t *testing.T, wantScope string) {
		t.Helper()
		sent, ok := lastUserReq.Load().(Object)
		if !ok || sent == nil {
			t.Fatalf("expected JSON request payload sent to AI")
		}
		if sent["scope"] != wantScope || sent["instructions"] != "按要求改写" {
			t.Fatalf("unexpected scope/instructions in AI payload: %v", sent)
		}
		copyObj, ok := sent["copy"].(Object)
		if !ok || copyObj["content"] == nil || copyObj["main"] == nil || copyObj["meta"] == nil {
			t.Fatalf("expected copy.{content,main,meta} in AI payload, got %v", sent["copy"])
		}
		voiceStr, _ := sent["voice"].(string)
		if !strings.Contains(voiceStr, "法式克制温润") || !strings.Contains(voiceStr, "晨露般的光泽") {
			t.Fatalf("expected voice with examples in AI payload, got %q", voiceStr)
		}
		if !strings.Contains(core.JS(sent["keywords"]), "巴洛克珍珠") {
			t.Fatalf("expected keywords in AI payload, got %v", sent["keywords"])
		}
		if !strings.Contains(core.JS(sent["banned"]), "顶级皇家") {
			t.Fatalf("expected shop banned words in AI payload, got %v", sent["banned"])
		}
	}

	checkComplianceWarn := func(t *testing.T, meta Object) {
		t.Helper()
		compJS := core.JS(meta["compliance"])
		if !strings.Contains(compJS, "原有提示") || !strings.Contains(compJS, "部分夸张要求未照做，因违反广告法") {
			t.Fatalf("expected original compliance + model appended warn compliance, got %s", compJS)
		}
	}

	// 1. Scope = "title"
	jTitle := env.req(t, "POST", fmt.Sprintf("/api/copies/%d/rewrite", origID), Object{
		"scope":        "title",
		"instructions": "按要求改写",
	}, 202)["job"].(Object)
	doneTitle := env.waitJob(t, int64(jTitle["id"].(float64)))
	if doneTitle["status"] != "done" || !strings.Contains(doneTitle["message"].(string), "已忽略") {
		t.Fatalf("expected done job with ignored count message for title scope, got %v", doneTitle)
	}
	checkReqPayload(t, "title")
	copiesAfterTitle := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", lid), nil, 200)["items"].([]any)
	cTitle := env.req(t, "GET", fmt.Sprintf("/api/copies/%d", int64(copiesAfterTitle[0].(Object)["id"].(float64))), nil, 200)["copy"].(Object)

	// In-scope title fields changed
	if cTitle["meta"].(Object)["title"] != "AI新标题" || core.JS(cTitle["meta"].(Object)["title_alts"]) != `["AI新备选1","AI新备选2"]` {
		t.Fatalf("scope=title did not update title/title_alts: %v", cTitle["meta"])
	}
	checkComplianceWarn(t, cTitle["meta"].(Object))
	// Out-of-scope content & main must be byte-for-byte identical to origCopy
	if core.JS(cTitle["content"]) != core.JS(origCopy["content"]) {
		t.Fatalf("scope=title modified content!\ngot:  %s\nwant: %s", core.JS(cTitle["content"]), core.JS(origCopy["content"]))
	}
	if core.JS(cTitle["main"]) != core.JS(origCopy["main"]) {
		t.Fatalf("scope=title modified main!\ngot:  %s\nwant: %s", core.JS(cTitle["main"]), core.JS(origCopy["main"]))
	}
	// Restore title, title_alts, compliance on meta and verify byte-for-byte identical to origCopy["meta"]
	cTitleMetaRestored := cloneTestMap(cTitle["meta"].(Object))
	origMeta := origCopy["meta"].(Object)
	cTitleMetaRestored["title"] = origMeta["title"]
	cTitleMetaRestored["title_alts"] = origMeta["title_alts"]
	cTitleMetaRestored["compliance"] = origMeta["compliance"]
	if core.JS(cTitleMetaRestored) != core.JS(origMeta) {
		t.Fatalf("scope=title modified out-of-scope meta fields!\ngot:  %s\nwant: %s", core.JS(cTitleMetaRestored), core.JS(origMeta))
	}

	// 2. Scope = "selling"
	jSelling := env.req(t, "POST", fmt.Sprintf("/api/copies/%d/rewrite", origID), Object{
		"scope":        "selling",
		"instructions": "按要求改写",
	}, 202)["job"].(Object)
	doneSelling := env.waitJob(t, int64(jSelling["id"].(float64)))
	if doneSelling["status"] != "done" || !strings.Contains(doneSelling["message"].(string), "已忽略") {
		t.Fatalf("expected done job with ignored count message for selling scope, got %v", doneSelling)
	}
	checkReqPayload(t, "selling")
	copiesAfterSelling := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", lid), nil, 200)["items"].([]any)
	cSelling := env.req(t, "GET", fmt.Sprintf("/api/copies/%d", int64(copiesAfterSelling[0].(Object)["id"].(float64))), nil, 200)["copy"].(Object)

	sContent := cSelling["content"].(Object)
	sMain := cSelling["main"].(Object)
	sMeta := cSelling["meta"].(Object)
	sDetails := sContent["details"].([]any)
	sImgs := sMain["imgs"].([]any)
	if sContent["oneLiner"] != "AI新卖点" ||
		core.JS(sContent["tags"]) != `["AI新标签"]` ||
		sContent["real"].(Object)["note"] != "AI新实拍说明" ||
		sContent["detailsLead"] != "AI新细节导语" ||
		len(sDetails) != 1 ||
		sDetails[0].(Object)["title"] != "AI新细节标题" ||
		sDetails[0].(Object)["text"] != "AI新细节正文" ||
		sContent["wear"].(Object)["lead"] != "AI新佩戴导语" ||
		sContent["sizeLead"] != "AI新尺码导语" ||
		sContent["relatedLead"] != "AI新搭配导语" ||
		len(sImgs) != 1 ||
		sImgs[0].(Object)["text"] != "AI新主图文案" ||
		sImgs[0].(Object)["label"] != "AI新主图标签" {
		t.Fatalf("scope=selling did not update expected selling fields: content=%s main=%s", core.JS(sContent), core.JS(sMain))
	}
	checkComplianceWarn(t, sMeta)

	// Restore only selling whitelisted fields and verify byte-for-byte identical to origCopy
	origContent := origCopy["content"].(Object)
	origMain := origCopy["main"].(Object)
	sContentRestored := cloneTestMap(sContent)
	sContentRestored["oneLiner"] = origContent["oneLiner"]
	sContentRestored["tags"] = origContent["tags"]
	sContentRestored["real"].(Object)["note"] = origContent["real"].(Object)["note"]
	sContentRestored["detailsLead"] = origContent["detailsLead"]
	sContentRestored["details"].([]any)[0].(Object)["title"] = origContent["details"].([]any)[0].(Object)["title"]
	sContentRestored["details"].([]any)[0].(Object)["text"] = origContent["details"].([]any)[0].(Object)["text"]
	sContentRestored["wear"].(Object)["lead"] = origContent["wear"].(Object)["lead"]
	sContentRestored["sizeLead"] = origContent["sizeLead"]
	sContentRestored["relatedLead"] = origContent["relatedLead"]
	if core.JS(sContentRestored) != core.JS(origContent) {
		t.Fatalf("scope=selling modified out-of-scope content fields!\ngot:  %s\nwant: %s", core.JS(sContentRestored), core.JS(origContent))
	}
	sMainRestored := cloneTestMap(sMain)
	sMainRestored["imgs"].([]any)[0].(Object)["text"] = origMain["imgs"].([]any)[0].(Object)["text"]
	sMainRestored["imgs"].([]any)[0].(Object)["label"] = origMain["imgs"].([]any)[0].(Object)["label"]
	if core.JS(sMainRestored) != core.JS(origMain) {
		t.Fatalf("scope=selling modified out-of-scope main fields!\ngot:  %s\nwant: %s", core.JS(sMainRestored), core.JS(origMain))
	}
	sMetaRestored := cloneTestMap(sMeta)
	sMetaRestored["compliance"] = origMeta["compliance"]
	if core.JS(sMetaRestored) != core.JS(origMeta) {
		t.Fatalf("scope=selling modified out-of-scope meta fields!\ngot:  %s\nwant: %s", core.JS(sMetaRestored), core.JS(origMeta))
	}

	// 3. Scope = "all"
	jAll := env.req(t, "POST", fmt.Sprintf("/api/copies/%d/rewrite", origID), Object{
		"scope":        "all",
		"instructions": "按要求改写",
	}, 202)["job"].(Object)
	doneAll := env.waitJob(t, int64(jAll["id"].(float64)))
	if doneAll["status"] != "done" || !strings.Contains(doneAll["message"].(string), "已忽略") {
		t.Fatalf("expected done job with ignored count message for all scope, got %v", doneAll)
	}
	checkReqPayload(t, "all")
	copiesAfterAll := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", lid), nil, 200)["items"].([]any)
	cAll := env.req(t, "GET", fmt.Sprintf("/api/copies/%d", int64(copiesAfterAll[0].(Object)["id"].(float64))), nil, 200)["copy"].(Object)

	aContent := cAll["content"].(Object)
	aMain := cAll["main"].(Object)
	aMeta := cAll["meta"].(Object)
	if aContent["name"] != "AI新名称" || aContent["nameEn"] != "AI NEW NAME" || aContent["oneLiner"] != "AI新卖点" || aMeta["title"] != "AI新标题" {
		t.Fatalf("scope=all did not update expected fields: content=%s meta=%s", core.JS(aContent), core.JS(aMeta))
	}
	checkComplianceWarn(t, aMeta)

	// Restore only all whitelisted fields and verify byte-for-byte identical to origCopy
	aContentRestored := cloneTestMap(aContent)
	aContentRestored["name"] = origContent["name"]
	aContentRestored["nameEn"] = origContent["nameEn"]
	aContentRestored["oneLiner"] = origContent["oneLiner"]
	aContentRestored["tags"] = origContent["tags"]
	aContentRestored["real"].(Object)["note"] = origContent["real"].(Object)["note"]
	aContentRestored["detailsLead"] = origContent["detailsLead"]
	aContentRestored["details"].([]any)[0].(Object)["title"] = origContent["details"].([]any)[0].(Object)["title"]
	aContentRestored["details"].([]any)[0].(Object)["text"] = origContent["details"].([]any)[0].(Object)["text"]
	aContentRestored["wear"].(Object)["lead"] = origContent["wear"].(Object)["lead"]
	aContentRestored["sizeLead"] = origContent["sizeLead"]
	aContentRestored["relatedLead"] = origContent["relatedLead"]
	if core.JS(aContentRestored) != core.JS(origContent) {
		t.Fatalf("scope=all modified out-of-scope content fields!\ngot:  %s\nwant: %s", core.JS(aContentRestored), core.JS(origContent))
	}
	aMainRestored := cloneTestMap(aMain)
	aMainRestored["imgs"].([]any)[0].(Object)["text"] = origMain["imgs"].([]any)[0].(Object)["text"]
	aMainRestored["imgs"].([]any)[0].(Object)["label"] = origMain["imgs"].([]any)[0].(Object)["label"]
	if core.JS(aMainRestored) != core.JS(origMain) {
		t.Fatalf("scope=all modified out-of-scope main fields!\ngot:  %s\nwant: %s", core.JS(aMainRestored), core.JS(origMain))
	}
	aMetaRestored := cloneTestMap(aMeta)
	aMetaRestored["title"] = origMeta["title"]
	aMetaRestored["title_alts"] = origMeta["title_alts"]
	aMetaRestored["compliance"] = origMeta["compliance"]
	if core.JS(aMetaRestored) != core.JS(origMeta) {
		t.Fatalf("scope=all modified out-of-scope meta fields!\ngot:  %s\nwant: %s", core.JS(aMetaRestored), core.JS(origMeta))
	}
}

func cloneTestMap(m Object) Object {
	var out Object
	b, _ := json.Marshal(m)
	_ = json.Unmarshal(b, &out)
	return out
}
