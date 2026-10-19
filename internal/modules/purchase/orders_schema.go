package purchase

const ordersSchema = `
CREATE TABLE purchase_orders (
 id INTEGER PRIMARY KEY, no TEXT NOT NULL UNIQUE, supplier_id INTEGER NOT NULL REFERENCES suppliers(id),
 status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','ordered','partial','received','done','canceled')),
 currency TEXT NOT NULL DEFAULT 'CNY', shipping_fee INTEGER NOT NULL DEFAULT 0 CHECK(shipping_fee>=0),
 expected_at TEXT NOT NULL DEFAULT '', product_id INTEGER, is_sample INTEGER NOT NULL DEFAULT 0,
 note TEXT NOT NULL DEFAULT '', created_by INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 ordered_at TEXT NOT NULL DEFAULT '', done_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX purchase_orders_filter ON purchase_orders(supplier_id,status,id);
CREATE TABLE purchase_lines (
 id INTEGER PRIMARY KEY AUTOINCREMENT, po_id INTEGER NOT NULL REFERENCES purchase_orders(id),
 item_id INTEGER NOT NULL REFERENCES inv_items(id), qty INTEGER NOT NULL CHECK(qty>0),
 amount INTEGER NOT NULL CHECK(amount>=0), received_qty INTEGER NOT NULL DEFAULT 0, rejected_qty INTEGER NOT NULL DEFAULT 0,
 shipping_share INTEGER NOT NULL DEFAULT 0, accounted_cost INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX purchase_lines_po ON purchase_lines(po_id);
CREATE INDEX purchase_lines_item ON purchase_lines(item_id,po_id);
CREATE TABLE purchase_receipts (
 id INTEGER PRIMARY KEY, po_id INTEGER NOT NULL REFERENCES purchase_orders(id), warehouse_id INTEGER NOT NULL,
 received_at TEXT NOT NULL, note TEXT NOT NULL DEFAULT '', created_by INTEGER NOT NULL, created_at TEXT NOT NULL
);
CREATE INDEX purchase_receipts_po ON purchase_receipts(po_id);
CREATE TABLE purchase_receipt_lines (
 id INTEGER PRIMARY KEY, receipt_id INTEGER NOT NULL REFERENCES purchase_receipts(id), line_id INTEGER NOT NULL REFERENCES purchase_lines(id),
 qty INTEGER NOT NULL CHECK(qty>0), rejected INTEGER NOT NULL CHECK(rejected>=0 AND rejected<=qty),
 grades TEXT NOT NULL DEFAULT '[]', batch_ids TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE purchase_returns (
 id INTEGER PRIMARY KEY, po_id INTEGER NOT NULL REFERENCES purchase_orders(id), line_id INTEGER NOT NULL REFERENCES purchase_lines(id),
 qty INTEGER NOT NULL CHECK(qty>0), amount INTEGER NOT NULL CHECK(amount>=0), status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','done')),
 note TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, done_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX purchase_returns_po ON purchase_returns(po_id);
`
