package studio

import "xstudio/internal/core"

type Module struct {
	*core.Core
}

func New(c *core.Core) *Module                   { return &Module{Core: c} }
func (*Module) Name() string                     { return "studio" }
func (*Module) Jobs() map[string]core.JobHandler { return nil }

func (a *Module) Routes(r *core.Router) {
	r.Handle("GET /api/templates", core.ProductView, a.listTemplates)
	r.Handle("PUT /api/shops/{id}/template-prefs", core.TemplateManage, a.putTemplatePrefs)
	r.Handle("GET /api/listings/{id}/render", core.ProductView, a.getListingRender)
	r.Handle("PUT /api/listings/{id}/render", core.RenderRun, a.putListingRender)
	r.Handle("POST /api/listings/{id}/render/run", core.RenderRun, a.runListingRender)
	r.Handle("POST /api/render/batch", core.RenderRun, a.runBatchRender)
	r.Handle("POST /api/listings/{id}/review", core.RenderReview, a.reviewListing)
	r.Handle("GET /api/review/queue", core.RenderReview, a.reviewQueue)
	r.Handle("GET /api/studio/settings", core.LoggedIn, a.getSettings)
	r.Handle("PATCH /api/studio/settings", core.AdminSettings, a.patchSettings)
}
