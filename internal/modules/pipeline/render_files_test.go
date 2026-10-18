package pipeline

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderTemplatePublicFilesAndStyles(t *testing.T) {
	root := t.TempDir()
	assets := []struct{ name, content, contentType string }{
		{"引擎.js", "window.renderEngine = true;", "application/javascript; charset=utf-8"},
		{"公共.css", ".screen { color: navy; }", "text/css; charset=utf-8"},
		{"图片.png", "png", "image/png"},
		{"图片.jpg", "jpg", "image/jpeg"},
		{"图标.svg", `<svg xmlns="http://www.w3.org/2000/svg"></svg>`, "image/svg+xml"},
		{"字体.woff2", "font", "font/woff2"},
	}
	for _, dir := range []string{"主图模板", "详情页模板"} {
		if err := os.MkdirAll(filepath.Join(root, dir, "子目录"), 0700); err != nil {
			t.Fatal(err)
		}
		for _, asset := range assets {
			if err := os.WriteFile(filepath.Join(root, dir, asset.name), []byte(asset.content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		for name, content := range map[string]string{
			"另一套.html":     `<!doctype html><html><head><script src="引擎.js"></script><link href="公共.css" rel="stylesheet"></head><body>另一套风格</body></html>`,
			"私有.txt":       "不允许公开",
			"两个..点.js":     "不允许公开",
			"子目录/引擎.js":    "不允许公开",
			"子目录/另一套.html": "不允许公开",
		} {
			if err := os.WriteFile(filepath.Join(root, dir, filepath.FromSlash(name)), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	f := newFixture(t, func(c *Config) { c.TemplateDir = root })
	f.login(t, "admin")
	pid := f.product(t)
	f.newUser(t, "photographer", 4, 1)
	f.newUser(t, "unassigned", 3)
	// Keep a second version outside the photographer's assigned shop.
	shop := idOf(f.req(t, "POST", "/api/shops", Object{"name": "另一店", "platform": "other", "language": "en"}, 201), "shop")
	otherListing := idOf(f.req(t, "POST", fmt.Sprintf("/api/products/%d/listings", pid), Object{"shop_id": shop}, 201), "listing")
	if _, err := f.a.DB.Exec("UPDATE listings SET content=?,main=? WHERE id=?", js(Object{"name": "动态详情"}), js(Object{"name": "动态主图"}), pid); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.a.Dir(pid), "照片"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.a.Dir(pid), "照片", "B1.jpg"), photoBytes(t, 8, 8), 0600); err != nil {
		t.Fatal(err)
	}

	get := func(client *http.Client, path string, status int, contentType, content string) {
		t.Helper()
		res, err := client.Get(f.s.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != status {
			t.Fatalf("%s：状态 %d，期望 %d：%s", path, res.StatusCode, status, body)
		}
		if contentType != "" && res.Header.Get("Content-Type") != contentType {
			t.Fatalf("%s：Content-Type 为 %q", path, res.Header.Get("Content-Type"))
		}
		if content != "" && !strings.Contains(string(body), content) {
			t.Fatalf("%s：缺少预期内容 %q：%s", path, content, body)
		}
	}
	anonymous := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	token := f.a.RenderToken()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokenClient := &http.Client{Jar: jar}
	for _, dir := range []string{"主图模板", "详情页模板"} {
		base := "/render/" + dir + "/"
		for _, asset := range assets {
			get(f.client, base+asset.name, 200, asset.contentType, asset.content)
			get(anonymous, base+asset.name+"?_t="+token, 200, asset.contentType, asset.content)
			get(anonymous, base+asset.name, 401, "", "")
		}
		style := base + "另一套.html"
		get(f.client, style, 404, "", "")
		get(anonymous, style+"?_t="+token, 404, "", "")
		get(anonymous, style+fmt.Sprintf("?p=%d", pid), 401, "", "")
		get(f.client, style+fmt.Sprintf("?p=%d", pid), 200, "text/html; charset=utf-8", "另一套风格")
		get(anonymous, style+fmt.Sprintf("?p=%d&_t=%s", pid, token), 200, "text/html; charset=utf-8", "另一套风格")
		get(tokenClient, style+fmt.Sprintf("?p=%d&_t=%s", pid, token), 200, "text/html; charset=utf-8", "另一套风格")
		get(tokenClient, base+"引擎.js", 200, "application/javascript; charset=utf-8", "window.renderEngine")
		get(tokenClient, base+"公共.css", 200, "text/css; charset=utf-8", ".screen")
		for _, name := range []string{"../详情页模板/引擎.js", "%2e%2e%2f详情页模板%2f引擎.js", "子目录/引擎.js", "子目录/另一套.html", "子目录%2f引擎.js", `子目录%5c引擎.js`, "私有.txt", "两个..点.js", "不存在.js"} {
			get(anonymous, base+name+"?_t="+token, 404, "", "")
		}
	}
	for name, content := range map[string]string{"内容.js": "window.PRODUCT = ", "主图.js": "window.MAIN = "} {
		path := fmt.Sprintf("/render/详情页模板/商品/%d/%s", pid, name)
		get(f.client, path, 200, "application/javascript; charset=utf-8", content)
		get(anonymous, path+"?_t="+token, 200, "application/javascript; charset=utf-8", content)
		get(anonymous, path, 401, "", "")
	}
	photo := fmt.Sprintf("/render/详情页模板/商品/%d/照片/B1.jpg", pid)
	get(f.client, photo, 200, "", "")
	get(anonymous, photo+"?_t="+token, 200, "", "")
	get(anonymous, photo, 401, "", "")

	f.freshClient()
	f.login(t, "photographer")
	for _, dir := range []string{"主图模板", "详情页模板"} {
		base := "/render/" + dir + "/"
		get(f.client, base+"引擎.js", 200, "application/javascript; charset=utf-8", "window.renderEngine")
		get(f.client, base+"公共.css", 200, "text/css; charset=utf-8", ".screen")
		get(f.client, base+fmt.Sprintf("另一套.html?p=%d", pid), 403, "", "")
		get(f.client, base+fmt.Sprintf("另一套.html?p=%d", otherListing), 404, "", "")
	}
	get(f.client, fmt.Sprintf("/render/详情页模板/商品/%d/内容.js", pid), 403, "", "")
	f.freshClient()
	f.login(t, "unassigned")
	get(f.client, fmt.Sprintf("/render/详情页模板/另一套.html?p=%d", pid), 404, "", "")
	get(f.client, fmt.Sprintf("/render/详情页模板/商品/%d/主图.js", pid), 404, "", "")
}
