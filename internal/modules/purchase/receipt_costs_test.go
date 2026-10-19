package purchase

import (
	"fmt"
	"testing"
)

func TestReceiptRejectedUnitValueIsIndependentOfArrivalOrder(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	for i, tc := range []struct {
		qty, amount, rejected, stockCost, returnAmount int64
		steps                                          [][2]int64
	}{
		{3, 100, 1, 67, 33, [][2]int64{{1, 0}, {1, 1}, {1, 0}}},
		{3, 100, 1, 67, 33, [][2]int64{{1, 1}, {1, 0}, {1, 0}}},
		{3, 100, 1, 67, 33, [][2]int64{{2, 0}, {1, 1}}},
		// A half-cent belongs to rejection, even if the good unit arrives first.
		{2, 1, 1, 0, 1, [][2]int64{{1, 0}, {1, 1}}},
		{2, 1, 1, 0, 1, [][2]int64{{1, 1}, {1, 0}}},
		{4, 2, 2, 1, 1, [][2]int64{{1, 1}, {1, 0}, {1, 1}, {1, 0}}},
		// Both single and split over-receipts consume the same capped budget.
		{3, 100, 1, 67, 33, [][2]int64{{4, 1}}},
		{3, 100, 1, 67, 33, [][2]int64{{1, 1}, {3, 0}}},
		{3, 100, 3, 33, 67, [][2]int64{{1, 0}, {3, 3}}},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			iid := f.item(t, fmt.Sprintf("VALUATION-%d", i))
			id := f.po(t, 0, []object{{"item_id": iid, "qty": tc.qty, "amount": tc.amount}})
			lid := num(f.req(t, "GET", poPath(id), nil, 200)["lines"].([]any)[0].(object)["id"])
			f.req(t, "POST", poPath(id)+"/order", nil, 200)
			var received int64
			for _, step := range tc.steps {
				f.req(t, "POST", poPath(id)+"/receipts", object{"warehouse_id": 1, "lines": []object{{"line_id": lid, "qty": step[0], "rejected": step[1]}}}, 200)
				received += step[0]
				stock := int64(f.count(t, "SELECT coalesce(sum(cost_in),0) FROM inv_batches WHERE item_id=?", iid))
				refund := int64(f.count(t, "SELECT coalesce(sum(amount),0) FROM purchase_returns WHERE line_id=?", lid))
				accounted := int64(f.count(t, "SELECT accounted_cost FROM purchase_lines WHERE id=?", lid))
				if stock+refund != accounted || accounted > tc.amount {
					t.Fatalf("unposted or over-budget valuation: stock=%d refund=%d accounted=%d", stock, refund, accounted)
				}
			}
			if got := int64(f.count(t, "SELECT coalesce(sum(cost_in),0) FROM inv_batches WHERE item_id=?", iid)); got != tc.stockCost {
				t.Fatalf("stock cost=%d want=%d", got, tc.stockCost)
			}
			if got := int64(f.count(t, "SELECT coalesce(sum(amount),0) FROM purchase_returns WHERE line_id=?", lid)); got != tc.returnAmount {
				t.Fatalf("return amount=%d want=%d", got, tc.returnAmount)
			}
			line := f.req(t, "GET", poPath(id), nil, 200)["lines"].([]any)[0].(object)
			if num(line["received_qty"]) != received || num(line["rejected_qty"]) != tc.rejected || num(line["accounted_cost"]) != tc.amount {
				t.Fatal(line)
			}
		})
	}
}
