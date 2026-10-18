package tasks

import (
	"database/sql"
	"strconv"
	"xstudio/internal/core"
)

const schema = `
CREATE TABLE task_rules(
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 0,
	event TEXT NOT NULL,
	title_tpl TEXT NOT NULL,
	body_tpl TEXT NOT NULL DEFAULT '',
	assign_mode TEXT NOT NULL,
	assign_user_id INTEGER REFERENCES users(id),
	assign_role_id INTEGER REFERENCES roles(id),
	due_hours INTEGER,
	priority TEXT NOT NULL DEFAULT 'normal',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE tasks(
	id INTEGER PRIMARY KEY,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'todo' CHECK(status IN ('todo','doing','waiting','done','canceled')),
	priority TEXT NOT NULL DEFAULT 'normal' CHECK(priority IN ('low','normal','high','urgent')),
	assignee_id INTEGER REFERENCES users(id),
	created_by INTEGER NOT NULL DEFAULT 0,
	due_at TEXT,
	target_type TEXT CHECK(target_type IS NULL OR target_type IN ('product','listing','asset')),
	target_id INTEGER,
	checklist TEXT NOT NULL DEFAULT '[]',
	rule_id INTEGER REFERENCES task_rules(id) ON DELETE SET NULL,
	reopen_count INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	done_at TEXT,
	CHECK((target_type IS NULL) = (target_id IS NULL))
);
CREATE TABLE task_watchers(
	task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	PRIMARY KEY(task_id, user_id)
);
CREATE TABLE task_comments(
	id INTEGER PRIMARY KEY,
	task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL,
	body TEXT NOT NULL,
	mentions TEXT NOT NULL DEFAULT '[]',
	created_at TEXT NOT NULL
);
CREATE TABLE task_events(
	id INTEGER PRIMARY KEY,
	task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL,
	kind TEXT NOT NULL,
	detail TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL
);
CREATE TABLE notifications(
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	type TEXT NOT NULL,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	link TEXT NOT NULL DEFAULT '',
	target_type TEXT,
	target_id INTEGER,
	read_at TEXT,
	created_at TEXT NOT NULL
);
CREATE TABLE notification_prefs(
	user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	prefs TEXT NOT NULL DEFAULT '{}',
	updated_at TEXT NOT NULL
);
CREATE TABLE task_cursors(
	name TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE task_job_seen(
	job_id INTEGER PRIMARY KEY,
	finished_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE task_once(
	key TEXT PRIMARY KEY,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS tasks_assignee_status ON tasks(assignee_id, status);
CREATE INDEX IF NOT EXISTS tasks_target ON tasks(target_type, target_id);
CREATE INDEX IF NOT EXISTS tasks_rule_open ON tasks(rule_id, target_type, target_id, status);
CREATE INDEX IF NOT EXISTS tasks_created_by ON tasks(created_by);
CREATE INDEX IF NOT EXISTS notifications_user_time ON notifications(user_id, created_at);
CREATE INDEX IF NOT EXISTS notifications_user_read ON notifications(user_id, read_at);
CREATE INDEX IF NOT EXISTS task_comments_task ON task_comments(task_id, id);
CREATE INDEX IF NOT EXISTS task_events_task ON task_events(task_id, id);
`

func (a *Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		if _, e := tx.Exec(schema); e != nil {
			return e
		}
		now := core.Now()
		var maxAudit, maxJob int64
		if e := tx.QueryRow("SELECT coalesce(max(id),0) FROM audit_log").Scan(&maxAudit); e != nil {
			return e
		}
		if e := tx.QueryRow("SELECT coalesce(max(id),0) FROM jobs").Scan(&maxJob); e != nil {
			return e
		}
		if _, e := tx.Exec("INSERT INTO task_cursors(name,value,updated_at) VALUES('audit_id',?,?)", strconv.FormatInt(maxAudit, 10), now); e != nil {
			return e
		}
		if _, e := tx.Exec("INSERT INTO task_cursors(name,value,updated_at) VALUES('job_id',?,?)", strconv.FormatInt(maxJob, 10), now); e != nil {
			return e
		}
		if _, e := tx.Exec("INSERT INTO task_job_seen(job_id, finished_at) SELECT id, finished_at FROM jobs WHERE status IN ('done','failed')"); e != nil {
			return e
		}
		var photoID, designID int64
		if e := tx.QueryRow("SELECT id FROM roles WHERE key='photographer'").Scan(&photoID); e != nil {
			return e
		}
		if e := tx.QueryRow("SELECT id FROM roles WHERE key='designer'").Scan(&designID); e != nil {
			return e
		}
		type seed struct {
			name, event, title, body, mode, priority string
			role                                     int64
		}
		seeds := []seed{
			{"拍样", "product.created", "拍样：{product}", "", "role_round_robin", "normal", photoID},
			{"修图", "assets.first_upload", "修图：{product}", "", "role_least_loaded", "normal", designID},
			{"处理出图失败", "render.failed", "处理出图失败：{product} · {shop}", "{reason}", "product_owner", "high", 0},
			{"审核被退回", "review.rejected", "审核被退回：{product} · {shop}", "{reason}", "job_user", "normal", 0},
		}
		for _, s := range seeds {
			var role any
			if s.role > 0 {
				role = s.role
			}
			if _, e := tx.Exec(`INSERT INTO task_rules(name,enabled,event,title_tpl,body_tpl,assign_mode,assign_user_id,assign_role_id,due_hours,priority,created_at,updated_at) VALUES(?,0,?,?,?,?,NULL,?,NULL,?,?,?)`, s.name, s.event, s.title, s.body, s.mode, role, s.priority, now, now); e != nil {
				return e
			}
		}
		return nil
	}}, {Version: 2, Up: migrateTargets}, {Version: 3, Up: indexTargetProducts}}
}
