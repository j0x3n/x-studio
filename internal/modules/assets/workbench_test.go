package assets_test

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/jpeg"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"xstudio/internal/core"
)

func wbExec(t *testing.T, f *fixture, q string, args ...any) {
	t.Helper()
	if _, err := f.c.DB.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func wbUpload(t *testing.T, f *fixture, files [][]byte, status int) obj {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for i, b := range files {
		part, err := mw.CreateFormFile("files", fmt.Sprintf("原图%d.png", i))
		if err != nil {
			t.Fatal(err)
		}
		part.Write(b)
	}
	mw.Close()
	req, _ := http.NewRequest("POST", f.s.URL+"/api/workbench/inputs", &buf)
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != status {
		t.Fatalf("上传 %d want %d: %s", res.StatusCode, status, b)
	}
	var out obj
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func wbInput(t *testing.T, f *fixture) int64 {
	t.Helper()
	return id(wbUpload(t, f, [][]byte{imageBytes(t, 40, 20)}, 201)["items"].([]any)[0])
}
func wbJob(t *testing.T, f *fixture, inputs []obj, ops obj) core.Job {
	t.Helper()
	out := f.req(t, "POST", "/api/workbench/process", obj{"inputs": inputs, "ops": ops}, 202)
	return f.wait(t, id(out["job"]))
}
func wbResult(t *testing.T, j core.Job) int64 {
	t.Helper()
	if j.Status != "done" {
		t.Fatal(j)
	}
	row := j.Result["items"].([]any)[0].(obj)
	if row["error"] != nil {
		t.Fatal(row)
	}
	return int64(row["result_id"].(float64))
}
func wbBinary(t *testing.T, f *fixture, method, path string, body any, status int) ([]byte, http.Header) {
	t.Helper()
	var input io.Reader
	if body != nil {
		input = strings.NewReader(core.JS(body))
	}
	req, _ := http.NewRequest(method, f.s.URL+path, input)
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", "application/json")
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s %d want %d: %s", method, path, res.StatusCode, status, b)
	}
	return b, res.Header
}
func TestWorkbenchIsolationInputsAndJobs(t *testing.T) {
	f := newFixture(t, nil)
	pid := f.product(t, "不可见商品")
	aid := f.upload(t, pid, imageBytes(t, 20, 20))
	f.user(t, "worker", 4, 1)
	f.login(t, "worker")
	input := wbInput(t, f)
	result := wbResult(t, wbJob(t, f, []obj{{"input_id": input}}, obj{}))
	ownJob := wbJob(t, f, []obj{{"input_id": input}}, obj{})
	f.req(t, "GET", fmt.Sprintf("/api/jobs/%d", ownJob.ID), nil, 200)
	f.req(t, "POST", "/api/workbench/process", obj{"inputs": []obj{{"asset_id": aid}}, "ops": obj{}}, 404)
	f.req(t, "POST", "/api/workbench/process", obj{"inputs": []obj{{"input_id": result}}, "ops": obj{}}, 404)
	f.login(t, "admin")
	for _, item := range []int64{input, result} {
		path := fmt.Sprintf("/api/workbench/items/%d", item)
		f.req(t, "GET", path, nil, 404)
		f.req(t, "DELETE", path, nil, 404)
		f.req(t, "GET", path+"/file", nil, 404)
		f.req(t, "GET", path+"/file?download=1", nil, 404)
		f.req(t, "POST", path+"/save", obj{"product_id": pid}, 404)
		f.req(t, "POST", "/api/workbench/download", obj{"ids": []int64{item}}, 404)
		f.req(t, "POST", "/api/workbench/process", obj{"inputs": []obj{{"input_id": item}}, "ops": obj{}}, 404)
	}
	f.req(t, "GET", fmt.Sprintf("/api/jobs/%d", ownJob.ID), nil, 404)
	if len(f.req(t, "GET", "/api/workbench/items", nil, 200)["items"].([]any)) != 0 {
		t.Fatal("管理员看见他人条目")
	}
	f.assertAudit(t, "assets.workbench.upload", "assets.workbench.process")
}
func TestWorkbenchUploadValidationExifAndLimits(t *testing.T) {
	f := newFixture(t, nil)
	good := imageBytes(t, 40, 20)
	wbUpload(t, f, [][]byte{[]byte("fake png")}, 400)
	wbUpload(t, f, [][]byte{good[:len(good)/2]}, 400)
	wbUpload(t, f, [][]byte{make([]byte, (25<<20)+1)}, 413)
	// A valid PNG IHDR with impossible dimensions is rejected before allocation.
	huge := append([]byte(nil), good...)
	binary.BigEndian.PutUint32(huge[16:20], 40001)
	binary.BigEndian.PutUint32(huge[20:24], 40001)
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	wbUpload(t, f, [][]byte{huge}, 400)
	many := make([][]byte, 21)
	for i := range many {
		many[i] = good
	}
	wbUpload(t, f, many, 413)
	if len(f.req(t, "GET", "/api/workbench/items", nil, 200)["items"].([]any)) != 0 {
		t.Fatal("非法批次留下记录")
	}
	// A real 1x1 WebP is accepted independent of the upload filename extension.
	webp, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	webpItem := wbUpload(t, f, [][]byte{webp}, 201)["items"].([]any)[0].(obj)
	if webpItem["width"] != float64(1) || webpItem["height"] != float64(1) {
		t.Fatal(webpItem)
	}
	// JPEG EXIF orientation 6 rotates 40x20 into 20x40.
	var jpg bytes.Buffer
	jpeg.Encode(&jpg, image.NewNRGBA(image.Rect(0, 0, 40, 20)), nil)
	exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	raw := append([]byte{}, jpg.Bytes()[:2]...)
	raw = append(raw, 0xff, 0xe1, 0, byte(len(exif)+2))
	raw = append(raw, exif...)
	raw = append(raw, jpg.Bytes()[2:]...)
	input := wbUpload(t, f, [][]byte{raw}, 201)["items"].([]any)[0].(obj)
	if input["width"] != float64(20) || input["height"] != float64(40) {
		t.Fatal(input)
	}
	iid := id(input)
	rid := wbResult(t, wbJob(t, f, []obj{{"input_id": iid}}, obj{"format": "png"}))
	result := f.req(t, "GET", fmt.Sprintf("/api/workbench/items/%d", rid), nil, 200)["item"].(obj)
	if result["width"] != float64(20) || result["height"] != float64(40) {
		t.Fatal(result)
	}
	thumb, h := wbBinary(t, f, "GET", fmt.Sprintf("/api/workbench/items/%d/file?w=240", iid), nil, 200)
	c, format, err := image.DecodeConfig(bytes.NewReader(thumb))
	if err != nil || format != "jpeg" || c.Width != 20 || c.Height != 40 || h.Get("Content-Type") != "image/jpeg" {
		t.Fatal(c, format, err, h)
	}
	f.req(t, "GET", fmt.Sprintf("/api/workbench/items/%d/file?w=250", iid), nil, 400)
	if len(f.req(t, "GET", "/api/workbench/items?kind=result", nil, 200)["items"].([]any)) != 1 {
		t.Fatal("kind 过滤失败")
	}
	f.req(t, "GET", "/api/workbench/items?kind=other", nil, 400)
	for _, body := range []obj{{"inputs": []obj{}}, {"inputs": []obj{{"input_id": iid, "asset_id": 1}}}, {"inputs": []obj{{"input_id": iid}, {"input_id": iid}}}, {"inputs": []obj{{"input_id": iid}}, "set_current": true}, {"inputs": []obj{{"input_id": iid}}, "ops": obj{"unknown": 1}}} {
		f.req(t, "POST", "/api/workbench/process", body, 400)
	}
}
func TestWorkbenchProcessingSourcesPartialFailureAndQuota(t *testing.T) {
	output := imageBytes(t, 40, 20)
	var calls atomic.Int32
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(obj{"data": []obj{{"b64_json": base64.StdEncoding.EncodeToString(output)}}})
	}))
	defer ai.Close()
	f := newFixture(t, func(c *core.Config) { c.AIKey = "fake"; c.AIBaseURL = ai.URL })
	prompts(t, f)
	input := wbInput(t, f)
	pid := f.product(t, "生成来源", 1)
	aid := f.upload(t, pid, output)
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), obj{"source_override": "ai_generated"}, 200)
	var before int
	f.c.DB.QueryRow("SELECT count(*) FROM assets").Scan(&before)
	for _, tc := range []struct {
		inputs []obj
		ops    obj
		source string
	}{{[]obj{{"input_id": input}}, obj{"ai": obj{"quick": []string{"dust"}}}, "ai_enhanced"}, {[]obj{{"input_id": input}}, obj{"ai": obj{"quick": []string{"bg_white"}}}, "ai_edited"}, {[]obj{{"input_id": input}}, obj{"ai": obj{"extra": "补背景"}}, "ai_edited"}, {[]obj{{"asset_id": aid}}, obj{}, "ai_generated"}} {
		rid := wbResult(t, wbJob(t, f, tc.inputs, tc.ops))
		v := f.req(t, "GET", fmt.Sprintf("/api/workbench/items/%d", rid), nil, 200)["item"].(obj)
		if v["source"] != tc.source {
			t.Fatal(v)
		}
		op := v["op"].(obj)
		if op["elapsed_ms"] == nil || op["warnings"] == nil || op["models"] == nil {
			t.Fatal(op)
		}
		if tc.ops["ai"] != nil && op["diff"] == nil {
			t.Fatal("缺少 AI 对比", op)
		}
	}
	broken := wbInput(t, f)
	var file string
	f.c.DB.QueryRow("SELECT file FROM workbench_items WHERE id=?", broken).Scan(&file)
	os.Remove(filepath.Join(f.c.Cfg.DataDir, "workbench", "1", filepath.FromSlash(file)))
	j := wbJob(t, f, []obj{{"input_id": broken}, {"input_id": input}}, obj{})
	rows := j.Result["items"].([]any)
	if j.Status != "done" || rows[0].(obj)["error"] == nil || rows[1].(obj)["result_id"] == nil {
		t.Fatal(j)
	}
	var after int
	f.c.DB.QueryRow("SELECT count(*) FROM assets").Scan(&after)
	if before != after {
		t.Fatal("处理写了素材表")
	}
	f.req(t, "PUT", "/api/users/1/quota", obj{"month_images": calls.Load()}, 200)
	beforeCalls := calls.Load()
	var countBefore int
	f.c.DB.QueryRow("SELECT count(*) FROM workbench_items WHERE kind='result'").Scan(&countBefore)
	j = wbJob(t, f, []obj{{"input_id": input}}, obj{"ai": obj{"quick": []string{"dust"}}})
	f.c.DB.QueryRow("SELECT count(*) FROM workbench_items WHERE kind='result'").Scan(&after)
	if j.Status != "failed" || !strings.Contains(j.Message, "额度") || calls.Load() != beforeCalls || after != countBefore {
		t.Fatal("额度预检失败", j)
	}
}
func TestWorkbenchSaveVersionsRootsIdempotencyAndBusy(t *testing.T) {
	f := newFixture(t, nil)
	p1 := f.product(t, "原款", 1)
	p2 := f.product(t, "另一款", 1)
	aid := f.upload(t, p1, imageBytes(t, 30, 20))
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), obj{"code": "B2", "title": "继承标题", "note": "继承备注", "tags": []string{"白色"}}, 200)
	rid := wbResult(t, wbJob(t, f, []obj{{"asset_id": aid}}, obj{"format": "png"}))
	save := func(rid int64, body obj) obj {
		return f.req(t, "POST", fmt.Sprintf("/api/workbench/items/%d/save", rid), body, 200)["asset"].(obj)
	}
	v := save(rid, obj{"set_current": true})
	nid := id(v)
	if v["parent_id"] != float64(aid) || v["product_id"] != float64(p1) || v["code"] != "B2" || v["title"] != "继承标题" || v["note"] != "继承备注" || len(v["tags"].([]any)) != 1 || v["kind"] != "photo" || v["op"] == nil || v["version_count"] != float64(2) {
		t.Fatal(v)
	}
	original := f.req(t, "GET", fmt.Sprintf("/api/assets/%d", aid), nil, 200)["asset"].(obj)
	if original["code"] != "" {
		t.Fatal("编号未移走")
	}
	if id(save(rid, obj{"product_id": p2})) != nid {
		t.Fatal("重复保存建了新图")
	}
	rid2 := wbResult(t, wbJob(t, f, []obj{{"asset_id": aid}}, obj{}))
	v = save(rid2, obj{"product_id": p1})
	if v["parent_id"] != float64(aid) {
		t.Fatal(v)
	}
	rid3 := wbResult(t, wbJob(t, f, []obj{{"asset_id": aid}}, obj{}))
	v = save(rid3, obj{"product_id": p2, "set_current": true})
	if v["parent_id"] != nil || v["product_id"] != float64(p2) || v["code"] != "" || v["title"] != "继承标题" {
		t.Fatal(v)
	}
	input := wbInput(t, f)
	f.req(t, "POST", fmt.Sprintf("/api/workbench/items/%d/save", input), obj{"product_id": p1}, 400)
	uploaded := wbResult(t, wbJob(t, f, []obj{{"input_id": input}}, obj{}))
	if f.req(t, "POST", fmt.Sprintf("/api/workbench/items/%d/save", uploaded), obj{}, 400)["error"] != "请选择保存到哪一款" {
		t.Fatal("缺少款号错误")
	}
	v = save(uploaded, obj{"product_id": p2, "set_current": true})
	if v["parent_id"] != nil || v["title"] != "" || v["note"] != "" || v["code"] != "" {
		t.Fatal(v)
	}
	busy := wbResult(t, wbJob(t, f, []obj{{"asset_id": aid}}, obj{}))
	wbExec(t, f, "INSERT INTO jobs(kind,target_type,target_id,user_id,status,steps,created_at) SELECT 'pipeline','listing',id,1,'running','enhance',? FROM listings WHERE product_id=?", core.Now(), p1)
	f.req(t, "POST", fmt.Sprintf("/api/workbench/items/%d/save", busy), obj{}, 409)
	wbExec(t, f, "UPDATE jobs SET status='done' WHERE kind='pipeline'")
	var savedFile string
	f.c.DB.QueryRow("SELECT file FROM assets WHERE id=?", nid).Scan(&savedFile)
	f.req(t, "DELETE", fmt.Sprintf("/api/workbench/items/%d", rid), nil, 200)
	if _, err := os.Stat(filepath.Join(f.c.Dir(p1), filepath.FromSlash(savedFile))); err != nil {
		t.Fatal("删除处理台误删素材", err)
	}
	f.req(t, "GET", fmt.Sprintf("/api/assets/%d", nid), nil, 200)
	f.assertAudit(t, "assets.workbench.save", "assets.set_current", "assets.workbench.delete")
}
func TestWorkbenchDownloadLabelsAndAssetDownload(t *testing.T) {
	f := newFixture(t, nil)
	pid := f.product(t, "标识", 1)
	aid := f.upload(t, pid, imageBytes(t, 30, 20))
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), obj{"source_override": "ai_edited"}, 200)
	edited := wbResult(t, wbJob(t, f, []obj{{"asset_id": aid}}, obj{"format": "png"}))
	input := wbInput(t, f)
	original := wbResult(t, wbJob(t, f, []obj{{"input_id": input}}, obj{}))
	for _, id := range []int64{edited, original, input} {
		b, h := wbBinary(t, f, "GET", fmt.Sprintf("/api/workbench/items/%d/file?download=1", id), nil, 200)
		if bytes.Contains(b, []byte("AI 生成或合成")) || bytes.Contains(b, []byte("X Studio")) {
			t.Fatal("下载仍写入了 AI 标识")
		}
		_, params, err := mime.ParseMediaType(h.Get("Content-Disposition"))
		if err != nil || !strings.Contains(params["filename"], "_") {
			t.Fatal(h)
		}
		if _, _, err = image.DecodeConfig(bytes.NewReader(b)); err != nil {
			t.Fatal("下载破坏图片", err)
		}
	}
	for _, endpoint := range []string{"/api/workbench/download", "/api/assets/download"} {
		body := obj{"ids": []int64{edited}}
		if endpoint == "/api/assets/download" {
			body = obj{"asset_ids": []int64{aid}}
		}
		b, _ := wbBinary(t, f, "POST", endpoint, body, 200)
		z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil || len(z.File) != 1 {
			t.Fatal(err)
		}
		r, err := z.File[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		imageData, _ := io.ReadAll(r)
		r.Close()
		if bytes.Contains(imageData, []byte("AI 生成或合成")) {
			t.Fatal("打包下载仍写入了 AI 标识")
		}
	}
	f.user(t, "operator", 2, 1)
	f.login(t, "operator")
	owned := wbInput(t, f)
	wbBinary(t, f, "GET", fmt.Sprintf("/api/workbench/items/%d/file?download=1", owned), nil, 200)
	wbBinary(t, f, "POST", "/api/workbench/download", obj{"ids": []int64{owned}}, 200)
	f.assertAudit(t, "assets.workbench.download", "assets.download")
}
func TestWorkbenchOneActiveTaskPerUser(t *testing.T) {
	start := make(chan struct{}, 1)
	release := make(chan struct{})
	var released atomic.Bool
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case start <- struct{}{}:
		default:
		}
		<-release
		json.NewEncoder(w).Encode(obj{"data": []obj{{"b64_json": base64.StdEncoding.EncodeToString(imageBytes(t, 20, 20))}}})
	}))
	defer ai.Close()
	f := newFixture(t, func(c *core.Config) { c.AIKey = "fake"; c.AIBaseURL = ai.URL })
	prompts(t, f)
	defer func() {
		if !released.Load() {
			close(release)
		}
	}()
	input := wbInput(t, f)
	body := obj{"inputs": []obj{{"input_id": input}}, "ops": obj{"ai": obj{"quick": []string{"dust"}}}}
	out := f.req(t, "POST", "/api/workbench/process", body, 202)
	select {
	case <-start:
	case <-time.After(10 * time.Second):
		t.Fatal("AI 未开始")
	}
	f.req(t, "POST", "/api/workbench/process", body, 409)
	f.req(t, "GET", fmt.Sprintf("/api/jobs/%d", id(out["job"])), nil, 200)
	released.Store(true)
	close(release)
	wbResult(t, f.wait(t, id(out["job"])))
	wbResult(t, wbJob(t, f, []obj{{"input_id": input}}, obj{}))
}

func TestWorkbenchPreflightPermissionsAndRecentList(t *testing.T) {
	f := newFixture(t, nil)
	input := wbInput(t, f)
	j := wbJob(t, f, []obj{{"input_id": input}}, obj{"ai": obj{"quick": []string{"dust"}}})
	if j.Status != "failed" || !strings.Contains(j.Message, "缺少提示词") {
		t.Fatal(j)
	}
	prompts(t, f)
	j = wbJob(t, f, []obj{{"input_id": input}}, obj{"ai": obj{"quick": []string{"dust"}}})
	if j.Status != "failed" || !strings.Contains(j.Message, "未配置 AI_API_KEY") {
		t.Fatal(j)
	}
	// Query ordering, retention, and the hard 200-item limit are independent of
	// paging parameters used elsewhere in the asset library.
	var file string
	f.c.DB.QueryRow("SELECT file FROM workbench_items WHERE id=?", input).Scan(&file)
	at := core.Now()
	wbExec(t, f, "WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<201) INSERT INTO workbench_items(user_id,kind,file,orig_name,width,height,created_at) SELECT 1,'input',?,'bulk.png',1,1,? FROM n", file, at)
	wbExec(t, f, "INSERT INTO workbench_items(user_id,kind,file,orig_name,width,height,created_at) VALUES(1,'result',?,'old.png',1,1,?)", file, time.Now().Add(-8*24*time.Hour).UTC().Format(time.RFC3339Nano))
	items := f.req(t, "GET", "/api/workbench/items?kind=input", nil, 200)["items"].([]any)
	if len(items) != 200 {
		t.Fatal("列表未限制 200", len(items))
	}
	for i := 1; i < len(items); i++ {
		if id(items[i-1]) <= id(items[i]) {
			t.Fatal("未按新到旧排序")
		}
	}
	if len(f.req(t, "GET", "/api/workbench/items?kind=result", nil, 200)["items"].([]any)) != 0 {
		t.Fatal("返回超过七天的记录")
	}
	f.user(t, "photo", 4, 1)
	f.login(t, "photo")
	owned := wbInput(t, f)
	f.req(t, "POST", "/api/workbench/process", obj{"inputs": []obj{{"input_id": owned}}, "ops": obj{"ai": obj{"quick": []string{"dust"}}}}, 403)
	f.req(t, "GET", fmt.Sprintf("/api/workbench/items/%d/file?download=1", owned), nil, 403)
	f.req(t, "POST", "/api/workbench/download", obj{"ids": []int64{owned}}, 403)
	wbBinary(t, f, "GET", fmt.Sprintf("/api/workbench/items/%d/file", owned), nil, 200)
}

func TestWorkbenchQuotaChangeDiscardsWholeStagedBatch(t *testing.T) {
	output := imageBytes(t, 20, 20)
	var calls atomic.Int32
	var f *fixture
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Simulate the administrator lowering a quota while the first call is in
		// flight. Preflight has already passed; the next core call must reject it.
		if _, err := f.c.DB.Exec("INSERT INTO quotas(user_id,month_images) VALUES(1,1) ON CONFLICT(user_id) DO UPDATE SET month_images=1"); err != nil {
			t.Error(err)
		}
		json.NewEncoder(w).Encode(obj{"data": []obj{{"b64_json": base64.StdEncoding.EncodeToString(output)}}})
	}))
	defer ai.Close()
	f = newFixture(t, func(c *core.Config) { c.AIKey = "fake"; c.AIBaseURL = ai.URL })
	prompts(t, f)
	first, second := wbInput(t, f), wbInput(t, f)
	j := wbJob(t, f, []obj{{"input_id": first}, {"input_id": second}}, obj{"ai": obj{"quick": []string{"dust"}}})
	if j.Status != "failed" || !strings.Contains(j.Message, "额度") || calls.Load() != 1 {
		t.Fatal(j, calls.Load())
	}
	var results int
	if err := f.c.DB.QueryRow("SELECT count(*) FROM workbench_items WHERE kind='result'").Scan(&results); err != nil {
		t.Fatal(err)
	}
	if results != 0 {
		t.Fatal("中途额度失败发布了部分结果", results)
	}
	files, err := os.ReadDir(filepath.Join(f.c.Cfg.DataDir, "workbench", "1", "results"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatal("额度失败留下临时结果", files)
	}
}
