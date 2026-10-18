package assistant

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"xstudio/internal/core"
)

type Module struct{ *core.Core }

func New(c *core.Core) *Module                   { return &Module{c} }
func (*Module) Name() string                     { return "assistant" }
func (*Module) Migrations() []core.Migration     { return nil }
func (*Module) Jobs() map[string]core.JobHandler { return nil }
func (a *Module) Routes(r *core.Router) {
	r.Handle("GET /api/home", core.LoggedIn, a.home)
	r.Handle("POST /api/assistant/chat", core.LoggedIn, a.chat)
}

type object = core.Object

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func number(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

// These SQL predicates mirror CanProduct/CanListing, including the shares
// branch taking precedence over ownership and shop assignments.
func productScope(u core.User) (string, []any) {
	if u.Role.Key == "admin" {
		return "1=1", nil
	}
	if u.Role.Scope == "shares" {
		return "EXISTS(SELECT 1 FROM shares sh WHERE sh.product_id=p.id AND sh.user_id=?)", []any{u.ID}
	}
	return "(p.created_by=? OR EXISTS(SELECT 1 FROM listings sl JOIN user_shops us ON us.shop_id=sl.shop_id WHERE sl.product_id=p.id AND us.user_id=?))", []any{u.ID, u.ID}
}
func listingScope(u core.User) (string, []any) {
	if u.Role.Key == "admin" {
		return "1=1", nil
	}
	if u.Role.Scope != "shops" {
		return "0=1", nil
	}
	return "EXISTS(SELECT 1 FROM user_shops us WHERE us.shop_id=l.shop_id AND us.user_id=?)", []any{u.ID}
}

func (a *Module) assetProduct(u core.User, id int64) (int64, bool) {
	var pid int64
	err := a.DB.QueryRow("SELECT product_id FROM assets WHERE id=?", id).Scan(&pid)
	return pid, err == nil && a.CanProduct(u, pid)
}

func parseArgs(raw json.RawMessage, out any) error {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" || !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		return fmt.Errorf("参数必须是对象")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return fmt.Errorf("参数字段或类型不正确")
	}
	return nil
}

func (a *Module) home(w http.ResponseWriter, r *http.Request) {
	out, err := a.today(core.CurrentUser(r), true)
	if err != nil {
		core.Fail(w, 500, "读取首页统计失败")
		return
	}
	core.WriteJSON(w, 200, out)
}
