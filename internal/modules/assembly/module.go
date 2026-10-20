package assembly

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"math/big"
	"net/http"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/modules/inventory"
)

type Module struct{ *core.Core }

func New(c *core.Core) *Module                   { return &Module{c} }
func (*Module) Name() string                     { return "assembly" }
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
		{"GET /api/assembly/recipes", core.InventoryView, a.listRecipes},
		{"POST /api/assembly/recipes", core.InventoryEdit, a.createRecipe},
		{"GET /api/assembly/recipes/{id}", core.InventoryView, a.getRecipe},
		{"PATCH /api/assembly/recipes/{id}", core.InventoryEdit, a.patchRecipe},
		{"DELETE /api/assembly/recipes/{id}", core.InventoryEdit, a.archiveRecipe},

		{"GET /api/assembly/orders", core.InventoryView, a.listOrders},
		{"POST /api/assembly/orders", core.InventoryEdit, a.createOrder},
		{"GET /api/assembly/orders/{id}", core.InventoryView, a.getOrder},
		{"POST /api/assembly/orders/{id}/complete", core.InventoryEdit, a.completeOrder},
		{"POST /api/assembly/orders/{id}/cancel", core.InventoryEdit, a.cancelOrder},
	} {
		fn := route.fn
		r.Handle(route.path, route.perm, func(w http.ResponseWriter, r *http.Request) {
			u := core.CurrentUser(r)
			if u.Role.Key != "admin" && u.Role.Scope == "shares" {
				core.Fail(w, 403, "外部协作者不能访问组装加工")
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
		core.Fail(w, 404, "记录不存在")
	case errors.Is(err, inventory.ErrShort):
		core.Fail(w, 409, err.Error())
	case strings.Contains(err.Error(), "UNIQUE constraint failed"):
		core.Fail(w, 409, "数据已存在或单号重复")
	default:
		log.Printf("组装加工操作失败：%v", err)
		core.Fail(w, 500, "组装加工操作失败")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		core.Fail(w, 400, "JSON 格式、字段或类型不正确")
		return errDecoded
	}
	if err := d.Decode(new(any)); err != io.EOF {
		core.Fail(w, 400, "只能提交一个 JSON 对象")
		return errDecoded
	}
	return nil
}

func costAllowed(u core.User) bool {
	return u.Role.Key == "admin" || u.Has(core.ProductCost) || u.Has(core.FinanceView)
}

func costEditAllowed(u core.User) bool {
	return u.Role.Key == "admin" || u.Has(core.ProductCost) || u.Has(core.FinanceEdit)
}

func redactRecipe(u core.User, r core.Object) {
	if !u.Has(core.KBView) {
		delete(r, "article_id")
		delete(r, "article_title")
	}
	if !costAllowed(u) {
		delete(r, "labor_cost")
	}
}

func redactOrder(u core.User, o core.Object) {
	if snap, ok := o["recipe_snapshot"].(map[string]any); ok {
		redactRecipe(u, snap)
	} else {
		delete(o, "recipe_snapshot")
	}
	if !costAllowed(u) {
		delete(o, "labor_cost")
		delete(o, "material_cost")
		delete(o, "total_cost")
		delete(o, "unit_cost")
		if lines, ok := o["lines"].([]core.Object); ok {
			for _, l := range lines {
				delete(l, "actual_cost")
				delete(l, "unit_cost")
			}
		}
		if snap, ok := o["recipe_snapshot"].(map[string]any); ok {
			delete(snap, "labor_cost")
			delete(snap, "material_cost")
			delete(snap, "total_cost")
		}
	}
}

func sum(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, bad(400, "数量或金额超出整数范围")
	}
	return a + b, nil
}

// calcRequiredQty calculates ceil(qty * runs * (10000+loss_bps) / 10000) with overflow protection.
func calcRequiredQty(qty, runs, lossBps int64) (int64, error) {
	if qty <= 0 || runs <= 0 || lossBps < 0 || lossBps > 10000 {
		return 0, bad(400, "物料用量、组装批数或损耗率不正确")
	}
	q := big.NewInt(qty)
	r := big.NewInt(runs)
	factor := big.NewInt(10000 + lossBps)
	num := new(big.Int).Mul(q, r)
	num.Mul(num, factor)
	num.Add(num, big.NewInt(9999))
	num.Quo(num, big.NewInt(10000))
	if !num.IsInt64() {
		return 0, bad(400, "扣料数量超出整数范围")
	}
	res := num.Int64()
	if res <= 0 {
		return 0, bad(400, "扣料数量必须大于0")
	}
	return res, nil
}

func calcTotalLabor(laborCost, runs int64) (int64, error) {
	if laborCost < 0 || runs <= 0 {
		return 0, bad(400, "工费或组装批数不正确")
	}
	if laborCost == 0 {
		return 0, nil
	}
	l := big.NewInt(laborCost)
	r := big.NewInt(runs)
	total := new(big.Int).Mul(l, r)
	if !total.IsInt64() {
		return 0, bad(400, "总工费金额超出整数范围")
	}
	return total.Int64(), nil
}

func calcTotalOutputQty(outputQty, runs int64) (int64, error) {
	if outputQty <= 0 || runs <= 0 {
		return 0, bad(400, "产出数量或组装批数不正确")
	}
	q := big.NewInt(outputQty)
	r := big.NewInt(runs)
	total := new(big.Int).Mul(q, r)
	if !total.IsInt64() {
		return 0, bad(400, "总产出数量超出整数范围")
	}
	return total.Int64(), nil
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
			if b, ok := v.([]byte); ok {
				v = string(b)
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
