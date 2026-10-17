package core

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

func (a *Core) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		Fail(w, 405, "请求方法不正确")
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}
	if !fs.ValidPath(p) || strings.ContainsAny(p, "\\:") {
		Fail(w, 404, "页面不存在")
		return
	}
	b, e := fs.ReadFile(a.web, p)
	if e != nil {
		if strings.HasPrefix(p, "assets/") {
			Fail(w, 404, "静态文件不存在")
			return
		}
		p = "index.html"
		b, e = fs.ReadFile(a.web, p)
	}
	if e != nil {
		b = []byte("<!doctype html><meta charset=utf-8><p>前端未构建</p>")
		p = "index.html"
	}
	ct := mime.TypeByExtension(path.Ext(p))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	if r.Method != "HEAD" {
		_, _ = w.Write(b)
	}
}
