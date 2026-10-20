package orders

import (
	"database/sql"
	"xstudio/internal/core"
)

func (*Module) Migrations() []core.Migration {
	return []core.Migration{
		{Version: 1, Up: func(tx *sql.Tx) error { _, err := tx.Exec(schema); return err }},
		{Version: 2, Up: normalizeOrderDates},
		{Version: 3, Up: addReturnReasonColumns},
	}
}

func addReturnReasonColumns(tx *sql.Tx) error {
	if _, err := tx.Exec("ALTER TABLE order_return_lines ADD COLUMN reason_code TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	_, err := tx.Exec("ALTER TABLE order_return_lines ADD COLUMN creative_set_id INTEGER")
	return err
}

func normalizeOrderDates(tx *sql.Tx) error {
	rows, err := objects(tx, "SELECT id,paid_at FROM orders")
	if err != nil {
		return err
	}
	for _, row := range rows {
		paidAt, err := orderTime(row["paid_at"].(string))
		if err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE orders SET paid_at=? WHERE id=?", orderTimestamp(paidAt), row["id"]); err != nil {
			return err
		}
	}
	return nil
}

const schema = `
CREATE TABLE orders (
 id INTEGER PRIMARY KEY, no TEXT NOT NULL UNIQUE, shop_id INTEGER NOT NULL REFERENCES shops(id), platform_no TEXT NOT NULL DEFAULT '',
 source TEXT NOT NULL DEFAULT 'manual', status TEXT NOT NULL DEFAULT 'paid' CHECK(status IN ('paid','shipped','canceled')),
 buyer_name TEXT NOT NULL DEFAULT '', buyer_phone TEXT NOT NULL DEFAULT '', address TEXT NOT NULL DEFAULT '',
 buyer_note TEXT NOT NULL DEFAULT '', seller_note TEXT NOT NULL DEFAULT '', currency TEXT NOT NULL DEFAULT 'CNY',
 goods_amount INTEGER NOT NULL CHECK(goods_amount>=0), shipping_fee INTEGER NOT NULL DEFAULT 0 CHECK(shipping_fee>=0),
 discount INTEGER NOT NULL DEFAULT 0 CHECK(discount>=0), paid_amount INTEGER NOT NULL CHECK(paid_amount>=0),
 short INTEGER NOT NULL DEFAULT 0, paid_at TEXT NOT NULL, shipped_at TEXT NOT NULL DEFAULT '', canceled_at TEXT NOT NULL DEFAULT '',
 carrier TEXT NOT NULL DEFAULT '', tracking_no TEXT NOT NULL DEFAULT '', created_by INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX orders_platform ON orders(shop_id,platform_no) WHERE platform_no<>'';
CREATE INDEX orders_filter ON orders(shop_id,status,id);
CREATE TABLE order_lines (
 id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER NOT NULL REFERENCES orders(id), item_id INTEGER NOT NULL REFERENCES inv_items(id),
 code TEXT NOT NULL, title TEXT NOT NULL, qty INTEGER NOT NULL CHECK(qty>0), price INTEGER NOT NULL CHECK(price>=0),
 amount INTEGER NOT NULL CHECK(amount>=0), returned_qty INTEGER NOT NULL DEFAULT 0 CHECK(returned_qty>=0 AND returned_qty<=qty)
);
CREATE INDEX order_lines_order ON order_lines(order_id);
CREATE TABLE order_picks (
 id INTEGER PRIMARY KEY, line_id INTEGER NOT NULL REFERENCES order_lines(id), warehouse_id INTEGER NOT NULL, batch_id INTEGER,
 qty INTEGER NOT NULL CHECK(qty>0), cost INTEGER NOT NULL CHECK(cost>=0), currency TEXT NOT NULL, grade TEXT NOT NULL DEFAULT '',
 returned_qty INTEGER NOT NULL DEFAULT 0, returned_cost INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX order_picks_line ON order_picks(line_id);
CREATE TABLE order_returns (
 id INTEGER PRIMARY KEY, order_id INTEGER NOT NULL REFERENCES orders(id), status TEXT NOT NULL DEFAULT 'done',
 refund_amount INTEGER NOT NULL CHECK(refund_amount>=0), reason TEXT NOT NULL DEFAULT '', created_by INTEGER NOT NULL, created_at TEXT NOT NULL
);
CREATE INDEX order_returns_order ON order_returns(order_id);
CREATE TABLE order_return_lines (
 return_id INTEGER NOT NULL REFERENCES order_returns(id), line_id INTEGER NOT NULL REFERENCES order_lines(id),
 qty INTEGER NOT NULL CHECK(qty>0), action TEXT NOT NULL CHECK(action IN ('restock','scrap','none')), warehouse_id INTEGER,
 PRIMARY KEY(return_id,line_id)
);
`
