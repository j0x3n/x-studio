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
)

func prompts(t *testing.T, f *fixture) {
	t.Helper()
	if err := os.MkdirAll(f.c.Cfg.PromptDir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"处理-高清化.md": "高清化但不改变商品", "处理-快捷需求.md": "dust\t轻度\t去灰尘\nglare\t轻度\t去反光\nbright\t轻度\t提亮\nwb\t轻度\t校色\nbg_white\t改图\t白底\nbg_linen\t改图\t亚麻\nbg_velvet\t改图\t丝绒\nshadow\t改图\t投影", "处理-额外需求.md": "处理：{{quick}}；额外：{{extra}}"} {
		if err := os.WriteFile(filepath.Join(f.c.Cfg.PromptDir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func processJob(t *testing.T, f *fixture, ids []int64, ops obj, current bool) core.Job {
	t.Helper()
	o := f.req(t, "POST", "/api/assets/process", obj{"asset_ids": ids, "ops": ops, "set_current": current}, 202)
	return f.wait(t, id(o["job"]))
}
func newID(t *testing.T, j core.Job) int64 {
	t.Helper()
	if j.Status != "done" {
		t.Fatal(j)
	}
	items := j.Result["items"].([]any)
	o := items[0].(obj)
	if o["error"] != nil {
		t.Fatal(o)
	}
	return int64(o["new_asset_id"].(float64))
}
func TestLocalProcessGeometryVersionsAndSetCurrent(t *testing.T) {
	f := newFixture(t, nil)
	p := f.product(t, "处理", 1)
	b := imageBytes(t, 80, 40)
	aid := f.upload(t, p, b)
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), obj{"code": "B1", "rotate": 90, "crop": []float64{0, 0, .5, 1}}, 200)
	f.user(t, "photo", 4, 1)
	f.login(t, "photo")
	j := processJob(t, f, []int64{aid}, obj{"format": "png"}, true)
	nid := newID(t, j)
	var parent, created int64
	var file, code, source, op string
	var w, h int
	if err := f.c.DB.QueryRow("SELECT parent_id,created_by,file,code,source,op,width,height FROM assets WHERE id=?", nid).Scan(&parent, &created, &file, &code, &source, &op, &w, &h); err != nil {
		t.Fatal(err)
	}
	if parent != aid || created == 1 || code != "B1" || source != "original" || w != 80 || h != 40 || !strings.Contains(file, fmt.Sprintf("proc_%d.png", nid)) {
		t.Fatal(parent, created, code, source, w, h, file)
	}
	var originalFile, originalCode string
	f.c.DB.QueryRow("SELECT file,code FROM assets WHERE id=?", aid).Scan(&originalFile, &originalCode)
	data, _ := os.ReadFile(filepath.Join(f.c.Dir(p), filepath.FromSlash(originalFile)))
	if !bytes.Equal(data, b) || originalCode != "" {
		t.Fatal("原图文件被改动或编号未转移")
	}
	var detail obj
	json.Unmarshal([]byte(op), &detail)
	if detail["ops"] == nil || detail["elapsed_ms"] == nil || detail["warnings"] == nil {
		t.Fatal(op)
	}
	j = processJob(t, f, []int64{aid}, obj{"geometry": obj{"rotate": 90, "crop": []float64{0, 0, .5, 1}}, "format": "png"}, false)
	nid = newID(t, j)
	f.c.DB.QueryRow("SELECT width,height FROM assets WHERE id=?", nid).Scan(&w, &h)
	if w != 20 || h != 80 {
		t.Fatal("叠加了素材原 crop/rotate", w, h)
	}
	f.assertAudit(t, "assets.process", "assets.set_current")
}
func TestAIProcessPermissionsSourcesDiffAndQuota(t *testing.T) {
	var calls atomic.Int32
	output := imageBytes(t, 64, 48)
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			t.Error(err)
		}
		json.NewEncoder(w).Encode(obj{"data": []obj{{"b64_json": base64.StdEncoding.EncodeToString(output)}}})
	}))
	defer ai.Close()
	f := newFixture(t, func(c *core.Config) { c.AIKey = "fake"; c.AIBaseURL = ai.URL })
	prompts(t, f)
	p := f.product(t, "AI 处理", 1)
	aid := f.upload(t, p, imageBytes(t, 48, 64))
	photo := f.user(t, "photo", 4, 1)
	f.login(t, "photo")
	f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{aid}, "ops": obj{"ai": obj{"quick": []string{"bg_white"}}}}, 403)
	j := processJob(t, f, []int64{aid}, obj{"upscale": obj{"target_long_side": 2048}}, false)
	nid := newID(t, j)
	var source, op string
	var w, h int
	f.c.DB.QueryRow("SELECT source,width,height,op FROM assets WHERE id=?", nid).Scan(&source, &w, &h, &op)
	if source != "ai_enhanced" || max(w, h) != 2048 || !strings.Contains(op, "无损放大") || !strings.Contains(op, "phash_distance") {
		t.Fatal(source, w, h, op)
	}
	f.login(t, "admin")
	for _, tc := range []struct {
		ops  obj
		want string
	}{{obj{"ai": obj{"quick": []string{"dust"}}}, "ai_enhanced"}, {obj{"ai": obj{"quick": []string{"bg_linen"}}}, "ai_edited"}, {obj{"ai": obj{"extra": "保留原有光泽"}}, "ai_edited"}} {
		nid = newID(t, processJob(t, f, []int64{aid}, tc.ops, false))
		f.c.DB.QueryRow("SELECT source FROM assets WHERE id=?", nid).Scan(&source)
		if source != tc.want {
			t.Fatal(source, tc.want)
		}
	}
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), obj{"source_override": "ai_generated"}, 200)
	nid = newID(t, processJob(t, f, []int64{aid}, obj{"ai": obj{"quick": []string{"dust"}}}, false))
	f.c.DB.QueryRow("SELECT source FROM assets WHERE id=?", nid).Scan(&source)
	if source != "ai_generated" {
		t.Fatal("来源降级")
	}
	nid = newID(t, processJob(t, f, []int64{aid}, obj{"format": "png"}, false))
	f.c.DB.QueryRow("SELECT source FROM assets WHERE id=?", nid).Scan(&source)
	if source != "ai_generated" {
		t.Fatal("本地处理未继承有效来源")
	}
	second := f.upload(t, p, output)
	f.req(t, "PUT", fmt.Sprintf("/api/users/%d/quota", photo), obj{"month_images": 2}, 200)
	before := calls.Load()
	var countBefore, countAfter int
	f.c.DB.QueryRow("SELECT count(*) FROM assets").Scan(&countBefore)
	f.login(t, "photo")
	j = processJob(t, f, []int64{aid, second}, obj{"upscale": obj{"target_long_side": 2048}}, false)
	f.c.DB.QueryRow("SELECT count(*) FROM assets").Scan(&countAfter)
	if j.Status != "failed" || !strings.Contains(j.Message, "额度") || calls.Load() != before || countAfter != countBefore {
		t.Fatal("额度未拦截", j, calls.Load())
	}
}
func TestProcessPreflightPartialFailureAndScope(t *testing.T) {
	f := newFixture(t, nil)
	p := f.product(t, "逐张结果", 1)
	a1 := f.upload(t, p, imageBytes(t, 20, 30))
	a2 := f.upload(t, p, imageBytes(t, 20, 30))
	var path string
	f.c.DB.QueryRow("SELECT file FROM assets WHERE id=?", a2).Scan(&path)
	os.Remove(filepath.Join(f.c.Dir(p), filepath.FromSlash(path)))
	j := processJob(t, f, []int64{a1, a2}, obj{}, false)
	if j.Status != "done" || j.Result["items"].([]any)[1].(obj)["error"] == nil {
		t.Fatal(j)
	}
	newID(t, j)
	j = processJob(t, f, []int64{a1}, obj{"ai": obj{"quick": []string{"dust"}}}, false)
	if j.Status != "failed" || !strings.Contains(j.Message, "缺少提示词") {
		t.Fatal(j)
	}
	prompts(t, f)
	j = processJob(t, f, []int64{a1}, obj{"ai": obj{"quick": []string{"dust"}}}, false)
	if j.Status != "failed" || !strings.Contains(j.Message, "未配置 AI_API_KEY") {
		t.Fatal("缺少 AI 配置应在整批前置检查失败", j)
	}
	hidden := f.product(t, "隐藏")
	hid := f.upload(t, hidden, imageBytes(t, 20, 30))
	f.user(t, "op", 2, 1)
	f.login(t, "op")
	f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{hid}, "ops": obj{}}, 404)
	f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{a1}, "ops": obj{"color": obj{"match_to": hid}}}, 404)
	f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{a1}, "ops": obj{"color": obj{"match_to": a1}}}, 400)
	if _, err := f.c.DB.Exec("INSERT INTO jobs(kind,target_type,target_id,user_id,status,steps,created_at) SELECT 'pipeline','listing',id,1,'running','check',? FROM listings WHERE product_id=?", core.Now(), p); err != nil {
		t.Fatal(err)
	}
	f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{a1}, "ops": obj{}, "set_current": true}, 409)
}
func TestSetCurrentRechecksRunningPipelineAtExecution(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	output := imageBytes(t, 20, 30)
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		json.NewEncoder(w).Encode(obj{"data": []obj{{"b64_json": base64.StdEncoding.EncodeToString(output)}}})
	}))
	defer ai.Close()
	f := newFixture(t, func(c *core.Config) { c.AIKey = "fake"; c.AIBaseURL = ai.URL })
	prompts(t, f)
	p := f.product(t, "并发保护", 1)
	aid := f.upload(t, p, output)
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), obj{"code": "A1"}, 200)
	o := f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{aid}, "ops": obj{"ai": obj{"quick": []string{"dust"}}}, "set_current": true}, 202)
	<-started
	_, err := f.c.DB.Exec("INSERT INTO jobs(kind,target_type,target_id,user_id,status,steps,created_at) SELECT 'pipeline','listing',id,1,'running','retouch',? FROM listings WHERE product_id=?", core.Now(), p)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	j := f.wait(t, id(o["job"]))
	nid := newID(t, j)
	var code, warnings string
	f.c.DB.QueryRow("SELECT code,op FROM assets WHERE id=?", nid).Scan(&code, &warnings)
	if code != "" || !strings.Contains(warnings, "跳过设为当前用图") {
		t.Fatal(code, warnings)
	}
	f.c.DB.QueryRow("SELECT code FROM assets WHERE id=?", aid).Scan(&code)
	if code != "A1" {
		t.Fatal("冲突期间仍转移了编号")
	}
}
func TestColorReferenceCutoutAndAspect(t *testing.T) {
	f := newFixture(t, nil)
	p := f.product(t, "本地操作", 1)
	aid := f.upload(t, p, imageBytes(t, 64, 32))
	ref := f.upload(t, p, imageBytes(t, 64, 32))
	j := processJob(t, f, []int64{aid}, obj{"geometry": obj{"rotate": 90}, "aspect": "1:1", "color": obj{"auto_wb": true, "match_to": ref}, "resize": obj{"long_side": 100}, "format": "png"}, false)
	nid := newID(t, j)
	var file string
	f.c.DB.QueryRow("SELECT file FROM assets WHERE id=?", nid).Scan(&file)
	data, _ := os.ReadFile(filepath.Join(f.c.Dir(p), filepath.FromSlash(file)))
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 100 || cfg.Height != 100 {
		t.Fatal(cfg, err)
	}
}

func TestCutoutKeepsAspectAndOriginalDimensions(t *testing.T) {
	f := newFixture(t, nil)
	p := f.product(t, "原尺寸抠图", 1)
	im := image.NewNRGBA(image.Rect(0, 0, 900, 1200))
	for y := 0; y < 1200; y++ {
		for x := 0; x < 900; x++ {
			c := color.NRGBA{20, 30, 40, 255}
			if x >= 300 && x < 600 && y >= 400 && y < 800 {
				c = color.NRGBA{190, 90, 130, 255}
			}
			im.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, im); err != nil {
		t.Fatal(err)
	}
	aid := f.upload(t, p, buf.Bytes())
	for _, background := range []string{"white", "transparent"} {
		nid := newID(t, processJob(t, f, []int64{aid}, obj{"aspect": "3:4", "cutout": obj{"background": background}, "format": "png"}, false))
		var file string
		f.c.DB.QueryRow("SELECT file FROM assets WHERE id=?", nid).Scan(&file)
		data, err := os.ReadFile(filepath.Join(f.c.Dir(p), filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		out, err := png.Decode(bytes.NewReader(data))
		if err != nil || out.Bounds().Size() != image.Pt(900, 1200) {
			t.Fatal("抠图改变了比例或尺寸", err)
		}
		bg := color.NRGBAModel.Convert(out.At(0, 0)).(color.NRGBA)
		fg := color.NRGBAModel.Convert(out.At(450, 600)).(color.NRGBA)
		if fg != im.NRGBAAt(450, 600) {
			t.Fatal("商品像素被缩放或平移", fg)
		}
		if background == "white" && bg != (color.NRGBA{255, 255, 255, 255}) {
			t.Fatal("背景不是纯白", bg)
		}
		if background == "transparent" && (bg.A != 0 || fg.A != 255) {
			t.Fatal("透明蒙版错误", bg, fg)
		}
	}
}
