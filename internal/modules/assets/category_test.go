package assets_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"xstudio/internal/core"
)

func requestedImageBytes(t *testing.T, r *http.Request) []byte {
	t.Helper()
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		t.Error(err)
	}
	defer r.MultipartForm.RemoveAll()
	return sizedImageBytes(t, r.FormValue("size"))
}

func sizedImageBytes(t *testing.T, size string) []byte {
	t.Helper()
	var width, height int
	if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil || width < 1 || height < 1 {
		t.Errorf("invalid image size %q", size)
		return nil
	}
	return imageBytes(t, width, height)
}

func TestCategoryTagProcessAndWorkbenchPrompts(t *testing.T) {
	requests := make(chan string, 8)
	var tagAsset atomic.Int64
	output := imageBytes(t, 32, 40)
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "chat/completions") {
			var body obj
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Error(e)
			}
			requests <- body["messages"].([]any)[0].(obj)["content"].(string)
			json.NewEncoder(w).Encode(obj{"choices": []obj{{"message": obj{"content": fmt.Sprintf(`{"items":[{"asset_id":%d,"tags":["晶体"]}]}`, tagAsset.Load())}}}})
			return
		}
		if e := r.ParseMultipartForm(16 << 20); e != nil {
			t.Error(e)
		}
		requests <- r.FormValue("prompt")
		json.NewEncoder(w).Encode(obj{"data": []obj{{"b64_json": base64.StdEncoding.EncodeToString(requestedImageBytes(t, r))}}})
	}))
	defer ai.Close()
	f := newFixture(t, func(c *core.Config) { c.AIKey = "fake"; c.AIBaseURL = ai.URL; c.VisionModel = "vision" })
	prompts(t, f)
	for _, name := range []string{"打标签.md", "处理-高清化.md", "处理-额外需求.md"} {
		body := name + " {{category_name}} {{category_rules}} {{codes}} {{banned_words}}"
		if name == "处理-额外需求.md" {
			body += " {{quick}} {{extra}}"
		}
		if e := os.WriteFile(filepath.Join(f.c.Cfg.PromptDir, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	f.req(t, "POST", "/api/categories", obj{"key": "crystal", "name": "水晶", "rules": "ASSET_CRYSTAL", "codes": []obj{{"code": "X1", "name": "晶体纹路"}}, "banned": []string{"水晶禁词"}}, 201)
	p := f.product(t, "水晶商品", 1)
	f.req(t, "PATCH", fmt.Sprintf("/api/products/%d", p), obj{"category_id": 2}, 200)
	aid := f.upload(t, p, output)
	tagAsset.Store(aid)
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), obj{"code": "X0"}, 200)
	tag := f.req(t, "POST", "/api/assets/tag", obj{"asset_ids": []int64{aid}}, 202)["job"]
	done := f.wait(t, id(tag))
	if done.Status != "done" || done.Result["items"].([]any)[0].(obj)["error"] != nil {
		t.Fatal(done)
	}
	newID(t, processJob(t, f, []int64{aid}, obj{"upscale": obj{"target_long_side": 2048}}, false))
	newID(t, processJob(t, f, []int64{aid}, obj{"ai": obj{"extra": "保留商品"}}, false))
	wbResult(t, wbJob(t, f, []obj{{"asset_id": aid}}, obj{"ai": obj{"extra": "保留商品"}}))
	for i := 0; i < 4; i++ {
		prompt := <-requests
		for _, want := range []string{"水晶", "ASSET_CRYSTAL", "晶体纹路", "水晶禁词"} {
			if !strings.Contains(prompt, want) {
				t.Fatal(prompt)
			}
		}
		if strings.Contains(prompt, "{{category_") {
			t.Fatal(prompt)
		}
	}
	input := wbInput(t, f)
	wbResult(t, wbJob(t, f, []obj{{"input_id": input}}, obj{"ai": obj{"extra": "保留商品"}}))
	anonymous := <-requests
	if strings.Contains(anonymous, "ASSET_CRYSTAL") || strings.Contains(anonymous, "{{category_") || strings.Contains(anonymous, "{{codes}}") || strings.Contains(anonymous, "{{banned_words}}") {
		t.Fatal(anonymous)
	}
}
