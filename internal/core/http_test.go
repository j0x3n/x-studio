package core

import (
	"net/http"
	"testing"
)

func TestRouterRejectsUndeclaredAndUnknownAPIPermissions(t *testing.T) {
	for _, p := range []string{"", "unknown.permission"} {
		r := &Router{Core: &Core{mux: http.NewServeMux()}}
		r.Handle("GET /api/test", p, func(http.ResponseWriter, *http.Request) {})
		if r.err == nil {
			t.Fatal("未声明权限的接口被注册", p)
		}
	}
	r := &Router{Core: &Core{mux: http.NewServeMux()}}
	r.Raw("GET /api/test", func(http.ResponseWriter, *http.Request) {})
	if r.err == nil {
		t.Fatal("绕过权限声明的 API 被注册")
	}
}
