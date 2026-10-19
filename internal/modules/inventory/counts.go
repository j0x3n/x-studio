package inventory

import (
	"database/sql"
	"net/http"
	"xstudio/internal/core"
)

func onHand(q Querier, itemID, warehouseID int64) (n int64, e error) {
	e = q.QueryRow("SELECT coalesce(sum(qty_left),0) FROM inv_batches WHERE item_id=? AND warehouse_id=?", itemID, warehouseID).Scan(&n)
	return
}
func countDraft(tx *sql.Tx, id int64) (core.Object, error) {
	o, e := one(tx, "SELECT * FROM inv_counts WHERE id=?", id)
	if e != nil {
		return nil, e
	}
	if o["status"] != "draft" {
		return nil, bad(409, "只有草稿盘点可以操作")
	}
	return o, nil
}
func countAdd(tx *sql.Tx, id, wid int64, ids []int64) error {
	for _, iid := range ids {
		if _, _, _, e := item(tx, iid); e != nil {
			return e
		}
		n, e := onHand(tx, iid, wid)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO inv_count_lines(count_id,item_id,system_qty) VALUES(?,?,?) ON CONFLICT(count_id,item_id) DO NOTHING", id, iid, n); e != nil {
			return e
		}
	}
	return nil
}
func (a *Module) createCount(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		WarehouseID int64    `json:"warehouse_id"`
		ItemIDs     *[]int64 `json:"item_ids"`
		Note        string   `json:"note"`
	}
	if e := decode(w, r, &in); e != nil {
		return nil, e
	}
	return a.change(r, "count.create", "inv_count", func(tx *sql.Tx) (int64, any, error) {
		typ, e := warehouse(tx, in.WarehouseID)
		if e != nil {
			return 0, nil, e
		}
		if typ == "dropship" {
			return 0, nil, bad(400, "代发仓不能盘点")
		}
		res, e := tx.Exec("INSERT INTO inv_counts(warehouse_id,note,created_by,created_at) VALUES(?,?,?,?)", in.WarehouseID, in.Note, core.CurrentUser(r).ID, core.Now())
		if e != nil {
			return 0, nil, e
		}
		id, e := res.LastInsertId()
		if e != nil {
			return 0, nil, e
		}
		ids := []int64{}
		if in.ItemIDs == nil {
			rows, err := objects(tx, "SELECT DISTINCT item_id FROM inv_batches WHERE warehouse_id=? AND qty_left>0", in.WarehouseID)
			if err != nil {
				return 0, nil, err
			}
			for _, o := range rows {
				ids = append(ids, o["item_id"].(int64))
			}
		} else {
			ids = *in.ItemIDs
		}
		if e = countAdd(tx, id, in.WarehouseID, ids); e != nil {
			return 0, nil, e
		}
		return id, core.Object{"id": id}, nil
	})
}
func (a *Module) listCounts(_ http.ResponseWriter, r *http.Request) (any, error) {
	query := `SELECT c.*,w.name AS warehouse_name,
 (SELECT count(*) FROM inv_count_lines l WHERE l.count_id=c.id) AS line_count,
 (SELECT count(*) FROM inv_count_lines l WHERE l.count_id=c.id AND l.counted_qty IS NOT NULL) AS counted_count,
 coalesce(nullif(u.display_name,''),u.username,'') AS created_by_name
 FROM inv_counts c JOIN inv_warehouses w ON w.id=c.warehouse_id LEFT JOIN users u ON u.id=c.created_by WHERE 1=1`
	args := []any{}
	for _, k := range []string{"warehouse_id", "status"} {
		if s := r.URL.Query().Get(k); s != "" {
			query += " AND c." + k + "=?"
			args = append(args, s)
		}
	}
	return a.queryPage(r, query+" ORDER BY c.id DESC", args...)
}
func (a *Module) getCount(_ http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	o, e := one(a.DB, `SELECT c.*,w.name AS warehouse_name,coalesce(nullif(u.display_name,''),u.username,'') AS created_by_name FROM inv_counts c JOIN inv_warehouses w ON w.id=c.warehouse_id LEFT JOIN users u ON u.id=c.created_by WHERE c.id=?`, id)
	if e != nil {
		return nil, e
	}
	rows, e := objects(a.DB, `SELECT l.*,i.code,i.name,(SELECT coalesce(sum(b.qty_left),0) FROM inv_batches b WHERE b.item_id=l.item_id AND b.warehouse_id=?) AS current_qty FROM inv_count_lines l JOIN inv_items i ON i.id=l.item_id WHERE count_id=? ORDER BY l.item_id`, o["warehouse_id"], id)
	if e != nil {
		return nil, e
	}
	for _, l := range rows {
		l["difference"] = nil
		if n, ok := l["counted_qty"].(int64); ok {
			if o["status"] == "done" {
				var actual int64
				if e = a.DB.QueryRow("SELECT coalesce(sum(qty),0) FROM inv_ledger WHERE ref_type='count' AND ref_id=? AND item_id=?", id, l["item_id"]).Scan(&actual); e != nil {
					return nil, e
				}
				l["difference"] = actual
			} else if o["status"] == "draft" {
				l["difference"] = n - l["current_qty"].(int64)
			}
		}
	}
	o["lines"] = redact(r, rows)
	return o, nil
}
func (a *Module) patchCount(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Lines []struct {
			ItemID     int64  `json:"item_id"`
			CountedQty *int64 `json:"counted_qty"`
			UnitCost   *int64 `json:"unit_cost"`
		} `json:"lines"`
		AddItemIDs []int64 `json:"add_item_ids"`
	}
	if e := decode(w, r, &in); e != nil {
		return nil, e
	}
	id := core.ID(r, "id")
	return a.change(r, "count.update", "inv_count", func(tx *sql.Tx) (int64, any, error) {
		o, e := countDraft(tx, id)
		if e != nil {
			return 0, nil, e
		}
		if e = countAdd(tx, id, o["warehouse_id"].(int64), in.AddItemIDs); e != nil {
			return 0, nil, e
		}
		for _, l := range in.Lines {
			if l.CountedQty != nil && *l.CountedQty < 0 || l.UnitCost != nil && *l.UnitCost < 0 {
				return 0, nil, bad(400, "盘点数量或成本不能为负数")
			}
			var res sql.Result
			if costAllowed(r) {
				res, e = tx.Exec("UPDATE inv_count_lines SET counted_qty=?,unit_cost=? WHERE count_id=? AND item_id=?", l.CountedQty, l.UnitCost, id, l.ItemID)
			} else {
				res, e = tx.Exec("UPDATE inv_count_lines SET counted_qty=? WHERE count_id=? AND item_id=?", l.CountedQty, id, l.ItemID)
			}
			if e != nil {
				return 0, nil, e
			}
			n, e := res.RowsAffected()
			if e != nil {
				return 0, nil, e
			}
			if n == 0 {
				return 0, nil, bad(404, "盘点行不存在，请先添加物料")
			}
		}
		return id, core.Object{"id": id}, nil
	})
}
func (a *Module) cancelCount(_ http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	return a.change(r, "count.cancel", "inv_count", func(tx *sql.Tx) (int64, any, error) {
		if _, e := countDraft(tx, id); e != nil {
			return 0, nil, e
		}
		_, e := tx.Exec("UPDATE inv_counts SET status='canceled',done_at=? WHERE id=?", core.Now(), id)
		return id, core.Object{"id": id}, e
	})
}
func (a *Module) finishCount(_ http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	return a.change(r, "count.finish", "inv_count", func(tx *sql.Tx) (int64, any, error) {
		o, e := countDraft(tx, id)
		if e != nil {
			return 0, nil, e
		}
		wid := o["warehouse_id"].(int64)
		uid := core.CurrentUser(r).ID
		rows, e := objects(tx, "SELECT * FROM inv_count_lines WHERE count_id=? AND counted_qty IS NOT NULL ORDER BY item_id", id)
		if e != nil {
			return 0, nil, e
		}
		var gain, loss, gainCost, lossCost, ledgerCount int64
		for _, l := range rows {
			iid := l["item_id"].(int64)
			current, err := onHand(tx, iid, wid)
			if err != nil {
				return 0, nil, err
			}
			diff := l["counted_qty"].(int64) - current
			if diff > 0 {
				cur := "CNY"
				var price int64
				// The most recently created batch supplies both the default unit cost and currency.
				last, err := one(tx, "SELECT cost_in,qty_in,currency FROM inv_batches WHERE item_id=? ORDER BY id DESC LIMIT 1", iid)
				if err == nil {
					cur = last["currency"].(string)
					price, err = rounded(last["cost_in"].(int64), 1, last["qty_in"].(int64))
					if err != nil {
						return 0, nil, err
					}
				} else if err != sql.ErrNoRows {
					return 0, nil, err
				}
				if p, ok := l["unit_cost"].(int64); ok {
					price = p
				}
				cost, err := rounded(price, diff, 1)
				if err != nil {
					return 0, nil, err
				}
				if _, err = StockIn(tx, In{ItemID: iid, WarehouseID: wid, Qty: diff, Cost: cost, Currency: cur, Kind: "count_gain", RefType: "count", RefID: id, Note: o["note"].(string), UserID: uid}); err != nil {
					return 0, nil, err
				}
				gain, err = add(gain, diff)
				if err != nil {
					return 0, nil, err
				}
				gainCost, err = add(gainCost, cost)
				if err != nil {
					return 0, nil, err
				}
				ledgerCount++
			} else if diff < 0 {
				taken, err := StockOut(tx, Out{ItemID: iid, WarehouseID: wid, Qty: -diff, Kind: "count_loss", RefType: "count", RefID: id, Note: o["note"].(string), UserID: uid})
				if err != nil {
					return 0, nil, err
				}
				loss, err = add(loss, -diff)
				if err != nil {
					return 0, nil, err
				}
				for _, t := range taken {
					lossCost, err = add(lossCost, t.Cost)
					if err != nil {
						return 0, nil, err
					}
				}
				ledgerCount += int64(len(taken))
			}
		}
		if _, e = tx.Exec("UPDATE inv_counts SET status='done',done_at=? WHERE id=?", core.Now(), id); e != nil {
			return 0, nil, e
		}
		result := core.Object{"id": id, "ledger_count": ledgerCount, "gain_qty": gain, "loss_qty": loss, "difference": gain - loss, "gain_cost": gainCost, "loss_cost": lossCost}
		redact(r, []core.Object{result})
		return id, result, nil
	})
}
