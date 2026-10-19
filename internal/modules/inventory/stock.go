package inventory

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
	"xstudio/internal/core"
)

var ErrShort = errors.New("库存不足")

type Querier interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}
type In struct {
	ItemID, WarehouseID       int64
	Qty, Cost                 int64
	Currency, Grade           string
	SupplierID                int64
	ReceivedAt, Kind, RefType string
	RefID                     int64
	Note                      string
	UserID                    int64
}
type Out struct {
	ItemID, WarehouseID int64
	Qty, BatchID        int64
	Kind, RefType       string
	RefID               int64
	Note                string
	UserID              int64
}
type Taken struct{ BatchID, Qty, Cost int64 }
type Level struct {
	OnHand, Dropship    int64
	Unlimited           bool
	Reserved, Available int64
}

// atomic uses a savepoint, never a second transaction or database connection.
// Callers must still roll back their transaction if their larger operation fails.
func atomic(tx *sql.Tx, fn func() error) (err error) {
	if _, err = tx.Exec("SAVEPOINT inventory_change"); err != nil {
		return
	}
	defer func() {
		if err != nil {
			_, _ = tx.Exec("ROLLBACK TO inventory_change")
		}
		_, e := tx.Exec("RELEASE inventory_change")
		if err == nil {
			err = e
		}
	}()
	return fn()
}

func add(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, bad(400, "数量或金额超出整数范围")
	}
	return a + b, nil
}

// rounded computes round(a*b/d) exactly, including when a*b exceeds int64.
func rounded(a, b, d int64) (int64, error) {
	if a < 0 || b < 0 || d <= 0 {
		return 0, bad(400, "数量或金额不正确")
	}
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi >= uint64(d) {
		return 0, bad(400, "金额超出整数范围")
	}
	q, r := bits.Div64(hi, lo, uint64(d))
	if q > math.MaxInt64 {
		return 0, bad(400, "金额超出整数范围")
	}
	if r >= uint64(d)/2+uint64(d)%2 {
		q++
	}
	if q > math.MaxInt64 {
		return 0, bad(400, "金额超出整数范围")
	}
	return int64(q), nil
}
func currency(s string) (string, error) {
	if s == "" {
		return "CNY", nil
	}
	if len(s) != 3 || strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return "", bad(400, "币种应为三个大写字母")
	}
	return s, nil
}
func received(s string) (string, error) {
	const layout = "2006-01-02T15:04:05.000000000Z"
	if s == "" {
		return time.Now().UTC().Format(layout), nil
	}
	if t, e := time.Parse("2006-01-02", s); e == nil {
		return t.Format(layout), nil
	}
	if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
		return t.UTC().Format(layout), nil
	}
	return "", bad(400, "到货日期格式不正确")
}
func nullable(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
func item(q Querier, id int64) (code, unit string, archived bool, err error) {
	err = q.QueryRow("SELECT code,unit,archived FROM inv_items WHERE id=?", id).Scan(&code, &unit, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		err = bad(404, "物料不存在")
	}
	return
}
func warehouse(q Querier, id int64) (typ string, err error) {
	var archived bool
	err = q.QueryRow("SELECT type,archived FROM inv_warehouses WHERE id=?", id).Scan(&typ, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return "", bad(404, "仓库不存在")
	}
	if err == nil && archived {
		err = bad(409, "仓库已归档")
	}
	return
}
func short(code, unit string, n int64) error {
	return fmt.Errorf("%s %w，差 %d %s", code, ErrShort, n, unit)
}
func availableShort(code, unit string, qty, available int64) error {
	if available >= 0 {
		return short(code, unit, qty-available)
	}
	// A supplier update can expose negative availability. Format even a
	// deficit exceeding MaxInt64 correctly without signed overflow.
	return fmt.Errorf("%s %w，差 %d %s", code, ErrShort, uint64(qty)+uint64(-available), unit)
}

// Stock accepts a DB for independent reads or the caller's transaction for a
// consistent view during a mutation. Unlimited does not replace Available.
func Stock(q Querier, itemID int64) (s Level, err error) {
	if _, _, _, err = item(q, itemID); err != nil {
		return
	}
	err = q.QueryRow(`SELECT
 (SELECT coalesce(sum(b.qty_left),0) FROM inv_batches b JOIN inv_warehouses w ON w.id=b.warehouse_id WHERE b.item_id=? AND w.type IN ('own','stall') AND w.archived=0),
 (SELECT coalesce(sum(d.qty),0) FROM inv_dropship d JOIN inv_warehouses w ON w.id=d.warehouse_id WHERE d.item_id=? AND w.archived=0),
 EXISTS(SELECT 1 FROM inv_dropship d JOIN inv_warehouses w ON w.id=d.warehouse_id WHERE d.item_id=? AND d.qty IS NULL AND w.archived=0),
 (SELECT coalesce(sum(qty),0) FROM inv_reservations WHERE item_id=?)`, itemID, itemID, itemID, itemID).Scan(&s.OnHand, &s.Dropship, &s.Unlimited, &s.Reserved)
	if err != nil {
		return
	}
	s.Available, err = add(s.OnHand, s.Dropship)
	if err == nil {
		s.Available, err = add(s.Available, -s.Reserved)
	}
	return
}

// AfterChange also runs after dropship and safety-stock edits. An empty request
// represents the system actor: core.AuditTx currently dereferences nil requests.
func AfterChange(tx *sql.Tx, itemID int64) error {
	s, e := Stock(tx, itemID)
	if e != nil {
		return e
	}
	var safety int64
	var low sql.NullString
	var code, name string
	if e = tx.QueryRow("SELECT safety_stock,low_since,code,name FROM inv_items WHERE id=?", itemID).Scan(&safety, &low, &code, &name); e != nil {
		return e
	}
	if s.Unlimited || safety == 0 || s.Available >= safety {
		_, e = tx.Exec("UPDATE inv_items SET low_since=NULL WHERE id=?", itemID)
		return e
	}
	if low.Valid {
		return nil
	}
	if _, e = tx.Exec("UPDATE inv_items SET low_since=? WHERE id=?", core.Now(), itemID); e != nil {
		return e
	}
	return core.AuditTx(tx, &http.Request{}, "inventory.low_stock", "inv_item", itemID, core.Object{"code": code, "name": name, "available": s.Available, "safety_stock": safety})
}

// StockIn creates a batch and its ledger entry in the caller's transaction.
// The calling module records its domain audit action before committing.
func StockIn(tx *sql.Tx, in In) (batchID int64, err error) {
	err = atomic(tx, func() error { var e error; batchID, e = stockIn(tx, in, true); return e })
	if err != nil {
		batchID = 0
	}
	return
}
func stockIn(tx *sql.Tx, in In, alert bool) (int64, error) {
	if in.Qty <= 0 || in.Cost < 0 || in.SupplierID < 0 || utf8.RuneCountInString(in.Grade) > 20 {
		return 0, bad(400, "入库数量、成本或等级不正确")
	}
	switch in.Kind {
	case "purchase_in", "return_in", "other_in", "count_gain", "transfer_in":
	default:
		return 0, bad(400, "入库类型不正确")
	}
	_, _, arch, e := item(tx, in.ItemID)
	if e != nil {
		return 0, e
	}
	if arch {
		return 0, bad(409, "物料已归档")
	}
	typ, e := warehouse(tx, in.WarehouseID)
	if e != nil {
		return 0, e
	}
	if typ == "dropship" {
		return 0, bad(400, "代发仓不能入库")
	}
	cur, e := currency(in.Currency)
	if e != nil {
		return 0, e
	}
	at, e := received(in.ReceivedAt)
	if e != nil {
		return 0, e
	}
	// Check aggregate representability before SQLite can promote integer addition to REAL.
	s, e := Stock(tx, in.ItemID)
	if e != nil {
		return 0, e
	}
	if _, e = add(s.OnHand, in.Qty); e != nil {
		return 0, e
	}
	var costValue int64
	if e = tx.QueryRow("SELECT coalesce(sum(cost_left),0) FROM inv_batches WHERE item_id=?", in.ItemID).Scan(&costValue); e != nil {
		return 0, e
	}
	if _, e = add(costValue, in.Cost); e != nil {
		return 0, e
	}
	res, e := tx.Exec(`INSERT INTO inv_batches(item_id,warehouse_id,qty_in,qty_left,cost_in,cost_left,currency,grade,supplier_id,received_at,ref_type,ref_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, in.ItemID, in.WarehouseID, in.Qty, in.Qty, in.Cost, in.Cost, cur, in.Grade, nullable(in.SupplierID), at, in.RefType, in.RefID, core.Now())
	if e != nil {
		return 0, e
	}
	id, e := res.LastInsertId()
	if e != nil {
		return 0, e
	}
	_, e = tx.Exec(`INSERT INTO inv_ledger(item_id,warehouse_id,batch_id,kind,qty,cost,currency,ref_type,ref_id,note,user_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, in.ItemID, in.WarehouseID, id, in.Kind, in.Qty, in.Cost, cur, in.RefType, in.RefID, in.Note, in.UserID, core.Now())
	if e == nil && alert {
		e = AfterChange(tx, in.ItemID)
	}
	return id, e
}

// StockOut consumes one specified batch or FIFO batches and records signed
// ledger entries. An error leaves the caller's transaction unchanged.
func StockOut(tx *sql.Tx, out Out) (taken []Taken, err error) {
	err = atomic(tx, func() error { var e error; taken, e = stockOut(tx, out, false, true); return e })
	if err != nil {
		taken = nil
	}
	return
}
func stockOut(tx *sql.Tx, out Out, transfer, alert bool) ([]Taken, error) {
	if out.Qty <= 0 || out.BatchID < 0 {
		return nil, bad(400, "出库数量或批次不正确")
	}
	switch out.Kind {
	case "sale_out", "other_out", "scrap", "count_loss", "transfer_out":
	default:
		return nil, bad(400, "出库类型不正确")
	}
	code, unit, _, e := item(tx, out.ItemID)
	if e != nil {
		return nil, e
	}
	typ, e := warehouse(tx, out.WarehouseID)
	if e != nil {
		return nil, e
	}
	// Reservations are company-wide, including finite dropship stock. Callers
	// shipping an order must release its reservation in this transaction first.
	s, e := Stock(tx, out.ItemID)
	if e != nil {
		return nil, e
	}
	if !transfer && !s.Unlimited && s.Available < out.Qty {
		return nil, availableShort(code, unit, out.Qty, s.Available)
	}
	taken := []Taken{}
	if typ == "dropship" {
		if out.BatchID != 0 {
			return nil, bad(400, "代发仓没有批次")
		}
		var qty, price sql.NullInt64
		var cur string
		e = tx.QueryRow("SELECT qty,unit_cost,currency FROM inv_dropship WHERE warehouse_id=? AND item_id=?", out.WarehouseID, out.ItemID).Scan(&qty, &price, &cur)
		if errors.Is(e, sql.ErrNoRows) {
			return nil, short(code, unit, out.Qty)
		}
		if e != nil {
			return nil, e
		}
		if qty.Valid && qty.Int64 < out.Qty {
			return nil, short(code, unit, out.Qty-qty.Int64)
		}
		cost, e := rounded(price.Int64, out.Qty, 1)
		if e != nil {
			return nil, e
		}
		if qty.Valid {
			if _, e = tx.Exec("UPDATE inv_dropship SET qty=qty-?,updated_at=? WHERE warehouse_id=? AND item_id=?", out.Qty, core.Now(), out.WarehouseID, out.ItemID); e != nil {
				return nil, e
			}
		}
		if e = outLedger(tx, out, 0, out.Qty, cost, cur); e != nil {
			return nil, e
		}
		taken = append(taken, Taken{Qty: out.Qty, Cost: cost})
	} else {
		rows, e := objects(tx, "SELECT id,qty_left,cost_left,currency FROM inv_batches WHERE item_id=? AND warehouse_id=? AND qty_left>0 AND (?=0 OR id=?) ORDER BY received_at,id", out.ItemID, out.WarehouseID, out.BatchID, out.BatchID)
		if e != nil {
			return nil, e
		}
		var total int64
		for _, b := range rows {
			total, e = add(total, b["qty_left"].(int64))
			if e != nil {
				return nil, e
			}
		}
		if total < out.Qty {
			return nil, short(code, unit, out.Qty-total)
		}
		left := out.Qty
		for _, b := range rows {
			if left == 0 {
				break
			}
			id := b["id"].(int64)
			qty := b["qty_left"].(int64)
			cost := b["cost_left"].(int64)
			n := min(left, qty)
			if n != qty {
				cost, e = rounded(cost, n, qty)
				if e != nil {
					return nil, e
				}
			}
			if _, e = tx.Exec("UPDATE inv_batches SET qty_left=qty_left-?,cost_left=cost_left-? WHERE id=?", n, cost, id); e != nil {
				return nil, e
			}
			if e = outLedger(tx, out, id, n, cost, b["currency"].(string)); e != nil {
				return nil, e
			}
			taken = append(taken, Taken{id, n, cost})
			left -= n
		}
	}
	if alert {
		e = AfterChange(tx, out.ItemID)
	}
	return taken, e
}
func outLedger(tx *sql.Tx, o Out, id, qty, cost int64, cur string) error {
	_, e := tx.Exec(`INSERT INTO inv_ledger(item_id,warehouse_id,batch_id,kind,qty,cost,currency,ref_type,ref_id,note,user_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, o.ItemID, o.WarehouseID, nullable(id), o.Kind, -qty, -cost, cur, o.RefType, o.RefID, o.Note, o.UserID, core.Now())
	return e
}

// Reserve adds to the reference's existing reservation, refusing finite shortages.
func Reserve(tx *sql.Tx, itemID, qty int64, refType string, refID int64) error {
	return atomic(tx, func() error {
		if qty <= 0 || refType == "" || refID <= 0 {
			return bad(400, "占用参数不正确")
		}
		code, unit, arch, e := item(tx, itemID)
		if e != nil {
			return e
		}
		if arch {
			return bad(409, "物料已归档")
		}
		s, e := Stock(tx, itemID)
		if e != nil {
			return e
		}
		if !s.Unlimited && s.Available < qty {
			return availableShort(code, unit, qty, s.Available)
		}
		if _, e = add(s.Reserved, qty); e != nil {
			return e
		}
		_, e = tx.Exec(`INSERT INTO inv_reservations(item_id,qty,ref_type,ref_id,created_at) VALUES(?,?,?,?,?) ON CONFLICT(ref_type,ref_id,item_id) DO UPDATE SET qty=qty+excluded.qty`, itemID, qty, refType, refID, core.Now())
		if e != nil {
			return e
		}
		return AfterChange(tx, itemID)
	})
}

// Release is idempotent; itemID zero releases all items belonging to the reference.
func Release(tx *sql.Tx, refType string, refID, itemID int64) error {
	return atomic(tx, func() error {
		rows, e := objects(tx, "SELECT item_id FROM inv_reservations WHERE ref_type=? AND ref_id=? AND (?=0 OR item_id=?)", refType, refID, itemID, itemID)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("DELETE FROM inv_reservations WHERE ref_type=? AND ref_id=? AND (?=0 OR item_id=?)", refType, refID, itemID, itemID); e != nil {
			return e
		}
		for _, r := range rows {
			if e = AfterChange(tx, r["item_id"].(int64)); e != nil {
				return e
			}
		}
		return nil
	})
}
