package creative

import "database/sql"

func migratePlans(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE creative_plans(id INTEGER PRIMARY KEY,name TEXT NOT NULL,note TEXT NOT NULL DEFAULT '',categories TEXT NOT NULL DEFAULT '[]',template_set TEXT NOT NULL DEFAULT '{}',models TEXT NOT NULL DEFAULT '{}',scene_id INTEGER,detail_layout TEXT NOT NULL DEFAULT '',archived INTEGER NOT NULL DEFAULT 0,created_by INTEGER NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE INDEX creative_plans_archived ON creative_plans(archived);`)
	return err
}
