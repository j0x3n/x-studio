package assets

import (
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"xstudio/internal/core"
	"xstudio/internal/imaging"
)

func algorithmModule(t *testing.T) *Module {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(`CREATE TABLE users(id INTEGER PRIMARY KEY);
CREATE TABLE products(id INTEGER PRIMARY KEY,created_by INTEGER);
CREATE TABLE listings(product_id INTEGER,shop_id INTEGER);
CREATE TABLE shares(user_id INTEGER,product_id INTEGER);
CREATE TABLE user_shops(user_id INTEGER,shop_id INTEGER);
INSERT INTO users VALUES(1),(2),(3),(4),(5);
INSERT INTO products VALUES(1,1),(2,2),(3,1);
INSERT INTO listings VALUES(1,1),(2,2),(3,3);
INSERT INTO user_shops VALUES(3,1);
INSERT INTO shares VALUES(4,2);`); err != nil {
		t.Fatal(err)
	}
	a := New(&core.Core{DB: db, Cfg: core.Config{DataDir: t.TempDir()}})
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, m := range a.Migrations() {
		if err = m.Up(tx); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestProductScopeSQLMatchesCanProduct(t *testing.T) {
	a := algorithmModule(t)
	for pid := 1; pid <= 3; pid++ {
		if _, err := a.DB.Exec("INSERT INTO assets(product_id,file,orig_name,width,height,created_by,created_at) VALUES(?,'原图/a.png','a',1,1,1,?)", pid, now()); err != nil {
			t.Fatal(err)
		}
	}
	users := []core.User{
		{ID: 1, Role: core.Role{Key: "admin", Scope: "shares"}},
		{ID: 2, Role: core.Role{Key: "owner", Scope: "shops"}},
		{ID: 3, Role: core.Role{Key: "operator", Scope: "shops"}},
		{ID: 4, Role: core.Role{Key: "guest", Scope: "shares"}},
		{ID: 5, Role: core.Role{Key: "unassigned", Scope: "shops"}},
		{ID: 2, Role: core.Role{Key: "guest", Scope: "shares"}},
		{ID: 3, Role: core.Role{Key: "custom", Scope: "custom"}},
	}
	for _, u := range users {
		for pid := int64(1); pid <= 4; pid++ {
			scope, args := productScopeSQL(u)
			args = append(args, pid)
			var count int
			if err := a.DB.QueryRow("SELECT count(*) FROM assets a JOIN products p ON p.id=a.product_id WHERE "+scope+" AND p.id=?", args...).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if (count > 0) != a.CanProduct(u, pid) {
				t.Fatalf("SQL 范围与 CanProduct 不一致：用户 %d，角色 %s，商品 %d", u.ID, u.Role.Key, pid)
			}
		}
	}
}

func TestHashBackfillBatchesFailureAndCancellation(t *testing.T) {
	a := algorithmModule(t)
	if err := os.MkdirAll(filepath.Join(a.Dir(1), "原图"), 0700); err != nil {
		t.Fatal(err)
	}
	im := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	im.SetNRGBA(1, 1, color.NRGBA{200, 100, 150, 255})
	if err := imaging.SavePNG(filepath.Join(a.Dir(1), "原图/a.png"), im); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 52; i++ {
		file := "原图/a.png"
		if i == 0 {
			file = "原图/丢失.png"
		}
		if _, err := a.DB.Exec("INSERT INTO assets(product_id,file,orig_name,width,height,created_by,created_at) VALUES(1,?,'a',4,4,1,?)", file, now()); err != nil {
			t.Fatal(err)
		}
	}
	for i, want := range []int{50, 2, 0} {
		n, err := a.backfillHashBatch(context.Background())
		if err != nil || n != want {
			t.Fatal("补算批次错误", i, n, err)
		}
	}
	var good, bad int
	if err := a.DB.QueryRow("SELECT sum(length(phash)=16),sum(phash='-') FROM assets").Scan(&good, &bad); err != nil || good != 51 || bad != 1 {
		t.Fatal(good, bad, err)
	}
	if err := imaging.SavePNG(filepath.Join(a.Dir(1), "原图/丢失.png"), im); err != nil {
		t.Fatal(err)
	}
	if n, err := a.backfillHashBatch(context.Background()); err != nil || n != 0 {
		t.Fatal("丢失文件被反复重试", n, err)
	}
	if _, err := a.DB.Exec("UPDATE assets SET phash='' WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.backfillHashBatch(ctx); err != context.Canceled {
		t.Fatal("未响应取消", err)
	}
	var hash string
	a.DB.QueryRow("SELECT phash FROM assets WHERE id=2").Scan(&hash)
	if hash != "" {
		t.Fatal(fmt.Sprintf("取消后仍补算：%s", hash))
	}
}
