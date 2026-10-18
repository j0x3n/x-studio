package inventory

import (
	"database/sql"
	"testing"
)

// Acceptance 7 and 8, including automatic checks in every exported mutation.
func TestLowStockCrossingsAndThresholdChanges(t *testing.T) {
	f := boot(t)
	id := f.item(t, "LOW")
	f.in(t, id, 1, 10, 100, "")
	f.req(t, "PATCH", path(id), obj{"safety_stock": 5}, 200)
	alerts := func() int64 {
		return f.n(t, "SELECT count(*) FROM audit_log WHERE action='inventory.low_stock' AND target_id=?", id)
	}
	f.out(t, id, 1, 5, 0)
	if alerts() != 0 {
		t.Fatal("equal threshold warned")
	}
	f.out(t, id, 1, 1, 0)
	if alerts() != 1 {
		t.Fatal("crossing did not warn")
	}
	f.tx(t, func(tx *sql.Tx) error {
		if e := AfterChange(tx, id); e != nil {
			return e
		}
		return Reserve(tx, id, 1, "order", 1)
	})
	f.out(t, id, 1, 1, 0)
	if alerts() != 1 {
		t.Fatal("continuous low repeated")
	}
	f.tx(t, func(tx *sql.Tx) error { return Release(tx, "order", 1, 0) })
	f.in(t, id, 1, 2, 20, "")
	if f.n(t, "SELECT count(*) FROM inv_items WHERE id=? AND low_since IS NULL", id) != 1 {
		t.Fatal("recovery did not clear")
	}
	f.tx(t, func(tx *sql.Tx) error { return Reserve(tx, id, 1, "order", 2) })
	if alerts() != 2 {
		t.Fatal("second crossing")
	}
	f.tx(t, func(tx *sql.Tx) error { return Release(tx, "order", 2, 0) })
	f.req(t, "PATCH", path(id), obj{"safety_stock": 6}, 200)
	if alerts() != 3 {
		t.Fatal("threshold change did not warn")
	}
	f.req(t, "PATCH", path(id), obj{"safety_stock": 0}, 200)
	f.out(t, id, 1, 5, 0)
	if alerts() != 3 || f.n(t, "SELECT count(*) FROM inv_items WHERE id=? AND low_since IS NULL", id) != 1 {
		t.Fatal("disabled threshold warned")
	}
	details, e := one(f.c.DB, "SELECT target_type,user_id,detail FROM audit_log WHERE action='inventory.low_stock' AND target_id=? ORDER BY id LIMIT 1", id)
	if e != nil {
		t.Fatal(e)
	}
	if details["target_type"] != "inv_item" || details["user_id"] != int64(0) {
		t.Fatal(details)
	}
}

func TestLowStockDropshipUnlimitedAndRollback(t *testing.T) {
	f := boot(t)
	id := f.item(t, "LOW-DROP")
	wid := f.wh(t, "dropship")
	p := fmtDropship(wid, id)
	f.req(t, "PUT", p, obj{"qty": nil}, 200)
	f.req(t, "PATCH", path(id), obj{"safety_stock": 5}, 200)
	if f.n(t, "SELECT count(*) FROM audit_log WHERE action='inventory.low_stock'") != 0 {
		t.Fatal("unlimited warned")
	}
	f.req(t, "PUT", p, obj{"qty": 4}, 200)
	f.req(t, "PUT", p, obj{"qty": 3}, 200)
	if f.n(t, "SELECT count(*) FROM audit_log WHERE action='inventory.low_stock'") != 1 {
		t.Fatal("dropship alert count")
	}
	f.req(t, "PUT", p, obj{"qty": nil}, 200)
	if f.n(t, "SELECT count(*) FROM inv_items WHERE id=? AND low_since IS NULL", id) != 1 {
		t.Fatal("unlimited did not clear stale low")
	}
	f.req(t, "PUT", p, obj{"qty": 5}, 200)
	tx, e := f.c.DB.Begin()
	if e != nil {
		t.Fatal(e)
	}
	if e = Reserve(tx, id, 1, "order", 1); e != nil {
		tx.Rollback()
		t.Fatal(e)
	}
	if e = tx.Rollback(); e != nil {
		t.Fatal(e)
	}
	if f.n(t, "SELECT count(*) FROM audit_log WHERE action='inventory.low_stock'") != 1 || f.n(t, "SELECT count(*) FROM inv_items WHERE id=? AND low_since IS NULL", id) != 1 {
		t.Fatal("rolled-back alert persisted")
	}
}
