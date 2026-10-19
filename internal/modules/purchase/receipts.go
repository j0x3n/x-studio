package purchase

import (
	"database/sql"
	"net/http"
	"xstudio/internal/core"
	"xstudio/internal/modules/inventory"
)

type gradeIn struct {
	Grade string `json:"grade"`
	Qty   int64  `json:"qty"`
}
type receiptLineIn struct {
	LineID   int64     `json:"line_id"`
	Qty      int64     `json:"qty"`
	Rejected int64     `json:"rejected"`
	Grades   []gradeIn `json:"grades"`
}

func (a *Module) receive(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		WarehouseID int64           `json:"warehouse_id"`
		ReceivedAt  string          `json:"received_at"`
		Note        string          `json:"note"`
		Lines       []receiptLineIn `json:"lines"`
	}
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	if len(in.Lines) == 0 || len(in.Lines) > 200 || !validDate(in.ReceivedAt) || runeLen(in.Note) > 1000 {
		return nil, bad(400, "到货明细、日期或备注不正确")
	}
	if in.ReceivedAt == "" {
		in.ReceivedAt = core.Now()
	}
	id := core.ID(r, "id")
	return a.change(r, "receive", func(tx *sql.Tx) (int64, any, any, error) {
		po, err := one(tx, `SELECT p.*,s.name AS supplier_name FROM purchase_orders p JOIN suppliers s ON s.id=p.supplier_id WHERE p.id=?`, id)
		if err != nil {
			return 0, nil, nil, err
		}
		if po["status"] != "ordered" && po["status"] != "partial" {
			return 0, nil, nil, bad(409, "只有已下单或部分到货的采购单能登记到货")
		}
		var typ string
		var arch bool
		if err = tx.QueryRow("SELECT type,archived FROM inv_warehouses WHERE id=?", in.WarehouseID).Scan(&typ, &arch); err != nil {
			return 0, nil, nil, bad(400, "仓库不存在")
		}
		if arch || typ == "dropship" {
			return 0, nil, nil, bad(409, "到货必须入未归档的自有仓或档口")
		}
		res, err := tx.Exec("INSERT INTO purchase_receipts(po_id,warehouse_id,received_at,note,created_by,created_at) VALUES(?,?,?,?,?,?)", id, in.WarehouseID, in.ReceivedAt, in.Note, core.CurrentUser(r).ID, core.Now())
		if err != nil {
			return 0, nil, nil, err
		}
		rid, err := res.LastInsertId()
		if err != nil {
			return 0, nil, nil, err
		}
		seen := map[int64]bool{}
		auditLines := []core.Object{}
		for _, line := range in.Lines {
			if seen[line.LineID] || line.Qty <= 0 || line.Rejected < 0 || line.Rejected > line.Qty || len(line.Grades) > 200 {
				return 0, nil, nil, bad(400, "到货行重复或实收、不合格数量不正确")
			}
			seen[line.LineID] = true
			l, e := one(tx, "SELECT * FROM purchase_lines WHERE po_id=? AND id=?", id, line.LineID)
			if e != nil {
				return 0, nil, nil, e
			}
			good := line.Qty - line.Rejected
			if len(line.Grades) == 0 && good > 0 {
				line.Grades = []gradeIn{{Qty: good}}
			}
			if line.Grades == nil {
				line.Grades = []gradeIn{}
			}
			var gradeQty int64
			for _, g := range line.Grades {
				if g.Qty <= 0 || runeLen(g.Grade) > 20 {
					return 0, nil, nil, bad(400, "等级需要不超过 20 字，等级数量必须为正整数")
				}
				gradeQty, e = sum(gradeQty, g.Qty)
				if e != nil {
					return 0, nil, nil, e
				}
			}
			if gradeQty != good {
				return 0, nil, nil, bad(400, "等级数量合计必须等于合格数量")
			}
			received, e := sum(l["received_qty"].(int64), line.Qty)
			if e != nil {
				return 0, nil, nil, e
			}
			rejected, e := sum(l["rejected_qty"].(int64), line.Rejected)
			if e != nil {
				return 0, nil, nil, e
			}
			budget, e := sum(l["amount"].(int64), l["shipping_share"].(int64))
			if e != nil {
				return 0, nil, nil, e
			}
			var previousRejectedCost int64
			if e = tx.QueryRow("SELECT coalesce(sum(amount),0) FROM purchase_returns WHERE line_id=?", line.LineID).Scan(&previousRejectedCost); e != nil {
				return 0, nil, nil, e
			}
			goodCost, rejectedCost, e := receiptCosts(budget, l["qty"].(int64), received-rejected, rejected, l["accounted_cost"].(int64), previousRejectedCost)
			if e != nil {
				return 0, nil, nil, e
			}
			accounted := l["accounted_cost"].(int64) + goodCost + rejectedCost
			var usedQty, usedCost int64
			batches := []int64{}
			for _, g := range line.Grades {
				usedQty += g.Qty
				end, e := ratio(goodCost, usedQty, good)
				if e != nil {
					return 0, nil, nil, e
				}
				bid, e := inventory.StockIn(tx, inventory.In{ItemID: l["item_id"].(int64), WarehouseID: in.WarehouseID, Qty: g.Qty, Cost: end - usedCost, Currency: po["currency"].(string), Grade: g.Grade, SupplierID: po["supplier_id"].(int64), ReceivedAt: in.ReceivedAt, Kind: "purchase_in", RefType: "purchase_receipt", RefID: rid, Note: in.Note, UserID: core.CurrentUser(r).ID})
				if e != nil {
					return 0, nil, nil, e
				}
				usedCost = end
				batches = append(batches, bid)
			}
			if _, e = tx.Exec("INSERT INTO purchase_receipt_lines(receipt_id,line_id,qty,rejected,grades,batch_ids) VALUES(?,?,?,?,?,?)", rid, line.LineID, line.Qty, line.Rejected, core.JS(line.Grades), core.JS(batches)); e != nil {
				return 0, nil, nil, e
			}
			if _, e = tx.Exec("UPDATE purchase_lines SET received_qty=?,rejected_qty=?,accounted_cost=? WHERE id=?", received, rejected, accounted, line.LineID); e != nil {
				return 0, nil, nil, e
			}
			if line.Rejected > 0 {
				if _, e = tx.Exec("INSERT INTO purchase_returns(po_id,line_id,qty,amount,note,created_at) VALUES(?,?,?,?,?,?)", id, line.LineID, line.Rejected, rejectedCost, in.Note, core.Now()); e != nil {
					return 0, nil, nil, e
				}
			}
			auditLines = append(auditLines, core.Object{"item_id": l["item_id"], "qty": good})
		}
		var remaining int
		if err = tx.QueryRow("SELECT count(*) FROM purchase_lines WHERE po_id=? AND received_qty<qty", id).Scan(&remaining); err != nil {
			return 0, nil, nil, err
		}
		status := "partial"
		if remaining == 0 {
			status = "received"
		}
		_, err = tx.Exec("UPDATE purchase_orders SET status=?,updated_at=? WHERE id=?", status, core.Now(), id)
		return id, core.Object{"id": rid, "po_id": id, "status": status}, core.Object{"po_id": id, "no": po["no"], "supplier": po["supplier_name"], "lines": auditLines}, err
	})
}

// Rejections use cumulative half-up unit valuation. Good units use its
// complement, so a final all-rejected receipt neither strands a cent nor
// requires changing the cost of stock that may already have been shipped.
// Supplier extras can consume only unallocated budget; posted costs stay fixed.
func receiptCosts(budget, ordered, good, rejected, accounted, previousRejected int64) (int64, int64, error) {
	rejectedTarget, err := ratio(budget, min(rejected, ordered), ordered)
	if err != nil {
		return 0, 0, err
	}
	remainingValue, err := ratio(budget, max(0, ordered-good), ordered)
	if err != nil {
		return 0, 0, err
	}
	remaining := max(0, budget-accounted)
	rejectedCost := min(remaining, max(0, rejectedTarget-previousRejected))
	goodTarget := budget - remainingValue
	previousGood := accounted - previousRejected
	goodCost := min(remaining-rejectedCost, max(0, goodTarget-previousGood))
	return goodCost, rejectedCost, nil
}

func (a *Module) finishReturn(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Status string `json:"status"`
	}
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	if in.Status != "done" {
		return nil, bad(400, "退货状态只能改为 done")
	}
	id := core.ID(r, "id")
	return a.change(r, "return", func(tx *sql.Tx) (int64, any, any, error) {
		ret, err := one(tx, "SELECT * FROM purchase_returns WHERE id=?", id)
		if err != nil {
			return 0, nil, nil, err
		}
		if ret["status"] != "pending" {
			return 0, nil, nil, bad(409, "退货已完成")
		}
		_, err = tx.Exec("UPDATE purchase_returns SET status='done',done_at=? WHERE id=?", core.Now(), id)
		return ret["po_id"].(int64), core.Object{"id": id, "status": "done"}, core.Object{"return_id": id}, err
	})
}
