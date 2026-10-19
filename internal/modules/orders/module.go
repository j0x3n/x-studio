package orders

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/big"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
	"xstudio/internal/core"
	"xstudio/internal/modules/inventory"
)

type Module struct{ *core.Core }

func New(c *core.Core) *Module                   { return &Module{c} }
func (*Module) Name() string                     { return "orders" }
func (*Module) Jobs() map[string]core.JobHandler { return nil }

type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }
func bad(code int, msg string) error { return &statusError{code, msg} }

var errDecoded = errors.New("decoded response")

type endpoint func(http.ResponseWriter, *http.Request) (any, error)

func (a *Module) Routes(r *core.Router) {
	for _, route := range []struct {
		path, perm string
		fn         endpoint
	}{
		{"GET /api/orders", core.OrderView, a.list}, {"POST /api/orders", core.OrderManage, a.create},
		{"GET /api/orders/{id}", core.OrderView, a.get}, {"PATCH /api/orders/{id}", core.OrderManage, a.patch},
		{"POST /api/orders/{id}/ship", core.OrderManage, a.ship}, {"POST /api/orders/{id}/cancel", core.OrderManage, a.cancel},
		{"POST /api/orders/{id}/returns", core.OrderManage, a.returnOrder},
	} {
		fn := route.fn
		r.Handle(route.path, route.perm, func(w http.ResponseWriter, r *http.Request) {
			if u := core.CurrentUser(r); u.Role.Key != "admin" && u.Role.Scope == "shares" {
				core.Fail(w, 403, "外部协作者不能访问订单")
				return
			}
			out, err := fn(w, r)
			if err != nil {
				respondErr(w, err)
				return
			}
			core.WriteJSON(w, 200, out)
		})
	}
}
func respondErr(w http.ResponseWriter, err error) {
	var se *statusError
	var he interface {
		error
		HTTPStatus() int
	}
	switch {
	case errors.Is(err, errDecoded):
		return
	case errors.As(err, &se):
		core.Fail(w, se.code, se.msg)
	case errors.As(err, &he):
		core.Fail(w, he.HTTPStatus(), he.Error())
	case errors.Is(err, sql.ErrNoRows):
		core.Fail(w, 404, "订单或明细不存在")
	case errors.Is(err, inventory.ErrShort):
		core.Fail(w, 409, err.Error())
	case strings.Contains(err.Error(), "UNIQUE constraint failed"):
		core.Fail(w, 409, "该店铺已有相同平台订单号")
	default:
		log.Printf("订单操作失败：%v", err)
		core.Fail(w, 500, "订单操作失败")
	}
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !core.Decode(w, r, v) {
		return errDecoded
	}
	return nil
}
func objects(q inventory.Querier, query string, args ...any) ([]core.Object, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []core.Object{}
	for rows.Next() {
		vs := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range vs {
			ptr[i] = &vs[i]
		}
		if err = rows.Scan(ptr...); err != nil {
			return nil, err
		}
		o := core.Object{}
		for i, k := range cols {
			v := vs[i]
			if k == "short" {
				if n, ok := v.(int64); ok {
					v = n != 0
				}
			}
			o[k] = v
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func one(q inventory.Querier, query string, args ...any) (core.Object, error) {
	os, err := objects(q, query, args...)
	if err != nil {
		return nil, err
	}
	if len(os) == 0 {
		return nil, sql.ErrNoRows
	}
	return os[0], nil
}
func scope(u core.User, alias string) (string, []any) {
	if u.Role.Key == "admin" {
		return "1=1", nil
	}
	if u.Role.Scope != "shops" {
		return "0=1", nil
	}
	return "EXISTS(SELECT 1 FROM user_shops us WHERE us.shop_id=" + alias + ".shop_id AND us.user_id=?)", []any{u.ID}
}
func accessible(q inventory.Querier, u core.User, id int64) (core.Object, error) {
	where, args := scope(u, "o")
	args = append(args, id)
	return one(q, "SELECT o.* FROM orders o WHERE "+where+" AND o.id=?", args...)
}
func shopAllowed(tx *sql.Tx, u core.User, id int64) error {
	var n int
	query := "SELECT count(*) FROM shops WHERE id=? AND archived=0"
	args := []any{id}
	if u.Role.Key != "admin" {
		if u.Role.Scope != "shops" {
			return sql.ErrNoRows
		}
		query += " AND EXISTS(SELECT 1 FROM user_shops us WHERE us.shop_id=shops.id AND us.user_id=?)"
		args = append(args, u.ID)
	}
	if err := tx.QueryRow(query, args...).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (a *Module) change(r *http.Request, action string, fn func(*sql.Tx) (int64, any, error)) (any, error) {
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id, out, err := fn(tx)
	if err != nil {
		return nil, err
	}
	if err = core.AuditTx(tx, r, "order."+action, "order", id, core.Object{"id": id}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
func sum(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, bad(400, "数量或金额超出范围")
	}
	return a + b, nil
}
func ratio(a, b, d int64) (int64, error) {
	if a < 0 || b < 0 || d <= 0 {
		return 0, bad(400, "数量或金额不正确")
	}
	n := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	n.Add(n, big.NewInt(d/2))
	n.Quo(n, big.NewInt(d))
	if !n.IsInt64() {
		return 0, bad(400, "金额超出范围")
	}
	return n.Int64(), nil
}
func validDate(s string) bool {
	if s == "" {
		return true
	}
	if _, err := time.Parse("2006-01-02", s); err == nil {
		return true
	}
	_, err := time.Parse(time.RFC3339Nano, s)
	return err == nil
}
func nullable(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
func runeLen(s string) int { return utf8.RuneCountInString(s) }
func number(tx *sql.Tx) (string, error) {
	prefix := "SO" + time.Now().In(time.FixedZone("CST", 8*3600)).Format("20060102")
	var last string
	if err := tx.QueryRow("SELECT coalesce(max(no),'') FROM orders WHERE no LIKE ?", prefix+"%").Scan(&last); err != nil {
		return "", err
	}
	seq := 0
	if last != "" {
		if _, err := fmt.Sscanf(strings.TrimPrefix(last, prefix), "%d", &seq); err != nil {
			return "", err
		}
	}
	if seq >= 9999 {
		return "", bad(409, "今日单号已用完")
	}
	return fmt.Sprintf("%s%04d", prefix, seq+1), nil
}
func costAllowed(u core.User) bool { return u.Has(core.ProductCost) || u.Has(core.FinanceView) }
func redact(u core.User, rows []core.Object) {
	if !costAllowed(u) {
		for _, row := range rows {
			delete(row, "cost")
			delete(row, "returned_cost")
		}
	}
}
func merge(raw map[string]json.RawMessage, v any) error {
	b, _ := json.Marshal(raw)
	if err := json.Unmarshal(b, v); err != nil {
		return bad(400, "字段类型不正确")
	}
	return nil
}
