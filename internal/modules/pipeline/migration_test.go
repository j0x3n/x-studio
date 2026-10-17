package pipeline

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/products"
)

func legacyFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	db, e := sql.Open("sqlite", filepath.Join(dir, "app.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(schema + `ALTER TABLE photos ADD COLUMN ai_generated INTEGER NOT NULL DEFAULT 0;ALTER TABLE photos ADD COLUMN ai_enhanced INTEGER NOT NULL DEFAULT 0;
INSERT INTO users(id,username,password_hash,role,created_at) VALUES(1,'oldadmin',X'00','admin','2026-01-01T00:00:00Z'),(2,'oldstaff',X'00','staff','2026-01-01T00:00:00Z');
INSERT INTO products(id,name,owner_id,created_at,updated_at,content,main,listing,status,step,error) VALUES(5,'有成品',2,'2026-01-01T00:00:00Z','2026-01-02T00:00:00Z','{"name":"旧文案"}','{"imgs":[]}','{"title":"旧标题"}','ready','render',''),(8,'没成品',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','{}','{}','{}','failed','classify','旧错误');
INSERT INTO photos(id,product_id,file,orig_name,width,height,code,ai_generated,ai_enhanced) VALUES(10,5,'原图/a.png','a.png',8,8,'A1',1,0),(11,8,'原图/b.png','b.png',8,8,'B1',0,1);
INSERT INTO jobs(id,product_id,kind,status,created_at,steps) VALUES(20,5,'run_all','done','2026-01-01T00:00:00Z','check,classify,enhance,retouch,copy,render'),(21,8,'classify','queued','2026-01-01T00:00:00Z','classify');`); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"products/5/导出/主图/3x4/01.jpg", "products/5/导出.zip", "products/5/原图/a.png", "products/8/原图/b.png"} {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if e = os.MkdirAll(filepath.Dir(full), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(full, []byte("旧文件"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	return dir
}
func TestLegacyFoundationMigrationAllTablesFilesAndBackup(t *testing.T) {
	dir := legacyFixture(t)
	c := Config{DataDir: dir, SessionSecret: strings.Repeat("s", 32), Workers: 1}
	for restart := 0; restart < 2; restart++ {
		a, e := newTestApp(c, nil, nil)
		if e != nil {
			t.Fatal(e)
		}
		var n int
		checks := []string{
			"SELECT count(*) FROM users u JOIN roles r ON u.role_id=r.id WHERE u.id=1 AND r.key='admin'",
			"SELECT count(*) FROM users u JOIN roles r ON u.role_id=r.id JOIN user_shops us ON us.user_id=u.id WHERE u.id=2 AND r.key='operator'",
			"SELECT count(*) FROM shops WHERE name='淘宝店' AND platform='taobao' AND language='zh-CN'",
			"SELECT count(*) FROM products WHERE id=5 AND created_by=2 AND status='pending'",
			"SELECT count(*) FROM products WHERE id=8 AND created_by=1 AND status='sampling'",
			`SELECT count(*) FROM listings WHERE product_id=5 AND content='{"name":"旧文案"}' AND main='{"imgs":[]}' AND meta='{"title":"旧标题"}' AND pipeline_status='ready' AND pipeline_step='render'`,
			"SELECT count(*) FROM listings WHERE product_id=8 AND pipeline_error='旧错误'",
			"SELECT count(*) FROM assets WHERE id=10 AND source='ai_generated' AND created_by=2",
			"SELECT count(*) FROM assets WHERE id=11 AND source='ai_enhanced' AND created_by=1",
			"SELECT count(*) FROM jobs j JOIN listings l ON j.target_id=l.id WHERE j.id=20 AND j.target_type='listing' AND l.product_id=5 AND j.user_id=2",
			"SELECT count(*) FROM jobs j JOIN listings l ON j.target_id=l.id WHERE j.id=21 AND j.status='queued' AND l.product_id=8",
		}
		for _, q := range checks {
			if e = a.DB.QueryRow(q).Scan(&n); e != nil || n != 1 {
				a.Close()
				t.Fatalf("迁移断言失败：%s：%d %v", q, n, e)
			}
		}
		var lid int64
		_ = a.DB.QueryRow("SELECT id FROM listings WHERE product_id=5").Scan(&lid)
		for _, p := range []string{filepath.Join(a.ListingDir(lid), "导出.zip"), filepath.Join(a.ListingDir(lid), "导出", "主图", "3x4", "01.jpg"), filepath.Join(a.Dir(5), "原图", "a.png")} {
			if _, e = os.Stat(p); e != nil {
				a.Close()
				t.Fatal(e)
			}
		}
		if _, e = os.Stat(filepath.Join(a.Dir(5), "导出.zip")); !os.IsNotExist(e) {
			a.Close()
			t.Fatal("旧导出未搬移")
		}
		_ = a.DB.QueryRow("SELECT count(*) FROM pragma_table_info('users') WHERE name='role'").Scan(&n)
		if n != 0 {
			a.Close()
			t.Fatal("残留旧 role 字段")
		}
		if e = a.Close(); e != nil {
			t.Fatal(e)
		}
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "app.db.bak-*"))
	if len(backups) != 1 {
		t.Fatal("备份缺失或重复迁移", backups)
	}
}

type failedMigration struct{}

func (failedMigration) Name() string { return "failure" }
func (failedMigration) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(*sql.Tx) error { return fmt.Errorf("测试迁移失败") }}}
}
func (failedMigration) Routes(*core.Router)              {}
func (failedMigration) Jobs() map[string]core.JobHandler { return nil }
func TestLegacyMigrationFailureRollsBackDatabaseAndFiles(t *testing.T) {
	dir := legacyFixture(t)
	c, e := core.New(Config{DataDir: dir, SessionSecret: strings.Repeat("s", 32)}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	p := New(c)
	e = c.Start(accounts.New(c), products.New(c), assets.New(c), p, failedMigration{})
	if e == nil || !strings.Contains(e.Error(), "测试迁移失败") {
		c.Close()
		t.Fatal(e)
	}
	var n int
	if e = c.DB.QueryRow("SELECT count(*) FROM products WHERE owner_id=2 AND id=5").Scan(&n); e != nil || n != 1 {
		t.Fatal("旧库没有回滚", e)
	}
	_ = c.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('listings','assets','schema_migrations')").Scan(&n)
	if n != 0 {
		t.Fatal("迁移表未回滚")
	}
	for _, name := range []string{"products/5/导出.zip", "products/5/导出/主图/3x4/01.jpg"} {
		if _, e = os.Stat(filepath.Join(dir, filepath.FromSlash(name))); e != nil {
			t.Fatal("旧文件未恢复", e)
		}
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "app.db.bak-*"))
	if len(backups) != 1 {
		t.Fatal("迁移失败时备份不存在")
	}
	c.Close()
}
