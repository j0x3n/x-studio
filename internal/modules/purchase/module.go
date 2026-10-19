package purchase

import (
	"log"
	"net/http"
	"unicode/utf8"
	"xstudio/internal/core"
)

type Module struct{ *core.Core }

func New(c *core.Core) *Module                   { return &Module{Core: c} }
func (*Module) Name() string                     { return "purchase" }
func (*Module) Jobs() map[string]core.JobHandler { return nil }

func (a *Module) Routes(r *core.Router) {
	a.purchaseRoutes(r)
	r.Handle("GET /api/suppliers", core.LoggedIn, a.listSuppliers)
	r.Handle("POST /api/suppliers", core.PurchaseManage, a.createSupplier)
	r.Handle("GET /api/suppliers/{id}", core.LoggedIn, a.getSupplier)
	r.Handle("PATCH /api/suppliers/{id}", core.PurchaseManage, a.patchSupplier)
}

type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }
func bad(code int, msg string) error { return &statusError{code, msg} }

func writeErr(w http.ResponseWriter, err error, logMsg, public string) {
	if err == nil {
		return
	}
	if he, ok := err.(*statusError); ok {
		core.Fail(w, he.code, he.msg)
		return
	}
	log.Print(logMsg, err)
	core.Fail(w, 500, public)
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
