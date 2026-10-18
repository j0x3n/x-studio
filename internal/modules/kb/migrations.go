package kb

import (
	"database/sql"
	"xstudio/internal/core"
)

func (*Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
CREATE TABLE kb_folders(id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, parent_id INTEGER REFERENCES kb_folders(id), sort INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE UNIQUE INDEX kb_folder_sibling_name ON kb_folders(COALESCE(parent_id,0),name);
CREATE TABLE kb_articles(id INTEGER PRIMARY KEY AUTOINCREMENT,folder_id INTEGER NOT NULL REFERENCES kb_folders(id),title TEXT NOT NULL,body TEXT NOT NULL DEFAULT '',summary TEXT NOT NULL DEFAULT '',tags TEXT NOT NULL DEFAULT '[]',pinned INTEGER NOT NULL DEFAULT 0,version INTEGER NOT NULL DEFAULT 1,created_by INTEGER NOT NULL,updated_by INTEGER NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,deleted_at TEXT);
CREATE INDEX kb_article_folder ON kb_articles(folder_id,deleted_at);
CREATE TABLE kb_links(article_id INTEGER NOT NULL REFERENCES kb_articles(id),target_type TEXT NOT NULL CHECK(target_type IN ('category','product','item')),target_id INTEGER NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(article_id,target_type,target_id));
CREATE INDEX kb_link_target ON kb_links(target_type,target_id);
CREATE TABLE kb_versions(id INTEGER PRIMARY KEY AUTOINCREMENT,article_id INTEGER NOT NULL REFERENCES kb_articles(id),version INTEGER NOT NULL,title TEXT NOT NULL,body TEXT NOT NULL,folder_id INTEGER NOT NULL,tags TEXT NOT NULL,note TEXT NOT NULL DEFAULT '',user_id INTEGER NOT NULL,created_at TEXT NOT NULL,UNIQUE(article_id,version));
CREATE TABLE kb_files(id INTEGER PRIMARY KEY AUTOINCREMENT,article_id INTEGER REFERENCES kb_articles(id),name TEXT NOT NULL,file TEXT UNIQUE NOT NULL,mime TEXT NOT NULL,size INTEGER NOT NULL,width INTEGER NOT NULL DEFAULT 0,height INTEGER NOT NULL DEFAULT 0,created_by INTEGER NOT NULL,created_at TEXT NOT NULL);
CREATE INDEX kb_file_article ON kb_files(article_id);
`)
		if err != nil {
			return err
		}
		for i, name := range []string{"鉴别", "工艺做法", "拍摄规范", "平台规则", "客服话术", "供应商资料"} {
			if _, err = tx.Exec("INSERT INTO kb_folders(name,sort,created_at,updated_at) VALUES(?,?,?,?)", name, i, core.Now(), core.Now()); err != nil {
				return err
			}
		}
		return nil
	}}}
}
