package accounts

import "xstudio/internal/core"

type Module struct{ *core.Core }

func New(c *core.Core) *Module                   { return &Module{c} }
func (*Module) Name() string                     { return "accounts" }
func (*Module) Jobs() map[string]core.JobHandler { return nil }
func (a *Module) Routes(r *core.Router) {
	r.Handle("POST /api/login", core.Public, a.Login)
	r.Handle("POST /api/logout", core.LoggedIn, a.Logout)
	r.Handle("GET /api/me", core.LoggedIn, a.me)
	r.Handle("GET /api/permissions", core.AdminUsers, a.permissions)
	r.Handle("GET /api/roles", core.AdminUsers, a.roles)
	r.Handle("POST /api/roles", core.AdminUsers, a.createRole)
	r.Handle("PATCH /api/roles/{id}", core.AdminUsers, a.patchRole)
	r.Handle("DELETE /api/roles/{id}", core.AdminUsers, a.deleteRole)
	r.Handle("GET /api/users", core.AdminUsers, a.users)
	r.Handle("POST /api/users", core.AdminUsers, a.createUser)
	r.Handle("PATCH /api/users/{id}", core.AdminUsers, a.patchUser)
	r.Handle("PUT /api/users/{id}/quota", core.AdminUsers, a.quota)
	r.Handle("GET /api/shops", core.LoggedIn, a.shops)
	r.Handle("POST /api/shops", core.AdminShops, a.createShop)
	r.Handle("PATCH /api/shops/{id}", core.AdminShops, a.patchShop)
	r.Handle("GET /api/products/{id}/shares", core.ShareManage, a.shares)
	r.Handle("POST /api/products/{id}/shares", core.ShareManage, a.createShare)
	r.Handle("DELETE /api/shares/{id}", core.ShareManage, a.deleteShare)
	r.Handle("GET /api/usage", core.AdminUsers, a.usage)
	r.Handle("GET /api/usage/me", core.LoggedIn, a.usage)
	r.Handle("GET /api/audit", core.AdminAudit, a.audit)
	r.Handle("GET /api/settings", core.LoggedIn, a.settings)
	r.Handle("PATCH /api/settings", core.AdminSettings, a.patchSettings)
}
