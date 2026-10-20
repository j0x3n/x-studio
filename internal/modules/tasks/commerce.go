package tasks

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

func commerceTarget(typ string) bool { return typ == "inv_item" || typ == "purchase_order" }

func tableExists(q dbx, table string) bool {
	var n int
	return q.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n) == nil && n > 0
}

// Optional modules must never appear in prepared SQL when their tables are absent.
func commerceLive(q dbx, u *core.User) string {
	clauses := []string{}
	for _, v := range []struct{ typ, table, perm string }{
		{"inv_item", "inv_items", core.InventoryView},
		{"purchase_order", "purchase_orders", core.PurchaseManage},
	} {
		if u != nil && (u.Role.Scope == "shares" || !u.Has(v.perm)) {
			continue
		}
		if tableExists(q, v.table) {
			clauses = append(clauses, "(t.target_type='"+v.typ+"' AND EXISTS(SELECT 1 FROM "+v.table+" c WHERE c.id=t.target_id AND c.created_at=t.target_created_at))")
		}
	}
	if len(clauses) == 0 {
		return "0=1"
	}
	return "(t.target_deleted=0 AND (" + strings.Join(clauses, " OR ") + "))"
}

func liveTargets(q dbx) string { return "(" + liveTarget + " OR " + commerceLive(q, nil) + ")" }

func bindingCreatedAt(q dbx) string {
	expr := targetCreatedAt
	for _, v := range []struct{ typ, table string }{{"inv_item", "inv_items"}, {"purchase_order", "purchase_orders"}} {
		if tableExists(q, v.table) {
			expr = "CASE WHEN target_type='" + v.typ + "' THEN coalesce((SELECT created_at FROM " + v.table + " WHERE id=tasks.target_id),'') ELSE " + expr + " END"
		}
	}
	return expr
}

func (a *Module) visibleWhereQ(q dbx, u core.User) (string, []any) {
	old, args := a.legacyVisibleWhere(u)
	// A participant relationship cannot retain access after module permission loss,
	// deletion, or ID reuse. Keep the original semantics for the original types.
	return "((coalesce(t.target_type,'') NOT IN ('inv_item','purchase_order') AND (" + old + ")) OR (" + commerceLive(q, &u) + " AND (" + participantOrManager(u) + ")))", args
}

func participantOrManager(u core.User) string {
	if u.Has(core.TaskManage) {
		return "1=1"
	}
	id := strconv.FormatInt(u.ID, 10)
	return "(t.assignee_id=" + id + " OR t.created_by=" + id + " OR EXISTS(SELECT 1 FROM task_watchers w WHERE w.task_id=t.id AND w.user_id=" + id + "))"
}

// SQLite requires a table rebuild to widen an existing CHECK constraint.
// Core runs migrations with foreign keys disabled and checks them before commit.
func migrateCommerce(tx *sql.Tx) error {
	start := strings.Index(schema, "CREATE TABLE tasks(")
	end := strings.Index(schema[start:], "CREATE TABLE task_watchers") + start
	ddl := strings.Replace(schema[start:end], "CREATE TABLE tasks(", "CREATE TABLE tasks_next(", 1)
	ddl = strings.Replace(ddl, "'product','listing','asset'", "'product','listing','asset','inv_item','purchase_order'", 1)
	ddl = strings.Replace(ddl, "\n\tCHECK((target_type", "\n target_created_at TEXT NOT NULL DEFAULT '',\n target_audit_id INTEGER NOT NULL DEFAULT 0,\n target_product_id INTEGER NOT NULL DEFAULT 0,\n target_deleted INTEGER NOT NULL DEFAULT 0,\n\tCHECK((target_type", 1)
	if _, err := tx.Exec(ddl); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO tasks_next(id,title,body,status,priority,assignee_id,created_by,due_at,target_type,target_id,checklist,rule_id,reopen_count,created_at,updated_at,done_at,target_created_at,target_audit_id,target_product_id,target_deleted) SELECT id,title,body,status,priority,assignee_id,created_by,due_at,target_type,target_id,checklist,rule_id,reopen_count,created_at,updated_at,done_at,target_created_at,target_audit_id,target_product_id,target_deleted FROM tasks; DROP TABLE tasks; ALTER TABLE tasks_next RENAME TO tasks;`); err != nil {
		return err
	}
	if _, err := tx.Exec(schema[strings.Index(schema, "CREATE INDEX IF NOT EXISTS tasks_assignee_status"):]); err != nil {
		return err
	}
	if err := indexTargetProducts(tx); err != nil {
		return err
	}
	if _, err := tx.Exec("ALTER TABLE notifications ADD COLUMN task_id INTEGER REFERENCES tasks(id); ALTER TABLE notifications ADD COLUMN target_created_at TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	now := core.Now()
	for _, v := range []struct{ name, event, title, mode, priority, role string }{
		{"库存预警", "inventory.low_stock", "补货：{item}", "role_least_loaded", "high", "operator"},
		{"到货复核", "purchase.received", "到货复核：{purchase}", "role_round_robin", "normal", "warehouse"},
	} {
		if _, err := tx.Exec(`INSERT INTO task_rules(name,enabled,event,title_tpl,assign_mode,assign_role_id,priority,created_at,updated_at) VALUES(?,0,?,?,?,(SELECT id FROM roles WHERE key=?),?,?,?)`, v.name, v.event, v.title, v.mode, v.role, v.priority, now, now); err != nil {
			return err
		}
	}
	// Do not reset the legacy cursor: pending legacy events still need consuming.
	_, err := tx.Exec(`INSERT INTO task_cursors(name,value,updated_at) SELECT 'commerce_audit_start',cast(coalesce(max(id),0) AS TEXT),? FROM audit_log`, now)
	return err
}

// Stored task notices retain the task identity; recheck it on every read and
// immediately before sending a queued SSE frame, using the latest account.
func (a *Module) noticeScope(u core.User) (string, []any) {
	if u.Disabled || !u.Has(core.TaskView) {
		return "coalesce(target_type,'') NOT IN ('inv_item','purchase_order')", nil
	}
	vis, args := a.visibleWhere(u)
	return "(coalesce(target_type,'') NOT IN ('inv_item','purchase_order') OR EXISTS(SELECT 1 FROM tasks t WHERE t.id=notifications.task_id AND t.target_type=notifications.target_type AND t.target_id=notifications.target_id AND t.target_created_at=notifications.target_created_at AND (" + vis + ")))", args
}

func (a *Module) unreadFor(u core.User) (int, error) {
	vis, args := a.noticeScope(u)
	var n int
	err := a.DB.QueryRow("SELECT count(*) FROM notifications WHERE user_id=? AND read_at IS NULL AND ("+vis+")", append([]any{u.ID}, args...)...).Scan(&n)
	return n, err
}

func (a *Module) frameAllowed(uid int64, frame []byte) bool {
	parts := strings.SplitN(string(frame), "\ndata: ", 2)
	if len(parts) != 2 {
		return true
	}
	var n struct {
		ID         int64  `json:"id"`
		TargetType string `json:"target_type"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(parts[1])), &n) != nil {
		return false
	}
	if !commerceTarget(n.TargetType) {
		return true
	}
	u, err := a.User(uid)
	if err != nil {
		return false
	}
	vis, args := a.noticeScope(u)
	var count int
	return a.DB.QueryRow("SELECT count(*) FROM notifications WHERE id=? AND user_id=? AND ("+vis+")", append([]any{n.ID, uid}, args...)...).Scan(&count) == nil && count > 0
}
