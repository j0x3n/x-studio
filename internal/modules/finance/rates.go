package finance

import (
	"database/sql"
	"net/http"
	"strings"
	"xstudio/internal/core"
)

type rate struct {
	Month      string `json:"month"`
	Currency   string `json:"currency"`
	RateMicros int64  `json:"rate_micros"`
	UpdatedAt  string `json:"updated_at"`
}

func (a *Module) listRates(_ http.ResponseWriter, r *http.Request) (any, error) {
	where := "1=1"
	args := []any{}
	if month := strings.TrimSpace(r.URL.Query().Get("month")); month != "" {
		if _, _, err := monthRange(month); err != nil {
			return nil, bad(400, "月份应为 YYYY-MM")
		}
		where += " AND month=?"
		args = append(args, month)
	}
	if cur := strings.TrimSpace(r.URL.Query().Get("currency")); cur != "" {
		if !validCurrency(cur) {
			return nil, bad(400, "币种不正确")
		}
		where += " AND currency=?"
		args = append(args, cur)
	}
	var total int64
	if err := a.DB.QueryRow("SELECT count(*) FROM fin_rates WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	offset, size := core.Page(r)
	rows, err := a.DB.Query("SELECT month,currency,rate_micros,updated_at FROM fin_rates WHERE "+where+" ORDER BY month DESC, currency LIMIT ? OFFSET ?", append(append([]any{}, args...), size, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []rate{}
	for rows.Next() {
		var item rate
		if err = rows.Scan(&item.Month, &item.Currency, &item.RateMicros, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if item.Currency == "CNY" {
			item.RateMicros = cnyRate
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return core.Object{"items": items, "total": total, "cny_rate_micros": cnyRate}, nil
}

type rateIn struct {
	Month      string `json:"month"`
	Currency   string `json:"currency"`
	RateMicros int64  `json:"rate_micros"`
}

func (a *Module) putRate(w http.ResponseWriter, r *http.Request) (any, error) {
	var in rateIn
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	in.Month = strings.TrimSpace(in.Month)
	in.Currency = strings.TrimSpace(in.Currency)
	if _, _, err := monthRange(in.Month); err != nil || !validCurrency(in.Currency) || in.RateMicros <= 0 || in.RateMicros > maxRate {
		return nil, bad(400, "月份、币种或汇率不正确")
	}
	if in.Currency == "CNY" && in.RateMicros != cnyRate {
		return nil, bad(400, "人民币汇率固定为 1000000，表示 1 元兑 1 元")
	}
	u := core.CurrentUser(r)
	return a.write(r, "rate.save", "finance_rate", func(tx *sql.Tx) (int64, any, any, bool, error) {
		now := core.Now()
		if _, err := tx.Exec(`INSERT INTO fin_rates(month,currency,rate_micros,updated_by,updated_at) VALUES(?,?,?,?,?)
 ON CONFLICT(month,currency) DO UPDATE SET rate_micros=excluded.rate_micros,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, in.Month, in.Currency, in.RateMicros, u.ID, now); err != nil {
			return 0, nil, nil, false, err
		}
		item := rate{Month: in.Month, Currency: in.Currency, RateMicros: in.RateMicros, UpdatedAt: now}
		if item.Currency == "CNY" {
			item.RateMicros = cnyRate
		}
		return 0, core.Object{"month": item.Month, "currency": item.Currency, "rate_micros": item.RateMicros}, core.Object{"rate": item}, true, nil
	})
}

func (a *Module) ratesFor(month string) (map[string]int64, error) {
	rows, err := a.DB.Query("SELECT currency,rate_micros FROM fin_rates WHERE month=?", month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{"CNY": cnyRate}
	for rows.Next() {
		var cur string
		var rate int64
		if err = rows.Scan(&cur, &rate); err != nil {
			return nil, err
		}
		if cur == "CNY" {
			continue
		}
		out[cur] = rate
	}
	return out, rows.Err()
}
