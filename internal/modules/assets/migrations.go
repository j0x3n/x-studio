package assets

import (
	"database/sql"
	"xstudio/internal/core"
)

func (a *Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		cols, e := core.Columns(tx, "photos")
		if e != nil {
			return e
		}
		if _, e = tx.Exec(`CREATE TABLE assets(id INTEGER PRIMARY KEY,product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,file TEXT NOT NULL,orig_name TEXT NOT NULL,width INTEGER NOT NULL,height INTEGER NOT NULL,metrics TEXT NOT NULL DEFAULT '{}',code TEXT NOT NULL DEFAULT '',crop TEXT NOT NULL DEFAULT 'null',rotate INTEGER NOT NULL DEFAULT 0,white_bg INTEGER NOT NULL DEFAULT 0,ai_note TEXT NOT NULL DEFAULT '',retouch_log TEXT NOT NULL DEFAULT '{}',manual INTEGER NOT NULL DEFAULT 0,enhanced_file TEXT NOT NULL DEFAULT '',ai_enhanced INTEGER NOT NULL DEFAULT 0,ai_generated INTEGER NOT NULL DEFAULT 0,source TEXT NOT NULL DEFAULT 'original',parent_id INTEGER REFERENCES assets(id) ON DELETE SET NULL,kind TEXT NOT NULL DEFAULT 'photo',tags TEXT NOT NULL DEFAULT '[]',op TEXT NOT NULL DEFAULT '{}',created_by INTEGER NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,source_override TEXT NOT NULL DEFAULT '');
CREATE UNIQUE INDEX asset_code ON assets(product_id,code) WHERE code<>'';
CREATE INDEX asset_product ON assets(product_id);`); e != nil {
			return e
		}
		if len(cols) > 0 {
			if e = core.AddColumns(tx, "photos", [][2]string{{"manual", "INTEGER NOT NULL DEFAULT 0"}, {"enhanced_file", "TEXT NOT NULL DEFAULT ''"}, {"ai_enhanced", "INTEGER NOT NULL DEFAULT 0"}, {"ai_generated", "INTEGER NOT NULL DEFAULT 0"}}); e != nil {
				return e
			}
			_, e = tx.Exec(`INSERT INTO assets(id,product_id,file,orig_name,width,height,metrics,code,crop,rotate,white_bg,ai_note,retouch_log,manual,enhanced_file,ai_enhanced,ai_generated,source,created_by,created_at) SELECT ph.id,ph.product_id,ph.file,ph.orig_name,ph.width,ph.height,ph.metrics,ph.code,ph.crop,ph.rotate,ph.white_bg,ph.ai_note,ph.retouch_log,ph.manual,ph.enhanced_file,ph.ai_enhanced,ph.ai_generated,CASE WHEN ph.ai_generated=1 THEN 'ai_generated' WHEN ph.ai_enhanced=1 THEN 'ai_enhanced' ELSE 'original' END,p.created_by,p.created_at FROM photos ph JOIN products p ON p.id=ph.product_id;DROP TABLE photos;`)
		}
		return e
	}}, {Version: 2, Up: func(tx *sql.Tx) error {
		if err := core.AddColumns(tx, "assets", [][2]string{{"title", "TEXT NOT NULL DEFAULT ''"}, {"note", "TEXT NOT NULL DEFAULT ''"}, {"archived", "INTEGER NOT NULL DEFAULT 0"}, {"updated_at", "TEXT NOT NULL DEFAULT ''"}, {"phash", "TEXT NOT NULL DEFAULT ''"}}); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE assets SET updated_at=created_at WHERE updated_at='';
CREATE INDEX IF NOT EXISTS assets_created_at ON assets(created_at);
CREATE INDEX IF NOT EXISTS assets_source ON assets(source);
CREATE INDEX IF NOT EXISTS assets_parent_id ON assets(parent_id);
CREATE TABLE IF NOT EXISTS asset_presets(id INTEGER PRIMARY KEY,name TEXT NOT NULL,owner_id INTEGER NOT NULL REFERENCES users(id),shared INTEGER NOT NULL DEFAULT 0,params TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TRIGGER IF NOT EXISTS assets_updated_at_insert AFTER INSERT ON assets WHEN NEW.updated_at='' BEGIN UPDATE assets SET updated_at=NEW.created_at WHERE id=NEW.id; END;`)
		return err
	}}, {Version: 3, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE workbench_items(
 id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id),
 kind TEXT NOT NULL CHECK(kind IN ('input','result')), file TEXT NOT NULL,
 orig_name TEXT NOT NULL, width INTEGER NOT NULL, height INTEGER NOT NULL,
 phash TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT 'original',
 asset_id INTEGER, input_id INTEGER, op TEXT NOT NULL DEFAULT '{}',
 saved_asset_id INTEGER, created_at TEXT NOT NULL);
 CREATE INDEX workbench_user_created ON workbench_items(user_id,created_at);`)
		return err
	}}, {Version: 4, Up: modelMigration}, {Version: 5, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec("ALTER TABLE model_references ADD COLUMN deleted_at TEXT NOT NULL DEFAULT ''")
		return err
	}}, {Version: 6, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE asset_folders(id INTEGER PRIMARY KEY,parent_id INTEGER REFERENCES asset_folders(id) ON DELETE CASCADE,name TEXT NOT NULL,created_by INTEGER NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE UNIQUE INDEX asset_folders_name ON asset_folders(COALESCE(parent_id,0),name);
ALTER TABLE assets ADD COLUMN folder_id INTEGER;
CREATE INDEX assets_folder_id ON assets(folder_id);`)
		return err
	}}, {Version: 7, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE assets ADD COLUMN media TEXT NOT NULL DEFAULT 'image';
ALTER TABLE assets ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE assets ADD COLUMN poster_file TEXT NOT NULL DEFAULT '';
ALTER TABLE assets ADD COLUMN stale INTEGER NOT NULL DEFAULT 0;
CREATE INDEX assets_media_stale ON assets(media,stale);`)
		return err
	}}}
}
