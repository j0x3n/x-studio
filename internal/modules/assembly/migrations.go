package assembly

import (
	"database/sql"
	"xstudio/internal/core"
)

const schema = `
CREATE TABLE assembly_recipes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	output_item_id INTEGER NOT NULL REFERENCES inv_items(id),
	output_qty INTEGER NOT NULL CHECK(output_qty > 0),
	currency TEXT NOT NULL DEFAULT 'CNY',
	labor_cost INTEGER NOT NULL DEFAULT 0 CHECK(labor_cost >= 0),
	article_id INTEGER REFERENCES kb_articles(id),
	note TEXT NOT NULL DEFAULT '',
	archived INTEGER NOT NULL DEFAULT 0 CHECK(archived IN (0, 1)),
	created_by INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX idx_assembly_recipes_output ON assembly_recipes(output_item_id);
CREATE INDEX idx_assembly_recipes_archived ON assembly_recipes(archived);

CREATE TABLE assembly_recipe_lines (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	recipe_id INTEGER NOT NULL REFERENCES assembly_recipes(id) ON DELETE CASCADE,
	item_id INTEGER NOT NULL REFERENCES inv_items(id),
	qty INTEGER NOT NULL CHECK(qty > 0),
	loss_bps INTEGER NOT NULL DEFAULT 0 CHECK(loss_bps >= 0 AND loss_bps <= 10000),
	created_at TEXT NOT NULL,
	UNIQUE(recipe_id, item_id)
);
CREATE INDEX idx_assembly_recipe_lines_item ON assembly_recipe_lines(item_id);

CREATE TABLE assembly_orders (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	no TEXT NOT NULL UNIQUE,
	recipe_id INTEGER NOT NULL REFERENCES assembly_recipes(id),
	runs INTEGER NOT NULL CHECK(runs > 0),
	warehouse_id INTEGER NOT NULL REFERENCES inv_warehouses(id),
	status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft', 'done', 'canceled')),
	output_item_id INTEGER NOT NULL REFERENCES inv_items(id),
	output_qty INTEGER NOT NULL CHECK(output_qty > 0),
	currency TEXT NOT NULL DEFAULT 'CNY',
	labor_cost INTEGER NOT NULL DEFAULT 0 CHECK(labor_cost >= 0),
	material_cost INTEGER NOT NULL DEFAULT 0 CHECK(material_cost >= 0),
	total_cost INTEGER NOT NULL DEFAULT 0 CHECK(total_cost >= 0),
	output_batch_id INTEGER REFERENCES inv_batches(id),
	recipe_snapshot TEXT NOT NULL DEFAULT '{}',
	note TEXT NOT NULL DEFAULT '',
	created_by INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	done_at TEXT,
	canceled_at TEXT
);
CREATE INDEX idx_assembly_orders_status ON assembly_orders(status);
CREATE INDEX idx_assembly_orders_warehouse ON assembly_orders(warehouse_id);
CREATE INDEX idx_assembly_orders_output_item ON assembly_orders(output_item_id);

CREATE TABLE assembly_order_lines (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	order_id INTEGER NOT NULL REFERENCES assembly_orders(id) ON DELETE CASCADE,
	item_id INTEGER NOT NULL REFERENCES inv_items(id),
	unit_qty INTEGER NOT NULL CHECK(unit_qty > 0),
	loss_bps INTEGER NOT NULL DEFAULT 0 CHECK(loss_bps >= 0 AND loss_bps <= 10000),
	required_qty INTEGER NOT NULL CHECK(required_qty > 0),
	actual_cost INTEGER NOT NULL DEFAULT 0 CHECK(actual_cost >= 0),
	currency TEXT NOT NULL DEFAULT 'CNY',
	created_at TEXT NOT NULL,
	UNIQUE(order_id, item_id)
);
CREATE INDEX idx_assembly_order_lines_item ON assembly_order_lines(item_id);
`

func (*Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(schema)
		return err
	}}}
}
