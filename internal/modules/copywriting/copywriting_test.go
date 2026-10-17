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
	if err := c.Start(accounts.New(c), products.New(c), assets.New(c), pipeline.New(c), copywriting.New(c)); err != nil {
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
		"文案-改写.md": "改写文案",
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
