package purchase

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
	"xstudio/internal/core"
	"xstudio/internal/modules/inventory"
)

type endpoint func(http.ResponseWriter, *http.Request) (any, error)

var errDecoded = errors.New("decoded response")

func (a *Module) purchaseRoutes(r *core.Router) {
	for _, route := range []struct {
		path string
		fn   endpoint
	}{
		{"GET /api/purchase-orders", a.listOrders}, {"POST /api/purchase-orders", a.createOrder},
		{"GET /api/purchase-orders/{id}", a.getOrder}, {"PATCH /api/purchase-orders/{id}", a.patchOrder},
		{"POST /api/purchase-orders/{id}/order", a.orderAction}, {"POST /api/purchase-orders/{id}/finish", a.orderAction},
		{"POST /api/purchase-orders/{id}/cancel", a.orderAction}, {"POST /api/purchase-orders/{id}/receipts", a.receive},
		{"PATCH /api/purchase-returns/{id}", a.finishReturn},
	} {
		fn := route.fn
		r.Handle(route.path, core.PurchaseManage, func(w http.ResponseWriter, r *http.Request) {
			if !canManageSupplier(core.CurrentUser(r)) {
				core.Fail(w, 403, "外部协作者不能访问采购")
				return
			}
			result, err := fn(w, r)
			if err != nil {
				purchaseErr(w, err)
				return
			}
			core.WriteJSON(w, 200, result)
		})
	}
}
func purchaseErr(w http.ResponseWriter, err error) {
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
		core.Fail(w, 404, "记录不存在")
	case errors.Is(err, inventory.ErrShort):
		core.Fail(w, 409, err.Error())
	case strings.Contains(err.Error(), "UNIQUE constraint failed"):
		core.Fail(w, 409, "单据重复")
	default:
		log.Printf("采购操作失败：%v", err)
		core.Fail(w, 500, "采购操作失败")
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
			switch k {
			case "grades", "batch_ids":
				if s, ok := v.(string); ok {
					if err = json.Unmarshal([]byte(s), &v); err != nil {
						return nil, err
					}
				}
			case "is_sample", "archived":
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
func (a *Module) change(r *http.Request, action string, fn func(*sql.Tx) (int64, any, any, error)) (any, error) {
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id, result, detail, err := fn(tx)
	if err != nil {
		return nil, err
	}
	if err = core.AuditTx(tx, r, "purchase."+action, "purchase_order", id, detail); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func sum(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, bad(400, "数量或金额超出范围")
	}
	return a + b, nil
}

// ratio uses integer half-up rounding without overflowing the intermediate product.
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
func validCurrency(s string) bool {
	return len(s) == 3 && strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == ""
}
func validDate(s string) bool {
	if s == "" {
		return true
	}
	if _, e := time.Parse("2006-01-02", s); e == nil {
		return true
	}
	_, e := time.Parse(time.RFC3339Nano, s)
	return e == nil
}
func number(tx *sql.Tx, prefix, table string, width int) (string, error) {
	prefix += time.Now().In(time.FixedZone("CST", 8*3600)).Format("20060102")
	var last string
	err := tx.QueryRow("SELECT coalesce(max(no),'') FROM "+table+" WHERE no LIKE ?", prefix+"%").Scan(&last)
	if err != nil {
		return "", err
	}
	seq := 0
	if last != "" {
		if _, err = fmt.Sscanf(strings.TrimPrefix(last, prefix), "%d", &seq); err != nil {
			return "", err
		}
	}
	limit := 1
	for i := 0; i < width; i++ {
		limit *= 10
	}
	if seq+1 >= limit {
		return "", bad(409, "今日单号已用完")
	}
	return fmt.Sprintf("%s%0*d", prefix, width, seq+1), nil
}
func nullable(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
