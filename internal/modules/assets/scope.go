package assets

import "xstudio/internal/core"

// productScopeSQL must stay consistent with core.CanProduct. The query must
// join assets as a and products as p, including for administrator requests.
func productScopeSQL(u core.User) (string, []any) {
	if u.Role.Key == "admin" {
		return "1=1", nil
	}
	if u.Role.Scope == "shares" {
		return "EXISTS(SELECT 1 FROM shares s WHERE s.user_id=? AND s.product_id=a.product_id)", []any{u.ID}
	}
	return "(p.created_by=? OR EXISTS(SELECT 1 FROM listings l JOIN user_shops us ON us.shop_id=l.shop_id WHERE l.product_id=p.id AND us.user_id=?))", []any{u.ID, u.ID}
}
