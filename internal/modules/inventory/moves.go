package inventory

import (
	"database/sql"
	"fmt"
	"net/http"
	"xstudio/internal/core"
)

type moveLine struct {
	ItemID     int64  `json:"item_id"`
	Qty        int64  `json:"qty"`
	Cost       int64  `json:"cost"`
	BatchID    int64  `json:"batch_id"`
	Grade      string `json:"grade"`
	ReceivedAt string `json:"received_at"`
	Currency   string `json:"currency"`
	SupplierID int64  `json:"supplier_id"`
}

func operation(tx *sql.Tx, kind string) (int64, error) {
	res, e := tx.Exec("INSERT INTO inv_operations(kind,created_at) VALUES(?,?)", kind, core.Now())
	if e != nil {
		return 0, e
	}
	return res.LastInsertId()
}
func (a *Module) moves(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Kind        string     `json:"kind"`
		WarehouseID int64      `json:"warehouse_id"`
		Lines       []moveLine `json:"lines"`
		Note        string     `json:"note"`
	}
	if e := decode(w, r, &in); e != nil {
		return nil, e
	}
	if len(in.Lines) == 0 || len(in.Lines) > 200 || (in.Kind != "other_in" && in.Kind != "other_out" && in.Kind != "scrap") {
		return nil, bad(400, "出入库类型不正确或行数不在 1–200 范围内")
	}
	return a.change(r, "move", "inv_operation", func(tx *sql.Tx) (int64, any, error) {
		id, e := operation(tx, "manual")
		if e != nil {
			return 0, nil, e
		}
		uid := core.CurrentUser(r).ID
		for _, l := range in.Lines {
			if in.Kind == "other_in" {
				if !costAllowed(r) {
					l.Cost = 0
				}
				_, e = StockIn(tx, In{ItemID: l.ItemID, WarehouseID: in.WarehouseID, Qty: l.Qty, Cost: l.Cost, Currency: l.Currency, Grade: l.Grade, SupplierID: l.SupplierID, ReceivedAt: l.ReceivedAt, Kind: in.Kind, RefType: "manual", RefID: id, Note: in.Note, UserID: uid})
			} else {
				_, e = StockOut(tx, Out{ItemID: l.ItemID, WarehouseID: in.WarehouseID, Qty: l.Qty, BatchID: l.BatchID, Kind: in.Kind, RefType: "manual", RefID: id, Note: in.Note, UserID: uid})
			}
			if e != nil {
				return 0, nil, e
			}
		}
		return id, core.Object{"id": id}, nil
	})
}
func (a *Module) transfers(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		From  int64      `json:"from"`
		To    int64      `json:"to"`
		Lines []moveLine `json:"lines"`
		Note  string     `json:"note"`
	}
	if e := decode(w, r, &in); e != nil {
		return nil, e
	}
	if in.From == in.To || len(in.Lines) == 0 || len(in.Lines) > 200 {
		return nil, bad(400, "调拨仓库或行数不正确")
	}
	return a.change(r, "transfer", "inv_operation", func(tx *sql.Tx) (int64, any, error) {
		for _, wid := range []int64{in.From, in.To} {
			typ, e := warehouse(tx, wid)
			if e != nil {
				return 0, nil, e
			}
			if typ == "dropship" {
				return 0, nil, bad(400, "代发仓不能调拨")
			}
		}
		id, e := operation(tx, "transfer")
		if e != nil {
			return 0, nil, e
		}
		uid := core.CurrentUser(r).ID
		for _, l := range in.Lines {
			taken, err := stockOut(tx, Out{ItemID: l.ItemID, WarehouseID: in.From, Qty: l.Qty, BatchID: l.BatchID, Kind: "transfer_out", RefType: "transfer", RefID: id, Note: in.Note, UserID: uid}, true, false)
			if err != nil {
				return 0, nil, err
			}
			for _, t := range taken {
				b, err := one(tx, "SELECT * FROM inv_batches WHERE id=?", t.BatchID)
				if err != nil {
					return 0, nil, err
				}
				var supplier int64
				if n, ok := b["supplier_id"].(int64); ok {
					supplier = n
				}
				bid, err := stockIn(tx, In{ItemID: l.ItemID, WarehouseID: in.To, Qty: t.Qty, Cost: t.Cost, Currency: b["currency"].(string), Grade: b["grade"].(string), SupplierID: supplier, ReceivedAt: b["received_at"].(string), Kind: "transfer_in", RefType: "transfer", RefID: id, Note: in.Note, UserID: uid}, false)
				if err != nil {
					return 0, nil, err
				}
				if _, err = tx.Exec("UPDATE inv_batches SET source_batch_id=? WHERE id=?", t.BatchID, bid); err != nil {
					return 0, nil, err
				}
			}
			if err = AfterChange(tx, l.ItemID); err != nil {
				return 0, nil, err
			}
		}
		return id, core.Object{"id": id}, nil
	})
}

const ledgerSelect = `SELECT l.*,i.code,i.name,i.name AS item_name,w.name AS warehouse_name,coalesce(nullif(u.display_name,''),u.username,'') AS user_name FROM inv_ledger l JOIN inv_items i ON i.id=l.item_id JOIN inv_warehouses w ON w.id=l.warehouse_id LEFT JOIN users u ON u.id=l.user_id`

func ledgerRefs(rows []core.Object) {
	// Frontend document routes and the purchase/orders modules are not installed
	// yet. Identify inventory-owned documents without inventing frontend paths.
	for _, o := range rows {
		o["ref_label"] = ""
		o["ref_path"] = ""
		if label, ok := map[string]string{"manual": "手工出入库", "transfer": "调拨", "count": "盘点"}[o["ref_type"].(string)]; ok && o["ref_id"].(int64) > 0 {
			o["ref_label"] = fmt.Sprintf("%s #%d", label, o["ref_id"])
		}
	}
}
func (a *Module) listLedger(_ http.ResponseWriter, r *http.Request) (any, error) {
	query := ledgerSelect + " WHERE 1=1"
	args := []any{}
	v := r.URL.Query()
	for _, k := range []string{"item_id", "warehouse_id", "kind", "ref_type", "ref_id"} {
		if s := v.Get(k); s != "" {
			query += " AND l." + k + "=?"
			args = append(args, s)
		}
	}
	if s := v.Get("from"); s != "" {
		query += " AND julianday(l.created_at)>=julianday(?)"
		args = append(args, s)
	}
	if s := v.Get("to"); s != "" {
		if len(s) == 10 {
			query += " AND julianday(l.created_at)<julianday(?,'+1 day')"
		} else {
			query += " AND julianday(l.created_at)<=julianday(?)"
		}
		args = append(args, s)
	}
	result, e := a.queryPage(r, query+" ORDER BY l.id DESC", args...)
	if e != nil {
		return nil, e
	}
	ledgerRefs(result["items"].([]core.Object))
	return result, nil
}
func (a *Module) listBatches(_ http.ResponseWriter, r *http.Request) (any, error) {
	query := `SELECT b.*,i.code,i.name,w.name AS warehouse_name FROM inv_batches b JOIN inv_items i ON i.id=b.item_id JOIN inv_warehouses w ON w.id=b.warehouse_id WHERE b.qty_left>0`
	args := []any{}
	for _, k := range []string{"item_id", "warehouse_id"} {
		if s := r.URL.Query().Get(k); s != "" {
			query += " AND b." + k + "=?"
			args = append(args, s)
		}
	}
	return a.queryPage(r, query+" ORDER BY b.received_at,b.id", args...)
}
