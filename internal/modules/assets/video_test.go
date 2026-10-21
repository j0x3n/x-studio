package assets_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xstudio/internal/core"
	"xstudio/internal/modules/assets"
)

func (f *fixture) postFile(t *testing.T, pid int64, name string, b []byte, status int) obj {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("files", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(b); err != nil {
		t.Fatal(err)
	}
	if err = mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/products/%d/assets", f.s.URL, pid), &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out obj
	if json.Unmarshal(raw, &out) != nil {
		t.Fatalf("无效 JSON：%s", raw)
	}
	if res.StatusCode != status {
		t.Fatalf("上传 %s：%d，期望 %d：%s", name, res.StatusCode, status, raw)
	}
	if status != 201 {
		return out
	}
	items := out["items"].([]any)
	if len(items) != 1 {
		t.Fatal(out)
	}
	return items[0].(obj)
}

func (f *fixture) getRaw(t *testing.T, path string, hdr map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", f.s.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func videoBox(brand string, compat ...string) []byte {
	payload := append([]byte(brand), 0, 0, 0, 0)
	for _, c := range compat {
		payload = append(payload, []byte(c)...)
	}
	for len(payload)%4 != 0 {
		payload = append(payload, 0)
	}
	box := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(box[:4], uint32(len(box)))
	copy(box[4:], "ftyp")
	copy(box[8:], payload)
	return box
}

func assertVideoNote(t *testing.T, item obj) {
	t.Helper()
	metrics, _ := item["metrics"].(map[string]any)
	note, _ := metrics["video_note"].(string)
	if !strings.Contains(note, "没有生成封面") || !strings.Contains(note, "没有找到 ffmpeg") {
		t.Fatal(item)
	}
}

func TestVideoUploadRangeAndFilters(t *testing.T) {
	f := newFixture(t, nil)
	missing := filepath.Join(t.TempDir(), "missing-ffmpeg")
	t.Cleanup(assets.SetFFmpegBins(missing, missing+"-probe"))
	pid := f.product(t, "视频款", 1)

	mp4 := videoBox("isom", "isom", "mp42")
	video := f.postFile(t, pid, "clip.exe", mp4, 201)
	vid := id(video)
	if video["media"] != "video" || video["kind"] != "photo" || video["source"] != "original" || video["code"] != "" || video["poster_url"] != "" || video["duration_ms"] != float64(0) || video["width"] != float64(0) || video["height"] != float64(0) || video["stale"] != false {
		t.Fatal(video)
	}
	file, _ := video["file"].(string)
	if !strings.Contains(file, "原图/视频/") || !strings.HasSuffix(file, ".mp4") {
		t.Fatal(video)
	}
	assertVideoNote(t, video)

	imageItem := f.postFile(t, pid, "fake.mp4", imageBytes(t, 40, 30), 201)
	if imageItem["media"] != "image" || len(imageItem["phash"].(string)) != 16 {
		t.Fatal(imageItem)
	}
	exe := append([]byte("MZ"), bytes.Repeat([]byte{0}, 32)...)
	if f.postFile(t, pid, "photo.mp4", exe, 400)["error"] != "只接受真实的 JPG、PNG、WebP 图片，或 MP4、MOV、WebM 视频" {
		t.Fatal("伪造扩展名没有被拒绝")
	}

	list := f.req(t, "GET", fmt.Sprintf("/api/products/%d/assets", pid), nil, 200)
	items := list["items"].([]any)
	if len(items) != 2 || items[0].(obj)["media"] != "image" || items[1].(obj)["media"] != "video" || items[1].(obj)["code"] != "" {
		t.Fatal(list)
	}
	photos, err := f.c.Photos(pid)
	if err != nil || len(photos) != 1 || photos[0].Media != "image" {
		t.Fatal(photos, err)
	}
	all, err := f.c.AllPhotos(pid)
	if err != nil || len(all) != 2 || all[0].Media != "image" || all[1].Media != "video" {
		t.Fatal(all, err)
	}

	res := f.getRaw(t, video["url"].(string), map[string]string{"Range": "bytes=0-3"})
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || res.Header.Get("Content-Type") != "video/mp4" || !bytes.Equal(body, mp4[:4]) {
		t.Fatalf("Range：%d %s %q", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
	if f.req(t, "GET", fmt.Sprintf("/api/assets/%d/poster", vid), nil, 404)["error"] != "没有封面" {
		t.Fatal("封面")
	}
	thumb := f.getRaw(t, video["url"].(string)+"?w=240", nil)
	tb, _ := io.ReadAll(thumb.Body)
	thumb.Body.Close()
	if thumb.StatusCode != 404 || !bytes.Contains(tb, []byte("没有封面")) {
		t.Fatalf("缩略图：%d %s", thumb.StatusCode, tb)
	}

	mov := f.postFile(t, pid, "clip.dat", videoBox("qt  "), 201)
	movRes := f.getRaw(t, mov["url"].(string), map[string]string{"Range": "bytes=0-3"})
	movRes.Body.Close()
	if mov["media"] != "video" || !strings.HasSuffix(mov["file"].(string), ".mov") || movRes.StatusCode != 206 || movRes.Header.Get("Content-Type") != "video/quicktime" {
		t.Fatal(mov, movRes.StatusCode, movRes.Header.Get("Content-Type"))
	}
	webm := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, bytes.Repeat([]byte{0}, 28)...)
	webmItem := f.postFile(t, pid, "clip.bin", webm, 201)
	webmRes := f.getRaw(t, webmItem["url"].(string), map[string]string{"Range": "bytes=0-3"})
	webmRes.Body.Close()
	if webmItem["media"] != "video" || !strings.HasSuffix(webmItem["file"].(string), ".webm") || webmRes.StatusCode != 206 || webmRes.Header.Get("Content-Type") != "video/webm" {
		t.Fatal(webmItem, webmRes.StatusCode, webmRes.Header.Get("Content-Type"))
	}

	restoreLimit := assets.SetMaxVideoBytes(48)
	defer restoreLimit()
	over := append(videoBox("isom", "isom", "mp42"), bytes.Repeat([]byte{1}, 64)...)
	if f.postFile(t, pid, "big.mp4", over, 413)["error"] != "视频不能超过 500 MB" {
		t.Fatal("超限提示不对")
	}
	restoreLimit()
	dir := filepath.Join(f.c.Cfg.DataDir, "products", fmt.Sprint(pid), "原图", "视频")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".upload-") || strings.HasPrefix(e.Name(), ".poster-") {
			t.Fatal(e.Name())
		}
	}

	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", vid), obj{"title": "开箱"}, 200)
	if f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", vid), obj{"code": "A1"}, 400)["error"] != "视频不能用来设置拍摄编号" {
		t.Fatal("编号")
	}
	for _, ops := range []obj{{"upscale": obj{"target_long_side": 2048}}, {"cutout": obj{"background": "white"}}, {}} {
		if f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{vid}, "ops": ops}, 400)["error"] != "视频不能用来做图片处理" {
			t.Fatal(ops)
		}
	}
	imgID := id(imageItem)
	if f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{imgID}, "ops": obj{"color": obj{"match_to": vid}}}, 400)["error"] != "视频不能用来做图片处理" {
		t.Fatal("校色参考")
	}
	if f.req(t, "POST", "/api/workbench/process", obj{"inputs": []obj{{"asset_id": vid}}, "ops": obj{}}, 400)["error"] != "视频不能用来导入处理台" {
		t.Fatal("处理台")
	}
	if f.req(t, "POST", "/api/tryon/analyze", obj{"inputs": []obj{{"asset_id": vid}}}, 400)["error"] != "视频不能用来试穿试戴" {
		t.Fatal("试穿")
	}
	if f.req(t, "POST", fmt.Sprintf("/api/assets/%d/similar", vid), obj{}, 400)["error"] != "视频不能用来查找相似图" {
		t.Fatal("相似图")
	}
	if f.req(t, "POST", "/api/assets/download", obj{"asset_ids": []int64{vid}}, 400)["error"] != "视频不能用来打包成图片" {
		t.Fatal("打包")
	}

	var detail string
	if err = f.c.DB.QueryRow("SELECT detail FROM audit_log WHERE action='assets.upload' AND target_id=? ORDER BY id DESC LIMIT 1", pid).Scan(&detail); err != nil || !strings.Contains(detail, "video") {
		t.Fatal(detail, err)
	}

	lib := f.product(t, "旧版本", 1)
	base := f.upload(t, lib, imageBytes(t, 32, 24))
	oldVideo := f.postFile(t, lib, "old.mp4", mp4, 201)
	if _, err = f.c.DB.Exec("INSERT INTO assets(product_id,parent_id,file,orig_name,width,height,source,kind,created_by,created_at,stale) SELECT product_id,id,file,orig_name,width,height,'ai_enhanced','output',created_by,created_at,1 FROM assets WHERE id=?", base); err != nil {
		t.Fatal(err)
	}
	var child int64
	if err = f.c.DB.QueryRow("SELECT id FROM assets WHERE parent_id=? AND stale=1", base).Scan(&child); err != nil {
		t.Fatal(err)
	}
	def := f.req(t, "GET", fmt.Sprintf("/api/assets?product_id=%d", lib), nil, 200)
	if def["total"] != float64(2) {
		t.Fatal(def)
	}
	var parent obj
	for _, raw := range def["items"].([]any) {
		item := raw.(obj)
		if id(item) == base {
			parent = item
		}
		if id(item) == child {
			t.Fatal("默认列表露出了旧版本")
		}
	}
	if parent["version_count"] != float64(1) {
		t.Fatal(parent)
	}
	onlyOld := f.req(t, "GET", fmt.Sprintf("/api/assets?product_id=%d&stale=1", lib), nil, 200)
	if onlyOld["total"] != float64(1) || id(onlyOld["items"].([]any)[0]) != child || onlyOld["items"].([]any)[0].(obj)["stale"] != true {
		t.Fatal(onlyOld)
	}
	everything := f.req(t, "GET", fmt.Sprintf("/api/assets?product_id=%d&stale=all", lib), nil, 200)
	if everything["total"] != float64(3) {
		t.Fatal(everything)
	}
	detailView := f.req(t, "GET", fmt.Sprintf("/api/assets/%d", base), nil, 200)
	versions := detailView["versions"].([]any)
	if len(versions) != 2 {
		t.Fatal(detailView)
	}
	sawStale := false
	for _, raw := range versions {
		if raw.(obj)["stale"] == true {
			sawStale = true
		}
	}
	if !sawStale {
		t.Fatal(detailView)
	}
	if f.req(t, "GET", fmt.Sprintf("/api/assets?product_id=%d&media=video", lib), nil, 200)["total"] != float64(1) || id(f.req(t, "GET", fmt.Sprintf("/api/assets?product_id=%d&media=video", lib), nil, 200)["items"].([]any)[0]) != id(oldVideo) {
		t.Fatal("media=video")
	}
	if f.req(t, "GET", fmt.Sprintf("/api/assets?product_id=%d&media=image", lib), nil, 200)["total"] != float64(1) {
		t.Fatal("media=image")
	}
	if f.req(t, "GET", fmt.Sprintf("/api/assets?product_id=%d&media=image&stale=all", lib), nil, 200)["total"] != float64(2) {
		t.Fatal("media=image stale=all")
	}
	if f.req(t, "GET", fmt.Sprintf("/api/assets?product_id=%d&media=gif", lib), nil, 400)["error"] != "media 只能是 image 或 video" {
		t.Fatal("media")
	}
	if f.req(t, "GET", fmt.Sprintf("/api/assets?product_id=%d&stale=bogus", lib), nil, 400)["error"] != "stale 只能是 0、1 或 all" {
		t.Fatal("stale")
	}
}

func TestVideoPosterWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal("本机没有 ffmpeg，无法验证封面")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "clip.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-y", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=10:duration=2", "-pix_fmt", "yuv420p", "-c:v", "libx264", "-movflags", "+faststart", out)
	log, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("生成测试视频失败：%v %s", err, log)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, nil)
	pid := f.product(t, "有封面", 1)
	item := f.postFile(t, pid, "real.mp4", data, 201)
	dur, _ := item["duration_ms"].(float64)
	if item["media"] != "video" || item["width"] != float64(640) || item["height"] != float64(360) || dur < 1500 || dur > 2500 || item["poster_url"] == "" {
		t.Fatal(item)
	}
	poster := f.getRaw(t, item["poster_url"].(string), nil)
	pb, _ := io.ReadAll(poster.Body)
	poster.Body.Close()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(pb))
	if poster.StatusCode != 200 || poster.Header.Get("Content-Type") != "image/jpeg" || err != nil || format != "jpeg" || cfg.Width < 1 || cfg.Width > 1280 || cfg.Height < 1 {
		t.Fatalf("封面：%d %s %v %s %dx%d", poster.StatusCode, poster.Header.Get("Content-Type"), err, format, cfg.Width, cfg.Height)
	}
	thumb := f.getRaw(t, item["url"].(string)+"?w=240", nil)
	tb, _ := io.ReadAll(thumb.Body)
	thumb.Body.Close()
	tcfg, tformat, err := image.DecodeConfig(bytes.NewReader(tb))
	if thumb.StatusCode != 200 || err != nil || tformat != "jpeg" || tcfg.Width < 1 || tcfg.Width > 240 {
		t.Fatalf("缩略图：%d %v %s %dx%d %s", thumb.StatusCode, err, tformat, tcfg.Width, tcfg.Height, tb)
	}
	var media, kind, source, code, posterFile string
	if err = f.c.DB.QueryRow("SELECT media,kind,source,code,poster_file FROM assets WHERE id=?", id(item)).Scan(&media, &kind, &source, &code, &posterFile); err != nil || media != "video" || kind != "photo" || source != "original" || code != "" || posterFile == "" {
		t.Fatal(media, kind, source, code, posterFile, err)
	}
}

func TestSniffVideo(t *testing.T) {
	ext, ct, ok := core.SniffVideo(videoBox("isom", "mp42"))
	if !ok || ext != "mp4" || ct != "video/mp4" {
		t.Fatal(ext, ct, ok)
	}
	ext, ct, ok = core.SniffVideo(videoBox("mov "))
	if !ok || ext != "mov" || ct != "video/quicktime" {
		t.Fatal(ext, ct, ok)
	}
	ext, ct, ok = core.SniffVideo([]byte{0x1A, 0x45, 0xDF, 0xA3})
	if !ok || ext != "webm" || ct != "video/webm" {
		t.Fatal(ext, ct, ok)
	}
	if _, _, ok = core.SniffVideo(append([]byte("MZ"), bytes.Repeat([]byte{0}, 16)...)); ok {
		t.Fatal("exe 被当成视频")
	}
}
