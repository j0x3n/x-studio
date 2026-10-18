package tasks

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xstudio/internal/core"
)

func createTestTask(t *testing.T, f *fixture, in object) int64 {
	t.Helper()
	return num(obj(f.req(t, "POST", "/api/tasks", in, 201)["task"])["id"])
}

func testAsset(t *testing.T, f *fixture, pid, owner int64) int64 {
	t.Helper()
	return f.exec(t, `INSERT INTO assets(product_id,file,orig_name,width,height,created_by,created_at) VALUES(?,'x.jpg','x.jpg',10,10,?,?)`, pid, owner, core.Now())
}

func assigneeOpen(t *testing.T, f *fixture, path string, uid int64) int64 {
	t.Helper()
	var people []object
	if err := json.Unmarshal(f.raw(t, "GET", path, nil, 200), &people); err != nil {
		t.Fatal(err)
	}
	for _, p := range people {
		if num(p["id"]) == uid {
			return num(p["open_count"])
		}
	}
	t.Fatalf("候选人员缺少 %d", uid)
	return -1
}

func TestPatchAssignmentRequiresManage(t *testing.T) {
	f := boot(t, "", true)
	photo := f.user(t, "photo", "摄影", "photographer", 1)
	design := f.user(t, "design", "美工", "designer", 1)
	f.login(t, "photo")
	id := createTestTask(t, f, object{"title": "自己的任务"})
	path := "/api/tasks/" + itoa(id)
	for _, id := range []any{design, int64(0), nil} {
		f.req(t, "PATCH", path, object{"assignee_id": id, "title": "不应保存"}, 403)
	}
	if f.ione(t, "SELECT assignee_id FROM tasks WHERE id=?", id) != photo || f.scalar(t, "SELECT title FROM tasks WHERE id=?", id) != "自己的任务" {
		t.Fatal("被拒绝的转派不能修改任何字段")
	}
	f.login(t, "admin")
	f.req(t, "PATCH", path, object{"assignee_id": design}, 200)
	f.login(t, "photo")
	f.req(t, "PATCH", path, object{"assignee_id": photo}, 200)
	f.login(t, "admin")
	f.req(t, "PATCH", path, object{"assignee_id": 0}, 200)
}

func TestDeletedTargetVisibility(t *testing.T) {
	f := boot(t, "", true)
	shop2 := f.shop(t, "外店")
	owner := f.user(t, "owner", "创建人", "operator", shop2)
	worker := f.user(t, "worker", "负责人", "photographer", shop2)
	f.user(t, "watcher", "协作人", "designer", shop2)
	watcher := f.ione(t, "SELECT id FROM users WHERE username='watcher'")
	outsider := f.user(t, "outsider", "无关运营", "operator", 1)
	pid, _ := f.product(t, "私有商品", owner, shop2)
	f.login(t, "owner")
	id := createTestTask(t, f, object{"title": "私有任务", "target_type": "product", "target_id": pid, "assignee_id": worker, "watcher_ids": []int64{watcher}})
	path := "/api/tasks/" + itoa(id)
	f.login(t, "outsider")
	createTestTask(t, f, object{"title": "未挂对象"})
	f.req(t, "GET", path, nil, 404)
	f.login(t, "admin")
	f.req(t, "DELETE", "/api/products/"+itoa(pid), nil, 200)
	for _, scan := range []bool{false, true} {
		if scan {
			f.scan(t)
		}
		f.login(t, "outsider")
		f.req(t, "GET", path, nil, 404)
		f.req(t, "PATCH", path, object{"title": "越权"}, 404)
		f.req(t, "POST", path+"/comments", object{"body": "越权"}, 404)
		list := f.req(t, "GET", "/api/tasks?view=all&page_size=1", nil, 200)
		if num(list["total"]) != 1 || str(obj(arr(list["items"])[0])["title"]) != "未挂对象" {
			t.Fatalf("删除对象不能扩大列表或 total：%s", core.JS(list))
		}
		for _, it := range arr(f.req(t, "GET", "/api/tasks/load", nil, 200)["items"]) {
			if num(obj(it)["id"]) == worker && num(obj(it)["open"]) != 0 {
				t.Fatal("负载不能计入已删除的外店对象任务")
			}
		}
		if assigneeOpen(t, f, "/api/tasks/assignees", worker) != 0 {
			t.Fatal("候选人员统计不能计入不可见任务")
		}
		for _, name := range []string{"owner", "worker", "watcher", "admin"} {
			f.login(t, name)
			target := obj(f.req(t, "GET", path, nil, 200)["target"])
			if target["label"] != "已删除" || target["path"] != "" {
				t.Fatalf("原参与人应看到删除标记：%s", core.JS(target))
			}
			f.req(t, "POST", path+"/comments", object{"body": "原参与人评论"}, 201)
		}
	}
	f.login(t, "owner")
	f.req(t, "PATCH", path, object{"title": "仍可管理原任务"}, 200)
	if f.m.canNotify(outsider, "product", pid) || !f.m.canNotify(1, "product", pid) {
		t.Fatal("缺失对象的对象通知仅管理员可接收")
	}
	managers := f.m.managersFor(happen{TargetType: "product", TargetID: pid})
	if len(managers) != 1 || managers[0] != 1 {
		t.Fatalf("已删除对象的新系统任务不能通知无关运营：%v", managers)
	}
}

func TestTaskNotificationsRequireCurrentAccess(t *testing.T) {
	f := boot(t, "", true)
	now := time.Now().UTC()
	f.m.Now = func() time.Time { return now }
	creator := f.user(t, "creator", "创建人", "operator", 1)
	worker := f.user(t, "worker", "负责人", "photographer", 1)
	watcher := f.user(t, "watcher", "协作人", "designer", 1)
	disabled := f.user(t, "disabled", "停用者", "designer", 1)
	stranger := f.user(t, "stranger", "无关者", "designer", f.shop(t, "外店"))
	f.login(t, "creator")
	id := createTestTask(t, f, object{"title": "内部任务", "assignee_id": worker, "watcher_ids": []int64{watcher, disabled}, "due_at": now.Add(time.Hour).Format(time.RFC3339Nano)})
	path := "/api/tasks/" + itoa(id)
	f.login(t, "admin")
	role := f.ione(t, "SELECT id FROM roles WHERE key='collaborator'")
	for _, uid := range []int64{creator, worker} {
		f.req(t, "PATCH", "/api/users/"+itoa(uid), object{"role_id": role}, 200)
	}
	f.req(t, "PATCH", "/api/users/"+itoa(disabled), object{"disabled": true}, 200)
	f.exec(t, "DELETE FROM notifications")
	ch, cancel := f.m.hub.subscribe(worker)
	defer cancel()
	res := f.req(t, "POST", path+"/comments", object{"body": "内部内容 @creator @worker @watcher @disabled @stranger"}, 201)
	if core.JS(res["ignored_mentions"]) != `["creator","worker","disabled","stranger"]` {
		t.Fatalf("失去权限或停用的人必须原样忽略：%s", core.JS(res))
	}
	f.req(t, "POST", path+"/comments", object{"body": "普通评论也不能绕过"}, 201)
	f.remind(t)
	now = now.Add(2 * time.Hour)
	f.remind(t)
	if f.count(t, "SELECT count(*) FROM task_once WHERE key IN (?,?)", "due:"+itoa(id), "overdue:"+itoa(id)) != 0 {
		t.Fatal("权限检查失败不能占用提醒次数")
	}
	f.req(t, "PATCH", path, object{"status": "done"}, 200)
	for _, typ := range []string{"task_comment", "task_mentioned", "task_due", "task_overdue", "task_done", "task_assigned", "task_unassigned"} {
		for _, uid := range []int64{creator, worker, disabled, stranger} {
			f.m.deliver(note{TaskID: id, UserID: uid, Type: typ, Title: "不能泄露", Link: "/tasks/" + itoa(id)})
		}
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id<>?", watcher) != 0 {
		t.Fatal("任务通知必须统一检查接收资格")
	}
	if f.count(t, "SELECT count(*) FROM notifications WHERE user_id=?", watcher) != 2 {
		t.Fatal("仍有权限的协作人应收到提及及普通评论")
	}
	select {
	case <-ch:
		t.Fatal("被收回权限的 SSE 连接不能收到任务内容")
	default:
	}
	f.login(t, "worker")
	f.req(t, "GET", path, nil, 403)
	if num(f.req(t, "GET", "/api/notifications", nil, 200)["total"]) != 0 {
		t.Fatal("通知接口不能成为无 task.view 账号的任务入口")
	}
}

func TestAssigneesCountsRespectVisibility(t *testing.T) {
	f := boot(t, "", true)
	shop2 := f.shop(t, "外店")
	worker := f.user(t, "worker", "跨店美工", "designer", 1, shop2)
	f.user(t, "manager", "本店运营", "operator", 1)
	f.user(t, "photo", "非参与摄影", "photographer", 1)
	f.login(t, "admin")
	visible, _ := f.product(t, "可见", 1, 1)
	hidden, _ := f.product(t, "不可见", 1, shop2)
	for _, pid := range []int64{visible, hidden} {
		createTestTask(t, f, object{"title": "挂对象", "target_type": "product", "target_id": pid, "assignee_id": worker})
	}
	createTestTask(t, f, object{"title": "未挂对象", "assignee_id": worker})
	f.login(t, "manager")
	for _, path := range []string{"/api/tasks/assignees", "/api/tasks/assignees?target_type=product&target_id=" + itoa(visible)} {
		if n := assigneeOpen(t, f, path, worker); n != 2 {
			t.Fatalf("只应统计请求人可见的两条任务，得到 %d", n)
		}
	}
	f.login(t, "photo")
	if n := assigneeOpen(t, f, "/api/tasks/assignees", worker); n != 0 {
		t.Fatalf("非参与人不应获知任务数，得到 %d", n)
	}
}

func TestTargetLookupDoesNotExposeExistence(t *testing.T) {
	f := boot(t, "", true)
	f.user(t, "photo", "摄影", "photographer", 1)
	pid, lid := f.product(t, "外店秘密", 1, f.shop(t, "外店"))
	aid := testAsset(t, f, pid, 1)
	f.login(t, "photo")
	for typ, id := range map[string]int64{"product": pid, "listing": lid, "asset": aid} {
		var first string
		for _, tid := range []int64{id, 999999} {
			for _, path := range []string{"/api/tasks/assignees?target_type=" + typ + "&target_id=" + itoa(tid), "/api/tasks"} {
				method, body := "GET", any(nil)
				if path == "/api/tasks" {
					method, body = "POST", object{"title": "探测", "target_type": typ, "target_id": tid}
				}
				res := string(f.raw(t, method, path, body, 404))
				if !strings.Contains(res, "对象不存在") || (first != "" && first != res) {
					t.Fatalf("不可见与不存在必须完全一致：%s / %s", first, res)
				}
				first = res
			}
		}
	}
	if f.count(t, "SELECT count(*) FROM tasks") != 0 {
		t.Fatal("失败的对象探测不应创建任务")
	}
}

func TestTargetDeletionAndReuse(t *testing.T) {
	for _, tc := range []struct {
		name, typ, action string
		cascade           bool
	}{
		{"product", "product", "product.delete", false},
		{"listing", "listing", "listing.delete", false},
		{"asset", "asset", "asset.delete", false},
		{"asset_batch", "asset", "assets.batch.delete", false},
		{"cascade_listing", "listing", "product.delete", true},
		{"cascade_asset", "asset", "product.delete", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := boot(t, "", true)
			worker := f.user(t, "worker", "原负责人", "photographer", 1)
			shop2 := f.shop(t, "新对象的店")
			newOwner := f.user(t, "new_owner", "新对象运营", "operator", shop2)
			pid, lid := f.product(t, "旧商品", 1, 1)
			targetID := pid
			if tc.typ == "listing" {
				targetID = lid
			} else if tc.typ == "asset" {
				targetID = testAsset(t, f, pid, 1)
			}
			f.login(t, "admin")
			id := createTestTask(t, f, object{"title": "旧任务", "target_type": tc.typ, "target_id": targetID, "assignee_id": worker})
			path := "/api/tasks/" + itoa(id)
			oldCreated := f.scalar(t, "SELECT target_created_at FROM tasks WHERE id=?", id)
			if tc.cascade || tc.typ == "product" {
				f.req(t, "DELETE", "/api/products/"+itoa(pid), nil, 200)
			} else if tc.typ == "listing" {
				f.req(t, "DELETE", "/api/listings/"+itoa(lid), nil, 200)
			} else {
				f.exec(t, "DELETE FROM assets WHERE id=?", targetID)
				f.audit(t, tc.action, "asset", targetID, core.JS(object{"product_id": pid}))
			}
			newPID, newLID := f.product(t, "新店秘密", newOwner, shop2)
			newID := newPID
			if tc.typ == "listing" {
				newID = newLID
			} else if tc.typ == "asset" {
				newID = testAsset(t, f, newPID, newOwner)
			}
			if newID != targetID {
				t.Fatalf("复现必须复用编号：原 %d 新 %d", targetID, newID)
			}
			// Reused rowids must have a new nanosecond creation-time snapshot.
			table := map[string]string{"product": "products", "listing": "listings", "asset": "assets"}[tc.typ]
			if f.scalar(t, "SELECT created_at FROM "+table+" WHERE id=?", newID) == oldCreated {
				t.Fatal("新对象必须具有不同的创建时间")
			}
			// Bind the new incarnation before processing the old deletion event.
			newTaskID := createTestTask(t, f, object{"title": "新对象的任务", "target_type": tc.typ, "target_id": newID, "assignee_id": newOwner})
			for _, scan := range []bool{false, true} {
				if scan {
					f.scan(t)
				}
				f.login(t, "worker")
				for _, got := range []object{f.req(t, "GET", path, nil, 200), obj(arr(f.req(t, "GET", "/api/tasks?view=all", nil, 200)["items"])[0])} {
					target := obj(got["target"])
					if target["label"] != "已删除" || target["path"] != "" {
						t.Fatalf("不能按复用的编号读取新对象：%s", core.JS(target))
					}
				}
				f.login(t, "new_owner")
				f.req(t, "GET", path, nil, 404)
				if obj(f.req(t, "GET", "/api/tasks/"+itoa(newTaskID), nil, 200)["target"])["label"] == "已删除" {
					t.Fatal("旧删除事件不能标记新对象的绑定")
				}
			}
			if f.ione(t, "SELECT target_deleted FROM tasks WHERE id=?", id) != 1 {
				t.Fatal("后台应永久记录删除状态")
			}
			f.login(t, "admin")
			f.req(t, "PATCH", path, object{"title": "仅改标题"}, 200)
			if target := obj(f.req(t, "GET", path, nil, 200)["target"]); target["label"] != "已删除" {
				t.Fatal("普通字段更新不能复活已删除的目标")
			}
			f.scan(t)
			f.login(t, "new_owner")
			if obj(f.req(t, "GET", "/api/tasks/"+itoa(newTaskID), nil, 200)["target"])["label"] == "已删除" {
				t.Fatal("旧删除记录不能污染新绑定")
			}
			f.login(t, "admin")
			f.req(t, "PATCH", path, object{"target_type": tc.typ, "target_id": newID, "assignee_id": newOwner, "watcher_ids": []int64{}}, 200)
			f.scan(t)
			f.login(t, "new_owner")
			if obj(f.req(t, "GET", path, nil, 200)["target"])["label"] == "已删除" {
				t.Fatal("显式重新绑定且校验新成员后应使用新对象")
			}
		})
	}
}

func TestTargetMigrationPreservesDeletedBindings(t *testing.T) {
	f := boot(t, "", false)
	pid, _ := f.product(t, "迁移前商品", 1, 1)
	tx, err := f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = f.m.Migrations()[0].Up(tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	old := "2026-10-01T00:00:00Z"
	id := f.exec(t, `INSERT INTO tasks(title,assignee_id,created_by,target_type,target_id,created_at,updated_at) VALUES('迁移前任务',1,1,'product',?,?,?)`, pid, old, old)
	f.exec(t, "DELETE FROM products WHERE id=?", pid)
	f.audit(t, "product.delete", "product", pid, "{}")
	newPID, _ := f.product(t, "复用编号的新商品", 1, 1)
	if pid != newPID {
		t.Fatal("需要复用旧商品编号")
	}
	activePID, _ := f.product(t, "仍存在的商品", 1, 1)
	current := core.Now()
	activeID := f.exec(t, `INSERT INTO tasks(title,created_by,target_type,target_id,created_at,updated_at) VALUES('正常旧任务',1,'product',?,?,?)`, activePID, current, current)
	tx, err = f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = f.m.Migrations()[1].Up(tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	task, err := f.m.loadTask(id)
	if err != nil || !task.TargetDeleted {
		t.Fatalf("升级不能采用编号复用的新对象：%+v %v", task, err)
	}
	active, err := f.m.loadTask(activeID)
	if err != nil || active.TargetDeleted || !f.m.taskTargetAlive(active) {
		t.Fatalf("升级应保留正常的旧绑定：%+v %v", active, err)
	}
}

func TestAutoDispatchDoesNotReuseDeletedTask(t *testing.T) {
	f := boot(t, "", true)
	f.user(t, "photo", "摄影", "photographer", 1)
	f.login(t, "admin")
	f.enable(t, "product.created")
	pid, _ := f.product(t, "旧款", 1, 1)
	f.audit(t, "product.create", "product", pid, "{}")
	f.scan(t)
	oldTask := f.ione(t, "SELECT id FROM tasks WHERE title='拍样：旧款'")
	f.req(t, "DELETE", "/api/products/"+itoa(pid), nil, 200)
	newPID, _ := f.product(t, "新款", 1, 1)
	if pid != newPID {
		t.Fatal("需要复用对象编号")
	}
	f.audit(t, "product.create", "product", newPID, "{}")
	f.scan(t)
	if f.count(t, "SELECT count(*) FROM tasks WHERE title='拍样：新款'") != 1 || f.ione(t, "SELECT target_deleted FROM tasks WHERE id=?", oldTask) != 1 {
		t.Fatal("新对象必须派新任务，不能与旧对象的未完成任务去重")
	}
}

func TestMovedAssetDeletionTracksCurrentProduct(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	oldProduct, _ := f.product(t, "原商品", 1, 1)
	newProduct, _ := f.product(t, "移入商品", 1, 1)
	aid := testAsset(t, f, oldProduct, 1)
	id := createTestTask(t, f, object{"title": "素材任务", "target_type": "asset", "target_id": aid})
	// Match the asset module's successful move and its actual audit payload.
	f.exec(t, "UPDATE assets SET product_id=? WHERE id=?", newProduct, aid)
	f.audit(t, "assets.batch.move", "asset", aid, core.JS(object{"product_id": oldProduct, "to_product_id": newProduct}))
	f.req(t, "DELETE", "/api/products/"+itoa(oldProduct), nil, 200)
	f.scan(t)
	path := "/api/tasks/" + itoa(id)
	if obj(f.req(t, "GET", path, nil, 200)["target"])["label"] != "移入商品" {
		t.Fatal("素材移走后，删除旧商品不能误标记素材任务")
	}
	f.req(t, "DELETE", "/api/products/"+itoa(newProduct), nil, 200)
	replacement, _ := f.product(t, "复用商品", 1, 1)
	newAsset := testAsset(t, f, replacement, 1)
	if newAsset != aid {
		t.Fatal("需要复用素材编号")
	}
	if obj(f.req(t, "GET", path, nil, 200)["target"])["label"] != "已删除" {
		t.Fatal("移入商品的级联删除必须标记原素材目标")
	}
	f.scan(t)
	if f.ione(t, "SELECT target_deleted FROM tasks WHERE id=?", id) != 1 {
		t.Fatal("当前所属商品删除后，增量扫描必须持久标记已移动的素材")
	}
}

func TestTaskTargetRedactsInaccessibleObject(t *testing.T) {
	f := boot(t, "", true)
	worker := f.user(t, "worker", "摄影", "photographer", 1)
	pid, _ := f.product(t, "不应泄露的名字", 1, 1)
	f.login(t, "admin")
	id := createTestTask(t, f, object{"title": "原任务", "target_type": "product", "target_id": pid, "assignee_id": worker})
	f.req(t, "PATCH", "/api/users/"+itoa(worker), object{"shop_ids": []int64{}}, 200)
	f.login(t, "worker")
	for _, got := range []object{f.req(t, "GET", "/api/tasks/"+itoa(id), nil, 200), obj(arr(f.req(t, "GET", "/api/tasks?view=all", nil, 200)["items"])[0])} {
		if target := obj(got["target"]); target["label"] != "无权查看" || target["path"] != "" {
			t.Fatalf("成员身份不应泄露已失权对象的名称：%s", core.JS(target))
		}
	}
}

func TestIgnoredMentionsPreserveToken(t *testing.T) {
	f := boot(t, "", true)
	f.user(t, "photo", "摄影", "photographer", 1)
	f.user(t, "external_jane", "私密真实姓名", "collaborator")
	f.login(t, "photo")
	id := createTestTask(t, f, object{"title": "自己的"})
	res := f.req(t, "POST", "/api/tasks/"+itoa(id)+"/comments", object{"body": "@external_jane @missing_name"}, 201)
	if core.JS(res["ignored_mentions"]) != `["external_jane","missing_name"]` || strings.Contains(core.JS(res), "私密真实姓名") {
		t.Fatalf("忽略项只能原样回显输入：%s", core.JS(res))
	}
}

func TestPatchValidatesFinalMembers(t *testing.T) {
	f := boot(t, "", true)
	shop2 := f.shop(t, "新店")
	oldWorker := f.user(t, "old_worker", "旧负责人", "photographer", 1)
	oldWatcher := f.user(t, "old_watcher", "旧协作人", "designer", 1)
	newWorker := f.user(t, "new_worker", "新负责人", "photographer", shop2)
	newWatcher := f.user(t, "new_watcher", "新协作人", "designer", shop2)
	p1, _ := f.product(t, "旧商品", 1, 1)
	p2, _ := f.product(t, "新商品", 1, shop2)
	f.login(t, "admin")
	id := createTestTask(t, f, object{"title": "换对象", "target_type": "product", "target_id": p1, "assignee_id": oldWorker, "watcher_ids": []int64{oldWatcher}})
	path := "/api/tasks/" + itoa(id)
	f.req(t, "PATCH", path, object{"target_type": "product", "target_id": p2, "assignee_id": newWorker, "watcher_ids": []int64{newWatcher}}, 200)
	for _, in := range []object{
		{"target_type": "product", "target_id": p1},
		{"assignee_id": oldWorker},
		{"watcher_ids": []int64{oldWatcher}},
		{"target_type": "product", "target_id": p1, "assignee_id": oldWorker},
	} {
		f.req(t, "PATCH", path, in, 400)
	}
	got := f.req(t, "GET", path, nil, 200)
	if assigneeOf(got) != newWorker || num(obj(got["target"])["id"]) != p2 || num(obj(arr(got["watchers"])[0])["id"]) != newWatcher {
		t.Fatalf("失败校验不能部分保存：%s", core.JS(got))
	}
	f.login(t, "old_worker")
	f.req(t, "GET", path, nil, 404)
	f.login(t, "new_worker")
	f.req(t, "GET", path, nil, 200)
}

func TestAssigneeEventsIncludeNames(t *testing.T) {
	f := boot(t, "", true)
	photo := f.user(t, "photo", "摄影旧名", "photographer", 1)
	design := f.user(t, "design", "美工", "designer", 1)
	f.login(t, "admin")
	id := createTestTask(t, f, object{"title": "时间线", "assignee_id": 0})
	path := "/api/tasks/" + itoa(id)
	for _, uid := range []int64{photo, design, 0} {
		f.req(t, "PATCH", path, object{"assignee_id": uid}, 200)
	}
	f.req(t, "PATCH", "/api/users/"+itoa(photo), object{"display_name": "摄影新名"}, 200)
	var details []object
	for _, ev := range arr(f.req(t, "GET", path, nil, 200)["events"]) {
		if obj(ev)["kind"] == "assignee" {
			details = append(details, obj(obj(ev)["detail"]))
		}
	}
	want := [][2]string{{"", "摄影新名"}, {"摄影新名", "美工"}, {"美工", ""}}
	if len(details) != len(want) {
		t.Fatalf("负责人事件数不正确：%s", core.JS(details))
	}
	for i, names := range want {
		if details[i]["from_name"] != names[0] || details[i]["to_name"] != names[1] {
			t.Fatalf("姓名应读取时解析，未分配用空串：%s", core.JS(details[i]))
		}
	}
	if f.count(t, "SELECT count(*) FROM task_events WHERE detail LIKE '%from_name%'") != 0 {
		t.Fatal("补充姓名不改变已存储的事件格式")
	}
}
