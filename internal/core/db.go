package core

import (
	"database/sql"
	"encoding/json"
)

// QueryObjects closes its rows before returning so callers can safely enrich
// records with other queries on SQLite's single connection.
func QueryObjects(db *sql.DB, query string, args ...any) ([]Object, error) {
	rows, e := db.Query(query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	cols, e := rows.Columns()
	if e != nil {
		return nil, e
	}
	out := []Object{}
	for rows.Next() {
		vs := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vs {
			ptrs[i] = &vs[i]
		}
		if e = rows.Scan(ptrs...); e != nil {
			return nil, e
		}
		o := Object{}
		for i, k := range cols {
			v := vs[i]
			switch k {
			case "info", "cost", "supplier", "content", "main", "meta", "settings", "perms", "detail", "params", "result", "tags", "op", "metrics", "crop", "retouch_log":
				if s, ok := v.(string); ok {
					var x any
					if json.Unmarshal([]byte(s), &x) == nil {
						v = x
					}
				}
			case "builtin", "disabled", "archived", "can_upload", "white_bg", "manual", "ai_label":
				if n, ok := v.(int64); ok {
					v = n != 0
				}
			}
			o[k] = v
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
