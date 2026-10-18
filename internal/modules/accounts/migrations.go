package accounts

import (
	"database/sql"
	"encoding/json"
	"xstudio/internal/core"
)

const usersTable = `CREATE TABLE users_new(id INTEGER PRIMARY KEY,username TEXT NOT NULL UNIQUE,password_hash BLOB NOT NULL,display_name TEXT NOT NULL DEFAULT '',role_id INTEGER NOT NULL REFERENCES roles(id),created_at TEXT NOT NULL,disabled INTEGER NOT NULL DEFAULT 0,version INTEGER NOT NULL DEFAULT 1);`

func (a *Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		if _, e := tx.Exec(`CREATE TABLE IF NOT EXISTS roles(id INTEGER PRIMARY KEY,key TEXT NOT NULL UNIQUE,name TEXT NOT NULL,builtin INTEGER NOT NULL DEFAULT 0,scope TEXT NOT NULL CHECK(scope IN ('all','shops','shares')),perms TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS shops(id INTEGER PRIMARY KEY,name TEXT NOT NULL,platform TEXT NOT NULL,language TEXT NOT NULL,settings TEXT NOT NULL DEFAULT '{}',archived INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS user_shops(user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,shop_id INTEGER NOT NULL REFERENCES shops(id),PRIMARY KEY(user_id,shop_id));
CREATE TABLE IF NOT EXISTS shares(id INTEGER PRIMARY KEY,product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,can_upload INTEGER NOT NULL DEFAULT 0,created_by INTEGER NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,UNIQUE(product_id,user_id));`); e != nil {
			return e
		}
		roles := []core.Role{
			{Key: "admin", Name: "管理员", Scope: "all", Perms: []string{}},
			{Key: "operator", Name: "运营", Scope: "shops", Perms: []string{core.ProductView, core.ProductCost, core.ProductEdit, core.ProductDeleteOwn, core.AssetUpload, core.AssetUpscale, core.AssetProcess, core.AssetDeleteOwn, core.CopyView, core.CopyEdit, core.RenderRun, core.ExportRun, core.ShareManage}},
			{Key: "designer", Name: "美工", Scope: "shops", Perms: []string{core.ProductView, core.AssetUpload, core.AssetUpscale, core.AssetProcess, core.AssetDeleteOwn, core.CopyView, core.TemplateManage, core.RenderRun, core.ExportRun}},
			{Key: "photographer", Name: "摄影", Scope: "shops", Perms: []string{core.ProductView, core.AssetUpload, core.AssetUpscale}},
			{Key: "collaborator", Name: "外部协作者", Scope: "shares", Perms: []string{core.ProductView, core.AssetUpload}},
		}
		for _, r := range roles {
			if _, e := tx.Exec("INSERT INTO roles(key,name,builtin,scope,perms,created_at) VALUES(?,?,1,?,?,?)", r.Key, r.Name, r.Scope, core.JS(r.Perms), core.Now()); e != nil {
				return e
			}
		}
		cols, e := core.Columns(tx, "users")
		if e != nil {
			return e
		}
		if _, e = tx.Exec(usersTable); e != nil {
			return e
		}
		if cols["role"] {
			if _, e = tx.Exec(`INSERT INTO users_new(id,username,password_hash,display_name,role_id,created_at,disabled,version) SELECT id,username,password_hash,username,(SELECT id FROM roles WHERE key=CASE users.role WHEN 'admin' THEN 'admin' ELSE 'operator' END),created_at,disabled,version FROM users;DROP TABLE users;`); e != nil {
				return e
			}
		}
		if _, e = tx.Exec("ALTER TABLE users_new RENAME TO users; INSERT INTO shops(name,platform,language,created_at) VALUES('淘宝店','taobao','zh-CN',?)", core.Now()); e != nil {
			return e
		}
		if cols["role"] {
			_, e = tx.Exec("INSERT INTO user_shops(user_id,shop_id) SELECT u.id,s.id FROM users u JOIN roles r ON r.id=u.role_id JOIN shops s WHERE r.key='operator'")
		}
		return e
	}}, {Version: 2, Up: func(tx *sql.Tx) error {
		// 第一期并行阶段新增"审核出图"权限点，默认给运营；已改过的角色只追加，不覆盖
		var raw string
		if e := tx.QueryRow("SELECT perms FROM roles WHERE key='operator' AND builtin=1").Scan(&raw); e != nil {
			return nil
		}
		var perms []string
		_ = json.Unmarshal([]byte(raw), &perms)
		for _, p := range perms {
			if p == core.RenderReview {
				return nil
			}
		}
		_, e := tx.Exec("UPDATE roles SET perms=? WHERE key='operator' AND builtin=1", core.JS(append(perms, core.RenderReview)))
		return e
	}}, {Version: 3, Up: func(tx *sql.Tx) error {
		if e := appendBuiltinPerms(tx, "operator", core.TaskView, core.TaskManage); e != nil {
			return e
		}
		if e := appendBuiltinPerms(tx, "designer", core.TaskView); e != nil {
			return e
		}
		return appendBuiltinPerms(tx, "photographer", core.TaskView)
	}}}
}

func appendBuiltinPerms(tx *sql.Tx, role string, add ...string) error {
	var raw string
	if e := tx.QueryRow("SELECT perms FROM roles WHERE key=? AND builtin=1", role).Scan(&raw); e != nil {
		return nil
	}
	var perms []string
	_ = json.Unmarshal([]byte(raw), &perms)
	have := map[string]bool{}
	for _, p := range perms {
		have[p] = true
	}
	changed := false
	for _, p := range add {
		if !have[p] {
			perms = append(perms, p)
			have[p] = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	_, e := tx.Exec("UPDATE roles SET perms=? WHERE key=? AND builtin=1", core.JS(perms), role)
	return e
}
