package finance

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"xstudio/internal/core"
)

type account struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Currency       string `json:"currency"`
	ShopID         *int64 `json:"shop_id"`
	ShopName       string `json:"shop_name"`
	OpeningBalance int64  `json:"opening_balance"`
	Balance        int64  `json:"balance"`
	Archived       bool   `json:"archived"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type entry struct {
	ID          int64  `json:"id"`
	AccountID   int64  `json:"account_id"`
	AccountName string `json:"account_name"`
	Direction   string `json:"direction"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	Category    string `json:"category"`
	Note        string `json:"note"`
	OccurredAt  string `json:"occurred_at"`
	ShopID      *int64 `json:"shop_id"`
	SourceType  string `json:"source_type"`
	SourceID    int64  `json:"source_id"`
	CreatedAt   string `json:"created_at"`
}

func (a *Module) accountScope(u core.User) (string, []any) {
	if u.Role.Key == "admin" {
		return "1=1", nil
	}
	if u.Role.Scope == "all" {
		return "a.shop_id IS NULL", nil
	}
	if u.Role.Scope != "shops" {
		return "0=1", nil
	}
	return "a.shop_id IN (SELECT shop_id FROM user_shops WHERE user_id=?)", []any{u.ID}
}

func (a *Module) listAccounts(_ http.ResponseWriter, r *http.Request) (any, error) {
	where, args := a.accountScope(core.CurrentUser(r))
	switch r.URL.Query().Get("archived") {
	case "1":
		where += " AND a.archived=1"
	case "all":
	default:
		where += " AND a.archived=0"
	}
	if shopID, ok, err := queryID(r, "shop_id"); err != nil {
		return nil, err
	} else if ok {
		if err = a.requireShop(core.CurrentUser(r), shopID); err != nil {
			return nil, err
		}
		where += " AND a.shop_id=?"
		args = append(args, shopID)
	}
	return pagedAccounts(a.DB, r, where, args)
}

func pagedAccounts(q querier, r *http.Request, where string, args []any) (any, error) {
	var total int64
	if err := q.QueryRow("SELECT count(*) FROM fin_accounts a WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	offset, size := core.Page(r)
	rows, err := q.Query(`SELECT a.id,a.name,a.kind,a.currency,a.shop_id,coalesce(s.name,''),a.opening_balance,a.archived,a.created_at,a.updated_at,
 coalesce((SELECT sum(amount) FROM fin_entries e WHERE e.account_id=a.id AND e.direction='in'),0),
 coalesce((SELECT sum(amount) FROM fin_entries e WHERE e.account_id=a.id AND e.direction='out'),0)
 FROM fin_accounts a LEFT JOIN shops s ON s.id=a.shop_id WHERE `+where+` ORDER BY a.id DESC LIMIT ? OFFSET ?`, append(append([]any{}, args...), size, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []account{}
	for rows.Next() {
		item, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return core.Object{"items": items, "total": total}, rows.Err()
}

func scanAccount(rows *sql.Rows) (account, error) {
	var item account
	var shop sql.NullInt64
	var archived, inn, out int64
	err := rows.Scan(&item.ID, &item.Name, &item.Kind, &item.Currency, &shop, &item.ShopName, &item.OpeningBalance, &archived, &item.CreatedAt, &item.UpdatedAt, &inn, &out)
	if err != nil {
		return item, err
	}
	item.ShopID = shopPtr(shop)
	item.Archived = archived != 0
	sum, err := addNonNeg(item.OpeningBalance, inn)
	if err != nil {
		return item, err
	}
	item.Balance = sum - out
	return item, nil
}

func loadAccount(q querier, id int64) (account, error) {
	rows, err := q.Query(`SELECT a.id,a.name,a.kind,a.currency,a.shop_id,coalesce(s.name,''),a.opening_balance,a.archived,a.created_at,a.updated_at,
 coalesce((SELECT sum(amount) FROM fin_entries e WHERE e.account_id=a.id AND e.direction='in'),0),
 coalesce((SELECT sum(amount) FROM fin_entries e WHERE e.account_id=a.id AND e.direction='out'),0)
 FROM fin_accounts a LEFT JOIN shops s ON s.id=a.shop_id WHERE a.id=?`, id)
	if err != nil {
		return account{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return account{}, err
		}
		return account{}, sql.ErrNoRows
	}
	item, err := scanAccount(rows)
	if err != nil {
		return account{}, err
	}
	return item, rows.Err()
}

func (a *Module) visibleAccount(q querier, u core.User, id int64) (account, error) {
	item, err := loadAccount(q, id)
	if err != nil {
		return account{}, err
	}
	if !shopAllowedQ(q, u, shopValue(item.ShopID)) {
		return account{}, sql.ErrNoRows
	}
	return item, nil
}

func shopValue(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}

func shopPtr(n sql.NullInt64) *int64 {
	if !n.Valid || n.Int64 == 0 {
		return nil
	}
	v := n.Int64
	return &v
}

func intPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

type accountIn struct {
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Currency       string `json:"currency"`
	ShopID         *int64 `json:"shop_id"`
	OpeningBalance int64  `json:"opening_balance"`
}

func normalizeAccount(in *accountIn) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Currency = strings.TrimSpace(in.Currency)
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	if in.Name == "" || !validText(in.Name, 80) || !validKind(in.Kind) || !validCurrency(in.Currency) || !validMoney(in.OpeningBalance, true) {
		return bad(400, "账户名称、类型、币种或期初余额不正确")
	}
	if in.ShopID != nil && *in.ShopID <= 0 {
		return bad(400, "店铺不正确")
	}
	if in.ShopID != nil && *in.ShopID == 0 {
		in.ShopID = nil
	}
	return nil
}

func (a *Module) createAccount(w http.ResponseWriter, r *http.Request) (any, error) {
	var in accountIn
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	if err := normalizeAccount(&in); err != nil {
		return nil, err
	}
	u := core.CurrentUser(r)
	if !a.shopAllowed(u, shopValue(in.ShopID)) {
		if in.ShopID == nil {
			return nil, bad(403, "只有管理员可以建立公司公共账户")
		}
		return nil, bad(404, "店铺不存在")
	}
	return a.write(r, "account.create", "finance_account", func(tx *sql.Tx) (int64, any, any, bool, error) {
		if in.ShopID != nil {
			if err := a.usableShop(tx, *in.ShopID); err != nil {
				return 0, nil, nil, false, err
			}
		}
		now := core.Now()
		res, err := tx.Exec(`INSERT INTO fin_accounts(name,kind,currency,shop_id,opening_balance,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, in.Name, in.Kind, in.Currency, shopArg(in.ShopID), in.OpeningBalance, u.ID, now, now)
		if err != nil {
			return 0, nil, nil, false, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, nil, nil, false, err
		}
		item, err := loadAccount(tx, id)
		if err != nil {
			return 0, nil, nil, false, err
		}
		return id, core.Object{"id": id, "currency": item.Currency, "shop_id": item.ShopID, "opening_balance": item.OpeningBalance}, core.Object{"account": item}, true, nil
	})
}

func (a *Module) patchAccount(w http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	if id == 0 {
		return nil, bad(404, "账户不存在")
	}
	raw, err := decodeAllowed(w, r, map[string]bool{"name": true, "kind": true, "currency": true, "shop_id": true, "archived": true, "opening_balance": true})
	if err != nil {
		return nil, err
	}
	u := core.CurrentUser(r)
	return a.write(r, "account.update", "finance_account", func(tx *sql.Tx) (int64, any, any, bool, error) {
		item, err := a.visibleAccount(tx, u, id)
		if err != nil {
			return 0, nil, nil, false, err
		}
		if n, ok, err := field[int64](raw, "opening_balance"); err != nil {
			return 0, nil, nil, false, err
		} else if ok && n != item.OpeningBalance {
			return 0, nil, nil, false, bad(409, "期初余额只能在创建时设置")
		}
		name, nameOK, err := field[string](raw, "name")
		if err != nil {
			return 0, nil, nil, false, err
		}
		kind, kindOK, err := field[string](raw, "kind")
		if err != nil {
			return 0, nil, nil, false, err
		}
		currency, curOK, err := field[string](raw, "currency")
		if err != nil {
			return 0, nil, nil, false, err
		}
		archived, archOK, err := field[bool](raw, "archived")
		if err != nil {
			return 0, nil, nil, false, err
		}
		shop, shopOK, err := field[*int64](raw, "shop_id")
		if err != nil {
			return 0, nil, nil, false, err
		}
		if nameOK {
			name = strings.TrimSpace(name)
			if name == "" || !validText(name, 80) {
				return 0, nil, nil, false, bad(400, "账户名称不正确")
			}
			item.Name = name
		}
		if kindOK {
			if !validKind(kind) {
				return 0, nil, nil, false, bad(400, "账户类型不正确")
			}
			item.Kind = kind
		}
		if curOK {
			currency = strings.TrimSpace(currency)
			if !validCurrency(currency) {
				return 0, nil, nil, false, bad(400, "币种不正确")
			}
		}
		if shopOK && shop != nil && *shop <= 0 {
			return 0, nil, nil, false, bad(400, "店铺不正确")
		}
		if shopOK && shop != nil && *shop == 0 {
			shop = nil
		}
		var n int
		if err = tx.QueryRow("SELECT count(*) FROM fin_entries WHERE account_id=?", id).Scan(&n); err != nil {
			return 0, nil, nil, false, err
		}
		if n > 0 && ((curOK && currency != item.Currency) || (shopOK && !sameShop(shop, item.ShopID))) {
			return 0, nil, nil, false, bad(409, "已有流水的账户不能改币种或店铺")
		}
		if curOK {
			item.Currency = currency
		}
		if shopOK {
			if !shopAllowedQ(tx, u, shopValue(shop)) {
				if shop == nil {
					return 0, nil, nil, false, bad(403, "只有管理员可以改为公司公共账户")
				}
				return 0, nil, nil, false, bad(404, "店铺不存在")
			}
			if shop != nil {
				if err = a.usableShop(tx, *shop); err != nil {
					return 0, nil, nil, false, err
				}
			}
			item.ShopID = cloneShop(shop)
		}
		if archOK {
			item.Archived = archived
		}
		arch := 0
		if item.Archived {
			arch = 1
		}
		if _, err = tx.Exec("UPDATE fin_accounts SET name=?,kind=?,currency=?,shop_id=?,archived=?,updated_at=? WHERE id=?", item.Name, item.Kind, item.Currency, shopArg(item.ShopID), arch, core.Now(), id); err != nil {
			return 0, nil, nil, false, err
		}
		item, err = loadAccount(tx, id)
		if err != nil {
			return 0, nil, nil, false, err
		}
		return id, core.Object{"id": id, "archived": item.Archived, "currency": item.Currency, "shop_id": item.ShopID}, core.Object{"account": item}, true, nil
	})
}

func (a *Module) listEntries(_ http.ResponseWriter, r *http.Request) (any, error) {
	u := core.CurrentUser(r)
	where, args := a.accountScope(u)
	if accountID, ok, err := queryID(r, "account_id"); err != nil {
		return nil, err
	} else if ok {
		if _, err = a.visibleAccount(a.DB, u, accountID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, bad(404, "账户不存在")
			}
			return nil, err
		}
		where += " AND e.account_id=?"
		args = append(args, accountID)
	}
	var total int64
	if err := a.DB.QueryRow("SELECT count(*) FROM fin_entries e JOIN fin_accounts a ON a.id=e.account_id WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	offset, size := core.Page(r)
	rows, err := a.DB.Query(`SELECT e.id,e.account_id,a.name,e.direction,e.amount,a.currency,e.category,e.note,e.occurred_at,e.shop_id,e.source_type,e.source_id,e.created_at
 FROM fin_entries e JOIN fin_accounts a ON a.id=e.account_id WHERE `+where+` ORDER BY e.occurred_at DESC, e.id DESC LIMIT ? OFFSET ?`, append(append([]any{}, args...), size, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []entry{}
	for rows.Next() {
		item, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return core.Object{"items": items, "total": total}, rows.Err()
}

func scanEntry(rows *sql.Rows) (entry, error) {
	var item entry
	var shop sql.NullInt64
	err := rows.Scan(&item.ID, &item.AccountID, &item.AccountName, &item.Direction, &item.Amount, &item.Currency, &item.Category, &item.Note, &item.OccurredAt, &shop, &item.SourceType, &item.SourceID, &item.CreatedAt)
	item.ShopID = shopPtr(shop)
	return item, err
}

type entryIn struct {
	AccountID  int64  `json:"account_id"`
	Direction  string `json:"direction"`
	Amount     int64  `json:"amount"`
	Category   string `json:"category"`
	Note       string `json:"note"`
	OccurredAt string `json:"occurred_at"`
	ShopID     *int64 `json:"shop_id"`
}

func (a *Module) createEntry(w http.ResponseWriter, r *http.Request) (any, error) {
	var in entryIn
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	in.Category = strings.TrimSpace(in.Category)
	in.Note = strings.TrimSpace(in.Note)
	in.Direction = strings.TrimSpace(in.Direction)
	if in.AccountID < 1 || (in.Direction != "in" && in.Direction != "out") || !validMoney(in.Amount, false) || in.Category == "" || !validText(in.Category, 40) || !validText(in.Note, 500) {
		return nil, bad(400, "账户、收支方向、金额、类别或备注不正确")
	}
	occurred, err := normalizeTime(in.OccurredAt)
	if err != nil {
		return nil, bad(400, "发生时间不正确")
	}
	if in.ShopID != nil && *in.ShopID <= 0 {
		return nil, bad(400, "店铺不正确")
	}
	u := core.CurrentUser(r)
	return a.write(r, "entry.create", "finance_entry", func(tx *sql.Tx) (int64, any, any, bool, error) {
		acc, err := a.visibleAccount(tx, u, in.AccountID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, nil, nil, false, bad(404, "账户不存在")
			}
			return 0, nil, nil, false, err
		}
		shop := acc.ShopID
		if _, ok := rawHasShop(in.ShopID); ok && !sameShop(in.ShopID, acc.ShopID) {
			return 0, nil, nil, false, bad(400, "流水店铺要和账户店铺一致")
		}
		id, err := postEntry(tx, acc, in.Direction, in.Amount, in.Category, in.Note, occurred, shop, "manual", 0, u.ID)
		if err != nil {
			return 0, nil, nil, false, err
		}
		item, err := loadEntry(tx, id)
		if err != nil {
			return 0, nil, nil, false, err
		}
		return id, core.Object{"id": id, "account_id": acc.ID, "direction": in.Direction, "amount": in.Amount}, core.Object{"entry": item}, true, nil
	})
}

func rawHasShop(id *int64) (int64, bool) {
	if id == nil {
		return 0, false
	}
	return *id, true
}

func postEntry(tx *sql.Tx, acc account, direction string, amount int64, category, note, occurred string, shop *int64, source string, sourceID, userID int64) (int64, error) {
	if acc.Archived {
		return 0, bad(409, "账户已归档，不能记账")
	}
	if !sameShop(shop, acc.ShopID) {
		return 0, bad(400, "流水店铺要和账户店铺一致")
	}
	if direction == "out" {
		if acc.Balance < amount {
			return 0, bad(409, "余额不足，不能透支")
		}
	} else if _, err := addNonNeg(acc.Balance, amount); err != nil {
		return 0, bad(400, "账户余额超出可计算范围")
	}
	res, err := tx.Exec(`INSERT INTO fin_entries(account_id,direction,amount,category,note,occurred_at,shop_id,source_type,source_id,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, acc.ID, direction, amount, category, note, occurred, shopArg(shop), source, sourceID, userID, core.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func loadEntry(q querier, id int64) (entry, error) {
	rows, err := q.Query(`SELECT e.id,e.account_id,a.name,e.direction,e.amount,a.currency,e.category,e.note,e.occurred_at,e.shop_id,e.source_type,e.source_id,e.created_at
 FROM fin_entries e JOIN fin_accounts a ON a.id=e.account_id WHERE e.id=?`, id)
	if err != nil {
		return entry{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return entry{}, err
		}
		return entry{}, sql.ErrNoRows
	}
	item, err := scanEntry(rows)
	if err != nil {
		return entry{}, err
	}
	return item, rows.Err()
}

func lockAccount(tx *sql.Tx, id int64) (account, error) {
	if _, err := tx.Exec("UPDATE fin_accounts SET updated_at=updated_at WHERE id=?", id); err != nil {
		return account{}, err
	}
	return loadAccount(tx, id)
}
