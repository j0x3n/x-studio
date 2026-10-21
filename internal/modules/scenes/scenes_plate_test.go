package scenes

import (
	"bytes"
	"context"
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

	"golang.org/x/image/draw"
	"xstudio/internal/core"
)

type plateCall struct {
	Path    string
	Size    string
	Prompt  string
	Err     string
	Corners [][3]uint8
}

type plateAI struct {
	mu    sync.Mutex
	calls []plateCall
}

func (ai *plateAI) add(call plateCall) {
	ai.mu.Lock()
	ai.calls = append(ai.calls, call)
	ai.mu.Unlock()
}
func (ai *plateAI) list() []plateCall {
	ai.mu.Lock()
	defer ai.mu.Unlock()
	out := make([]plateCall, len(ai.calls))
	copy(out, ai.calls)
	return out
}
func startPlateAI(t *testing.T) (*httptest.Server, *plateAI) {
	t.Helper()
	ai := &plateAI{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		call := plateCall{Path: request.URL.Path}
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
					case "image":
						img, decErr := png.Decode(bytes.NewReader(data))
						if decErr != nil {
							call.Err = decErr.Error()
						} else {
							r, g, b, _ := img.At(img.Bounds().Min.X, img.Bounds().Min.Y).RGBA()
							call.Corners = append(call.Corners, [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)})
						}
					}
					part.Close()
				}
			}
		} else {
			call.Err = "unexpected " + request.URL.Path
		}
		ai.add(call)
		raw, err := platePNG(call.Size)
		if err != nil {
			http.Error(writer, err.Error(), 500)
			return
		}
		core.WriteJSON(writer, 200, core.Object{"data": []core.Object{{"b64_json": base64.StdEncoding.EncodeToString(raw)}}})
	}))
	t.Cleanup(server.Close)
	return server, ai
}
func platePNG(size string) ([]byte, error) {
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
	draw.Draw(img, img.Bounds(), image.NewUniform(ink), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func (f *sceneFixture) uploadScene(t *testing.T, sceneID int64, data []byte, status int) core.Object {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "still.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = writer.WriteField("use", "still"); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest("POST", fmt.Sprintf("%s/api/scenes/%d/refs", f.server.URL, sceneID), &body)
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
func (f *sceneFixture) uploadProp(t *testing.T, propID int64, data []byte, lock bool) core.Object {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "prop.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = writer.WriteField("view", "main"); err != nil {
		t.Fatal(err)
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
	if response.StatusCode != 201 {
		t.Fatalf("prop upload %d: %s", response.StatusCode, raw)
	}
	var out core.Object
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err, string(raw))
	}
	return out
}
func (f *sceneFixture) wait(t *testing.T, id int64) core.Job {
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
func objectID(t *testing.T, body core.Object, key string) int64 {
	t.Helper()
	item, ok := body[key].(map[string]any)
	if !ok {
		t.Fatal(body)
	}
	return int64(item["id"].(float64))
}
func pixel(img image.Image, x, y int) color.NRGBA {
	r, g, b, a := img.At(x, y).RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}
func boxOf(t *testing.T, element map[string]any) [4]float64 {
	t.Helper()
	raw, ok := element["box"].([]any)
	if !ok || len(raw) != 4 {
		t.Fatal(element)
	}
	var box [4]float64
	for index := 0; index < 4; index++ {
		box[index] = raw[index].(float64)
	}
	return box
}
func TestSceneLayoutValidation(t *testing.T) {
	f := newSceneFixture(t, true)
	items := f.request(t, "GET", "/api/scenes", nil, 200)["items"].([]any)
	seed := items[0].(map[string]any)
	seedID := int64(seed["id"].(float64))
	productOnly := core.Object{"size": "1024x1536", "elements": []core.Object{{"id": "p", "type": "product", "label": "商品", "box": []float64{0.2, 0.2, 0.3, 0.3}, "note": ""}}}
	updated := f.request(t, "PATCH", fmt.Sprintf("/api/scenes/%d", seedID), core.Object{"camera": "angle45", "brief": "@商品 居中", "layout": productOnly}, 200)["scene"].(map[string]any)
	if updated["camera"] != "angle45" || updated["brief"] != "@商品 居中" || updated["builtin_key"] == nil {
		t.Fatal(updated)
	}
	if updated["layout"].(map[string]any)["size"] != "1024x1536" {
		t.Fatal(updated["layout"])
	}
	id := f.create(t, "校验场景", nil, []string{})
	path := fmt.Sprintf("/api/scenes/%d", id)
	expect := func(layout any, status int, text string) {
		t.Helper()
		out := f.request(t, "PATCH", path, core.Object{"layout": layout}, status)
		if status >= 400 && !strings.Contains(fmt.Sprint(out["error"]), text) {
			t.Fatal(out)
		}
	}
	base := func(elements ...core.Object) core.Object {
		return core.Object{"size": "1024x1024", "elements": elements}
	}
	expect(core.Object{"size": "800x800", "elements": []any{}}, 400, "尺寸")
	expect(base(core.Object{"id": "a", "type": "product", "label": "商品", "box": []float64{0.8, 0.8, 0.3, 0.3}, "note": ""}), 400, "画布")
	expect(base(core.Object{"id": "a", "type": "product", "label": "商品", "box": []float64{0.1, 0.1, 0, 0.2}, "note": ""}), 400, "大于 0")
	expect(base(
		core.Object{"id": "a", "type": "product", "label": "甲", "box": []float64{0.1, 0.1, 0.2, 0.2}, "note": ""},
		core.Object{"id": "b", "type": "product", "label": "乙", "box": []float64{0.4, 0.4, 0.2, 0.2}, "note": ""},
	), 400, "一个商品")
	f.request(t, "PATCH", path, core.Object{"camera": "side"}, 400)
	f.exec(t, "INSERT INTO shops(id,name,platform,language,created_at) VALUES(99,'不可见店铺','taobao','zh-CN',?)", core.Now())
	foreign := objectID(t, f.request(t, "POST", "/api/props", core.Object{"name": "别店桌", "kind": "table", "description": "别的店的桌子", "size_cm": core.Object{"w": 10}, "shop_id": 99}, 201), "prop")
	var password []byte
	if err := f.c.DB.QueryRow("SELECT password_hash FROM users WHERE id=1").Scan(&password); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "INSERT INTO roles(id,key,name,scope,perms,builtin,created_at) VALUES(98,'layout_editor','布局编辑','shops',?,0,?)", core.JS([]string{core.ProductView, core.CreativeTemplate}), core.Now())
	f.exec(t, "INSERT INTO users(id,username,password_hash,display_name,role_id,created_at) VALUES(98,'layout',?,'编辑',98,?)", password, core.Now())
	f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(98,1)")
	jar, _ := cookiejar.New(nil)
	f.client = &http.Client{Jar: jar}
	f.request(t, "POST", "/api/login", core.Object{"username": "layout", "password": "password123"}, 200)
	expect(base(core.Object{"id": "a", "type": "prop", "prop_id": foreign, "label": "别店桌", "box": []float64{0.1, 0.5, 0.4, 0.3}, "note": ""}), 404, "道具不存在")
}
func TestScenePlateGenerateLockOutdatedAndResolve(t *testing.T) {
	f := newSceneFixture(t, true)
	provider, ai := startPlateAI(t)
	promptDir, err := filepath.Abs(filepath.Join("..", "..", "..", "prompts"))
	if err != nil {
		t.Fatal(err)
	}
	f.c.Cfg.AIBaseURL = provider.URL
	f.c.Cfg.AIKey = "fake"
	f.c.Cfg.ImageModel = "fake-image"
	f.c.Cfg.ImageTimeout = 5 * time.Second
	f.c.Cfg.PromptDir = promptDir
	f.c.SetBaseURL(f.server.URL)
	items := f.request(t, "GET", "/api/scenes", nil, 200)["items"].([]any)
	seed := items[0].(map[string]any)
	seedID := int64(seed["id"].(float64))
	for _, raw := range seed["refs"].([]any) {
		ref := raw.(map[string]any)
		f.request(t, "DELETE", fmt.Sprintf("/api/scenes/%d/refs/%d", seedID, int64(ref["id"].(float64))), nil, 200)
	}
	propA := objectID(t, f.request(t, "POST", "/api/props", core.Object{"name": "胡桃木桌", "kind": "table", "description": "深棕色胡桃木，边缘有细金线", "size_cm": core.Object{"w": 60, "d": 40, "h": 2}}, 201), "prop")
	propB := objectID(t, f.request(t, "POST", "/api/props", core.Object{"name": "礼盒", "kind": "container", "description": "磨砂白礼盒，盖上有细麻绳", "size_cm": core.Object{"h": 8}}, 201), "prop")
	f.uploadProp(t, propA, solidPNG(t, 1024, 1024, color.NRGBA{R: 255, G: 0, B: 0, A: 255}), true)
	f.uploadScene(t, seedID, solidPNG(t, 1024, 1024, color.NRGBA{R: 0, G: 0, B: 255, A: 255}), 201)
	layout := core.Object{"size": "1024x1024", "elements": []core.Object{
		{"id": "e1", "type": "prop", "prop_id": propA, "label": "胡桃木桌", "box": []float64{0.05, 0.55, 0.9, 0.4}, "note": "占满下半部"},
		{"id": "e2", "type": "prop", "prop_id": propB, "label": "礼盒", "box": []float64{0.08, 0.58, 0.12, 0.1}, "note": ""},
		{"id": "e3", "type": "product", "label": "商品", "box": []float64{0.55, 0.62, 0.18, 0.12}, "note": ""},
	}}
	scenePath := fmt.Sprintf("/api/scenes/%d", seedID)
	f.request(t, "PATCH", scenePath, core.Object{"camera": "angle45", "brief": "@胡桃木桌 居中", "still": "米白亚麻桌面", "light": "侧窗柔光", "wear": "穿搭哨兵不要进提示词", "layout": layout}, 200)
	before, err := Resolve(context.Background(), f.c.DB, f.listingID, seedID)
	if err != nil || before.Plate != nil || strings.Contains(core.JS(before), `"plate"`) {
		t.Fatal(before, err)
	}
	created := f.request(t, "POST", scenePath+"/plates", core.Object{"extra": "不要出现鲜花"}, 202)
	job := f.wait(t, int64(created["job_id"].(float64)))
	if job.Status != "done" {
		t.Fatal(job.Status, job.Message)
	}
	calls := ai.list()
	if len(calls) != 1 || calls[0].Err != "" || calls[0].Path != "/images/edits" || calls[0].Size != "1024x1024" || len(calls[0].Corners) != 3 {
		t.Fatal(calls)
	}
	if calls[0].Corners[0] != [3]uint8{232, 232, 232} || calls[0].Corners[1] != [3]uint8{250, 250, 250} || calls[0].Corners[2] != [3]uint8{0, 0, 255} {
		t.Fatal(calls[0].Corners)
	}
	prompt := calls[0].Prompt
	for _, part := range []string{"洋红", "不要放任何东西", "深棕色胡桃木，边缘有细金线", "磨砂白礼盒，盖上有细麻绳", "俯拍", "米白亚麻桌面", "侧窗柔光", "不要出现鲜花", "真实尺寸约宽 60 cm", "第1张是布局示意图", "第2张是道具参考合集", "第3张是场景静物参考"} {
		if !strings.Contains(prompt, part) {
			t.Fatalf("提示词缺少 %s\n%s", part, prompt)
		}
	}
	if strings.Contains(prompt, "不给模型看") || strings.Contains(prompt, "穿搭哨兵") {
		t.Fatal(prompt)
	}
	scene := f.request(t, "GET", scenePath, nil, 200)["scene"].(map[string]any)
	if scene["plate"] != nil || scene["plate_outdated"] != false {
		t.Fatal(scene["plate"], scene["plate_outdated"])
	}
	plates := scene["plates"].([]any)
	if len(plates) != 1 {
		t.Fatal(plates)
	}
	first := plates[0].(map[string]any)
	firstID := int64(first["id"].(float64))
	if first["size"] != "1024x1024" || first["status"] != "candidate" {
		t.Fatal(first)
	}
	response, err := f.client.Get(f.server.URL + first["sketch_url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	sketch, _, decErr := image.Decode(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || decErr != nil {
		t.Fatal(response.StatusCode, decErr)
	}
	elements := scene["layout"].(map[string]any)["elements"].([]any)
	var tableBox, productBox [4]float64
	var sawCover bool
	for _, raw := range elements {
		element := raw.(map[string]any)
		switch element["id"] {
		case "e1":
			tableBox = boxOf(t, element)
			if element["prop_name"] != "胡桃木桌" || element["cover_url"] == "" {
				t.Fatal(element)
			}
			sawCover = true
		case "e2":
			if element["prop_name"] != "礼盒" || element["cover_url"] != nil && element["cover_url"] != "" {
				t.Fatal(element)
			}
		case "e3":
			productBox = boxOf(t, element)
		}
	}
	if !sawCover {
		t.Fatal(elements)
	}
	tableAt := boxRect(tableBox, 1024, 1024)
	productAt := boxRect(productBox, 1024, 1024)
	if got := pixel(sketch, tableAt.Min.X, tableAt.Min.Y); got != propColors[0].c {
		t.Fatal(got)
	}
	if got := pixel(sketch, productAt.Min.X, productAt.Min.Y); got != productColor {
		t.Fatal(got)
	}
	if got := pixel(sketch, 0, 0); got != sketchBackground {
		t.Fatal(got)
	}
	locked := f.request(t, "POST", fmt.Sprintf("%s/plates/%d/lock", scenePath, firstID), nil, 200)["scene"].(map[string]any)
	if locked["plate_outdated"] != false {
		t.Fatal(locked["plate_outdated"])
	}
	plate := locked["plate"].(map[string]any)
	if plate["status"] != "locked" || locked["thumb_url"] != plate["url"] {
		t.Fatal(locked["thumb_url"], plate)
	}
	var oldBox [4]float64
	for _, raw := range plate["layout_snapshot"].(map[string]any)["elements"].([]any) {
		element := raw.(map[string]any)
		if element["type"] == "product" {
			oldBox = boxOf(t, element)
		}
	}
	listed := f.request(t, "GET", "/api/scenes", nil, 200)["items"].([]any)
	if listed[0].(map[string]any)["thumb_url"] != plate["url"] {
		t.Fatal(listed[0])
	}
	moved := core.Object{"size": "1024x1024", "elements": []core.Object{
		{"id": "e1", "type": "prop", "prop_id": propA, "label": "胡桃木桌", "box": []float64{0.05, 0.55, 0.9, 0.4}, "note": "占满下半部"},
		{"id": "e2", "type": "prop", "prop_id": propB, "label": "礼盒", "box": []float64{0.08, 0.58, 0.12, 0.1}, "note": ""},
		{"id": "e3", "type": "product", "label": "商品", "box": []float64{0.1, 0.7, 0.2, 0.15}, "note": ""},
	}}
	outdated := f.request(t, "PATCH", scenePath, core.Object{"layout": moved}, 200)["scene"].(map[string]any)
	if outdated["plate_outdated"] != true || outdated["plate"].(map[string]any)["status"] != "locked" {
		t.Fatal(outdated["plate_outdated"], outdated["plate"])
	}
	snapshot, err := Resolve(context.Background(), f.c.DB, f.listingID, seedID)
	if err != nil || snapshot.Plate == nil || snapshot.Plate.Size != "1024x1024" || snapshot.Plate.Width != 1024 || snapshot.Plate.Height != 1024 || snapshot.Plate.Box == nil {
		t.Fatal(snapshot, err)
	}
	if _, err = os.Stat(snapshot.Plate.Path); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err = f.c.DB.QueryRow("SELECT file FROM scene_plates WHERE id=?", firstID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(stored)
	if err != nil || abs != snapshot.Plate.Path {
		t.Fatal(abs, snapshot.Plate.Path, err)
	}
	for index := 0; index < 4; index++ {
		if snapshot.Plate.Box[index] != oldBox[index] {
			t.Fatal(snapshot.Plate.Box, oldBox)
		}
	}
	if snapshot.Plate.Box[0] == 0.1 {
		t.Fatal(snapshot.Plate.Box)
	}
	var audits int
	if err = f.c.DB.QueryRow("SELECT count(*) FROM audit_log WHERE action='scene.plate_add' AND target_id=?", seedID).Scan(&audits); err != nil || audits != 1 {
		t.Fatal(audits, err)
	}
	secondJob := f.wait(t, int64(f.request(t, "POST", scenePath+"/plates", core.Object{"extra": ""}, 202)["job_id"].(float64)))
	if secondJob.Status != "done" {
		t.Fatal(secondJob.Status, secondJob.Message)
	}
	fresh := f.request(t, "GET", scenePath, nil, 200)["scene"].(map[string]any)
	var secondID int64
	for _, raw := range fresh["plates"].([]any) {
		item := raw.(map[string]any)
		if int64(item["id"].(float64)) != firstID {
			secondID = int64(item["id"].(float64))
		}
	}
	if secondID == 0 {
		t.Fatal(fresh["plates"])
	}
	relocked := f.request(t, "POST", fmt.Sprintf("%s/plates/%d/lock", scenePath, secondID), nil, 200)["scene"].(map[string]any)
	if relocked["plate_outdated"] != false || int64(relocked["plate"].(map[string]any)["id"].(float64)) != secondID {
		t.Fatal(relocked["plate_outdated"], relocked["plate"])
	}
	var firstStatus string
	if err = f.c.DB.QueryRow("SELECT status FROM scene_plates WHERE id=?", firstID).Scan(&firstStatus); err != nil || firstStatus != "candidate" {
		t.Fatal(firstStatus, err)
	}
}
