package finance

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"xstudio/internal/core"
)

type payable struct {
	PurchaseID     int64  `json:"purchase_id"`
	No             string `json:"no"`
	SupplierID     int64  `json:"supplier_id"`
	SupplierName   string `json:"supplier_name"`
	Status         string `json:"status"`
	Currency       string `json:"currency"`
	OrderedAt      string `json:"ordered_at"`
	GoodsAmount    int64  `json:"goods_amount"`
	ShippingFee    int64  `json:"shipping_fee"`
	OrderAmount    int64  `json:"order_amount"`
	RejectedAmount int64  `json:"rejected_amount"`
	PaidAmount     int64  `json:"paid_amount"`
	Payable        int64  `json:"payable"`
}

type payment struct {
	ID           int64  `json:"id"`
	PurchaseID   int64  `json:"purchase_id"`
	PurchaseNo   string `json:"purchase_no,omitempty"`
	SupplierID   int64  `json:"supplier_id"`
	SupplierName string `json:"supplier_name,omitempty"`
	AccountID    int64  `json:"account_id"`
	Amount       int64  `json:"amount"`
	Currency     string `json:"currency"`
	Note         string `json:"note"`
	OccurredAt   string `json:"occurred_at"`
	EntryID      int64  `json:"entry_id"`
	CreatedAt    string `json:"created_at"`
}

func (a *Module) listPayables(_ http.ResponseWriter, r *http.Request) (any, error) {
	where := "p.status NOT IN ('draft','canceled')"
	args := []any{}
	if id, ok, err := queryID(r, "supplier_id"); err != nil {
		return nil, err
	} else if ok {
		where += " AND p.supplier_id=?"
		args = append(args, id)
	}
	if cur := strings.TrimSpace(r.URL.Query().Get("currency")); cur != "" {
		if !validCurrency(cur) {
			return nil, bad(400, "币种不正确")
		}
		where += " AND p.currency=?"
		args = append(args, cur)
	}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		where += " AND instr(p.no,?)>0"
		args = append(args, q)
	}
	if r.URL.Query().Get("unpaid") == "1" {
		where += " AND " + payableExpr + ">0"
	}
	var total int64
	if err := a.DB.QueryRow("SELECT count(*) FROM purchase_orders p JOIN suppliers s ON s.id=p.supplier_id WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	offset, size := core.Page(r)
	rows, err := a.DB.Query(payableSelect+" WHERE "+where+" ORDER BY "+payableExpr+" DESC, p.id DESC LIMIT ? OFFSET ?", append(append([]any{}, args...), size, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanPayables(rows)
	if err != nil {
		return nil, err
	}
	return core.Object{"items": items, "total": total}, nil
}

const payableExpr = `((SELECT coalesce(sum(amount),0) FROM purchase_lines l WHERE l.po_id=p.id)+p.shipping_fee-(SELECT coalesce(sum(amount),0) FROM purchase_returns r WHERE r.po_id=p.id)-(SELECT coalesce(sum(amount),0) FROM fin_payments f WHERE f.purchase_id=p.id))`

const payableSelect = `SELECT p.id,p.no,p.supplier_id,s.name,p.status,p.currency,p.ordered_at,p.shipping_fee,
 (SELECT coalesce(sum(amount),0) FROM purchase_lines l WHERE l.po_id=p.id),
 (SELECT coalesce(sum(amount),0) FROM purchase_returns r WHERE r.po_id=p.id),
 (SELECT coalesce(sum(amount),0) FROM fin_payments f WHERE f.purchase_id=p.id)
 FROM purchase_orders p JOIN suppliers s ON s.id=p.supplier_id`

func scanPayables(rows *sql.Rows) ([]payable, error) {
	items := []payable{}
	for rows.Next() {
		item, err := scanPayable(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanPayable(rows *sql.Rows) (payable, error) {
	var item payable
	err := rows.Scan(&item.PurchaseID, &item.No, &item.SupplierID, &item.SupplierName, &item.Status, &item.Currency, &item.OrderedAt, &item.ShippingFee, &item.GoodsAmount, &item.RejectedAmount, &item.PaidAmount)
	if err != nil {
		return item, err
	}
	order, err := addNonNeg(item.GoodsAmount, item.ShippingFee)
	if err != nil {
		return item, err
	}
	item.OrderAmount = order
	remain, err := addSigned(order, -item.RejectedAmount)
	if err != nil {
		return item, err
	}
	remain, err = addSigned(remain, -item.PaidAmount)
	if err != nil {
		return item, err
	}
	item.Payable = remain
	return item, nil
}

func loadPayable(q querier, id int64) (payable, error) {
	rows, err := q.Query(payableSelect+" WHERE p.id=?", id)
	if err != nil {
		return payable{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return payable{}, err
		}
		return payable{}, sql.ErrNoRows
	}
	item, err := scanPayable(rows)
	if err != nil {
		return payable{}, err
	}
	return item, rows.Err()
}

type paymentIn struct {
	AccountID  int64  `json:"account_id"`
	Amount     int64  `json:"amount"`
	Note       string `json:"note"`
	RequestKey string `json:"request_key"`
}

func (a *Module) createPayment(w http.ResponseWriter, r *http.Request) (any, error) {
	var in paymentIn
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	poID := core.ID(r, "purchase_id")
	in.Note = strings.TrimSpace(in.Note)
	in.RequestKey = strings.TrimSpace(in.RequestKey)
	if poID == 0 || in.AccountID < 1 || !validMoney(in.Amount, false) || !validText(in.Note, 500) || !validKey(in.RequestKey) {
		return nil, bad(400, "采购单、账户、金额、备注或请求键不正确")
	}
	sum := hashParts("payment", i64s(poID), i64s(in.AccountID), i64s(in.Amount), in.Note)
	u := core.CurrentUser(r)
	return a.write(r, "payment.create", "finance_payment", func(tx *sql.Tx) (int64, any, any, bool, error) {
		existing, ok, err := findPayment(tx, in.RequestKey)
		if err != nil {
			return 0, nil, nil, false, err
		}
		if ok {
			if existing.Hash != sum {
				return 0, nil, nil, false, bad(409, "相同请求键对应的内容不一致")
			}
			if _, err := a.visibleAccount(tx, u, in.AccountID); err != nil {
				return 0, nil, nil, false, bad(404, "账户不存在")
			}
			body, err := paymentBody(tx, existing.ID, true)
			return existing.ID, nil, body, false, err
		}
		po, err := loadPayable(tx, poID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, nil, nil, false, bad(404, "采购单不存在")
			}
			return 0, nil, nil, false, err
		}
		if po.Status == "draft" || po.Status == "canceled" {
			return 0, nil, nil, false, bad(409, "草稿或已取消的采购单不能付款")
		}
		if po.Payable < in.Amount {
			return 0, nil, nil, false, bad(409, "付款超过剩余应付")
		}
		acc, err := lockAccount(tx, in.AccountID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, nil, nil, false, bad(404, "账户不存在")
			}
			return 0, nil, nil, false, err
		}
		if !shopAllowedQ(tx, u, shopValue(acc.ShopID)) {
			return 0, nil, nil, false, bad(404, "账户不存在")
		}
		if acc.Currency != po.Currency {
			return 0, nil, nil, false, bad(409, "账户币种和采购单不一致")
		}
		now := stamp(time.Now())
		res, err := tx.Exec(`INSERT INTO fin_payments(purchase_id,supplier_id,account_id,amount,currency,note,occurred_at,request_key,params_hash,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, po.PurchaseID, po.SupplierID, acc.ID, in.Amount, po.Currency, in.Note, now, in.RequestKey, sum, u.ID, core.Now())
		if err != nil {
			if isUnique(err) {
				return 0, nil, nil, false, bad(409, "相同请求键对应的内容不一致")
			}
			return 0, nil, nil, false, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, nil, nil, false, err
		}
		acc, err = lockAccount(tx, acc.ID)
		if err != nil {
			return 0, nil, nil, false, err
		}
		entryID, err := postEntry(tx, acc, "out", in.Amount, "采购付款", in.Note, now, acc.ShopID, "payment", id, u.ID)
		if err != nil {
			return 0, nil, nil, false, err
		}
		if _, err = tx.Exec("UPDATE fin_payments SET entry_id=? WHERE id=?", entryID, id); err != nil {
			return 0, nil, nil, false, err
		}
		body, err := paymentBody(tx, id, false)
		if err != nil {
			return 0, nil, nil, false, err
		}
		return id, core.Object{"id": id, "purchase_id": po.PurchaseID, "account_id": acc.ID, "amount": in.Amount}, body, true, nil
	})
}

type paymentRow struct {
	ID   int64
	Hash string
}

func findPayment(q querier, key string) (paymentRow, bool, error) {
	var row paymentRow
	err := q.QueryRow("SELECT id,params_hash FROM fin_payments WHERE request_key=?", key).Scan(&row.ID, &row.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	return row, err == nil, err
}

func paymentBody(q querier, id int64, idempotent bool) (any, error) {
	item, err := loadPayment(q, id)
	if err != nil {
		return nil, err
	}
	po, err := loadPayable(q, item.PurchaseID)
	if err != nil {
		return nil, err
	}
	return core.Object{"idempotent": idempotent, "payment": item, "payable": po.Payable}, nil
}

func loadPayment(q querier, id int64) (payment, error) {
	var item payment
	err := q.QueryRow(`SELECT f.id,f.purchase_id,p.no,f.supplier_id,s.name,f.account_id,f.amount,f.currency,f.note,f.occurred_at,f.entry_id,f.created_at
 FROM fin_payments f JOIN purchase_orders p ON p.id=f.purchase_id JOIN suppliers s ON s.id=f.supplier_id WHERE f.id=?`, id).Scan(&item.ID, &item.PurchaseID, &item.PurchaseNo, &item.SupplierID, &item.SupplierName, &item.AccountID, &item.Amount, &item.Currency, &item.Note, &item.OccurredAt, &item.EntryID, &item.CreatedAt)
	return item, err
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE")
}

func (a *Module) supplierStatement(_ http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	if id == 0 {
		return nil, bad(404, "供应商不存在")
	}
	var name string
	var archived int
	err := a.DB.QueryRow("SELECT name,archived FROM suppliers WHERE id=?", id).Scan(&name, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, bad(404, "供应商不存在")
	}
	if err != nil {
		return nil, err
	}
	start, end, hasStart, hasEnd, err := dateSpan(r)
	if err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(payableSelect+" WHERE p.supplier_id=? AND p.status NOT IN ('draft','canceled') ORDER BY p.id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	purchases := []payable{}
	for rows.Next() {
		item, err := scanPayable(rows)
		if err != nil {
			return nil, err
		}
		if !inOptional(item.OrderedAt, start, end, hasStart, hasEnd) {
			continue
		}
		if len(purchases) >= 5000 {
			return nil, bad(400, "对账结果太多，请缩小日期范围")
		}
		purchases = append(purchases, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	pays, err := a.statementPayments(id, start, end, hasStart, hasEnd)
	if err != nil {
		return nil, err
	}
	return core.Object{
		"supplier":  core.Object{"id": id, "name": name, "archived": archived != 0},
		"from":      r.URL.Query().Get("from"),
		"to":        r.URL.Query().Get("to"),
		"purchases": purchases,
		"payments":  pays,
		"totals":    statementTotals(purchases, pays),
	}, nil
}

func (a *Module) statementPayments(supplierID int64, start, end time.Time, hasStart, hasEnd bool) ([]payment, error) {
	rows, err := a.DB.Query(`SELECT f.id,f.purchase_id,p.no,f.supplier_id,s.name,f.account_id,f.amount,f.currency,f.note,f.occurred_at,f.entry_id,f.created_at
 FROM fin_payments f JOIN purchase_orders p ON p.id=f.purchase_id JOIN suppliers s ON s.id=f.supplier_id
 WHERE f.supplier_id=? ORDER BY f.occurred_at, f.id`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []payment{}
	for rows.Next() {
		var item payment
		if err = rows.Scan(&item.ID, &item.PurchaseID, &item.PurchaseNo, &item.SupplierID, &item.SupplierName, &item.AccountID, &item.Amount, &item.Currency, &item.Note, &item.OccurredAt, &item.EntryID, &item.CreatedAt); err != nil {
			return nil, err
		}
		if !inOptional(item.OccurredAt, start, end, hasStart, hasEnd) {
			continue
		}
		if len(items) >= 5000 {
			return nil, bad(400, "对账结果太多，请缩小日期范围")
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type stmtTotal struct {
	Currency        string `json:"currency"`
	PurchaseAmount  int64  `json:"purchase_amount"`
	RejectedAmount  int64  `json:"rejected_amount"`
	PurchasePayable int64  `json:"purchase_payable"`
	PaidInPeriod    int64  `json:"paid_in_period"`
}

func statementTotals(purchases []payable, pays []payment) []stmtTotal {
	by := map[string]*stmtTotal{}
	order := []string{}
	get := func(cur string) *stmtTotal {
		if by[cur] == nil {
			by[cur] = &stmtTotal{Currency: cur}
			order = append(order, cur)
		}
		return by[cur]
	}
	for _, p := range purchases {
		t := get(p.Currency)
		t.PurchaseAmount += p.OrderAmount
		t.RejectedAmount += p.RejectedAmount
		t.PurchasePayable += p.Payable
	}
	for _, p := range pays {
		get(p.Currency).PaidInPeriod += p.Amount
	}
	out := make([]stmtTotal, 0, len(order))
	for _, cur := range order {
		out = append(out, *by[cur])
	}
	return out
}

func dateSpan(r *http.Request) (time.Time, time.Time, bool, bool, error) {
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	var start, end time.Time
	var hasStart, hasEnd bool
	var err error
	if from != "" {
		start, err = dayStart(from)
		if err != nil {
			return time.Time{}, time.Time{}, false, false, bad(400, "开始日期不正确")
		}
		hasStart = true
	}
	if to != "" {
		end, err = dayStart(to)
		if err != nil {
			return time.Time{}, time.Time{}, false, false, bad(400, "结束日期不正确")
		}
		end = end.AddDate(0, 0, 1)
		hasEnd = true
	}
	if hasStart && hasEnd && !start.Before(end) {
		return time.Time{}, time.Time{}, false, false, bad(400, "开始日期要早于结束日期")
	}
	return start, end, hasStart, hasEnd, nil
}

func inOptional(s string, start, end time.Time, hasStart, hasEnd bool) bool {
	if !hasStart && !hasEnd {
		return true
	}
	if strings.TrimSpace(s) == "" {
		return false
	}
	t, err := parseTime(s)
	if err != nil {
		return false
	}
	if hasStart && t.Before(start) {
		return false
	}
	if hasEnd && !t.Before(end) {
		return false
	}
	return true
}
