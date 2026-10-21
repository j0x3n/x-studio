package assets_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"xstudio/internal/core"
	"xstudio/internal/modules/assets"
)

func TestGenerateWornNoTransactionFeatureAndSaveOutput(t *testing.T) {
	var f *fixture
	var mu sync.Mutex
	var before func()
	output := imageBytes(t, 1024, 1536)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			t.Error(r.URL.Path)
			w.WriteHeader(400)
			return
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if len(r.MultipartForm.File["image"]) != 2 || r.FormValue("prompt") != "完整 creative 提示词" || r.FormValue("size") != "1024x1536" {
			t.Error("unexpected request")
		}
		mu.Lock()
		callback := before
		mu.Unlock()
		if callback != nil {
			callback()
		}
		core.WriteJSON(w, 200, obj{"data": []obj{{"b64_json": base64.StdEncoding.EncodeToString(output)}}})
	}))
	t.Cleanup(provider.Close)
	f = newFixture(t, func(cfg *core.Config) {
		cfg.AIBaseURL = provider.URL
		cfg.AIKey = "fake"
		cfg.ImageModel = "fake"
		cfg.ImageTimeout = 2 * time.Second
	})
	pid := f.product(t, "worn", 1)
	aid := f.upload(t, pid, imageBytes(t, 40, 60))
	mid := modelCreate(t, f, 1)
	dir := filepath.Join(f.c.Cfg.DataDir, "models", fmt.Sprint(mid))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reference.png"), imageBytes(t, 30, 50), 0600); err != nil {
		t.Fatal(err)
	}
	wbExec(t, f, "INSERT INTO model_references(model_id,body_part,source,file,orig_name,width,height,created_by,created_at) VALUES(?,'ear','original','reference.png','reference',30,50,1,?)", mid, core.Now())
	request := assets.WornRequest{UserID: 1, ModelID: mid, ProductID: pid, SourceAssets: []int64{aid}, BodyPart: "ear", Framing: "closeup", Size: "1024x1536", Prompt: "完整 creative 提示词"}
	before = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		var count int
		if err := f.c.DB.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&count); err != nil {
			t.Error("AI was called with a SQLite transaction held", err)
		}
	}
	ctx := core.WithFeature(context.Background(), "creative")
	data, op, err := assets.GenerateWorn(ctx, f.c, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || op["model_id"] != mid {
		t.Fatal(op)
	}
	var feature string
	var uid int64
	if err = f.c.DB.QueryRow("SELECT feature,user_id FROM ai_usage WHERE kind='image' ORDER BY id DESC LIMIT 1").Scan(&feature, &uid); err != nil || feature != "creative" || uid != 1 {
		t.Fatal(feature, uid, err)
	}
	tx, err := f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := assets.SaveOutput(context.Background(), tx, f.c, assets.OutputRequest{ProductID: pid, ParentID: aid, PNG: data, Source: "ai_generated", Title: "AI 主图", UserID: 1, Op: op})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var kind, code, tags string
	if err = f.c.DB.QueryRow("SELECT kind,code,tags FROM assets WHERE id=?", saved).Scan(&kind, &code, &tags); err != nil || kind != "output" || code != "" || tags != "[\"AI出图\"]" {
		t.Fatal(kind, code, tags, err)
	}
	photos, err := f.c.Photos(pid)
	if err != nil || len(photos) != 1 {
		t.Fatal(len(photos), err)
	}
	var file string
	_ = f.c.DB.QueryRow("SELECT file FROM assets WHERE id=?", saved).Scan(&file)
	res, err := f.client.Get(fmt.Sprintf("%s/files/products/%d/%s", f.s.URL, pid, file))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("output URL unavailable", res.StatusCode)
	}
	wbExec(t, f, "UPDATE shop_models SET archived=1 WHERE id=?", mid)
	tx, err = f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = assets.SaveOutput(context.Background(), tx, f.c, assets.OutputRequest{ProductID: pid, ParentID: aid, PNG: data, Source: "ai_generated", Title: "过期模特结果", UserID: 1, Op: op})
	_ = tx.Rollback()
	var publishError *assets.WornError
	if !errors.As(err, &publishError) || publishError.Status != 404 {
		t.Fatal("archived model result published", err)
	}
	_, _, err = assets.GenerateWorn(ctx, f.c, request)
	var scoped *assets.WornError
	if !errors.As(err, &scoped) || scoped.Status != 404 {
		t.Fatal(err)
	}
}

func TestGenerateWornRechecksPermissionsAfterAI(t *testing.T) {
	var f *fixture
	var roleID int64
	output := imageBytes(t, 1024, 1024)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if _, err := f.c.DB.Exec("UPDATE roles SET perms='[]' WHERE id=?", roleID); err != nil {
			t.Error(err)
		}
		core.WriteJSON(w, 200, obj{"data": []obj{{"b64_json": base64.StdEncoding.EncodeToString(output)}}})
	}))
	t.Cleanup(provider.Close)
	f = newFixture(t, func(cfg *core.Config) {
		cfg.AIBaseURL = provider.URL
		cfg.AIKey = "fake"
		cfg.ImageTimeout = 2 * time.Second
	})
	pid := f.product(t, "worn", 1)
	aid := f.upload(t, pid, imageBytes(t, 30, 30))
	mid := modelCreate(t, f, 1)
	dir := filepath.Join(f.c.Cfg.DataDir, "models", fmt.Sprint(mid))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "r.png"), imageBytes(t, 20, 20), 0600); err != nil {
		t.Fatal(err)
	}
	wbExec(t, f, "INSERT INTO model_references(model_id,body_part,source,file,orig_name,width,height,created_by,created_at) VALUES(?,'ear','original','r.png','r',20,20,1,?)", mid, core.Now())
	wbExec(t, f, "INSERT INTO roles(key,name,builtin,scope,perms,created_at) VALUES('worn_worker','worker',0,'shops',?,?)", core.JS([]string{core.ModelView, core.AssetProcess, core.CreativeRun}), core.Now())
	_ = f.c.DB.QueryRow("SELECT id FROM roles WHERE key='worn_worker'").Scan(&roleID)
	uid := f.user(t, "worn_worker", roleID, 1)
	ctx, cancel := context.WithTimeout(core.WithFeature(context.Background(), "creative"), 3*time.Second)
	defer cancel()
	_, _, err := assets.GenerateWorn(ctx, f.c, assets.WornRequest{UserID: uid, ModelID: mid, ProductID: pid, SourceAssets: []int64{aid}, BodyPart: "ear", Framing: "closeup", Size: "1024x1024", Prompt: "test"})
	var scoped *assets.WornError
	if !errors.As(err, &scoped) || scoped.Status != 403 {
		t.Fatal("revoked user received result", err)
	}
}
