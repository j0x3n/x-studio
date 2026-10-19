package inventory

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"xstudio/internal/core"
)

func (a *Module) listWarehouses(_ http.ResponseWriter, r *http.Request) (any, error) {
	return a.queryPage(r, `SELECT w.*,
 CASE WHEN w.type='dropship' THEN (SELECT count(*) FROM inv_dropship d WHERE d.warehouse_id=w.id AND (qty IS NULL OR qty>0))
 ELSE (SELECT count(DISTINCT item_id) FROM inv_batches b WHERE b.warehouse_id=w.id AND qty_left>0) END AS item_count,
 (SELECT coalesce(sum(cost_left),0) FROM inv_batches b WHERE b.warehouse_id=w.id) AS cost_value
 FROM inv_warehouses w ORDER BY sort,id`)
}

type warehouseInput struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	SupplierID *int64 `json:"supplier_id"`
	Address    string `json:"address"`
	Note       string `json:"note"`
	Sort       int64  `json:"sort"`
	Archived   bool   `json:"archived"`
}

func (a *Module) writeWarehouse(w http.ResponseWriter, r *http.Request) (any, error) {
	var raw json.RawMessage
	if e := decode(w, r, &raw); e != nil {
		return nil, e
	}
	id := core.ID(r, "id")
	return a.change(r, "warehouse.update", "inv_warehouse", func(tx *sql.Tx) (int64, any, error) {
		in := warehouseInput{Type: "own"}
		oldType := ""
		if r.Method == "PATCH" {
			o, e := one(tx, "SELECT * FROM inv_warehouses WHERE id=?", id)
			if e != nil {
				return 0, nil, e
			}
			if e = json.Unmarshal([]byte(core.JS(o)), &in); e != nil {
				return 0, nil, e
			}
			oldType = in.Type
		}
		if e := json.Unmarshal(raw, &in); e != nil {
			return 0, nil, bad(400, "仓库字段类型不正确")
		}
		if !length(in.Name, 1, 50) || (in.Type != "own" && in.Type != "stall" && in.Type != "dropship") || in.SupplierID != nil && *in.SupplierID <= 0 {
			return 0, nil, bad(400, "仓库名称、类型或供应商不正确")
		}
		if oldType != "" && oldType != in.Type {
			return 0, nil, bad(409, "仓库类型不能修改")
		}
		if in.Archived && id > 0 {
			var occupied bool
			if e := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM inv_batches WHERE warehouse_id=? AND qty_left>0) OR EXISTS(SELECT 1 FROM inv_dropship WHERE warehouse_id=? AND (qty IS NULL OR qty>0))`, id, id).Scan(&occupied); e != nil {
				return 0, nil, e
			}
			if occupied {
				return 0, nil, bad(409, "仓库有库存，不能归档")
			}
		}
		now := core.Now()
		args := []any{in.Name, in.Type, in.SupplierID, in.Address, in.Note, in.Sort, in.Archived, now}
		if r.Method == "POST" {
			args = append(args, now)
			res, e := tx.Exec(`INSERT INTO inv_warehouses(name,type,supplier_id,address,note,sort,archived,updated_at,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, args...)
			if e != nil {
				return 0, nil, e
			}
			id, e = res.LastInsertId()
			if e != nil {
				return 0, nil, e
			}
		} else {
			args = append(args, id)
			if _, e := tx.Exec(`UPDATE inv_warehouses SET name=?,type=?,supplier_id=?,address=?,note=?,sort=?,archived=?,updated_at=? WHERE id=?`, args...); e != nil {
				return 0, nil, e
			}
		}
		return id, core.Object{"id": id}, nil
	})
}
func (a *Module) listDropship(_ http.ResponseWriter, r *http.Request) (any, error) {
	query := `SELECT d.*,i.code,i.name,w.name AS warehouse_name FROM inv_dropship d JOIN inv_items i ON i.id=d.item_id JOIN inv_warehouses w ON w.id=d.warehouse_id WHERE 1=1`
	args := []any{}
	if s := r.URL.Query().Get("warehouse_id"); s != "" {
		query += " AND d.warehouse_id=?"
		args = append(args, s)
	}
	return a.queryPage(r, query+" ORDER BY d.warehouse_id,d.item_id", args...)
}
func (a *Module) writeDropship(w http.ResponseWriter, r *http.Request) (any, error) {
	var raw map[string]json.RawMessage
	if r.Method != "DELETE" {
		if e := decode(w, r, &raw); e != nil {
			return nil, e
		}
		if raw == nil {
			return nil, bad(400, "代发数据不能为空")
		}
	}
	wid, iid := core.ID(r, "warehouse_id"), core.ID(r, "item_id")
	return a.change(r, "dropship.update", "inv_item", func(tx *sql.Tx) (int64, any, error) {
		typ, e := warehouse(tx, wid)
		if e != nil {
			return 0, nil, e
		}
		if typ != "dropship" {
			return 0, nil, bad(400, "必须选择代发仓")
		}
		_, _, arch, e := item(tx, iid)
		if e != nil {
			return 0, nil, e
		}
		if arch {
			return 0, nil, bad(409, "物料已归档")
		}
		if r.Method == "DELETE" {
			_, e = tx.Exec("DELETE FROM inv_dropship WHERE warehouse_id=? AND item_id=?", wid, iid)
		} else {
			in := struct {
				Qty      *int64 `json:"qty"`
				UnitCost *int64 `json:"unit_cost"`
				Currency string `json:"currency"`
			}{}
			// Preserve secret costs when an operator without cost permission updates quantities.
			old, err := one(tx, "SELECT * FROM inv_dropship WHERE warehouse_id=? AND item_id=?", wid, iid)
			if err == nil {
				if err = json.Unmarshal([]byte(core.JS(old)), &in); err != nil {
					return 0, nil, err
				}
			} else if err != sql.ErrNoRows {
				return 0, nil, err
			}
			if !costAllowed(r) {
				delete(raw, "unit_cost")
				delete(raw, "currency")
			}
			if e = json.Unmarshal([]byte(core.JS(raw)), &in); e != nil {
				return 0, nil, bad(400, "代发字段类型不正确")
			}
			if in.Qty != nil && *in.Qty < 0 || in.UnitCost != nil && *in.UnitCost < 0 {
				return 0, nil, bad(400, "数量或成本不能为负数")
			}
			cur, err := currency(in.Currency)
			if err != nil {
				return 0, nil, err
			}
			_, e = tx.Exec(`INSERT INTO inv_dropship(warehouse_id,item_id,qty,unit_cost,currency,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(warehouse_id,item_id) DO UPDATE SET qty=excluded.qty,unit_cost=excluded.unit_cost,currency=excluded.currency,updated_at=excluded.updated_at`, wid, iid, in.Qty, in.UnitCost, cur, core.Now())
		}
		if e != nil {
			return 0, nil, e
		}
		// Supplier-reported availability may fall below existing reservations.
		// Keep that truthful shortage visible; new reservations and outbound
		// stock are subject to the availability guard in stock.go.
		if e = AfterChange(tx, iid); e != nil {
			return 0, nil, e
		}
		return iid, core.Object{"ok": true}, nil
	})
}
