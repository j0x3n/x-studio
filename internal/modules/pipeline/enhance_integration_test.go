package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"xstudio"
)

func TestConfigImageDefaultsAndReasoning(t *testing.T) {
	for _, key := range []string{"AI_MODEL_VISION", "AI_MODEL_TEXT", "AI_REASONING_EFFORT", "IMAGE_MODEL", "IMAGE_TIMEOUT", "AUTO_UPSCALE", "AUTO_FILL", "UPSCALE_MIN_SIDE"} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
	c, err := EnvConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.VisionModel != "gpt-6-sol" || c.TextModel != "gpt-6-sol" || c.ReasoningEffort != "medium" || c.ImageModel != "gpt-image-2.5-sunburst" || c.ImageTimeout != 240*time.Second || !c.AutoFill || !c.AutoUpscale || c.UpscaleMinSide != 900 {
		t.Fatal("wrong defaults")
	}
	t.Setenv("AI_REASONING_EFFORT", "")
	t.Setenv("AUTO_UPSCALE", "0")
	t.Setenv("AUTO_FILL", "0")
	t.Setenv("IMAGE_TIMEOUT", "1")
	t.Setenv("UPSCALE_MIN_SIDE", "600")
	c, err = EnvConfig()
	if err != nil || c.ReasoningEffort != "" || c.AutoUpscale || c.AutoFill || c.ImageTimeout != time.Second || c.UpscaleMinSide != 600 {
		t.Fatal(err, "overrides not applied")
	}
	for key, value := range map[string]string{"AUTO_UPSCALE": "yes", "AUTO_FILL": "bad", "IMAGE_TIMEOUT": "0", "UPSCALE_MIN_SIDE": "-1"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if _, err := EnvConfig(); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	for _, effort := range []string{"medium", ""} {
		t.Run("reasoning="+effort, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body Object
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				v, exists := body["reasoning_effort"]
				if effort == "" && exists || effort != "" && v != effort {
					t.Error("wrong reasoning_effort", body)
				}
				aiReply(w, `{"ok":true}`)
			}))
			defer s.Close()
			f := newFixture(t, func(c *Config) { c.AIKey = "fake"; c.AIBaseURL = s.URL + "/v1"; c.ReasoningEffort = effort })
			var result Object
			for _, model := range []string{"vision", "text"} {
				if err := f.a.aiJSON(testActor(context.Background()), model, "prompt", "input", &result); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestLegacyPhotoMigrationAndIdempotence(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO users(id,username,password_hash,role,created_at) VALUES(1,'old',X'00','admin',?); INSERT INTO products(id,name,owner_id,created_at,updated_at) VALUES(1,'旧款',1,?,?); INSERT INTO photos(product_id,file,orig_name,width,height,code,manual) VALUES(1,'原图/old.jpg','old.jpg',80,60,'A1',1)", now(), now(), now()); err != nil {
		t.Fatal(err)
	}
	// Simulate a partial migration whose physical column order differs.
	if _, err = db.Exec("ALTER TABLE photos ADD COLUMN source TEXT NOT NULL DEFAULT 'original'"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	web, _ := fs.Sub(xstudio.Assets, "web/dist")
	slots, _ := xstudio.Assets.ReadFile("槽位.json")
	c := Config{DataDir: dir, SessionSecret: strings.Repeat("s", 32), Workers: 1}
	for i := 0; i < 2; i++ {
		a, err := newTestApp(c, web, slots)
		if err != nil {
			t.Fatal(err)
		}
		photos, err := a.photos(1)
		if err != nil {
			a.Close()
			t.Fatal(err)
		}
		if len(photos) != 1 || photos[0].Code != "A1" || !photos[0].Manual || photos[0].Source != "original" || photos[0].EnhancedFile != "" || photos[0].AIGenerated || photos[0].AIEnhanced {
			a.Close()
			t.Fatal(photos)
		}
		if err = a.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRealPhotosDisplaceAIAndClassifyIgnoresGenerated(t *testing.T) {
	var realID, generatedID int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request Object
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		parts := request["messages"].([]any)[1].(Object)["content"].([]any)
		imageCount := 0
		for _, v := range parts {
			part := v.(Object)
			if part["type"] == "image_url" {
				imageCount++
			}
			if text, _ := part["text"].(string); strings.HasPrefix(text, fmt.Sprintf("photo_id=%d，", generatedID)) {
				t.Error("AI photo sent to classify")
			}
		}
		if imageCount != 1 {
			t.Error("wrong classify image count", imageCount)
		}
		aiReply(w, js([]Object{{"photo_id": realID, "code": "E1"}, {"photo_id": generatedID, "code": "B1"}}))
	}))
	defer s.Close()
	f := newFixture(t, func(c *Config) { c.AIKey = "fake"; c.AIBaseURL = s.URL + "/v1"; c.VisionModel = "vision" })
	writeEnhancePrompt(t, f.a)
	f.login(t, "admin")
	id := f.product(t)
	generated := addGeneratedPhoto(t, f, id, "E1")
	photoDir := filepath.Join(f.a.Dir(id), "照片")
	if err := os.MkdirAll(photoDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(photoDir, "E1.jpg"), photoBytes(t, 8, 8), 0600); err != nil {
		t.Fatal(err)
	}
	real := addRealPhoto(t, f, id, "", 80, 120)
	realID, generatedID = real.ID, generated.ID
	p, _ := f.a.product(id)
	if err := f.a.classify(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(photoDir, "E1.jpg")); !os.IsNotExist(err) {
		t.Fatal("old AI JPEG survived real-photo takeover", err)
	}
	photos, _ := f.a.photos(id)
	for _, ph := range photos {
		if ph.ID == generatedID && ph.Code != "" || ph.ID == realID && ph.Code != "E1" {
			t.Fatal(photos)
		}
	}
	// Manual code edits follow the same real-photo-first rule.
	generated = addGeneratedPhoto(t, f, id, "D4")
	f.req(t, "PATCH", "/api/assets/"+intString(realID), Object{"code": "D4"}, 200)
	photos, _ = f.a.photos(id)
	for _, ph := range photos {
		if ph.ID == generated.ID && ph.Code != "" {
			t.Fatal("manual real photo did not displace AI", ph)
		}
	}
}

func TestCopyAndPatchForceAILabels(t *testing.T) {
	var copies atomic.Int32
	modelCopy := Object{"content": Object{"photos": Object{"hero": Object{"f": "E1.jpg", "ai": false}}, "details": []Object{{"photo": Object{"f": "B2.jpg", "ai": false}}}, "wear": Object{"a": Object{"f": "A2.jpg", "ai": "AI 生成"}}}, "main": Object{"imgs": []Object{{"n": 1, "f": "E1.jpg", "ai": false}, {"n": 2, "layout": "variants", "items": []Object{{"f": "A2.jpg", "ai": true}, {"f": "B2.jpg", "ai": false}}}}}, "listing": Object{}}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request Object
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		var input Object
		_ = json.Unmarshal([]byte(request["messages"].([]any)[1].(Object)["content"].(string)), &input)
		foundGenerated, foundEnhanced := false, false
		for _, v := range input["photos"].([]any) {
			ph := v.(Object)
			if ph["code"] == "E1" {
				foundGenerated = ph["ai_generated"] == true
			}
			if ph["code"] == "B2" {
				foundEnhanced = ph["ai_enhanced"] == true
			}
		}
		if !foundGenerated || !foundEnhanced {
			t.Error("copy input omitted provenance")
		}
		copies.Add(1)
		aiReply(w, js(modelCopy))
	}))
	defer s.Close()
	f := newFixture(t, func(c *Config) { c.AIKey = "fake"; c.AIBaseURL = s.URL + "/v1"; c.TextModel = "text" })
	writeEnhancePrompt(t, f.a)
	f.login(t, "admin")
	id := f.product(t)
	addRealPhoto(t, f, id, "A2", 80, 100)
	enhanced := addRealPhoto(t, f, id, "B2", 80, 100)
	addGeneratedPhoto(t, f, id, "E1")
	if _, err := f.a.DB.Exec("UPDATE assets SET enhanced_file=?,ai_enhanced=1 WHERE id=?", fmt.Sprintf("原图/ai_up_%d.png", enhanced.ID), enhanced.ID); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.a.Dir(id), "照片")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"A2.jpg", "B2.jpg", "E1.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), photoBytes(t, 8, 8), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := f.a.product(id)
	if err := f.a.copyText(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	assertLabels := func() {
		t.Helper()
		p, err := f.a.product(id)
		if err != nil {
			t.Fatal(err)
		}
		hero := p.Content["photos"].(Object)["hero"].(Object)
		detail := p.Content["details"].([]any)[0].(Object)["photo"].(Object)
		real := p.Content["wear"].(Object)["a"].(Object)
		if hero["ai"] != nil || detail["ai"] != nil {
			t.Fatal(p.Content)
		}
		if _, exists := real["ai"]; exists {
			t.Fatal("real photo kept false AI label")
		}
		imgs := p.Main["imgs"].([]any)
		if _, exists := imgs[0].(Object)["ai"]; exists {
			t.Fatal(p.Main)
		}
		items := imgs[1].(Object)["items"].([]any)
		if _, exists := items[0].(Object)["ai"]; exists {
			t.Fatal("real variant mislabeled")
		}
		if items[1].(Object)["ai"] != nil {
			t.Fatal(items)
		}
	}
	assertLabels()
	f.req(t, "PATCH", "/api/listings/"+intString(id), Object{"content": modelCopy["content"], "main": modelCopy["main"]}, 200)
	assertLabels()
	photos := f.req(t, "GET", "/api/products/"+intString(id), nil, 200)["assets"].([]any)
	for _, v := range photos {
		ph := v.(Object)
		for _, key := range []string{"source", "enhanced_file"} {
			if _, exists := ph[key]; !exists {
				t.Fatal("missing API photo field", key)
			}
		}
	}
	if copies.Load() != 1 {
		t.Fatal(copies.Load())
	}
}

func TestPerProductEnhanceSwitches(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		imageReply(w, sizedPhotoBytes(t, readImageRequest(t, r).size))
	}))
	defer s.Close()
	f := newFixture(t, func(c *Config) {
		c.AIKey = "fake"
		c.AIBaseURL = s.URL + "/v1"
		c.AutoFill = true
		c.AutoUpscale = true
	})
	writeEnhancePrompt(t, f.a)
	f.login(t, "admin")
	id := f.product(t)
	addRealPhoto(t, f, id, "A2", 80, 120)
	f.req(t, "PATCH", "/api/products/"+intString(id), Object{"info": Object{"auto_upscale": false, "auto_fill": false}}, 200)
	p, _ := f.a.product(id)
	if _, err := f.a.enhance(testActor(context.Background()), p); err != nil || calls.Load() != 0 {
		t.Fatal(err, calls.Load())
	}
	f.a.Cfg.AutoUpscale = false
	f.req(t, "PATCH", "/api/products/"+intString(id), Object{"info": Object{"auto_upscale": true, "auto_fill": false}}, 200)
	p, _ = f.a.product(id)
	if _, err := f.a.enhance(testActor(context.Background()), p); err != nil || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
	f.req(t, "PATCH", "/api/products/"+intString(id), Object{"info": Object{"auto_fill": "yes"}}, 400)
	f.req(t, "POST", "/api/products", Object{"name": "bad", "info": Object{"auto_upscale": 1}}, 400)
}

func TestFullEnhancePipelineWithFakeAIAndChrome(t *testing.T) {
	chrome := ChromePath(os.Getenv("CHROME_PATH"))
	if chrome == "" {
		t.Skip("未安装 Chrome，跳过真实渲染测试")
	}
	t.Setenv("CHROME_NO_SANDBOX", "1")
	var ids []int64
	var imageCalls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/edits" {
			got := readImageRequest(t, r)
			imageCalls.Add(1)
			imageReply(w, sizedPhotoBytes(t, got.size))
			return
		}
		var request Object
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request["reasoning_effort"] != "medium" {
			t.Error("reasoning effort missing")
		}
		if request["model"] == "vision" {
			aiReply(w, js([]Object{{"photo_id": ids[0], "code": "B2"}, {"photo_id": ids[1], "code": "F2"}, {"photo_id": ids[2], "code": "C1"}}))
			return
		}
		aiReply(w, js(Object{"content": Object{"name": "测试增强流程", "photos": Object{"hero": Object{"f": "E1_AI.jpg"}, "pack": Object{"f": "F2.jpg"}}, "details": []Object{{"photo": Object{"f": "B2.jpg"}, "title": "细节"}}}, "main": Object{"imgs": []Object{{"n": 1, "f": "E1_AI.jpg"}}}, "listing": Object{"title": "测试耳饰"}}))
	}))
	defer s.Close()
	f := newFixture(t, func(c *Config) {
		c.AIKey = "fake"
		c.AIBaseURL = s.URL + "/v1"
		c.VisionModel = "vision"
		c.TextModel = "text"
		c.ReasoningEffort = "medium"
		c.AutoFill = true
		c.AutoUpscale = true
		c.ChromePath = chrome
	})
	writeEnhancePrompt(t, f.a)
	f.login(t, "admin")
	id := f.product(t)
	f.setRenderMode(t, id, "html")
	for i := 0; i < 3; i++ {
		ph := addRealPhoto(t, f, id, "", 120, 160)
		ids = append(ids, ph.ID)
	}
	jobResponse := f.req(t, "POST", "/api/listings/"+intString(id)+"/run", nil, 202)["job"].(Object)
	job := waitJob(t, f.a, int64(jobResponse["id"].(float64)))
	if job.Status != "done" || job.Progress != 100 || job.Steps != "check,classify,enhance,retouch,copy,render" {
		t.Fatal(job)
	}
	if imageCalls.Load() != 5 {
		t.Fatal("expected 3 upscales and 2 fills; A1_AI white background reuses the fill without another call", imageCalls.Load())
	}
	p, _ := f.a.product(id)
	if p.Status != "ready" || p.Step != "render" {
		t.Fatal(p)
	}
	if _, exists := p.Main["imgs"].([]any)[0].(Object)["ai"]; exists {
		t.Fatal(p.Main)
	}
	if p.Content["photos"].(Object)["pack"].(Object)["ai"] != nil {
		t.Fatal(p.Content)
	}
	photos, _ := f.a.photos(id)
	if len(photos) != 5 {
		t.Fatal(len(photos))
	}
	for _, ph := range photos {
		if !ph.AIGenerated && (!ph.AIEnhanced || ph.RetouchLog["geometry_already_applied"] != true) {
			t.Fatal(ph)
		}
	}
	if _, err := os.Stat(filepath.Join(f.a.ListingDir(id), "导出.zip")); err != nil {
		t.Fatal("enhance pipeline did not produce ZIP", err)
	}
}
