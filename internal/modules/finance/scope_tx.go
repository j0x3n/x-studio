package finance

import "xstudio/internal/core"

// Authorization during a write must use its transaction. Core intentionally
// has one SQLite connection, so querying a.DB inside that transaction blocks.
func shopAllowedQ(q querier, u core.User, shopID int64) bool {
	if u.Disabled || u.Role.Scope == "shares" {
		return false
	}
	if shopID <= 0 {
		return companyAccess(u)
	}
	if u.Role.Key == "admin" {
		return true
	}
	if u.Role.Scope != "shops" {
		return false
	}
	var count int
	return q.QueryRow("SELECT count(*) FROM user_shops WHERE user_id=? AND shop_id=?", u.ID, shopID).Scan(&count) == nil && count > 0
}
