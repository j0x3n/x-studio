package finance

import (
	"database/sql"
	"xstudio/internal/core"
)

func (*Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(schema)
		return err
	}}}
}

const schema = `
CREATE TABLE fin_accounts (
 id INTEGER PRIMARY KEY,
 name TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('bank','alipay','shopee','cash')),
 currency TEXT NOT NULL,
 shop_id INTEGER REFERENCES shops(id),
 opening_balance INTEGER NOT NULL CHECK(opening_balance>=0),
 archived INTEGER NOT NULL DEFAULT 0,
 created_by INTEGER NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX fin_accounts_shop ON fin_accounts(shop_id, archived, id);

CREATE TABLE fin_entries (
 id INTEGER PRIMARY KEY,
 account_id INTEGER NOT NULL REFERENCES fin_accounts(id),
 direction TEXT NOT NULL CHECK(direction IN ('in','out')),
 amount INTEGER NOT NULL CHECK(amount>0),
 category TEXT NOT NULL,
 note TEXT NOT NULL DEFAULT '',
 occurred_at TEXT NOT NULL,
 shop_id INTEGER,
 source_type TEXT NOT NULL CHECK(source_type IN ('manual','payment','expense')),
 source_id INTEGER NOT NULL DEFAULT 0,
 created_by INTEGER NOT NULL,
 created_at TEXT NOT NULL
);
CREATE INDEX fin_entries_account ON fin_entries(account_id, id);
CREATE INDEX fin_entries_occurred ON fin_entries(occurred_at, id);

CREATE TABLE fin_payments (
 id INTEGER PRIMARY KEY,
 purchase_id INTEGER NOT NULL,
 supplier_id INTEGER NOT NULL,
 account_id INTEGER NOT NULL REFERENCES fin_accounts(id),
 amount INTEGER NOT NULL CHECK(amount>0),
 currency TEXT NOT NULL,
 note TEXT NOT NULL DEFAULT '',
 occurred_at TEXT NOT NULL,
 request_key TEXT NOT NULL UNIQUE,
 params_hash TEXT NOT NULL,
 entry_id INTEGER NOT NULL DEFAULT 0,
 created_by INTEGER NOT NULL,
 created_at TEXT NOT NULL
);
CREATE INDEX fin_payments_po ON fin_payments(purchase_id);
CREATE INDEX fin_payments_supplier ON fin_payments(supplier_id, occurred_at);

CREATE TABLE fin_expenses (
 id INTEGER PRIMARY KEY,
 shop_id INTEGER,
 account_id INTEGER NOT NULL REFERENCES fin_accounts(id),
 amount INTEGER NOT NULL CHECK(amount>0),
 currency TEXT NOT NULL,
 category TEXT NOT NULL,
 note TEXT NOT NULL DEFAULT '',
 occurred_at TEXT NOT NULL,
 request_key TEXT NOT NULL UNIQUE,
 params_hash TEXT NOT NULL,
 entry_id INTEGER NOT NULL DEFAULT 0,
 created_by INTEGER NOT NULL,
 created_at TEXT NOT NULL
);
CREATE INDEX fin_expenses_shop ON fin_expenses(shop_id, occurred_at, id);

CREATE TABLE fin_rates (
 month TEXT NOT NULL,
 currency TEXT NOT NULL,
 rate_micros INTEGER NOT NULL CHECK(rate_micros>0),
 updated_by INTEGER NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(month, currency)
);

CREATE TABLE fin_reconcile_batches (
 id INTEGER PRIMARY KEY,
 shop_id INTEGER NOT NULL,
 currency TEXT NOT NULL,
 request_key TEXT NOT NULL UNIQUE,
 params_hash TEXT NOT NULL,
 row_count INTEGER NOT NULL,
 inserted INTEGER NOT NULL,
 skipped INTEGER NOT NULL,
 matched INTEGER NOT NULL,
 created_by INTEGER NOT NULL,
 created_at TEXT NOT NULL
);
CREATE INDEX fin_reconcile_batches_shop ON fin_reconcile_batches(shop_id, id);

CREATE TABLE fin_reconcile_rows (
 id INTEGER PRIMARY KEY,
 batch_id INTEGER NOT NULL REFERENCES fin_reconcile_batches(id),
 shop_id INTEGER NOT NULL,
 currency TEXT NOT NULL,
 order_no TEXT NOT NULL,
 amount INTEGER NOT NULL CHECK(amount>=0),
 fee INTEGER NOT NULL CHECK(fee>=0),
 shipping_insurance INTEGER NOT NULL CHECK(shipping_insurance>=0),
 refund INTEGER NOT NULL CHECK(refund>=0),
 settled_at TEXT NOT NULL,
 order_id INTEGER,
 system_no TEXT NOT NULL DEFAULT '',
 platform_no TEXT NOT NULL DEFAULT '',
 system_amount INTEGER,
 system_refund INTEGER,
 diff_amount INTEGER,
 diff_refund INTEGER,
 match_status TEXT NOT NULL,
 reason TEXT NOT NULL DEFAULT '',
 fingerprint TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX fin_reconcile_fp ON fin_reconcile_rows(shop_id, fingerprint);
CREATE INDEX fin_reconcile_settled ON fin_reconcile_rows(shop_id, settled_at);
CREATE INDEX fin_reconcile_batch ON fin_reconcile_rows(batch_id, id);
`
