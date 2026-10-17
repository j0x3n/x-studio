package assets_test

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"xstudio/internal/core"
)

type blockedWorkbenchWriter struct {
	*httptest.ResponseRecorder
	blocked chan struct{}
	release chan struct{}
	start   sync.Once
	finish  sync.Once
}

func newBlockedWorkbenchWriter() *blockedWorkbenchWriter {
	return &blockedWorkbenchWriter{ResponseRecorder: httptest.NewRecorder(), blocked: make(chan struct{}), release: make(chan struct{})}
}
func (w *blockedWorkbenchWriter) Write(b []byte) (int, error) {
	w.start.Do(func() { close(w.blocked) })
	<-w.release
	return w.ResponseRecorder.Write(b)
}
func (w *blockedWorkbenchWriter) unblock() { w.finish.Do(func() { close(w.release) }) }
func waitWorkbenchSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal(message)
	}
}
func directWorkbenchRequest(f *fixture, method, path string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, f.s.URL+path, body)
	r.Header.Set("X-Requested-With", "fetch")
	r.Header.Set("Content-Type", "application/json")
	for _, cookie := range f.client.Jar.Cookies(r.URL) {
		r.AddCookie(cookie)
	}
	return r
}
func assertWorkbenchMetadataCanWrite(t *testing.T, f *fixture, iid int64) {
	t.Helper()
	done := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		f.c.Mu.Lock()
		_, err := f.c.DB.Exec("UPDATE workbench_items SET op=op WHERE id=?", iid)
		f.c.Mu.Unlock()
		result <- err
		close(done)
	}()
	waitWorkbenchSignal(t, done, "慢请求挡住了需要 Core.Mu 的元数据写操作")
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
func TestWorkbenchReadsAndDownloadsDoNotHoldGlobalMutex(t *testing.T) {
	f := newFixture(t, nil)
	iid := wbInput(t, f)
	cases := []struct {
		name, method, path string
		body               io.Reader
	}{
		{"thumbnail-cold", "GET", fmt.Sprintf("/api/workbench/items/%d/file?w=240", iid), nil},
		{"thumbnail-cached", "GET", fmt.Sprintf("/api/workbench/items/%d/file?w=240", iid), nil},
		{"preview", "GET", fmt.Sprintf("/api/workbench/items/%d/file", iid), nil},
		{"single-download", "GET", fmt.Sprintf("/api/workbench/items/%d/file?download=1", iid), nil},
		{"zip-download", "POST", "/api/workbench/download", strings.NewReader(core.JS(obj{"ids": []int64{iid}}))},
		{"item", "GET", fmt.Sprintf("/api/workbench/items/%d", iid), nil},
		{"list", "GET", "/api/workbench/items", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newBlockedWorkbenchWriter()
			defer w.unblock()
			r := directWorkbenchRequest(f, tc.method, tc.path, tc.body)
			done := make(chan struct{})
			// Hold Core.Mu until the response body starts. Cold-thumbnail decoding,
			// encoding, cache publication, and ZIP creation must all finish meanwhile.
			f.c.Mu.Lock()
			go func() { defer close(done); f.c.Handler().ServeHTTP(w, r) }()
			select {
			case <-w.blocked:
				f.c.Mu.Unlock()
			case <-time.After(10 * time.Second):
				f.c.Mu.Unlock()
				w.unblock()
				waitWorkbenchSignal(t, done, "请求未退出")
				t.Fatal("读请求在生成响应前等待了全局锁")
			}
			// Keep the network writer blocked while another module-style write finishes.
			assertWorkbenchMetadataCanWrite(t, f, iid)
			w.unblock()
			waitWorkbenchSignal(t, done, "响应未完成")
			if w.Code != 200 {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
		})
	}
}

type gatedWorkbenchBody struct {
	prefix  *bytes.Reader
	rest    *bytes.Reader
	blocked chan struct{}
	release chan struct{}
	once    sync.Once
	finish  sync.Once
}

func (b *gatedWorkbenchBody) Read(p []byte) (int, error) {
	if b.prefix.Len() > 0 {
		return b.prefix.Read(p)
	}
	b.once.Do(func() { close(b.blocked) })
	<-b.release
	return b.rest.Read(p)
}
func (b *gatedWorkbenchBody) unblock() { b.finish.Do(func() { close(b.release) }) }
func TestWorkbenchSlowUploadDoesNotHoldGlobalMutexAndRollsBack(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			f := newFixture(t, nil)
			var buf bytes.Buffer
			mw := multipart.NewWriter(&buf)
			first, _ := mw.CreateFormFile("files", "first.png")
			first.Write(imageBytes(t, 40, 20))
			second, _ := mw.CreateFormFile("files", "second.png")
			cut := buf.Len()
			status := 201
			if valid {
				second.Write(imageBytes(t, 20, 40))
			} else {
				second.Write([]byte("invalid"))
				status = 400
			}
			mw.Close()
			body := &gatedWorkbenchBody{prefix: bytes.NewReader(buf.Bytes()[:cut]), rest: bytes.NewReader(buf.Bytes()[cut:]), blocked: make(chan struct{}), release: make(chan struct{})}
			defer body.unblock()
			r := directWorkbenchRequest(f, "POST", "/api/workbench/inputs", body)
			r.Header.Set("Content-Type", mw.FormDataContentType())
			w := newBlockedWorkbenchWriter()
			defer w.unblock()
			done := make(chan struct{})
			go func() { defer close(done); f.c.Handler().ServeHTTP(w, r) }()
			waitWorkbenchSignal(t, body.blocked, "上传请求未进入第二张的慢读")
			dir := filepath.Join(f.c.Cfg.DataDir, "workbench", "1", "inputs")
			files, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 || !strings.HasSuffix(files[0].Name(), ".tmp") {
				t.Fatal("第一张应暂存为临时文件", files)
			}
			var count int
			f.c.DB.QueryRow("SELECT count(*) FROM workbench_items").Scan(&count)
			if count != 0 {
				t.Fatal("读取整批前已发布记录")
			}
			assertWorkbenchMetadataCanWrite(t, f, 0)
			body.unblock()
			waitWorkbenchSignal(t, w.blocked, "上传未生成响应")
			assertWorkbenchMetadataCanWrite(t, f, 0)
			w.unblock()
			waitWorkbenchSignal(t, done, "上传未完成")
			if w.Code != status {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			files, err = os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			expected := 0
			if valid {
				expected = 2
			}
			if len(files) != expected {
				t.Fatal("临时文件未清理或正式文件不全", files)
			}
			for _, file := range files {
				if strings.HasSuffix(file.Name(), ".tmp") {
					t.Fatal("残留临时文件", file.Name())
				}
			}
		})
	}
}
func TestWorkbenchThumbnailCacheWhiteBackgroundAndDeletion(t *testing.T) {
	f := newFixture(t, nil)
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewNRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	iid := id(wbUpload(t, f, [][]byte{pngData.Bytes()}, 201)["items"].([]any)[0])
	var file string
	if err := f.c.DB.QueryRow("SELECT file FROM workbench_items WHERE id=?", iid).Scan(&file); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(filepath.Join(f.c.Cfg.DataDir, "workbench", "1"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	path := fmt.Sprintf("/api/workbench/items/%d/file?w=240", iid)
	first, header := wbBinary(t, f, "GET", path, nil, 200)
	if header.Get("Cache-Control") != "private, max-age=86400" || header.Get("Vary") != "Cookie" {
		t.Fatal(header)
	}
	cache := ".thumbs/240/" + file + ".jpg"
	cached, err := root.ReadFile(cache)
	if err != nil || !bytes.Equal(cached, first) {
		t.Fatal("缩略图未落盘", err)
	}
	im, format, err := image.Decode(bytes.NewReader(first))
	if err != nil || format != "jpeg" {
		t.Fatal(format, err)
	}
	red, green, blue, alpha := im.At(0, 0).RGBA()
	if red < 250*257 || green < 250*257 || blue < 250*257 || alpha != 65535 {
		t.Fatalf("透明区域没有合成白底: %d %d %d %d", red, green, blue, alpha)
	}
	// Corrupt the source: a cache hit must not re-read or decode the source.
	if err = root.WriteFile(file, []byte("source changed"), 0600); err != nil {
		t.Fatal(err)
	}
	second, _ := wbBinary(t, f, "GET", path, nil, 200)
	if !bytes.Equal(first, second) {
		t.Fatal("第二次请求未命中缓存")
	}
	root.WriteFile(file, pngData.Bytes(), 0600)
	for _, width := range []int{480, 960} {
		wbBinary(t, f, "GET", fmt.Sprintf("/api/workbench/items/%d/file?w=%d", iid, width), nil, 200)
	}
	_, header = wbBinary(t, f, "GET", fmt.Sprintf("/api/workbench/items/%d/file?download=1", iid), nil, 200)
	if header.Get("Cache-Control") != "private, no-store" {
		t.Fatal(header)
	}
	_, header = wbBinary(t, f, "POST", "/api/workbench/download", obj{"ids": []int64{iid}}, 200)
	if header.Get("Cache-Control") != "private, no-store" {
		t.Fatal(header)
	}
	f.req(t, "DELETE", fmt.Sprintf("/api/workbench/items/%d", iid), nil, 200)
	for _, width := range []int{240, 480, 960} {
		if _, err = root.Stat(fmt.Sprintf(".thumbs/%d/%s.jpg", width, file)); !os.IsNotExist(err) {
			t.Fatal("删除 item 残留缓存", width, err)
		}
	}
}

func TestWorkbenchMissingFilesReturnNotFound(t *testing.T) {
	f := newFixture(t, nil)
	iid := wbInput(t, f)
	var file string
	if err := f.c.DB.QueryRow("SELECT file FROM workbench_items WHERE id=?", iid).Scan(&file); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(filepath.Join(f.c.Cfg.DataDir, "workbench", "1"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = root.Remove(file); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"", "?w=240", "?download=1"} {
		f.req(t, "GET", fmt.Sprintf("/api/workbench/items/%d/file%s", iid, query), nil, 404)
	}
	f.req(t, "POST", "/api/workbench/download", obj{"ids": []int64{iid}}, 404)
}
