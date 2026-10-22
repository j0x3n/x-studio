package assets_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"xstudio/internal/core"
	"xstudio/internal/media"
)

type viewsAI struct {
	confidence atomic.Int64
	fidelity   atomic.Int64
	code       atomic.Int32
	models     chan string
}

func viewsAIFixture(t *testing.T) (*fixture, *viewsAI) {
	t.Helper()
	fake := &viewsAI{models: make(chan string, 64)}
	fake.confidence.Store(95)
	fake.fidelity.Store(95)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			var request obj
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			fake.models <- request["model"].(string)
			prompt := request["messages"].([]any)[0].(obj)["content"].(string)
			out := obj{"fidelity": fake.fidelity.Load(), "issues": []string{}}
			if strings.Contains(prompt, "TEST_CLASSIFY") {
				code := map[int32]string{0: "A1", 1: "A2", 2: ""}[fake.code.Load()]
				out = obj{"code": code, "confidence": float64(fake.confidence.Load()) / 100, "reason": "视角明确"}
				if code == "" {
					out["reason"] = "带他人水印"
				}
			}
			core.WriteJSON(w, 200, obj{"choices": []obj{{"message": obj{"content": core.JS(out)}}}})
			return
		}
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		var width, height int
		fmt.Sscanf(r.FormValue("size"), "%dx%d", &width, &height)
		if width < 1 || height < 1 {
			t.Error("missing generation size")
			w.WriteHeader(400)
			return
		}
		im := image.NewNRGBA(image.Rect(0, 0, width, height))
		for vertical := 0; vertical < height; vertical++ {
			for horizontal := 0; horizontal < width; horizontal++ {
				value := uint8(20)
				if (horizontal/16+vertical/16)%2 == 0 {
					value = 230
				}
				im.SetNRGBA(horizontal, vertical, color.NRGBA{value, value, value, 255})
			}
		}
		var data bytes.Buffer
		png.Encode(&data, im)
		core.WriteJSON(w, 200, obj{"data": []obj{{"b64_json": base64.StdEncoding.EncodeToString(data.Bytes())}}})
	}))
	t.Cleanup(provider.Close)
	f := newFixture(t, func(config *core.Config) {
		config.AIKey = "fake"
		config.AIBaseURL = provider.URL
		config.VisionModel = "fake-vision"
		config.ImageModel = "fake-image"
	})
	prompts(t, f)
	for name, body := range map[string]string{"分拣-底图.md": "TEST_CLASSIFY", "高清化-核对.md": "TEST_VERIFY"} {
		if err := os.WriteFile(filepath.Join(f.c.Cfg.PromptDir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	configureViews(t, f)
	f.req(t, "PATCH", "/api/settings", obj{"ai_models": obj{"classify": "slot-classifier", "creative_verify": "slot-verifier"}}, 200)
	return f, fake
}

func viewJob(t *testing.T, f *fixture, pid int64, action string, body obj) core.Job {
	t.Helper()
	out := f.req(t, "POST", fmt.Sprintf("/api/products/%d/views/%s", pid, action), body, 202)
	j := f.wait(t, int64(out["job_id"].(float64)))
	if j.Status != "done" {
		t.Fatal(j)
	}
	for _, raw := range j.Result["items"].([]any) {
		if raw.(obj)["error"] != nil {
			t.Fatal(raw)
		}
	}
	return j
}

func TestViewsClassifyConfidenceAndOccupiedSlot(t *testing.T) {
	f, fake := viewsAIFixture(t)
	pid := f.product(t, "分拣")
	aid := f.upload(t, pid, imageBytes(t, 48, 32))
	fake.confidence.Store(40)
	viewJob(t, f, pid, "classify", obj{})
	p, _ := core.ScanPhoto(f.c.DB.QueryRow("SELECT "+core.PhotoColumns+" FROM assets WHERE id=?", aid))
	if p.Code != "" || p.Suggest["code"] != "A1" {
		t.Fatal(p)
	}
	if model := <-fake.models; model != "slot-classifier" {
		t.Fatal(model)
	}
	current := uploadView(t, f, pid, "A1", 201)
	fake.confidence.Store(99)
	viewJob(t, f, pid, "classify", obj{"asset_ids": []int64{aid}})
	out := f.req(t, "GET", fmt.Sprintf("/api/products/%d/views", pid), nil, 200)
	if id(viewEntry(out, "front")["current"]) != current {
		t.Fatal(out)
	}
	fake.code.Store(1)
	viewJob(t, f, pid, "classify", obj{"asset_ids": []int64{aid}})
	p, _ = core.ScanPhoto(f.c.DB.QueryRow("SELECT "+core.PhotoColumns+" FROM assets WHERE id=?", aid))
	if p.Code != "A2" || p.ViewCode != "A2" || len(p.Suggest) != 0 {
		t.Fatal(p)
	}
	watermark := f.upload(t, pid, imageBytes(t, 20, 20))
	fake.code.Store(2)
	viewJob(t, f, pid, "classify", obj{"asset_ids": []int64{watermark}})
	p, _ = core.ScanPhoto(f.c.DB.QueryRow("SELECT "+core.PhotoColumns+" FROM assets WHERE id=?", watermark))
	if p.Code != "" || p.Suggest["reason"] != "带他人水印" {
		t.Fatal(p)
	}
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", watermark), obj{"code": "A1"}, 200)
	p, _ = core.ScanPhoto(f.c.DB.QueryRow("SELECT "+core.PhotoColumns+" FROM assets WHERE id=?", watermark))
	if len(p.Suggest) != 0 {
		t.Fatal(p.Suggest)
	}
}

func TestViewsEnhanceAutoConfirmAndFidelity(t *testing.T) {
	f, fake := viewsAIFixture(t)
	pid := f.product(t, "高清化")
	path := fmt.Sprintf("/api/products/%d/views", pid)
	original := uploadView(t, f, pid, "A1", 201)
	j := viewJob(t, f, pid, "enhance", obj{"asset_ids": []int64{original}, "replace": "auto"})
	passed := newID(t, j)
	photo, err := core.ScanPhoto(f.c.DB.QueryRow("SELECT "+core.PhotoColumns+" FROM assets WHERE id=?", passed))
	if err != nil || photo.Code != "A1" || photo.ViewCode != "A1" || photo.ParentID == nil || *photo.ParentID != original || media.AIMade(photo) || photo.EnhanceCheck["verdict"] != "pass" {
		t.Fatal(photo, err)
	}
	if model := <-fake.models; model != "slot-verifier" {
		t.Fatal(model)
	}
	if photo.EnhanceCheck["blur_after"].(float64) <= photo.EnhanceCheck["blur_before"].(float64) {
		t.Fatal(photo.EnhanceCheck)
	}
	fake.fidelity.Store(60)
	j = viewJob(t, f, pid, "enhance", obj{"asset_ids": []int64{original}, "replace": "auto"})
	failed := newID(t, j)
	photo, err = core.ScanPhoto(f.c.DB.QueryRow("SELECT "+core.PhotoColumns+" FROM assets WHERE id=?", failed))
	if err != nil || photo.Code != "" || photo.EnhanceCheck["verdict"] != "fail" || !media.AIMade(photo) {
		t.Fatal(photo, err)
	}
	out := f.req(t, "GET", path, nil, 200)
	if id(viewEntry(out, "front")["current"]) != passed || len(viewEntry(out, "front")["pending"].([]any)) != 1 {
		t.Fatal(out)
	}
	f.req(t, "POST", fmt.Sprintf("/api/assets/%d/discard", failed), obj{}, 200)
	out = f.req(t, "GET", path, nil, 200)
	if len(viewEntry(out, "front")["pending"].([]any)) != 0 || len(viewEntry(out, "front")["versions"].([]any)) != 2 {
		t.Fatal(out)
	}
	fake.fidelity.Store(98)
	j = viewJob(t, f, pid, "enhance", obj{"asset_ids": []int64{original}, "replace": "confirm"})
	confirmed := newID(t, j)
	out = f.req(t, "GET", path, nil, 200)
	if id(viewEntry(out, "front")["current"]) != passed || len(viewEntry(out, "front")["pending"].([]any)) != 1 {
		t.Fatal(out)
	}
	f.req(t, "POST", fmt.Sprintf("/api/assets/%d/make-current", confirmed), obj{}, 200)
	f.req(t, "POST", fmt.Sprintf("/api/assets/%d/make-current", original), obj{}, 200)
	out = f.req(t, "GET", path, nil, 200)
	if id(viewEntry(out, "front")["current"]) != original || len(viewEntry(out, "front")["pending"].([]any)) != 0 {
		t.Fatal(out)
	}
	f.assertAudit(t, "assets.views.enhance", "assets.make_current")
}

func TestViewsEnhanceKeepsCurrentWithoutClarityGain(t *testing.T) {
	f, _ := viewsAIFixture(t)
	pid := f.product(t, "重复高清化")
	original := uploadView(t, f, pid, "A1", 201)
	first := newID(t, viewJob(t, f, pid, "enhance", obj{"asset_ids": []int64{original}, "replace": "auto"}))
	f.req(t, "PATCH", "/api/settings", obj{"ai_models": obj{"classify": "fallback-classify"}}, 200)
	second := newID(t, viewJob(t, f, pid, "enhance", obj{"asset_ids": []int64{first}, "replace": "auto"}))
	photo, err := core.ScanPhoto(f.c.DB.QueryRow("SELECT "+core.PhotoColumns+" FROM assets WHERE id=?", second))
	if err != nil || photo.Code != "" || photo.EnhanceCheck["pending"] != true || photo.EnhanceCheck["verdict"] != "pass" {
		t.Fatal(photo, err)
	}
	out := f.req(t, "GET", fmt.Sprintf("/api/products/%d/views", pid), nil, 200)
	if id(viewEntry(out, "front")["current"]) != first || len(viewEntry(out, "front")["pending"].([]any)) != 1 {
		t.Fatal(out)
	}
	var model string
	if err = f.c.DB.QueryRow("SELECT model FROM ai_usage WHERE feature='classify' ORDER BY id DESC LIMIT 1").Scan(&model); err != nil || model != "fallback-classify" {
		t.Fatal(model, err)
	}
	unassigned := f.upload(t, pid, imageBytes(t, 48, 32))
	candidate := newID(t, viewJob(t, f, pid, "enhance", obj{"asset_ids": []int64{unassigned}, "replace": "auto"}))
	photo, err = core.ScanPhoto(f.c.DB.QueryRow("SELECT "+core.PhotoColumns+" FROM assets WHERE id=?", candidate))
	if err != nil || photo.Code != "" || photo.ViewCode != "" || photo.EnhanceCheck["pending"] != true {
		t.Fatal(photo, err)
	}
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", candidate), obj{"code": "A2"}, 200)
	f.req(t, "POST", fmt.Sprintf("/api/assets/%d/make-current", unassigned), obj{}, 200)
}
