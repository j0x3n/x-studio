package inventory

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"xstudio/internal/core"
)

// Inventory also runs without commerce modules (including in isolated tests).
// Resolve optional tables only when installed, and never expose a document
// number or path without the same permissions required by its detail endpoint.
func (a *Module) documentRefs(r *http.Request, rows []core.Object) error {
	if len(rows) == 0 {
		return nil
	}
	tableRows, err := objects(a.DB, "SELECT name FROM sqlite_master WHERE type='table' AND name IN ('purchase_orders','purchase_receipts','orders','order_returns','assembly_orders')")
	if err != nil {
		return err
	}
	tables := map[string]bool{}
	for _, row := range tableRows {
		tables[row["name"].(string)] = true
	}
	type source struct {
		kind string
		id   int64
	}
	type link struct{ label, path string }
	cache := map[source]link{}
	u := core.CurrentUser(r)
	for _, row := range rows {
		key := source{row["ref_type"].(string), row["ref_id"].(int64)}
		ref, ok := cache[key]
		if !ok {
			ref.label, ref.path, err = a.documentRef(u, key.kind, key.id, tables)
			if err != nil {
				return err
			}
			cache[key] = ref
		}
		row["ref_label"], row["ref_path"] = ref.label, ref.path
	}
	return nil
}

func (a *Module) documentRef(u core.User, kind string, id int64, tables map[string]bool) (string, string, error) {
	label := map[string]string{"manual": "手工出入库", "transfer": "调拨", "count": "盘点", "purchase_receipt": "采购到货", "order": "订单", "order_return": "售后退货", "assembly": "组装加工", "assembly_order": "组装加工"}[kind]
	if id <= 0 {
		return label, "", nil
	}
	var query string
	switch kind {
	case "assembly", "assembly_order":
		if u.Role.Scope != "shares" && u.Has(core.InventoryView) && tables["assembly_orders"] {
			var no string
			if err := a.DB.QueryRow("SELECT no FROM assembly_orders WHERE id=?", id).Scan(&no); err == nil {
				return label + " " + no, fmt.Sprintf("/assembly?order_id=%d", id), nil
			} else if !errors.Is(err, sql.ErrNoRows) {
				return "", "", err
			}
		}
	case "manual", "transfer":
		return fmt.Sprintf("%s #%d", label, id), "", nil
	case "count":
		var exists bool
		if err := a.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM inv_counts WHERE id=?)", id).Scan(&exists); err != nil {
			return "", "", err
		}
		if exists && u.Has(core.InventoryView) {
			return fmt.Sprintf("%s #%d", label, id), fmt.Sprintf("/inventory/counts/%d", id), nil
		}
	case "purchase_receipt":
		if u.Has(core.PurchaseManage) && tables["purchase_receipts"] && tables["purchase_orders"] {
			var poID int64
			var no string
			err := a.DB.QueryRow("SELECT p.id,p.no FROM purchase_receipts r JOIN purchase_orders p ON p.id=r.po_id WHERE r.id=?", id).Scan(&poID, &no)
			if err == nil {
				return label + " " + no, fmt.Sprintf("/purchase-orders/%d", poID), nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return "", "", err
			}
		}
	case "order":
		query = "SELECT id,no,shop_id FROM orders WHERE id=?"
	case "order_return":
		if tables["order_returns"] {
			query = "SELECT o.id,o.no,o.shop_id FROM order_returns r JOIN orders o ON o.id=r.order_id WHERE r.id=?"
		}
	}
	if query != "" && tables["orders"] && u.Has(core.OrderView) {
		var orderID, shopID int64
		var no string
		err := a.DB.QueryRow(query, id).Scan(&orderID, &no, &shopID)
		if err == nil && a.CanShop(u, shopID) {
			return label + " " + no, fmt.Sprintf("/orders/%d", orderID), nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", "", err
		}
	}
	return label, "", nil
}
