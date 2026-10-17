package pipeline

import (
	"context"
	"io/fs"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/products"
	"xstudio/internal/modules/studio"
)

var EnvConfig = core.EnvConfig
var object = core.JSONObj

func newTestApp(c Config, web fs.FS, slots []byte) (*Module, error) {
	b, e := core.New(c, web, slots)
	if e != nil {
		return nil, e
	}
	a := New(b)
	if e = b.Start(accounts.New(b), products.New(b), assets.New(b), a, studio.New(b)); e != nil {
		b.Close()
		return nil, e
	}
	_, _ = b.DB.Exec("UPDATE studio_settings SET value='false' WHERE key='require_review'")
	return a, nil
}
func testActor(ctx context.Context) context.Context {
	if core.Actor(ctx).UserID == 0 {
		return core.WithAIActor(ctx, core.AIActor{UserID: 1, Feature: "test"})
	}
	return ctx
}

const schema = `
PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY,username TEXT NOT NULL UNIQUE,password_hash BLOB NOT NULL,role TEXT NOT NULL CHECK(role IN ('admin','staff')),created_at TEXT NOT NULL,disabled INTEGER NOT NULL DEFAULT 0,version INTEGER NOT NULL DEFAULT 1);
CREATE TABLE IF NOT EXISTS products(id INTEGER PRIMARY KEY,name TEXT NOT NULL,owner_id INTEGER NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,updated_at TEXT NOT NULL,info TEXT NOT NULL DEFAULT '{}',status TEXT NOT NULL DEFAULT 'draft',step TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '',content TEXT NOT NULL DEFAULT '{}',main TEXT NOT NULL DEFAULT '{}',listing TEXT NOT NULL DEFAULT '{}');
CREATE TABLE IF NOT EXISTS photos(id INTEGER PRIMARY KEY,product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,file TEXT NOT NULL,orig_name TEXT NOT NULL,width INTEGER NOT NULL,height INTEGER NOT NULL,metrics TEXT NOT NULL DEFAULT '{}',code TEXT NOT NULL DEFAULT '',crop TEXT NOT NULL DEFAULT 'null',rotate INTEGER NOT NULL DEFAULT 0,white_bg INTEGER NOT NULL DEFAULT 0,ai_note TEXT NOT NULL DEFAULT '',retouch_log TEXT NOT NULL DEFAULT '{}',manual INTEGER NOT NULL DEFAULT 0);
CREATE UNIQUE INDEX IF NOT EXISTS photo_code ON photos(product_id,code) WHERE code<>'';
CREATE TABLE IF NOT EXISTS jobs(id INTEGER PRIMARY KEY,product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,kind TEXT NOT NULL,status TEXT NOT NULL,progress INTEGER NOT NULL DEFAULT 0,message TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,finished_at TEXT NOT NULL DEFAULT '',steps TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS active_job ON jobs(product_id) WHERE status IN ('running','queued');
CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY,user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,version INTEGER NOT NULL,expires INTEGER NOT NULL);
`

const maxPhotoBytes int64 = 25 << 20

var imageSize = core.ImageSize

func (a *Module) product(id int64) (Product, error) {
	var lid int64
	if e := a.DB.QueryRow("SELECT id FROM listings WHERE product_id=? ORDER BY id LIMIT 1", id).Scan(&lid); e != nil {
		return Product{}, e
	}
	return a.PipelineProduct(lid)
}
