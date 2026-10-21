package pipeline

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xstudio"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/creative"
	"xstudio/internal/modules/products"
	"xstudio/internal/modules/studio"
)

func newAIFixture(t *testing.T) *fixture {
	t.Helper()
	web, _ := fs.Sub(xstudio.Assets, "web/dist")
	slots, _ := xstudio.Assets.ReadFile("槽位.json")
	root := t.TempDir()
	cfg := Config{
		DataDir:       filepath.Join(root, "data"),
		TemplateDir:   filepath.Join("..", "..", "..", "templates"),
		PromptDir:     filepath.Join(root, "prompts"),
		AdminUser:     "admin",
		AdminPassword: "password123",
		SessionSecret: strings.Repeat("s", 32),
		Workers:       1,
		AIBaseURL:     "http://127.0.0.1:1/v1",
		RembgURL:      "http://127.0.0.1:1",
	}
	base, err := core.New(cfg, web, slots)
	if err != nil {
		t.Fatal(err)
	}
	app := New(base)
	if err = base.Start(accounts.New(base), products.New(base), assets.New(base), app, studio.New(base), creative.New(base)); err != nil {
		base.Close()
		t.Fatal(err)
	}
	_, _ = base.DB.Exec("UPDATE studio_settings SET value='false' WHERE key='require_review'")
	server := httptest.NewServer(app.Handler())
	app.SetBaseURL(server.URL)
	jar, _ := cookiejar.New(nil)
	f := &fixture{app, server, &http.Client{Jar: jar}}
	t.Cleanup(func() {
		server.Close()
		if err := app.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}

func (f *fixture) listingOf(t *testing.T, productID int64) int64 {
	t.Helper()
	var id int64
	if err := f.a.DB.QueryRow("SELECT id FROM listings WHERE product_id=? ORDER BY id LIMIT 1", productID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fixture) runRender(t *testing.T, listingID int64) Job {
	t.Helper()
	body := f.req(t, "POST", fmt.Sprintf("/api/listings/%d/render/run", listingID), nil, 202)
	return waitJob(t, f.a, int64(body["job"].(Object)["id"].(float64)))
}

func TestAIRenderCreatesSetWithoutChromeOrZip(t *testing.T) {
	f := newAIFixture(t)
	f.login(t, "admin")
	productID := f.product(t)
	listingID := f.listingOf(t, productID)
	up := f.upload(t, productID, "a1.png", photoBytes(t, 80, 80), 201)
	photoID := int64(up["photos"].([]any)[0].(Object)["id"].(float64))
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", photoID), Object{"code": "A1"}, 200)
	job := f.runRender(t, listingID)
	if job.Status != "done" || !strings.Contains(job.Message, "已转 AI 出图") || !strings.Contains(job.Message, "缺素材跳过") {
		t.Fatal(job.Status, job.Message)
	}
	var setID, jobID int64
	var parts, rendered, review, note string
	var renderedJob int64
	if err := f.a.DB.QueryRow("SELECT id, parts FROM creative_sets WHERE listing_id=?", listingID).Scan(&setID, &parts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parts, "main") || !strings.Contains(parts, "detail") {
		t.Fatal(parts)
	}
	if err := f.a.DB.QueryRow("SELECT id FROM jobs WHERE kind='creative.main' AND target_type='creative_set' AND target_id=?", setID).Scan(&jobID); err != nil || jobID == 0 {
		t.Fatal(jobID, err)
	}
	if err := f.a.DB.QueryRow("SELECT rendered_template, review_status, render_note, rendered_job_id FROM listing_render WHERE listing_id=?", listingID).Scan(&rendered, &review, &note, &renderedJob); err != nil {
		t.Fatal(err)
	}
	if rendered != "ai" || review != "none" || renderedJob != job.ID || !strings.Contains(note, fmt.Sprintf("套装 #%d", setID)) || !strings.Contains(note, "缺素材跳过") || !strings.Contains(note, "主图 2") {
		t.Fatal(rendered, review, renderedJob, note)
	}
	var skipped, kept int
	if err := f.a.DB.QueryRow("SELECT count(*) FROM creative_items WHERE set_id=? AND instr(route_reasons,'缺少素材')>0", setID).Scan(&skipped); err != nil || skipped == 0 {
		t.Fatal(skipped, err)
	}
	if err := f.a.DB.QueryRow("SELECT count(*) FROM creative_items WHERE set_id=? AND instr(route_reasons,'缺少素材')=0", setID).Scan(&kept); err != nil || kept == 0 {
		t.Fatal(kept, err)
	}
	if _, err := os.Stat(filepath.Join(f.a.ListingDir(listingID), "导出.zip")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if f.a.browser.ctx != nil {
		t.Fatal("chrome started")
	}
	var detail string
	if err := f.a.DB.QueryRow("SELECT detail FROM audit_log WHERE action='creative.create' AND target_id=?", setID).Scan(&detail); err != nil || !strings.Contains(detail, `"source":"pipeline"`) {
		t.Fatal(detail, err)
	}
	res, err := f.client.Get(fmt.Sprintf("%s/api/listings/%d/download", f.s.URL, listingID))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 409 || !strings.Contains(string(body), "AI 出图请到商品详情的「AI 出图」标签导出") {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	// 上一套还在生成时，再跑一键出图不开新的一套
	// 等这一套的任务（含自动排的后续任务）都停下，再把第一个任务改回 running 模拟还在生成
	deadline := time.Now().Add(2 * time.Minute)
	for active := 1; active > 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("creative 任务没有结束")
		}
		if err := f.a.DB.QueryRow("SELECT count(*) FROM jobs WHERE target_type='creative_set' AND target_id=? AND status IN ('queued','running')", setID).Scan(&active); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.a.DB.Exec("UPDATE jobs SET status='running' WHERE id=?", jobID); err != nil {
		t.Fatal(err)
	}
	again := f.runRender(t, listingID)
	if again.Status != "failed" || !strings.Contains(again.Message, "已有一套 AI 出图在生成") {
		t.Fatal(again.Status, again.Message)
	}
	var sets int
	if err := f.a.DB.QueryRow("SELECT count(*) FROM creative_sets WHERE listing_id=?", listingID).Scan(&sets); err != nil || sets != 1 {
		t.Fatal(sets, err)
	}
	if _, err := f.a.DB.Exec("UPDATE jobs SET status='failed' WHERE id=?", jobID); err != nil {
		t.Fatal(err)
	}
}

func TestAIRenderFailsWhenEverySlotIsMissing(t *testing.T) {
	f := newAIFixture(t)
	f.login(t, "admin")
	listingID := f.listingOf(t, f.product(t))
	if _, err := f.a.DB.Exec(`UPDATE creative_templates SET product_mode='redraw', photo_codes='["NOPE"]' WHERE product_mode='none'`); err != nil {
		t.Fatal(err)
	}
	job := f.runRender(t, listingID)
	if job.Status != "failed" || !strings.Contains(job.Message, "缺少实拍参考图（NOPE）") {
		t.Fatal(job.Status, job.Message)
	}
	var status, pipelineError string
	var sets int
	if err := f.a.DB.QueryRow("SELECT pipeline_status, pipeline_error FROM listings WHERE id=?", listingID).Scan(&status, &pipelineError); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !strings.Contains(pipelineError, "缺少实拍参考图（NOPE）") {
		t.Fatal(status, pipelineError)
	}
	if err := f.a.DB.QueryRow("SELECT count(*) FROM creative_sets WHERE listing_id=?", listingID).Scan(&sets); err != nil || sets != 0 {
		t.Fatal(sets, err)
	}
}

func TestAIRenderRejectsUserWithoutCreativeRun(t *testing.T) {
	f := newAIFixture(t)
	f.login(t, "admin")
	listingID := f.listingOf(t, f.product(t))
	var role int64
	if err := f.a.DB.QueryRow("SELECT id FROM roles WHERE key='photographer'").Scan(&role); err != nil {
		t.Fatal(err)
	}
	uid := f.newUser(t, "shooter", role, 1)
	res, err := f.a.DB.Exec("INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,created_at,steps) VALUES('pipeline','listing',?,?,'{}','queued',?,'render')", listingID, uid, now())
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := res.LastInsertId()
	f.a.Wake()
	job := waitJob(t, f.a, jobID)
	if job.Status != "failed" || !strings.Contains(job.Message, "没有 AI 出图权限") {
		t.Fatal(job.Status, job.Message)
	}
	var status, pipelineError string
	if err := f.a.DB.QueryRow("SELECT pipeline_status, pipeline_error FROM listings WHERE id=?", listingID).Scan(&status, &pipelineError); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !strings.Contains(pipelineError, "没有 AI 出图权限") {
		t.Fatal(status, pipelineError)
	}
}
