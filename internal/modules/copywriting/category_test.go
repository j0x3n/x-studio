package copywriting_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"xstudio/internal/core"
)

func TestCategoryCopywritingPromptsAndCompliance(t *testing.T) {
	requests := make(chan Object, 5)
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body Object
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		requests <- body
		prompt := body["messages"].([]any)[0].(Object)["content"].(string)
		reply := `{"content":{"name":"水晶","oneLiner":"水晶专属禁词"},"main":{"imgs":[]},"listing":{"title":"水晶标题"}}`
		if strings.Contains(prompt, "改写") {
			reply = `{"meta":{"title":"新水晶标题"}}`
		}
		if strings.Contains(prompt, "合规") {
			reply = `{"issues":[]}`
		}
		json.NewEncoder(w).Encode(Object{"choices": []Object{{"message": Object{"content": reply}}}})
	}))
	defer ai.Close()
	env := newTestEnv(t, func(c *core.Config) { c.AIBaseURL = ai.URL })
	for _, name := range []string{"文案.md", "文案-附加.md", "文案-改写.md", "合规审查.md"} {
		if e := os.WriteFile(filepath.Join(env.c.Cfg.PromptDir, name), []byte(name+" {{category_name}} {{category_rules}} {{codes}} {{banned_words}}"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	env.login(t, "admin", "AdminPassword123!")
	slots := Object{"主图": []Object{{"位": 1, "名": "晶体", "候选": []string{"X1"}, "必需": true}}, "详情页": []Object{}}
	env.req(t, "POST", "/api/categories", Object{"key": "crystal", "name": "水晶", "rules": "CRYSTAL_COPY_RULES", "slots": slots, "codes": []Object{{"code": "X1", "name": "晶体照片", "desc": "晶体纹路"}}, "banned": []string{"水晶专属禁词"}}, 201)
	p := env.req(t, "POST", "/api/products", Object{"name": "水晶", "category_id": 2, "shop_ids": []int64{1}}, 201)["product"].(Object)
	lid := int64(p["listings"].([]any)[0].(Object)["id"].(float64))
	// Disabled categories remain usable by their existing products.
	env.req(t, "PATCH", "/api/categories/2", Object{"disabled": true}, 200)
	job := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies/generate", lid), Object{}, 202)["job"].(Object)
	done := env.waitJob(t, int64(job["id"].(float64)))
	if done["status"] != "done" {
		t.Fatal(done)
	}
	cid := int64(done["result"].(Object)["copy_id"].(float64))
	checked := env.req(t, "POST", fmt.Sprintf("/api/copies/%d/check?ai=0", cid), nil, 200)["issues"].([]any)
	if len(checked) != 1 || checked[0].(Object)["reason"] != "水晶违禁词" {
		t.Fatal(checked)
	}
	job = env.req(t, "POST", fmt.Sprintf("/api/copies/%d/rewrite", cid), Object{"scope": "title", "instructions": "改标题"}, 202)["job"].(Object)
	done = env.waitJob(t, int64(job["id"].(float64)))
	if done["status"] != "done" {
		t.Fatal(done)
	}
	env.req(t, "POST", fmt.Sprintf("/api/copies/%d/check", cid), nil, 200)
	for i := 0; i < 3; i++ {
		body := <-requests
		messages := body["messages"].([]any)
		prompt := messages[0].(Object)["content"].(string)
		if !strings.Contains(prompt, "CRYSTAL_COPY_RULES") || !strings.Contains(prompt, "晶体纹路") || !strings.Contains(prompt, "水晶专属禁词") || strings.Contains(prompt, "{{category_") {
			t.Fatal(prompt)
		}
		if i == 0 {
			var input Object
			if e := json.Unmarshal([]byte(messages[1].(Object)["content"].(string)), &input); e != nil {
				t.Fatal(e)
			}
			if core.JS(input["slots"]) != core.JS(slots) || input["category"].(Object)["name"] != "水晶" || len(input["coverage"].(Object)["main"].([]any)) != 1 {
				t.Fatal(input)
			}
		}
	}
	if e := os.Remove(filepath.Join(env.c.Cfg.PromptDir, "违禁词.txt")); e != nil {
		t.Fatal(e)
	}
	manual := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies", lid), Object{"content": Object{"oneLiner": "水晶专属禁词"}, "main": Object{}, "meta": Object{"title": "水晶"}}, 201)["copy"].(Object)
	savedIssues := manual["meta"].(Object)["compliance"].([]any)
	if len(savedIssues) != 1 || savedIssues[0].(Object)["reason"] != "水晶违禁词" {
		t.Fatal(savedIssues)
	}
	pearl := env.req(t, "POST", "/api/products", Object{"name": "珍珠", "category_id": 1, "shop_ids": []int64{1}}, 201)["product"].(Object)
	plid := int64(pearl["listings"].([]any)[0].(Object)["id"].(float64))
	version := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/copies", plid), Object{"content": Object{"oneLiner": "水晶专属禁词"}, "main": Object{}, "meta": Object{"title": "珍珠"}}, 201)["copy"].(Object)
	issues := env.req(t, "POST", fmt.Sprintf("/api/copies/%.0f/check?ai=0", version["id"].(float64)), nil, 200)["issues"].([]any)
	if len(issues) != 0 {
		t.Fatal(issues)
	}
}
