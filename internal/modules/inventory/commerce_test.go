package inventory

import (
	"database/sql"
	"errors"
	"testing"
)

func TestFiniteDropshipCannotConsumeReservedStock(t *testing.T) {
	f := boot(t)
	iid, wid := f.item(t, "RESERVED-DROPSHIP"), f.wh(t, "dropship")
	f.req(t, "PUT", fmtDropship(wid, iid), obj{"qty": 2, "unit_cost": 50}, 200)
	f.tx(t, func(tx *sql.Tx) error { return Reserve(tx, iid, 2, "order", 10) })
	f.tx(t, func(tx *sql.Tx) error {
		_, err := StockOut(tx, Out{ItemID: iid, WarehouseID: wid, Qty: 1, Kind: "sale_out", RefType: "order", RefID: 11})
		if !errors.Is(err, ErrShort) {
			t.Fatalf("reserved dropship stock was consumed: %v", err)
		}
		return nil // Even committing after the error must leave stock untouched.
	})
	if f.level(t, iid).Dropship != 2 || f.level(t, iid).Reserved != 2 || f.n(t, "SELECT count(*) FROM inv_ledger") != 0 {
		t.Fatal("failed stock-out changed inventory")
	}
	f.tx(t, func(tx *sql.Tx) error {
		if err := Release(tx, "order", 10, 0); err != nil {
			return err
		}
		_, err := StockOut(tx, Out{ItemID: iid, WarehouseID: wid, Qty: 2, Kind: "sale_out", RefType: "order", RefID: 10})
		return err
	})
	if f.level(t, iid).Dropship != 0 || f.level(t, iid).Reserved != 0 || f.n(t, "SELECT sum(cost) FROM inv_ledger") != -100 {
		t.Fatal("normal shipment after release failed")
	}
}

func TestReferencesWithoutCommerceModules(t *testing.T) {
	f := boot(t)
	iid := f.item(t, "OPTIONAL-COMMERCE")
	f.tx(t, func(tx *sql.Tx) error {
		if _, err := StockIn(tx, In{ItemID: iid, WarehouseID: 1, Qty: 2, Kind: "purchase_in", RefType: "purchase_receipt", RefID: 123}); err != nil {
			return err
		}
		return Reserve(tx, iid, 1, "order", 456)
	})
	detail := f.req(t, "GET", path(iid), nil, 200)
	for key, label := range map[string]string{"ledger": "采购到货", "batches": "采购到货", "reservations": "订单"} {
		ref := detail[key].([]any)[0].(map[string]any)
		if ref["ref_label"] != label || ref["ref_path"] != "" {
			t.Fatal(key, ref)
		}
	}
	f.req(t, "GET", "/api/inventory/ledger", nil, 200)
	f.req(t, "GET", "/api/inventory/batches", nil, 200)
}
