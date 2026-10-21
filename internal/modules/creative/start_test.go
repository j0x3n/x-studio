package creative

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"xstudio/internal/core"
)

func TestStartSetPipelineSourcePermissionAndSkip(t *testing.T) {
	f := newFixture(t)
	f.mainTemplates(t, nil)
	f.request(t, "POST", fmt.Sprintf("/api/listings/%d/creative", f.listingID), core.Object{"parts": []string{"main"}}, 202)
	var httpDetail string
	if err := f.c.DB.QueryRow("SELECT detail FROM audit_log WHERE action='creative.create' ORDER BY id DESC LIMIT 1").Scan(&httpDetail); err != nil || strings.Contains(httpDetail, "source") {
		t.Fatal(httpDetail, err)
	}
	// HTTP 那套的任务还在排队：一键出图不开新的一套
	if _, err := StartSet(context.Background(), f.c, f.listingID, 1, []string{"main"}, Params{SkipMissing: true}); err == nil || !strings.Contains(err.Error(), "已有一套 AI 出图在生成") {
		t.Fatal(err)
	}
	idle := func() { f.exec(t, `UPDATE jobs SET status='done' WHERE status IN ('queued','running')`) }
	idle()
	result, err := StartSet(context.Background(), f.c, f.listingID, 1, []string{"main"}, Params{SkipMissing: true})
	if err != nil || result.SetID == 0 || result.JobID == 0 || len(result.Missing) != 0 {
		t.Fatal(result, err)
	}
	var detail string
	if err = f.c.DB.QueryRow("SELECT detail FROM audit_log WHERE action='creative.create' AND target_id=?", result.SetID).Scan(&detail); err != nil || !strings.Contains(detail, `"source":"pipeline"`) {
		t.Fatal(detail, err)
	}
	var hash []byte
	var role, uid int64
	if err = f.c.DB.QueryRow("SELECT password_hash FROM users WHERE id=1").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if err = f.c.DB.QueryRow("SELECT id FROM roles WHERE key='photographer'").Scan(&role); err != nil {
		t.Fatal(err)
	}
	res, err := f.c.DB.Exec("INSERT INTO users(username,password_hash,display_name,role_id,created_at) VALUES('no_creative',?,?,?,?)", hash, "摄影", role, core.Now())
	if err != nil {
		t.Fatal(err)
	}
	uid, _ = res.LastInsertId()
	idle()
	if _, err = StartSet(context.Background(), f.c, f.listingID, uid, []string{"main"}, Params{}); err == nil || err.Error() != "没有 AI 出图权限" {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE creative_templates SET product_mode='redraw', photo_codes='["A1","A2"]'`)
	if _, err = StartSet(context.Background(), f.c, f.listingID, 1, []string{"main"}, Params{SkipMissing: true}); err == nil || !strings.Contains(err.Error(), "一对平放正面（A1）、一对立起 45°（A2）") {
		t.Fatal(err)
	}
}
