package inventory

import (
	"database/sql"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

func safeAdd(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, bad(400, "数量超出整数范围")
	}
	return a + b, nil
}

func safeSub(a, b int64) (int64, error) {
	if b < 0 && a > math.MaxInt64+b || b > 0 && a < math.MinInt64+b {
		return 0, bad(400, "数量超出整数范围")
	}
	return a - b, nil
}

func (a *Module) replenishment(_ http.ResponseWriter, r *http.Request) (any, error) {
	u := core.CurrentUser(r)
	canCost := u.Has(core.PurchaseManage) || u.Has(core.ProductCost)

	targetStr := strings.TrimSpace(r.URL.Query().Get("target"))
	var targetOverride *int64
	if targetStr != "" {
		val, err := strconv.ParseInt(targetStr, 10, 64)
		if err != nil || val < 0 || val > 1_000_000_000 {
			return nil, bad(400, "目标库存不正确")
		}
		targetOverride = &val
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	allMode := r.URL.Query().Get("all") == "1"

	query := `SELECT
  i.id, i.code, i.name, i.unit, i.safety_stock,
  (SELECT coalesce(sum(b.qty_left),0) FROM inv_batches b JOIN inv_warehouses w ON w.id=b.warehouse_id WHERE b.item_id=i.id AND w.type IN ('own','stall') AND w.archived=0) AS on_hand,
  (SELECT coalesce(sum(d.qty),0) FROM inv_dropship d JOIN inv_warehouses w ON w.id=d.warehouse_id WHERE d.item_id=i.id AND w.archived=0) AS dropship_qty,
  EXISTS(SELECT 1 FROM inv_dropship d JOIN inv_warehouses w ON w.id=d.warehouse_id WHERE d.item_id=i.id AND d.qty IS NULL AND w.archived=0) AS unlimited,
  (SELECT coalesce(sum(qty),0) FROM inv_reservations WHERE item_id=i.id) AS reserved,
  (SELECT coalesce(sum(max(0, l.qty - l.received_qty)), 0) FROM purchase_lines l JOIN purchase_orders po ON po.id=l.po_id WHERE l.item_id=i.id AND po.status IN ('ordered','partial')) AS incoming
FROM inv_items i
WHERE i.archived = 0`

	args := []any{}
	if q != "" {
		query += " AND (instr(lower(i.code), lower(?)) > 0 OR instr(lower(i.name), lower(?)) > 0)"
		args = append(args, q, q)
	}
	query += " ORDER BY i.id DESC"

	rows, err := a.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type rawItem struct {
		id          int64
		code        string
		name        string
		unit        string
		safetyStock int64
		onHand      int64
		dropshipQty int64
		unlimited   bool
		reserved    int64
		incoming    int64
	}

	rawList := []rawItem{}
	for rows.Next() {
		var it rawItem
		var unl int
		if err := rows.Scan(&it.id, &it.code, &it.name, &it.unit, &it.safetyStock, &it.onHand, &it.dropshipQty, &unl, &it.reserved, &it.incoming); err != nil {
			return nil, err
		}
		it.unlimited = unl != 0
		rawList = append(rawList, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	matched := []core.Object{}
	for _, it := range rawList {
		if it.unlimited {
			continue
		}

		phys, err := safeAdd(it.onHand, it.dropshipQty)
		if err != nil {
			return nil, err
		}
		available, err := safeSub(phys, it.reserved)
		if err != nil {
			return nil, err
		}

		effectiveTarget := it.safetyStock
		if targetOverride != nil {
			effectiveTarget = *targetOverride
		}

		diff, err := safeSub(effectiveTarget, available)
		if err != nil {
			return nil, err
		}
		needed, err := safeSub(diff, it.incoming)
		if err != nil {
			return nil, err
		}

		suggestedQty := int64(0)
		if needed > 0 {
			suggestedQty = needed
		}

		if !allMode && suggestedQty <= 0 {
			continue
		}

		itemObj := core.Object{
			"item_id":       it.id,
			"code":          it.code,
			"name":          it.name,
			"unit":          it.unit,
			"available":     available,
			"safety_stock":  it.safetyStock,
			"incoming":      it.incoming,
			"suggested_qty": suggestedQty,
		}

		if canCost {
			currency := "CNY"
			var supID sql.NullInt64
			var supName sql.NullString
			var unitCost sql.NullInt64

			var poSupID, poAmount, poQty int64
			var poName string
			poErr := a.DB.QueryRow(`SELECT po.supplier_id, s.name, l.amount, l.qty, po.currency
FROM purchase_lines l
JOIN purchase_orders po ON po.id = l.po_id
JOIN suppliers s ON s.id = po.supplier_id
WHERE l.item_id = ? AND po.status <> 'canceled' AND s.archived = 0
ORDER BY po.id DESC
LIMIT 1`, it.id).Scan(&poSupID, &poName, &poAmount, &poQty, &currency)
			if poErr == nil {
				supID = sql.NullInt64{Int64: poSupID, Valid: true}
				supName = sql.NullString{String: poName, Valid: true}
				if poQty > 0 {
					cost, err := rounded(poAmount, 1, poQty)
					if err != nil {
						return nil, err
					}
					unitCost = sql.NullInt64{Int64: cost, Valid: true}
				}
			} else {
				var bSupID, bCost, bQty int64
				var bName string
				bErr := a.DB.QueryRow(`SELECT b.supplier_id, s.name, b.cost_in, b.qty_in, b.currency
FROM inv_batches b
JOIN suppliers s ON s.id = b.supplier_id
WHERE b.item_id = ? AND b.supplier_id IS NOT NULL AND s.archived = 0
ORDER BY b.received_at DESC, b.id DESC
LIMIT 1`, it.id).Scan(&bSupID, &bName, &bCost, &bQty, &currency)
				if bErr == nil {
					supID = sql.NullInt64{Int64: bSupID, Valid: true}
					supName = sql.NullString{String: bName, Valid: true}
					if bQty > 0 {
						cost, err := rounded(bCost, 1, bQty)
						if err != nil {
							return nil, err
						}
						unitCost = sql.NullInt64{Int64: cost, Valid: true}
					}
				} else {
					var dSupID, dCost int64
					var dName string
					dErr := a.DB.QueryRow(`SELECT w.supplier_id, s.name, d.unit_cost, d.currency
FROM inv_dropship d
JOIN inv_warehouses w ON w.id = d.warehouse_id
JOIN suppliers s ON s.id = w.supplier_id
WHERE d.item_id = ? AND w.supplier_id IS NOT NULL AND s.archived = 0 AND w.archived = 0
ORDER BY d.updated_at DESC
LIMIT 1`, it.id).Scan(&dSupID, &dName, &dCost, &currency)
					if dErr == nil {
						supID = sql.NullInt64{Int64: dSupID, Valid: true}
						supName = sql.NullString{String: dName, Valid: true}
						unitCost = sql.NullInt64{Int64: dCost, Valid: true}
					}
				}
			}

			if supID.Valid {
				itemObj["supplier_id"] = supID.Int64
			} else {
				itemObj["supplier_id"] = nil
			}
			if supName.Valid {
				itemObj["supplier_name"] = supName.String
			} else {
				itemObj["supplier_name"] = nil
			}
			if unitCost.Valid {
				itemObj["cost"] = unitCost.Int64
				itemObj["currency"] = currency
			} else {
				itemObj["cost"] = nil
			}
		}

		matched = append(matched, itemObj)
	}

	sort.Slice(matched, func(i, j int) bool {
		si := matched[i]["suggested_qty"].(int64)
		sj := matched[j]["suggested_qty"].(int64)
		if si != sj {
			return si > sj
		}
		return matched[i]["item_id"].(int64) > matched[j]["item_id"].(int64)
	})

	total := len(matched)
	offset, size := core.Page(r)
	offset = min(offset, total)
	pageItems := matched[offset:min(offset+size, total)]

	return core.Object{
		"items": pageItems,
		"total": total,
	}, nil
}
