package assets

import "database/sql"

func modelInPlan(tx *sql.Tx, id int64) (bool, error) {
	var count int
	if err := tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='creative_plans'").Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		if err := tx.QueryRow(`SELECT count(*) FROM creative_plans p,json_each(p.models) slots,json_each(slots.value) models WHERE models.value=?`, id).Scan(&count); err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	err := tx.QueryRow(`SELECT count(*) FROM products p,json_each(json_extract(p.plan_overrides,'$.models')) slots,json_each(slots.value) models WHERE models.value=?`, id).Scan(&count)
	return count > 0, err
}
