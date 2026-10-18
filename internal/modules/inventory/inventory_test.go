package inventory

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/products"
)

type fixture struct {
	c      *core.Core
	cookie *http.Cookie
}
type obj = core.Object

func boot(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	c, e := core.New(core.Config{DataDir: filepath.Join(dir, "data"), PromptDir: filepath.Join(dir, "prompts"), TemplateDir: filepath.Join(dir, "templates"), SessionSecret: strings.Repeat("s", 32), AdminUser: "admin", AdminPassword: "password123", Workers: 1}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	if e = c.Start(accounts.New(c), products.New(c), New(c)); e != nil {
		t.Fatal(e)
	}
	f := &fixture{c: c}
	f.login(t, "admin")
	return f
}
func (f *fixture) raw(t *testing.T, method, path string, body any, status int) []byte {
	t.Helper()
	var b []byte
	if body != nil {
		b = []byte(core.JS(body))
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("X-Requested-With", "fetch")
	if f.cookie != nil {
		r.AddCookie(f.cookie)
	}
	w := httptest.NewRecorder()
	f.c.Handler().ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	return w.Body.Bytes()
}
func (f *fixture) req(t *testing.T, method, path string, body any, status int) obj {
	t.Helper()
	b := f.raw(t, method, path, body, status)
	var o obj
	if e := json.Unmarshal(b, &o); e != nil {
		t.Fatalf("response %s: %v", b, e)
	}
	return o
}
func (f *fixture) login(t *testing.T, name string) {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/login", strings.NewReader(core.JS(obj{"username": name, "password": "password123"})))
	r.Header.Set("X-Requested-With", "fetch")
	w := httptest.NewRecorder()
	f.c.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	f.cookie = w.Result().Cookies()[0]
}
func (f *fixture) exec(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	res, e := f.c.DB.Exec(q, args...)
	if e != nil {
		t.Fatalf("%s: %v", q, e)
	}
	id, e := res.LastInsertId()
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func (f *fixture) n(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if e := f.c.DB.QueryRow(q, args...).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func (f *fixture) user(t *testing.T, name, role string) int64 {
	t.Helper()
	hash, e := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if e != nil {
		t.Fatal(e)
	}
	return f.exec(t, "INSERT INTO users(username,password_hash,display_name,role_id,created_at) SELECT ?,?,?,id,? FROM roles WHERE key=?", name, hash, name, core.Now(), role)
}
func (f *fixture) item(t *testing.T, code string) int64 {
	t.Helper()
	return int64(f.req(t, "POST", "/api/inventory/items", obj{"code": code, "name": "物料 " + code, "kind": "sku", "unit": "个"}, 200)["id"].(float64))
}
func (f *fixture) wh(t *testing.T, typ string) int64 {
	t.Helper()
	return int64(f.req(t, "POST", "/api/inventory/warehouses", obj{"name": typ, "type": typ}, 200)["id"].(float64))
}
func (f *fixture) tx(t *testing.T, fn func(*sql.Tx) error) {
	t.Helper()
	if e := f.transaction(fn); e != nil {
		t.Fatal(e)
	}
}
func (f *fixture) transaction(fn func(*sql.Tx) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, e := f.c.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}
func (f *fixture) in(t *testing.T, iid, wid, qty, cost int64, at string) int64 {
	t.Helper()
	var id int64
	f.tx(t, func(tx *sql.Tx) error {
		var e error
		id, e = StockIn(tx, In{ItemID: iid, WarehouseID: wid, Qty: qty, Cost: cost, Kind: "other_in", Grade: "AA", SupplierID: 42, ReceivedAt: at, RefType: "manual", RefID: 1})
		return e
	})
	return id
}
func (f *fixture) out(t *testing.T, iid, wid, qty, bid int64) []Taken {
	t.Helper()
	var taken []Taken
	f.tx(t, func(tx *sql.Tx) error {
		var e error
		taken, e = StockOut(tx, Out{ItemID: iid, WarehouseID: wid, Qty: qty, BatchID: bid, Kind: "other_out", RefType: "manual", RefID: 2})
		return e
	})
	return taken
}
func (f *fixture) level(t *testing.T, id int64) Level {
	t.Helper()
	s, e := Stock(f.c.DB, id)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func path(id int64) string      { return fmt.Sprintf("/api/inventory/items/%d", id) }
func countPath(id int64) string { return fmt.Sprintf("/api/inventory/counts/%d", id) }
func rows(o obj) []any          { return o["items"].([]any) }

// Acceptance 2 and 8: the public API is exercised inside callers' transactions.
func TestStockBatchesFIFOExactCostsAndAtomicShort(t *testing.T) {
	f := boot(t)
	id := f.item(t, "R1")
	newer := f.in(t, id, 1, 2, 51, "2026-02-02")
	older := f.in(t, id, 1, 3, 100, "2026-01-01")
	for i, want := range []int64{33, 34, 33} {
		got := f.out(t, id, 1, 1, 0)
		if len(got) != 1 || got[0] != (Taken{older, 1, want}) {
			t.Fatalf("step %d: %+v", i, got)
		}
	}
	if n := f.n(t, "SELECT cost_left FROM inv_batches WHERE id=?", older); n != 0 {
		t.Fatal(n)
	}
	later := f.in(t, id, 1, 4, 101, "2026-03-03")
	if got := f.out(t, id, 1, 1, later); got[0] != (Taken{later, 1, 25}) {
		t.Fatal(got)
	}
	got := f.out(t, id, 1, 3, 0)
	if len(got) != 2 || got[0] != (Taken{newer, 2, 51}) || got[1] != (Taken{later, 1, 25}) {
		t.Fatal(got)
	}
	before := f.n(t, "SELECT count(*) FROM inv_ledger")
	f.tx(t, func(tx *sql.Tx) error {
		_, e := StockOut(tx, Out{ItemID: id, WarehouseID: 1, Qty: 3, Kind: "other_out"})
		if !errors.Is(e, ErrShort) || !strings.Contains(e.Error(), "R1") || !strings.Contains(e.Error(), "差 1 个") {
			t.Fatalf("short error: %v", e)
		}
		// Commit despite the error: StockOut must not have changed any rows.
		return nil
	})
	if f.n(t, "SELECT count(*) FROM inv_ledger") != before || f.level(t, id).OnHand != 2 {
		t.Fatal("short changed stock")
	}
	f.out(t, id, 1, 2, later)
	if n := f.n(t, "SELECT sum(cost) FROM inv_ledger WHERE item_id=?", id); n != 0 {
		t.Fatal("cost not conserved", n)
	}
	if n := f.n(t, "SELECT sum(qty) FROM inv_ledger WHERE item_id=?", id); n != 0 {
		t.Fatal("quantity not conserved", n)
	}
	if f.n(t, "SELECT count(*) FROM inv_batches WHERE qty_left<0 OR cost_left<0 OR (qty_left=0 AND cost_left<>0)") != 0 {
		t.Fatal("invalid batch")
	}
	f.tx(t, func(tx *sql.Tx) error {
		if _, e := StockIn(tx, In{ItemID: id, WarehouseID: 1, Qty: -1, Kind: "other_in"}); e == nil {
			t.Fatal("negative accepted")
		}
		return nil
	})
}

func TestStockCrossBatchAndSpecifiedShort(t *testing.T) {
	f := boot(t)
	id := f.item(t, "B1")
	b1 := f.in(t, id, 1, 2, 100, "2026-01-01")
	b2 := f.in(t, id, 1, 5, 300, "2026-01-02")
	f.tx(t, func(tx *sql.Tx) error {
		_, e := StockOut(tx, Out{ItemID: id, WarehouseID: 1, Qty: 3, BatchID: b1, Kind: "scrap"})
		if !errors.Is(e, ErrShort) {
			t.Fatal(e)
		}
		return nil
	})
	got := f.out(t, id, 1, 4, 0)
	if len(got) != 2 || got[0] != (Taken{b1, 2, 100}) || got[1] != (Taken{b2, 2, 120}) {
		t.Fatal(got)
	}
}

// Acceptance 3 and 8: accumulation, partial/all release and shipping order.
func TestReservationsAndShipping(t *testing.T) {
	f := boot(t)
	id := f.item(t, "S1")
	other := f.item(t, "S2")
	f.in(t, id, 1, 5, 100, "")
	f.in(t, other, 1, 2, 20, "")
	f.tx(t, func(tx *sql.Tx) error {
		if e := Reserve(tx, id, 2, "order", 10); e != nil {
			return e
		}
		return Reserve(tx, id, 2, "order", 10)
	})
	s := f.level(t, id)
	if s.Reserved != 4 || s.Available != 1 {
		t.Fatal(s)
	}
	f.tx(t, func(tx *sql.Tx) error {
		if e := Reserve(tx, id, 2, "order", 11); !errors.Is(e, ErrShort) {
			t.Fatal(e)
		}
		if _, e := StockOut(tx, Out{ItemID: id, WarehouseID: 1, Qty: 2, Kind: "other_out"}); !errors.Is(e, ErrShort) {
			t.Fatal(e)
		}
		return Reserve(tx, other, 1, "order", 10)
	})
	f.tx(t, func(tx *sql.Tx) error { return Release(tx, "order", 10, id) })
	if f.level(t, id).Reserved != 0 || f.level(t, other).Reserved != 1 {
		t.Fatal("item release")
	}
	f.tx(t, func(tx *sql.Tx) error {
		if e := Reserve(tx, id, 4, "order", 10); e != nil {
			return e
		}
		if e := Release(tx, "order", 10, 0); e != nil {
			return e
		}
		_, e := StockOut(tx, Out{ItemID: id, WarehouseID: 1, Qty: 4, Kind: "sale_out"})
		return e
	})
	if f.level(t, id).OnHand != 1 || f.level(t, other).Reserved != 0 {
		t.Fatal("ship/release all")
	}
	f.tx(t, func(tx *sql.Tx) error { return Release(tx, "order", 999, 0) })
}

func TestDropshipFiniteUnlimitedAndCosts(t *testing.T) {
	f := boot(t)
	id := f.item(t, "D1")
	wid := f.wh(t, "dropship")
	p := fmt.Sprintf("/api/inventory/dropship/%d/%d", wid, id)
	f.req(t, "PUT", p, obj{"qty": 3, "unit_cost": 17}, 200)
	got := f.out(t, id, wid, 2, 0)
	if len(got) != 1 || got[0] != (Taken{0, 2, 34}) {
		t.Fatal(got)
	}
	if f.level(t, id).Dropship != 1 {
		t.Fatal("finite qty not deducted")
	}
	f.tx(t, func(tx *sql.Tx) error {
		_, e := StockOut(tx, Out{ItemID: id, WarehouseID: wid, Qty: 2, Kind: "sale_out"})
		if !errors.Is(e, ErrShort) {
			t.Fatal(e)
		}
		return nil
	})
	f.req(t, "PUT", p, obj{"qty": nil, "unit_cost": 17}, 200)
	f.tx(t, func(tx *sql.Tx) error { return Reserve(tx, id, 100, "order", 1) })
	s := f.level(t, id)
	if !s.Unlimited || s.Available != -100 || s.Dropship != 0 {
		t.Fatal(s)
	}
	if got = f.out(t, id, wid, 1000, 0); got[0].Cost != 17000 {
		t.Fatal(got)
	}
	f.req(t, "PUT", p, obj{"qty": 1}, 200) // Supplier reports may expose a shortage against existing orders.
	if s = f.level(t, id); s.Unlimited || s.Available != -99 {
		t.Fatal(s)
	}
	f.tx(t, func(tx *sql.Tx) error { return Release(tx, "order", 1, 0) })
	f.req(t, "DELETE", p, nil, 200)
	f.tx(t, func(tx *sql.Tx) error {
		_, e := StockOut(tx, Out{ItemID: id, WarehouseID: wid, Qty: 1, Kind: "sale_out"})
		if !errors.Is(e, ErrShort) {
			t.Fatal(e)
		}
		return nil
	})
	f.tx(t, func(tx *sql.Tx) error {
		_, e := StockIn(tx, In{ItemID: id, WarehouseID: wid, Qty: 1, Kind: "other_in"})
		if e == nil {
			t.Fatal("dropship batch accepted")
		}
		return nil
	})
}

// Acceptance 4: twenty callers compete through the actual single connection.
func TestConcurrentReserveAndStockOut(t *testing.T) {
	for _, mode := range []string{"reserve", "out", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			f := boot(t)
			id := f.item(t, "C1")
			f.in(t, id, 1, 10, 100, "")
			start := make(chan struct{})
			results := make(chan error, 20)
			var wg sync.WaitGroup
			for i := 0; i < 20; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					results <- f.transaction(func(tx *sql.Tx) error {
						if mode == "reserve" || mode == "mixed" && i%2 == 0 {
							return Reserve(tx, id, 1, "order", int64(i+1))
						}
						_, e := StockOut(tx, Out{ItemID: id, WarehouseID: 1, Qty: 1, Kind: "sale_out"})
						return e
					})
				}(i)
			}
			close(start)
			wg.Wait()
			close(results)
			success := 0
			for e := range results {
				if e == nil {
					success++
				} else if !errors.Is(e, ErrShort) {
					t.Fatal(e)
				}
			}
			s := f.level(t, id)
			if success != 10 || s.OnHand < 0 || s.Available != 0 || s.Reserved > s.OnHand {
				t.Fatalf("success %d, %+v", success, s)
			}
		})
	}
}

// Acceptance 5: a transfer of fully reserved stock must still succeed.
func TestTransferPreservesStockCostMetadataAndAlerts(t *testing.T) {
	f := boot(t)
	id := f.item(t, "T1")
	wid := f.wh(t, "stall")
	b1 := f.in(t, id, 1, 3, 100, "2025-01-01")
	b2 := f.in(t, id, 1, 2, 77, "2025-02-01")
	f.req(t, "PATCH", path(id), obj{"safety_stock": 1}, 200)
	f.tx(t, func(tx *sql.Tx) error { return Reserve(tx, id, 5, "order", 1) })
	before := f.n(t, "SELECT count(*) FROM audit_log WHERE action='inventory.low_stock'")
	out := f.req(t, "POST", "/api/inventory/transfers", obj{"from": 1, "to": wid, "lines": []obj{{"item_id": id, "qty": 4}}}, 200)
	ref := int64(out["id"].(float64))
	if s := f.level(t, id); s.OnHand != 5 || s.Reserved != 5 {
		t.Fatal(s)
	}
	if n := f.n(t, "SELECT sum(cost_left) FROM inv_batches WHERE item_id=?", id); n != 177 {
		t.Fatal(n)
	}
	if f.n(t, "SELECT sum(qty) FROM inv_ledger WHERE ref_type='transfer' AND ref_id=?", ref) != 0 || f.n(t, "SELECT sum(cost) FROM inv_ledger WHERE ref_type='transfer' AND ref_id=?", ref) != 0 {
		t.Fatal("unbalanced transfer")
	}
	bs, e := objects(f.c.DB, "SELECT * FROM inv_batches WHERE warehouse_id=? ORDER BY id", wid)
	if e != nil {
		t.Fatal(e)
	}
	if len(bs) != 2 || bs[0]["source_batch_id"] != b1 || bs[1]["source_batch_id"] != b2 || bs[0]["grade"] != "AA" || bs[0]["supplier_id"] != int64(42) || bs[0]["received_at"] != "2025-01-01T00:00:00.000000000Z" || bs[0]["cost_in"] != int64(100) {
		t.Fatal(bs)
	}
	if f.n(t, "SELECT count(*) FROM audit_log WHERE action='inventory.low_stock'") != before {
		t.Fatal("transfer emitted transient alert")
	}
	f.req(t, "POST", "/api/inventory/transfers", obj{"from": 1, "to": wid, "lines": []obj{{"item_id": id, "qty": 1}, {"item_id": id, "qty": 1}}}, 409)
	if f.level(t, id).OnHand != 5 || f.n(t, "SELECT sum(qty_left) FROM inv_batches WHERE warehouse_id=1") != 1 {
		t.Fatal("partial transfer committed")
	}
}

func TestIntegerRoundingAndOverflow(t *testing.T) {
	for _, v := range []struct{ a, b, d, w int64 }{{100, 1, 3, 33}, {67, 1, 2, 34}, {math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64}, {1, 1, 2, 1}} {
		got, e := rounded(v.a, v.b, v.d)
		if e != nil || got != v.w {
			t.Fatalf("%+v: %d %v", v, got, e)
		}
	}
	if _, e := rounded(math.MaxInt64, 2, 1); e == nil {
		t.Fatal("overflow accepted")
	}
	if _, e := add(math.MaxInt64, 1); e == nil {
		t.Fatal("overflow accepted")
	}
}

func TestFIFOTimePrecisionAndEqualDateIDOrder(t *testing.T) {
	f := boot(t)
	id := f.item(t, "FIFO-TIME")
	newest := f.in(t, id, 1, 1, 11, "2026-01-01T00:00:00.11Z")
	middle := f.in(t, id, 1, 1, 22, "2026-01-01T00:00:00.1Z")
	oldest := f.in(t, id, 1, 1, 33, "2026-01-01T08:00:00+08:00")
	tie := f.in(t, id, 1, 1, 44, "2026-01-01T00:00:00.100Z")
	got := f.out(t, id, 1, 4, 0)
	for i, want := range []int64{oldest, middle, tie, newest} {
		if got[i].BatchID != want {
			t.Fatal(got)
		}
	}
}

func TestStockMutationSavepointRollsBackAuditFailure(t *testing.T) {
	f := boot(t)
	id := f.item(t, "AUDIT-FAIL")
	f.in(t, id, 1, 5, 100, "")
	f.req(t, "PATCH", path(id), obj{"safety_stock": 5}, 200)
	f.exec(t, `CREATE TRIGGER fail_low_audit BEFORE INSERT ON audit_log WHEN NEW.action='inventory.low_stock' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`)
	before := f.n(t, "SELECT count(*) FROM inv_ledger")
	f.tx(t, func(tx *sql.Tx) error {
		got, e := StockOut(tx, Out{ItemID: id, WarehouseID: 1, Qty: 1, Kind: "other_out"})
		if e == nil || got != nil {
			t.Fatal("audit failure was not returned", got, e)
		}
		if e = Reserve(tx, id, 1, "order", 1); e == nil {
			t.Fatal("reservation audit failure ignored")
		}
		return nil // Even an outer commit must preserve the state before failed calls.
	})
	if s := f.level(t, id); s.OnHand != 5 || s.Reserved != 0 || s.Available != 5 {
		t.Fatal(s)
	}
	if f.n(t, "SELECT count(*) FROM inv_ledger") != before || f.n(t, "SELECT count(*) FROM inv_items WHERE id=? AND low_since IS NULL", id) != 1 {
		t.Fatal("failed mutation partially persisted")
	}
}

func TestStockInRejectsAggregateIntegerOverflow(t *testing.T) {
	f := boot(t)
	id := f.item(t, "MAX-COST")
	f.in(t, id, 1, 1, math.MaxInt64, "")
	f.tx(t, func(tx *sql.Tx) error {
		if bid, e := StockIn(tx, In{ItemID: id, WarehouseID: 1, Qty: 1, Cost: 1, Kind: "other_in"}); e == nil || bid != 0 {
			t.Fatal("cost aggregate overflow", bid, e)
		}
		return nil
	})
	if f.level(t, id).OnHand != 1 {
		t.Fatal("overflow changed quantity")
	}
	if got := f.out(t, id, 1, 1, 0); got[0].Cost != math.MaxInt64 {
		t.Fatal(got)
	}
}
