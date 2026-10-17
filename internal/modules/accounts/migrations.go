package accounts

import (
	"database/sql"
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
	}}}
}
