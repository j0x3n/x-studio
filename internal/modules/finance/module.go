package finance

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"xstudio/internal/core"
)

type Module struct{ *core.Core }

func New(c *core.Core) *Module                   { return &Module{c} }
func (*Module) Name() string                     { return "finance" }
func (*Module) Jobs() map[string]core.JobHandler { return nil }

type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }
func bad(code int, msg string) error { return &statusError{code, msg} }

var errDecoded = errors.New("decoded")

type endpoint func(http.ResponseWriter, *http.Request) (any, error)

type querier interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
	Exec(string, ...any) (sql.Result, error)
}

func (a *Module) Routes(r *core.Router) {
	routes := []struct {
		path, perm string
		fn         endpoint
	}{
		{"GET /api/finance/accounts", core.FinanceView, a.listAccounts},
		{"POST /api/finance/accounts", core.FinanceEdit, a.createAccount},
		{"PATCH /api/finance/accounts/{id}", core.FinanceEdit, a.patchAccount},
		{"GET /api/finance/entries", core.FinanceView, a.listEntries},
		{"POST /api/finance/entries", core.FinanceEdit, a.createEntry},
		{"GET /api/finance/payables", core.FinanceView, a.listPayables},
		{"POST /api/finance/payables/{purchase_id}/payments", core.FinanceEdit, a.createPayment},
		{"GET /api/finance/suppliers/{id}/statement", core.FinanceView, a.supplierStatement},
		{"GET /api/finance/expenses", core.FinanceView, a.listExpenses},
		{"POST /api/finance/expenses", core.FinanceEdit, a.createExpense},
		{"GET /api/finance/rates", core.FinanceView, a.listRates},
		{"PUT /api/finance/rates", core.FinanceEdit, a.putRate},
		{"POST /api/finance/reconcile/preview", core.FinanceView, a.previewReconcile},
		{"POST /api/finance/reconcile/import", core.FinanceEdit, a.importReconcile},
		{"GET /api/finance/reconcile", core.FinanceView, a.listReconcile},
		{"GET /api/finance/reconcile/{id}", core.FinanceView, a.getReconcile},
		{"POST /api/finance/reconcile/{id}/explain", core.FinanceView, a.explainReconcile},
		{"GET /api/finance/reports", core.FinanceView, a.report},
	}
	for _, route := range routes {
		fn := route.fn
		r.Handle(route.path, route.perm, func(w http.ResponseWriter, r *http.Request) {
			u := core.CurrentUser(r)
			if u.Role.Key != "admin" && u.Role.Scope == "shares" {
				core.Fail(w, 403, "外部协作者不能使用财务")
				return
			}
			out, err := fn(w, r)
			if err != nil {
				if !errors.Is(err, errDecoded) {
					respond(w, err)
				}
				return
			}
			core.WriteJSON(w, 200, out)
		})
	}
}

func respond(w http.ResponseWriter, err error) {
	var se *statusError
	switch {
	case errors.As(err, &se):
		core.Fail(w, se.code, se.msg)
	case errors.Is(err, errOverflow):
		log.Printf("财务金额超出计算范围：%v", err)
		core.Fail(w, 500, "金额超出可计算范围")
	case errors.Is(err, sql.ErrNoRows):
		core.Fail(w, 404, "记录不存在")
	default:
		log.Printf("财务操作失败：%v", err)
		core.Fail(w, 500, "财务操作失败")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		core.Fail(w, 400, "JSON 格式或字段类型不正确，或请求过大")
		return errDecoded
	}
	if err := d.Decode(new(any)); err != io.EOF {
		core.Fail(w, 400, "只能提交一个 JSON 值")
		return errDecoded
	}
	return nil
}

func decodeAllowed(w http.ResponseWriter, r *http.Request, allow map[string]bool) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := decode(w, r, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}
	for k := range raw {
		if !allow[k] {
			core.Fail(w, 400, "包含不支持的字段")
			return nil, errDecoded
		}
	}
	return raw, nil
}

func field[T any](raw map[string]json.RawMessage, key string) (T, bool, error) {
	var zero T
	b, ok := raw[key]
	if !ok {
		return zero, false, nil
	}
	var v T
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return zero, true, bad(400, "字段类型不正确")
	}
	return v, true, nil
}

func companyAccess(u core.User) bool {
	return u.Role.Key == "admin" || u.Role.Scope == "all"
}

func (a *Module) shopAllowed(u core.User, shopID int64) bool {
	return shopAllowedQ(a.DB, u, shopID)
}

func (a *Module) requireShop(u core.User, id int64) error {
	if id < 1 {
		return bad(400, "请选择店铺")
	}
	var n int
	if err := a.DB.QueryRow("SELECT count(*) FROM shops WHERE id=?", id).Scan(&n); err != nil {
		return err
	}
	if n == 0 || !a.shopAllowed(u, id) {
		return bad(404, "店铺不存在")
	}
	return nil
}

func (a *Module) usableShop(q querier, id int64) error {
	var archived int
	err := q.QueryRow("SELECT archived FROM shops WHERE id=?", id).Scan(&archived)
	if errors.Is(err, sql.ErrNoRows) {
		return bad(404, "店铺不存在")
	}
	if err != nil {
		return err
	}
	if archived != 0 {
		return bad(409, "店铺已归档")
	}
	return nil
}

func queryID(r *http.Request, key string) (int64, bool, error) {
	s := r.URL.Query().Get(key)
	if s == "" {
		return 0, false, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 1 {
		return 0, false, bad(400, "编号不正确")
	}
	return n, true, nil
}

func (a *Module) write(r *http.Request, action, kind string, fn func(*sql.Tx) (int64, any, any, bool, error)) (any, error) {
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id, detail, out, audit, err := fn(tx)
	if err != nil {
		return nil, err
	}
	if audit {
		if detail == nil {
			detail = core.Object{"id": id}
		}
		if err = core.AuditTx(tx, r, "finance."+action, kind, id, detail); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func shopArg(id *int64) any {
	if id == nil || *id == 0 {
		return nil
	}
	return *id
}

func sameShop(a, b *int64) bool {
	if a == nil || *a == 0 {
		return b == nil || *b == 0
	}
	return b != nil && *a == *b
}

func cloneShop(id *int64) *int64 {
	if id == nil || *id == 0 {
		return nil
	}
	v := *id
	return &v
}
