package assets_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"xstudio/internal/core"
)

type modelAITrace struct {
	Path, Size, Prompt string
	Widths             []int
	N                  int
	Err                string
}
type modelFakeAI struct {
	traces chan modelAITrace
	calls  atomic.Int64
	mode   atomic.Int32
	mu     sync.Mutex
	before func()
}

func modelFixture(t *testing.T) (*fixture, *modelFakeAI) {
	t.Helper()
	fake := &modelFakeAI{traces: make(chan modelAITrace, 30)}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.calls.Add(1)
		tr := modelAITrace{Path: r.URL.Path}
		fake.mu.Lock()
		fn := fake.before
		fake.mu.Unlock()
		if fn != nil {
			fn()
		}
		if r.URL.Path == "/chat/completions" {
			var req obj
			if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
				tr.Err = e.Error()
			}
			messages, _ := req["messages"].([]any)
			if len(messages) == 2 {
				tr.Prompt = messages[0].(obj)["content"].(string)
				parts := messages[1].(obj)["content"].([]any)
				for _, v := range parts {
					p := v.(obj)
					if p["type"] == "image_url" {
						url := p["image_url"].(obj)["url"].(string)
						b, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, "data:image/jpeg;base64,"))
						if e != nil {
							tr.Err = "invalid data URL"
						}
						cfg, _, e := image.DecodeConfig(bytes.NewReader(b))
						if e != nil || max(cfg.Width, cfg.Height) > 768 || cfg.Width < 1 || cfg.Height < 1 {
							tr.Err = "analysis must resize to 768"
						}
						tr.Widths = append(tr.Widths, cfg.Width)
					}
				}
			}
			fake.traces <- tr
			out := obj{"product_type": "耳钉", "body_part": "ear", "framing": "closeup", "description": "耳饰", "suggested_prompt": "保持商品形状", "url": "https://invalid.test/private", "input_id": 999}
			if fake.mode.Load() == 1 {
				out["body_part"] = "invalid"
			}
			if fake.mode.Load() == 2 {
				out["description"] = strings.Repeat("字", 4001)
			}
			core.WriteJSON(w, 200, obj{"choices": []obj{{"message": obj{"content": core.JS(out)}}}})
			return
		}
		if r.URL.Path == "/images/generations" {
			var req struct {
				Size   string
				Prompt string
				N      int
			}
			if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
				tr.Err = e.Error()
			}
			tr.Size, tr.Prompt, tr.N = req.Size, req.Prompt, req.N
		} else if r.URL.Path == "/images/edits" {
			mr, e := r.MultipartReader()
			if e != nil {
				tr.Err = e.Error()
			} else {
				for {
					p, err := mr.NextPart()
					if err == io.EOF {
						break
					}
					if err != nil {
						tr.Err = err.Error()
						break
					}
					b, _ := io.ReadAll(p)
					switch p.FormName() {
					case "size":
						tr.Size = string(b)
					case "prompt":
						tr.Prompt = string(b)
					case "image":
						cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
						if err != nil || format != "png" || p.Header.Get("Content-Type") != "image/png" {
							tr.Err = "reference is not PNG"
						}
						tr.Widths = append(tr.Widths, cfg.Width)
					}
					p.Close()
				}
			}
		} else {
			tr.Err = "unexpected endpoint"
		}
		fake.traces <- tr
		if fake.mode.Load() == 3 {
			core.WriteJSON(w, 200, obj{"data": []obj{{"url": "http://invalid.test/secret"}}})
			return
		}
		if fake.mode.Load() == 4 {
			w.WriteHeader(500)
			io.WriteString(w, "test-key http://private.internal C:\\secret")
			return
		}
		core.WriteJSON(w, 200, obj{"data": []obj{{"b64_json": base64.StdEncoding.EncodeToString(sizedImageBytes(t, tr.Size))}}})
	}))
	t.Cleanup(s.Close)
	f := newFixture(t, func(c *core.Config) {
		c.AIBaseURL = s.URL
		c.AIKey = "test-key"
		c.ImageModel = "fake-image"
		c.VisionModel = "fake-vision"
		c.ImageTimeout = 3 * time.Second
	})
	if e := os.MkdirAll(f.c.Cfg.PromptDir, 0700); e != nil {
		t.Fatal(e)
	}
	templates := map[string]string{
		"模特-创建.md":   "创建 {{description}} {{body_part}} {{extra}}",
		"模特-局部参考.md": "局部 {{description}} {{body_part}} {{extra}}",
		"试穿-识别.md":   "识别 {{hint}}",
		"试穿-生成.md":   "试穿 {{model_description}} {{body_part}} {{framing}} {{product_type}} {{extra}} {{reference_roles}} {{product_info}}",
	}
	for name, body := range templates {
		if e := os.WriteFile(filepath.Join(f.c.Cfg.PromptDir, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	return f, fake
}
func modelCreate(t *testing.T, f *fixture, shop int64) int64 {
	t.Helper()
	return id(f.req(t, "POST", "/api/models", obj{"shop_id": shop, "name": "模特甲", "description": "短发，自然肤色"}, 201)["model"])
}
func modelJob(t *testing.T, f *fixture, path string, in obj) core.Job {
	t.Helper()
	return f.wait(t, id(f.req(t, "POST", path, in, 202)["job"]))
}
func modelCandidate(t *testing.T, f *fixture, mid int64, base *int64) int64 {
	t.Helper()
	in := obj{"body_part": "hand", "prompt": "自然手部", "aspect_ratio": "3:2"}
	if base != nil {
		in["base_reference_id"] = *base
	}
	return wbResult(t, modelJob(t, f, fmt.Sprintf("/api/models/%d/generate", mid), in))
}
func modelConfirm(t *testing.T, f *fixture, mid, rid int64, cover bool) obj {
	t.Helper()
	return f.req(t, "POST", fmt.Sprintf("/api/models/%d/references/confirm", mid), obj{"item_id": rid, "set_cover": cover}, 201)
}
func modelTrace(t *testing.T, fake *modelFakeAI) modelAITrace {
	t.Helper()
	select {
	case tr := <-fake.traces:
		if tr.Err != "" {
			t.Fatal(tr)
		}
		return tr
	case <-time.After(3 * time.Second):
		t.Fatal("missing AI call")
	}
	return modelAITrace{}
}
func modelTryon(mid, ref int64, inputs ...obj) obj {
	return obj{"model_id": mid, "reference_id": ref, "inputs": inputs, "body_part": "ear", "framing": "closeup", "product_type": "耳钉", "prompt": "自然光", "aspect_ratio": "2:3"}
}

func TestModelsCandidateConfirmAndManualTryon(t *testing.T) {
	f, ai := modelFixture(t)
	mid := modelCreate(t, f, 1)
	rid := modelCandidate(t, f, mid, nil)
	tr := modelTrace(t, ai)
	if tr.Path != "/images/generations" || tr.Size != "1536x1024" || tr.N != 1 || !strings.Contains(tr.Prompt, "hand") {
		t.Fatal(tr)
	}
	m := f.req(t, "GET", fmt.Sprintf("/api/models/%d", mid), nil, 200)["model"].(obj)
	if len(m["references"].([]any)) != 0 {
		t.Fatal("candidate auto-saved", m)
	}
	item := f.req(t, "GET", fmt.Sprintf("/api/workbench/items/%d", rid), nil, 200)["item"].(obj)
	op := item["op"].(obj)
	if op["type"] != "model_candidate" || op["template"] != "模特-创建.md" || op["model"] != "fake-image" {
		t.Fatal(item)
	}
	f.req(t, "POST", fmt.Sprintf("/api/workbench/items/%d/save", rid), obj{"product_id": 1}, 400)
	out := modelConfirm(t, f, mid, rid, true)
	ref := id(out["reference"])
	if id(modelConfirm(t, f, mid, rid, false)["reference"]) != ref {
		t.Fatal("confirmation not idempotent")
	}
	rid2 := modelCandidate(t, f, mid, &ref)
	tr = modelTrace(t, ai)
	if tr.Path != "/images/edits" || tr.Size != "1536x1024" || len(tr.Widths) != 1 || tr.Widths[0] != 1536 {
		t.Fatal(tr)
	}
	m = f.req(t, "GET", fmt.Sprintf("/api/models/%d", mid), nil, 200)["model"].(obj)
	if len(m["references"].([]any)) != 1 || int64(m["cover_reference_id"].(float64)) != ref {
		t.Fatal("generation overwrote reference", m)
	}
	f.req(t, "POST", fmt.Sprintf("/api/models/%d/generate", mid), obj{"body_part": "ear", "aspect_ratio": "1:1"}, 400)
	f.req(t, "DELETE", fmt.Sprintf("/api/workbench/items/%d", rid), nil, 200)
	b, _ := wbBinary(t, f, "GET", fmt.Sprintf("/api/models/%d/references/%d/file?w=240", mid, ref), nil, 200)
	if len(b) == 0 {
		t.Fatal("permanent reference removed with candidate")
	}
	pid := f.product(t, "耳钉", 1)
	a1 := f.upload(t, pid, imageBytes(t, 60, 30))
	a2 := f.upload(t, pid, imageBytes(t, 40, 20))
	wbExec(t, f, "UPDATE assets SET code='X01' WHERE id=?", a1)
	wbExec(t, f, "UPDATE products SET info=? WHERE id=?", core.JS(obj{"style": "蝴蝶结", "material": "银", "cost": "SECRET_COST", "supplier": "SECRET_SUPPLIER"}), pid)
	result := wbResult(t, modelJob(t, f, "/api/tryon/generate", modelTryon(mid, ref, obj{"asset_id": a1}, obj{"asset_id": a2})))
	tr = modelTrace(t, ai)
	if tr.Size != "1024x1536" || fmt.Sprint(tr.Widths) != "[1024 1024 1024]" || strings.Contains(tr.Prompt, "SECRET") || !strings.Contains(tr.Prompt, "蝴蝶结") || !strings.Contains(tr.Prompt, "第3张：模特参考图") {
		t.Fatal(tr)
	}
	saved := f.req(t, "POST", fmt.Sprintf("/api/workbench/items/%d/save", result), obj{"set_current": true}, 200)["asset"].(obj)
	if saved["source"] != "ai_generated" || saved["code"] != "" || saved["parent_id"] != float64(a1) {
		t.Fatal(saved)
	}
	if id(f.req(t, "POST", fmt.Sprintf("/api/workbench/items/%d/save", result), obj{}, 200)["asset"]) != id(saved) {
		t.Fatal("save not idempotent")
	}
	var code string
	f.c.DB.QueryRow("SELECT code FROM assets WHERE id=?", a1).Scan(&code)
	if code != "X01" {
		t.Fatal("tryon transferred shooting code")
	}
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", mid), obj{"archived": true}, 200)
	f.req(t, "GET", fmt.Sprintf("/api/workbench/items/%d", rid2), nil, 200)
	wbBinary(t, f, "GET", fmt.Sprintf("/api/workbench/items/%d/file", result), nil, 200)
	f.req(t, "POST", fmt.Sprintf("/api/models/%d/references/confirm", mid), obj{"item_id": rid2}, 404)
	f.req(t, "POST", "/api/tryon/generate", modelTryon(mid, ref, obj{"asset_id": a1}), 404)
}

func TestTryonAnalyzeValidationAndInputBoundaries(t *testing.T) {
	f, ai := modelFixture(t)
	mid := modelCreate(t, f, 1)
	ref := id(modelConfirm(t, f, mid, modelCandidate(t, f, mid, nil), false)["reference"])
	modelTrace(t, ai)
	input := id(wbUpload(t, f, [][]byte{imageBytes(t, 1200, 600)}, 201)["items"].([]any)[0])
	other := wbInput(t, f)
	p1 := f.product(t, "甲", 1)
	p2 := f.product(t, "乙", 1)
	a1 := f.upload(t, p1, imageBytes(t, 20, 40))
	a2 := f.upload(t, p2, imageBytes(t, 20, 40))
	for _, inputs := range [][]obj{{{"asset_id": a1}, {"asset_id": a2}}, {{"asset_id": a1}, {"input_id": input}}, {{"input_id": input}, {"input_id": input}}, {{"input_id": input, "url": "http://invalid.test"}}, {{"input_id": input}, {"input_id": other}, {"input_id": 999}}} {
		f.req(t, "POST", "/api/tryon/analyze", obj{"inputs": inputs}, 400)
	}
	j := modelJob(t, f, "/api/tryon/analyze", obj{"inputs": []obj{{"input_id": input}, {"input_id": other}}, "hint": "耳钉"})
	tr := modelTrace(t, ai)
	if j.Status != "done" || j.Result["body_part"] != "ear" || j.Result["url"] != nil || j.Result["input_id"] != nil || len(tr.Widths) != 2 {
		t.Fatal(j, tr)
	}
	for _, mode := range []int32{1, 2} {
		ai.mode.Store(mode)
		j = modelJob(t, f, "/api/tryon/analyze", obj{"inputs": []obj{{"asset_id": a1}}})
		modelTrace(t, ai)
		if j.Status != "failed" || len(j.Result) != 0 {
			t.Fatal(j)
		}
	}
	ai.mode.Store(0)
	// Failed analysis does not prevent manual generation.
	result := wbResult(t, modelJob(t, f, "/api/tryon/generate", modelTryon(mid, ref, obj{"input_id": input})))
	tr = modelTrace(t, ai)
	if !strings.Contains(tr.Prompt, "不得猜测精确尺寸") {
		t.Fatal(tr)
	}
	f.req(t, "POST", "/api/tryon/analyze", obj{"inputs": []obj{{"input_id": result}}}, 400)
	otherModel := modelCreate(t, f, 1)
	f.req(t, "POST", "/api/tryon/generate", modelTryon(otherModel, ref, obj{"input_id": input}), 400)
	f.req(t, "POST", fmt.Sprintf("/api/models/%d/references/confirm", otherModel), obj{"item_id": result}, 404)
	in := modelTryon(mid, ref, obj{"input_id": input})
	in["prompt"] = ""
	in["product_type"] = ""
	f.req(t, "POST", "/api/tryon/generate", in, 400)
	in = modelTryon(mid, ref, obj{"input_id": input})
	in["prompt"] = strings.Repeat("字", 4001)
	f.req(t, "POST", "/api/tryon/generate", in, 400)
	f.user(t, "operator", 2, 1)
	f.login(t, "operator")
	f.req(t, "POST", "/api/tryon/analyze", obj{"inputs": []obj{{"input_id": input}}}, 400)
}

func TestModelsScopeAndWorkbenchRevocation(t *testing.T) {
	f, ai := modelFixture(t)
	hidden := id(f.req(t, "POST", "/api/shops", obj{"name": "隐藏店", "platform": "other", "language": "en"}, 201)["shop"])
	m1 := modelCreate(t, f, 1)
	m2 := modelCreate(t, f, hidden)
	ref := id(modelConfirm(t, f, m1, modelCandidate(t, f, m1, nil), false)["reference"])
	modelTrace(t, ai)
	// Model shop and source product shop differ, so each revocation is exercised separately.
	pid := f.product(t, "源商品", hidden)
	aid := f.upload(t, pid, imageBytes(t, 30, 30))
	uid := f.user(t, "operator", 2, 1, hidden)
	guest := f.user(t, "guest", 5)
	f.req(t, "POST", fmt.Sprintf("/api/products/%d/shares", pid), obj{"user_id": guest, "can_upload": true}, 201)
	wbExec(t, f, "UPDATE roles SET perms=? WHERE id=5", core.JS([]string{core.ModelView, core.ModelManage, core.AssetUpload, core.AssetProcess, core.ProductView}))
	f.login(t, "guest")
	f.req(t, "GET", "/api/models", nil, 403)
	f.req(t, "GET", fmt.Sprintf("/api/models/%d", m1), nil, 404)
	f.req(t, "POST", "/api/tryon/analyze", obj{"inputs": []obj{{"asset_id": aid}}}, 403)
	f.req(t, "GET", "/api/tryon/state", nil, 403)
	f.login(t, "operator")
	candidate := modelCandidate(t, f, m1, &ref)
	modelTrace(t, ai)
	result := wbResult(t, modelJob(t, f, "/api/tryon/generate", modelTryon(m1, ref, obj{"asset_id": aid})))
	modelTrace(t, ai)
	wbExec(t, f, "DELETE FROM user_shops WHERE user_id=? AND shop_id=?", uid, hidden)
	f.req(t, "GET", fmt.Sprintf("/api/models/%d", m2), nil, 404)
	f.req(t, "GET", fmt.Sprintf("/api/workbench/items/%d", result), nil, 404)
	wbBinary(t, f, "GET", fmt.Sprintf("/api/workbench/items/%d/file?download=1", result), nil, 404)
	f.req(t, "POST", "/api/workbench/download", obj{"ids": []int64{result}}, 404)
	// Supplying a different destination cannot bypass source revocation.
	dest := f.productAsAdmin(t, "目标商品", 1)
	f.req(t, "POST", fmt.Sprintf("/api/workbench/items/%d/save", result), obj{"product_id": dest}, 404)
	if len(f.req(t, "GET", "/api/workbench/items?operation=tryon", nil, 200)["items"].([]any)) != 0 {
		t.Fatal("revoked source still listed")
	}
	f.req(t, "GET", fmt.Sprintf("/api/workbench/items/%d", candidate), nil, 200)
	wbExec(t, f, "DELETE FROM user_shops WHERE user_id=? AND shop_id=1", uid)
	f.req(t, "GET", fmt.Sprintf("/api/workbench/items/%d", candidate), nil, 404)
	wbBinary(t, f, "GET", fmt.Sprintf("/api/models/%d/references/%d/file", m1, ref), nil, 404)
	f.req(t, "POST", fmt.Sprintf("/api/models/%d/references/confirm", m1), obj{"item_id": candidate}, 404)
	f.req(t, "DELETE", fmt.Sprintf("/api/workbench/items/%d", candidate), nil, 200)
	f.req(t, "DELETE", fmt.Sprintf("/api/workbench/items/%d", result), nil, 200)
}
func (f *fixture) productAsAdmin(t *testing.T, name string, shop int64) int64 {
	t.Helper()
	f.login(t, "admin")
	pid := f.product(t, name, shop)
	f.login(t, "operator")
	return pid
}

func TestModelQueueQuotaAndPublicationRecheck(t *testing.T) {
	f, ai := modelFixture(t)
	mid := modelCreate(t, f, 1)
	uid := f.user(t, "operator", 2, 1)
	f.login(t, "operator")
	input := wbInput(t, f)
	f.c.SetBaseURL("")
	out := f.req(t, "POST", "/api/tryon/analyze", obj{"inputs": []obj{{"input_id": input}}}, 202)
	jid := id(out["job"])
	state := f.req(t, "GET", "/api/tryon/state", nil, 200)
	if id(state["job"]) != jid {
		t.Fatal(state)
	}
	f.req(t, "POST", fmt.Sprintf("/api/models/%d/generate", mid), obj{"body_part": "hand", "aspect_ratio": "1:1"}, 409)
	f.req(t, "POST", "/api/workbench/process", obj{"inputs": []obj{{"input_id": input}}, "ops": obj{"format": "png"}}, 409)
	f.login(t, "admin")
	if f.req(t, "GET", "/api/tryon/state", nil, 200)["job"] != nil {
		t.Fatal("exposed another user job")
	}
	wbExec(t, f, "DELETE FROM user_shops WHERE user_id=?", uid)
	wbExec(t, f, "UPDATE roles SET perms='[]' WHERE id=2")
	f.c.SetBaseURL(f.s.URL)
	if j := f.wait(t, jid); j.Status != "failed" || ai.calls.Load() != 0 {
		t.Fatal(j)
	}
	wbExec(t, f, "UPDATE roles SET perms=? WHERE id=2", core.JS([]string{core.ModelView, core.ModelManage, core.AssetUpload, core.AssetProcess, core.ProductView, core.ExportRun}))
	wbExec(t, f, "INSERT INTO user_shops VALUES(?,1)", uid)
	f.req(t, "PUT", fmt.Sprintf("/api/users/%d/quota", uid), obj{"month_images": 0, "month_text": 0}, 200)
	f.login(t, "operator")
	j := modelJob(t, f, fmt.Sprintf("/api/models/%d/generate", mid), obj{"body_part": "hand", "aspect_ratio": "1:1"})
	if j.Status != "failed" || ai.calls.Load() != 0 {
		t.Fatal(j)
	}
	j = modelJob(t, f, "/api/tryon/analyze", obj{"inputs": []obj{{"input_id": input}}})
	if j.Status != "failed" || ai.calls.Load() != 0 {
		t.Fatal(j)
	}
	wbExec(t, f, "UPDATE quotas SET month_images=10,month_text=10 WHERE user_id=?", uid)
	ai.mu.Lock()
	ai.before = func() { wbExec(t, f, "UPDATE shop_models SET archived=1 WHERE id=?", mid) }
	ai.mu.Unlock()
	j = modelJob(t, f, fmt.Sprintf("/api/models/%d/generate", mid), obj{"body_part": "hand", "aspect_ratio": "1:1"})
	modelTrace(t, ai)
	if j.Status != "failed" {
		t.Fatal(j)
	}
	var count int
	f.c.DB.QueryRow("SELECT count(*) FROM workbench_items WHERE kind='result'").Scan(&count)
	if count != 0 {
		t.Fatal("published after model archived")
	}
	files, e := filepath.Glob(filepath.Join(f.c.Cfg.DataDir, "workbench", fmt.Sprint(uid), "results", "*"))
	if e != nil || len(files) != 0 {
		t.Fatal("orphan result files", files, e)
	}
	var charged int
	f.c.DB.QueryRow("SELECT count(*) FROM ai_usage WHERE user_id=? AND feature='model_create' AND job_id=?", uid, j.ID).Scan(&charged)
	if charged != 1 {
		t.Fatal("wrong actor/feature usage")
	}
}

func TestModelFailurePrivacyAndFilterBeforeLimit(t *testing.T) {
	f, ai := modelFixture(t)
	mid := modelCreate(t, f, 1)
	ai.mode.Store(4)
	j := modelJob(t, f, fmt.Sprintf("/api/models/%d/generate", mid), obj{"body_part": "hand", "aspect_ratio": "1:1"})
	if j.Status != "failed" || strings.Contains(j.Message, "test-key") || strings.Contains(j.Message, "private.internal") || ai.calls.Load() != 2 {
		t.Fatal(j)
	}
	modelTrace(t, ai)
	modelTrace(t, ai)
	ai.mode.Store(3)
	j = modelJob(t, f, fmt.Sprintf("/api/models/%d/generate", mid), obj{"body_part": "hand", "aspect_ratio": "1:1"})
	if j.Status != "failed" {
		t.Fatal(j)
	}
	modelTrace(t, ai)
	modelTrace(t, ai)
	ai.mode.Store(0)
	candidate := modelCandidate(t, f, mid, nil)
	modelTrace(t, ai)
	for i := 0; i < 205; i++ {
		wbExec(t, f, "INSERT INTO workbench_items(user_id,kind,file,orig_name,width,height,source,op,created_at) VALUES(1,'result','missing.png','ordinary',1,1,'original','{}',?)", core.Now())
	}
	listed := f.req(t, "GET", "/api/workbench/items?operation=model_candidate", nil, 200)["items"].([]any)
	if len(listed) != 1 || id(listed[0]) != candidate {
		t.Fatal("operation filtered after LIMIT", listed)
	}
	if n := len(f.req(t, "GET", "/api/workbench/items?operation=process", nil, 200)["items"].([]any)); n != 200 {
		t.Fatal(n)
	}
	if n := len(f.req(t, "GET", "/api/workbench/items", nil, 200)["items"].([]any)); n != 200 {
		t.Fatal(n)
	}
	for i := 0; i < 205; i++ {
		wbExec(t, f, "INSERT INTO workbench_items(user_id,kind,file,orig_name,width,height,source,op,created_at) VALUES(1,'result','missing.png','revoked',1,1,'ai_generated',?,?)", core.JS(obj{"type": "model_candidate", "shop_id": 999, "model_id": mid}), core.Now())
	}
	listed = f.req(t, "GET", "/api/workbench/items?operation=model_candidate", nil, 200)["items"].([]any)
	if len(listed) != 1 || id(listed[0]) != candidate {
		t.Fatal("permission filtering starved visible candidates", listed)
	}
	if e := os.Remove(filepath.Join(f.c.Cfg.PromptDir, "模特-创建.md")); e != nil {
		t.Fatal(e)
	}
	calls := ai.calls.Load()
	j = modelJob(t, f, fmt.Sprintf("/api/models/%d/generate", mid), obj{"body_part": "hand", "aspect_ratio": "1:1"})
	if j.Status != "failed" || !strings.Contains(j.Message, "缺少提示词") || ai.calls.Load() != calls {
		t.Fatal(j)
	}
}

func TestModelUploadImmutableIdentityAndCover(t *testing.T) {
	f, _ := modelFixture(t)
	mid := modelCreate(t, f, 1)
	other := modelCreate(t, f, 1)
	upload := func(mid int64, n int, part string, status int) obj {
		t.Helper()
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		mw.WriteField("body_part", part)
		for i := 0; i < n; i++ {
			p, _ := mw.CreateFormFile("files", "reference.png")
			p.Write(imageBytes(t, 15, 25))
		}
		mw.Close()
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/models/%d/references", f.s.URL, mid), &b)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Requested-With", "fetch")
		res, e := f.client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var out obj
		json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode != status {
			t.Fatal(res.StatusCode, out)
		}
		return out
	}
	upload(mid, 2, "hand", 400)
	upload(mid, 1, "invalid", 400)
	ref := id(upload(mid, 1, "ear", 201)["reference"])
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", other), obj{"cover_reference_id": ref}, 400)
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", mid), obj{"shop_id": nil}, 200)
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", mid), obj{"shop_id": 1}, 200)
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", mid), obj{"cover_reference_id": ref}, 200)
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", mid), obj{"cover_reference_id": nil}, 200)
	wbBinary(t, f, "GET", fmt.Sprintf("/api/models/%d/references/%d/file?w=480", mid, ref), nil, 200)
	f.req(t, "GET", fmt.Sprintf("/api/models/%d/references/%d/file?w=100", mid, ref), nil, 400)
	if _, e := f.c.DB.Exec("UPDATE shop_models SET shop_id=999 WHERE id=?", mid); e == nil {
		t.Fatal("identity changed")
	}
	f.req(t, "PATCH", "/api/shops/1", obj{"archived": true}, 200)
	f.req(t, "POST", "/api/models", obj{"shop_id": 1, "name": "bad"}, 404)
	wbBinary(t, f, "GET", fmt.Sprintf("/api/models/%d/references/%d/file", mid, ref), nil, 200)
}

func TestModelReferenceDeletion(t *testing.T) {
	f := newFixture(t, nil)
	mid := modelCreate(t, f, 1)
	other := modelCreate(t, f, 1)
	dir := filepath.Join(f.c.Cfg.DataDir, "models", fmt.Sprint(mid))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	add := func(name string) int64 {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), imageBytes(t, 12, 12), 0600); err != nil {
			t.Fatal(err)
		}
		result, err := f.c.DB.Exec(`INSERT INTO model_references(model_id,body_part,source,file,orig_name,width,height,created_by,created_at) VALUES(?,'hand','original',?,?,12,12,1,?)`, mid, name, name, core.Now())
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		return id
	}
	first, second := add("first.png"), add("second.png")
	path := fmt.Sprintf("/api/models/%d/references/%d", mid, first)
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", mid), obj{"cover_reference_id": first}, 200)
	f.user(t, "viewer-delete", 4, 1)
	f.user(t, "outside-delete", 2)
	f.login(t, "viewer-delete")
	f.req(t, "DELETE", path, nil, 403)
	f.login(t, "outside-delete")
	f.req(t, "DELETE", path, nil, 404)
	f.login(t, "admin")
	f.req(t, "DELETE", fmt.Sprintf("/api/models/%d/references/%d", other, first), nil, 404)
	wbExec(t, f, `INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,steps,created_at) VALUES('assets.tryon','user',1,1,?,'running','',?)`, core.JS(obj{"model_id": mid, "reference_id": first}), core.Now())
	f.req(t, "DELETE", path, nil, 409)
	wbBinary(t, f, "GET", path+"/file", nil, 200)
	wbExec(t, f, "UPDATE jobs SET status='done' WHERE kind='assets.tryon'")
	deleted := f.req(t, "DELETE", path, nil, 200)["model"].(obj)
	if deleted["cover_reference_id"] != float64(second) || len(deleted["references"].([]any)) != 1 {
		t.Fatal("deleted reference still visible or cover was not replaced", deleted)
	}
	if _, err := os.Stat(filepath.Join(dir, "first.png")); !os.IsNotExist(err) {
		t.Fatal("reference file was not removed", err)
	}
	f.req(t, "DELETE", path, nil, 404)
	wbBinary(t, f, "GET", path+"/file", nil, 404)
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", mid), obj{"cover_reference_id": first}, 400)
	last := f.req(t, "DELETE", fmt.Sprintf("/api/models/%d/references/%d", mid, second), nil, 200)["model"].(obj)
	if last["cover_reference_id"] != nil || len(last["references"].([]any)) != 0 {
		t.Fatal("last reference did not clear cover", last)
	}
	if added := add("new.png"); added <= second {
		t.Fatal("deleted reference ID was reused")
	}
	f.assertAudit(t, "assets.model.reference.delete")
}

func TestConcurrentModelCandidateConfirmation(t *testing.T) {
	f, ai := modelFixture(t)
	mid := modelCreate(t, f, 1)
	candidate := modelCandidate(t, f, mid, nil)
	modelTrace(t, ai)
	type result struct {
		status int
		id     int64
		err    error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			req, e := http.NewRequest("POST", fmt.Sprintf("%s/api/models/%d/references/confirm", f.s.URL, mid), strings.NewReader(core.JS(obj{"item_id": candidate, "set_cover": true})))
			if e != nil {
				results <- result{err: e}
				return
			}
			req.Header.Set("X-Requested-With", "fetch")
			req.Header.Set("Content-Type", "application/json")
			res, e := f.client.Do(req)
			if e != nil {
				results <- result{err: e}
				return
			}
			defer res.Body.Close()
			var out obj
			e = json.NewDecoder(res.Body).Decode(&out)
			var rid int64
			if e == nil && res.StatusCode == 201 {
				rid = id(out["reference"])
			}
			results <- result{status: res.StatusCode, id: rid, err: e}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.status != 201 || second.status != 201 || first.id == 0 || first.id != second.id {
		t.Fatal(first, second)
	}
	var count int
	f.c.DB.QueryRow("SELECT count(*) FROM model_references WHERE model_id=?", mid).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate permanent references", count)
	}
	files, e := filepath.Glob(filepath.Join(f.c.Cfg.DataDir, "models", fmt.Sprint(mid), "*.png"))
	if e != nil || len(files) != 1 {
		t.Fatal(files, e)
	}
}

func TestModelJobsRecheckRevocationBeforePublication(t *testing.T) {
	for _, kind := range []string{"candidate_permission", "tryon_shop", "tryon_source", "analyze_source"} {
		t.Run(kind, func(t *testing.T) {
			f, ai := modelFixture(t)
			shop := id(f.req(t, "POST", "/api/shops", obj{"name": "商品店", "platform": "other", "language": "en"}, 201)["shop"])
			mid := modelCreate(t, f, 1)
			ref := id(modelConfirm(t, f, mid, modelCandidate(t, f, mid, nil), false)["reference"])
			modelTrace(t, ai)
			pid := f.product(t, "商品", shop)
			aid := f.upload(t, pid, imageBytes(t, 20, 30))
			uid := f.user(t, "operator", 2, 1, shop)
			f.login(t, "operator")
			ai.mu.Lock()
			ai.before = func() {
				switch kind {
				case "candidate_permission":
					wbExec(t, f, "UPDATE roles SET perms=? WHERE id=2", core.JS([]string{core.AssetUpload, core.AssetProcess, core.ModelView}))
				case "tryon_shop":
					wbExec(t, f, "DELETE FROM user_shops WHERE user_id=? AND shop_id=1", uid)
				default:
					wbExec(t, f, "DELETE FROM user_shops WHERE user_id=? AND shop_id=?", uid, shop)
				}
			}
			ai.mu.Unlock()
			var j core.Job
			switch kind {
			case "candidate_permission":
				j = modelJob(t, f, fmt.Sprintf("/api/models/%d/generate", mid), obj{"body_part": "hand", "aspect_ratio": "1:1", "base_reference_id": ref})
			case "analyze_source":
				j = modelJob(t, f, "/api/tryon/analyze", obj{"inputs": []obj{{"asset_id": aid}}})
			default:
				j = modelJob(t, f, "/api/tryon/generate", modelTryon(mid, ref, obj{"asset_id": aid}))
			}
			modelTrace(t, ai)
			if j.Status != "failed" || len(j.Result) != 0 {
				t.Fatal("published after revocation", j)
			}
			var count int
			f.c.DB.QueryRow("SELECT count(*) FROM workbench_items WHERE user_id=? AND kind='result'", uid).Scan(&count)
			if count != 0 {
				t.Fatal("leaked result", count)
			}
			files, e := filepath.Glob(filepath.Join(f.c.Cfg.DataDir, "workbench", fmt.Sprint(uid), "results", "*"))
			if e != nil || len(files) != 0 {
				t.Fatal("orphan output", files, e)
			}
		})
	}
}
