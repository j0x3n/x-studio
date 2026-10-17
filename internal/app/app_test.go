package app

import (
	"bytes"
	"chutu"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	a      *App
	s      *httptest.Server
	client *http.Client
}

func newFixture(t *testing.T, change func(*Config)) *fixture {
	t.Helper()
	web, _ := fs.Sub(chutu.Assets, "web")
	slots, _ := chutu.Assets.ReadFile("槽位.json")
	root := t.TempDir()
	c := Config{DataDir: filepath.Join(root, "data"), TemplateDir: filepath.Join("..", "..", "templates"), PromptDir: filepath.Join(root, "prompts"), AdminUser: "admin", AdminPassword: "password123", SessionSecret: strings.Repeat("s", 32), Workers: 2, AIBaseURL: "http://127.0.0.1:1/v1", RembgURL: "http://127.0.0.1:1"}
	if change != nil {
		change(&c)
	}
	a, e := New(c, web, slots)
	if e != nil {
		t.Fatal(e)
	}
	s := httptest.NewServer(a.Handler())
	a.SetBaseURL(s.URL)
	jar, _ := cookiejar.New(nil)
	f := &fixture{a, s, &http.Client{Jar: jar}}
	t.Cleanup(func() {
		s.Close()
		if e := a.Close(); e != nil {
			t.Error(e)
		}
	})
	return f
}
func (f *fixture) req(t *testing.T, method, path string, body any, want int) Object {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = strings.NewReader(js(body))
	}
	req, e := http.NewRequest(method, f.s.URL+path, r)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("X-Requested-With", "fetch")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, e := f.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(res.Body)
	if e != nil {
		t.Fatal(e)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, res.StatusCode, want, b)
	}
	var o Object
	if e = json.Unmarshal(b, &o); e != nil {
		t.Fatalf("invalid JSON: %s", b)
	}
	return o
}
func (f *fixture) login(t *testing.T, name string) {
	f.req(t, "POST", "/api/login", Object{"username": name, "password": "password123"}, 200)
}
func (f *fixture) product(t *testing.T) int64 {
	r := f.req(t, "POST", "/api/products", Object{"name": "测试珍珠"}, 201)
	return int64(r["product"].(Object)["id"].(float64))
}
func photoBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBA{240, 240, 240, 255}
			if x > w/3 && x < w*2/3 && y > h/3 && y < h*2/3 {
				c = color.NRGBA{190, 125, 150, 255}
			}
			im.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if e := png.Encode(&b, im); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func (f *fixture) upload(t *testing.T, id int64, name string, b []byte, want int) Object {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, e := mw.CreateFormFile("files", name)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = part.Write(b)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/products/%d/photos", f.s.URL, id), &buf)
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, e := f.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		t.Fatalf("upload got %d want %d: %s", res.StatusCode, want, body)
	}
	var out Object
	_ = json.Unmarshal(body, &out)
	return out
}
func waitJob(t *testing.T, a *App, id int64) Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		j, e := scanJob(a.db.QueryRow("SELECT * FROM jobs WHERE id=?", id))
		if e != nil {
			t.Fatal(e)
		}
		if j.Status == "done" || j.Status == "failed" {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job timeout")
	return Job{}
}

func TestAuthenticationPermissionsCSRF(t *testing.T) {
	f := newFixture(t, nil)
	f.req(t, "GET", "/api/me", nil, 401)
	req, _ := http.NewRequest("POST", f.s.URL+"/api/login", strings.NewReader(`{"username":"admin","password":"password123"}`))
	res, e := f.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	_ = res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("login requires CSRF header")
	}
	f.login(t, "admin")
	if cookies := f.client.Jar.Cookies(req.URL); len(cookies) == 0 {
		t.Fatal("missing session cookie")
	}
	// Inspect the actual Set-Cookie attributes, which cookiejar omits.
	req.Header.Set("X-Requested-With", "fetch")
	req.Body = io.NopCloser(strings.NewReader(`{"username":"admin","password":"password123"}`))
	res, e = f.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	_ = res.Body.Close()
	if !strings.Contains(res.Header.Get("Set-Cookie"), "HttpOnly") || !strings.Contains(res.Header.Get("Set-Cookie"), "SameSite=Lax") {
		t.Fatal("unsafe session cookie")
	}
	id := f.product(t)
	f.req(t, "POST", "/api/users", Object{"username": "staff", "password": "password123", "role": "staff"}, 201)
	f.req(t, "POST", "/api/users", Object{"username": "other", "password": "password123", "role": "staff"}, 201)
	f.login(t, "staff")
	f.req(t, "GET", fmt.Sprintf("/api/products/%d", id), nil, 200)
	f.req(t, "PATCH", fmt.Sprintf("/api/products/%d", id), Object{"name": "越权修改"}, 403)
	f.req(t, "DELETE", fmt.Sprintf("/api/products/%d", id), nil, 403)
	f.req(t, "POST", fmt.Sprintf("/api/products/%d/run", id), nil, 403)
	f.req(t, "GET", "/api/users", nil, 403)
	f.req(t, "POST", "/api/users", Object{}, 403)
	owned := f.product(t)
	out := f.upload(t, owned, "test.png", photoBytes(t, 24, 32), 201)
	pid := int64(out["photos"].([]any)[0].(Object)["id"].(float64))
	f.req(t, "PATCH", fmt.Sprintf("/api/photos/%d", pid), Object{"code": "A1"}, 200)
	f.login(t, "other")
	f.req(t, "PATCH", fmt.Sprintf("/api/photos/%d", pid), Object{"code": "A2"}, 403)
	f.req(t, "DELETE", fmt.Sprintf("/api/photos/%d", pid), nil, 403)
	f.upload(t, owned, "x.png", photoBytes(t, 12, 12), 403)
	f.login(t, "staff")
	f.req(t, "POST", "/api/logout", nil, 200)
	f.req(t, "GET", "/api/me", nil, 401)
	f.login(t, "admin")
	users := f.req(t, "GET", "/api/users", nil, 200)["users"].([]any)
	var staffID int64
	for _, v := range users {
		u := v.(Object)
		if u["username"] == "staff" {
			staffID = int64(u["id"].(float64))
		}
	}
	jar, _ := cookiejar.New(nil)
	staffClient := &http.Client{Jar: jar}
	old := f.client
	f.client = staffClient
	f.login(t, "staff")
	f.client = old
	f.req(t, "PATCH", fmt.Sprintf("/api/users/%d", staffID), Object{"disabled": true}, 200)
	f.client = staffClient
	f.req(t, "GET", "/api/me", nil, 401)
}
func TestLoginRateLimitAndTampering(t *testing.T) {
	f := newFixture(t, nil)
	for i := 0; i < 10; i++ {
		f.req(t, "POST", "/api/login", Object{"username": "admin", "password": "bad"}, 401)
	}
	f.req(t, "POST", "/api/login", Object{"username": "admin", "password": "password123"}, 429)
	f.a.limitMu.Lock()
	for k, v := range f.a.failures {
		for i := range v {
			v[i] = time.Now().Add(-6 * time.Minute)
		}
		f.a.failures[k] = v
	}
	f.a.limitMu.Unlock()
	f.login(t, "admin")
	url, _ := http.NewRequest("GET", f.s.URL, nil)
	f.client.Jar.SetCookies(url.URL, []*http.Cookie{{Name: "session", Value: "tampered", Path: "/"}})
	f.req(t, "GET", "/api/me", nil, 401)
}
func TestUploadValidationAndTraversal(t *testing.T) {
	f := newFixture(t, nil)
	f.login(t, "admin")
	id := f.product(t)
	f.upload(t, id, "fake.jpg", []byte("this is not a JPEG"), 400)
	f.upload(t, id, "too-big.png", make([]byte, maxPhotoBytes+1), 413)
	r := f.upload(t, id, `..\..\escape.png`, photoBytes(t, 64, 32), 201)
	ph := r["photos"].([]any)[0].(Object)
	name := ph["file"].(string)
	if strings.Contains(name, "..") || strings.Contains(name, "\\") {
		t.Fatal("unsafe filename", name)
	}
	if _, e := os.Stat(filepath.Join(f.a.dir(id), filepath.FromSlash(name))); e != nil {
		t.Fatal(e)
	}
	// The content, rather than an attacker-controlled extension, chooses the format.
	r = f.upload(t, id, "disguised.jpg", photoBytes(t, 24, 24), 201)
	if !strings.HasSuffix(r["photos"].([]any)[0].(Object)["file"].(string), ".png") {
		t.Fatal("extension trusted")
	}
	f.req(t, "GET", fmt.Sprintf("/files/products/%d/照片/..%%5c..%%5capp.db", id), nil, 400)
}
func TestQueueFailureAndRestartRecovery(t *testing.T) {
	f := newFixture(t, nil)
	f.login(t, "admin")
	id := f.product(t)
	for i := 0; i < 3; i++ {
		f.upload(t, id, fmt.Sprintf("%d.png", i), photoBytes(t, 24, 32), 201)
	}
	if e := os.MkdirAll(f.a.cfg.PromptDir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(f.a.cfg.PromptDir, "分拣.md"), []byte("输出 JSON"), 0600); e != nil {
		t.Fatal(e)
	}
	j := f.req(t, "POST", fmt.Sprintf("/api/products/%d/run", id), nil, 202)["job"].(Object)
	done := waitJob(t, f.a, int64(j["id"].(float64)))
	if done.Status != "failed" || !strings.Contains(done.Message, "AI_API_KEY") {
		t.Fatal(done)
	}
	p, _ := f.a.product(id)
	if p.Step != "classify" || p.Status != "failed" {
		t.Fatal(p)
	}
	photos, _ := f.a.photos(id)
	if _, ok := photos[0].Metrics["blur"]; !ok {
		t.Fatal("check results not preserved")
	}
	f.req(t, "POST", fmt.Sprintf("/api/products/%d/run?step=nope", id), nil, 400)
	f.req(t, "POST", fmt.Sprintf("/api/products/%d/run?step=check&from=copy", id), nil, 400)
	// Reopen a persisted DB with an interrupted job, before starting workers.
	_, e := f.a.db.Exec("INSERT INTO jobs(product_id,kind,status,created_at,steps) VALUES(?,'render','running',?,'render')", id, now())
	if e != nil {
		t.Fatal(e)
	}
	web, _ := fs.Sub(chutu.Assets, "web")
	other, e := New(f.a.cfg, web, f.a.slots)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	latest := other.latestJob(id)
	if latest.Status != "failed" || latest.Message != "服务重启中断" {
		t.Fatal(latest)
	}
}
func TestCoveragePrefixAndMissingPages(t *testing.T) {
	f := newFixture(t, nil)
	c := f.a.coverage([]Photo{{Code: "C4_8mm"}, {Code: "A3_白"}})
	main := c["main"].([]Object)
	if main[3]["covered"] != true || main[4]["covered"] != true {
		t.Fatal(c)
	}
	if len(c["need_reshoot"].([]Object)) == 0 {
		t.Fatal("missing reshoot warnings")
	}
	res, e := f.client.Get(f.s.URL + "/login.html")
	if e != nil {
		t.Fatal(e)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 404 {
		t.Fatal(res.StatusCode)
	}
	f.login(t, "admin")
	f.req(t, "GET", "/api/codes", nil, 200)
	f.req(t, "GET", "/api/slots", nil, 200)
}
