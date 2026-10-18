package inventory

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestItemValidationUnitsArchivingAndWarehouseRules(t *testing.T) {
	f := boot(t)
	id := f.item(t, "CASE-CODE")
	f.req(t, "POST", "/api/inventory/items", obj{"code": "case-code", "name": "重复", "kind": "sku", "unit": "个"}, 409)
	for _, body := range []obj{{"code": ""}, {"name": strings.Repeat("字", 101)}, {"unit": strings.Repeat("字", 9)}, {"safety_stock": -1}, {"units": []obj{{"name": "串", "factor": 0}}}, {"kind": "invalid"}, {"category_id": 9999}} {
		f.req(t, "PATCH", path(id), body, 400)
	}
	f.req(t, "PATCH", path(id), obj{"unit": "颗", "units": []obj{{"name": "串", "factor": 40}}, "spec": obj{"尺寸": "8mm"}}, 200)
	f.in(t, id, 1, 2, 100, "")
	f.req(t, "PATCH", path(id), obj{"unit": "对"}, 409)
	f.req(t, "PATCH", path(id), obj{"archived": true}, 409)
	f.req(t, "PATCH", "/api/inventory/warehouses/1", obj{"type": "stall"}, 409)
	f.req(t, "PATCH", "/api/inventory/warehouses/1", obj{"archived": true}, 409)
	f.out(t, id, 1, 2, 0)
	f.req(t, "PATCH", path(id), obj{"archived": true}, 200)
	if len(rows(f.req(t, "GET", "/api/inventory/items", nil, 200))) != 0 {
		t.Fatal("archived listed")
	}
	if len(rows(f.req(t, "GET", "/api/inventory/items?archived=1", nil, 200))) != 1 {
		t.Fatal("archived missing")
	}
	f.tx(t, func(tx *sql.Tx) error {
		_, e := StockIn(tx, In{ItemID: id, WarehouseID: 1, Qty: 1, Kind: "other_in"})
		if e == nil {
			t.Fatal("archived stock-in accepted")
		}
		return nil
	})
	f.req(t, "PATCH", "/api/inventory/warehouses/1", obj{"archived": true}, 200)
	f.req(t, "POST", "/api/inventory/moves", obj{"kind": "other_in", "warehouse_id": 1, "lines": []obj{{"item_id": id, "qty": 1}}}, 409)
}

func TestManualMovesWholeDocumentRollbackGroupingAndAudit(t *testing.T) {
	f := boot(t)
	id := f.item(t, "MANUAL1")
	other := f.item(t, "MANUAL2")
	move := f.req(t, "POST", "/api/inventory/moves", obj{"kind": "other_in", "warehouse_id": 1, "note": "期初库存", "lines": []obj{{"item_id": id, "qty": 3, "cost": 100}, {"item_id": other, "qty": 2, "cost": 23}}}, 200)
	ref := int64(move["id"].(float64))
	if f.n(t, "SELECT count(*) FROM inv_ledger WHERE ref_type='manual' AND ref_id=?", ref) != 2 || f.n(t, "SELECT count(*) FROM audit_log WHERE action='inventory.move' AND target_id=?", ref) != 1 {
		t.Fatal("document grouping/audit")
	}
	before := f.n(t, "SELECT count(*) FROM inv_ledger")
	f.req(t, "POST", "/api/inventory/moves", obj{"kind": "other_out", "warehouse_id": 1, "lines": []obj{{"item_id": id, "qty": 1}, {"item_id": other, "qty": 3}}}, 409)
	if f.n(t, "SELECT count(*) FROM inv_ledger") != before || f.level(t, id).OnHand != 3 {
		t.Fatal("partial manual document")
	}
	f.req(t, "POST", "/api/inventory/moves", obj{"kind": "scrap", "warehouse_id": 1, "lines": []obj{{"item_id": id, "qty": 1}}}, 200)
	f.req(t, "POST", "/api/inventory/moves", obj{"kind": "sale_out", "warehouse_id": 1, "lines": []obj{{"item_id": id, "qty": 1}}}, 400)
	f.req(t, "POST", "/api/inventory/moves", obj{"kind": "other_in", "warehouse_id": 1, "lines": make([]obj, 201)}, 400)
}

func TestListFiltersLookupPaginationLedgerAndCSV(t *testing.T) {
	f := boot(t)
	id := f.item(t, "FILTER-A")
	other := f.item(t, "FILTER-B")
	f.in(t, id, 1, 3, 100, "2026-01-01")
	f.in(t, other, 1, 4, 200, "2026-02-01")
	wid := f.wh(t, "stall")
	f.in(t, id, wid, 2, 40, "")
	f.req(t, "PATCH", path(id), obj{"safety_stock": 6, "spec": obj{"颜色": "白", "尺寸": "8mm"}}, 200)
	if o := f.req(t, "GET", "/api/inventory/items?page_size=1&page=2", nil, 200); o["total"] != float64(2) || len(rows(o)) != 1 {
		t.Fatal(o)
	}
	if o := f.req(t, "GET", "/api/inventory/items?q=filter-a&low=1&kind=sku", nil, 200); len(rows(o)) != 1 {
		t.Fatal(o)
	}
	if o := f.req(t, "GET", fmt.Sprintf("/api/inventory/items?warehouse_id=%d", wid), nil, 200); len(rows(o)) != 1 || rows(o)[0].(map[string]any)["on_hand"] != float64(2) {
		t.Fatal(o)
	}
	if o := f.req(t, "GET", "/api/inventory/items/lookup?codes=filter-a,MISSING,FILTER-B", nil, 200); o["total"] != float64(2) {
		t.Fatal(o)
	}
	if o := f.req(t, "GET", fmt.Sprintf("/api/inventory/batches?item_id=%d&warehouse_id=1", id), nil, 200); len(rows(o)) != 1 {
		t.Fatal(o)
	}
	ledger := f.req(t, "GET", fmt.Sprintf("/api/inventory/ledger?item_id=%d&warehouse_id=1&kind=other_in&ref_type=manual&ref_id=1&from=2000-01-01&to=2099-12-31", id), nil, 200)
	if len(rows(ledger)) != 1 {
		t.Fatal(ledger)
	}
	l := rows(ledger)[0].(map[string]any)
	if l["item_name"] != "物料 FILTER-A" || l["item_name"] != l["name"] {
		t.Fatal("ledger item_name alias", l)
	}
	detail := f.req(t, "GET", path(id), nil, 200)
	for _, entry := range detail["ledger"].([]any) {
		line := entry.(map[string]any)
		if line["item_name"] != "物料 FILTER-A" || line["item_name"] != line["name"] {
			t.Fatal("item detail ledger alias", line)
		}
	}
	for _, k := range []string{"code", "name", "warehouse_name", "user_name", "ref_label", "ref_path"} {
		if _, ok := l[k]; !ok {
			t.Fatal("missing", k)
		}
	}
	did := f.wh(t, "dropship")
	f.req(t, "PUT", fmtDropship(did, id), obj{"qty": nil}, 200)
	csvData := f.raw(t, "GET", "/api/inventory/export.csv?kind=sku", nil, 200)
	if !bytes.HasPrefix(csvData, []byte{0xef, 0xbb, 0xbf}) || !bytes.Contains(csvData, []byte("编码,名称,规格,可售数")) || !bytes.Contains(csvData, []byte("不限")) {
		t.Fatalf("csv: %s", csvData)
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(csvData), "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	specs := map[string]string{}
	for _, record := range records[1:] {
		specs[record[0]] = record[2]
	}
	if specs["FILTER-A"] != "尺寸 8mm · 颜色 白" || specs["FILTER-B"] != "" {
		t.Fatal("CSV spec text", specs)
	}
	count := f.count(t, 1, []int64{id})
	o := f.req(t, "GET", "/api/inventory/counts?warehouse_id=1&status=draft", nil, 200)
	if len(rows(o)) != 1 || rows(o)[0].(map[string]any)["id"] != float64(count) {
		t.Fatal(o)
	}
}

func TestUnicodeCodesCompareWithoutCaseAndKeepDisplay(t *testing.T) {
	f := boot(t)
	f.item(t, "SKU-Σ")
	f.req(t, "POST", "/api/inventory/items", obj{"code": "sku-ς", "name": "重复", "kind": "sku", "unit": "个"}, 409)
	o := f.req(t, "GET", "/api/inventory/items/lookup?codes=sku-σ", nil, 200)
	if len(rows(o)) != 1 || rows(o)[0].(map[string]any)["code"] != "SKU-Σ" {
		t.Fatal(o)
	}
	if _, ok := rows(o)[0].(map[string]any)["code_key"]; ok {
		t.Fatal("private key exposed")
	}
}

func TestItemPatchPreservesInt64UnitConversion(t *testing.T) {
	f := boot(t)
	id := f.item(t, "UNIT-INT64")
	f.req(t, "PATCH", path(id), obj{"units": []obj{{"name": "包", "factor": int64(math.MaxInt64)}}}, 200)
	f.req(t, "PATCH", path(id), obj{"name": "只改名称"}, 200)
	if n := f.n(t, "SELECT json_extract(units,'$[0].factor') FROM inv_items WHERE id=?", id); n != math.MaxInt64 {
		t.Fatal("conversion lost integer precision", n)
	}
}
