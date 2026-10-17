package core

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func Columns(tx *sql.Tx, table string) (map[string]bool, error) {
	rows, e := tx.Query("PRAGMA table_info(" + table + ")")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid, nn, pk int
		var name, typ string
		var def any
		if e = rows.Scan(&cid, &name, &typ, &nn, &def, &pk); e != nil {
			return nil, e
		}
		out[name] = true
	}
	return out, rows.Err()
}
func AddColumns(tx *sql.Tx, table string, defs [][2]string) error {
	cols, e := Columns(tx, table)
	if e != nil {
		return e
	}
	for _, d := range defs {
		if !cols[d[0]] {
			if _, e = tx.Exec("ALTER TABLE " + table + " ADD COLUMN " + d[0] + " " + d[1]); e != nil {
				return fmt.Errorf("补齐 %s.%s 失败：%w", table, d[0], e)
			}
		}
	}
	return nil
}

const coreSchema = `
CREATE TABLE IF NOT EXISTS schema_migrations(module TEXT NOT NULL,version INTEGER NOT NULL,applied_at TEXT NOT NULL,PRIMARY KEY(module,version));
CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY,user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,version INTEGER NOT NULL,expires INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS jobs(id INTEGER PRIMARY KEY,kind TEXT NOT NULL,target_type TEXT NOT NULL,target_id INTEGER NOT NULL,user_id INTEGER NOT NULL REFERENCES users(id),params TEXT NOT NULL DEFAULT '{}',status TEXT NOT NULL,progress INTEGER NOT NULL DEFAULT 0,message TEXT NOT NULL DEFAULT '',steps TEXT NOT NULL,result TEXT NOT NULL DEFAULT '{}',created_at TEXT NOT NULL,finished_at TEXT NOT NULL DEFAULT '');
CREATE UNIQUE INDEX IF NOT EXISTS active_target_job ON jobs(target_type,target_id) WHERE status IN ('running','queued');
CREATE TABLE IF NOT EXISTS audit_log(id INTEGER PRIMARY KEY,at TEXT NOT NULL,user_id INTEGER,action TEXT NOT NULL,target_type TEXT NOT NULL,target_id INTEGER NOT NULL,detail TEXT NOT NULL,ip TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS ai_usage(id INTEGER PRIMARY KEY,at TEXT NOT NULL,user_id INTEGER NOT NULL REFERENCES users(id),feature TEXT NOT NULL,model TEXT NOT NULL,kind TEXT NOT NULL,units INTEGER NOT NULL,est_cost REAL NOT NULL,job_id INTEGER);
CREATE INDEX IF NOT EXISTS usage_user_time ON ai_usage(user_id,at);
CREATE TABLE IF NOT EXISTS quotas(user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,month_images INTEGER CHECK(month_images>=0),month_text INTEGER CHECK(month_text>=0));
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL,updated_by INTEGER,updated_at TEXT NOT NULL);
`

func (a *Core) backup() error {
	var busy, pages, written int
	if e := a.DB.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &written); e != nil {
		return fmt.Errorf("备份前同步数据库失败：%w", e)
	}
	if busy != 0 {
		return fmt.Errorf("数据库仍被其他进程使用，请停止旧服务后再迁移")
	}
	src := filepath.Join(a.Cfg.DataDir, "app.db")
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(src+".bak-"+time.Now().UTC().Format("20060102T150405.000000000"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	if e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e != nil {
		return e
	}
	return ce
}

// MoveLegacy participates in the migration's compensation journal. Both paths
// are constructed from database integer IDs and fixed directory names.
func (a *Core) MoveLegacy(src, dst string) error {
	if _, e := os.Stat(src); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	if _, e := os.Stat(dst); e == nil {
		return fmt.Errorf("迁移目标已存在：%s", dst)
	} else if !os.IsNotExist(e) {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
		return e
	}
	if e := os.Rename(src, dst); e != nil {
		return fmt.Errorf("搬移旧导出文件失败：%w", e)
	}
	a.moves = append(a.moves, [2]string{src, dst})
	return nil
}
func (a *Core) Migrate(modules []Module) (err error) {
	var legacy bool
	var n int
	if e := a.DB.QueryRow("SELECT count(*) FROM pragma_table_info('products') WHERE name='owner_id'").Scan(&n); e != nil {
		return e
	}
	legacy = n > 0
	if legacy {
		if e := a.backup(); e != nil {
			return fmt.Errorf("旧数据库备份失败：%w", e)
		}
	}
	if _, err = a.DB.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		return
	}
	defer a.DB.Exec("PRAGMA foreign_keys=ON")
	tx, e := a.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	committed := false
	defer func() {
		if !committed {
			for i := len(a.moves) - 1; i >= 0; i-- {
				m := a.moves[i]
				if e := os.Rename(m[1], m[0]); e != nil {
					err = fmt.Errorf("%v；恢复旧文件目录失败：%w", err, e)
				}
			}
		}
		a.moves = nil
	}()
	if legacy {
		if _, err = tx.Exec("ALTER TABLE jobs RENAME TO legacy_jobs; DROP INDEX IF EXISTS active_job"); err != nil {
			return
		}
	}
	if _, err = tx.Exec(coreSchema); err != nil {
		return
	}
	seen := map[string]bool{}
	for _, m := range modules {
		if seen[m.Name()] {
			return fmt.Errorf("重复模块：%s", m.Name())
		}
		seen[m.Name()] = true
		ms := append([]Migration(nil), m.Migrations()...)
		sort.Slice(ms, func(i, j int) bool { return ms[i].Version < ms[j].Version })
		last := 0
		for _, v := range ms {
			if v.Version <= last {
				return fmt.Errorf("模块迁移版本不合法：%s", m.Name())
			}
			last = v.Version
			if err = tx.QueryRow("SELECT count(*) FROM schema_migrations WHERE module=? AND version=?", m.Name(), v.Version).Scan(&n); err != nil {
				return
			}
			if n > 0 {
				continue
			}
			if err = v.Up(tx); err != nil {
				return fmt.Errorf("模块 %s 版本 %d：%w", m.Name(), v.Version, err)
			}
			if _, err = tx.Exec("INSERT INTO schema_migrations VALUES(?,?,?)", m.Name(), v.Version, Now()); err != nil {
				return
			}
		}
	}
	if legacy {
		_, err = tx.Exec(`INSERT INTO jobs(id,kind,target_type,target_id,user_id,params,status,progress,message,steps,created_at,finished_at) SELECT j.id,'pipeline','listing',l.id,p.created_by,json_object('legacy_kind',j.kind,'ai_label',1),j.status,j.progress,j.message,j.steps,j.created_at,j.finished_at FROM legacy_jobs j JOIN products p ON p.id=j.product_id JOIN listings l ON l.product_id=p.id AND l.shop_id=(SELECT id FROM shops ORDER BY id LIMIT 1); DROP TABLE legacy_jobs; DELETE FROM sessions;`)
		if err != nil {
			return
		}
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO schema_migrations VALUES('core',1,?)", Now()); err != nil {
		return
	}
	defaults := map[string]any{"ai_label": Object{"text": "AI 生成", "position": "bottom-right", "size": 14, "color": "#FFFFFF", "opacity": 0.6}, "ai_models": Object{}, "ai_prices": Object{}, "quota_default": Object{"month_images": nil, "month_text": nil}}
	for k, v := range defaults {
		if _, err = tx.Exec("INSERT OR IGNORE INTO settings(key,value,updated_at) VALUES(?,?,?)", k, JS(v), Now()); err != nil {
			return
		}
	}
	rows, e := tx.Query("PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	bad := rows.Next()
	ce := rows.Err()
	rows.Close()
	if ce != nil {
		return ce
	}
	if bad {
		return fmt.Errorf("迁移后数据引用不完整，已取消升级")
	}
	if err = tx.Commit(); err != nil {
		return
	}
	committed = true
	_, err = a.DB.Exec("PRAGMA journal_mode=WAL")
	return
}
