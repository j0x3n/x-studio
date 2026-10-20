package tasks

import (
	"database/sql"
	"encoding/json"
)

// A task binds to an object incarnation, not just its reusable SQLite rowid.
// Reads only probe object primary keys; their cost is independent of audit history.
const liveTarget = `t.target_deleted=0 AND (
 (t.target_type='product' AND EXISTS(SELECT 1 FROM products p WHERE p.id=t.target_id AND p.created_at=t.target_created_at)) OR
 (t.target_type='listing' AND EXISTS(SELECT 1 FROM listings l WHERE l.id=t.target_id AND l.created_at=t.target_created_at)) OR
 (t.target_type='asset' AND EXISTS(SELECT 1 FROM assets ast WHERE ast.id=t.target_id AND ast.created_at=t.target_created_at))
)`

const targetCreatedAt = `coalesce(CASE target_type
 WHEN 'product' THEN (SELECT created_at FROM products WHERE id=tasks.target_id)
 WHEN 'listing' THEN (SELECT created_at FROM listings WHERE id=tasks.target_id)
 WHEN 'asset' THEN (SELECT created_at FROM assets WHERE id=tasks.target_id)
 END,'')`

const targetProductID = `coalesce(CASE target_type
 WHEN 'product' THEN target_id
 WHEN 'listing' THEN (SELECT product_id FROM listings WHERE id=tasks.target_id)
 WHEN 'asset' THEN (SELECT product_id FROM assets WHERE id=tasks.target_id)
 END,0)`

// Called in the same transaction as an explicit new binding. Unrelated PATCHes
// must never refresh these fields, or a reused id could resurrect an old target.
func bindTaskTarget(q dbx, id int64) error {
	_, err := q.Exec(`UPDATE tasks SET target_created_at=`+bindingCreatedAt(q)+`,
 target_product_id=`+targetProductID+`,
 target_deleted=CASE WHEN target_type IS NOT NULL AND `+bindingCreatedAt(q)+`='' THEN 1 ELSE 0 END WHERE id=?`, id)
	return err
}

func (a *Module) taskTargetAlive(t taskRec) bool {
	if t.TargetType == "" || t.TargetID == 0 || t.TargetDeleted {
		return false
	}
	var n int
	return a.DB.QueryRow("SELECT count(*) FROM tasks t WHERE t.id=? AND ("+liveTargets(a.DB)+")", t.ID).Scan(&n) == nil && n > 0
}

// One-time migration sweep only. Runtime deletion marking uses incremental events.
func markDeletedTargets(q dbx) error {
	_, err := q.Exec("UPDATE tasks AS t SET target_deleted=1 WHERE t.target_type IS NOT NULL AND t.target_deleted=0 AND NOT (" + liveTargets(q) + ")")
	return err
}

// Apply within the cursor transaction, so a failed update cannot lose a deletion.
// Both updates select affected bindings through task-owned indexes. A live object
// is left alone: it may be a new binding of a reused id, or an asset moved away
// from the deleted product before the scanner caught up.
func applyTargetAudit(q dbx, row auditRow) error {
	if row.TargetID <= 0 {
		return nil
	}
	var typ string
	switch row.Action {
	case "inventory.item.delete":
		typ = "inv_item"
	case "purchase.delete":
		typ = "purchase_order"
	case "product.delete":
		typ = "product"
	case "listing.delete":
		typ = "listing"
	case "asset.delete", "assets.batch.delete":
		typ = "asset"
	case "assets.batch.move":
		if row.TargetType != "asset" {
			return nil
		}
		var detail struct {
			ProductID int64 `json:"to_product_id"`
		}
		if json.Unmarshal([]byte(row.Detail), &detail) != nil || detail.ProductID <= 0 {
			return nil
		}
		// Keep the indexed parent reference current for later cascade deletions.
		// Prefer the live asset over a queued move event that may already be stale.
		_, err := q.Exec(`UPDATE tasks SET target_product_id=coalesce(
 (SELECT product_id FROM assets WHERE id=tasks.target_id AND created_at=tasks.target_created_at), ?)
 WHERE target_type='asset' AND target_id=? AND target_deleted=0`, detail.ProductID, row.TargetID)
		return err
	default:
		return nil
	}
	if row.TargetType != typ {
		return nil
	}
	if _, err := q.Exec("UPDATE tasks AS t SET target_deleted=1 WHERE target_type=? AND target_id=? AND target_deleted=0 AND NOT ("+liveTargets(q)+")", typ, row.TargetID); err != nil {
		return err
	}
	if typ == "product" {
		_, err := q.Exec("UPDATE tasks AS t SET target_deleted=1 WHERE target_product_id=? AND target_type IN ('listing','asset') AND target_deleted=0 AND NOT ("+liveTargets(q)+")", row.TargetID)
		return err
	}
	return nil
}

func indexTargetProducts(tx *sql.Tx) error {
	_, err := tx.Exec("CREATE INDEX IF NOT EXISTS tasks_live_target_product ON tasks(target_product_id,target_type) WHERE target_deleted=0")
	return err
}

func migrateTargets(tx *sql.Tx) error {
	if _, err := tx.Exec(`ALTER TABLE tasks ADD COLUMN target_created_at TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN target_audit_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN target_product_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN target_deleted INTEGER NOT NULL DEFAULT 0;`); err != nil {
		return err
	}
	// Existing bindings have no snapshot. Conservatively retire targets created
	// after the task's last update rather than adopting a subsequently reused id.
	if _, err := tx.Exec(`UPDATE tasks SET target_created_at=` + targetCreatedAt + `,
 target_product_id=` + targetProductID + `;
UPDATE tasks SET target_deleted=1 WHERE target_type IS NOT NULL AND julianday(target_created_at)>julianday(updated_at);`); err != nil {
		return err
	}
	return markDeletedTargets(tx)
}
