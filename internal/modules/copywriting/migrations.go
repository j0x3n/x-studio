package copywriting

import (
	"database/sql"
	"xstudio/internal/core"
)

const copywritingSchema = `
CREATE TABLE IF NOT EXISTS copy_versions(
	id INTEGER PRIMARY KEY,
	listing_id INTEGER NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
	title TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '{}',
	main TEXT NOT NULL DEFAULT '{}',
	meta TEXT NOT NULL DEFAULT '{}',
	source TEXT NOT NULL CHECK(source IN ('ai_generate','ai_rewrite','manual','pipeline','restore')),
	note TEXT NOT NULL DEFAULT '',
	created_by INTEGER NOT NULL REFERENCES users(id),
	created_at TEXT NOT NULL,
	applied_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_copy_versions_listing ON copy_versions(listing_id, id DESC);

CREATE TABLE IF NOT EXISTS keywords(
	id INTEGER PRIMARY KEY,
	shop_id INTEGER REFERENCES shops(id) ON DELETE CASCADE,
	word TEXT NOT NULL,
	grp TEXT NOT NULL DEFAULT '',
	weight INTEGER NOT NULL DEFAULT 0,
	note TEXT NOT NULL DEFAULT '',
	created_by INTEGER NOT NULL REFERENCES users(id),
	created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_keywords_shop_word ON keywords(coalesce(shop_id, 0), word);
CREATE INDEX IF NOT EXISTS idx_keywords_shop_grp ON keywords(shop_id, grp, weight DESC);

CREATE TABLE IF NOT EXISTS voice_profiles(
	shop_id INTEGER PRIMARY KEY REFERENCES shops(id) ON DELETE CASCADE,
	text TEXT NOT NULL DEFAULT '',
	examples TEXT NOT NULL DEFAULT '[]',
	updated_by INTEGER REFERENCES users(id),
	updated_at TEXT NOT NULL
);
`

func (a *Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		_, e := tx.Exec(copywritingSchema)
		return e
	}}}
}
