package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
)

type Object = map[string]any

func JS(v any) string { b, _ := json.Marshal(v); return string(b) }
func JSONObj(s string) Object {
	var v Object
	_ = json.Unmarshal([]byte(s), &v)
	if v == nil {
		v = Object{}
	}
	return v
}
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func Fail(w http.ResponseWriter, status int, s string) { WriteJSON(w, status, Object{"error": s}) }
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	if e := d.Decode(v); e != nil {
		Fail(w, 400, "JSON 格式或字段类型不正确，或请求过大")
		return false
	}
	if e := d.Decode(new(any)); e != io.EOF {
		Fail(w, 400, "只能提交一个 JSON 值")
		return false
	}
	return true
}
func ID(r *http.Request, key string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(key), 10, 64)
	if n < 1 {
		return 0
	}
	return n
}
func Page(r *http.Request) (int, int) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	s, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if p < 1 {
		p = 1
	}
	if p > 1000000 {
		p = 1000000
	}
	if s < 1 {
		s = 50
	}
	if s > 200 {
		s = 200
	}
	return (p - 1) * s, s
}
func PageItems[T any](w http.ResponseWriter, r *http.Request, items []T) {
	total := len(items)
	offset, size := Page(r)
	if offset > total {
		offset = total
	}
	end := min(offset+size, total)
	WriteJSON(w, 200, Object{"items": items[offset:end], "total": total})
}
func HasPrefix(s string, ps ...string) bool {
	for _, p := range ps {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

type userKey struct{}

func CurrentUser(r *http.Request) User { u, _ := r.Context().Value(userKey{}).(User); return u }

type Router struct {
	Core *Core
	err  error
}

const Public = "@public"
const LoggedIn = "@login"
const Scoped = "@scoped"

func (r *Router) Handle(pattern, perm string, h http.HandlerFunc) {
	if perm == "" || (!KnownPermission(perm) && perm != Public && perm != LoggedIn && perm != Scoped) {
		r.err = fmt.Errorf("路由未声明有效权限：%s", pattern)
		return
	}
	r.Core.mux.HandleFunc(pattern, func(w http.ResponseWriter, q *http.Request) {
		if perm != Public && perm != LoggedIn && perm != Scoped && !CurrentUser(q).Has(perm) {
			Fail(w, 403, "没有此操作的权限")
			return
		}
		h(w, q)
	})
}
func (r *Router) Raw(pattern string, h http.HandlerFunc) {
	if strings.Contains(pattern, "/api/") {
		r.err = fmt.Errorf("API 路由必须声明权限：%s", pattern)
		return
	}
	r.Core.mux.HandleFunc(pattern, h)
}
func (a *Core) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Cache-Control", "no-store")
	defer func() {
		if recover() != nil {
			log.Print("请求处理异常")
			Fail(w, 500, "服务器处理异常")
		}
	}()
	if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("X-Requested-With") != "fetch" {
		Fail(w, 403, "缺少 X-Requested-With: fetch 请求头")
		return
	}
	// Reject render traversal before ServeMux can redirect it to a valid file.
	if HasPrefix(r.URL.Path, "/render/") {
		for _, part := range strings.Split(r.URL.Path, "/") {
			if part == "." || part == ".." {
				Fail(w, 404, "渲染文件不存在")
				return
			}
		}
	}
	protected := HasPrefix(r.URL.Path, "/api/", "/files/", "/render/")
	if protected && !(r.URL.Path == "/api/login" && r.Method == "POST") {
		if HasPrefix(r.URL.Path, "/render/") && a.RenderAuth(w, r) {
			a.mux.ServeHTTP(w, r)
			return
		}
		u, e := a.authenticate(r)
		if e != nil {
			Fail(w, 401, "请先登录")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), userKey{}, u))
	}
	a.mux.ServeHTTP(w, r)
}
