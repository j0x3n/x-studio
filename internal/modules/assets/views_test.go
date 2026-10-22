package assets_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"testing"
)

func configureViews(t *testing.T, f *fixture) {
	t.Helper()
	f.req(t, "PATCH", "/api/categories/1", obj{"views": []obj{
		{"key": "front", "name": "正面", "code": "A1", "groups": []string{"small"}, "required": []string{"small"}, "hint": "完整"},
		{"key": "flat", "name": "平铺", "code": "A1", "groups": []string{"strand"}, "required": []string{"strand"}, "hint": "全部珠子"},
		{"key": "angle", "name": "侧面", "code": "A2", "groups": []string{"small", "strand"}, "required": []string{"small"}, "hint": ""},
		{"key": "close", "name": "近景", "code": "B2", "groups": []string{"strand"}, "required": []string{"strand"}, "hint": ""},
	}}, 200)
}

func uploadView(t *testing.T, f *fixture, pid int64, code string, status int) int64 {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	file, _ := writer.CreateFormFile("files", "上游.png")
	file.Write(imageBytes(t, 48, 32))
	writer.WriteField("code", code)
	writer.Close()
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/products/%d/assets", f.s.URL, pid), &buf)
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out obj
	json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != status {
		t.Fatalf("upload: %d %v", res.StatusCode, out)
	}
	if status != 201 {
		return 0
	}
	return id(out["items"].([]any)[0])
}

func viewEntry(out obj, key string) obj {
	for _, entry := range out["views"].([]any) {
		if entry.(obj)["key"] == key {
			return entry.(obj)
		}
	}
	return nil
}

func TestViewsUploadVersionsRestoreAndDiscard(t *testing.T) {
	f := newFixture(t, nil)
	configureViews(t, f)
	pid := f.product(t, "底图")
	path := fmt.Sprintf("/api/products/%d/views", pid)
	first := uploadView(t, f, pid, "A1", 201)
	second := uploadView(t, f, pid, "A1", 201)
	out := f.req(t, "GET", path, nil, 200)
	front := viewEntry(out, "front")
	if id(front["current"]) != second || len(front["versions"].([]any)) != 2 || out["ready"] != false {
		t.Fatal(out)
	}
	var code, view string
	if err := f.c.DB.QueryRow("SELECT code,view_code FROM assets WHERE id=?", first).Scan(&code, &view); err != nil || code != "" || view != "A1" {
		t.Fatal(code, view, err)
	}
	f.req(t, "POST", fmt.Sprintf("/api/assets/%d/make-current", first), obj{}, 200)
	out = f.req(t, "GET", path, nil, 200)
	if id(viewEntry(out, "front")["current"]) != first {
		t.Fatal(out)
	}
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", second), obj{"code": "A2"}, 200)
	out = f.req(t, "GET", path, nil, 200)
	if out["ready"] != true || id(viewEntry(out, "angle")["current"]) != second || len(viewEntry(out, "front")["versions"].([]any)) != 1 {
		t.Fatal(out)
	}
	other := f.upload(t, pid, imageBytes(t, 20, 20))
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", other), obj{"code": "A3_8mm"}, 200)
	f.upload(t, pid, imageBytes(t, 20, 20))
	out = f.req(t, "GET", path, nil, 200)
	if len(out["others"].([]any)) != 1 || len(out["unassigned"].([]any)) != 1 {
		t.Fatal(out)
	}
	uploadView(t, f, pid, "E1", 400)
	f.req(t, "POST", fmt.Sprintf("/api/assets/%d/discard", second), obj{}, 200)
	out = f.req(t, "GET", path, nil, 200)
	if len(viewEntry(out, "angle")["versions"].([]any)) != 0 {
		t.Fatal(out)
	}
	f.req(t, "PATCH", fmt.Sprintf("/api/products/%d", pid), obj{"info": obj{"style": "手串"}}, 200)
	out = f.req(t, "GET", path, nil, 200)
	if out["group"] != "strand" || viewEntry(out, "flat") == nil || fmt.Sprint(out["missing"]) != "[close]" {
		t.Fatal(out)
	}
	f.assertAudit(t, "assets.make_current", "assets.discard")
}

func TestViewsAssignProcessedVersionKeepsUnassignedOriginal(t *testing.T) {
	f := newFixture(t, nil)
	configureViews(t, f)
	pid := f.product(t, "先处理后分槽")
	original := f.upload(t, pid, imageBytes(t, 40, 30))
	processed := newID(t, processJob(t, f, []int64{original}, obj{"format": "png"}, false))
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", processed), obj{"code": "A2"}, 200)
	out := f.req(t, "GET", fmt.Sprintf("/api/products/%d/views", pid), nil, 200)
	if len(viewEntry(out, "angle")["versions"].([]any)) != 2 || len(out["unassigned"].([]any)) != 0 {
		t.Fatal(out)
	}
	f.req(t, "POST", fmt.Sprintf("/api/assets/%d/make-current", original), obj{}, 200)
	out = f.req(t, "GET", fmt.Sprintf("/api/products/%d/views", pid), nil, 200)
	if id(viewEntry(out, "angle")["current"]) != original {
		t.Fatal(out)
	}
}
