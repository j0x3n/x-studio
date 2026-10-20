package core

import (
	"net/http"
	"net/http/httptest"
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

func TestPersonalTokenOnlyWorksOnTokenRoutes(t *testing.T) {
	a := &Core{mux: http.NewServeMux()}
	a.TokenAuth = func(token, scope string) (User, error) {
		if token != "good" || scope != MarketIngest {
			return User{}, http.ErrNoCookie
		}
		return User{ID: 7, Role: Role{Key: "admin"}}, nil
	}
	r := &Router{Core: a}
	r.Handle("POST /api/market/ingest", MarketImport, func(w http.ResponseWriter, q *http.Request) { WriteJSON(w, 200, Object{"user": CurrentUser(q).ID}) })
	r.Handle("POST /api/other", LoggedIn, func(w http.ResponseWriter, q *http.Request) { WriteJSON(w, 200, Object{}) })
	call := func(path, token string) int {
		req := httptest.NewRequest("POST", path, nil)
		req.Header.Set("X-Requested-With", "fetch")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		a.serve(w, req)
		return w.Code
	}
	if c := call("/api/market/ingest", "good"); c != 200 {
		t.Fatal("token route rejected a valid token", c)
	}
	if c := call("/api/market/ingest", "bad"); c != 401 {
		t.Fatal("token route accepted a bad token", c)
	}
	if c := call("/api/other", "good"); c != 401 {
		t.Fatal("token worked outside TokenRoutes", c)
	}
}
