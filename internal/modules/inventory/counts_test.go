package inventory

import (
	"database/sql"
	"testing"
)

func (f *fixture) count(t *testing.T, wid int64, ids []int64) int64 {
	t.Helper()
	in := obj{"warehouse_id": wid}
	if ids != nil {
		in["item_ids"] = ids
	}
	return int64(f.req(t, "POST", "/api/inventory/counts", in, 200)["id"].(float64))
}

func TestCountListProgressAndCreatorName(t *testing.T) {
	f := boot(t)
	first, second := f.item(t, "PROGRESS-A"), f.item(t, "PROGRESS-B")
	cid := f.count(t, 1, []int64{first, second})
	f.req(t, "PATCH", countPath(cid), obj{"lines": []obj{{"item_id": first, "counted_qty": 0}}}, 200)
	for _, displayName := range []string{"盘点员", ""} {
		f.exec(t, "UPDATE users SET display_name=? WHERE username='admin'", displayName)
		wantName := displayName
		if wantName == "" {
			wantName = "admin"
		}
		list := f.req(t, "GET", "/api/inventory/counts?warehouse_id=1&status=draft", nil, 200)
		line := rows(list)[0].(map[string]any)
		if line["line_count"] != float64(2) || line["counted_count"] != float64(1) || line["created_by_name"] != wantName {
			t.Fatal(line)
		}
		detail := f.req(t, "GET", countPath(cid), nil, 200)
		if detail["created_by_name"] != wantName {
			t.Fatal(detail)
		}
	}
}

// Acceptance 6: finish uses current stock even if it differs from the snapshot.
func TestCountCurrentQuantityLossAndDraftExclusion(t *testing.T) {
	f := boot(t)
	id := f.item(t, "COUNT-LOSS")
	f.in(t, id, 1, 10, 1000, "")
	cid := f.count(t, 1, nil)
	f.req(t, "POST", "/api/inventory/counts", obj{"warehouse_id": 1}, 409)
	f.in(t, id, 1, 5, 500, "") // Current 15, snapshot 10, counted 8: loss must be 7.
	f.req(t, "PATCH", countPath(cid), obj{"lines": []obj{{"item_id": id, "counted_qty": 8}}}, 200)
	detail := f.req(t, "GET", countPath(cid), nil, 200)
	line := detail["lines"].([]any)[0].(map[string]any)
	if line["system_qty"] != float64(10) || line["current_qty"] != float64(15) || line["difference"] != float64(-7) {
		t.Fatal(line)
	}
	result := f.req(t, "POST", countPath(cid)+"/finish", nil, 200)
	if result["loss_qty"] != float64(7) || result["ledger_count"] != float64(1) || f.level(t, id).OnHand != 8 {
		t.Fatal(result)
	}
	if f.n(t, "SELECT sum(qty) FROM inv_ledger WHERE ref_type='count' AND ref_id=?", cid) != -7 {
		t.Fatal("used snapshot")
	}
	f.req(t, "POST", countPath(cid)+"/finish", nil, 409)
	f.req(t, "PATCH", countPath(cid), obj{}, 409)
	f.req(t, "POST", countPath(cid)+"/cancel", nil, 409)
	next := f.count(t, 1, nil)
	f.req(t, "POST", countPath(next)+"/cancel", nil, 200)
	f.req(t, "POST", countPath(next)+"/finish", nil, 409)
	f.count(t, 1, []int64{})
}

func TestCountGainCostsCurrentStockAndSkippedLines(t *testing.T) {
	f := boot(t)
	id := f.item(t, "GAIN-ROUND")
	explicit := f.item(t, "GAIN-EXPLICIT")
	zero := f.item(t, "GAIN-ZERO")
	skip := f.item(t, "GAIN-SKIP")
	f.in(t, id, 1, 3, 100, "")
	f.in(t, skip, 1, 2, 20, "")
	cid := f.count(t, 1, []int64{id, skip})
	f.out(t, id, 1, 1, 0) // Current 2, snapshot 3; counted 5 means gain 3 at round(100/3)=33.
	f.req(t, "PATCH", countPath(cid), obj{"add_item_ids": []int64{explicit, zero}, "lines": []obj{{"item_id": id, "counted_qty": 5}, {"item_id": explicit, "counted_qty": 2, "unit_cost": 19}, {"item_id": zero, "counted_qty": 4}}}, 200)
	result := f.req(t, "POST", countPath(cid)+"/finish", nil, 200)
	if result["gain_qty"] != float64(9) || result["gain_cost"] != float64(137) || result["ledger_count"] != float64(3) {
		t.Fatal(result)
	}
	if f.level(t, id).OnHand != 5 || f.level(t, skip).OnHand != 2 || f.level(t, zero).OnHand != 4 {
		t.Fatal("count quantities")
	}
	if f.n(t, "SELECT cost FROM inv_ledger WHERE ref_type='count' AND ref_id=? AND item_id=?", cid, id) != 99 {
		t.Fatal("default cost not rounded")
	}
	if f.n(t, "SELECT cost FROM inv_ledger WHERE ref_type='count' AND ref_id=? AND item_id=?", cid, zero) != 0 {
		t.Fatal("no-batch default cost")
	}
}

func TestCountFinishAtomicWhenReservedStockWouldBeLost(t *testing.T) {
	f := boot(t)
	gain := f.item(t, "ATOMIC-GAIN")
	loss := f.item(t, "ATOMIC-LOSS")
	f.in(t, loss, 1, 3, 100, "")
	cid := f.count(t, 1, []int64{gain, loss})
	f.req(t, "PATCH", countPath(cid), obj{"lines": []obj{{"item_id": gain, "counted_qty": 1}, {"item_id": loss, "counted_qty": 0}}}, 200)
	f.tx(t, func(tx *sql.Tx) error { return Reserve(tx, loss, 3, "order", 1) })
	f.req(t, "POST", countPath(cid)+"/finish", nil, 409)
	if f.level(t, gain).OnHand != 0 || f.level(t, loss).OnHand != 3 || f.n(t, "SELECT count(*) FROM inv_counts WHERE id=? AND status='draft'", cid) != 1 {
		t.Fatal("partial count committed")
	}
}

func TestCountRejectsDropshipAndInvalidUpdates(t *testing.T) {
	f := boot(t)
	id := f.item(t, "COUNT-INVALID")
	wid := f.wh(t, "dropship")
	f.req(t, "POST", "/api/inventory/counts", obj{"warehouse_id": wid}, 400)
	cid := f.count(t, 1, []int64{id})
	f.req(t, "PATCH", countPath(cid), obj{"lines": []obj{{"item_id": id, "counted_qty": -1}}}, 400)
	f.req(t, "PATCH", countPath(cid), obj{"lines": []obj{{"item_id": 9999, "counted_qty": 1}}}, 404)
	f.req(t, "PATCH", countPath(cid), obj{"add_item_ids": []int64{9999}}, 404)
}
