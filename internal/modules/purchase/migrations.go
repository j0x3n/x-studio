package purchase

import (
	"database/sql"
	"strings"
	"xstudio/internal/core"
)

const schema = `
CREATE TABLE suppliers(
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	contact TEXT NOT NULL DEFAULT '',
	phone TEXT NOT NULL DEFAULT '',
	wechat TEXT NOT NULL DEFAULT '',
	address TEXT NOT NULL DEFAULT '',
	products TEXT NOT NULL DEFAULT '',
	settlement TEXT NOT NULL DEFAULT 'cash' CHECK(settlement IN ('cash','monthly')),
	payment_days INTEGER NOT NULL DEFAULT 0 CHECK(payment_days >= 0 AND payment_days <= 365),
	note TEXT NOT NULL DEFAULT '',
	archived INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`

func (a *Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		if _, err := tx.Exec(schema); err != nil {
			return err
		}
		cols, err := core.Columns(tx, "products")
		if err != nil {
			return err
		}
		if !cols["supplier"] {
			return nil
		}
		rows, err := tx.Query("SELECT DISTINCT json_extract(supplier,'$.name') FROM products WHERE json_valid(supplier)")
		if err != nil {
			return err
		}
		var names []string
		seen := map[string]bool{}
		for rows.Next() {
			var raw sql.NullString
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return err
			}
			if !raw.Valid {
				continue
			}
			name := strings.TrimSpace(raw.String)
			if name == "" {
				continue
			}
			if runeLen(name) > 50 {
				name = clip(name, 50)
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		now := core.Now()
		for _, name := range names {
			if _, err = tx.Exec("INSERT OR IGNORE INTO suppliers(name,created_at,updated_at) VALUES(?,?,?)", name, now, now); err != nil {
				return err
			}
		}
		return nil
	}}, {Version: 2, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(ordersSchema)
		return err
	}}}
}
