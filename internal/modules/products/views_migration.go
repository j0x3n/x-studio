package products

import "database/sql"

func migrateViewsAndPlans(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE categories ADD COLUMN views TEXT NOT NULL DEFAULT '[]';
ALTER TABLE products ADD COLUMN based_on_id INTEGER REFERENCES products(id) ON DELETE SET NULL;
ALTER TABLE products ADD COLUMN plan_id INTEGER;
ALTER TABLE products ADD COLUMN plan_overrides TEXT NOT NULL DEFAULT '{}';
CREATE INDEX products_plan ON products(plan_id);`)
	return err
}
