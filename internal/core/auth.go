package core

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (a *Core) sign(s string) string {
	h := hmac.New(sha256.New, []byte(a.Cfg.SessionSecret))
	_, _ = h.Write([]byte(s))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (a *Core) validSigned(s string) (string, bool) {
	p := strings.LastIndex(s, ".")
	if p < 0 {
		return "", false
	}
	v := s[:p]
	return v, hmac.Equal([]byte(a.sign(v)), []byte(s[p+1:]))
}
func (a *Core) authenticate(r *http.Request) (User, error) {
	c, e := r.Cookie("session")
	if e != nil {
		return User{}, e
	}
	raw, ok := a.validSigned(c.Value)
	if !ok {
		return User{}, errors.New("会话无效")
	}
	return ScanUser(a.DB.QueryRow(`SELECT u.id,u.username,u.password_hash,u.display_name,u.role_id,u.created_at,u.disabled,u.version,r.id,r.key,r.name,r.builtin,r.scope,r.perms,r.created_at FROM users u JOIN roles r ON r.id=u.role_id JOIN sessions s ON u.id=s.user_id WHERE s.token=? AND s.expires>? AND s.version=u.version AND u.disabled=0`, a.sign("store:"+raw), time.Now().Unix()))
}
func (a *Core) Login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !Decode(w, r, &in) {
		return
	}
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		ip = r.RemoteAddr
	}
	key := ip + "\x00" + in.Username
	// Hold the limiter lock through password verification so simultaneous failures
	// cannot all pass the ten-attempt boundary.
	a.limitMu.Lock()
	defer a.limitMu.Unlock()
	t := time.Now()
	for k, v := range a.failures {
		j := 0
		for _, ts := range v {
			if t.Sub(ts) < 5*time.Minute {
				v[j] = ts
				j++
			}
		}
		if j == 0 {
			delete(a.failures, k)
		} else {
			a.failures[k] = v[:j]
		}
	}
	if len(a.failures[key]) >= 10 {
		Fail(w, 429, "登录失败次数过多，请 5 分钟后再试")
		return
	}
	u, e := ScanUser(a.DB.QueryRow(UserQuery+" WHERE u.username=?", in.Username))
	hash := u.Hash
	if e != nil {
		hash = a.dummyHash
	}
	passErr := bcrypt.CompareHashAndPassword(hash, []byte(in.Password))
	if e != nil || u.Disabled || passErr != nil {
		a.failures[key] = append(a.failures[key], t)
		a.AuditUser(u.ID, ip, "login.failed", "user", u.ID, Object{"username": in.Username})
		Fail(w, 401, "用户名或密码错误，或账号已禁用")
		return
	}
	delete(a.failures, key)
	b := make([]byte, 32)
	if _, e = rand.Read(b); e != nil {
		Fail(w, 500, "无法创建会话")
		return
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	expires := t.Add(24 * time.Hour)
	_, _ = a.DB.Exec("DELETE FROM sessions WHERE expires<=?", t.Unix())
	if _, e = a.DB.Exec("INSERT INTO sessions(token,user_id,version,expires) VALUES(?,?,?,?)", a.sign("store:"+raw), u.ID, u.Version, expires.Unix()); e != nil {
		Fail(w, 500, "无法保存会话")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: raw + "." + a.sign(raw), Path: "/", HttpOnly: true, Secure: a.Cfg.SecureCookie || r.TLS != nil, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: 86400})
	a.AuditUser(u.ID, ip, "login.success", "user", u.ID, Object{})
	WriteJSON(w, 200, Object{"user": u})
}
func (a *Core) Logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie("session"); e == nil {
		if raw, ok := a.validSigned(c.Value); ok {
			_, _ = a.DB.Exec("DELETE FROM sessions WHERE token=?", a.sign("store:"+raw))
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Path: "/", HttpOnly: true, Secure: a.Cfg.SecureCookie || r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	WriteJSON(w, 200, Object{})
}
func (a *Core) RenderToken() string {
	v := "render:" + strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(), 10)
	return v + "." + a.sign(v)
}
func (a *Core) validRender(v string) bool {
	raw, ok := a.validSigned(v)
	if !ok || !strings.HasPrefix(raw, "render:") {
		return false
	}
	ts, e := strconv.ParseInt(strings.TrimPrefix(raw, "render:"), 10, 64)
	return e == nil && ts > time.Now().Unix()
}
func (a *Core) RenderAuth(w http.ResponseWriter, r *http.Request) bool {
	if v := r.URL.Query().Get("_t"); v != "" && a.validRender(v) {
		// Relative scripts, photos and the logo have no query token; a restricted
		// cookie carries the short-lived render credential to those subrequests.
		http.SetCookie(w, &http.Cookie{Name: "render_session", Value: v, Path: "/render/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300})
		return true
	}
	c, e := r.Cookie("render_session")
	return e == nil && a.validRender(c.Value)
}
