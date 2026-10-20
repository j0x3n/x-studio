package finance

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"xstudio/internal/core"
)

type expense struct {
	ID         int64  `json:"id"`
	ShopID     *int64 `json:"shop_id"`
	ShopName   string `json:"shop_name"`
	AccountID  int64  `json:"account_id"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	Category   string `json:"category"`
	Note       string `json:"note"`
	OccurredAt string `json:"occurred_at"`
	EntryID    int64  `json:"entry_id"`
	CreatedAt  string `json:"created_at"`
}

func (a *Module) expenseScope(u core.User, shopID int64, filtered bool) (string, []any, error) {
	if filtered {
		if err := a.requireShop(u, shopID); err != nil {
			return "", nil, err
		}
		return "e.shop_id=?", []any{shopID}, nil
	}
	if u.Role.Key == "admin" {
		return "1=1", nil, nil
	}
	if u.Role.Scope == "all" {
		return "e.shop_id IS NULL", nil, nil
	}
	if u.Role.Scope != "shops" {
		return "0=1", nil, nil
	}
	return "e.shop_id IN (SELECT shop_id FROM user_shops WHERE user_id=?)", []any{u.ID}, nil
}

func (a *Module) listExpenses(_ http.ResponseWriter, r *http.Request) (any, error) {
	shopID, filtered, err := queryID(r, "shop_id")
	if err != nil {
		return nil, err
	}
	where, args, err := a.expenseScope(core.CurrentUser(r), shopID, filtered)
	if err != nil {
		return nil, err
	}
	start, end, hasStart, hasEnd, err := dateSpan(r)
	if err != nil {
		return nil, err
	}
	if hasStart {
		where += " AND e.occurred_at>=?"
		args = append(args, stamp(start))
	}
	if hasEnd {
		where += " AND e.occurred_at<?"
		args = append(args, stamp(end))
	}
	var total int64
	if err = a.DB.QueryRow("SELECT count(*) FROM fin_expenses e WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	offset, size := core.Page(r)
	rows, err := a.DB.Query(`SELECT e.id,e.shop_id,coalesce(s.name,''),e.account_id,e.amount,e.currency,e.category,e.note,e.occurred_at,e.entry_id,e.created_at
 FROM fin_expenses e LEFT JOIN shops s ON s.id=e.shop_id WHERE `+where+` ORDER BY e.occurred_at DESC, e.id DESC LIMIT ? OFFSET ?`, append(append([]any{}, args...), size, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []expense{}
	for rows.Next() {
		item, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return core.Object{"items": items, "total": total}, rows.Err()
}

func scanExpense(rows *sql.Rows) (expense, error) {
	var item expense
	var shop sql.NullInt64
	err := rows.Scan(&item.ID, &shop, &item.ShopName, &item.AccountID, &item.Amount, &item.Currency, &item.Category, &item.Note, &item.OccurredAt, &item.EntryID, &item.CreatedAt)
	item.ShopID = shopPtr(shop)
	if item.ShopName == "" && item.ShopID == nil {
		item.ShopName = "公司公共"
	}
	return item, err
}

type expenseIn struct {
	ShopID     *int64 `json:"shop_id"`
	AccountID  int64  `json:"account_id"`
	Amount     int64  `json:"amount"`
	Category   string `json:"category"`
	Note       string `json:"note"`
	OccurredAt string `json:"occurred_at"`
	RequestKey string `json:"request_key"`
}

func (a *Module) createExpense(w http.ResponseWriter, r *http.Request) (any, error) {
	var in expenseIn
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	in.Category = strings.TrimSpace(in.Category)
	in.Note = strings.TrimSpace(in.Note)
	in.RequestKey = strings.TrimSpace(in.RequestKey)
	if in.AccountID < 1 || !validMoney(in.Amount, false) || in.Category == "" || !validText(in.Category, 40) || !validText(in.Note, 500) || !validKey(in.RequestKey) {
		return nil, bad(400, "账户、金额、类别、备注或请求键不正确")
	}
	if in.ShopID != nil && *in.ShopID <= 0 {
		return nil, bad(400, "店铺不正确")
	}
	occurred, err := normalizeTime(in.OccurredAt)
	if err != nil {
		return nil, bad(400, "发生时间不正确")
	}
	shopKey := int64(0)
	if in.ShopID != nil {
		shopKey = *in.ShopID
	}
	hashTime := occurred
	if strings.TrimSpace(in.OccurredAt) == "" {
		hashTime = ""
	}
	sum := hashParts("expense", i64s(shopKey), i64s(in.AccountID), i64s(in.Amount), in.Category, in.Note, hashTime)
	u := core.CurrentUser(r)
	if !a.shopAllowed(u, shopKey) {
		if shopKey == 0 {
			return nil, bad(403, "只有管理员可以记公司公共费用")
		}
		return nil, bad(404, "店铺不存在")
	}
	return a.write(r, "expense.create", "finance_expense", func(tx *sql.Tx) (int64, any, any, bool, error) {
		var existingID int64
		var hash string
		err := tx.QueryRow("SELECT id,params_hash FROM fin_expenses WHERE request_key=?", in.RequestKey).Scan(&existingID, &hash)
		if err == nil {
			if hash != sum {
				return 0, nil, nil, false, bad(409, "相同请求键对应的内容不一致")
			}
			item, err := loadExpense(tx, existingID)
			return existingID, nil, core.Object{"idempotent": true, "expense": item}, false, err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, nil, nil, false, err
		}
		if in.ShopID != nil {
			if err = a.usableShop(tx, *in.ShopID); err != nil {
				return 0, nil, nil, false, err
			}
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
		if !sameShop(in.ShopID, acc.ShopID) {
			return 0, nil, nil, false, bad(400, "费用店铺要和付款账户一致")
		}
		res, err := tx.Exec(`INSERT INTO fin_expenses(shop_id,account_id,amount,currency,category,note,occurred_at,request_key,params_hash,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, shopArg(in.ShopID), acc.ID, in.Amount, acc.Currency, in.Category, in.Note, occurred, in.RequestKey, sum, u.ID, core.Now())
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
		entryID, err := postEntry(tx, acc, "out", in.Amount, in.Category, in.Note, occurred, acc.ShopID, "expense", id, u.ID)
		if err != nil {
			return 0, nil, nil, false, err
		}
		if _, err = tx.Exec("UPDATE fin_expenses SET entry_id=? WHERE id=?", entryID, id); err != nil {
			return 0, nil, nil, false, err
		}
		item, err := loadExpense(tx, id)
		if err != nil {
			return 0, nil, nil, false, err
		}
		return id, core.Object{"id": id, "account_id": acc.ID, "amount": in.Amount, "shop_id": item.ShopID}, core.Object{"idempotent": false, "expense": item}, true, nil
	})
}

func loadExpense(q querier, id int64) (expense, error) {
	rows, err := q.Query(`SELECT e.id,e.shop_id,coalesce(s.name,''),e.account_id,e.amount,e.currency,e.category,e.note,e.occurred_at,e.entry_id,e.created_at
 FROM fin_expenses e LEFT JOIN shops s ON s.id=e.shop_id WHERE e.id=?`, id)
	if err != nil {
		return expense{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return expense{}, err
		}
		return expense{}, sql.ErrNoRows
	}
	item, err := scanExpense(rows)
	if err != nil {
		return expense{}, err
	}
	return item, rows.Err()
}
