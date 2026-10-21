package pipeline

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"xstudio/internal/media"
)

const enhanceTestPrompt = `## 通用要求
COMMON keep product identical {info}
## 高清化（upscale）
UPSCALE keep composition
## A1 白底图
FILL_A1 product {info}
## E1 场景
FILL_E1 product {info}
## E2 场景
FILL_E2 product {info}
## E3 场景
FILL_E3 product {info}
## 规则（不发给模型）
NEVER_SEND_RULES
`

func writeEnhancePrompt(t *testing.T, a *Module) {
	t.Helper()
	writePrompts(t, a)
	if err := os.WriteFile(filepath.Join(a.Cfg.PromptDir, "补图.md"), []byte(enhanceTestPrompt), 0600); err != nil {
		t.Fatal(err)
	}
}
func addRealPhoto(t *testing.T, f *fixture, id int64, code string, w, h int) Photo {
	t.Helper()
	up := f.upload(t, id, code+".png", photoBytes(t, w, h), 201)
	pid := int64(up["photos"].([]any)[0].(Object)["id"].(float64))
	if code != "" {
		f.req(t, "PATCH", "/api/assets/"+intString(pid), Object{"code": code}, 200)
	}
	ph, err := scanPhoto(f.a.DB.QueryRow("SELECT "+photoColumns+" FROM assets WHERE id=?", pid))
	if err != nil {
		t.Fatal(err)
	}
	return ph
}
func addGeneratedPhoto(t *testing.T, f *fixture, id int64, code string) Photo {
	t.Helper()
	name := "原图/ai_" + code + ".png"
	if err := writeAIFile(filepath.Join(f.a.Dir(id), filepath.FromSlash(name)), photoBytes(t, 64, 80)); err != nil {
		t.Fatal(err)
	}
	res, err := f.a.DB.Exec("INSERT INTO assets(product_id,file,orig_name,width,height,code,ai_generated,source,created_by,created_at) VALUES(?,?,?,64,80,?,1,'ai_generated',1,'2026-10-18T00:00:00Z')", id, name, code+".png", code)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := res.LastInsertId()
	ph, err := scanPhoto(f.a.DB.QueryRow("SELECT "+photoColumns+" FROM assets WHERE id=?", pid))
	if err != nil {
		t.Fatal(err)
	}
	return ph
}

type imageRequest struct {
	prompt, size string
	dimensions   []image.Point
}

func readImageRequest(t *testing.T, r *http.Request) imageRequest {
	t.Helper()
	if r.Method != "POST" || r.URL.Path != "/v1/images/edits" {
		t.Errorf("wrong image endpoint %s %s", r.Method, r.URL.Path)
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		t.Error(err)
		return imageRequest{}
	}
	defer r.MultipartForm.RemoveAll()
	if r.FormValue("model") != "gpt-image-2.5-sunburst" || r.FormValue("quality") != "high" || r.Header.Get("Authorization") != "Bearer fake" {
		t.Error("missing model, high quality or authorization")
	}
	got := imageRequest{prompt: r.FormValue("prompt"), size: r.FormValue("size")}
	if strings.Contains(got.prompt, "NEVER_SEND_RULES") {
		t.Error("rules leaked into model prompt")
	}
	if len(r.MultipartForm.File) != 1 {
		t.Error("all references must use the same image field")
	}
	for _, header := range r.MultipartForm.File["image"] {
		file, err := header.Open()
		if err != nil {
			t.Error(err)
			continue
		}
		cfg, _, err := image.DecodeConfig(file)
		_ = file.Close()
		if err != nil {
			t.Error(err)
			continue
		}
		got.dimensions = append(got.dimensions, image.Pt(cfg.Width, cfg.Height))
	}
	return got
}
func imageReply(w http.ResponseWriter, b []byte) {
	writeJSON(w, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(b)}}})
}

func sizedPhotoBytes(t *testing.T, size string) []byte {
	t.Helper()
	var width, height int
	if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil || width < 1 || height < 1 {
		t.Fatalf("invalid image size %q", size)
	}
	return photoBytes(t, width, height)
}

func TestImageEditsMultipartRetryAndSize(t *testing.T) {
	png := sizedPhotoBytes(t, "1536x1024")
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := readImageRequest(t, r)
		if len(got.dimensions) != 3 || got.size != "1536x1024" || got.prompt != "test prompt" {
			t.Error(got)
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		imageReply(w, png)
	}))
	defer s.Close()
	f := newFixture(t, func(c *Config) { c.AIKey = "fake"; c.AIBaseURL = s.URL + "/v1" })
	refs := []image.Image{image.NewNRGBA(image.Rect(0, 0, 1440, 1080)), image.NewNRGBA(image.Rect(0, 0, 20, 30)), image.NewNRGBA(image.Rect(0, 0, 30, 30))}
	b, err := f.a.editImage(testActor(context.Background()), "test prompt", refs)
	if err != nil || !bytes.Equal(b, png) || calls.Load() != 2 {
		t.Fatal(err, calls.Load())
	}
	for _, tc := range []struct {
		w, h int
		size string
	}{{900, 900, "1024x1024"}, {600, 900, "1024x1536"}, {900, 600, "1536x1024"}} {
		if got := imageSize(image.NewNRGBA(image.Rect(0, 0, tc.w, tc.h))); got != tc.size {
			t.Fatal(got, tc)
		}
	}
}

func TestUpscaleThresholdGeometryAndInvalidation(t *testing.T) {
	var mu sync.Mutex
	requests := []imageRequest{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := readImageRequest(t, r)
		mu.Lock()
		requests = append(requests, got)
		mu.Unlock()
		imageReply(w, sizedPhotoBytes(t, got.size))
	}))
	defer s.Close()
	f := newFixture(t, func(c *Config) {
		c.AIKey = "fake"
		c.AIBaseURL = s.URL + "/v1"
		c.AutoUpscale = true
		c.UpscaleMinSide = 900
	})
	writeEnhancePrompt(t, f.a)
	f.login(t, "admin")
	id := f.product(t)
	addRealPhoto(t, f, id, "A2", 1200, 900) // Exactly at threshold: no request.
	below := addRealPhoto(t, f, id, "B1", 1200, 899)
	cropped := addRealPhoto(t, f, id, "B2", 1200, 1200)
	f.req(t, "PATCH", "/api/assets/"+intString(cropped.ID), Object{"rotate": 90, "crop": []float64{0, 0, .5, .5}}, 200)
	addRealPhoto(t, f, id, "", 80, 80)
	addRealPhoto(t, f, id, "R1", 80, 80)
	p, _ := f.a.product(id)
	warnings, err := f.a.enhance(testActor(context.Background()), p)
	if err != nil || len(warnings) > 0 {
		t.Fatal(warnings, err)
	}
	mu.Lock()
	got := append([]imageRequest(nil), requests...)
	mu.Unlock()
	if len(got) != 2 || got[0].dimensions[0] != image.Pt(1536, 1024) || got[1].dimensions[0] != image.Pt(1024, 1024) {
		t.Fatal(got)
	}
	if err = f.a.retouch(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	photos, _ := f.a.photos(id)
	for _, ph := range photos {
		if ph.ID != below.ID && ph.ID != cropped.ID {
			continue
		}
		if !ph.AIEnhanced || ph.EnhancedFile != fmt.Sprintf("原图/ai_up_%d.png", ph.ID) {
			t.Fatal(ph)
		}
		data, err := os.ReadFile(filepath.Join(f.a.Dir(id), filepath.FromSlash(ph.EnhancedFile)))
		size := "1536x1024"
		if ph.ID == cropped.ID {
			size = "1024x1024"
		}
		if err != nil || !bytes.Equal(data, sizedPhotoBytes(t, size)) {
			t.Fatal("enhanced PNG not saved", err)
		}
		file, err := os.Open(filepath.Join(f.a.Dir(id), "照片", ph.Code+".jpg"))
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := jpeg.DecodeConfig(file)
		_ = file.Close()
		if err != nil || cfg.Width != map[bool]int{true: 1024, false: 1536}[ph.ID == cropped.ID] || cfg.Height != 1024 {
			t.Fatal("retouch applied crop/rotate twice", cfg, err)
		}
	}
	if warnings, err = f.a.enhance(testActor(context.Background()), p); err != nil || len(warnings) > 0 {
		t.Fatal(warnings, err)
	}
	mu.Lock()
	count := len(requests)
	mu.Unlock()
	if count != 2 {
		t.Fatal("repeated unchanged upscale", count)
	}
	r := f.req(t, "PATCH", "/api/assets/"+intString(cropped.ID), Object{"rotate": 180}, 200)["asset"].(Object)
	if r["enhanced_file"] != "" || r["source"] != "original" {
		t.Fatal("geometry change kept stale enhancement", r)
	}
	if _, err = f.a.enhance(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	count = len(requests)
	mu.Unlock()
	if count != 3 {
		t.Fatal("geometry change did not request new upscale", count)
	}
}

func TestFillRulesReferencesStorageCoverageAndLimit(t *testing.T) {
	var count atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := readImageRequest(t, r)
		if len(got.dimensions) != 2 {
			t.Error("expected two real reference images", got)
		}
		if !(strings.Contains(got.prompt, "FILL_A1") || strings.Contains(got.prompt, "FILL_E1")) {
			t.Error("forbidden generated code", got.prompt)
		}
		if strings.Contains(got.prompt, "{info}") || strings.Contains(got.prompt, "{size}") {
			t.Error("placeholders not replaced")
		}
		count.Add(1)
		imageReply(w, sizedPhotoBytes(t, got.size))
	}))
	defer s.Close()
	f := newFixture(t, func(c *Config) { c.AIKey = "fake"; c.AIBaseURL = s.URL + "/v1"; c.AutoFill = true })
	writeEnhancePrompt(t, f.a)
	f.login(t, "admin")
	id := f.product(t)
	addRealPhoto(t, f, id, "B2", 80, 120)
	addRealPhoto(t, f, id, "F2", 80, 120)
	p, _ := f.a.product(id)
	p.Info = Object{"size": "8 mm"}
	warnings, err := f.a.enhance(testActor(context.Background()), p)
	if err != nil || len(warnings) > 0 {
		t.Fatal(warnings, err)
	}
	if count.Load() != 2 {
		t.Fatal(count.Load())
	}
	photos, _ := f.a.photos(id)
	generated := 0
	for _, ph := range photos {
		if !ph.AIGenerated {
			continue
		}
		generated++
		if !allowedFillCode(ph.Code) || ph.Source != "ai_generated" || ph.WhiteBG != (ph.Code == "A1_AI") || !strings.Contains(ph.AINote, "AI 生成（参考 #") {
			t.Fatal(ph)
		}
		data, err := os.ReadFile(filepath.Join(f.a.Dir(id), filepath.FromSlash(ph.File)))
		if err != nil || !bytes.Equal(data, sizedPhotoBytes(t, "1024x1536")) {
			t.Fatal("generated base64 PNG not saved", err)
		}
		getJPEG(t, f, fmt.Sprintf("/files/products/%d/%s?w=240", id, ph.File), 160, 240)
	}
	if generated != 2 {
		t.Fatal(generated)
	}
	cov := f.a.coverage(photos)
	for _, entry := range cov["main"].([]Object) {
		if entry["position"] == "白底" || entry["position"] == float64(1) {
			if entry["covered"] != true || entry["ai"] != true {
				t.Fatal(entry)
			}
		}
	}
	for _, entry := range cov["need_reshoot"].([]Object) {
		for _, candidate := range entry["candidates"].([]string) {
			if allowedFillCode(candidate) {
				t.Fatal("AI fillable slot in need_reshoot", entry)
			}
		}
	}
	if _, err = f.a.enhance(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 2 {
		t.Fatal("existing generated photos were regenerated")
	}
	// A separate product with four AI records must never generate a fifth.
	second := f.product(t)
	addRealPhoto(t, f, second, "A2", 80, 120)
	// 四张旧 AI 图都不在首图位，首图仍要补，但已到上限
	addGeneratedPhoto(t, f, second, "A1_AI")
	for i := 0; i < 3; i++ {
		if _, err := f.a.DB.Exec("INSERT INTO assets(product_id,file,orig_name,width,height,code,ai_generated,source,created_by,created_at) VALUES(?,?,'old.png',64,80,'',1,'ai_generated',1,?)", second, fmt.Sprintf("原图/ai_old%d.png", i), now()); err != nil {
			t.Fatal(err)
		}
	}
	p, _ = f.a.product(second)
	warnings, err = f.a.enhance(testActor(context.Background()), p)
	if err != nil || len(warnings) == 0 || !strings.Contains(strings.Join(warnings, ""), "4 张上限") || count.Load() != 2 {
		t.Fatal(warnings, err, count.Load())
	}
	refs := selectReferences([]Photo{{ID: 1, Code: "D2"}, {ID: 2, Code: "A2"}, {ID: 3, Code: "B2"}, {ID: 4, Code: "A1", Source: "ai_edited"}, {ID: 5, Code: "F2", AIGenerated: true}})
	for _, ref := range refs {
		if media.AIMade(ref) {
			t.Fatal("AI reference selected")
		}
	}
	if len(refs) != 3 || refs[0].Code != "A2" || refs[1].Code != "B2" || refs[2].Code != "D2" {
		t.Fatal(refs)
	}
}

func TestEnhanceFailuresContinueQueueAndPreserveWarnings(t *testing.T) {
	for _, up := range []bool{true, false} {
		t.Run(fmt.Sprint("upscale=", up), func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
			defer s.Close()
			f := newFixture(t, func(c *Config) { c.AIKey = "fake"; c.AIBaseURL = s.URL + "/v1"; c.AutoUpscale = up; c.AutoFill = !up })
			writeEnhancePrompt(t, f.a)
			f.login(t, "admin")
			id := f.product(t)
			ph := addRealPhoto(t, f, id, "B1", 100, 80)
			res, err := f.a.DB.Exec("INSERT INTO jobs(target_id,target_type,user_id,kind,status,created_at,steps) VALUES(?,'listing',1,'pipeline','queued',?,'enhance,retouch')", id, now())
			if err != nil {
				t.Fatal(err)
			}
			jid, _ := res.LastInsertId()
			job := waitJob(t, f.a, jid)
			if job.Status != "done" || !strings.Contains(job.Message, "失败") || !strings.Contains(job.Message, "503") {
				t.Fatal(job)
			}
			p, _ := f.a.product(id)
			if p.Step != "retouch" {
				t.Fatal("pipeline stopped at enhance", p.Step)
			}
			photos, _ := f.a.photos(id)
			if up {
				if calls.Load() != 2 || photos[0].RetouchLog["upscale_error"] == nil || photos[0].EnhancedFile != "" {
					t.Fatal(calls.Load(), photos)
				}
			} else {
				if calls.Load() != 4 || len(photos) != 1 {
					t.Fatal(calls.Load(), photos)
				}
			}
			if _, err = os.Stat(filepath.Join(f.a.Dir(id), "照片", ph.Code+".jpg")); err != nil {
				t.Fatal("original was not retouched after AI failure", err)
			}
		})
	}
}

func TestImageTimeoutAndInvalidBase64(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint(timeout), func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if timeout {
					_, _ = io.Copy(io.Discard, r.Body)
					select {
					case <-r.Context().Done():
					case <-time.After(time.Second):
					}
					return
				}
				writeJSON(w, 200, Object{"data": []Object{{"b64_json": "not base64!"}}})
			}))
			defer s.Close()
			f := newFixture(t, func(c *Config) { c.AIKey = "fake"; c.AIBaseURL = s.URL + "/v1"; c.ImageTimeout = 40 * time.Millisecond })
			_, err := f.a.editImage(testActor(context.Background()), "test", []image.Image{image.NewNRGBA(image.Rect(0, 0, 8, 8))})
			if err == nil || calls.Load() != 2 {
				t.Fatal(err, calls.Load())
			}
		})
	}
}

func TestEvidenceSlotsOnlyAcceptRealPhotos(t *testing.T) {
	photos := []Photo{
		{ID: 1, Code: "B2", Source: "ai_edited"},
		{ID: 2, Code: "E1_AI", AIGenerated: true, Source: "ai_generated"},
		{ID: 3, Code: "E1"},
		{ID: 4, Code: "B1", Source: "ai_enhanced", AIEnhanced: true},
		{ID: 5, Code: "C1", Source: "original", SourceOverride: "ai_edited"},
	}
	ids := func(list []Photo) string {
		out := []string{}
		for _, ph := range list {
			out = append(out, fmt.Sprint(ph.ID))
		}
		return strings.Join(out, ",")
	}
	if got := ids(slotHits(slot{Candidates: []string{"B1", "B2"}}, photos)); got != "4" {
		t.Fatal("evidence slot accepted AI-made photo", got)
	}
	if got := ids(slotHits(slot{Candidates: []string{"C1"}}, photos)); got != "" {
		t.Fatal("source override to AI must leave evidence slot", got)
	}
	if got := ids(slotHits(slot{Candidates: []string{"D4", "E1"}}, photos)); got != "3,2" {
		t.Fatal("real photo must come before AI fill of the same code", got)
	}
	for _, c := range []struct {
		ph Photo
		ok bool
	}{
		{Photo{Code: "E1_AI", AIGenerated: true}, true},
		{Photo{Code: "D4", AIGenerated: true}, false},
		{Photo{Code: "D4_AI", AIGenerated: true}, false},
		{Photo{Code: "A1", AIGenerated: true}, false},
		{Photo{Code: "B2_AI"}, false},
		{Photo{Code: "C4_8mm"}, true},
	} {
		if err := validatePhoto(c.ph); (err == nil) != c.ok {
			t.Fatal(c.ph.Code, err)
		}
	}
}
