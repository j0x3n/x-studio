package props

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/products"
	"xstudio/internal/modules/scenes"
)

type aiCall struct {
	Path   string
	Size   string
	Prompt string
	Err    string
}

type fakeAI struct {
	mu    sync.Mutex
	calls []aiCall
}

func (ai *fakeAI) add(call aiCall) {
	ai.mu.Lock()
	ai.calls = append(ai.calls, call)
	ai.mu.Unlock()
}
func (ai *fakeAI) list() []aiCall {
	ai.mu.Lock()
	defer ai.mu.Unlock()
	out := make([]aiCall, len(ai.calls))
	copy(out, ai.calls)
	return out
}

type propFixture struct {
	c      *core.Core
	server *httptest.Server
	client *http.Client
	ai     *fakeAI
}

func pngOfSize(size string) ([]byte, error) {
	var width, height int
	if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil || width <= 0 || height <= 0 {
		width, height = 1024, 1024
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
func solidPNG(t *testing.T, width, height int, ink color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, ink)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func newPropFixture(t *testing.T) *propFixture {
	t.Helper()
	ai := &fakeAI{}
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		call := aiCall{Path: request.URL.Path}
		if request.URL.Path == "/images/generations" {
			var body struct {
				Size   string `json:"size"`
				Prompt string `json:"prompt"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				call.Err = err.Error()
			}
			call.Size, call.Prompt = body.Size, body.Prompt
		} else if request.URL.Path == "/images/edits" {
			reader, err := request.MultipartReader()
			if err != nil {
				call.Err = err.Error()
			} else {
				for {
					part, err := reader.NextPart()
					if err == io.EOF {
						break
					}
					if err != nil {
						call.Err = err.Error()
						break
					}
					data, _ := io.ReadAll(part)
					switch part.FormName() {
					case "size":
						call.Size = string(data)
					case "prompt":
						call.Prompt = string(data)
					}
					part.Close()
				}
			}
		} else {
			call.Err = "unexpected " + request.URL.Path
		}
		ai.add(call)
		raw, err := pngOfSize(call.Size)
		if err != nil {
			http.Error(writer, err.Error(), 500)
			return
		}
		core.WriteJSON(writer, 200, core.Object{"data": []core.Object{{"b64_json": base64.StdEncoding.EncodeToString(raw)}}})
	}))
	t.Cleanup(provider.Close)
	dir := t.TempDir()
	promptDir, err := filepath.Abs(filepath.Join("..", "..", "..", "prompts"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := core.Config{DataDir: filepath.Join(dir, "store"), TemplateDir: filepath.Join(dir, "templates"), PromptDir: promptDir, AdminUser: "admin", AdminPassword: "password123", SessionSecret: strings.Repeat("s", 32), Workers: 1, AIBaseURL: provider.URL, AIKey: "fake", ImageModel: "fake-image", ImageTimeout: 5 * time.Second}
	app, err := core.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(accounts.New(app), products.New(app), assets.New(app), New(app), scenes.New(app)); err != nil {
		app.Close()
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	app.SetBaseURL(server.URL)
	jar, _ := cookiejar.New(nil)
	fixture := &propFixture{c: app, server: server, client: &http.Client{Jar: jar}, ai: ai}
	t.Cleanup(func() { server.Close(); app.Close() })
	fixture.request(t, "POST", "/api/login", core.Object{"username": "admin", "password": "password123"}, 200)
	return fixture
}
func (f *propFixture) request(t *testing.T, method, path string, body any, status int) core.Object {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(core.JS(body))
	}
	request, err := http.NewRequest(method, f.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Requested-With", "fetch")
	response, err := f.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, response.StatusCode, status, data)
	}
	var out core.Object
	if len(data) > 0 {
		if err = json.Unmarshal(data, &out); err != nil {
			t.Fatal(err, string(data))
		}
	}
	return out
}
func (f *propFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.c.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
func (f *propFixture) wait(t *testing.T, id int64) core.Job {
	t.Helper()
	until := time.Now().Add(15 * time.Second)
	for time.Now().Before(until) {
		job, err := core.ScanJob(f.c.DB.QueryRow("SELECT "+core.JobColumns+" FROM jobs WHERE id=?", id))
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "done" || job.Status == "failed" {
			return job
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("任务超时")
	return core.Job{}
}
func (f *propFixture) upload(t *testing.T, propID int64, data []byte, view string, lock bool, status int) core.Object {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "ref.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if view != "" {
		if err = writer.WriteField("view", view); err != nil {
			t.Fatal(err)
		}
	}
	if lock {
		if err = writer.WriteField("lock", "true"); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest("POST", fmt.Sprintf("%s/api/props/%d/refs", f.server.URL, propID), &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Requested-With", "fetch")
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := f.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("upload %d: %s", response.StatusCode, raw)
	}
	var out core.Object
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err, string(raw))
	}
	return out
}
func propID(t *testing.T, body core.Object) int64 {
	t.Helper()
	prop, ok := body["prop"].(map[string]any)
	if !ok {
		t.Fatal(body)
	}
	return int64(prop["id"].(float64))
}
func TestPropsCRUDAndShopVisibility(t *testing.T) {
	f := newPropFixture(t)
	for _, body := range []core.Object{
		{"name": "", "kind": "table", "description": "木头", "size_cm": core.Object{"w": 10}},
		{"name": "桌", "kind": "nope", "description": "木头", "size_cm": core.Object{"w": 10}},
		{"name": "桌", "kind": "table", "description": "木头", "size_cm": core.Object{}},
	} {
		f.request(t, "POST", "/api/props", body, 400)
	}
	f.exec(t, "INSERT INTO shops(id,name,platform,language,created_at) VALUES(99,'不可见店铺','taobao','zh-CN',?)", core.Now())
	globalID := propID(t, f.request(t, "POST", "/api/props", core.Object{"name": "胡桃木桌", "kind": "table", "description": "深棕桌面", "size_cm": core.Object{"w": 60, "d": 40}}, 201))
	ownID := propID(t, f.request(t, "POST", "/api/props", core.Object{"name": "本店礼盒", "kind": "container", "description": "白盒子", "size_cm": core.Object{"h": 8}, "shop_id": 1}, 201))
	foreignID := propID(t, f.request(t, "POST", "/api/props", core.Object{"name": "别店花", "kind": "plant", "description": "一枝白花", "size_cm": core.Object{"h": 20}, "shop_id": 99}, 201))
	var audits int
	if err := f.c.DB.QueryRow("SELECT count(*) FROM audit_log WHERE action='prop.create'").Scan(&audits); err != nil || audits < 3 {
		t.Fatal(audits, err)
	}
	picture := solidPNG(t, 12, 8, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	uploaded := f.upload(t, globalID, picture, "main", true, 201)["ref"].(map[string]any)
	response, err := f.client.Get(f.server.URL + uploaded["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	received, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Equal(received, picture) {
		t.Fatal(response.StatusCode, len(received))
	}
	detail := f.request(t, "GET", fmt.Sprintf("/api/props/%d", globalID), nil, 200)["prop"].(map[string]any)
	if int64(detail["cover_ref_id"].(float64)) != int64(uploaded["id"].(float64)) || detail["locked_views"].(map[string]any)["main"].(float64) != 1 {
		t.Fatal(detail)
	}
	tables := f.request(t, "GET", "/api/props?kind=table&q=胡桃", nil, 200)["items"].([]any)
	if len(tables) != 1 {
		t.Fatal(tables)
	}
	loose := propID(t, f.request(t, "POST", "/api/props", core.Object{"name": "临时布", "kind": "fabric", "description": "一块灰布", "size_cm": core.Object{"w": 30}}, 201))
	looseFile := f.upload(t, loose, picture, "", false, 201)
	var saved string
	refID := int64(looseFile["ref"].(map[string]any)["id"].(float64))
	if err = f.c.DB.QueryRow("SELECT file FROM prop_refs WHERE id=?", refID).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	removed := f.request(t, "DELETE", fmt.Sprintf("/api/props/%d", loose), nil, 200)
	if removed["deleted"] != true {
		t.Fatal(removed)
	}
	if _, err = os.Stat(saved); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	f.request(t, "GET", fmt.Sprintf("/api/props/%d", loose), nil, 404)
	var password []byte
	if err = f.c.DB.QueryRow("SELECT password_hash FROM users WHERE id=1").Scan(&password); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "INSERT INTO roles(id,key,name,scope,perms,builtin,created_at) VALUES(99,'prop_reader','道具读者','shops',?,0,?)", core.JS([]string{core.ProductView}), core.Now())
	f.exec(t, "INSERT INTO users(id,username,password_hash,display_name,role_id,created_at) VALUES(99,'reader',?,'读者',99,?)", password, core.Now())
	f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(99,1)")
	jar, _ := cookiejar.New(nil)
	f.client = &http.Client{Jar: jar}
	f.request(t, "POST", "/api/login", core.Object{"username": "reader", "password": "password123"}, 200)
	items := f.request(t, "GET", "/api/props", nil, 200)["items"].([]any)
	if len(items) != 2 {
		t.Fatal(items)
	}
	f.request(t, "GET", fmt.Sprintf("/api/props/%d", globalID), nil, 200)
	f.request(t, "GET", fmt.Sprintf("/api/props/%d", ownID), nil, 200)
	f.request(t, "GET", fmt.Sprintf("/api/props/%d", foreignID), nil, 404)
	f.request(t, "POST", "/api/props", core.Object{"name": "无权", "kind": "other", "description": "不能建", "size_cm": core.Object{"w": 1}}, 403)
	f.request(t, "PATCH", fmt.Sprintf("/api/props/%d", ownID), core.Object{"name": "不能改"}, 403)
	f.request(t, "DELETE", fmt.Sprintf("/api/props/%d", ownID), nil, 403)
}
func TestPropGenerateLockCoverAndViews(t *testing.T) {
	f := newPropFixture(t)
	id := propID(t, f.request(t, "POST", "/api/props", core.Object{"name": "胡桃木桌", "kind": "table", "description": "深棕胡桃木台面", "size_cm": core.Object{"w": 60, "d": 40, "h": 2}}, 201))
	path := fmt.Sprintf("/api/props/%d", id)
	created := f.request(t, "POST", path+"/generate", core.Object{"count": 2, "extra": "边缘有金线"}, 202)
	job := f.wait(t, int64(created["job_id"].(float64)))
	if job.Status != "done" {
		t.Fatal(job.Status, job.Message)
	}
	calls := f.ai.list()
	if len(calls) != 2 {
		t.Fatal(calls)
	}
	for _, call := range calls {
		if call.Err != "" || call.Path != "/images/generations" || call.Size != "1024x1024" || !strings.Contains(call.Prompt, "深棕胡桃木台面") || !strings.Contains(call.Prompt, "浅灰") || !strings.Contains(call.Prompt, "宽 60 cm") || strings.Contains(call.Prompt, "不给模型看") {
			t.Fatal(call)
		}
	}
	detail := f.request(t, "GET", path, nil, 200)["prop"].(map[string]any)
	if detail["cover_ref_id"] != nil {
		t.Fatal(detail)
	}
	refs := detail["refs"].([]any)
	if len(refs) != 2 {
		t.Fatal(refs)
	}
	for _, raw := range refs {
		ref := raw.(map[string]any)
		if ref["view"] != "main" || ref["status"] != "candidate" || ref["source"] != "ai" {
			t.Fatal(ref)
		}
	}
	f.request(t, "POST", path+"/views", core.Object{"views": []string{"left"}}, 409)
	f.request(t, "POST", path+"/views", core.Object{"views": []string{"main"}}, 400)
	if len(f.ai.list()) != 2 {
		t.Fatal(f.ai.list())
	}
	first := int64(refs[0].(map[string]any)["id"].(float64))
	second := int64(refs[1].(map[string]any)["id"].(float64))
	locked := f.request(t, "POST", fmt.Sprintf("%s/refs/%d/lock", path, first), nil, 200)["prop"].(map[string]any)
	if int64(locked["cover_ref_id"].(float64)) != first {
		t.Fatal(locked)
	}
	replaced := f.request(t, "POST", fmt.Sprintf("%s/refs/%d/lock", path, second), nil, 200)["prop"].(map[string]any)
	if int64(replaced["cover_ref_id"].(float64)) != second {
		t.Fatal(replaced)
	}
	var discarded string
	var discardedFile string
	if err := f.c.DB.QueryRow("SELECT status,file FROM prop_refs WHERE id=?", first).Scan(&discarded, &discardedFile); err != nil || discarded != "discarded" {
		t.Fatal(discarded, err)
	}
	if _, err := os.Stat(discardedFile); err != nil {
		t.Fatal(err)
	}
	response, err := f.client.Get(f.server.URL + fmt.Sprintf("/api/props/%d/refs/%d/file", id, first))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	viewJob := f.request(t, "POST", path+"/views", core.Object{"views": []string{"left", "top"}}, 202)
	done := f.wait(t, int64(viewJob["job_id"].(float64)))
	if done.Status != "done" {
		t.Fatal(done.Status, done.Message)
	}
	calls = f.ai.list()
	if len(calls) != 4 || calls[2].Path != "/images/edits" || calls[3].Path != "/images/edits" {
		t.Fatal(calls)
	}
	if !strings.Contains(calls[2].Prompt, "同一个物体") || !strings.Contains(calls[2].Prompt, "左侧") || !strings.Contains(calls[3].Prompt, "正上方") || strings.Contains(calls[2].Prompt, "不给模型看") {
		t.Fatal(calls[2].Prompt, calls[3].Prompt)
	}
	final := f.request(t, "GET", path, nil, 200)["prop"].(map[string]any)
	if int64(final["cover_ref_id"].(float64)) != second {
		t.Fatal(final)
	}
}
func TestPropDeleteArchivesWhenSceneReferences(t *testing.T) {
	f := newPropFixture(t)
	id := propID(t, f.request(t, "POST", "/api/props", core.Object{"name": "胡桃木桌", "kind": "table", "description": "深棕桌面", "size_cm": core.Object{"w": 60}}, 201))
	picture := solidPNG(t, 8, 8, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	ref := f.upload(t, id, picture, "main", true, 201)["ref"].(map[string]any)
	var saved string
	if err := f.c.DB.QueryRow("SELECT file FROM prop_refs WHERE id=?", int64(ref["id"].(float64))).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	scene := f.request(t, "POST", "/api/scenes", core.Object{"name": "桌面场景", "still": "木桌", "layout": core.Object{"size": "1024x1024", "elements": []core.Object{{"id": "e1", "type": "prop", "prop_id": id, "label": "胡桃木桌", "box": []float64{0.1, 0.5, 0.8, 0.4}, "note": ""}}}}, 201)
	sceneID := int64(scene["scene"].(map[string]any)["id"].(float64))
	archived := f.request(t, "DELETE", fmt.Sprintf("/api/props/%d", id), nil, 200)
	if archived["archived"] != true || archived["deleted"] == true {
		t.Fatal(archived)
	}
	if _, err := os.Stat(saved); err != nil {
		t.Fatal(err)
	}
	again := f.request(t, "DELETE", fmt.Sprintf("/api/props/%d", id), nil, 200)
	if again["archived"] != true {
		t.Fatal(again)
	}
	prop := f.request(t, "GET", fmt.Sprintf("/api/props/%d", id), nil, 200)["prop"].(map[string]any)
	if prop["archived"] != true {
		t.Fatal(prop)
	}
	var count int
	if err := f.c.DB.QueryRow("SELECT count(*) FROM audit_log WHERE action='prop.archive' AND target_id=?", id).Scan(&count); err != nil || count < 1 {
		t.Fatal(count, err)
	}
	f.request(t, "PATCH", fmt.Sprintf("/api/scenes/%d", sceneID), core.Object{"layout": core.Object{"size": "1024x1024", "elements": []any{}}}, 200)
	deleted := f.request(t, "DELETE", fmt.Sprintf("/api/props/%d", id), nil, 200)
	if deleted["deleted"] != true {
		t.Fatal(deleted)
	}
	if _, err := os.Stat(saved); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	f.request(t, "GET", fmt.Sprintf("/api/props/%d", id), nil, 404)
}
