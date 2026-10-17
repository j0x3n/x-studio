package pipeline

import (
	"bytes"
	"context"
	"encoding/json"

	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"xstudio/internal/core"
)

func (f *fixture) newUser(t *testing.T, name string, role int64, shops ...int64) int64 {
	t.Helper()
	o := f.req(t, "POST", "/api/users", Object{"username": name, "password": "password123", "role_id": role, "shop_ids": shops}, 201)
	return int64(o["user"].(Object)["id"].(float64))
}
func (f *fixture) freshClient()       { jar, _ := cookiejar.New(nil); f.client = &http.Client{Jar: jar} }
func idOf(o Object, key string) int64 { return int64(o[key].(Object)["id"].(float64)) }
func TestAuthenticationPermissionsCSRF(t *testing.T) {
	f := newFixture(t, nil)
	f.req(t, "GET", "/api/me", nil, 401)
	req, _ := http.NewRequest("POST", f.s.URL+"/api/login", strings.NewReader(`{"username":"admin","password":"password123"}`))
	res, e := f.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("登录缺少 CSRF 头却被放行")
	}
	req.Header.Set("X-Requested-With", "fetch")
	req.Body = io.NopCloser(strings.NewReader(`{"username":"admin","password":"password123"}`))
	res, e = f.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if !strings.Contains(res.Header.Get("Set-Cookie"), "HttpOnly") || !strings.Contains(res.Header.Get("Set-Cookie"), "SameSite=Lax") {
		t.Fatal("会话属性不安全")
	}
	f.login(t, "admin")
	pid := f.product(t)
	lid := pid
	adminClient := f.client
	for _, tc := range []struct {
		name      string
		role      int64
		edit, run bool
	}{{"admin2", 1, true, true}, {"operator", 2, true, true}, {"designer", 3, false, true}, {"photographer", 4, false, false}, {"collaborator", 5, false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f.client = adminClient
			shops := []int64{1}
			if tc.role == 5 {
				shops = nil
			}
			uid := f.newUser(t, tc.name, tc.role, shops...)
			if tc.role == 5 {
				f.req(t, "POST", fmt.Sprintf("/api/products/%d/shares", pid), Object{"user_id": uid, "can_upload": true}, 201)
			}
			f.freshClient()
			f.login(t, tc.name)
			f.req(t, "GET", fmt.Sprintf("/api/products/%d", pid), nil, 200)
			f.req(t, "GET", "/api/shops", nil, 200)
			f.req(t, "GET", "/api/settings", nil, 200)
			up := f.upload(t, pid, "test.png", photoBytes(t, 12, 12), 201)
			aid := int64(up["photos"].([]any)[0].(Object)["id"].(float64))
			f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), Object{"tags": []string{"权限测试"}}, 200)
			overrideStatus := 403
			if tc.role == 1 {
				overrideStatus = 200
			}
			f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), Object{"source_override": "ai_edited"}, overrideStatus)
			deleteStatus := 403
			if tc.role <= 3 {
				deleteStatus = 200
			}
			f.req(t, "DELETE", fmt.Sprintf("/api/assets/%d", aid), nil, deleteStatus)
			want := 403
			if tc.edit {
				want = 200
			}
			f.req(t, "PATCH", fmt.Sprintf("/api/products/%d", pid), Object{"name": "测试权限"}, want)
			want = 403
			if tc.run {
				want = 400
			}
			f.req(t, "POST", fmt.Sprintf("/api/listings/%d/run?step=invalid", lid), nil, want)
			want = 403
			if tc.role == 1 {
				want = 200
			}
			for _, url := range []string{"/api/users", "/api/roles", "/api/audit", "/api/usage"} {
				f.req(t, "GET", url, nil, want)
			}
			if tc.role != 1 {
				f.req(t, "POST", "/api/shops", Object{}, 403)
				f.req(t, "PATCH", "/api/settings", Object{}, 403)
				f.req(t, "PUT", fmt.Sprintf("/api/users/%d/quota", uid), Object{}, 403)
			}
			want = 403
			if tc.role == 1 {
				want = 404
			}
			f.req(t, "GET", fmt.Sprintf("/api/listings/%d/download?ai_label=0", lid), nil, want)
			want = 403
			if tc.role <= 3 {
				want = 404
			}
			f.req(t, "GET", fmt.Sprintf("/api/listings/%d/download", lid), nil, want)
			want = 403
			if tc.role <= 2 {
				want = 400
			}
			f.req(t, "POST", fmt.Sprintf("/api/products/%d/shares", pid), Object{}, want)
			want = 200
			if tc.role == 5 {
				want = 404
			}
			listing := f.req(t, "GET", fmt.Sprintf("/api/listings/%d", lid), nil, want)
			if tc.role == 4 {
				if _, exists := listing["listing"].(Object)["content"]; exists {
					t.Fatal("没有 copy.view 却看到了文案")
				}
			}
			f.req(t, "GET", "/api/usage/me", nil, 200)
			f.req(t, "POST", "/api/logout", nil, 200)
			f.req(t, "GET", "/api/me", nil, 401)
		})
	}
}

func TestShopShareCostOwnershipAndFileJobScopes(t *testing.T) {
	f := newFixture(t, nil)
	f.login(t, "admin")
	adminClient := f.client
	s2 := idOf(f.req(t, "POST", "/api/shops", Object{"name": "虾皮店", "platform": "shopee_tw", "language": "zh-TW"}, 201), "shop")
	op := f.newUser(t, "op", 2, 1)
	designer := f.newUser(t, "designer", 3, 1)
	collab := f.newUser(t, "guest", 5)
	p := f.req(t, "POST", "/api/products", Object{"name": "跨店商品", "shop_ids": []int64{1, s2}, "cost": Object{"unit": 12.5}, "supplier": Object{"name": "货源"}}, 201)["product"].(Object)
	pid := int64(p["id"].(float64))
	ls := p["listings"].([]any)
	l1 := int64(ls[0].(Object)["id"].(float64))
	l2 := int64(ls[1].(Object)["id"].(float64))
	unlisted := idOf(f.req(t, "POST", "/api/products", Object{"name": "别人的未上架商品"}, 201), "product")
	up := f.upload(t, pid, "original.png", photoBytes(t, 16, 16), 201)
	ph := up["photos"].([]any)[0].(Object)
	aid := int64(ph["id"].(float64))
	file := ph["url"].(string)
	jobResult, e := f.a.DB.Exec("INSERT INTO jobs(kind,target_type,target_id,user_id,status,steps,created_at) VALUES('pipeline','listing',?,?, 'done','check',?)", l2, op, now())
	if e != nil {
		t.Fatal(e)
	}
	jid, _ := jobResult.LastInsertId()
	f.freshClient()
	f.login(t, "op")
	f.req(t, "GET", fmt.Sprintf("/api/products/%d", pid), nil, 200)
	f.req(t, "GET", fmt.Sprintf("/api/listings/%d", l2), nil, 404)
	f.req(t, "PATCH", fmt.Sprintf("/api/listings/%d", l2), Object{"url": "https://example.invalid"}, 404)
	f.req(t, "GET", fmt.Sprintf("/api/products/%d", unlisted), nil, 404)
	f.req(t, "GET", fmt.Sprintf("/api/jobs/%d", jid), nil, 404)
	f.req(t, "GET", fmt.Sprintf("/files/listings/%d/导出/01.jpg", l2), nil, 404)
	f.req(t, "GET", fmt.Sprintf("/render/详情页模板/商品/%d/内容.js", l2), nil, 404)
	f.req(t, "DELETE", fmt.Sprintf("/api/products/%d", pid), nil, 403)
	f.req(t, "DELETE", fmt.Sprintf("/api/assets/%d", aid), nil, 403)
	listing := f.req(t, "GET", fmt.Sprintf("/api/products/%d", pid), nil, 200)["product"].(Object)["listings"].([]any)
	if len(listing) != 1 {
		t.Fatal("跨店版本泄漏")
	}
	f.freshClient()
	f.login(t, "designer")
	o := f.req(t, "GET", fmt.Sprintf("/api/products/%d", pid), nil, 200)["product"].(Object)
	if _, ok := o["cost"]; ok {
		t.Fatal("泄漏成本")
	}
	if _, ok := o["supplier"]; ok {
		t.Fatal("泄漏货源")
	}
	f.client = adminClient
	// Give a custom role edit permission without product.cost and exercise write filtering.
	rid := idOf(f.req(t, "POST", "/api/roles", Object{"key": "editor", "name": "编辑", "scope": "shops", "perms": []string{core.ProductView, core.ProductEdit}}, 201), "role")
	f.req(t, "PATCH", fmt.Sprintf("/api/users/%d", designer), Object{"role_id": rid}, 200)
	f.freshClient()
	f.login(t, "designer")
	f.req(t, "PATCH", fmt.Sprintf("/api/products/%d", pid), Object{"cost": nil}, 403)
	f.req(t, "POST", "/api/products", Object{"name": "无权成本", "supplier": nil}, 403)
	f.client = adminClient
	f.req(t, "POST", fmt.Sprintf("/api/products/%d/shares", pid), Object{"user_id": collab, "can_upload": false}, 201)
	f.freshClient()
	f.login(t, "guest")
	o = f.req(t, "GET", fmt.Sprintf("/api/products/%d", pid), nil, 200)["product"].(Object)
	if len(o["listings"].([]any)) != 0 {
		t.Fatal("协作者看到店铺版本")
	}
	if f.req(t, "GET", "/api/shops", nil, 200)["total"] != float64(0) {
		t.Fatal("协作者看到店铺")
	}
	f.req(t, "GET", fmt.Sprintf("/api/listings/%d", l1), nil, 404)
	f.req(t, "GET", fmt.Sprintf("/api/products/%d", unlisted), nil, 404)
	f.upload(t, pid, "denied.png", photoBytes(t, 12, 12), 403)
	f.req(t, "GET", fmt.Sprintf("/api/jobs/%d", jid), nil, 404)
	res, e := f.client.Get(f.s.URL + file)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("共享商品原图不可见")
	}
	f.client = adminClient
	f.req(t, "POST", fmt.Sprintf("/api/products/%d/shares", pid), Object{"user_id": collab, "can_upload": true}, 201)
	f.freshClient()
	f.login(t, "guest")
	f.upload(t, pid, "allowed.png", photoBytes(t, 12, 12), 201)
	f.client = adminClient
	items := f.req(t, "GET", fmt.Sprintf("/api/products/%d/shares", pid), nil, 200)["items"].([]any)
	sid := int64(items[0].(Object)["id"].(float64))
	f.req(t, "DELETE", fmt.Sprintf("/api/shares/%d", sid), nil, 200)
	f.freshClient()
	f.login(t, "guest")
	f.req(t, "GET", file, nil, 404)
}

func TestSessionsLastAdminRolesAndSettings(t *testing.T) {
	f := newFixture(t, nil)
	f.login(t, "admin")
	if _, e := f.a.DB.Exec("INSERT INTO audit_log(at,user_id,action,target_type,target_id,detail,ip) VALUES('2026-10-18T00:00:00.1Z',1,'test.range','user',1,'{}','本机')"); e != nil {
		t.Fatal(e)
	}
	if o := f.req(t, "GET", "/api/audit?action=test.range&from=2026-10-18T00:00:00Z&to=2026-10-18T00:00:01Z", nil, 200); o["total"] != float64(1) {
		t.Fatal("日志时间范围丢失小数秒记录", o)
	}
	adminClient := f.client
	uid := f.newUser(t, "staff", 2, 1)
	for _, patch := range []Object{{"password": "password123"}, {"role_id": 2}, {"shop_ids": []int64{1}}, {"disabled": true}} {
		f.freshClient()
		f.login(t, "staff")
		staffClient := f.client
		f.client = adminClient
		f.req(t, "PATCH", fmt.Sprintf("/api/users/%d", uid), patch, 200)
		f.client = staffClient
		f.req(t, "GET", "/api/me", nil, 401)
		f.client = adminClient
		f.req(t, "PATCH", fmt.Sprintf("/api/users/%d", uid), Object{"disabled": false}, 200)
	}
	f.req(t, "PATCH", "/api/users/1", Object{"disabled": true}, 409)
	f.req(t, "PATCH", "/api/users/1", Object{"role_id": 2}, 409)
	f.req(t, "PATCH", "/api/roles/1", Object{"name": "改名"}, 403)
	f.req(t, "DELETE", "/api/roles/2", nil, 403)
	f.req(t, "PATCH", "/api/settings", Object{"ai_prices": Object{"vision": .5}, "quota_default": Object{"month_images": 2, "month_text": 3}, "ai_label": Object{"text": "AI 合成", "position": "top-left", "size": 18, "color": "#00FF00", "opacity": .7}}, 200)
	f.freshClient()
	f.login(t, "staff")
	if _, ok := f.req(t, "GET", "/api/settings", nil, 200)["ai_prices"]; ok {
		t.Fatal("非管理员看到价格")
	}
	f.client = adminClient
	uid = f.newUser(t, "quota", 2, 1)
	var images, text int
	if e := f.a.DB.QueryRow("SELECT month_images,month_text FROM quotas WHERE user_id=?", uid).Scan(&images, &text); e != nil || images != 2 || text != 3 {
		t.Fatal("默认额度未应用", e)
	}
}

func TestQuotaBlocksModelConcurrentCallsAndChargesActor(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		aiReply(w, `{"ok":true}`)
	}))
	defer s.Close()
	f := newFixture(t, func(c *Config) { c.AIKey = "fake"; c.AIBaseURL = s.URL; c.TextModel = "text" })
	f.login(t, "admin")
	uid := f.newUser(t, "worker", 2, 1)
	f.req(t, "PUT", fmt.Sprintf("/api/users/%d/quota", uid), Object{"month_images": 0, "month_text": 1}, 200)
	ctx := core.WithAIActor(context.Background(), core.AIActor{UserID: uid, JobID: 99, Feature: "copy"})
	if _, e := f.a.EditImage(ctx, "test", []image.Image{image.NewNRGBA(image.Rect(0, 0, 8, 8))}); e == nil || !strings.Contains(e.Error(), "额度已用完") {
		t.Fatal("生图额度未拦截", e)
	}
	if calls.Load() != 0 {
		t.Fatal("额度不足仍请求模型")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var out Object
		if e := f.a.AIJSON(ctx, "text", "test", "test", &out); e != nil {
			t.Error(e)
		}
	}()
	<-started
	var out Object
	if e := f.a.AIJSON(ctx, "text", "test", "test", &out); e == nil || !strings.Contains(e.Error(), "额度已用完") {
		t.Error("并发额度未拦截", e)
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("不正确的调用次数", calls.Load())
	}
	var user, job, units int64
	if e := f.a.DB.QueryRow("SELECT user_id,job_id,units FROM ai_usage").Scan(&user, &job, &units); e != nil || user != uid || job != 99 || units != 1 {
		t.Fatal("计量未归属触发人", e, user, job, units)
	}
	if e := f.a.AIJSON(ctx, "text", "test", "test", &out); e == nil {
		t.Fatal("用尽额度后仍成功")
	}
	start, end, e := core.MonthRange("2026-10")
	if e != nil || start != "2026-09-30T16:00:00Z" || end != "2026-10-31T16:00:00Z" {
		t.Fatal(start, end, e)
	}
}

func TestAIUsageFailureReleaseAndJobAttribution(t *testing.T) {
	var calls atomic.Int32
	var failModel atomic.Bool
	failModel.Store(true)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if failModel.Load() {
			w.WriteHeader(503)
			return
		}
		var body Object
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body["model"] != "configured" {
			t.Error("未使用设置里的模型", body["model"])
		}
		aiReply(w, `[]`)
	}))
	defer s.Close()
	f := newFixture(t, func(c *Config) { c.AIKey = "fake"; c.AIBaseURL = s.URL; c.VisionModel = "environment" })
	f.login(t, "admin")
	uid := f.newUser(t, "trigger", 2, 1)
	pid := f.product(t)
	f.upload(t, pid, "real.png", photoBytes(t, 12, 12), 201)
	writePrompts(t, f.a)
	f.req(t, "PATCH", "/api/settings", Object{"ai_models": Object{"classify": "configured"}, "ai_prices": Object{"configured": .25}}, 200)
	f.req(t, "PUT", fmt.Sprintf("/api/users/%d/quota", uid), Object{"month_text": 1}, 200)
	ctx := core.WithAIActor(context.Background(), core.AIActor{UserID: uid, Feature: "classify"})
	for i := 0; i < 2; i++ {
		var out any
		if e := f.a.AIJSON(ctx, "environment", "test", "test", &out); e == nil {
			t.Fatal("失败模型却成功")
		}
	}
	var n int
	_ = f.a.DB.QueryRow("SELECT count(*) FROM ai_usage").Scan(&n)
	if n != 0 || calls.Load() != 4 {
		t.Fatal("失败调用被计费或未按约定重试", n, calls.Load())
	}
	failModel.Store(false)
	f.freshClient()
	f.login(t, "trigger")
	j := f.req(t, "POST", fmt.Sprintf("/api/listings/%d/run?step=classify", pid), nil, 202)["job"].(Object)
	jid := int64(j["id"].(float64))
	if done := waitJob(t, f.a, jid); done.Status != "done" {
		t.Fatal(done)
	}
	var gotUser, gotJob int64
	var price float64
	if e := f.a.DB.QueryRow("SELECT user_id,job_id,est_cost FROM ai_usage").Scan(&gotUser, &gotJob, &price); e != nil || gotUser != uid || gotJob != jid || price != .25 {
		t.Fatal("任务用量归属或价格不正确", e, gotUser, gotJob, price)
	}
	usage := f.req(t, "GET", "/api/usage/me", nil, 200)
	if usage["total"] != float64(1) {
		t.Fatal(usage)
	}
	f.req(t, "GET", "/api/usage", nil, 403)
}

func TestImplicitMetadataAndSPA(t *testing.T) {
	var pngBytes bytes.Buffer
	if e := png.Encode(&pngBytes, image.NewNRGBA(image.Rect(0, 0, 12, 12))); e != nil {
		t.Fatal(e)
	}
	b, e := core.ImplicitLabel(pngBytes.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(b, []byte("iTXt")) || !bytes.Contains(b, []byte("AI 生成或合成")) || !bytes.Contains(b, []byte("X Studio")) {
		t.Fatal("PNG 隐式标识缺失")
	}
	if _, e = png.Decode(bytes.NewReader(b)); e != nil {
		t.Fatal("标识损坏 PNG", e)
	}
	f := newFixture(t, nil)
	for _, p := range []string{"/", "/products/123", "/login"} {
		res, e := f.client.Get(f.s.URL + p)
		if e != nil {
			t.Fatal(e)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !(bytes.Contains(data, []byte("前端未构建")) || bytes.Contains(data, []byte(`id="root"`))) {
			t.Fatal("SPA 回退失败", p, res.StatusCode)
		}
	}
	f.login(t, "admin")
	pid := f.product(t)
	dir := f.a.ListingDir(pid)
	if e = os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "导出.zip"), []byte("PK测试"), 0600); e != nil {
		t.Fatal(e)
	}
	res, e := f.client.Get(fmt.Sprintf("%s/api/listings/%d/download?ai_label=0", f.s.URL, pid))
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("X-AI-Label") != "1" {
		t.Fatal("下载应返回最近出图参数")
	}
	var n int
	_ = f.a.DB.QueryRow("SELECT count(*) FROM audit_log WHERE action='export.no_ai_label'").Scan(&n)
	if n < 1 {
		t.Fatal("取消标识下载缺少日志")
	}

}
