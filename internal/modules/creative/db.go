package creative

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"xstudio/internal/core"
	"xstudio/internal/modules/assets"
)

func (*Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE creative_templates(
 id INTEGER PRIMARY KEY,key TEXT NOT NULL UNIQUE,name TEXT NOT NULL DEFAULT '',kind TEXT NOT NULL DEFAULT '',slot TEXT NOT NULL DEFAULT '',categories TEXT NOT NULL DEFAULT '[]',purpose TEXT NOT NULL DEFAULT '',prompt TEXT NOT NULL DEFAULT '',style_refs TEXT NOT NULL DEFAULT '[]',texts TEXT NOT NULL DEFAULT '[]',photo_codes TEXT NOT NULL DEFAULT '[]',product_mode TEXT NOT NULL DEFAULT '',product_box TEXT NOT NULL DEFAULT '[]',gen_size TEXT NOT NULL DEFAULT '',export TEXT NOT NULL DEFAULT '[]',count INTEGER NOT NULL DEFAULT 1,wear TEXT NOT NULL DEFAULT 'null',version INTEGER NOT NULL DEFAULT 1,enabled INTEGER NOT NULL DEFAULT 1,source TEXT NOT NULL DEFAULT 'custom',updated_by INTEGER,updated_at TEXT NOT NULL);
 CREATE TABLE creative_sets(id INTEGER PRIMARY KEY,listing_id INTEGER NOT NULL REFERENCES listings(id) ON DELETE CASCADE,version INTEGER NOT NULL,status TEXT NOT NULL DEFAULT 'draft',template_keys TEXT NOT NULL DEFAULT '{}',params TEXT NOT NULL DEFAULT '{}',parts TEXT NOT NULL DEFAULT '[]',hero_b_item_id INTEGER,images_used INTEGER NOT NULL DEFAULT 0,images_allowance INTEGER NOT NULL,glanced_by INTEGER,glanced_at TEXT NOT NULL DEFAULT '',exported_at TEXT NOT NULL DEFAULT '',export_file TEXT NOT NULL DEFAULT '',created_by INTEGER NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,UNIQUE(listing_id,version));
 CREATE TABLE creative_items(id INTEGER PRIMARY KEY,set_id INTEGER REFERENCES creative_sets(id) ON DELETE CASCADE,listing_id INTEGER NOT NULL REFERENCES listings(id) ON DELETE CASCADE,slot TEXT NOT NULL,seq INTEGER NOT NULL DEFAULT 1,template_key TEXT NOT NULL,template_version INTEGER NOT NULL,template_snapshot TEXT NOT NULL DEFAULT '{}',attempt INTEGER NOT NULL DEFAULT 1,auto_attempt INTEGER NOT NULL DEFAULT 0,superseded INTEGER NOT NULL DEFAULT 0,variant TEXT NOT NULL DEFAULT '',product_mode TEXT NOT NULL,source_asset_ids TEXT NOT NULL DEFAULT '[]',model_id INTEGER NOT NULL DEFAULT 0,model_ref_ids TEXT NOT NULL DEFAULT '[]',wear_type TEXT NOT NULL DEFAULT '',size_compare INTEGER NOT NULL DEFAULT 0,prompt TEXT NOT NULL DEFAULT '',texts TEXT NOT NULL DEFAULT '[]',output_asset_id INTEGER NOT NULL DEFAULT 0,canvas_file TEXT NOT NULL DEFAULT '',hard TEXT NOT NULL DEFAULT '{}',review_a TEXT NOT NULL DEFAULT '{}',review_b TEXT NOT NULL DEFAULT '{}',route TEXT NOT NULL DEFAULT 'pending',route_reasons TEXT NOT NULL DEFAULT '[]',risk INTEGER NOT NULL DEFAULT 0,human TEXT NOT NULL DEFAULT '',human_note TEXT NOT NULL DEFAULT '',human_by INTEGER,human_at TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '',created_by INTEGER NOT NULL,created_at TEXT NOT NULL);
 CREATE UNIQUE INDEX creative_current_position ON creative_items(set_id,slot,seq,variant) WHERE superseded=0 AND set_id IS NOT NULL;
 CREATE INDEX creative_review_queue ON creative_items(route,superseded,human,risk);
 CREATE TABLE creative_shop_settings(shop_id INTEGER PRIMARY KEY REFERENCES shops(id),models TEXT NOT NULL DEFAULT '{}',template_set TEXT NOT NULL DEFAULT '{}',updated_by INTEGER,updated_at TEXT NOT NULL);
 CREATE TABLE creative_calibration(category TEXT PRIMARY KEY,total INTEGER NOT NULL DEFAULT 0,agree INTEGER NOT NULL DEFAULT 0,open INTEGER NOT NULL DEFAULT 0,tightened INTEGER NOT NULL DEFAULT 0,tightened_reason TEXT NOT NULL DEFAULT '',updated_at TEXT NOT NULL);
 CREATE TABLE creative_suggestions(id INTEGER PRIMARY KEY,listing_id INTEGER NOT NULL REFERENCES listings(id) ON DELETE CASCADE,set_id INTEGER,item_id INTEGER,slot TEXT NOT NULL,reason_code TEXT NOT NULL,count INTEGER NOT NULL,status TEXT NOT NULL DEFAULT 'open',created_at TEXT NOT NULL,updated_at TEXT NOT NULL,UNIQUE(listing_id,slot,reason_code));
 CREATE TABLE creative_return_scan(id INTEGER PRIMARY KEY CHECK(id=1),watermark INTEGER NOT NULL DEFAULT 0);
 INSERT INTO creative_return_scan(id) VALUES(1);
 CREATE TABLE creative_return_counts(listing_id INTEGER NOT NULL REFERENCES listings(id) ON DELETE CASCADE,reason_code TEXT NOT NULL,count INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(listing_id,reason_code));`)
		return err
	}}, {Version: 2, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE creative_templates ADD COLUMN lock_shape TEXT NOT NULL DEFAULT '';
ALTER TABLE creative_templates ADD COLUMN size_compare INTEGER;
ALTER TABLE creative_templates ADD COLUMN hero_style TEXT NOT NULL DEFAULT '';`)
		if err != nil {
			return err
		}
		rows, err := objects(context.Background(), tx, "SELECT id,exported_at FROM creative_sets WHERE exported_at<>''")
		if err != nil {
			return err
		}
		for _, row := range rows {
			at, err := time.Parse(time.RFC3339Nano, stringValue(row["exported_at"]))
			if err != nil {
				return fmt.Errorf("套装 %v 的导出时间不正确：%w", row["id"], err)
			}
			if _, err = tx.Exec("UPDATE creative_sets SET exported_at=? WHERE id=?", at.UTC().Format(exportTimeLayout), row["id"]); err != nil {
				return err
			}
		}
		return nil
	}}, {Version: 3, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE creative_items ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE creative_items ADD COLUMN model_returned TEXT NOT NULL DEFAULT '';`)
		return err
	}}, {Version: 4, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE creative_items ADD COLUMN scene_snapshot TEXT NOT NULL DEFAULT '{}';
ALTER TABLE creative_shop_settings ADD COLUMN main_ratios TEXT NOT NULL DEFAULT '[]';
ALTER TABLE creative_shop_settings ADD COLUMN detail_width INTEGER NOT NULL DEFAULT 0;
ALTER TABLE creative_shop_settings ADD COLUMN about_text TEXT NOT NULL DEFAULT '';`)
		return err
	}}, {Version: 5, Up: migratePlans}}
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

var jsonColumns = map[string]bool{}

func init() {
	for _, key := range strings.Fields("categories style_refs texts photo_codes product_box export wear template_keys params parts source_asset_ids model_ref_ids hard review_a review_b route_reasons models template_set template_snapshot scene_snapshot main_ratios") {
		jsonColumns[key] = true
	}
}
func objects(ctx context.Context, db queryer, query string, args ...any) ([]core.Object, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []core.Object{}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err = rows.Scan(pointers...); err != nil {
			return nil, err
		}
		obj := core.Object{}
		for index, key := range columns {
			value := values[index]
			if raw, ok := value.([]byte); ok {
				value = string(raw)
			}
			if jsonColumns[key] {
				if raw, ok := value.(string); ok {
					var parsed any
					if json.Unmarshal([]byte(raw), &parsed) == nil {
						value = parsed
					}
				}
			}
			if value != nil && (key == "enabled" || key == "superseded" || key == "size_compare" || key == "open" || key == "tightened" || key == "archived") {
				value = value == int64(1)
			}
			obj[key] = value
		}
		out = append(out, obj)
	}
	return out, rows.Err()
}
func (a *Module) one(ctx context.Context, query string, args ...any) (core.Object, error) {
	items, err := objects(ctx, a.DB, query, args...)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, sql.ErrNoRows
	}
	return items[0], nil
}
func typed[T any](value any) (out T, err error) {
	err = json.Unmarshal([]byte(core.JS(value)), &out)
	return
}

type statusError struct {
	status  int
	message string
}

func (e *statusError) Error() string           { return e.message }
func problem(status int, message string) error { return &statusError{status, message} }
func fail(w http.ResponseWriter, err error) {
	switch value := err.(type) {
	case *statusError:
		core.Fail(w, value.status, value.message)
	case *assets.WornError:
		core.Fail(w, value.Status, value.Message)
	default:
		core.Fail(w, 500, "AI 出图处理失败："+err.Error())
	}
}
func (a *Module) authorize(ctx context.Context, uid, lid int64, perm string) (core.User, error) {
	u, err := a.User(uid)
	if err != nil || u.Disabled || u.Role.Scope == "shares" || !u.Has(perm) {
		return u, problem(403, "没有 AI 出图权限")
	}
	if lid != 0 && !a.CanListing(u, lid) {
		return u, problem(404, "上架版本不存在")
	}
	return u, nil
}
func authorizeTx(ctx context.Context, tx *sql.Tx, uid, lid int64, perm string) error {
	u, err := core.ScanUser(tx.QueryRowContext(ctx, core.UserQuery+" WHERE u.id=?", uid))
	if err != nil || u.Disabled || u.Role.Scope == "shares" || !u.Has(perm) {
		return problem(403, "没有 AI 出图权限")
	}
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM listings l WHERE l.id=? AND (? OR (?='shops' AND EXISTS(SELECT 1 FROM user_shops us WHERE us.shop_id=l.shop_id AND us.user_id=?)))`, lid, u.Role.Key == "admin", u.Role.Scope, uid).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return problem(404, "上架版本不存在")
	}
	return nil
}
func auditTx(ctx context.Context, tx *sql.Tx, uid int64, action, kind string, id int64, detail any) error {
	request, _ := http.NewRequestWithContext(ctx, "POST", "http://localhost/", nil)
	if err := core.AuditTx(tx, request, action, kind, id, detail); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE audit_log SET user_id=? WHERE id=last_insert_rowid()", uid)
	return err
}
func (a *Module) settings() core.Object { return a.SettingWithDefaults("creative") }
func number(value any) float64 {
	switch n := value.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}
