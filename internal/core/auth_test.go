package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestExpiredLoginFailuresDoNotBlockFreshAttempt(t *testing.T) {
	a, e := New(Config{DataDir: t.TempDir(), SessionSecret: strings.Repeat("s", 32)}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	// A minimal users schema is sufficient because expired failures must reach
	// password verification (which returns the standard invalid-login response).
	_, e = a.DB.Exec(`CREATE TABLE users(id INTEGER,username TEXT,password_hash BLOB,display_name TEXT,role_id INTEGER,created_at TEXT,disabled INTEGER,version INTEGER);CREATE TABLE roles(id INTEGER,key TEXT,name TEXT,builtin INTEGER,scope TEXT,perms TEXT,created_at TEXT);CREATE TABLE audit_log(id INTEGER PRIMARY KEY,at TEXT,user_id INTEGER,action TEXT,target_type TEXT,target_id INTEGER,detail TEXT,ip TEXT);`)
	if e != nil {
		t.Fatal(e)
	}
	a.dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy"), bcrypt.MinCost)
	key := "127.0.0.1\x00admin"
	for i := 0; i < 10; i++ {
		a.failures[key] = append(a.failures[key], time.Now().Add(-6*time.Minute))
	}
	r := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	a.Login(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("过期限流记录仍拦截登录", w.Code)
	}
}
