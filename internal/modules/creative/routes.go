package creative

import (
	"context"
	"database/sql"
	"net/http"
	"xstudio/internal/core"
)

func itemBusyTx(ctx context.Context, tx *sql.Tx, item Item) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs j WHERE j.status IN ('queued','running') AND ((j.target_type='creative_item' AND (j.target_id=? OR j.target_id IN (SELECT id FROM creative_items WHERE set_id=?))) OR (j.target_type='creative_set' AND j.target_id=?))`, item.ID, item.SetID, item.SetID).Scan(&count)
	return count > 0, err
}

func (a *Module) Routes(r *core.Router) {
	routes := []struct {
		pattern, permission string
		handler             http.HandlerFunc
	}{
		{"GET /api/creative/plans", core.ProductView, a.plansList},
		{"POST /api/creative/plans", core.CreativeTemplate, a.planWrite},
		{"GET /api/creative/plans/{id}", core.ProductView, a.planGet},
		{"PATCH /api/creative/plans/{id}", core.CreativeTemplate, a.planWrite},
		{"DELETE /api/creative/plans/{id}", core.CreativeTemplate, a.planDelete},
		{"GET /api/products/{id}/plan", core.ProductView, a.productPlan},
		{"GET /api/creative/templates", core.CreativeRun, a.templatesList},
		{"GET /api/creative/templates/{key}", core.CreativeRun, a.templateGet},
		{"PATCH /api/creative/templates/{key}", core.CreativeTemplate, a.templatePatch},
		{"POST /api/creative/templates/{key}/trial", core.CreativeTemplate, a.templateTrial},
		{"GET /api/creative/shops/{id}/settings", core.CreativeRun, a.shopGet},
		{"PUT /api/creative/shops/{id}/settings", core.CreativeTemplate, a.shopPut},
		{"PATCH /api/creative/shops/{id}/settings", core.CreativeTemplate, a.shopPut},
		{"GET /api/listings/{id}/creative", core.CreativeRun, a.setsList},
		{"POST /api/listings/{id}/creative", core.CreativeRun, a.setsCreate},
		{"GET /api/creative/sets/{id}", core.CreativeRun, a.setGet},
		{"DELETE /api/creative/sets/{id}", core.CreativeRun, a.setDelete},
		{"POST /api/creative/sets/{id}/continue", core.CreativeRun, a.setContinue},
		{"POST /api/creative/sets/{id}/glance", core.CreativeReview, a.setGlance},
		{"POST /api/creative/sets/{id}/export", core.CreativeRun, a.exportPost},
		{"GET /api/creative/sets/{id}/export", core.CreativeRun, a.exportGet},
		{"POST /api/creative/items/{id}/redraw", core.CreativeRun, a.itemRedraw},
		{"POST /api/creative/items/{id}/retext", core.CreativeRun, a.itemRetext},
		{"POST /api/creative/items/{id}/choose", core.CreativeReview, a.itemChoose},
		{"POST /api/creative/items/{id}/judge", core.CreativeReview, a.itemJudge},
		{"GET /api/creative/review", core.CreativeReview, a.reviewQueue},
		{"GET /api/creative/stats", core.CreativeReview, a.stats},
		{"POST /api/creative/suggestions/{id}", core.CreativeReview, a.suggestionUpdate},
		{"PATCH /api/creative/calibration/{category}", core.CreativeTemplate, a.calibrationUpdate},
	}
	for _, route := range routes {
		permission, handler := route.permission, route.handler
		r.Handle(route.pattern, permission, func(w http.ResponseWriter, r *http.Request) {
			if _, err := a.authorize(r.Context(), core.CurrentUser(r).ID, 0, permission); err != nil && permission != core.ProductView {
				fail(w, err)
				return
			}
			handler(w, r)
		})
	}
}
func (a *Module) listingScope(w http.ResponseWriter, r *http.Request, id int64) bool {
	return a.RequireListing(w, r, id, false)
}
func (a *Module) setScope(w http.ResponseWriter, r *http.Request) (Set, bool) {
	set, err := a.set(r.Context(), core.ID(r, "id"))
	if err != nil {
		fail(w, err)
		return set, false
	}
	return set, a.listingScope(w, r, set.ListingID)
}
func (a *Module) itemScope(w http.ResponseWriter, r *http.Request) (Item, bool) {
	item, err := a.item(r.Context(), core.ID(r, "id"))
	if err != nil {
		fail(w, err)
		return item, false
	}
	return item, a.listingScope(w, r, item.ListingID)
}
func (a *Module) itemBusy(item Item) bool {
	var count int
	_ = a.DB.QueryRow(`SELECT count(*) FROM jobs j WHERE j.status IN ('queued','running') AND ((j.target_type='creative_item' AND (j.target_id=? OR j.target_id IN (SELECT id FROM creative_items WHERE set_id=?))) OR (j.target_type='creative_set' AND j.target_id=?))`, item.ID, item.SetID, item.SetID).Scan(&count)
	return count > 0
}
func (a *Module) setBusy(id int64) bool {
	var count int
	_ = a.DB.QueryRow(`SELECT count(*) FROM jobs j WHERE j.status IN ('queued','running') AND ((j.target_type='creative_set' AND j.target_id=?) OR (j.target_type='creative_item' AND j.target_id IN(SELECT id FROM creative_items WHERE set_id=?)))`, id, id).Scan(&count)
	return count > 0
}
