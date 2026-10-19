package orders

import (
	"net/url"
	"testing"
)

func TestOrderDateFiltersUseChineseDaysAndExactInstants(t *testing.T) {
	f := boot(t)
	iid := f.item(t, "DATE-BOUNDARIES")
	f.in(t, iid, 20, 200, "")
	ids := map[string]int64{}
	for label, timestamp := range map[string]string{
		"before": "2026-10-18T15:59:59.999999999Z",
		"start":  "2026-10-19T00:00:00+08:00",
		"offset": "2026-10-19T01:00:00+08:00",
		"date":   "2026-10-19",
		"nano1":  "2026-10-19T01:00:00.000000001Z",
		"nano2":  "2026-10-19T01:00:00.000000002Z",
		"last":   "2026-10-19T23:59:59.999999999+08:00",
		"after":  "2026-10-20T00:00:00+08:00",
	} {
		ids[label] = num(f.req(t, "POST", "/api/orders", obj{"shop_id": 1, "paid_at": timestamp, "lines": []obj{{"item_id": iid, "qty": 1, "price": 100}}}, 200)["id"])
	}
	for _, tc := range []struct {
		from, to string
		want     []string
	}{
		{"2026-10-19", "2026-10-19", []string{"start", "offset", "date", "nano1", "nano2", "last"}},
		{"2026-10-19T00:00:00Z", "2026-10-19", []string{"nano1", "nano2", "last"}},
		{"2026-10-19T01:00:00.000000001Z", "2026-10-18T18:00:00.000000001-07:00", []string{"nano1"}},
		{"2026-10-19T00:00:00+08:00", "2026-10-18T16:00:00Z", []string{"start", "date"}},
	} {
		query := url.Values{"from": {tc.from}, "to": {tc.to}}
		out := f.req(t, "GET", "/api/orders?"+query.Encode(), nil, 200)
		got := map[int64]bool{}
		for _, row := range out["items"].([]any) {
			got[num(row.(obj)["id"])] = true
		}
		if num(out["total"]) != int64(len(tc.want)) || len(got) != len(tc.want) {
			t.Fatalf("%v: got %v want %v", query, got, tc.want)
		}
		for _, label := range tc.want {
			if !got[ids[label]] {
				t.Fatalf("%v: missing %s", query, label)
			}
		}
	}
	id := ids["offset"]
	f.req(t, "PATCH", path(id), obj{"paid_at": "2026-10-19T01:00:00.123456789+08:00"}, 200)
	if got := f.req(t, "GET", path(id), nil, 200)["paid_at"]; got != "2026-10-18T17:00:00.123456789Z" {
		t.Fatal("PATCH did not normalize the instant", got)
	}
	f.req(t, "GET", "/api/orders?from=2026-02-30", nil, 400)
	f.req(t, "GET", "/api/orders?to=invalid", nil, 400)
}

func TestOrderDateMigrationNormalizesExistingRawValues(t *testing.T) {
	f := boot(t)
	iid := f.item(t, "LEGACY-DATES")
	f.in(t, iid, 3, 100, "")
	ids := []int64{f.order(t, iid, 1), f.order(t, iid, 1), f.order(t, iid, 1)}
	before := []string{"2026-10-19T01:00:00+08:00", "2026-10-19", "2026-10-18T16:00:00.000000001Z"}
	want := []string{"2026-10-18T17:00:00.000000000Z", "2026-10-18T16:00:00.000000000Z", "2026-10-18T16:00:00.000000001Z"}
	for i, id := range ids {
		f.exec(t, "UPDATE orders SET paid_at=? WHERE id=?", before[i], id)
	}
	tx, err := f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = New(f.c).Migrations()[1].Up(tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		if got := f.req(t, "GET", path(id), nil, 200)["paid_at"]; got != want[i] {
			t.Fatalf("migration: got %v want %s", got, want[i])
		}
	}
	if got := f.req(t, "GET", "/api/orders?from=2026-10-19&to=2026-10-19", nil, 200); num(got["total"]) != 3 {
		t.Fatal("legacy dates lost from the Chinese day", got)
	}
}
