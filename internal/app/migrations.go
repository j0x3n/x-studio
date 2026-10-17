package app

import (
	"database/sql"
	"fmt"
)

// ALTER TABLE runs for both existing databases and fresh installations. Explicit
// scan columns keep partial migrations and pre-existing column order compatible.
func migratePhotos(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query("PRAGMA table_info(photos)")
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var def any
		if err = rows.Scan(&cid, &name, &kind, &notNull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{{"enhanced_file", "TEXT NOT NULL DEFAULT ''"}, {"ai_enhanced", "INTEGER NOT NULL DEFAULT 0"}, {"ai_generated", "INTEGER NOT NULL DEFAULT 0"}, {"source", "TEXT NOT NULL DEFAULT 'original'"}} {
		if !columns[column.name] {
			if _, err = tx.Exec("ALTER TABLE photos ADD COLUMN " + column.name + " " + column.definition); err != nil {
				return fmt.Errorf("升级照片字段 %s 失败：%w", column.name, err)
			}
		}
	}
	return tx.Commit()
}
