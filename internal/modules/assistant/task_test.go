package assistant

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
	"xstudio/internal/core"
)

// Fixture-only read projection of tasks' schema; deliberately no tasks import,
// background scanners, or dependency on the local database.
func taskTables(t *testing.T, f *fixture) {
	t.Helper()
	f.exec(t, `CREATE TABLE tasks(
 id INTEGER PRIMARY KEY, assignee_id INTEGER, created_by INTEGER NOT NULL,
 status TEXT NOT NULL, target_type TEXT, target_id INTEGER,
 target_created_at TEXT NOT NULL DEFAULT '', target_deleted INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE task_watchers(task_id INTEGER, user_id INTEGER, PRIMARY KEY(task_id,user_id));`)
}

func TestPromptTodayIncludesShanghaiTime(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.c.Cfg.PromptDir, "助手.md"), []byte("{{today}}"), 0600); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	f.req(t, "POST", "/api/assistant/chat", chatBody("现在几点"), 200)
	after := time.Now()
	got := strings.SplitN(aiPrompt(f.ai.snapshot()[0]), "\n", 2)[0]
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2} 周[日一二三四五六] \d{2}:\d{2}$`).MatchString(got) {
		t.Fatal(got)
	}
	days := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	format := func(now time.Time) string {
		now = now.In(loc)
		return now.Format("2006-01-02") + " " + days[now.Weekday()] + " " + now.Format("15:04")
	}
	if got != format(before) && got != format(after) {
		t.Fatal(got)
	}
}

func TestListAssigneesPermissionsVisibilityAndCounts(t *testing.T) {
	f := newFixture(t)
	taskTables(t, f)
	pid, lid := f.product(t, "珠蝶", 1)
	aid := f.asset(t, pid, "original")
	otherShop := f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES('其他店','other','en',?)", core.Now())
	hidden, _ := f.product(t, "隐藏款", otherShop)
	manager := f.user(t, "manager", "shops", []string{core.TaskView, core.TaskManage}, 1)
	worker := f.user(t, "worker", "shops", []string{core.TaskView}, 1)
	outsider := f.user(t, "outsider", "shops", []string{core.TaskView}, otherShop)
	guest := f.user(t, "guest", "shares", []string{core.TaskView})
	disabled := f.user(t, "disabled", "shops", []string{core.TaskView}, 1)
	noTask := f.user(t, "noTask", "shops", []string{core.ProductView}, 1)
	f.exec(t, "UPDATE users SET disabled=1 WHERE id=?", disabled)
	f.exec(t, "UPDATE users SET display_name='' WHERE id=?", outsider)
	f.exec(t, "INSERT INTO shares(product_id,user_id,created_by,created_at) VALUES(?,?,1,?)", pid, guest, core.Now())
	addTask := func(status, typ string, targetID, creator int64, stamp string, deleted int) int64 {
		var targetType, target any
		if typ != "" {
			targetType, target = typ, targetID
		}
		return f.exec(t, "INSERT INTO tasks(assignee_id,created_by,status,target_type,target_id,target_created_at,target_deleted) VALUES(?,?,?,?,?,?,?)", worker, creator, status, targetType, target, stamp, deleted)
	}
	var stamp string
	if err := f.c.DB.QueryRow("SELECT created_at FROM products WHERE id=?", pid).Scan(&stamp); err != nil {
		t.Fatal(err)
	}
	addTask("todo", "product", pid, 1, stamp, 0)
	addTask("doing", "", 0, 1, "", 0)
	addTask("waiting", "", 0, 1, "", 0)
	addTask("done", "", 0, 1, "", 0)
	addTask("canceled", "", 0, 1, "", 0)
	addTask("todo", "product", hidden, 1, stamp, 0)
	addTask("todo", "product", pid, 1, "old incarnation", 0)
	addTask("todo", "product", pid, 1, stamp, 1)
	addTask("todo", "product", hidden, manager, stamp, 0)
	watched := addTask("todo", "product", hidden, 1, stamp, 0)
	f.exec(t, "INSERT INTO task_watchers(task_id,user_id) VALUES(?,?)", watched, manager)
	// Every target kind follows the same workload visibility rules.
	for _, target := range []struct {
		typ string
		id  int64
	}{{"listing", lid}, {"asset", aid}} {
		table := map[string]string{"listing": "listings", "asset": "assets"}[target.typ]
		var targetStamp string
		if err := f.c.DB.QueryRow("SELECT created_at FROM "+table+" WHERE id=?", target.id).Scan(&targetStamp); err != nil {
			t.Fatal(err)
		}
		addTask("todo", target.typ, target.id, 1, targetStamp, 0)
	}
	list := func(uid int64, args object) []object {
		return f.tool(t, uid, "list_assignees", args).([]object)
	}
	ids := func(items []object) []int64 {
		out := []int64{}
		for _, item := range items {
			out = append(out, number(item["id"]))
		}
		return out
	}
	all := list(manager, object{})
	if !reflect.DeepEqual(ids(all), []int64{1, manager, worker, outsider, guest}) {
		t.Fatal(all)
	}
	for _, p := range all {
		if p["id"] == worker && (p["open_count"] != 7 || p["name"] != "worker同事" || p["username"] != "worker" || p["role_name"] != "worker角色") {
			t.Fatal(p)
		}
		if p["id"] == outsider && p["name"] != "outsider" {
			t.Fatal(p)
		}
	}
	for _, tc := range []struct {
		typ  string
		id   int64
		want []int64
	}{{"product", pid, []int64{1, manager, worker, guest}}, {"listing", lid, []int64{1, manager, worker}}, {"asset", aid, []int64{1, manager, worker, guest}}} {
		if got := list(manager, object{"target_type": tc.typ, "target_id": tc.id}); !reflect.DeepEqual(ids(got), tc.want) {
			t.Fatal(tc, got)
		}
	}
	self := list(worker, object{})
	if !reflect.DeepEqual(ids(self), []int64{worker}) || self[0]["open_count"] != 10 {
		t.Fatal(self)
	}
	admin := list(1, object{})
	for _, p := range admin {
		if p["id"] == worker && p["open_count"] != 10 {
			t.Fatal(p)
		}
	}
	for _, args := range []object{{"target_type": "product", "target_id": hidden}, {"target_type": "product"}, {"target_id": pid}, {"target_type": "job", "target_id": pid}, {"target_type": "product", "target_id": 999}, {"target_type": "", "target_id": 0}, {"target_id": nil}, {"unknown": 1}} {
		if _, err := f.m.tool(mustUser(t, f, manager), "list_assignees", json.RawMessage(core.JS(args))); err == nil {
			t.Fatal("invalid filter accepted", args)
		}
	}
	u := mustUser(t, f, noTask)
	if strings.Contains(catalogText(u, toolCatalog), "list_assignees") {
		t.Fatal("tool advertised without permission")
	}
	if _, err := f.m.tool(u, "list_assignees", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "权限") {
		t.Fatal(err)
	}
}

func TestListAssigneesLimitAfterFiltering(t *testing.T) {
	f := newFixture(t)
	taskTables(t, f)
	// Seed via SQL in the temporary fixture to avoid 60 password hashes.
	role := f.exec(t, "INSERT INTO roles(key,name,scope,perms,created_at) VALUES('taskPeople','人员','shops',?,?)", core.JS([]string{core.TaskView}), core.Now())
	for i := 0; i < 60; i++ {
		f.exec(t, "INSERT INTO users(username,password_hash,display_name,role_id,created_at,disabled) VALUES(?,'unused','测试',?,?,?)", fmt.Sprintf("person%02d", i), role, core.Now(), i%10 == 0)
	}
	got := f.tool(t, 1, "list_assignees", object{}).([]object)
	if len(got) != 50 || number(got[0]["id"]) != 1 || number(got[49]["id"]) != 56 {
		t.Fatal(got)
	}
}

func TestCreateTaskNormalizesTrustedPreviewAndPreservesParams(t *testing.T) {
	f := newFixture(t)
	pid, lid := f.product(t, "珠蝶", 1)
	aid := f.asset(t, pid, "original")
	worker := f.user(t, "worker", "shops", []string{core.TaskView}, 1)
	watcher := f.user(t, "watcher", "shops", []string{core.TaskView}, 1)
	validate := func(uid int64, params object, label string) Action {
		t.Helper()
		input := card("create_task", params)
		input["label"], input["summary"], input["cost_hint"], input["preview"] = label, "伪造摘要", "伪造额度", object{"assignee": "伪造负责人"}
		c, ok := f.m.validateAction(mustUser(t, f, uid), json.RawMessage(core.JS(input)))
		if !ok || c.CostHint != "" {
			t.Fatal(c, ok)
		}
		return c
	}
	c := validate(worker, object{"title": "  修图：珠蝶 \n"}, "")
	if c.Params["assignee_id"] != worker || c.Params["title"] != "修图：珠蝶" || !reflect.DeepEqual(c.Preview, object{"assignee": "自己", "due": "", "target": ""}) || c.Summary != "修图：珠蝶 · 自己" || c.Label != "建任务" {
		t.Fatal(c)
	}
	params := object{"title": " 修图：珠蝶 ", "body": "主图去灰", "assignee_id": worker, "priority": "high", "due_at": "2099-10-20T10:00:00Z", "target_type": "product", "target_id": pid, "checklist": []object{{"text": "去灰", "done": false}}, "watcher_ids": []int64{watcher}}
	c = validate(1, params, "建任务")
	if !reflect.DeepEqual(c.Preview, object{"assignee": "worker同事", "due": "10-20 周二 18:00", "target": "珠蝶"}) || c.Summary != "修图：珠蝶 · worker同事 · 10-20 周二 18:00 · 珠蝶" {
		t.Fatal(c)
	}
	params["title"] = "修图：珠蝶"
	if core.JS(params) != core.JS(c.Params) {
		t.Fatal("params changed", c.Params)
	}
	for _, target := range []struct {
		typ string
		id  int64
	}{{"listing", lid}, {"asset", aid}} {
		_, label, _ := f.m.reference(mustUser(t, f, 1), target.typ, target.id)
		c := validate(1, object{"title": "处理", "target_type": target.typ, "target_id": target.id}, "")
		if c.Preview["target"] != label || label == "" {
			t.Fatal(c)
		}
	}
	c = validate(1, object{"title": "等待认领", "assignee_id": 0}, "")
	if c.Preview["assignee"] != "暂不派人" || c.Summary != "等待认领 · 暂不派人" {
		t.Fatal(c)
	}
	const largeID int64 = 9007199254740993
	f.exec(t, "INSERT INTO users(id,username,password_hash,display_name,role_id,created_at) VALUES(?,'largeID','unused','大编号',?,?)", largeID, mustUser(t, f, worker).RoleID, core.Now())
	c = validate(1, object{"title": "处理", "assignee_id": largeID}, "")
	if c.Params["assignee_id"] != largeID || c.Preview["assignee"] != "大编号" {
		t.Fatal("integer ID lost precision", c)
	}
	c = validate(worker, object{"title": strings.Repeat("字", 100), "body": strings.Repeat("字", 1000), "due_at": time.Now().Add(-30 * time.Minute).Format(time.RFC3339)}, strings.Repeat("字", 20))
	if utf8.RuneCountInString(c.Label) != 12 || utf8.RuneCountInString(c.Summary) != 60 {
		t.Fatal(c)
	}
	open, ok := f.m.validateAction(mustUser(t, f, 1), json.RawMessage(`{"type":"open","params":{"path":"/"},"preview":{"assignee":"伪造"}}`))
	if !ok || open.Preview != nil || strings.Contains(core.JS(open), "preview") {
		t.Fatal(open)
	}
}

func TestCreateTaskRejectsInvalidCards(t *testing.T) {
	f := newFixture(t)
	pid, lid := f.product(t, "珠蝶", 1)
	aid := f.asset(t, pid, "original")
	otherShop := f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES('其他店','other','en',?)", core.Now())
	hidden, _ := f.product(t, "隐藏款", otherShop)
	worker := f.user(t, "worker", "shops", []string{core.TaskView}, 1)
	outsider := f.user(t, "outsider", "shops", []string{core.TaskView}, otherShop)
	noTask := f.user(t, "noTask", "shops", []string{core.ProductView}, 1)
	disabled := f.user(t, "disabled", "shops", []string{core.TaskView}, 1)
	f.exec(t, "UPDATE users SET disabled=1 WHERE id=?", disabled)
	checks := make([]object, 21)
	for i := range checks {
		checks[i] = object{"text": "检查", "done": false}
	}
	tests := []struct {
		name   string
		uid    int64
		params object
	}{
		{"no task.view", noTask, object{}},
		{"assign other without manage", worker, object{"assignee_id": outsider}},
		{"unassign without manage", worker, object{"assignee_id": 0}},
		{"negative assignee", 1, object{"assignee_id": -1}},
		{"missing assignee", 1, object{"assignee_id": 999}},
		{"disabled assignee", 1, object{"assignee_id": disabled}},
		{"assignee without task.view", 1, object{"assignee_id": noTask}},
		{"assignee cannot see product", 1, object{"assignee_id": outsider, "target_type": "product", "target_id": pid}},
		{"assignee cannot see listing", 1, object{"assignee_id": outsider, "target_type": "listing", "target_id": lid}},
		{"assignee cannot see asset", 1, object{"assignee_id": outsider, "target_type": "asset", "target_id": aid}},
		{"caller cannot see target", worker, object{"target_type": "product", "target_id": hidden}},
		{"missing target", 1, object{"target_type": "product", "target_id": 999}},
		{"target type alone", 1, object{"target_type": "product"}},
		{"target id alone", 1, object{"target_id": pid}},
		{"empty target", 1, object{"target_type": "", "target_id": 0}},
		{"unknown target type", 1, object{"target_type": "job", "target_id": pid}},
		{"blank title", 1, object{"title": " \n "}},
		{"long title", 1, object{"title": strings.Repeat("字", 101)}},
		{"long body", 1, object{"body": strings.Repeat("字", 1001)}},
		{"bad priority", 1, object{"priority": "critical"}},
		{"empty priority", 1, object{"priority": ""}},
		{"bad due", 1, object{"due_at": "tomorrow"}},
		{"no timezone", 1, object{"due_at": "2099-10-20T18:00:00"}},
		{"empty due", 1, object{"due_at": ""}},
		{"past due", 1, object{"due_at": time.Now().Add(-2 * time.Hour).Format(time.RFC3339)}},
		{"too many checks", 1, object{"checklist": checks}},
		{"done check", 1, object{"checklist": []object{{"text": "修图", "done": true}}}},
		{"blank check", 1, object{"checklist": []object{{"text": " "}}}},
		{"long check", 1, object{"checklist": []object{{"text": strings.Repeat("字", 101)}}}},
		{"unknown check key", 1, object{"checklist": []object{{"text": "修图", "id": 1}}}},
		{"null check", 1, object{"checklist": []any{nil}}},
		{"null done", 1, object{"checklist": []object{{"text": "修图", "done": nil}}}},
		{"watcher is assignee", 1, object{"watcher_ids": []int64{1}}},
		{"watcher is explicit assignee", 1, object{"assignee_id": worker, "watcher_ids": []int64{worker}}},
		{"too many watchers", 1, object{"watcher_ids": make([]int64, 11)}},
		{"duplicate watchers", 1, object{"watcher_ids": []int64{worker, worker}}},
		{"missing watcher", 1, object{"watcher_ids": []int64{999}}},
		{"disabled watcher", 1, object{"watcher_ids": []int64{disabled}}},
		{"watcher without task.view", 1, object{"watcher_ids": []int64{noTask}}},
		{"invisible watcher target", 1, object{"target_type": "product", "target_id": pid, "watcher_ids": []int64{outsider}}},
		{"unknown param", 1, object{"status": "done"}},
		{"wrong case", 1, object{"Title": "覆盖"}},
		{"fractional id", 1, object{"assignee_id": 1.5}},
		{"string id", 1, object{"assignee_id": "1"}},
		{"null id", 1, object{"assignee_id": nil}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := object{"title": "修图"}
			for k, v := range tc.params {
				params[k] = v
			}
			if c, ok := f.m.validateAction(mustUser(t, f, tc.uid), json.RawMessage(core.JS(card("create_task", params)))); ok {
				t.Fatal("invalid card accepted", c)
			}
		})
	}
	if _, ok := f.m.validateAction(mustUser(t, f, 1), json.RawMessage(`{"type":"create_task","params":{}}`)); ok {
		t.Fatal("missing title accepted")
	}
	if strings.Contains(actionText(mustUser(t, f, noTask)), "create_task") {
		t.Fatal("action advertised without permission")
	}
}

func TestChatCreateTaskAndAssigneeToolAreReadOnly(t *testing.T) {
	f := newFixture(t, calls("list_assignees", object{}), object{"reply": "请确认", "actions": []object{
		{"type": "create_task", "params": object{"title": " 修图 "}, "summary": "伪造", "preview": object{"assignee": "伪造"}},
		card("create_task", object{"title": ""}),
		card("create_task", object{"title": "修图", "extra": true}),
	}})
	taskTables(t, f)
	before, err := core.QueryObjects(f.c.DB, "SELECT * FROM users")
	if err != nil {
		t.Fatal(err)
	}
	o := f.req(t, "POST", "/api/assistant/chat", chatBody("派活"), 200)
	if o["dropped"] != float64(2) || len(o["actions"].([]any)) != 1 || o["rounds"] != float64(2) {
		t.Fatal(o)
	}
	c := o["actions"].([]any)[0].(map[string]any)
	if c["params"].(map[string]any)["assignee_id"] != float64(1) || c["preview"].(map[string]any)["assignee"] != "自己" || c["summary"] != "修图 · 自己" || c["label"] != "建任务" {
		t.Fatal(c)
	}
	h := aiHistory(t, f.ai.snapshot()[1])
	if !strings.Contains(h[len(h)-1]["content"].(string), `"username":"admin"`) {
		t.Fatal(h)
	}
	after, err := core.QueryObjects(f.c.DB, "SELECT * FROM users")
	if err != nil || core.JS(before) != core.JS(after) {
		t.Fatal("users mutated", err)
	}
	for _, table := range []string{"tasks", "task_watchers", "jobs"} {
		var n int
		if err := f.c.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("assistant wrote", table, n, err)
		}
	}
}

func TestTaskOpenPathsAndPermission(t *testing.T) {
	f := newFixture(t, object{"reply": "打开", "actions": []object{card("open", object{"path": "/tasks"}), card("open", object{"path": "/tasks?view=created"}), card("open", object{"path": "/tasks/5"})}})
	o := f.req(t, "POST", "/api/assistant/chat", chatBody("打开任务"), 200)
	if o["dropped"] != float64(1) || len(o["actions"].([]any)) != 2 {
		t.Fatal(o)
	}
	for _, path := range []string{"/tasks/", "/tasks/5", "/tasks%2F5", "/tasks/../", "/tasks//evil"} {
		if safePath(path) {
			t.Fatal(path)
		}
	}
	viewer := f.user(t, "viewer", "shops", []string{core.ProductView}, 1)
	f.login(t, "viewer")
	o = f.req(t, "POST", "/api/assistant/chat", chatBody("打开任务"), 200)
	if o["dropped"] != float64(3) || len(o["actions"].([]any)) != 0 {
		t.Fatal(o, viewer)
	}
}

func TestChatTaskCapabilitiesRequireTaskView(t *testing.T) {
	f := newFixture(t, calls("list_assignees", object{}), object{"reply": "无任务权限", "actions": []object{card("create_task", object{"title": "修图"})}})
	f.user(t, "viewer", "shops", []string{core.ProductView}, 1)
	f.login(t, "viewer")
	o := f.req(t, "POST", "/api/assistant/chat", chatBody("派活"), 200)
	if o["dropped"] != float64(1) || len(o["actions"].([]any)) != 0 {
		t.Fatal(o)
	}
	requests := f.ai.snapshot()
	if strings.Contains(aiPrompt(requests[0]), "list_assignees") || strings.Contains(aiPrompt(requests[0]), "create_task") {
		t.Fatal(aiPrompt(requests[0]))
	}
	h := aiHistory(t, requests[1])
	if !strings.Contains(h[len(h)-1]["content"].(string), "没有此工具的权限") {
		t.Fatal(h)
	}
}
