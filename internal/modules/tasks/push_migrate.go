package tasks

import "database/sql"

func migratePush(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE push_subscriptions(
	id INTEGER PRIMARY KEY,
	owner INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	endpoint TEXT NOT NULL UNIQUE,
	p256dh TEXT NOT NULL,
	auth TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS push_subscriptions_owner ON push_subscriptions(owner);
`)
	return err
}
