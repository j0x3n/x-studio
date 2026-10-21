package assets_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xstudio/internal/core"
)

func TestDeleteArchivedModelKeepsGeneratedProductImage(t *testing.T) {
	f := newFixture(t, nil)
	mid := modelCreate(t, f, 1)
	dir := filepath.Join(f.c.Cfg.DataDir, "models", fmt.Sprint(mid))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ref.png", "old.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), imageBytes(t, 12, 12), 0600); err != nil {
			t.Fatal(err)
		}
	}
	wbExec(t, f, `INSERT INTO model_references(model_id,body_part,source,file,orig_name,width,height,created_by,created_at) VALUES(?,'hand','original','ref.png','ref.png',12,12,1,?)`, mid, core.Now())
	wbExec(t, f, `INSERT INTO model_references(model_id,body_part,source,file,orig_name,width,height,created_by,created_at,deleted_at) VALUES(?,'hand','original','old.png','old.png',12,12,1,?,?)`, mid, core.Now(), core.Now())
	var rid int64
	if err := f.c.DB.QueryRow("SELECT id FROM model_references WHERE model_id=? AND file='ref.png'", mid).Scan(&rid); err != nil {
		t.Fatal(err)
	}
	wbExec(t, f, "UPDATE shop_models SET cover_reference_id=? WHERE id=?", rid, mid)
	pid := f.product(t, "已出图", 1)
	aid := f.upload(t, pid, imageBytes(t, 20, 20))
	wbExec(t, f, "UPDATE assets SET source='ai_generated', kind='output' WHERE id=?", aid)
	var file string
	if err := f.c.DB.QueryRow("SELECT file FROM assets WHERE id=?", aid).Scan(&file); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(file, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	imageURL := fmt.Sprintf("/files/products/%d/%s", pid, strings.Join(parts, "/"))
	wbExec(t, f, `CREATE TABLE creative_items(id INTEGER PRIMARY KEY, model_id INTEGER NOT NULL DEFAULT 0, output_asset_id INTEGER NOT NULL DEFAULT 0)`)
	wbExec(t, f, `INSERT INTO creative_items(model_id, output_asset_id) VALUES(?,?)`, mid, aid)
	readImage := func() {
		t.Helper()
		res, err := f.client.Get(f.s.URL + imageURL)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || len(body) == 0 {
			t.Fatal(res.StatusCode, len(body))
		}
	}
	readImage()
	path := fmt.Sprintf("/api/models/%d", mid)
	if got := f.req(t, "DELETE", path, nil, 409)["error"]; got != "先归档再删除" {
		t.Fatal(got)
	}
	if _, err := os.Stat(filepath.Join(dir, "ref.png")); err != nil {
		t.Fatal(err)
	}
	f.user(t, "photographer-delete", 4, 1)
	shop2 := id(f.req(t, "POST", "/api/shops", obj{"name": "第二店", "platform": "taobao", "language": "zh-CN"}, 201)["shop"])
	f.user(t, "outsider-model", 2, shop2)
	f.login(t, "photographer-delete")
	f.req(t, "DELETE", path, nil, 403)
	f.login(t, "outsider-model")
	f.req(t, "DELETE", path, nil, 404)
	f.login(t, "admin")
	other := modelCreate(t, f, 1)
	f.req(t, "PATCH", fmt.Sprintf("/api/models/%d", other), obj{"archived": true}, 200)
	f.req(t, "DELETE", fmt.Sprintf("/api/models/%d", other), nil, 200)
	var otherLeft int
	if err := f.c.DB.QueryRow("SELECT count(*) FROM shop_models WHERE id=?", other).Scan(&otherLeft); err != nil || otherLeft != 0 {
		t.Fatal(otherLeft, err)
	}
	f.req(t, "PATCH", path, obj{"archived": true}, 200)
	wbExec(t, f, `CREATE TABLE creative_shop_settings(shop_id INTEGER PRIMARY KEY, models TEXT NOT NULL DEFAULT '{}', template_set TEXT NOT NULL DEFAULT '{}', updated_by INTEGER, updated_at TEXT NOT NULL)`)
	wbExec(t, f, `INSERT INTO creative_shop_settings(shop_id, models, updated_at) VALUES(1, ?, ?)`, core.JS(obj{"female_main": mid, "male": 0}), core.Now())
	if got := f.req(t, "DELETE", path, nil, 409)["error"]; got != "这个模特还在「设置 → AI 出图」的模特位里，先换掉再删除" {
		t.Fatal(got)
	}
	wbExec(t, f, `UPDATE creative_shop_settings SET models='{}' WHERE shop_id=1`)
	wbExec(t, f, `INSERT INTO creative_shop_settings(shop_id, models, updated_at) VALUES(?, ?, ?)`, shop2, core.JS(obj{"hand": mid}), core.Now())
	if got := f.req(t, "DELETE", path, nil, 409)["error"]; got != "这个模特还在「设置 → AI 出图」的模特位里，先换掉再删除" {
		t.Fatal(got)
	}
	wbExec(t, f, `UPDATE creative_shop_settings SET models=? WHERE shop_id=?`, core.JS(obj{"hand": other}), shop2)
	f.req(t, "DELETE", path, nil, 200)
	for _, name := range []string{"ref.png", "old.png"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatal(name, err)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("模特目录还在", err)
	}
	var models, references int
	if err := f.c.DB.QueryRow("SELECT count(*) FROM shop_models WHERE id=?", mid).Scan(&models); err != nil || models != 0 {
		t.Fatal(models, err)
	}
	if err := f.c.DB.QueryRow("SELECT count(*) FROM model_references WHERE model_id=?", mid).Scan(&references); err != nil || references != 0 {
		t.Fatal(references, err)
	}
	f.req(t, "GET", path, nil, 404)
	readImage()
	var source string
	var modelID, outputID int64
	if err := f.c.DB.QueryRow("SELECT source FROM assets WHERE id=?", aid).Scan(&source); err != nil || source != "ai_generated" {
		t.Fatal(source, err)
	}
	if err := f.c.DB.QueryRow("SELECT model_id, output_asset_id FROM creative_items").Scan(&modelID, &outputID); err != nil || modelID != mid || outputID != aid {
		t.Fatal(modelID, outputID, err)
	}
	var detail string
	if err := f.c.DB.QueryRow("SELECT detail FROM audit_log WHERE action='model.delete' AND target_type='model' AND target_id=?", mid).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	var audited obj
	if err := json.Unmarshal([]byte(detail), &audited); err != nil || audited["name"] != "模特甲" || audited["reference_count"] != float64(2) {
		t.Fatal(detail, err)
	}
}
