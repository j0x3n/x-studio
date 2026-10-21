package market

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"xstudio/internal/core"
)

func hashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func (m *Module) authenticateToken(token, scope string) (core.User, error) {
	token = strings.TrimSpace(token)
	if token == "" || scope == "" {
		return core.User{}, errors.New("令牌无效")
	}
	var userID int64
	var scopesRaw, revoked string
	err := m.DB.QueryRow("SELECT user_id, scopes, revoked_at FROM user_api_tokens WHERE token_hash=?", hashToken(token)).Scan(&userID, &scopesRaw, &revoked)
	if err != nil || revoked != "" {
		return core.User{}, errors.New("令牌无效")
	}
	var scopes []string
	if json.Unmarshal([]byte(scopesRaw), &scopes) != nil {
		return core.User{}, errors.New("令牌无效")
	}
	found := false
	for _, s := range scopes {
		if s == scope {
			found = true
			break
		}
	}
	if !found {
		return core.User{}, errors.New("令牌范围不符")
	}
	u, err := m.User(userID)
	if err != nil || u.Disabled {
		return core.User{}, errors.New("用户不可用")
	}
	return u, nil
}

func (m *Module) touchToken(r *http.Request) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	if token == "" {
		return
	}
	_, _ = m.DB.Exec("UPDATE user_api_tokens SET last_used_at=? WHERE token_hash=? AND revoked_at=''", core.Now(), hashToken(token))
}

type tokenView struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Scopes     []string `json:"scopes"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt string   `json:"last_used_at"`
	RevokedAt  string   `json:"revoked_at"`
}

func (m *Module) listTokens(w http.ResponseWriter, r *http.Request) {
	rows, err := m.DB.Query("SELECT id, name, prefix, scopes, created_at, last_used_at, revoked_at FROM user_api_tokens WHERE user_id=? ORDER BY id DESC", core.CurrentUser(r).ID)
	if err != nil {
		core.Fail(w, 500, "读取令牌失败")
		return
	}
	defer rows.Close()
	items := []tokenView{}
	for rows.Next() {
		var v tokenView
		var scopes string
		if err = rows.Scan(&v.ID, &v.Name, &v.Prefix, &scopes, &v.CreatedAt, &v.LastUsedAt, &v.RevokedAt); err != nil {
			core.Fail(w, 500, "读取令牌失败")
			return
		}
		_ = json.Unmarshal([]byte(scopes), &v.Scopes)
		if v.Scopes == nil {
			v.Scopes = []string{}
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		core.Fail(w, 500, "读取令牌失败")
		return
	}
	core.PageItems(w, r, items)
}

func (m *Module) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		core.Fail(w, 400, "请填写 1–80 字的令牌名称")
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		core.Fail(w, 500, "生成令牌失败")
		return
	}
	plain := "xst_" + base64.RawURLEncoding.EncodeToString(buf)
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int
	if err := m.DB.QueryRow("SELECT count(*) FROM user_api_tokens WHERE user_id=? AND revoked_at=''", core.CurrentUser(r).ID).Scan(&n); err != nil {
		core.Fail(w, 500, "生成令牌失败")
		return
	}
	if n >= 10 {
		core.Fail(w, 400, "有效令牌最多 10 个，请先吊销不用的")
		return
	}
	res, err := m.DB.Exec("INSERT INTO user_api_tokens(user_id, name, prefix, token_hash, scopes, created_at) VALUES(?,?,?,?,?,?)", core.CurrentUser(r).ID, name, plain[:8], hashToken(plain), `["market.ingest"]`, core.Now())
	if err != nil {
		core.Fail(w, 500, "生成令牌失败")
		return
	}
	id, _ := res.LastInsertId()
	m.Audit(r, "market.token.create", "token", id, core.Object{"name": name, "prefix": plain[:8]})
	core.WriteJSON(w, 201, core.Object{"id": id, "token": plain})
}

func (m *Module) revokeToken(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	var owner int64
	var revoked string
	err := m.DB.QueryRow("SELECT user_id, revoked_at FROM user_api_tokens WHERE id=?", id).Scan(&owner, &revoked)
	if err != nil || id == 0 {
		core.Fail(w, 404, "令牌不存在")
		return
	}
	u := core.CurrentUser(r)
	if owner != u.ID && !u.Has(core.AdminUsers) {
		core.Fail(w, 403, "只能吊销自己的令牌")
		return
	}
	if revoked == "" {
		revoked = core.Now()
		if _, err = m.DB.Exec("UPDATE user_api_tokens SET revoked_at=? WHERE id=? AND revoked_at=''", revoked, id); err != nil {
			core.Fail(w, 500, "吊销令牌失败")
			return
		}
		m.Audit(r, "market.token.revoke", "token", id, core.Object{"user_id": owner})
	}
	core.WriteJSON(w, 200, core.Object{"id": id, "revoked_at": revoked})
}
