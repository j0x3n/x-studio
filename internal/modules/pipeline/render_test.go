package pipeline

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGreenTrimAndEqualSlices(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 750, 6000))
	for y := 0; y < 6000; y++ {
		for x := 0; x < 750; x++ {
			im.SetNRGBA(x, y, color.NRGBA{0, 255, 0, 255})
		}
	}
	if DetailSlices(im) != nil {
		t.Fatal("all-green screen should be skipped")
	}
	for y := 10; y < 3311; y++ {
		for x := 0; x < 750; x++ {
			im.SetNRGBA(x, y, color.NRGBA{245, 245, 245, 255})
		}
	}
	parts := DetailSlices(im)
	if len(parts) != 3 {
		t.Fatal(len(parts))
	}
	sum := 0
	for _, p := range parts {
		if p.Bounds().Dx() != 750 || p.Bounds().Dy() > 1500 {
			t.Fatal(p.Bounds())
		}
		sum += p.Bounds().Dy()
	}
	if sum != 3301 || parts[0].Bounds().Dy() != 1101 || parts[2].Bounds().Dy() != 1099 {
		t.Fatal("slice boundary differs from export.sh", sum)
	}
}
func TestRenderFullPipelineDimensionsAndZip(t *testing.T) {
	chrome := ChromePath(os.Getenv("CHROME_PATH"))
	if chrome == "" {
		t.Skip("未安装 Chrome，跳过真实渲染测试")
	}
	// Exercise real Chrome in the same mode as the container, including CI and
	// restricted Windows runners where GPU child-process sandboxing is blocked.
	t.Setenv("CHROME_NO_SANDBOX", "1")
	ids := []int64{}
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/edits" {
			got := readImageRequest(t, r)
			imageReply(w, sizedPhotoBytes(t, got.size))
			return
		}
		var req Object
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
		}
		if req["model"] == "vision" {
			aiReply(w, js([]Object{{"photo_id": ids[0], "code": "A1", "white_bg": true}, {"photo_id": ids[1], "code": "B1"}, {"photo_id": ids[2], "code": "C4_8mm"}}))
			return
		}
		params := []any{}
		for i := 0; i < 45; i++ {
			params = append(params, []string{fmt.Sprintf("参数%d", i), "【待定】"})
		}
		aiReply(w, js(Object{"content": Object{"name": "测试珍珠", "photos": Object{"hero": Object{"f": "B1.jpg"}}, "details": []Object{{"photo": Object{"f": "B1.jpg"}, "title": "实拍"}}, "params": params, "related": []Object{{"photo": Object{"f": "not-present.jpg"}, "name": "缺图应跳过"}}, "care": []string{"轻轻擦拭"}, "service": []string{"信息待确认"}, "about": []string{"测试品牌"}}, "main": Object{"name": "测试珍珠", "imgs": []Object{{"n": 1, "layout": "photo", "f": "B1.jpg"}}}, "listing": Object{"title": "真淡水珍珠耳钉", "sku": []Object{{"name": "白色", "price": 69}}}}))
	}))
	defer ai.Close()
	f := newFixture(t, func(c *Config) {
		c.AIKey = "fake"
		c.AIBaseURL = ai.URL + "/v1"
		c.VisionModel = "vision"
		c.TextModel = "text"
		c.ChromePath = chrome
	})
	writeEnhancePrompt(t, f.a)
	f.login(t, "admin")
	id := f.product(t)
	for i := 0; i < 3; i++ {
		up := f.upload(t, id, fmt.Sprintf("test-%d.png", i), photoBytes(t, 120, 160), 201)
		ids = append(ids, int64(up["photos"].([]any)[0].(Object)["id"].(float64)))
	}
	j := f.req(t, "POST", fmt.Sprintf("/api/listings/%d/run", id), nil, 202)["job"].(Object)
	job := waitJob(t, f.a, int64(j["id"].(float64)))
	if job.Status != "done" {
		t.Fatalf("full pipeline: %+v", job)
	}
	p, _ := f.a.product(id)
	if p.Status != "ready" {
		t.Fatal(p.Status)
	}
	for file, want := range map[string]image.Point{"主图/3x4/01.jpg": {1440, 1920}, "主图/1x1/01.jpg": {1200, 1200}, "白底.jpg": {1200, 1200}} {
		b, e := os.ReadFile(filepath.Join(f.a.ListingDir(id), "导出", filepath.FromSlash(file)))
		if e != nil {
			t.Fatal(e)
		}
		cfg, e := jpeg.DecodeConfig(bytes.NewReader(b))
		if e != nil {
			t.Fatal(e)
		}
		if cfg.Width != want.X || cfg.Height != want.Y {
			t.Fatalf("%s = %dx%d", file, cfg.Width, cfg.Height)
		}
		if strings.HasPrefix(file, "主图/") && len(b) > 900000 {
			t.Fatal("main image >900KB")
		}
	}
	details, e := os.ReadDir(filepath.Join(f.a.ListingDir(id), "导出", "详情页"))
	if e != nil {
		t.Fatal(e)
	}
	splitCount := 0
	for _, entry := range details {
		name := entry.Name()
		if hasPrefix(name, "04", "06", "07") {
			t.Fatal("screen with no photos was not skipped", name)
		}
		if strings.HasPrefix(name, "05-") {
			splitCount++
		}
		b, _ := os.ReadFile(filepath.Join(f.a.ListingDir(id), "导出", "详情页", name))
		cfg, e := jpeg.DecodeConfig(bytes.NewReader(b))
		if e != nil {
			t.Fatal(e)
		}
		if cfg.Width != 750 || cfg.Height < 1 || cfg.Height > 1500 {
			t.Fatalf("detail %s: %dx%d", name, cfg.Width, cfg.Height)
		}
	}
	if splitCount < 2 {
		t.Fatal("long screen was not split")
	}
	z, e := zip.OpenReader(filepath.Join(f.a.ListingDir(id), "导出.zip"))
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	names := map[string]bool{}
	for _, f := range z.File {
		names[f.Name] = true
		if strings.Contains(f.Name, "..") || strings.Contains(f.Name, "\\") || strings.HasPrefix(f.Name, "主图/") {
			t.Fatal("wrong ZIP layout", f.Name)
		}
	}
	for _, name := range []string{"主图3x4/01.jpg", "主图1x1/01.jpg", "白底.jpg", "标题和SKU.txt", "详情页/01.jpg"} {
		if !names[name] {
			t.Fatal("ZIP missing", name)
		}
	}
	res, e := f.client.Get(fmt.Sprintf("%s/api/listings/%d/download", f.s.URL, id))
	if e != nil {
		t.Fatal(e)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || !bytes.HasPrefix(body, []byte("PK")) {
		t.Fatal("download failed", res.StatusCode)
	}
	// A second tab reuses the same browser, with render-only authentication.
	if f.a.browser.ctx == nil {
		t.Fatal("shared browser missing")
	}
	browser := f.a.browser.ctx
	token := f.a.renderToken()
	res, e = http.Get(f.s.URL + "/render/详情页模板/商品/" + intString(id) + "/内容.js?_t=" + token)
	if e != nil {
		t.Fatal(e)
	}
	body, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(body, []byte("window.PRODUCT")) {
		t.Fatal("render token failed")
	}
	if f.a.browser.ctx != browser {
		t.Fatal("browser recreated")
	}
	res, e = http.Get(f.s.URL + "/api/me?_t=" + token)
	if e != nil {
		t.Fatal(e)
	}
	_ = res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("render token accepted by API")
	}
	detail := f.req(t, "GET", fmt.Sprintf("/api/listings/%d", id), nil, 200)
	if detail["outputs"].(Object)["zip"] == "" {
		t.Fatal("outputs not exposed")
	}
}
