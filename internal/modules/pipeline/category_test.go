package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCategoryClassifyCopyCoverageAndScopedEndpoints(t *testing.T) {
	requests := make(chan Object, 4)
	var assetID atomic.Int64
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body Object
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		requests <- body
		messages := body["messages"].([]any)
		prompt := messages[0].(Object)["content"].(string)
		if strings.Contains(prompt, "分拣") {
			aiReply(w, js(Object{"photos": []Object{{"photo_id": assetID.Load(), "code": "X1"}}}))
		} else {
			aiReply(w, `{"content":{"name":"水晶","oneLiner":"水晶禁词"},"main":{"imgs":[]},"listing":{"title":"水晶"}}`)
		}
	}))
	defer ai.Close()
	f := newFixture(t, func(c *Config) {
		c.VisionModel = "vision"
		c.TextModel = "text"
		c.AIKey = "fake"
		c.AIBaseURL = ai.URL + "/v1"
		c.TemplateDir = t.TempDir()
	})
	writePrompts(t, f.a)
	for _, name := range []string{"分拣.md", "文案.md"} {
		if e := os.WriteFile(filepath.Join(f.a.Cfg.PromptDir, name), []byte(name+" {{category_name}} {{category_rules}} {{codes}} {{banned_words}}"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	sampleDir := filepath.Join(f.a.Cfg.TemplateDir, "详情页模板", "商品", "示例款")
	if e := os.MkdirAll(sampleDir, 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"内容.js", "主图.js"} {
		if e := os.WriteFile(filepath.Join(sampleDir, name), []byte("{}"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	f.login(t, "admin")
	f.req(t, "PATCH", "/api/categories/1", Object{"rules": "PEARL_ONLY", "banned": []string{"珍珠禁词"}}, 200)
	slots := Object{"主图": []Object{{"位": 1, "名": "晶体", "候选": []string{"X1"}, "必需": true}}, "详情页": []Object{{"屏": 1, "名": "晶体细节", "候选": []string{"X2"}, "必需": true}}}
	cat := f.req(t, "POST", "/api/categories", Object{"key": "crystal", "name": "水晶", "rules": "CRYSTAL_ONLY", "banned": []string{"水晶禁词"}, "slots": slots, "codes": []Object{{"code": "X1", "name": "水晶实拍", "desc": "晶体纹路"}, {"code": "X2", "name": "水晶细节", "suffix": "尺寸"}}}, 201)["category"].(Object)
	cid := int64(cat["id"].(float64))
	product := f.req(t, "POST", "/api/products", Object{"name": "水晶", "category_id": cid, "shop_ids": []int64{1}}, 201)["product"].(Object)
	pid := int64(product["id"].(float64))
	path := "/api/products/" + intString(pid)
	before := f.req(t, "GET", path, nil, 200)["coverage"].(Object)
	if len(before["main"].([]any)) != 1 || len(before["need_reshoot"].([]any)) != 2 {
		t.Fatal(before)
	}
	up := f.upload(t, pid, "crystal.png", photoBytes(t, 64, 80), 201)
	assetID.Store(int64(up["photos"].([]any)[0].(Object)["id"].(float64)))
	p, e := f.a.product(pid)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.a.classify(testActor(context.Background()), p); e != nil {
		t.Fatal(e)
	}
	if e = f.a.copyText(testActor(context.Background()), p); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		request := <-requests
		messages := request["messages"].([]any)
		prompt := messages[0].(Object)["content"].(string)
		if !strings.Contains(prompt, "CRYSTAL_ONLY") || !strings.Contains(prompt, "晶体纹路") || !strings.Contains(prompt, "X2_<尺寸>") || strings.Contains(prompt, "PEARL_ONLY") || strings.Contains(prompt, "珍珠表面") {
			t.Fatal(prompt)
		}
		var input Object
		content := messages[1].(Object)["content"]
		if parts, ok := content.([]any); ok {
			content = parts[0].(Object)["text"]
		}
		if e = json.Unmarshal([]byte(content.(string)), &input); e != nil {
			t.Fatal(e)
		}
		if js(input["slots"]) != js(slots) || input["category"].(Object)["key"] != "crystal" {
			t.Fatal(input)
		}
	}
	after := f.req(t, "GET", path, nil, 200)["coverage"].(Object)
	if len(after["need_reshoot"].([]any)) != 1 || after["main"].([]any)[0].(Object)["covered"] != true {
		t.Fatal(after)
	}
	p, e = f.a.product(pid)
	if e != nil {
		t.Fatal(e)
	}
	issues := p.Listing["compliance"].([]any)
	if len(issues) != 1 || issues[0].(Object)["reason"] != "水晶违禁词" {
		t.Fatal(issues)
	}
	if e := f.a.DB.QueryRow("SELECT category_id FROM products WHERE id=?", pid).Scan(&cid); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{"?product_id=" + intString(pid), "?category_id=" + intString(cid)} {
		got := f.req(t, "GET", "/api/slots"+q, nil, 200)
		if js(got) != js(slots) {
			t.Fatal(got)
		}
		codes := f.req(t, "GET", "/api/codes"+q, nil, 200)["codes"].([]any)
		if len(codes) != 2 || codes[0].(Object)["code"] != "X1" {
			t.Fatal(codes)
		}
	}
	if n := len(f.req(t, "GET", "/api/codes", nil, 200)["codes"].([]any)); n != 26 {
		t.Fatal(n)
	}
	for _, endpoint := range []string{"/api/slots", "/api/codes"} {
		f.req(t, "GET", endpoint+"?product_id=99999", nil, 404)
		f.req(t, "GET", endpoint+"?category_id=no", nil, 400)
	}
	f.newUser(t, "outsider", 5)
	f.login(t, "outsider")
	for _, endpoint := range []string{"/api/slots", "/api/codes"} {
		f.req(t, "GET", fmt.Sprintf("%s?product_id=%d", endpoint, pid), nil, 404)
	}
}

func TestCategoryEnhancementSlotsCodesAndPrompt(t *testing.T) {
	requests := make(chan imageRequest, 4)
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := readImageRequest(t, r)
		requests <- got
		imageReply(w, sizedPhotoBytes(t, got.size))
	}))
	defer ai.Close()
	f := newFixture(t, func(c *Config) {
		c.AIKey = "fake"
		c.AIBaseURL = ai.URL + "/v1"
		c.AutoUpscale = true
		c.AutoFill = true
		c.UpscaleMinSide = 900
	})
	writeEnhancePrompt(t, f.a)
	doc := strings.Replace(enhanceTestPrompt, "COMMON", "{{category_name}} {{category_rules}} {{codes}} {{banned_words}} COMMON", 1)
	if e := os.WriteFile(filepath.Join(f.a.Cfg.PromptDir, "补图.md"), []byte(doc), 0600); e != nil {
		t.Fatal(e)
	}
	f.login(t, "admin")
	slots := Object{"主图": []Object{{"位": 1, "名": "晶体实拍", "候选": []string{"X1"}, "必需": true}}, "详情页": []Object{{"屏": 1, "名": "晶体白底", "候选": []string{"A1"}, "必需": true}}}
	f.req(t, "POST", "/api/categories", Object{"key": "crystal", "name": "水晶", "slots": slots, "codes": []Object{{"code": "X1", "name": "晶体纹路"}, {"code": "A1", "name": "水晶白底"}}, "rules": "CRYSTAL_ENHANCE\n## 水晶特性\n保留晶体", "banned": []string{"水晶禁词"}}, 201)
	pJSON := f.req(t, "POST", "/api/products", Object{"name": "水晶", "category_id": 2, "shop_ids": []int64{1}}, 201)["product"].(Object)
	pid := int64(pJSON["id"].(float64))
	addRealPhoto(t, f, pid, "X1", 64, 80)
	p, e := f.a.product(pid)
	if e != nil {
		t.Fatal(e)
	}
	if targets := f.a.fillTargets(nil, p.Category); js(targets) != `["A1_AI"]` {
		t.Fatal(targets)
	}
	warnings, e := f.a.enhance(testActor(context.Background()), p)
	if e != nil || len(warnings) > 0 {
		t.Fatal(warnings, e)
	}
	for i := 0; i < 2; i++ {
		got := <-requests
		for _, want := range []string{"CRYSTAL_ENHANCE", "## 水晶特性", "保留晶体", "晶体纹路", "水晶禁词", "品类=水晶"} {
			if !strings.Contains(got.prompt, want) {
				t.Fatal(got.prompt)
			}
		}
		if strings.Contains(got.prompt, "珍珠表面") || strings.Contains(got.prompt, `"slots":`) {
			t.Fatal(got.prompt)
		}
	}
	if len(requests) != 0 {
		t.Fatal("生成了当前品类没有的编号")
	}
}
