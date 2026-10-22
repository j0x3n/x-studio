package assets_test

import (
	"path/filepath"
	"strings"
	"testing"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/products"
)

type oldViewAssets struct{ *assets.Module }

func (a oldViewAssets) Migrations() []core.Migration     { return a.Module.Migrations()[:7] }
func (a oldViewAssets) Routes(*core.Router)              {}
func (a oldViewAssets) Jobs() map[string]core.JobHandler { return nil }

func TestViewsAndGlobalModelsMigrationPreservesReferences(t *testing.T) {
	c, err := core.New(core.Config{DataDir: filepath.Join(t.TempDir(), "data"), AdminUser: "admin", AdminPassword: "password123", SessionSecret: strings.Repeat("s", 32)}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	account, product, asset := accounts.New(c), products.New(c), assets.New(c)
	if err = c.Start(account, product, oldViewAssets{asset}); err != nil {
		t.Fatal(err)
	}
	_, err = c.DB.Exec(`INSERT INTO products(id,name,created_by,created_at,updated_at) VALUES(1,'old',1,'old','old');
INSERT INTO assets(id,product_id,parent_id,file,orig_name,width,height,code,created_by,created_at) VALUES(1,1,NULL,'original.png','original',10,10,'',1,'old'),(2,1,1,'processed.png','processed',10,10,'A1',1,'old'),(3,1,2,'version.png','version',10,10,'',1,'old');
INSERT INTO shop_models(id,shop_id,name,created_by,created_at,updated_at) VALUES(7,1,'existing',1,'old','old');
INSERT INTO model_references(id,model_id,body_part,source,file,orig_name,width,height,created_by,created_at) VALUES(9,7,'ear','original','ear.png','ear',10,10,1,'old');
UPDATE shop_models SET cover_reference_id=9 WHERE id=7;
CREATE TABLE model_consumer(model_id INTEGER REFERENCES shop_models(id));
INSERT INTO model_consumer VALUES(7);`)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Migrate([]core.Module{account, product, asset}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = c.DB.QueryRow("SELECT count(*) FROM assets WHERE view_code='A1'").Scan(&count); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	var cover int64
	if err = c.DB.QueryRow("SELECT cover_reference_id FROM shop_models WHERE id=7 AND shop_id=1").Scan(&cover); err != nil || cover != 9 {
		t.Fatal(cover, err)
	}
	if _, err = c.DB.Exec("UPDATE shop_models SET shop_id=NULL WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.DB.Exec("UPDATE shop_models SET id=8 WHERE id=7"); err == nil {
		t.Fatal("immutable id was lost")
	}
	if err = c.DB.QueryRow("SELECT model_id FROM model_references WHERE id=9").Scan(&cover); err != nil || cover != 7 {
		t.Fatal(cover, err)
	}
	if err = c.Migrate([]core.Module{account, product, asset}); err != nil {
		t.Fatal(err)
	}
}
