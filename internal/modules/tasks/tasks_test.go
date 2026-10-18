package tasks

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/products"
)

type assetSchema struct{ *assets.Module }

func (assetSchema) Routes(*core.Router)              {}
func (assetSchema) Jobs() map[string]core.JobHandler { return nil }

type object = map[string]any

type fixture struct {
	c      *core.Core
	m      *Module
	s      *httptest.Server
	client *http.Client
	dir    string
}

var (
	hashOnce sync.Once
	hashPass []byte
)

func passwordHash() []byte {
	hashOnce.Do(func() {
		var err error
		hashPass, err = bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
		if err != nil {
			panic(err)
		}
	})
	return hashPass
}

func boot(t *testing.T, dir string, withTasks bool) *fixture {
	t.Helper()
	if dir == "" {
		dir = t.TempDir()
	}
	cfg := core.Config{DataDir: filepath.Join(dir, "data"), PromptDir: filepath.Join(dir, "prompts"), TemplateDir: filepath.Join(dir, "templates"), AdminUser: "admin", AdminPassword: "password123", SessionSecret: strings.Repeat("s", 32), Workers: 1}
	if err := os.MkdirAll(cfg.PromptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	c, err := core.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := New(c)
	m.ScanInterval = time.Hour
	m.RemindInterval = time.Hour
	mods := []core.Module{accounts.New(c), products.New(c), assetSchema{assets.New(c)}}
	if withTasks {
		mods = append(mods, m)
	}
	if err = c.Start(mods...); err != nil {
		c.Close()
		t.Fatal(err)
	}
	s := httptest.NewServer(c.Handler())
	jar, _ := cookiejar.New(nil)
	f := &fixture{c: c, m: m, s: s, client: &http.Client{Jar: jar}, dir: dir}
	t.Cleanup(f.close)
	return f
}

func (f *fixture) close() {
	if f.s != nil {
		f.s.Close()
		f.s = nil
	}
	if f.c != nil {
		_ = f.c.Close()
		f.c = nil
	}
}

func (f *fixture) req(t *testing.T, method, path string, body any, status int) object {
	t.Helper()
	b := f.raw(t, method, path, body, status)
	if len(b) == 0 {
		return object{}
	}
	var out object
	if json.Unmarshal(b, &out) != nil {
		t.Fatalf("不是对象：%s", b)
	}
	return out
}

func (f *fixture) raw(t *testing.T, method, path string, body any, status int) []byte {
	t.Helper()
	var input io.Reader
	if body != nil {
		input = strings.NewReader(core.JS(body))
	}
	req, err := http.NewRequest(method, f.s.URL+path, input)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", "application/json")
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != status {
		t.Fatalf("%s %s：得到 %d，想要 %d：%s", method, path, res.StatusCode, status, b)
	}
	return b
}

func (f *fixture) login(t *testing.T, name string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	f.client.Jar = jar
	f.req(t, "POST", "/api/login", object{"username": name, "password": "password123"}, 200)
}

func (f *fixture) exec(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	res, err := f.c.DB.Exec(q, args...)
	if err != nil {
		t.Fatalf("%s：%v", q, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (f *fixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := f.c.DB.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) scalar(t *testing.T, q string, args ...any) string {
	t.Helper()
	var s string
	if err := f.c.DB.QueryRow(q, args...).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *fixture) ione(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := f.c.DB.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s：%v", q, err)
	}
	return n
}

func (f *fixture) user(t *testing.T, username, display, role string, shops ...int64) int64 {
	t.Helper()
	id := f.exec(t, "INSERT INTO users(username,password_hash,display_name,role_id,created_at) SELECT ?,?,?,id,? FROM roles WHERE key=?", username, passwordHash(), display, core.Now(), role)
	for _, shop := range shops {
		f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(?,?)", id, shop)
	}
	return id
}

func (f *fixture) product(t *testing.T, name string, owner, shop int64) (int64, int64) {
	t.Helper()
	now := core.Now()
	pid := f.exec(t, "INSERT INTO products(name,created_by,created_at,updated_at) VALUES(?,?,?,?)", name, owner, now, now)
	var lid int64
	if shop > 0 {
		lid = f.exec(t, "INSERT INTO listings(product_id,shop_id,created_by,created_at,updated_at) VALUES(?,?,?,?,?)", pid, shop, owner, now, now)
	}
	return pid, lid
}

func (f *fixture) shop(t *testing.T, name string) int64 {
	t.Helper()
	return f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES(?,'taobao','zh-CN',?)", name, core.Now())
}

func (f *fixture) audit(t *testing.T, action, typ string, id int64, detail string) int64 {
	t.Helper()
	if detail == "" {
		detail = "{}"
	}
	return f.exec(t, "INSERT INTO audit_log(at,user_id,action,target_type,target_id,detail,ip) VALUES(?,1,?,?,?,?,?)", core.Now(), action, typ, id, detail, "127.0.0.1")
}

func (f *fixture) job(t *testing.T, kind, typ string, target, user int64, status, message, created, finished string) int64 {
	t.Helper()
	return f.exec(t, "INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,progress,message,steps,result,created_at,finished_at) VALUES(?,?,?,?, '{}', ?,100,?, '[]', '{}', ?,?)", kind, typ, target, user, status, message, created, finished)
}

func (f *fixture) enable(t *testing.T, event string) {
	t.Helper()
	f.exec(t, "UPDATE task_rules SET enabled=1 WHERE event=?", event)
}

func (f *fixture) scan(t *testing.T) {
	t.Helper()
	if err := f.m.scanAudit(); err != nil {
		t.Fatal(err)
	}
	if err := f.m.scanJobs(); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) remind(t *testing.T) {
	t.Helper()
	if err := f.m.remindDue(); err != nil {
		t.Fatal(err)
	}
	if err := f.m.remindQuota(); err != nil {
		t.Fatal(err)
	}
}

func num(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}
func str(v any) string {
	s, _ := v.(string)
	return s
}
func obj(v any) object {
	o, _ := v.(map[string]any)
	return o
}
func arr(v any) []any {
	a, _ := v.([]any)
	return a
}
func assigneeOf(v object) int64 {
	if v["assignee"] == nil {
		return 0
	}
	return num(obj(v["assignee"])["id"])
}

type sseClient struct {
	body io.ReadCloser
	r    *bufio.Reader
}

func (f *fixture) openStream(t *testing.T) *sseClient {
	t.Helper()
	req, err := http.NewRequest("GET", f.s.URL+"/api/notifications/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Requested-With", "fetch")
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("SSE %d：%s", res.StatusCode, b)
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "text/event-stream") {
		res.Body.Close()
		t.Fatalf("Content-Type：%s", res.Header.Get("Content-Type"))
	}
	return &sseClient{body: res.Body, r: bufio.NewReader(res.Body)}
}

func (s *sseClient) next(timeout time.Duration) (string, string, error) {
	type got struct {
		ev, data string
		err      error
	}
	ch := make(chan got, 1)
	go func() {
		ev, data := "", ""
		for {
			line, err := s.r.ReadString('\n')
			if err != nil {
				ch <- got{err: err}
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, ":") {
				ch <- got{ev: "comment", data: strings.TrimSpace(strings.TrimPrefix(line, ":"))}
				return
			}
			if strings.HasPrefix(line, "event:") {
				ev = strings.TrimSpace(line[len("event:"):])
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data = strings.TrimSpace(line[len("data:"):])
				continue
			}
			if line == "" && (ev != "" || data != "") {
				ch <- got{ev: ev, data: data}
				return
			}
		}
	}()
	select {
	case v := <-ch:
		return v.ev, v.data, v.err
	case <-time.After(timeout):
		return "", "", os.ErrDeadlineExceeded
	}
}

func idsOf(items []any) []int64 {
	out := make([]int64, 0, len(items))
	for _, it := range items {
		out = append(out, num(obj(it)["id"]))
	}
	return out
}

func containsNum(list []int64, id int64) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

func TestPermissionsAndVisibility(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	shop2 := f.shop(t, "隐藏店")
	op := f.user(t, "op", "运营小周", "operator", 1)
	f.user(t, "photo", "摄影小林", "photographer", 1)
	outsider := f.user(t, "outsider", "外店美工", "designer", shop2)
	collab := f.user(t, "collab", "协作者", "collaborator")
	visible, _ := f.product(t, "店内款", 1, 1)
	hidden, _ := f.product(t, "外店款", outsider, shop2)
	f.exec(t, "INSERT INTO shares(product_id,user_id,can_upload,created_by,created_at) VALUES(?,?,0,1,?)", hidden, collab, core.Now())
	secret := num(obj(f.req(t, "POST", "/api/tasks", object{"title": "外店任务", "assignee_id": outsider, "target_type": "product", "target_id": hidden}, 201)["task"])["id"])

	f.login(t, "collab")
	f.req(t, "GET", "/api/tasks", nil, 403)
	f.req(t, "GET", "/api/tasks/"+itoa(secret), nil, 403)
	f.req(t, "POST", "/api/tasks", object{"title": "不行"}, 403)

	f.login(t, "photo")
	f.req(t, "POST", "/api/tasks", object{"title": "派给别人", "assignee_id": op, "target_type": "product", "target_id": visible}, 403)
	own := f.req(t, "GET", "/api/tasks?view=all", nil, 200)
	if num(own["total"]) != 0 {
		t.Fatalf("摄影不该看到外店任务：%s", core.JS(own))
	}
	f.req(t, "GET", "/api/tasks/"+itoa(secret), nil, 404)
	f.req(t, "GET", "/api/tasks/load", nil, 403)

	f.login(t, "op")
	seen := f.req(t, "GET", "/api/tasks?view=all", nil, 200)
	if num(seen["total"]) != 0 {
		t.Fatalf("运营看不到外店商品，不该看到任务：%s", core.JS(seen))
	}
	f.req(t, "GET", "/api/tasks/"+itoa(secret), nil, 404)
	load := arr(f.req(t, "GET", "/api/tasks/load", nil, 200)["items"])
	for _, it := range load {
		if num(obj(it)["id"]) == outsider && num(obj(it)["open"]) != 0 {
			t.Fatalf("负载不该算上看不到的任务：%s", core.JS(it))
		}
	}

	f.login(t, "outsider")
	mine := f.req(t, "GET", "/api/tasks?view=mine", nil, 200)
	if num(mine["total"]) != 1 || str(obj(arr(mine["items"])[0])["title"]) != "外店任务" {
		t.Fatalf("负责人要看得到自己的任务：%s", core.JS(mine))
	}

	f.login(t, "admin")
	all := f.req(t, "GET", "/api/tasks?view=all", nil, 200)
	if num(all["total"]) != 1 {
		t.Fatalf("管理员要看得到：%s", core.JS(all))
	}
	adminLoad := arr(f.req(t, "GET", "/api/tasks/load", nil, 200)["items"])
	found := false
	for _, it := range adminLoad {
		if num(obj(it)["id"]) == outsider && num(obj(it)["open"]) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("管理员负载要算上这条任务：%s", core.JS(adminLoad))
	}
}

func TestFieldRulesAndReopen(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	f.user(t, "op", "运营小周", "operator", 1)
	photo := f.user(t, "photo", "摄影小林", "photographer", 1)
	design := f.user(t, "design", "美工阿周", "designer", 1)
	pid, _ := f.product(t, "珠蝶", 1, 1)

	f.login(t, "op")
	task := obj(f.req(t, "POST", "/api/tasks", object{"title": "原标题", "assignee_id": photo, "target_type": "product", "target_id": pid}, 201)["task"])
	id := num(task["id"])
	path := "/api/tasks/" + itoa(id)

	f.login(t, "photo")
	got := obj(f.req(t, "PATCH", path, object{"status": "doing"}, 200)["task"])
	if str(got["status"]) != "doing" {
		t.Fatalf("负责人要能改状态：%s", core.JS(got))
	}
	got = obj(f.req(t, "PATCH", path, object{"checklist": []object{{"text": "调色", "done": true}}}, 200)["task"])
	if num(got["checklist_done"]) != 1 {
		t.Fatalf("负责人要能改清单：%s", core.JS(got))
	}
	f.req(t, "PATCH", path, object{"title": "改不了"}, 403)
	f.req(t, "PATCH", path, object{"assignee_id": design}, 403)

	f.login(t, "op")
	got = obj(f.req(t, "PATCH", path, object{"title": "改过了", "assignee_id": design}, 200)["task"])
	if str(got["title"]) != "改过了" || assigneeOf(got) != design {
		t.Fatalf("创建人要能改标题和负责人：%s", core.JS(got))
	}
	f.login(t, "design")
	f.req(t, "PATCH", path, object{"assignee_id": photo}, 403)

	f.login(t, "photo")
	own := obj(f.req(t, "POST", "/api/tasks", object{"title": "自己的", "target_type": "product", "target_id": pid}, 201)["task"])
	if assigneeOf(own) != photo {
		t.Fatalf("没指定负责人时应派给自己：%s", core.JS(own))
	}
	ownPath := "/api/tasks/" + itoa(num(own["id"]))
	f.req(t, "PATCH", ownPath, object{"title": "创建人改的", "assignee_id": design}, 403)
	got = obj(f.req(t, "PATCH", ownPath, object{"title": "创建人改的", "assignee_id": photo}, 200)["task"])
	if str(got["title"]) != "创建人改的" || assigneeOf(got) != photo {
		t.Fatalf("没有管理权限的创建人只能派给自己：%s", core.JS(got))
	}

	f.login(t, "op")
	back := obj(f.req(t, "PATCH", path, object{"assignee_id": photo, "status": "done"}, 200)["task"])
	if num(back["reopen_count"]) != 0 || str(back["done_at"]) == "" {
		t.Fatalf("完成时返工次数不变：%s", core.JS(back))
	}
	back = obj(f.req(t, "PATCH", path, object{"status": "todo"}, 200)["task"])
	if num(back["reopen_count"]) != 1 || str(back["done_at"]) != "" {
		t.Fatalf("从完成改回要记一次返工：%s", core.JS(back))
	}
	f.req(t, "PATCH", path, object{"status": "done"}, 200)
	back = obj(f.req(t, "PATCH", path, object{"status": "canceled"}, 200)["task"])
	if num(back["reopen_count"]) != 2 {
		t.Fatalf("从完成改成取消也算返工：%s", core.JS(back))
	}
}

func TestMentions(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	op := f.user(t, "op", "运营小周", "operator", 1)
	photo := f.user(t, "photo", "摄影小林", "photographer", 1)
	design := f.user(t, "design", "美工阿周", "designer", 1)
	outsider := f.user(t, "outsider", "外店美工", "designer", f.shop(t, "隐藏店"))
	pid, _ := f.product(t, "珠蝶", 1, 1)
	f.login(t, "op")
	task := obj(f.req(t, "POST", "/api/tasks", object{"title": "修图", "assignee_id": photo, "target_type": "product", "target_id": pid, "watcher_ids": []int64{design}}, 201)["task"])
	id := num(task["id"])
	updated := f.scalar(t, "SELECT updated_at FROM tasks WHERE id=?", id)
	f.login(t, "photo")
	res := f.req(t, "POST", "/api/tasks/"+itoa(id)+"/comments", object{"body": "请 @运营小周 看一下 @摄影小林 @外店美工 @没这个人"}, 201)
	ignored := arr(res["ignored_mentions"])
	if len(ignored) != 2 || str(ignored[0]) != "外店美工" || str(ignored[1]) != "没这个人" {
		t.Fatalf("看不到的人和不存在的人要列出来：%s", core.JS(ignored))
	}
	mentioned := arr(obj(res["comment"])["mentions"])
	if len(mentioned) != 1 || num(mentioned[0]) != op {
		t.Fatalf("只该提到看得到的人：%s", core.JS(mentioned))
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='task_mentioned'", op) != 1 {
		t.Fatal("看得到的人要收到提及")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='task_comment'", design) != 1 {
		t.Fatal("协作人要收到评论通知")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='task_comment'", op) != 0 {
		t.Fatal("已经提及的人不要再发评论通知")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type IN ('task_mentioned','task_comment')", photo) != 0 {
		t.Fatal("评论人自己不要收到通知")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=?", outsider) != 0 {
		t.Fatal("看不到任务的人不要收到通知")
	}
	if f.scalar(t, "SELECT updated_at FROM tasks WHERE id=?", id) != updated {
		t.Fatal("评论不改任务的更新时间")
	}
}

func TestAutoEventsAndDedup(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	op := f.user(t, "op", "运营小周", "operator", 1)
	photo := f.user(t, "photo", "摄影小林", "photographer", 1)
	shop2 := f.shop(t, "隐藏店")
	pid, lid := f.product(t, "珠蝶", 1, 1)
	other, otherList := f.product(t, "珠蝶二", 1, 1)
	hidden, _ := f.product(t, "外店款", 1, shop2)
	f.enable(t, "product.created")
	f.enable(t, "assets.first_upload")
	f.enable(t, "render.failed")
	f.enable(t, "review.rejected")
	f.user(t, "design", "美工阿周", "designer", 1)

	f.audit(t, "product.create", "product", pid, "{}")
	f.audit(t, "product.create", "product", pid, "{}")
	f.audit(t, "assets.upload", "product", pid, "{}")
	f.audit(t, "assets.upload", "product", pid, "{}")
	f.audit(t, "product.create", "product", 999999, "{}")
	f.audit(t, "product.create", "product", hidden, "{}")
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	jobID := f.job(t, "pipeline", "listing", lid, photo, "failed", "模板缺失", start.Format(time.RFC3339Nano), start.Add(time.Minute).Format(time.RFC3339Nano))
	f.audit(t, "studio.review", "listing", lid, core.JS(object{"status": "rejected", "note": "颜色不对", "rendered_job_id": jobID}))
	f.audit(t, "studio.review", "listing", lid, core.JS(object{"status": "approved", "note": "可以", "rendered_job_id": jobID}))
	f.audit(t, "studio.review", "listing", otherList, `{"status":"rejected","note":"没有任务编号"}`)
	_ = other
	f.scan(t)

	if f.count(t, "SELECT count(*) FROM tasks WHERE title=?", "拍样：珠蝶") != 1 {
		t.Fatal("同一商品的拍样任务不能重复建")
	}
	if f.ione(t, "SELECT ifnull(assignee_id,0) FROM tasks WHERE title=?", "拍样：珠蝶") != photo {
		t.Fatal("拍样要派给摄影")
	}
	if f.count(t, "SELECT count(*) FROM task_events WHERE kind='repeat' AND task_id=(SELECT id FROM tasks WHERE title=?)", "拍样：珠蝶") != 1 {
		t.Fatal("重复事件要记在原任务上")
	}
	if f.count(t, "SELECT count(*) FROM tasks WHERE title=?", "修图：珠蝶") != 1 {
		t.Fatal("第一次上传要生成修图任务")
	}
	if f.count(t, "SELECT count(*) FROM task_events WHERE kind='repeat' AND task_id=(SELECT id FROM tasks WHERE title=?)", "修图：珠蝶") != 0 {
		t.Fatal("第二次上传不是第一次，不应记重复事件")
	}
	failTitle := "处理出图失败：珠蝶 · 淘宝店"
	if f.scalar(t, "SELECT priority||':'||body||':'||ifnull(assignee_id,0) FROM tasks WHERE title=?", failTitle) != "high:模板缺失:1" {
		t.Fatalf("出图失败要派给商品创建人：%s", f.scalar(t, "SELECT priority||':'||body||':'||ifnull(assignee_id,0) FROM tasks WHERE title=?", failTitle))
	}
	rejectTitle := "审核被退回：珠蝶 · 淘宝店"
	if f.scalar(t, "SELECT body||':'||ifnull(assignee_id,0) FROM tasks WHERE title=?", rejectTitle) != "颜色不对:"+itoa(photo) {
		t.Fatal("退回要派给出图任务的发起人，并写上原因")
	}
	if f.count(t, "SELECT count(*) FROM tasks WHERE title LIKE '审核被退回%'") != 2 {
		t.Fatal("没有出图任务编号时也要建任务，只是派不出去")
	}
	if f.ione(t, "SELECT ifnull(assignee_id,0) FROM tasks WHERE title=?", "审核被退回：珠蝶二 · 淘宝店") != 0 {
		t.Fatal("没有可派的人时负责人为空")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE type='review_result' AND user_id=?", photo) != 2 {
		t.Fatal("通过和退回都要通知出图发起人")
	}
	if f.count(t, "SELECT count(*) FROM tasks WHERE title=?", "拍样：已删除") != 1 {
		t.Fatal("对象已删除也要建任务")
	}
	if f.ione(t, "SELECT ifnull(assignee_id,0) FROM tasks WHERE title=?", "拍样：已删除") != 0 {
		t.Fatal("对象已删除时负责人为空")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE type='task_unassigned' AND title=?", "没派出去：拍样：已删除") != 1 {
		t.Fatal("已删除对象的系统任务只能通知管理员")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE type='task_unassigned' AND user_id=? AND title=?", op, "没派出去：拍样：外店款") != 0 {
		t.Fatal("看不到对象的管理员不要收到没派出去的通知")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE type='task_unassigned' AND user_id=1 AND title=?", "没派出去：拍样：外店款") != 1 {
		t.Fatal("看得到对象的管理员要收到没派出去的通知")
	}
	f.scan(t)
	if f.count(t, "SELECT count(*) FROM tasks") != 7 {
		t.Fatalf("再扫一轮不能加任务，现在 %d", f.count(t, "SELECT count(*) FROM tasks"))
	}
}

func TestAssignStrategies(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	p1 := f.user(t, "p1", "摄影甲", "photographer", 1)
	p2 := f.user(t, "p2", "摄影乙", "photographer", 1)
	d1 := f.user(t, "d1", "美工甲", "designer", 1)
	d2 := f.user(t, "d2", "美工乙", "designer", 1)
	if p1 > p2 || d1 > d2 {
		t.Fatal("测试依赖先插入的人编号更小")
	}
	var products []int64
	for _, name := range []string{"甲", "乙", "丙", "丁"} {
		pid, _ := f.product(t, name, 1, 1)
		products = append(products, pid)
	}
	f.enable(t, "product.created")
	for _, pid := range products {
		f.audit(t, "product.create", "product", pid, "{}")
	}
	f.scan(t)
	rows, err := f.c.DB.Query("SELECT assignee_id FROM tasks WHERE title LIKE '拍样：%' ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) != nil {
			rows.Close()
			t.Fatal("读取负责人失败")
		}
		got = append(got, id)
	}
	rows.Close()
	want := []int64{p1, p2, p1, p2}
	if len(got) != 4 {
		t.Fatalf("轮流结果：%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("轮流想要 %v，得到 %v", want, got)
		}
	}

	f.exec(t, "UPDATE task_rules SET enabled=0 WHERE event='product.created'")
	f.enable(t, "assets.first_upload")
	upload := func(name string) {
		t.Helper()
		pid, _ := f.product(t, name, 1, 1)
		f.audit(t, "assets.upload", "product", pid, "{}")
		f.scan(t)
	}
	upload("闲时")
	if f.ione(t, "SELECT assignee_id FROM tasks WHERE title=?", "修图：闲时") != d1 {
		t.Fatal("同样空闲时派给编号更小的人")
	}
	for i := 0; i < 2; i++ {
		f.exec(t, "INSERT INTO tasks(title,body,status,priority,assignee_id,created_by,checklist,created_at,updated_at) VALUES(?,'','todo','normal',?,0,'[]',?,?)", "占手"+itoa(int64(i)), d1, core.Now(), core.Now())
	}
	upload("更闲")
	if f.ione(t, "SELECT assignee_id FROM tasks WHERE title=?", "修图：更闲") != d2 {
		t.Fatal("要派给未完成任务更少的人")
	}
	for i := 0; i < 3; i++ {
		f.exec(t, "INSERT INTO tasks(title,body,status,priority,assignee_id,created_by,checklist,created_at,updated_at) VALUES(?,'','todo','normal',?,0,'[]',?,?)", "再占"+itoa(int64(i)), d2, core.Now(), core.Now())
	}
	upload("回头")
	if f.ione(t, "SELECT assignee_id FROM tasks WHERE title=?", "修图：回头") != d1 {
		t.Fatal("另一人更忙时要派回来")
	}

	f.exec(t, "UPDATE users SET disabled=1 WHERE id=?", p2)
	f.exec(t, "UPDATE task_rules SET enabled=1 WHERE event='product.created' AND name='拍样'")
	f.exec(t, "UPDATE task_rules SET enabled=0 WHERE event='assets.first_upload'")
	pid, _ := f.product(t, "停用后", 1, 1)
	f.audit(t, "product.create", "product", pid, "{}")
	f.scan(t)
	if f.ione(t, "SELECT assignee_id FROM tasks WHERE title=?", "拍样：停用后") != p1 {
		t.Fatal("停用的人不能参与轮流")
	}

	role := f.exec(t, "INSERT INTO roles(key,name,builtin,scope,perms,created_at) VALUES('plain','无任务',0,'shops',?,?)", `["product.view"]`, core.Now())
	f.exec(t, "INSERT INTO users(username,password_hash,display_name,role_id,created_at) VALUES('plain1',?, '甲', ?, ?)", passwordHash(), role, core.Now())
	f.exec(t, "INSERT INTO users(username,password_hash,display_name,role_id,created_at) VALUES('plain2',?, '乙', ?, ?)", passwordHash(), role, core.Now())
	f.req(t, "POST", "/api/task-rules", object{"name": "无人", "enabled": true, "event": "product.created", "title_tpl": "无人：{product}", "assign_mode": "role_round_robin", "assign_role_id": role}, 201)
	f.exec(t, "UPDATE task_rules SET enabled=0 WHERE name='拍样'")
	pid, _ = f.product(t, "没权限", 1, 1)
	f.audit(t, "product.create", "product", pid, "{}")
	f.scan(t)
	if f.ione(t, "SELECT ifnull(assignee_id,0) FROM tasks WHERE title=?", "无人：没权限") != 0 {
		t.Fatal("没有任务权限的人不能接活")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE type='task_unassigned' AND title=?", "没派出去：无人：没权限") == 0 {
		t.Fatal("派不出去要通知任务管理员")
	}
}

func TestCursorAndRestart(t *testing.T) {
	dir := t.TempDir()
	first := boot(t, dir, false)
	first.login(t, "admin")
	photo := first.user(t, "photo", "摄影小林", "photographer", 1)
	oldID, oldList := first.product(t, "旧款", 1, 1)
	newID, _ := first.product(t, "新款", 1, 1)
	first.audit(t, "product.create", "product", oldID, "{}")
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	first.job(t, "pipeline", "listing", oldList, 1, "failed", "旧失败", start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	first.job(t, "export", "product", oldID, 1, "done", "旧完成", start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	queued := first.job(t, "export", "product", newID, 1, "queued", "", start.Format(time.RFC3339Nano), "")
	first.close()

	f := boot(t, dir, true)
	f.login(t, "admin")
	f.exec(t, "UPDATE task_rules SET enabled=1")
	f.scan(t)
	if f.count(t, "SELECT count(*) FROM tasks") != 0 || f.count(t, "SELECT count(*) FROM notifications") != 0 {
		t.Fatal("第一次启动不能处理历史记录")
	}
	finished := start.Add(45 * time.Second)
	f.exec(t, "UPDATE jobs SET status='done', message='新完成', finished_at=? WHERE id=?", finished.Format(time.RFC3339Nano), queued)
	f.audit(t, "product.create", "product", newID, "{}")
	f.scan(t)
	if f.count(t, "SELECT count(*) FROM tasks WHERE title=?", "拍样：新款") != 1 {
		t.Fatal("新的商品创建要派活")
	}
	if f.count(t, "SELECT count(*) FROM tasks WHERE title=?", "拍样：旧款") != 0 {
		t.Fatal("历史商品创建不能补派")
	}
	if f.count(t, "SELECT count(*) FROM tasks WHERE title LIKE '处理出图失败%'") != 0 {
		t.Fatal("历史失败任务不能补派")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE type='job_done'") != 1 {
		t.Fatal("模块启动后才完成的后台任务要通知一次")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE type='job_failed'") != 0 {
		t.Fatal("历史失败通知不能补发")
	}
	if f.ione(t, "SELECT ifnull(assignee_id,0) FROM tasks WHERE title=?", "拍样：新款") != photo {
		t.Fatal("新任务要派给摄影")
	}
	tasks, notes := f.count(t, "SELECT count(*) FROM tasks"), f.count(t, "SELECT count(*) FROM notifications")
	f.scan(t)
	if f.count(t, "SELECT count(*) FROM tasks") != tasks || f.count(t, "SELECT count(*) FROM notifications") != notes {
		t.Fatal("同一轮之后不能重复处理")
	}
	f.close()

	again := boot(t, dir, true)
	again.scan(t)
	if again.count(t, "SELECT count(*) FROM tasks") != tasks || again.count(t, "SELECT count(*) FROM notifications") != notes {
		t.Fatal("重启后不能重复处理")
	}
}

func TestNotifications(t *testing.T) {
	f := boot(t, "", true)
	frozen := time.Date(2026, 10, 18, 4, 0, 0, 0, time.UTC)
	f.m.Now = func() time.Time { return frozen }
	f.login(t, "admin")
	op := f.user(t, "op", "运营小周", "operator", 1)
	photo := f.user(t, "photo", "摄影小林", "photographer", 1)
	design := f.user(t, "design", "美工阿周", "designer", 1)
	pid, lid := f.product(t, "珠蝶", op, 1)

	f.login(t, "op")
	self := obj(f.req(t, "POST", "/api/tasks", object{"title": "给自己", "target_type": "product", "target_id": pid}, 201)["task"])
	if f.count(t, "SELECT count(*) FROM notifications WHERE type='task_assigned'") != 0 {
		t.Fatal("派给自己不发通知")
	}
	task := obj(f.req(t, "POST", "/api/tasks", object{"title": "给摄影", "assignee_id": photo, "target_type": "product", "target_id": pid, "watcher_ids": []int64{design}}, 201)["task"])
	id := num(task["id"])
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='task_assigned'", photo) != 1 {
		t.Fatal("派给别人要发 task_assigned")
	}
	f.login(t, "photo")
	f.req(t, "POST", "/api/tasks/"+itoa(id)+"/comments", object{"body": "@运营小周 看一下"}, 201)
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='task_mentioned'", op) != 1 {
		t.Fatal("要有提及通知")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='task_comment'", design) != 1 {
		t.Fatal("要有评论通知")
	}
	f.req(t, "PATCH", "/api/tasks/"+itoa(id), object{"status": "done"}, 200)
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='task_done'", op) != 1 {
		t.Fatal("别人完成我建的任务要通知我")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='task_done'", photo) != 0 {
		t.Fatal("自己完成自己的任务不发 task_done")
	}

	f.login(t, "photo")
	f.req(t, "PUT", "/api/notification-prefs", object{"types": object{"task_assigned": object{"inbox": false, "desktop": true}}, "quiet": object{"from": "22:00", "to": "08:00"}}, 200)
	night := time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC)
	f.m.Now = func() time.Time { return night }
	f.login(t, "op")
	ch, cancel := f.m.hub.subscribe(photo)
	f.req(t, "POST", "/api/tasks", object{"title": "夜里", "assignee_id": photo, "target_type": "product", "target_id": pid}, 201)
	select {
	case <-ch:
		t.Fatal("收件箱关掉时不应该推送")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	if f.count(t, "SELECT count(*) FROM notifications WHERE title LIKE '%夜里%'") != 0 {
		t.Fatal("inbox 关掉时不写通知表")
	}
	f.login(t, "photo")
	f.req(t, "PUT", "/api/notification-prefs", object{"types": object{"task_assigned": object{"inbox": true, "desktop": true}}, "quiet": object{"from": "22:00", "to": "08:00"}}, 200)
	f.login(t, "op")
	ch, cancel = f.m.hub.subscribe(photo)
	f.req(t, "POST", "/api/tasks", object{"title": "免打扰", "assignee_id": photo, "target_type": "product", "target_id": pid}, 201)
	frame := waitFrame(t, ch)
	cancel()
	ev, data := parseFrame(t, frame)
	if ev != "notification" || data["desktop"] != false {
		t.Fatalf("免打扰时段桌面通知要关掉：%s %s", ev, core.JS(data))
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE title=?", "新任务：免打扰") != 1 {
		t.Fatal("免打扰仍要写入收件箱")
	}
	f.m.Now = func() time.Time { return frozen }
	ch, cancel = f.m.hub.subscribe(photo)
	f.req(t, "POST", "/api/tasks", object{"title": "白天", "assignee_id": photo, "target_type": "product", "target_id": pid}, 201)
	frame = waitFrame(t, ch)
	cancel()
	_, data = parseFrame(t, frame)
	if data["desktop"] != true {
		t.Fatalf("免打扰时段外要弹桌面通知：%s", core.JS(data))
	}

	dueAt := frozen.Add(2 * time.Hour).Format(time.RFC3339Nano)
	dueTask := num(obj(f.req(t, "POST", "/api/tasks", object{"title": "快到期", "assignee_id": photo, "due_at": dueAt, "target_type": "product", "target_id": pid}, 201)["task"])["id"])
	f.remind(t)
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='task_due'", photo) != 1 {
		t.Fatal("24 小时内要提醒一次")
	}
	f.remind(t)
	if f.count(t, "SELECT count(*) FROM notifications WHERE type='task_due'") != 1 {
		t.Fatal("到期提醒不能重复")
	}
	later := frozen.Add(3 * time.Hour)
	f.m.Now = func() time.Time { return later }
	f.remind(t)
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='task_overdue' AND link=?", photo, "/tasks/"+itoa(dueTask)) != 1 {
		t.Fatal("过了截止时间要提醒逾期")
	}
	f.remind(t)
	if f.count(t, "SELECT count(*) FROM notifications WHERE type='task_overdue'") != 1 {
		t.Fatal("逾期提醒不能重复")
	}
	_ = self

	jobStart := frozen.Add(-time.Minute)
	f.job(t, "export", "product", pid, photo, "failed", "导出失败", jobStart.Format(time.RFC3339Nano), frozen.Format(time.RFC3339Nano))
	f.job(t, "export", "product", pid, photo, "done", "导出完成", jobStart.Format(time.RFC3339Nano), jobStart.Add(30*time.Second).Format(time.RFC3339Nano))
	f.job(t, "export", "product", pid, photo, "done", "太短", jobStart.Format(time.RFC3339Nano), jobStart.Add(10*time.Second).Format(time.RFC3339Nano))
	renderJob := f.job(t, "pipeline", "listing", lid, photo, "done", "出图完成", jobStart.Format(time.RFC3339Nano), jobStart.Add(40*time.Second).Format(time.RFC3339Nano))
	f.audit(t, "studio.review", "listing", lid, core.JS(object{"status": "approved", "note": "可以", "rendered_job_id": renderJob}))
	f.scan(t)
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='job_failed'", photo) != 1 {
		t.Fatal("失败的后台任务要通知发起人")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='job_done'", photo) != 2 {
		t.Fatal("满 30 秒的完成要通知，短任务不要")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='review_result'", photo) != 1 {
		t.Fatal("审核结果要通知出图发起人")
	}

	f.m.Now = func() time.Time { return frozen }
	f.exec(t, "INSERT INTO quotas(user_id,month_images,month_text) VALUES(?,10,10)", photo)
	for i := 0; i < 7; i++ {
		f.exec(t, "INSERT INTO ai_usage(at,user_id,feature,model,kind,units,est_cost) VALUES(?,?,'x','m','image',1,0)", frozen.Format(time.RFC3339Nano), photo)
	}
	f.remind(t)
	if f.count(t, "SELECT count(*) FROM notifications WHERE type='quota_80'") != 0 {
		t.Fatal("70% 不要提醒")
	}
	f.exec(t, "INSERT INTO ai_usage(at,user_id,feature,model,kind,units,est_cost) VALUES(?,?,'x','m','image',1,0)", frozen.Format(time.RFC3339Nano), photo)
	f.remind(t)
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='quota_80' AND title=?", photo, "本月生图额度已用到 80%") != 1 {
		t.Fatal("生图到 80% 要提醒一次")
	}
	f.remind(t)
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='quota_80' AND title LIKE '本月生图%'", photo) != 1 {
		t.Fatal("额度提醒每月每类只发一次")
	}
	for i := 0; i < 8; i++ {
		f.exec(t, "INSERT INTO ai_usage(at,user_id,feature,model,kind,units,est_cost) VALUES(?,?,'x','m','text',1,0)", frozen.Format(time.RFC3339Nano), photo)
	}
	f.remind(t)
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='quota_80' AND title=?", photo, "本月文本额度已用到 80%") != 1 {
		t.Fatal("文本额度单独提醒")
	}
	f.login(t, "design")
	f.req(t, "PUT", "/api/notification-prefs", object{"types": object{"quota_80": object{"inbox": false}}}, 200)
	f.exec(t, "INSERT INTO quotas(user_id,month_images,month_text) VALUES(?,10,NULL)", design)
	for i := 0; i < 8; i++ {
		f.exec(t, "INSERT INTO ai_usage(at,user_id,feature,model,kind,units,est_cost) VALUES(?,?,'x','m','image',1,0)", frozen.Format(time.RFC3339Nano), design)
	}
	f.remind(t)
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='quota_80'", design) != 0 {
		t.Fatal("关掉收件箱后额度提醒不写表")
	}
	if f.count(t, "SELECT count(*) FROM task_once WHERE key=?", "quota:"+itoa(design)+":image:2026-10") != 1 {
		t.Fatal("关掉收件箱也占住每月一次的名额")
	}
	f.req(t, "PUT", "/api/notification-prefs", object{"types": object{"quota_80": object{"inbox": true}}}, 200)
	f.remind(t)
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND type='quota_80'", design) != 0 {
		t.Fatal("这个月已经记过就不要补发")
	}

	f.login(t, "photo")
	mine := f.req(t, "GET", "/api/notifications?type=quota_80", nil, 200)
	for _, it := range arr(mine["items"]) {
		if _, ok := obj(it)["desktop"]; ok {
			t.Fatal("列表里不要带 desktop")
		}
		if _, ok := obj(it)["user_id"]; ok {
			t.Fatal("列表里不要带 user_id")
		}
	}
	var foreign int64
	if err := f.c.DB.QueryRow("SELECT id FROM notifications WHERE user_id=? LIMIT 1", op).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	marked := f.req(t, "POST", "/api/notifications/read", object{"ids": []int64{foreign}}, 200)
	if num(marked["updated"]) != 0 {
		t.Fatal("不能把别人的通知标已读")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE id=? AND read_at IS NULL", foreign) != 1 {
		t.Fatal("别人的通知还是未读")
	}
	f.req(t, "POST", "/api/notifications/read", object{"all": true}, 200)
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=? AND read_at IS NULL", op) == 0 {
		t.Fatal("全部已读只能标自己的")
	}
	f.login(t, "op")
	listed := f.req(t, "GET", "/api/notifications", nil, 200)
	for _, it := range arr(listed["items"]) {
		if num(obj(it)["id"]) == foreign && str(obj(it)["read_at"]) != "" {
			t.Fatal("创建人打开列表时这条还该是未读")
		}
	}
}

func TestSSE(t *testing.T) {
	f := boot(t, "", true)
	f.m.PingInterval = time.Hour
	f.login(t, "admin")
	op := f.user(t, "op", "运营小周", "operator", 1)
	photo := f.user(t, "photo", "摄影小林", "photographer", 1)
	pid, _ := f.product(t, "珠蝶", 1, 1)
	f.login(t, "photo")
	a := f.openStream(t)
	b := f.openStream(t)
	defer a.body.Close()
	defer b.body.Close()
	for _, s := range []*sseClient{a, b} {
		ev, data, err := s.next(2 * time.Second)
		if err != nil || ev != "unread" || !strings.Contains(data, `"count":0`) {
			t.Fatalf("连上先收到未读数，得到 %s %s %v", ev, data, err)
		}
	}
	f.login(t, "op")
	other := f.openStream(t)
	defer other.body.Close()
	if ev, _, err := other.next(2 * time.Second); err != nil || ev != "unread" {
		t.Fatal(ev, err)
	}
	f.login(t, "op")
	f.req(t, "POST", "/api/tasks", object{"title": "推送", "assignee_id": photo, "target_type": "product", "target_id": pid}, 201)
	for _, s := range []*sseClient{a, b} {
		ev, data, err := s.next(2 * time.Second)
		if err != nil || ev != "notification" || !strings.Contains(data, "推送") {
			t.Fatalf("两个连接都要收到：%s %s %v", ev, data, err)
		}
	}
	if _, _, err := other.next(300 * time.Millisecond); err == nil {
		t.Fatal("别人的连接不该收到这条通知")
	}
	photoID := photo
	a.body.Close()
	b.body.Close()
	waitHub(t, f, photoID, 0)
	if f.m.hub.count(op) != 1 {
		t.Fatalf("运营的连接还在，数量 %d", f.m.hub.count(op))
	}
	other.body.Close()
	waitHub(t, f, op, 0)

	f.m.PingInterval = 30 * time.Millisecond
	f.login(t, "photo")
	ping := f.openStream(t)
	defer ping.body.Close()
	if ev, _, err := ping.next(2 * time.Second); err != nil || ev != "unread" {
		t.Fatal(ev, err)
	}
	ev, data, err := ping.next(2 * time.Second)
	if err != nil || ev != "comment" || !strings.Contains(data, "ping") {
		t.Fatalf("要有保活注释：%s %s %v", ev, data, err)
	}
	ping.body.Close()
	waitHub(t, f, photo, 0)
}

func TestRules(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	list := f.req(t, "GET", "/api/task-rules", nil, 200)
	if num(list["total"]) != 4 {
		t.Fatalf("要有 4 条默认规则：%s", core.JS(list))
	}
	seen := map[string]bool{}
	for _, it := range arr(list["items"]) {
		item := obj(it)
		if item["enabled"] != false {
			t.Fatalf("默认规则要停用：%s", core.JS(item))
		}
		seen[str(item["event"])] = true
	}
	for _, ev := range []string{"product.created", "assets.first_upload", "render.failed", "review.rejected"} {
		if !seen[ev] {
			t.Fatalf("缺默认事件 %s", ev)
		}
	}
	photo := f.user(t, "photo", "摄影小林", "photographer", 1)
	var role int64
	if err := f.c.DB.QueryRow("SELECT id FROM roles WHERE key='photographer'").Scan(&role); err != nil {
		t.Fatal(err)
	}
	f.req(t, "POST", "/api/task-rules", object{"name": "坏", "event": "nope", "title_tpl": "x", "assign_mode": "fixed", "assign_user_id": photo}, 400)
	f.req(t, "POST", "/api/task-rules", object{"name": "坏小时", "event": "product.created", "title_tpl": "x", "assign_mode": "fixed", "assign_user_id": photo, "due_hours": 9000}, 400)
	created := obj(f.req(t, "POST", "/api/task-rules", object{"name": "自定义", "enabled": true, "event": "product.created", "title_tpl": "跟一下：{product}", "assign_mode": "role_round_robin", "assign_role_id": role, "due_hours": 0}, 201)["rule"])
	id := num(created["id"])
	if created["enabled"] != true || num(created["due_hours"]) != 0 {
		t.Fatalf("规则没按提交保存：%s", core.JS(created))
	}
	patched := obj(f.req(t, "PATCH", "/api/task-rules/"+itoa(id), object{"enabled": false}, 200)["rule"])
	if patched["enabled"] != false {
		t.Fatal("要能停用规则")
	}
	f.req(t, "DELETE", "/api/task-rules/"+itoa(id), nil, 200)
	f.req(t, "DELETE", "/api/task-rules/"+itoa(id), nil, 404)
	f.login(t, "photo")
	f.req(t, "GET", "/api/task-rules", nil, 403)
	f.req(t, "POST", "/api/task-rules", object{"name": "不行"}, 403)
}

func TestListFiltersAndStats(t *testing.T) {
	f := boot(t, "", true)
	frozen := time.Date(2026, 10, 18, 4, 0, 0, 0, time.UTC)
	f.m.Now = func() time.Time { return frozen }
	f.login(t, "admin")
	op := f.user(t, "op", "运营小周", "operator", 1)
	pid, _ := f.product(t, "珠蝶", 1, 1)
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(frozen.In(loc).Year(), frozen.In(loc).Month(), frozen.In(loc).Day(), 0, 0, 0, 0, loc)
	delta := (int(day.Weekday()) + 6) % 7
	weekStart := day.AddDate(0, 0, -delta)
	yesterday := day.Add(-time.Hour)
	today := day.Add(16 * time.Hour)
	outside := weekStart.AddDate(0, 0, 8)
	mk := func(title string, due *time.Time) int64 {
		t.Helper()
		body := object{"title": title, "assignee_id": int64(1), "target_type": "product", "target_id": pid, "priority": "normal"}
		if due != nil {
			body["due_at"] = due.UTC().Format(time.RFC3339Nano)
		}
		return num(obj(f.req(t, "POST", "/api/tasks", body, 201)["task"])["id"])
	}
	overdueID := mk("逾期珠蝶", &yesterday)
	todayID := mk("今天珠蝶", &today)
	laterID := mk("下周珠蝶", &outside)
	noneID := mk("无截止", nil)
	items := arr(f.req(t, "GET", "/api/tasks?view=all&status=open", nil, 200)["items"])
	order := idsOf(items)
	wantOrder := []int64{overdueID, todayID, laterID, noneID}
	if len(order) != 4 {
		t.Fatalf("排序：%v", order)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("想要 %v，得到 %v", wantOrder, order)
		}
	}
	if num(f.req(t, "GET", "/api/tasks?view=all&q="+url.QueryEscape("珠蝶"), nil, 200)["total"]) != 3 {
		t.Fatal("标题搜索不对")
	}
	one := arr(f.req(t, "GET", "/api/tasks?view=all&due=overdue", nil, 200)["items"])
	if len(one) != 1 || num(obj(one[0])["id"]) != overdueID || obj(one[0])["overdue"] != true {
		t.Fatalf("逾期筛选：%s", core.JS(one))
	}
	one = arr(f.req(t, "GET", "/api/tasks?view=all&due=today", nil, 200)["items"])
	if len(one) != 1 || num(obj(one[0])["id"]) != todayID {
		t.Fatalf("今天筛选：%s", core.JS(one))
	}
	week := idsOf(arr(f.req(t, "GET", "/api/tasks?view=all&due=week", nil, 200)["items"]))
	in := func(ts time.Time) bool {
		return !ts.Before(weekStart) && ts.Before(weekStart.AddDate(0, 0, 7))
	}
	if in(yesterday) != containsNum(week, overdueID) || in(today) != containsNum(week, todayID) || containsNum(week, laterID) {
		t.Fatalf("本周筛选：%v", week)
	}
	people := f.raw(t, "GET", "/api/tasks/assignees?target_type=product&target_id="+itoa(pid), nil, 200)
	var assignees []object
	if json.Unmarshal(people, &assignees) != nil || len(assignees) == 0 {
		t.Fatalf("可派人员要是数组：%s", people)
	}
	adminOpen := int64(-1)
	for _, p := range assignees {
		if num(p["id"]) == 1 {
			adminOpen = num(p["open_count"])
		}
	}
	if adminOpen != 4 {
		t.Fatalf("管理员手上应有 4 条未完成，得到 %d", adminOpen)
	}
	f.req(t, "GET", "/api/tasks/assignees?target_type=product&target_id=999999", nil, 404)
	f.req(t, "GET", "/api/tasks/assignees?target_type=product", nil, 400)

	onTime := frozen.Add(-2 * time.Hour)
	late := frozen.Add(-time.Hour)
	old := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	ins := func(title string, done, due time.Time, reopen int) {
		t.Helper()
		f.exec(t, "INSERT INTO tasks(title,body,status,priority,assignee_id,created_by,due_at,checklist,reopen_count,created_at,updated_at,done_at) VALUES(?,'','done','normal',1,1,?,'[]',?,?,?,?)", title, due.UTC().Format(time.RFC3339Nano), reopen, core.Now(), core.Now(), done.UTC().Format(time.RFC3339Nano))
	}
	ins("按时", onTime, onTime.Add(time.Hour), 1)
	ins("迟到", late, late.Add(-time.Hour), 2)
	ins("上月", old, old.Add(time.Hour), 9)
	f.login(t, "op")
	f.req(t, "GET", "/api/tasks/stats", nil, 403)
	f.login(t, "admin")
	f.req(t, "GET", "/api/tasks/stats?from=nope", nil, 400)
	stats := arr(f.req(t, "GET", "/api/tasks/stats?from=2026-10-18&to=2026-10-18", nil, 200)["items"])
	var row object
	for _, it := range stats {
		if num(obj(it)["id"]) == 1 {
			row = obj(it)
		}
		if num(obj(it)["id"]) == op && num(obj(it)["done"]) != 0 {
			t.Fatal("没完成的人要显示 0")
		}
	}
	if num(row["done"]) != 2 || num(row["reopen_count"]) != 3 || row["on_time_rate"].(float64) != 0.5 {
		t.Fatalf("十月统计不对：%s", core.JS(row))
	}
	cut := arr(f.req(t, "GET", "/api/tasks/stats?from=2026-10-18T00:00:00Z&to=2026-10-18T03:00:00Z", nil, 200)["items"])
	for _, it := range cut {
		if num(obj(it)["id"]) == 1 && num(obj(it)["done"]) != 1 {
			t.Fatalf("截止时刻不应包含整点那条：%s", core.JS(it))
		}
	}
	load := arr(f.req(t, "GET", "/api/tasks/load", nil, 200)["items"])
	for _, it := range load {
		if num(obj(it)["id"]) != 1 {
			continue
		}
		if num(obj(it)["open"]) != 4 || num(obj(it)["overdue"]) != 1 || num(obj(it)["due_today"]) != 1 || num(obj(it)["done_7d"]) != 2 {
			t.Fatalf("负载：%s", core.JS(it))
		}
	}
}

func waitFrame(t *testing.T, ch <-chan []byte) []byte {
	t.Helper()
	select {
	case frame := <-ch:
		return frame
	case <-time.After(2 * time.Second):
		t.Fatal("没有等到推送")
	}
	return nil
}

func parseFrame(t *testing.T, frame []byte) (string, object) {
	t.Helper()
	text := string(frame)
	ev, data := "", ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "event:") {
			ev = strings.TrimSpace(line[len("event:"):])
		}
		if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(line[len("data:"):])
		}
	}
	var obj object
	if json.Unmarshal([]byte(data), &obj) != nil {
		t.Fatalf("推送不是 JSON：%s", text)
	}
	return ev, obj
}

func waitHub(t *testing.T, f *fixture, uid int64, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f.m.hub.count(uid) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("用户 %d 的连接数是 %d，想要 %d", uid, f.m.hub.count(uid), want)
}

func itoa(n int64) string {
	return strconvI(n)
}

func strconvI(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
