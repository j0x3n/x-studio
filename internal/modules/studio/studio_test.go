package studio_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/copywriting"
	"xstudio/internal/modules/pipeline"
	"xstudio/internal/modules/products"
	"xstudio/internal/modules/studio"
)

type Object = core.Object

type testEnv struct {
	c      *core.Core
	s      *httptest.Server
	client *http.Client
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
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
		ChromePath:    pipeline.ChromePath(os.Getenv("CHROME_PATH")),
		Workers:       2,
	}
	copyDir(t, filepath.Join("..", "..", "..", "templates"), cfg.TemplateDir)
	if err := os.MkdirAll(cfg.PromptDir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"分拣.md":   "按编号分拣",
		"文案.md":   "生成文案",
		"违禁词.txt": "保证招财\n",
	} {
		_ = os.WriteFile(filepath.Join(cfg.PromptDir, name), []byte(content), 0600)
	}
	if cfgFn != nil {
		cfgFn(&cfg)
	}
	web := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	slots := []byte(`{"A1":{"cat":"white","desc":"白底主图"},"B1":{"cat":"ear","desc":"上耳实拍"},"C4_8mm":{"cat":"size","desc":"尺寸图"}}`)
	c, err := core.New(cfg, web, slots)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(accounts.New(c), products.New(c), assets.New(c), pipeline.New(c), copywriting.New(c), studio.New(c)); err != nil {
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
		client: &http.Client{Jar: jar, Timeout: 60 * time.Second},
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

func (e *testEnv) waitJob(t *testing.T, jobID int64) Object {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		res := e.req(t, "GET", fmt.Sprintf("/api/jobs/%d", jobID), nil, 200)
		job, _ := res["job"].(Object)
		st, _ := job["status"].(string)
		if st == "done" || st == "failed" {
			return job
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job %d timed out", jobID)
	return nil
}

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, color.NRGBA{210, 190, 175, 255})
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
	_, _ = fw.Write(makePNG(t, 120, 160))
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

func TestStudioPermissionsTemplatesAndShopScope(t *testing.T) {
	env := newTestEnv(t, nil)
	env.login(t, "admin", "AdminPassword123!")

	// Add a thumbnail for navy to test thumb_url and /render/详情页模板/缩略图/<key>.jpg
	thumbDir := filepath.Join(env.c.Cfg.TemplateDir, "详情页模板", "缩略图")
	_ = os.MkdirAll(thumbDir, 0700)
	var jpgBuf bytes.Buffer
	_ = jpeg.Encode(&jpgBuf, image.NewNRGBA(image.Rect(0, 0, 16, 16)), nil)
	_ = os.WriteFile(filepath.Join(thumbDir, "navy.jpg"), jpgBuf.Bytes(), 0600)

	// Create shop 2
	shop2Res := env.req(t, "POST", "/api/shops", Object{"name": "二号店", "platform": "taobao", "language": "zh-CN"}, 201)
	shop2ID := int64(shop2Res["shop"].(Object)["id"].(float64))

	// Create listings in shop 1 and shop 2
	p1 := env.req(t, "POST", "/api/products", Object{"name": "一店项链", "shop_ids": []int64{1}}, 201)["product"].(Object)
	l1ID := int64(p1["listings"].([]any)[0].(Object)["id"].(float64))
	p2 := env.req(t, "POST", "/api/products", Object{"name": "二店项链", "shop_ids": []int64{shop2ID}}, 201)["product"].(Object)
	l2ID := int64(p2["listings"].([]any)[0].(Object)["id"].(float64))

	// Verify GET /api/templates returns 5 detail templates and thumbnail URL
	tpls := env.req(t, "GET", "/api/templates?shop_id=1", nil, 200)
	detailTpls := tpls["detail"].([]any)
	if len(detailTpls) != 5 {
		t.Fatalf("expected 5 detail templates, got %d", len(detailTpls))
	}
	navyTpl := detailTpls[0].(Object)
	if navyTpl["key"] != "navy" || navyTpl["thumb_url"] != "/render/详情页模板/缩略图/navy.jpg" || navyTpl["is_default"] != true {
		t.Fatalf("unexpected navy template entry: %v", navyTpl)
	}
	thumbRes, err := env.client.Get(env.s.URL + "/render/详情页模板/缩略图/navy.jpg")
	if err != nil || thumbRes.StatusCode != 200 {
		t.Fatalf("thumbnail fetch failed: status=%v err=%v", thumbRes, err)
	}
	_ = thumbRes.Body.Close()

	// Create users:
	// - operator on shop 1 (has render.run, render.review, no template.manage, no export.no_ai_label)
	env.req(t, "POST", "/api/users", Object{
		"username": "op1",
		"password": "Password123!",
		"role_id":  env.roleIDByKey(t, "operator"),
		"shop_ids": []int64{1},
	}, 201)
	// - designer on shop 1 (has template.manage, render.run, no render.review, no export.no_ai_label)
	env.req(t, "POST", "/api/users", Object{
		"username": "des1",
		"password": "Password123!",
		"role_id":  env.roleIDByKey(t, "designer"),
		"shop_ids": []int64{1},
	}, 201)

	// 1. Operator cannot manage template prefs (403), cannot access shop 2 (404), cannot set ai_label=false (403)
	env.login(t, "op1", "Password123!")
	env.req(t, "PUT", "/api/shops/1/template-prefs", Object{"default_detail": "cream", "disabled": []string{}}, 403)
	env.req(t, "GET", fmt.Sprintf("/api/templates?shop_id=%d", shop2ID), nil, 404)
	env.req(t, "GET", fmt.Sprintf("/api/listings/%d/render", l2ID), nil, 404)
	env.req(t, "PUT", fmt.Sprintf("/api/listings/%d/render", l2ID), Object{"detail_template": "cream"}, 404)
	env.req(t, "PUT", fmt.Sprintf("/api/listings/%d/render", l1ID), Object{"ai_label": false}, 403)
	env.req(t, "PUT", fmt.Sprintf("/api/listings/%d/render", l1ID), Object{"ai_label": 0}, 403)
	env.req(t, "POST", fmt.Sprintf("/api/listings/%d/render/run?ai_label=0", l1ID), nil, 403)
	env.req(t, "POST", fmt.Sprintf("/api/listings/%d/review", l2ID), Object{"status": "approved"}, 404)
	// Reviewing unrendered listing -> 409
	env.req(t, "POST", fmt.Sprintf("/api/listings/%d/review", l1ID), Object{"status": "approved"}, 409)
	// Non-admin cannot patch studio settings -> 403
	env.req(t, "PATCH", "/api/studio/settings", Object{"require_review": false}, 403)

	// 2. Designer can manage shop 1 template prefs, cannot manage shop 2 (404), cannot review (403)
	env.login(t, "des1", "Password123!")
	// Cannot disable default template -> 400
	env.req(t, "PUT", "/api/shops/1/template-prefs", Object{"default_detail": "cream", "disabled": []string{"cream"}}, 400)
	// Valid prefs for shop 1 -> 200
	env.req(t, "PUT", "/api/shops/1/template-prefs", Object{"default_detail": "cream", "disabled": []string{"oriental"}}, 200)
	// Cannot set disabled template on listing -> 400
	env.req(t, "PUT", fmt.Sprintf("/api/listings/%d/render", l1ID), Object{"detail_template": "oriental"}, 400)
	// Shop 2 template prefs -> 404
	env.req(t, "PUT", fmt.Sprintf("/api/shops/%d/template-prefs", shop2ID), Object{"default_detail": "cream"}, 404)
	// Designer lacks render.review -> 403
	env.req(t, "POST", fmt.Sprintf("/api/listings/%d/review", l1ID), Object{"status": "approved"}, 403)
	env.req(t, "GET", "/api/review/queue", nil, 403)
}

func TestRenderCreamFallbackBatchAndReviewWorkflow(t *testing.T) {
	chrome := pipeline.ChromePath(os.Getenv("CHROME_PATH"))
	if chrome == "" {
		t.Skip("未安装 Chrome，跳过真实渲染测试")
	}
	t.Setenv("CHROME_NO_SANDBOX", "1")

	env := newTestEnv(t, func(c *core.Config) {
		c.ChromePath = chrome
	})
	env.login(t, "admin", "AdminPassword123!")

	// Create shop 2 for batch scope test
	shop2Res := env.req(t, "POST", "/api/shops", Object{"name": "二号店", "platform": "taobao", "language": "zh-CN"}, 201)
	shop2ID := int64(shop2Res["shop"].(Object)["id"].(float64))

	// Create product + listing in shop 1 and shop 2
	p1 := env.req(t, "POST", "/api/products", Object{"name": "奶油留白测试耳环", "shop_ids": []int64{1, shop2ID}}, 201)["product"].(Object)
	pid := int64(p1["id"].(float64))
	listings := p1["listings"].([]any)
	l1ID := int64(listings[0].(Object)["id"].(float64))
	l2ID := int64(listings[1].(Object)["id"].(float64))

	// Upload photo, set code=B1, run retouch so 照片/B1.jpg exists
	photoID := env.uploadPhoto(t, pid, "b1.png")
	env.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", photoID), Object{"code": "B1"}, 200)
	retJob := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/run?step=retouch", l1ID), nil, 202)["job"].(Object)
	if j := env.waitJob(t, int64(retJob["id"].(float64))); j["status"] != "done" {
		t.Fatalf("retouch failed: %v", j)
	}

	// Save & apply copy on l1ID with optional story & storySign for cream template
	copyRes := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies", l1ID), Object{
		"content": Object{
			"name":      "奶油留白珍珠耳钉",
			"oneLiner":  "温润日常通勤之选",
			"story":     "在工作室窗边挑出的第一批暖白珠",
			"storySign": "主理人手记",
			"photos":    Object{"hero": Object{"f": "B1.jpg"}},
			"details":   []Object{{"photo": Object{"f": "B1.jpg"}, "title": "珠光", "body": "细腻柔光"}},
			"care":      []string{"避免接触香水"},
			"service":   []string{"48小时发货"},
			"about":     []string{"手作饰品"},
		},
		"main": Object{"imgs": []Object{{"n": 1, "layout": "photo", "f": "B1.jpg"}}},
		"meta": Object{
			"title": "奶油留白淡水珍珠耳钉",
			"sku":   []Object{{"name": "暖白", "price": 79}},
		},
	}, 201)["copy"].(Object)
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/apply", int64(copyRes["id"].(float64))), nil, 200)

	// 1. Select cream template on listing 1
	renSet := env.req(t, "PUT", fmt.Sprintf("/api/listings/%d/render", l1ID), Object{
		"detail_template": "cream",
	}, 200)
	if !strings.Contains(renSet["preview_url"].(string), "奶油留白.html") {
		t.Fatalf("expected preview_url to use 奶油留白.html, got %v", renSet["preview_url"])
	}

	// 2. Run single listing render
	runJob := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/render/run", l1ID), nil, 202)["job"].(Object)
	runJobID := int64(runJob["id"].(float64))
	doneJob := env.waitJob(t, runJobID)
	if doneJob["status"] != "done" {
		t.Fatalf("render job failed: %v", doneJob)
	}

	// Verify rendered outputs and listing_render state
	renAfter := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/render", l1ID), nil, 200)
	if renAfter["rendered_template"] != "cream" || int64(renAfter["rendered_job_id"].(float64)) != runJobID || renAfter["review_status"] != "pending" {
		t.Fatalf("unexpected render state after cream render: %v", renAfter)
	}
	outputs := renAfter["outputs"].(Object)
	detailOutputs := outputs["detail"].([]any)
	if len(detailOutputs) == 0 || outputs["zip"] == "" {
		t.Fatalf("expected detail slices and zip output, got %v", outputs)
	}

	// 3. Review & Download gating (require_review=true by default)
	// Pending -> download 409 even for admin
	dlPending, err := env.client.Get(fmt.Sprintf("%s/api/listings/%d/download", env.s.URL, l1ID))
	if err != nil {
		t.Fatal(err)
	}
	_ = dlPending.Body.Close()
	if dlPending.StatusCode != 409 {
		t.Fatalf("expected 409 when pending review, got %d", dlPending.StatusCode)
	}

	// Review queue contains l1ID
	qItems := env.req(t, "GET", "/api/review/queue?shop_id=1", nil, 200)["items"].([]any)
	if len(qItems) != 1 || int64(qItems[0].(Object)["listing_id"].(float64)) != l1ID {
		t.Fatalf("expected l1ID in review queue, got %v", qItems)
	}

	// Reject with note -> download still 409, note visible in render state
	rejRes := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/review", l1ID), Object{
		"status": "rejected",
		"note":   "首屏色调需复核",
	}, 200)
	if rejRes["review_status"] != "rejected" || rejRes["review_note"] != "首屏色调需复核" {
		t.Fatalf("unexpected reject state: %v", rejRes)
	}
	dlRejected, err := env.client.Get(fmt.Sprintf("%s/api/listings/%d/download", env.s.URL, l1ID))
	if err != nil {
		t.Fatal(err)
	}
	_ = dlRejected.Body.Close()
	if dlRejected.StatusCode != 409 {
		t.Fatalf("expected 409 when rejected, got %d", dlRejected.StatusCode)
	}

	// Approve -> download 200 with valid ZIP
	appRes := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/review", l1ID), Object{
		"status": "approved",
		"note":   "审核通过可导出",
	}, 200)
	if appRes["review_status"] != "approved" {
		t.Fatalf("unexpected approve state: %v", appRes)
	}
	dlApproved, err := env.client.Get(fmt.Sprintf("%s/api/listings/%d/download", env.s.URL, l1ID))
	if err != nil {
		t.Fatal(err)
	}
	zipBytes, _ := io.ReadAll(dlApproved.Body)
	_ = dlApproved.Body.Close()
	if dlApproved.StatusCode != 200 || !bytes.HasPrefix(zipBytes, []byte("PK")) {
		t.Fatalf("expected 200 ZIP after approval, got status=%d", dlApproved.StatusCode)
	}

	// 4. Disable cream in shop 1 prefs and re-render -> falls back to 模板.html (navy), job message has warning, review_status resets to pending!
	env.req(t, "PUT", "/api/shops/1/template-prefs", Object{
		"default_detail": "navy",
		"disabled":       []string{"cream"},
	}, 200)
	rerunJob := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/render/run", l1ID), nil, 202)["job"].(Object)
	doneRerun := env.waitJob(t, int64(rerunJob["id"].(float64)))
	if doneRerun["status"] != "done" {
		t.Fatalf("rerun failed: %v", doneRerun)
	}
	if !strings.Contains(doneRerun["message"].(string), "回退到 模板.html") {
		t.Fatalf("expected fallback warning in job message, got %q", doneRerun["message"])
	}
	renFallback := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/render", l1ID), nil, 200)
	if renFallback["rendered_template"] != "navy" || renFallback["review_status"] != "pending" {
		t.Fatalf("expected fallback rendered_template=navy and review_status=pending, got %v", renFallback)
	}
	dlAfterRerun, err := env.client.Get(fmt.Sprintf("%s/api/listings/%d/download", env.s.URL, l1ID))
	if err != nil {
		t.Fatal(err)
	}
	_ = dlAfterRerun.Body.Close()
	if dlAfterRerun.StatusCode != 409 {
		t.Fatalf("expected 409 after re-render reset to pending, got %d", dlAfterRerun.StatusCode)
	}

	// 5. require_review=false -> render sets review_status=approved directly and download succeeds
	env.req(t, "PATCH", "/api/studio/settings", Object{"require_review": false}, 200)
	env.req(t, "PUT", fmt.Sprintf("/api/listings/%d/render", l1ID), Object{"detail_template": "navy"}, 200)
	noRevJob := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/render/run", l1ID), nil, 202)["job"].(Object)
	if j := env.waitJob(t, int64(noRevJob["id"].(float64))); j["status"] != "done" {
		t.Fatalf("no-review render failed: %v", j)
	}
	renNoRev := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/render", l1ID), nil, 200)
	if renNoRev["review_status"] != "approved" {
		t.Fatalf("expected review_status=approved when require_review=false, got %v", renNoRev)
	}
	dlNoRev, err := env.client.Get(fmt.Sprintf("%s/api/listings/%d/download", env.s.URL, l1ID))
	if err != nil {
		t.Fatal(err)
	}
	_ = dlNoRev.Body.Close()
	if dlNoRev.StatusCode != 200 {
		t.Fatalf("expected 200 download when require_review=false, got %d", dlNoRev.StatusCode)
	}

	// 6. Batch render skips invisible and active listings, enqueues idle visible listing
	pBatch := env.req(t, "POST", "/api/products", Object{"name": "批量测试第二款", "shop_ids": []int64{1}}, 201)["product"].(Object)
	lBatchBusy := int64(pBatch["listings"].([]any)[0].(Object)["id"].(float64))
	// Insert a queued job on lBatchBusy to simulate busy listing
	_, err = env.c.DB.Exec(
		"INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,created_at,steps) VALUES('pipeline','listing',?,1,'{}','running',?,'render')",
		lBatchBusy, core.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}

	env.req(t, "POST", "/api/users", Object{
		"username": "batch_op",
		"password": "Password123!",
		"role_id":  env.roleIDByKey(t, "operator"),
		"shop_ids": []int64{1},
	}, 201)
	env.login(t, "batch_op", "Password123!")

	batchRes := env.req(t, "POST", "/api/render/batch", Object{
		"listing_ids": []int64{l2ID, lBatchBusy, l1ID},
	}, 200)["items"].([]any)
	if len(batchRes) != 3 {
		t.Fatalf("expected 3 batch results, got %v", batchRes)
	}
	if batchRes[0].(Object)["skipped_reason"] == nil || batchRes[1].(Object)["skipped_reason"] == nil {
		t.Fatalf("expected invisible and busy listings to be skipped, got %v", batchRes)
	}
	batchJobID, ok := batchRes[2].(Object)["job_id"].(float64)
	if !ok || batchJobID < 1 {
		t.Fatalf("expected l1ID to be enqueued with job_id, got %v", batchRes[2])
	}
	// Clean up simulated running job and wait for queued batch job
	_, _ = env.c.DB.Exec("UPDATE jobs SET status='done',finished_at=? WHERE target_id=? AND status='running'", core.Now(), lBatchBusy)
	env.waitJob(t, int64(batchJobID))
}

func TestMigrationUpgradeExistingOutputsApproved(t *testing.T) {
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
		Workers:       1,
	}
	copyDir(t, filepath.Join("..", "..", "..", "templates"), cfg.TemplateDir)
	_ = os.MkdirAll(cfg.PromptDir, 0700)
	web := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	slots := []byte(`{"A1":{"cat":"white","desc":"白底"}}`)

	// Phase 1: Start base database WITHOUT studio module
	c1, err := core.New(cfg, web, slots)
	if err != nil {
		t.Fatal(err)
	}
	if err := c1.Start(accounts.New(c1), products.New(c1), assets.New(c1), pipeline.New(c1)); err != nil {
		_ = c1.Close()
		t.Fatal(err)
	}
	ts := core.Now()
	_, _ = c1.DB.Exec("INSERT INTO products(id,name,created_by,created_at,updated_at) VALUES(1,'老商品',1,?,?)", ts, ts)
	_, _ = c1.DB.Exec("INSERT INTO listings(id,product_id,shop_id,status,created_by,created_at,updated_at) VALUES(1,1,1,'ready',1,?,?)", ts, ts)
	// Create 导出.zip on disk for listing 1
	if err := os.MkdirAll(c1.ListingDir(1), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c1.ListingDir(1), "导出.zip"), []byte("PK\x03\x04dummy"), 0600); err != nil {
		t.Fatal(err)
	}
	_ = c1.Close()

	// Phase 2: Upgrade database WITH studio module registered
	c2, err := core.New(cfg, web, slots)
	if err != nil {
		t.Fatal(err)
	}
	if err := c2.Start(accounts.New(c2), products.New(c2), assets.New(c2), pipeline.New(c2), copywriting.New(c2), studio.New(c2)); err != nil {
		_ = c2.Close()
		t.Fatal(err)
	}
	s := httptest.NewServer(c2.Handler())
	c2.SetBaseURL(s.URL)
	defer func() {
		s.Close()
		_ = c2.Close()
	}()
	jar, _ := cookiejar.New(nil)
	env := &testEnv{
		c:      c2,
		s:      s,
		client: &http.Client{Jar: jar, Timeout: 15 * time.Second},
	}
	env.login(t, "admin", "AdminPassword123!")

	ren := env.req(t, "GET", "/api/listings/1/render", nil, 200)
	if ren["review_status"] != "approved" {
		t.Fatalf("expected upgraded ready listing to have review_status=approved, got %v", ren)
	}
	dlRes, err := env.client.Get(s.URL + "/api/listings/1/download")
	if err != nil {
		t.Fatal(err)
	}
	_ = dlRes.Body.Close()
	if dlRes.StatusCode != 200 {
		t.Fatalf("expected upgraded ready listing download to return 200, got %d", dlRes.StatusCode)
	}
}
