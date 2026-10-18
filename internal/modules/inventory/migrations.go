package inventory

import (
	"database/sql"
	"xstudio/internal/core"
)

func (*Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(schema)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO inv_warehouses(name,type,created_at,updated_at) VALUES('自有仓','own',?,?)`, core.Now(), core.Now())
		return err
	}}}
}

const schema = `
CREATE TABLE inv_items(
 id INTEGER PRIMARY KEY, code TEXT NOT NULL COLLATE NOCASE UNIQUE, code_key TEXT NOT NULL UNIQUE, name TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('sku','material','packaging')), product_id INTEGER REFERENCES products(id) ON DELETE SET NULL,
 category_id INTEGER REFERENCES categories(id), spec TEXT NOT NULL DEFAULT '{}', unit TEXT NOT NULL, units TEXT NOT NULL DEFAULT '[]',
 safety_stock INTEGER NOT NULL DEFAULT 0 CHECK(safety_stock>=0), low_since TEXT, note TEXT NOT NULL DEFAULT '', archived INTEGER NOT NULL DEFAULT 0,
 created_by INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 CHECK(kind='sku' OR product_id IS NULL)
);
CREATE TABLE inv_warehouses(
 id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, type TEXT NOT NULL CHECK(type IN ('own','stall','dropship')),
 supplier_id INTEGER, address TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '', sort INTEGER NOT NULL DEFAULT 0,
 archived INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE inv_batches(
 id INTEGER PRIMARY KEY, item_id INTEGER NOT NULL REFERENCES inv_items(id), warehouse_id INTEGER NOT NULL REFERENCES inv_warehouses(id),
 qty_in INTEGER NOT NULL CHECK(qty_in>0), qty_left INTEGER NOT NULL CHECK(qty_left>=0 AND qty_left<=qty_in),
 cost_in INTEGER NOT NULL CHECK(cost_in>=0), cost_left INTEGER NOT NULL CHECK(cost_left>=0 AND cost_left<=cost_in),
 currency TEXT NOT NULL, grade TEXT NOT NULL DEFAULT '', supplier_id INTEGER, received_at TEXT NOT NULL,
 source_batch_id INTEGER REFERENCES inv_batches(id), ref_type TEXT NOT NULL, ref_id INTEGER NOT NULL, created_at TEXT NOT NULL,
 CHECK(qty_left>0 OR cost_left=0)
);
CREATE INDEX inv_batches_fifo ON inv_batches(item_id,warehouse_id,received_at,id);
CREATE INDEX inv_batches_warehouse ON inv_batches(warehouse_id,item_id);
CREATE TABLE inv_ledger(
 id INTEGER PRIMARY KEY, item_id INTEGER NOT NULL REFERENCES inv_items(id), warehouse_id INTEGER NOT NULL REFERENCES inv_warehouses(id),
 batch_id INTEGER REFERENCES inv_batches(id), kind TEXT NOT NULL, qty INTEGER NOT NULL CHECK(qty<>0), cost INTEGER NOT NULL,
 currency TEXT NOT NULL, ref_type TEXT NOT NULL, ref_id INTEGER NOT NULL, note TEXT NOT NULL DEFAULT '', user_id INTEGER NOT NULL, created_at TEXT NOT NULL
);
CREATE INDEX inv_ledger_item ON inv_ledger(item_id,id);
CREATE INDEX inv_ledger_ref ON inv_ledger(ref_type,ref_id);
CREATE TABLE inv_reservations(
 id INTEGER PRIMARY KEY, item_id INTEGER NOT NULL REFERENCES inv_items(id), qty INTEGER NOT NULL CHECK(qty>0),
 ref_type TEXT NOT NULL, ref_id INTEGER NOT NULL, created_at TEXT NOT NULL, UNIQUE(ref_type,ref_id,item_id)
);
CREATE INDEX inv_reservations_item ON inv_reservations(item_id);
CREATE TABLE inv_dropship(
 warehouse_id INTEGER NOT NULL REFERENCES inv_warehouses(id), item_id INTEGER NOT NULL REFERENCES inv_items(id),
 qty INTEGER CHECK(qty>=0), unit_cost INTEGER CHECK(unit_cost>=0), currency TEXT NOT NULL DEFAULT 'CNY', updated_at TEXT NOT NULL,
 PRIMARY KEY(warehouse_id,item_id)
);
CREATE TABLE inv_counts(
 id INTEGER PRIMARY KEY, warehouse_id INTEGER NOT NULL REFERENCES inv_warehouses(id),
 status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','done','canceled')), note TEXT NOT NULL DEFAULT '',
 created_by INTEGER NOT NULL, created_at TEXT NOT NULL, done_at TEXT
);
CREATE UNIQUE INDEX inv_counts_draft ON inv_counts(warehouse_id) WHERE status='draft';
CREATE TABLE inv_count_lines(
 count_id INTEGER NOT NULL REFERENCES inv_counts(id), item_id INTEGER NOT NULL REFERENCES inv_items(id), system_qty INTEGER NOT NULL,
 counted_qty INTEGER CHECK(counted_qty>=0), unit_cost INTEGER CHECK(unit_cost>=0), PRIMARY KEY(count_id,item_id)
);
CREATE TABLE inv_operations(id INTEGER PRIMARY KEY, kind TEXT NOT NULL, created_at TEXT NOT NULL);
`
