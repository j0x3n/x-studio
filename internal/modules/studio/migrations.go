package studio

import (
	"database/sql"
	"os"
	"path/filepath"
	"xstudio/internal/core"
)

const studioSchema = `
CREATE TABLE IF NOT EXISTS listing_render(
	listing_id INTEGER PRIMARY KEY REFERENCES listings(id) ON DELETE CASCADE,
	detail_template TEXT NOT NULL DEFAULT '',
	main_template TEXT NOT NULL DEFAULT 'default',
	ai_label INTEGER NOT NULL DEFAULT 1,
	review_status TEXT NOT NULL DEFAULT 'none' CHECK(review_status IN ('none','pending','approved','rejected')),
	review_note TEXT NOT NULL DEFAULT '',
	reviewed_by INTEGER REFERENCES users(id),
	reviewed_at TEXT NOT NULL DEFAULT '',
	rendered_job_id INTEGER,
	rendered_template TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL
);

CREATE TRIGGER IF NOT EXISTS trg_listing_render_insert
AFTER INSERT ON listings
BEGIN
	INSERT OR IGNORE INTO listing_render(listing_id, detail_template, main_template, ai_label, review_status, updated_at)
	VALUES(NEW.id, '', 'default', NEW.ai_label, 'none', NEW.updated_at);
END;

CREATE TABLE IF NOT EXISTS template_prefs(
	shop_id INTEGER PRIMARY KEY REFERENCES shops(id) ON DELETE CASCADE,
	default_detail TEXT NOT NULL DEFAULT 'navy',
	disabled TEXT NOT NULL DEFAULT '[]',
	updated_by INTEGER REFERENCES users(id),
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS studio_settings(
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_by INTEGER REFERENCES users(id),
	updated_at TEXT NOT NULL
);
`

func (a *Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		if _, e := tx.Exec(studioSchema); e != nil {
			return e
		}
		if _, e := tx.Exec("INSERT OR IGNORE INTO studio_settings(key,value,updated_at) VALUES('require_review','true',?)", core.Now()); e != nil {
			return e
		}
		rows, e := tx.Query("SELECT id,status,pipeline_status,ai_label,updated_at FROM listings")
		if e != nil {
			return e
		}
		type existingListing struct {
			id             int64
			status         string
			pipelineStatus string
			aiLabel        int
			updatedAt      string
		}
		var list []existingListing
		for rows.Next() {
			var item existingListing
			if e := rows.Scan(&item.id, &item.status, &item.pipelineStatus, &item.aiLabel, &item.updatedAt); e != nil {
				rows.Close()
				return e
			}
			list = append(list, item)
		}
		if e := rows.Err(); e != nil {
			rows.Close()
			return e
		}
		rows.Close()

		for _, item := range list {
			hasOutput := item.status == "ready" || item.status == "listed" || item.pipelineStatus == "ready"
			for _, name := range []string{"导出.zip", "导出"} {
				if _, err := os.Stat(filepath.Join(a.ListingDir(item.id), name)); err == nil {
					hasOutput = true
				}
			}
			reviewStatus := "none"
			renderedTpl := ""
			var renderedJob any
			if hasOutput {
				reviewStatus = "approved"
				renderedTpl = "navy"
				var jid int64
				if tx.QueryRow("SELECT id FROM jobs WHERE target_type='listing' AND target_id=? AND status='done' AND instr(steps,'render')>0 ORDER BY id DESC LIMIT 1", item.id).Scan(&jid) == nil {
					renderedJob = jid
				}
			}
			if _, e := tx.Exec(
				"INSERT OR IGNORE INTO listing_render(listing_id,detail_template,main_template,ai_label,review_status,rendered_job_id,rendered_template,updated_at) VALUES(?, '', 'default', ?, ?, ?, ?, ?)",
				item.id, item.aiLabel, reviewStatus, renderedJob, renderedTpl, item.updatedAt,
			); e != nil {
				return e
			}
		}
		return nil
	}}, {Version: 2, Up: func(tx *sql.Tx) error {
		_, e := tx.Exec("ALTER TABLE listing_render ADD COLUMN render_note TEXT NOT NULL DEFAULT ''")
		return e
	}}}
}
