package products

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"xstudio/internal/core"
)

const productTable = `CREATE TABLE products_new(id INTEGER PRIMARY KEY,name TEXT NOT NULL,created_by INTEGER NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,updated_at TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'sampling',info TEXT NOT NULL DEFAULT '{}',cost TEXT NOT NULL DEFAULT '{}',supplier TEXT NOT NULL DEFAULT '{}');`

func (a *Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		cols, e := core.Columns(tx, "products")
		if e != nil {
			return e
		}
		if _, e = tx.Exec(`CREATE TABLE listings(id INTEGER PRIMARY KEY,product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,shop_id INTEGER NOT NULL REFERENCES shops(id),status TEXT NOT NULL DEFAULT 'draft',url TEXT NOT NULL DEFAULT '',content TEXT NOT NULL DEFAULT '{}',main TEXT NOT NULL DEFAULT '{}',meta TEXT NOT NULL DEFAULT '{}',pipeline_status TEXT NOT NULL DEFAULT 'draft',pipeline_step TEXT NOT NULL DEFAULT '',pipeline_error TEXT NOT NULL DEFAULT '',created_by INTEGER NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,updated_at TEXT NOT NULL,ai_label INTEGER NOT NULL DEFAULT 1,render_ai_label INTEGER NOT NULL DEFAULT 1,UNIQUE(product_id,shop_id));` + productTable); e != nil {
			return e
		}
		if cols["owner_id"] {
			if _, e = tx.Exec(`INSERT INTO listings(product_id,shop_id,status,content,main,meta,pipeline_status,pipeline_step,pipeline_error,created_by,created_at,updated_at) SELECT id,(SELECT id FROM shops ORDER BY id LIMIT 1),CASE status WHEN 'ready' THEN 'ready' ELSE 'draft' END,content,main,listing,status,step,error,owner_id,created_at,updated_at FROM products;
INSERT INTO products_new(id,name,created_by,created_at,updated_at,status,info) SELECT id,name,owner_id,created_at,updated_at,'sampling',info FROM products;`); e != nil {
				return e
			}
			rows, e := tx.Query("SELECT id,product_id FROM listings")
			if e != nil {
				return e
			}
			var ids [][2]int64
			for rows.Next() {
				var pair [2]int64
				if e = rows.Scan(&pair[0], &pair[1]); e != nil {
					rows.Close()
					return e
				}
				ids = append(ids, pair)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			for _, ids := range ids {
				old := a.Dir(ids[1])
				dst := a.ListingDir(ids[0])
				hasOutput := false
				for _, name := range []string{"导出", "导出.zip"} {
					if _, e := os.Stat(filepath.Join(old, name)); e == nil {
						hasOutput = true
					}
					if e = a.MoveLegacy(filepath.Join(old, name), filepath.Join(dst, name)); e != nil {
						return e
					}
				}
				if hasOutput {
					if _, e = tx.Exec("UPDATE products_new SET status='pending' WHERE id=?", ids[1]); e != nil {
						return e
					}
				}
			}
			if _, e = tx.Exec("DROP TABLE products"); e != nil {
				return fmt.Errorf("替换旧商品结构失败：%w", e)
			}
		}
		_, e = tx.Exec("ALTER TABLE products_new RENAME TO products")
		return e
	}}}
}
