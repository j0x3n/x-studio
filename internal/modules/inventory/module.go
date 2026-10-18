package inventory

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"xstudio/internal/core"
)

type Module struct{ *core.Core }

func New(c *core.Core) *Module                   { return &Module{c} }
func (*Module) Name() string                     { return "inventory" }
func (*Module) Jobs() map[string]core.JobHandler { return nil }

type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }
func bad(code int, msg string) error { return &statusError{code, msg} }

type endpoint func(http.ResponseWriter, *http.Request) (any, error)

func (a *Module) Routes(r *core.Router) {
	for _, v := range []struct {
		path, perm string
		fn         endpoint
	}{
		{"GET /api/inventory/items", core.InventoryView, a.listItems},
		{"POST /api/inventory/items", core.InventoryEdit, a.writeItem},
		{"GET /api/inventory/items/lookup", core.InventoryView, a.lookup},
		{"GET /api/inventory/items/{id}", core.InventoryView, a.getItem},
		{"PATCH /api/inventory/items/{id}", core.InventoryEdit, a.writeItem},
		{"GET /api/inventory/warehouses", core.InventoryView, a.listWarehouses},
		{"POST /api/inventory/warehouses", core.InventoryEdit, a.writeWarehouse},
		{"PATCH /api/inventory/warehouses/{id}", core.InventoryEdit, a.writeWarehouse},
		{"GET /api/inventory/dropship", core.InventoryView, a.listDropship},
		{"PUT /api/inventory/dropship/{warehouse_id}/{item_id}", core.InventoryEdit, a.writeDropship},
		{"DELETE /api/inventory/dropship/{warehouse_id}/{item_id}", core.InventoryEdit, a.writeDropship},
		{"POST /api/inventory/moves", core.InventoryEdit, a.moves},
		{"POST /api/inventory/transfers", core.InventoryEdit, a.transfers},
		{"GET /api/inventory/ledger", core.InventoryView, a.listLedger},
		{"GET /api/inventory/batches", core.InventoryView, a.listBatches},
		{"GET /api/inventory/counts", core.InventoryView, a.listCounts},
		{"POST /api/inventory/counts", core.InventoryEdit, a.createCount},
		{"GET /api/inventory/counts/{id}", core.InventoryView, a.getCount},
		{"PATCH /api/inventory/counts/{id}", core.InventoryEdit, a.patchCount},
		{"POST /api/inventory/counts/{id}/finish", core.InventoryEdit, a.finishCount},
		{"POST /api/inventory/counts/{id}/cancel", core.InventoryEdit, a.cancelCount},
		{"GET /api/inventory/export.csv", core.InventoryView, a.export},
	} {
		fn := v.fn
		r.Handle(v.path, v.perm, func(w http.ResponseWriter, r *http.Request) {
			if core.CurrentUser(r).Role.Scope == "shares" {
				core.Fail(w, 403, "外部协作者不能访问库存")
				return
			}
			result, e := fn(w, r)
			if e != nil {
				if e == errDecoded {
					return
				}
				var se *statusError
				switch {
				case errors.As(e, &se):
					core.Fail(w, se.code, se.msg)
				case errors.Is(e, ErrShort):
					core.Fail(w, 409, e.Error())
				case errors.Is(e, sql.ErrNoRows):
					core.Fail(w, 404, "记录不存在")
				case strings.Contains(e.Error(), "UNIQUE constraint failed"):
					core.Fail(w, 409, "记录重复或该仓已有草稿盘点")
				default:
					log.Printf("库存操作失败：%v", e)
					core.Fail(w, 500, "库存操作失败")
				}
				return
			}
			if result != nil {
				core.WriteJSON(w, 200, result)
			}
		})
	}
}
func (a *Module) change(r *http.Request, action, kind string, fn func(*sql.Tx) (int64, any, error)) (any, error) {
	tx, e := a.DB.BeginTx(r.Context(), nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	id, result, e := fn(tx)
	if e != nil {
		return nil, e
	}
	if e = core.AuditTx(tx, r, "inventory."+action, kind, id, core.Object{"id": id}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return result, nil
}
func costAllowed(r *http.Request) bool {
	u := core.CurrentUser(r)
	return u.Has(core.ProductCost) || u.Has(core.FinanceView)
}
func redact(r *http.Request, rows []core.Object) []core.Object {
	if !costAllowed(r) {
		for _, o := range rows {
			for _, k := range []string{"cost", "cost_in", "cost_left", "cost_value", "unit_cost", "gain_cost", "loss_cost"} {
				delete(o, k)
			}
		}
	}
	return rows
}
func objects(q Querier, query string, args ...any) ([]core.Object, error) {
	rows, e := q.Query(query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	cols, e := rows.Columns()
	if e != nil {
		return nil, e
	}
	out := []core.Object{}
	for rows.Next() {
		vs := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range vs {
			ptr[i] = &vs[i]
		}
		if e = rows.Scan(ptr...); e != nil {
			return nil, e
		}
		o := core.Object{}
		for i, k := range cols {
			if k == "code_key" {
				continue
			}
			v := vs[i]
			switch k {
			case "spec", "units":
				if s, ok := v.(string); ok {
					decoder := json.NewDecoder(strings.NewReader(s))
					decoder.UseNumber()
					if e = decoder.Decode(&v); e != nil {
						return nil, e
					}
				}
			case "archived":
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
func one(q Querier, query string, args ...any) (core.Object, error) {
	rows, e := objects(q, query, args...)
	if e != nil {
		return nil, e
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	return rows[0], nil
}
func paged(r *http.Request, rows []core.Object) any {
	offset, size := core.Page(r)
	total := len(rows)
	offset = min(offset, total)
	return core.Object{"items": redact(r, rows[offset:min(offset+size, total)]), "total": total}
}

// queryPage bounds database reads for growing history lists; item lists use
// paged after applying their computed availability and low-stock filters.
func (a *Module) queryPage(r *http.Request, query string, args ...any) (core.Object, error) {
	var total int64
	if e := a.DB.QueryRow("SELECT count(*) FROM ("+query+")", args...).Scan(&total); e != nil {
		return nil, e
	}
	offset, size := core.Page(r)
	pageArgs := append(append([]any{}, args...), size, offset)
	rows, e := objects(a.DB, query+" LIMIT ? OFFSET ?", pageArgs...)
	if e != nil {
		return nil, e
	}
	return core.Object{"items": redact(r, rows), "total": total}, nil
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !core.Decode(w, r, v) {
		return errDecoded
	}
	return nil
}

var errDecoded = bad(400, "JSON 格式或字段类型不正确")
