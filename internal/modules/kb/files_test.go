package kb

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func (f *fixture) upload(t *testing.T, name string, src io.Reader, size int64, article string, status int) object {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if article != "" {
		if err := mw.WriteField("article_id", article); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mw.CreateFormFile("file", name); err != nil {
		t.Fatal(err)
	}
	n := buf.Len()
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest("POST", f.s.URL+"/api/kb/files", io.MultiReader(bytes.NewReader(buf.Bytes()[:n]), src, bytes.NewReader(buf.Bytes()[n:])))
	if err != nil {
		t.Fatal(err)
	}
	r.ContentLength = int64(buf.Len()) + size
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("X-Requested-With", "fetch")
	res, err := f.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("upload %s: got %d want %d: %s", name, res.StatusCode, status, b)
	}
	var out object
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) { clear(b); return len(b), nil }
func padded(b []byte, size int64) io.Reader {
	return io.MultiReader(bytes.NewReader(b), io.LimitReader(zeroReader{}, size-int64(len(b))))
}
func mp4Bytes() []byte {
	return []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'i', 's', 'o', 'm', 'm', 'p', '4', '2'}
}
func webmBytes() []byte {
	return []byte{0x1a, 0x45, 0xdf, 0xa3, 0x87, 0x42, 0x82, 0x84, 'w', 'e', 'b', 'm'}
}

func TestUploadTypesAndFileAccess(t *testing.T) {
	f := boot(t)
	pngData := pngBytes(t)
	var jpg, g bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 2, 3)), nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 2, 3), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	webp, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEAAUAmJaQAA3AA/v89WAAAAA==")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, mime string
		data       []byte
	}{{"test.PNG", "image/png", pngData}, {"test.jpeg", "image/jpeg", jpg.Bytes()}, {"test.gif", "image/gif", g.Bytes()}, {"test.webp", "image/webp", webp}, {"test.mp4", "video/mp4", mp4Bytes()}, {"test.webm", "video/webm", webmBytes()}} {
		v := f.upload(t, tc.name, bytes.NewReader(tc.data), int64(len(tc.data)), "", 201)
		assertField(t, v, "mime", tc.mime)
		if strings.HasPrefix(tc.mime, "image/") && v["width"].(float64) < 1 {
			t.Fatal(v)
		}
		got := f.response(t, "GET", v["url"].(string), nil, 200)
		if !bytes.Equal(got, tc.data) {
			t.Fatal("wrong file bytes")
		}
		// Range requests are necessary for video playback.
		r, _ := http.NewRequest("GET", f.s.URL+v["url"].(string), nil)
		r.Header.Set("Range", "bytes=0-3")
		res, e := f.client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		part, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 206 || !bytes.Equal(part, tc.data[:4]) {
			t.Fatal("range", res.StatusCode, part)
		}
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{{"fake.jpg", []byte("<html>not an image</html>")}, {"wrong.jpg", pngData}, {"wrong.exe", pngData}, {"broken.png", pngData[:16]}, {"fake.webm", []byte{0x1a, 0x45, 0xdf, 0xa3, 0x88, 'm', 'a', 't', 'r', 'o', 's', 'k', 'a'}}} {
		f.upload(t, tc.name, bytes.NewReader(tc.data), int64(len(tc.data)), "", 400)
	}
	f.upload(t, "valid.png", bytes.NewReader(pngData), int64(len(pngData)), "9999", 404)
	if f.count(t, "SELECT count(*) FROM audit_log WHERE action='kb.file.upload'") != 6 {
		t.Fatal("missing upload audit")
	}
}

func TestUploadSizeBoundaries(t *testing.T) {
	f := boot(t)
	f.client.Timeout = time.Minute
	for _, tc := range []struct {
		name  string
		head  []byte
		limit int64
	}{{"limit.png", pngBytes(t), maxImageBytes}, {"limit.mp4", mp4Bytes(), maxVideoBytes}} {
		v := f.upload(t, tc.name, padded(tc.head, tc.limit), tc.limit, "", 201)
		assertField(t, v, "size", tc.limit)
		f.upload(t, tc.name, padded(tc.head, tc.limit+1), tc.limit+1, "", 413)
	}
	if f.count(t, "SELECT count(*) FROM kb_files") != 2 {
		t.Fatal("oversized file retained")
	}
}

func TestAttachmentClaimCleanupAndShutdown(t *testing.T) {
	f := boot(t)
	data := pngBytes(t)
	upload := func(article string) object {
		return f.upload(t, "test.png", bytes.NewReader(data), int64(len(data)), article, 201)
	}
	unbound := upload("")
	oldOrphan := upload("")
	freshOrphan := upload("")
	v := f.article(t, "附件归属", "![图片]("+unbound["url"].(string)+")", nil)
	p := articlePath(v)
	if f.count(t, "SELECT count(*) FROM kb_files WHERE id=? AND article_id=?", unbound["id"], v["id"]) != 1 {
		t.Fatal("attachment not claimed")
	}
	if len(v["files"].([]any)) != 1 {
		t.Fatal(v)
	}
	attachedUnused := upload(strings.TrimPrefix(p, "/api/kb/articles/"))
	// A second article references the same file without taking its ownership.
	f.article(t, "共享引用", unbound["url"].(string), nil)
	f.req(t, "PATCH", p, object{"base_version": 1, "body": "移除当前引用"}, 200)
	f.req(t, "DELETE", p, nil, 200)
	// Only the historical version now references this file.
	f.exec(t, "UPDATE kb_articles SET body='' WHERE title='共享引用'")
	f.exec(t, "UPDATE kb_versions SET body='' WHERE article_id<>(SELECT id FROM kb_articles WHERE title='附件归属')")
	old := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)
	for _, file := range []object{unbound, oldOrphan, attachedUnused} {
		f.exec(t, "UPDATE kb_files SET created_at=? WHERE id=?", old, file["id"])
	}
	if err := f.m.cleanupFiles(time.Now()); err != nil {
		t.Fatal(err)
	}
	if f.count(t, "SELECT count(*) FROM kb_files WHERE id IN (?,?)", unbound["id"], freshOrphan["id"]) != 2 {
		t.Fatal("referenced or fresh attachment removed")
	}
	for _, file := range []object{oldOrphan, attachedUnused} {
		if f.count(t, "SELECT count(*) FROM kb_files WHERE id=?", file["id"]) != 0 {
			t.Fatal("orphan record retained")
		}
		if _, err := os.Stat(filepath.Join(f.c.Cfg.DataDir, "kb", strings.TrimPrefix(file["url"].(string), "/files/kb/"))); !os.IsNotExist(err) {
			t.Fatal("orphan file retained", err)
		}
	}
	f.req(t, "POST", p+"/restore", nil, 200)
	f.req(t, "POST", p+"/revert", object{"version": 1, "base_version": 2}, 200)
	f.response(t, "GET", unbound["url"].(string), nil, 200)
	if f.count(t, "SELECT count(*) FROM audit_log WHERE action='kb.file.cleanup'") != 2 {
		t.Fatal("missing cleanup audits")
	}
	// Close must cancel and join the registered background goroutine.
	done := make(chan error, 1)
	go func() { done <- f.c.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		f.c = nil
	case <-time.After(3 * time.Second):
		t.Fatal("background cleanup did not exit")
	}
}

func TestFileTraversal(t *testing.T) {
	f := boot(t)
	root := filepath.Join(f.c.Cfg.DataDir, "kb")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	secret := []byte("PRIVATE OUTSIDE ROOT")
	if err := os.WriteFile(filepath.Join(f.c.Cfg.DataDir, "secret.png"), secret, 0600); err != nil {
		t.Fatal(err)
	}
	// Direct handler tests exercise ServeRoot's input boundary before ServeMux's
	// dot-segment redirects. Integration tests below ensure no bytes escape.
	for _, name := range []string{"../secret.png", "..\\secret.png", "C:/secret.png", "2026-10/../../secret.png", "/secret.png"} {
		r := httptest.NewRequest("GET", "/files/kb/x", nil)
		r.SetPathValue("path", name)
		w := httptest.NewRecorder()
		f.m.serveFile(w, r)
		if w.Code != 400 {
			t.Fatal(name, w.Code, w.Body.String())
		}
	}
	client := *f.client
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	for _, url := range []string{"/files/kb/%2e%2e/secret.png", "/files/kb/..%5csecret.png", "/files/kb/C%3a/secret.png", "/files/kb/2026-10/%2e%2e/%2e%2e/secret.png"} {
		r, _ := http.NewRequest("GET", f.s.URL+url, nil)
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 || bytes.Contains(b, secret) {
			t.Fatal("traversal leaked", url, res.StatusCode, string(b))
		}
	}
	// A database row cannot turn an escaping symlink into a readable file.
	link := filepath.Join(root, "link.png")
	if err := os.Symlink(filepath.Join(f.c.Cfg.DataDir, "secret.png"), link); err == nil {
		f.exec(t, "INSERT INTO kb_files(name,file,mime,size,created_by,created_at) VALUES('link','link.png','image/png',1,1,?)", time.Now().UTC().Format(time.RFC3339Nano))
		f.response(t, "GET", "/files/kb/link.png", nil, 404)
	}
}
