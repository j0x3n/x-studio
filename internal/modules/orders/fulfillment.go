package orders

import (
	"database/sql"
	"net/http"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/modules/inventory"
)

func (a *Module) ship(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Carrier    string `json:"carrier"`
		TrackingNo string `json:"tracking_no"`
		Lines      []struct {
			LineID      int64 `json:"line_id"`
			WarehouseID int64 `json:"warehouse_id"`
			BatchID     int64 `json:"batch_id"`
		} `json:"lines"`
	}
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	in.Carrier = strings.TrimSpace(in.Carrier)
	in.TrackingNo = strings.TrimSpace(in.TrackingNo)
	if runeLen(in.Carrier) > 100 || runeLen(in.TrackingNo) > 100 || len(in.Lines) == 0 || len(in.Lines) > 200 {
		return nil, bad(400, "快递信息或发货明细不正确")
	}
	id := core.ID(r, "id")
	return a.change(r, "ship", func(tx *sql.Tx) (int64, any, error) {
		o, err := accessible(tx, core.CurrentUser(r), id)
		if err != nil {
			return 0, nil, err
		}
		if o["status"] != "paid" {
			return 0, nil, bad(409, "只有待发货订单可以发货")
		}
		lines, err := objects(tx, "SELECT * FROM order_lines WHERE order_id=? ORDER BY id", id)
		if err != nil {
			return 0, nil, err
		}
		if len(lines) != len(in.Lines) {
			return 0, nil, bad(400, "必须为每一行明细选择一个发货仓库")
		}
		byID := map[int64]core.Object{}
		for _, l := range lines {
			byID[l["id"].(int64)] = l
		}
		if err = inventory.Release(tx, "order", id, 0); err != nil {
			return 0, nil, err
		}
		for _, pick := range in.Lines {
			l, ok := byID[pick.LineID]
			if !ok || pick.BatchID < 0 {
				return 0, nil, bad(400, "发货行重复、批次或订单明细不正确")
			}
			delete(byID, pick.LineID)
			taken, err := inventory.StockOut(tx, inventory.Out{ItemID: l["item_id"].(int64), WarehouseID: pick.WarehouseID, Qty: l["qty"].(int64), BatchID: pick.BatchID, Kind: "sale_out", RefType: "order", RefID: id, UserID: core.CurrentUser(r).ID})
			if err != nil {
				return 0, nil, err
			}
			lineCurrency := ""
			var lineCost int64
			for _, part := range taken {
				lineCost, err = sum(lineCost, part.Cost)
				if err != nil {
					return 0, nil, err
				}
				var currency, grade string
				if part.BatchID == 0 {
					err = tx.QueryRow("SELECT currency FROM inv_dropship WHERE item_id=? AND warehouse_id=?", l["item_id"], pick.WarehouseID).Scan(&currency)
				} else {
					err = tx.QueryRow("SELECT currency,grade FROM inv_batches WHERE id=?", part.BatchID).Scan(&currency, &grade)
				}
				if err != nil {
					return 0, nil, err
				}
				if lineCurrency != "" && lineCurrency != currency {
					return 0, nil, bad(409, "同一明细的发货批次必须使用相同成本币种，请指定批次")
				}
				lineCurrency = currency
				if _, err = tx.Exec("INSERT INTO order_picks(line_id,warehouse_id,batch_id,qty,cost,currency,grade) VALUES(?,?,?,?,?,?,?)", pick.LineID, pick.WarehouseID, nullable(part.BatchID), part.Qty, part.Cost, currency, grade); err != nil {
					return 0, nil, err
				}
			}
		}
		now := core.Now()
		_, err = tx.Exec("UPDATE orders SET status='shipped',short=0,carrier=?,tracking_no=?,shipped_at=?,updated_at=? WHERE id=?", in.Carrier, in.TrackingNo, now, now, id)
		return id, core.Object{"id": id, "status": "shipped"}, err
	})
}
func (a *Module) cancel(_ http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	return a.change(r, "cancel", func(tx *sql.Tx) (int64, any, error) {
		o, err := accessible(tx, core.CurrentUser(r), id)
		if err != nil {
			return 0, nil, err
		}
		if o["status"] != "paid" {
			return 0, nil, bad(409, "只有待发货订单可以取消")
		}
		if err = inventory.Release(tx, "order", id, 0); err != nil {
			return 0, nil, err
		}
		now := core.Now()
		_, err = tx.Exec("UPDATE orders SET status='canceled',short=0,canceled_at=?,updated_at=? WHERE id=?", now, now, id)
		return id, core.Object{"id": id, "status": "canceled"}, err
	})
}
func (a *Module) returnOrder(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		RefundAmount int64  `json:"refund_amount"`
		Reason       string `json:"reason"`
		Lines        []struct {
			LineID      int64  `json:"line_id"`
			Qty         int64  `json:"qty"`
			Action      string `json:"action"`
			WarehouseID int64  `json:"warehouse_id"`
		} `json:"lines"`
	}
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	if in.RefundAmount < 0 || runeLen(in.Reason) > 1000 || len(in.Lines) > 200 || len(in.Lines) == 0 && in.RefundAmount == 0 {
		return nil, bad(400, "退款金额、原因或售后明细不正确")
	}
	id := core.ID(r, "id")
	return a.change(r, "return", func(tx *sql.Tx) (int64, any, error) {
		o, err := accessible(tx, core.CurrentUser(r), id)
		if err != nil {
			return 0, nil, err
		}
		if o["status"] != "shipped" {
			return 0, nil, bad(409, "只有已发货订单可以登记售后")
		}
		var refunded int64
		if err = tx.QueryRow("SELECT coalesce(sum(refund_amount),0) FROM order_returns WHERE order_id=?", id).Scan(&refunded); err != nil {
			return 0, nil, err
		}
		refunded, err = sum(refunded, in.RefundAmount)
		if err != nil {
			return 0, nil, err
		}
		if refunded > o["paid_amount"].(int64) {
			return 0, nil, bad(409, "累计退款不能超过实收金额")
		}
		res, err := tx.Exec("INSERT INTO order_returns(order_id,refund_amount,reason,created_by,created_at) VALUES(?,?,?,?,?)", id, in.RefundAmount, in.Reason, core.CurrentUser(r).ID, core.Now())
		if err != nil {
			return 0, nil, err
		}
		rid, err := res.LastInsertId()
		if err != nil {
			return 0, nil, err
		}
		seen := map[int64]bool{}
		for _, line := range in.Lines {
			if seen[line.LineID] || line.Qty <= 0 || line.Action != "restock" && line.Action != "scrap" && line.Action != "none" {
				return 0, nil, bad(400, "售后行重复、数量或处理方式不正确")
			}
			seen[line.LineID] = true
			l, err := one(tx, "SELECT * FROM order_lines WHERE order_id=? AND id=?", id, line.LineID)
			if err != nil {
				return 0, nil, err
			}
			if line.Qty > l["qty"].(int64)-l["returned_qty"].(int64) {
				return 0, nil, bad(409, "售后数量超过尚未退回的发货数量")
			}
			picks, err := objects(tx, "SELECT * FROM order_picks WHERE line_id=? ORDER BY id", line.LineID)
			if err != nil {
				return 0, nil, err
			}
			var totalCost, accountedCost int64
			for _, pick := range picks {
				totalCost, err = sum(totalCost, pick["cost"].(int64))
				if err != nil {
					return 0, nil, err
				}
				accountedCost, err = sum(accountedCost, pick["returned_cost"].(int64))
				if err != nil {
					return 0, nil, err
				}
			}
			// Value returns against the whole line, carrying rounding remainders
			// across all after-sales actions. Grades still follow original picks.
			targetCost, err := ratio(totalCost, l["returned_qty"].(int64)+line.Qty, l["qty"].(int64))
			if err != nil {
				return 0, nil, err
			}
			returnCost := targetCost - accountedCost
			left := line.Qty
			var allocatedCost int64
			for _, pick := range picks {
				qty := min(left, pick["qty"].(int64)-pick["returned_qty"].(int64))
				if qty == 0 {
					continue
				}
				returned := pick["returned_qty"].(int64) + qty
				cumulative, err := ratio(returnCost, line.Qty-left+qty, line.Qty)
				if err != nil {
					return 0, nil, err
				}
				partCost := cumulative - allocatedCost
				allocatedCost = cumulative
				if line.Action == "restock" {
					_, err = inventory.StockIn(tx, inventory.In{ItemID: l["item_id"].(int64), WarehouseID: line.WarehouseID, Qty: qty, Cost: partCost, Currency: pick["currency"].(string), Grade: pick["grade"].(string), Kind: "return_in", RefType: "order_return", RefID: rid, Note: in.Reason, UserID: core.CurrentUser(r).ID})
					if err != nil {
						return 0, nil, err
					}
				}
				if _, err = tx.Exec("UPDATE order_picks SET returned_qty=?,returned_cost=returned_cost+? WHERE id=?", returned, partCost, pick["id"]); err != nil {
					return 0, nil, err
				}
				left -= qty
				if left == 0 {
					break
				}
			}
			if left != 0 {
				return 0, nil, bad(409, "发货批次数量不完整")
			}
			if _, err = tx.Exec("INSERT INTO order_return_lines(return_id,line_id,qty,action,warehouse_id) VALUES(?,?,?,?,?)", rid, line.LineID, line.Qty, line.Action, nullable(line.WarehouseID)); err != nil {
				return 0, nil, err
			}
			if _, err = tx.Exec("UPDATE order_lines SET returned_qty=returned_qty+? WHERE id=?", line.Qty, line.LineID); err != nil {
				return 0, nil, err
			}
		}
		_, err = tx.Exec("UPDATE orders SET updated_at=? WHERE id=?", core.Now(), id)
		return id, core.Object{"id": rid, "order_id": id}, err
	})
}
