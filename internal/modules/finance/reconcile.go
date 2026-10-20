package finance

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"xstudio/internal/core"
)

type reconRow struct {
	OrderNo           string `json:"order_no"`
	Amount            int64  `json:"amount"`
	Fee               int64  `json:"fee"`
	ShippingInsurance int64  `json:"shipping_insurance"`
	Refund            int64  `json:"refund"`
	SettledAt         string `json:"settled_at"`
}

type reconView struct {
	OrderNo           string `json:"order_no"`
	Amount            int64  `json:"amount"`
	Fee               int64  `json:"fee"`
	ShippingInsurance int64  `json:"shipping_insurance"`
	Refund            int64  `json:"refund"`
	SettledAt         string `json:"settled_at"`
	Status            string `json:"status"`
	Reason            string `json:"reason"`
	OrderID           *int64 `json:"order_id"`
	SystemNo          string `json:"system_no"`
	PlatformNo        string `json:"platform_no"`
	SystemAmount      *int64 `json:"system_amount"`
	SystemRefund      *int64 `json:"system_refund"`
	DiffAmount        *int64 `json:"diff_amount"`
	DiffRefund        *int64 `json:"diff_refund"`
}

type reconSummary struct {
	Total      int `json:"total"`
	Matched    int `json:"matched"`
	Unmatched  int `json:"unmatched"`
	Ambiguous  int `json:"ambiguous"`
	Currency   int `json:"currency"`
	Canceled   int `json:"canceled"`
	Duplicate  int `json:"duplicate"`
	Difference int `json:"difference"`
}

type reconBatch struct {
	ID        int64  `json:"id"`
	ShopID    int64  `json:"shop_id"`
	ShopName  string `json:"shop_name"`
	Currency  string `json:"currency"`
	RowCount  int64  `json:"row_count"`
	Inserted  int64  `json:"inserted"`
	Skipped   int64  `json:"skipped"`
	Matched   int64  `json:"matched"`
	CreatedAt string `json:"created_at"`
}

type previewIn struct {
	ShopID   int64      `json:"shop_id"`
	Currency string     `json:"currency"`
	Rows     []reconRow `json:"rows"`
}

type importIn struct {
	ShopID     int64      `json:"shop_id"`
	Currency   string     `json:"currency"`
	RequestKey string     `json:"request_key"`
	Rows       []reconRow `json:"rows"`
}

func normalizeRows(rows []reconRow) ([]reconRow, error) {
	if len(rows) < 1 || len(rows) > 1000 {
		return nil, bad(400, "对账需要 1–1000 行")
	}
	out := make([]reconRow, len(rows))
	for i, row := range rows {
		row.OrderNo = strings.TrimSpace(row.OrderNo)
		if row.OrderNo == "" || !validText(row.OrderNo, 100) || !validMoney(row.Amount, true) || !validMoney(row.Fee, true) || !validMoney(row.ShippingInsurance, true) || !validMoney(row.Refund, true) {
			return nil, bad(400, "订单号或金额不正确")
		}
		if strings.TrimSpace(row.SettledAt) == "" {
			return nil, bad(400, "结算日期不正确")
		}
		settled, err := chinaDate(row.SettledAt)
		if err != nil {
			return nil, bad(400, "结算日期不正确")
		}
		row.SettledAt = settled
		out[i] = row
	}
	return out, nil
}

func rowPrint(currency string, row reconRow) string {
	return hashParts("row", currency, row.OrderNo, i64s(row.Amount), i64s(row.Fee), i64s(row.ShippingInsurance), i64s(row.Refund), row.SettledAt)
}

func reconHash(shopID int64, currency string, rows []reconRow) string {
	parts := []string{"reconcile", i64s(shopID), currency}
	for _, row := range rows {
		parts = append(parts, row.OrderNo, i64s(row.Amount), i64s(row.Fee), i64s(row.ShippingInsurance), i64s(row.Refund), row.SettledAt)
	}
	return hashParts(parts...)
}

func summarize(rows []reconView) reconSummary {
	var s reconSummary
	s.Total = len(rows)
	for _, row := range rows {
		switch row.Status {
		case "matched":
			s.Matched++
			if row.DiffAmount != nil && *row.DiffAmount != 0 || row.DiffRefund != nil && *row.DiffRefund != 0 {
				s.Difference++
			}
		case "unmatched":
			s.Unmatched++
		case "ambiguous":
			s.Ambiguous++
		case "currency":
			s.Currency++
		case "canceled":
			s.Canceled++
		case "duplicate":
			s.Duplicate++
		}
	}
	return s
}

type orderHit struct {
	id         int64
	no         string
	platformNo string
	currency   string
	paid       int64
	status     string
}

func textArgs(parts []string) []any {
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = p
	}
	return out
}

func textChunks(ids []string, n int) [][]string {
	var out [][]string
	for len(ids) > 0 {
		if len(ids) < n {
			n = len(ids)
		}
		out = append(out, ids[:n])
		ids = ids[n:]
	}
	return out
}

func uniqText(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (a *Module) matchRows(q querier, shopID int64, currency string, rows []reconRow) ([]reconView, error) {
	nos := []string{}
	for _, row := range rows {
		nos = append(nos, row.OrderNo)
	}
	byNo := map[string]orderHit{}
	byPlatform := map[string]orderHit{}
	for _, part := range textChunks(uniqText(nos), 200) {
		ph := placeholders(len(part))
		args := append([]any{shopID}, textArgs(part)...)
		args = append(args, textArgs(part)...)
		found, err := q.Query("SELECT id,no,platform_no,currency,paid_amount,status FROM orders WHERE shop_id=? AND (no IN ("+ph+") OR (platform_no<>'' AND platform_no IN ("+ph+")))", args...)
		if err != nil {
			return nil, err
		}
		for found.Next() {
			var hit orderHit
			if err = found.Scan(&hit.id, &hit.no, &hit.platformNo, &hit.currency, &hit.paid, &hit.status); err != nil {
				found.Close()
				return nil, err
			}
			byNo[hit.no] = hit
			if hit.platformNo != "" {
				byPlatform[hit.platformNo] = hit
			}
		}
		if err = found.Err(); err != nil {
			found.Close()
			return nil, err
		}
		found.Close()
	}
	ids := []int64{}
	for _, hit := range byNo {
		ids = append(ids, hit.id)
	}
	for _, hit := range byPlatform {
		ids = append(ids, hit.id)
	}
	refunds := map[int64]int64{}
	for _, part := range chunks(uniq(ids), 200) {
		ph := placeholders(len(part))
		found, err := q.Query("SELECT order_id,coalesce(sum(refund_amount),0) FROM order_returns WHERE order_id IN ("+ph+") GROUP BY order_id", idList(part)...)
		if err != nil {
			return nil, err
		}
		for found.Next() {
			var id, sum int64
			if err = found.Scan(&id, &sum); err != nil {
				found.Close()
				return nil, err
			}
			refunds[id] = sum
		}
		if err = found.Err(); err != nil {
			found.Close()
			return nil, err
		}
		found.Close()
	}
	seen := map[string]bool{}
	out := make([]reconView, 0, len(rows))
	for _, row := range rows {
		view := reconView{OrderNo: row.OrderNo, Amount: row.Amount, Fee: row.Fee, ShippingInsurance: row.ShippingInsurance, Refund: row.Refund, SettledAt: row.SettledAt}
		if seen[rowPrint(currency, row)] {
			view.Status = "duplicate"
			view.Reason = "文件里这一行重复，导入时会跳过"
			out = append(out, view)
			continue
		}
		seen[rowPrint(currency, row)] = true
		noHit, noOK := byNo[row.OrderNo]
		platHit, platOK := byPlatform[row.OrderNo]
		switch {
		case noOK && platOK && noHit.id != platHit.id:
			view.Status = "ambiguous"
			view.Reason = "系统单号和平台单号对上了不同订单"
		case !noOK && !platOK:
			view.Status = "unmatched"
			view.Reason = "这个店铺里没有相同的系统单号或平台单号"
		default:
			hit := noHit
			if !noOK {
				hit = platHit
			}
			fillMatch(&view, hit, refunds[hit.id], currency)
		}
		out = append(out, view)
	}
	return out, nil
}

func fillMatch(view *reconView, hit orderHit, refund int64, currency string) {
	id := hit.id
	paid := hit.paid
	ref := refund
	view.OrderID = &id
	view.SystemNo = hit.no
	view.PlatformNo = hit.platformNo
	view.SystemAmount = &paid
	view.SystemRefund = &ref
	diffAmount := view.Amount - paid
	diffRefund := view.Refund - refund
	view.DiffAmount = &diffAmount
	view.DiffRefund = &diffRefund
	switch {
	case hit.currency != currency:
		view.Status = "currency"
		view.Reason = "币种不一致"
	case hit.status == "canceled":
		view.Status = "canceled"
		view.Reason = "订单已取消"
	default:
		view.Status = "matched"
		if diffAmount != 0 || diffRefund != 0 {
			view.Reason = "结算金额或退款和系统不一致"
		}
	}
}

func (a *Module) previewReconcile(w http.ResponseWriter, r *http.Request) (any, error) {
	var in previewIn
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	rows, err := a.prepareReconcile(r, in.ShopID, in.Currency, in.Rows)
	if err != nil {
		return nil, err
	}
	views, err := a.matchRows(a.DB, in.ShopID, strings.TrimSpace(in.Currency), rows)
	if err != nil {
		return nil, err
	}
	return core.Object{"shop_id": in.ShopID, "currency": strings.TrimSpace(in.Currency), "rows": views, "summary": summarize(views)}, nil
}

func (a *Module) prepareReconcile(r *http.Request, shopID int64, currency string, rows []reconRow) ([]reconRow, error) {
	currency = strings.TrimSpace(currency)
	if !validCurrency(currency) {
		return nil, bad(400, "币种不正确")
	}
	if err := a.requireShop(core.CurrentUser(r), shopID); err != nil {
		return nil, err
	}
	return normalizeRows(rows)
}

func (a *Module) importReconcile(w http.ResponseWriter, r *http.Request) (any, error) {
	var in importIn
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	in.RequestKey = strings.TrimSpace(in.RequestKey)
	in.Currency = strings.TrimSpace(in.Currency)
	if !validKey(in.RequestKey) {
		return nil, bad(400, "请求键不正确")
	}
	rows, err := a.prepareReconcile(r, in.ShopID, in.Currency, in.Rows)
	if err != nil {
		return nil, err
	}
	sum := reconHash(in.ShopID, in.Currency, rows)
	u := core.CurrentUser(r)
	return a.write(r, "reconcile.import", "finance_reconcile", func(tx *sql.Tx) (int64, any, any, bool, error) {
		existing, err := findBatch(tx, in.RequestKey)
		if err == nil {
			if existing.Hash != sum {
				return 0, nil, nil, false, bad(409, "相同请求键对应的内容不一致")
			}
			body, err := batchBody(tx, existing.ID, true)
			return existing.ID, nil, body, false, err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, nil, nil, false, err
		}
		views, err := a.matchRows(tx, in.ShopID, in.Currency, rows)
		if err != nil {
			return 0, nil, nil, false, err
		}
		res, err := tx.Exec(`INSERT INTO fin_reconcile_batches(shop_id,currency,request_key,params_hash,row_count,inserted,skipped,matched,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, in.ShopID, in.Currency, in.RequestKey, sum, len(rows), 0, 0, 0, u.ID, core.Now())
		if err != nil {
			if isUnique(err) {
				return 0, nil, nil, false, bad(409, "相同请求键对应的内容不一致")
			}
			return 0, nil, nil, false, err
		}
		batchID, err := res.LastInsertId()
		if err != nil {
			return 0, nil, nil, false, err
		}
		var inserted, skipped, matched int64
		now := core.Now()
		for i, view := range views {
			row := rows[i]
			if view.Status == "duplicate" {
				skipped++
				continue
			}
			fp := rowPrint(in.Currency, row)
			var n int
			if err = tx.QueryRow("SELECT count(*) FROM fin_reconcile_rows WHERE shop_id=? AND fingerprint=?", in.ShopID, fp).Scan(&n); err != nil {
				return 0, nil, nil, false, err
			}
			if n > 0 {
				skipped++
				continue
			}
			if _, err = tx.Exec(`INSERT INTO fin_reconcile_rows(batch_id,shop_id,currency,order_no,amount,fee,shipping_insurance,refund,settled_at,order_id,system_no,platform_no,system_amount,system_refund,diff_amount,diff_refund,match_status,reason,fingerprint,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, batchID, in.ShopID, in.Currency, row.OrderNo, row.Amount, row.Fee, row.ShippingInsurance, row.Refund, row.SettledAt, shopArg(view.OrderID), view.SystemNo, view.PlatformNo, nullableAmount(view.SystemAmount), nullableAmount(view.SystemRefund), nullableAmount(view.DiffAmount), nullableAmount(view.DiffRefund), view.Status, view.Reason, fp, now); err != nil {
				if isUnique(err) {
					skipped++
					continue
				}
				return 0, nil, nil, false, err
			}
			inserted++
			if view.Status == "matched" {
				matched++
			}
		}
		if _, err = tx.Exec("UPDATE fin_reconcile_batches SET inserted=?,skipped=?,matched=? WHERE id=?", inserted, skipped, matched, batchID); err != nil {
			return 0, nil, nil, false, err
		}
		body, err := batchBody(tx, batchID, false)
		if err != nil {
			return 0, nil, nil, false, err
		}
		return batchID, core.Object{"id": batchID, "shop_id": in.ShopID, "currency": in.Currency, "inserted": inserted, "skipped": skipped}, body, true, nil
	})
}

func nullableAmount(n *int64) any {
	if n == nil {
		return nil
	}
	return *n
}

type batchKey struct {
	ID   int64
	Hash string
}

func findBatch(q querier, key string) (batchKey, error) {
	var item batchKey
	err := q.QueryRow("SELECT id,params_hash FROM fin_reconcile_batches WHERE request_key=?", key).Scan(&item.ID, &item.Hash)
	return item, err
}

func batchBody(q querier, id int64, idempotent bool) (any, error) {
	item, err := loadBatch(q, id)
	if err != nil {
		return nil, err
	}
	rows, err := loadBatchRows(q, id)
	if err != nil {
		return nil, err
	}
	return core.Object{"idempotent": idempotent, "batch": item, "rows": rows}, nil
}

func loadBatch(q querier, id int64) (reconBatch, error) {
	var item reconBatch
	err := q.QueryRow(`SELECT b.id,b.shop_id,coalesce(s.name,''),b.currency,b.row_count,b.inserted,b.skipped,b.matched,b.created_at
 FROM fin_reconcile_batches b LEFT JOIN shops s ON s.id=b.shop_id WHERE b.id=?`, id).Scan(&item.ID, &item.ShopID, &item.ShopName, &item.Currency, &item.RowCount, &item.Inserted, &item.Skipped, &item.Matched, &item.CreatedAt)
	return item, err
}

func loadBatchRows(q querier, id int64) ([]reconView, error) {
	rows, err := q.Query(`SELECT order_no,amount,fee,shipping_insurance,refund,settled_at,order_id,system_no,platform_no,system_amount,system_refund,diff_amount,diff_refund,match_status,reason
 FROM fin_reconcile_rows WHERE batch_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []reconView{}
	for rows.Next() {
		var item reconView
		var orderID, systemAmount, systemRefund, diffAmount, diffRefund sql.NullInt64
		if err = rows.Scan(&item.OrderNo, &item.Amount, &item.Fee, &item.ShippingInsurance, &item.Refund, &item.SettledAt, &orderID, &item.SystemNo, &item.PlatformNo, &systemAmount, &systemRefund, &diffAmount, &diffRefund, &item.Status, &item.Reason); err != nil {
			return nil, err
		}
		item.OrderID = shopPtr(orderID)
		item.SystemAmount = intPtr(systemAmount)
		item.SystemRefund = intPtr(systemRefund)
		item.DiffAmount = intPtr(diffAmount)
		item.DiffRefund = intPtr(diffRefund)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *Module) shopScopeSQL(u core.User, shopID int64, col string) (string, []any, error) {
	if shopID > 0 {
		if err := a.requireShop(u, shopID); err != nil {
			return "", nil, err
		}
		return col + "=?", []any{shopID}, nil
	}
	if u.Role.Key == "admin" {
		return "1=1", nil, nil
	}
	if u.Role.Scope != "shops" {
		return "0=1", nil, nil
	}
	return col + " IN (SELECT shop_id FROM user_shops WHERE user_id=?)", []any{u.ID}, nil
}

func (a *Module) listReconcile(_ http.ResponseWriter, r *http.Request) (any, error) {
	shopID, _, err := queryID(r, "shop_id")
	if err != nil {
		return nil, err
	}
	where, args, err := a.shopScopeSQL(core.CurrentUser(r), shopID, "b.shop_id")
	if err != nil {
		return nil, err
	}
	var total int64
	if err = a.DB.QueryRow("SELECT count(*) FROM fin_reconcile_batches b WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	offset, size := core.Page(r)
	rows, err := a.DB.Query(`SELECT b.id,b.shop_id,coalesce(s.name,''),b.currency,b.row_count,b.inserted,b.skipped,b.matched,b.created_at
 FROM fin_reconcile_batches b LEFT JOIN shops s ON s.id=b.shop_id WHERE `+where+` ORDER BY b.id DESC LIMIT ? OFFSET ?`, append(append([]any{}, args...), size, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []reconBatch{}
	for rows.Next() {
		var item reconBatch
		if err = rows.Scan(&item.ID, &item.ShopID, &item.ShopName, &item.Currency, &item.RowCount, &item.Inserted, &item.Skipped, &item.Matched, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return core.Object{"items": items, "total": total}, rows.Err()
}

func (a *Module) getReconcile(_ http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	if id == 0 {
		return nil, bad(404, "对账记录不存在")
	}
	item, err := loadBatch(a.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, bad(404, "对账记录不存在")
	}
	if err != nil {
		return nil, err
	}
	if !a.shopAllowed(core.CurrentUser(r), item.ShopID) {
		return nil, bad(404, "对账记录不存在")
	}
	rows, err := loadBatchRows(a.DB, id)
	if err != nil {
		return nil, err
	}
	return core.Object{"batch": item, "rows": rows}, nil
}
