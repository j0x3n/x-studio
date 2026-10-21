package accounts

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"xstudio/internal/core"
)

func (a *Module) permissions(w http.ResponseWriter, r *http.Request) {
	core.WriteJSON(w, 200, core.Permissions)
}
func (a *Module) visibleShops(u core.User) ([]core.Object, error) {
	q := "SELECT id,name,platform,site,currency,language,settings,archived,created_at FROM shops"
	args := []any{}
	if u.Role.Key != "admin" {
		q += " WHERE id IN (SELECT shop_id FROM user_shops WHERE user_id=?)"
		args = append(args, u.ID)
		if u.Role.Scope == "shares" {
			q += " AND 0"
		}
	}
	return core.QueryObjects(a.DB, q+" ORDER BY id", args...)
}
func (a *Module) me(w http.ResponseWriter, r *http.Request) {
	u := core.CurrentUser(r)
	shops, e := a.visibleShops(u)
	if e != nil {
		core.Fail(w, 500, "读取店铺失败")
		return
	}
	ps := u.Role.Perms
	if u.Role.Key == "admin" {
		ps = []string{}
		for _, p := range core.Permissions {
			ps = append(ps, p.Key)
		}
	}
	core.WriteJSON(w, 200, core.Object{"user": core.Object{"id": u.ID, "username": u.Username, "display_name": u.DisplayName}, "role": u.Role, "perms": ps, "shops": shops})
}
func (a *Module) roles(w http.ResponseWriter, r *http.Request) {
	items, e := core.QueryObjects(a.DB, "SELECT * FROM roles ORDER BY id")
	if e != nil {
		core.Fail(w, 500, "读取角色失败")
		return
	}
	core.PageItems(w, r, items)
}
func validRole(v core.Role) bool {
	if v.Key == "" || v.Key == "admin" || len(v.Key) > 80 || strings.TrimSpace(v.Name) == "" || utf8.RuneCountInString(v.Name) > 120 || (v.Scope != "shops" && v.Scope != "shares") {
		return false
	}
	for _, p := range v.Perms {
		if !core.KnownPermission(p) {
			return false
		}
	}
	return true
}
func (a *Module) createRole(w http.ResponseWriter, r *http.Request) {
	var v core.Role
	if !core.Decode(w, r, &v) {
		return
	}
	if !validRole(v) {
		core.Fail(w, 400, "角色名称、数据范围或权限不正确")
		return
	}
	if v.Perms == nil {
		v.Perms = []string{}
	}
	res, e := a.DB.Exec("INSERT INTO roles(key,name,scope,perms,created_at) VALUES(?,?,?,?,?)", v.Key, v.Name, v.Scope, core.JS(v.Perms), core.Now())
	if e != nil {
		core.Fail(w, 409, "角色标识已存在")
		return
	}
	v.ID, _ = res.LastInsertId()
	a.Audit(r, "role.create", "role", v.ID, v)
	core.WriteJSON(w, 201, core.Object{"role": v})
}
func (a *Module) patchRole(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	items, e := core.QueryObjects(a.DB, "SELECT * FROM roles WHERE id=?", id)
	if e != nil || len(items) == 0 {
		core.Fail(w, 404, "角色不存在")
		return
	}
	b, _ := json.Marshal(items[0])
	var v core.Role
	_ = json.Unmarshal(b, &v)
	if v.Key == "admin" {
		core.Fail(w, 403, "管理员角色不能修改")
		return
	}
	var in map[string]json.RawMessage
	if !core.Decode(w, r, &in) {
		return
	}
	for k, x := range in {
		var e error
		switch k {
		case "name":
			e = json.Unmarshal(x, &v.Name)
		case "scope":
			e = json.Unmarshal(x, &v.Scope)
		case "perms":
			e = json.Unmarshal(x, &v.Perms)
		default:
			e = fmt.Errorf("不支持的角色字段")
		}
		if e != nil {
			core.Fail(w, 400, "角色字段不正确")
			return
		}
	}
	if !validRole(v) {
		core.Fail(w, 400, "角色名称、数据范围或权限不正确")
		return
	}
	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "保存角色失败")
		return
	}
	defer tx.Rollback()
	if _, e = tx.Exec("UPDATE roles SET name=?,scope=?,perms=? WHERE id=?", v.Name, v.Scope, core.JS(v.Perms), id); e == nil {
		_, e = tx.Exec("UPDATE users SET version=version+1 WHERE role_id=?", id)
	}
	if e == nil {
		e = core.AuditTx(tx, r, "role.update", "role", id, core.Object{"fields": keys(in)})
	}
	if e != nil || tx.Commit() != nil {
		core.Fail(w, 500, "保存角色失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"role": v})
}
func (a *Module) deleteRole(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	var builtin bool
	var n int
	if a.DB.QueryRow("SELECT builtin FROM roles WHERE id=?", id).Scan(&builtin) != nil {
		core.Fail(w, 404, "角色不存在")
		return
	}
	if builtin {
		core.Fail(w, 403, "预置角色不能删除")
		return
	}
	_ = a.DB.QueryRow("SELECT count(*) FROM users WHERE role_id=?", id).Scan(&n)
	if n > 0 {
		core.Fail(w, 409, "角色仍有用户，不能删除")
		return
	}
	if _, e := a.DB.Exec("DELETE FROM roles WHERE id=?", id); e != nil {
		core.Fail(w, 500, "删除角色失败")
		return
	}
	a.Audit(r, "role.delete", "role", id, core.Object{})
	core.WriteJSON(w, 200, core.Object{})
}
func keys(in map[string]json.RawMessage) []string {
	ks := []string{}
	for k := range in {
		ks = append(ks, k)
	}
	return ks
}
func (a *Module) users(w http.ResponseWriter, r *http.Request) {
	items, e := core.QueryObjects(a.DB, "SELECT id,username,display_name,role_id,created_at,disabled FROM users ORDER BY id")
	if e != nil {
		core.Fail(w, 500, "读取账号失败")
		return
	}
	for _, u := range items {
		shops, e := core.QueryObjects(a.DB, "SELECT shop_id FROM user_shops WHERE user_id=? ORDER BY shop_id", u["id"])
		if e != nil {
			core.Fail(w, 500, "读取分店失败")
			return
		}
		ids := []any{}
		for _, s := range shops {
			ids = append(ids, s["shop_id"])
		}
		u["shop_ids"] = ids
		q, _ := core.QueryObjects(a.DB, "SELECT month_images,month_text FROM quotas WHERE user_id=?", u["id"])
		u["quota"] = core.Object{"month_images": nil, "month_text": nil}
		if len(q) > 0 {
			u["quota"] = q[0]
		}
	}
	core.PageItems(w, r, items)
}

type userInput struct {
	Username    string  `json:"username"`
	Password    string  `json:"password"`
	DisplayName string  `json:"display_name"`
	RoleID      int64   `json:"role_id"`
	Disabled    bool    `json:"disabled"`
	ShopIDs     []int64 `json:"shop_ids"`
}

func validateShops(tx *sql.Tx, ids []int64, scope string) error {
	if scope == "shares" && len(ids) > 0 {
		return fmt.Errorf("分享范围的角色不能分配店铺")
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		var n int
		if seen[id] {
			return fmt.Errorf("店铺不能重复")
		}
		seen[id] = true
		if e := tx.QueryRow("SELECT count(*) FROM shops WHERE id=? AND archived=0", id).Scan(&n); e != nil || n == 0 {
			return fmt.Errorf("店铺不存在或已归档")
		}
	}
	return nil
}
func assignShops(tx *sql.Tx, uid int64, ids []int64) error {
	if _, e := tx.Exec("DELETE FROM user_shops WHERE user_id=?", uid); e != nil {
		return e
	}
	for _, id := range ids {
		if _, e := tx.Exec("INSERT INTO user_shops VALUES(?,?)", uid, id); e != nil {
			return e
		}
	}
	return nil
}
func (a *Module) createUser(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	var in userInput
	if !core.Decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Username) == "" || len(in.Username) > 120 || len(in.Password) < 8 || len(in.Password) > 72 {
		core.Fail(w, 400, "用户名或密码不符合要求（密码 8–72 字节）")
		return
	}
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	if utf8.RuneCountInString(in.DisplayName) > 120 {
		core.Fail(w, 400, "显示名称不能超过 120 字")
		return
	}
	defaults := a.Setting("quota_default")
	h, e := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if e != nil {
		core.Fail(w, 400, "密码不符合要求")
		return
	}
	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "创建账号失败")
		return
	}
	defer tx.Rollback()
	var scope, key string
	if tx.QueryRow("SELECT scope,key FROM roles WHERE id=?", in.RoleID).Scan(&scope, &key) != nil {
		core.Fail(w, 400, "角色不存在")
		return
	}
	if e = validateShops(tx, in.ShopIDs, scope); e != nil {
		core.Fail(w, 400, e.Error())
		return
	}
	res, e := tx.Exec("INSERT INTO users(username,password_hash,display_name,role_id,created_at,disabled) VALUES(?,?,?,?,?,?)", in.Username, h, in.DisplayName, in.RoleID, core.Now(), in.Disabled)
	if e != nil {
		core.Fail(w, 409, "用户名已存在")
		return
	}
	id, _ := res.LastInsertId()
	if e = assignShops(tx, id, in.ShopIDs); e == nil {
		var images, text any
		if key != "admin" {
			images = defaults["month_images"]
			text = defaults["month_text"]
		}
		_, e = tx.Exec("INSERT INTO quotas VALUES(?,?,?)", id, images, text)
	}
	if e == nil {
		e = core.AuditTx(tx, r, "user.create", "user", id, core.Object{"username": in.Username, "role_id": in.RoleID, "shop_ids": in.ShopIDs})
	}
	if e != nil || tx.Commit() != nil {
		core.Fail(w, 500, "创建账号失败")
		return
	}
	u, _ := a.User(id)
	core.WriteJSON(w, 201, core.Object{"user": u})
}
func (a *Module) patchUser(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	u, e := a.User(id)
	if e != nil {
		core.Fail(w, 404, "账号不存在")
		return
	}
	var in map[string]json.RawMessage
	if !core.Decode(w, r, &in) {
		return
	}
	var shops []int64
	for k, v := range in {
		switch k {
		case "display_name":
			e = json.Unmarshal(v, &u.DisplayName)
			if len([]rune(u.DisplayName)) > 120 {
				e = fmt.Errorf("显示名称过长")
			}
		case "password":
			var s string
			e = json.Unmarshal(v, &s)
			if e == nil && (len(s) < 8 || len(s) > 72) {
				e = fmt.Errorf("密码必须是 8–72 字节")
			}
			if e == nil {
				u.Hash, e = bcrypt.GenerateFromPassword([]byte(s), bcrypt.DefaultCost)
			}
		case "role_id":
			e = json.Unmarshal(v, &u.RoleID)
		case "disabled":
			e = json.Unmarshal(v, &u.Disabled)
		case "shop_ids":
			e = json.Unmarshal(v, &shops)
		default:
			e = fmt.Errorf("不支持的账号字段")
		}
		if e != nil {
			core.Fail(w, 400, "账号字段不正确："+k)
			return
		}
	}
	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "保存账号失败")
		return
	}
	defer tx.Rollback()
	var scope, key string
	if tx.QueryRow("SELECT scope,key FROM roles WHERE id=?", u.RoleID).Scan(&scope, &key) != nil {
		core.Fail(w, 400, "角色不存在")
		return
	}
	if key != "admin" || u.Disabled {
		var n int
		_ = tx.QueryRow("SELECT count(*) FROM users u JOIN roles r ON u.role_id=r.id WHERE r.key='admin' AND u.disabled=0 AND u.id<>?", id).Scan(&n)
		if n == 0 {
			core.Fail(w, 409, "必须保留至少一位启用的管理员")
			return
		}
	}
	if _, ok := in["shop_ids"]; ok {
		if e = validateShops(tx, shops, scope); e == nil {
			e = assignShops(tx, id, shops)
		}
		if e != nil {
			core.Fail(w, 400, e.Error())
			return
		}
	} else if scope == "shares" {
		if e = assignShops(tx, id, nil); e != nil {
			core.Fail(w, 500, "更新分店失败")
			return
		}
	}
	_, e = tx.Exec("UPDATE users SET display_name=?,password_hash=?,role_id=?,disabled=?,version=version+1 WHERE id=?", u.DisplayName, u.Hash, u.RoleID, u.Disabled, id)
	if e == nil {
		_, e = tx.Exec("DELETE FROM sessions WHERE user_id=?", id)
	}
	if e == nil {
		e = core.AuditTx(tx, r, "user.update", "user", id, core.Object{"fields": keys(in)})
	}
	if e == nil && in["shop_ids"] != nil {
		e = core.AuditTx(tx, r, "user.shops", "user", id, core.Object{"shop_ids": shops})
	}
	if e != nil || tx.Commit() != nil {
		core.Fail(w, 500, "保存账号失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"user": u})
}
