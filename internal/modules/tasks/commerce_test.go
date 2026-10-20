package tasks

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"xstudio/internal/core"
)

func commerceItem(t *testing.T, f *fixture, code string) int64 {
	t.Helper()
	return num(f.req(t, "POST", "/api/inventory/items", object{"code": code, "name": "复核物料", "kind": "material", "unit": "个"}, 200)["id"])
}
func lowAgain(t *testing.T, f *fixture, id int64) {
	t.Helper()
	for _, n := range []int{0, 5} {
		f.req(t, "PATCH", "/api/inventory/items/"+itoa(id), object{"safety_stock": n}, 200)
	}
}
func commercePO(t *testing.T, f *fixture, item int64) (int64, int64) {
	t.Helper()
	sid := num(obj(f.req(t, "POST", "/api/suppliers", object{"name": "保密供应商"}, 201)["supplier"])["id"])
	po := num(f.req(t, "POST", "/api/purchase-orders", object{"supplier_id": sid, "lines": []object{{"item_id": item, "qty": 10, "amount": 1000}}}, 200)["id"])
	line := f.ione(t, "SELECT id FROM purchase_lines WHERE po_id=?", po)
	f.req(t, "POST", "/api/purchase-orders/"+itoa(po)+"/order", nil, 200)
	return po, line
}
func receiveOne(t *testing.T, f *fixture, po, line int64) {
	t.Helper()
	f.req(t, "POST", "/api/purchase-orders/"+itoa(po)+"/receipts", object{"warehouse_id": 1, "lines": []object{{"line_id": line, "qty": 1}}}, 200)
}

func TestCommerceRealProducersDispatchRepeatRestartAndCompletion(t *testing.T) {
	dir := t.TempDir()
	f := boot(t, dir, true, true)
	f.login(t, "admin")
	op1 := f.user(t, "op1", "运营1", "operator", 1)
	op2 := f.user(t, "op2", "运营2", "operator", 1)
	w1 := f.user(t, "w1", "仓管1", "warehouse", 1)
	w2 := f.user(t, "w2", "仓管2", "warehouse", 1)
	f.exec(t, "UPDATE task_rules SET enabled=1 WHERE event IN ('inventory.low_stock','purchase.received')")
	// Least loaded must choose the second operator, not the lower id.
	createTestTask(t, f, object{"title": "已有任务", "assignee_id": op1})
	item := commerceItem(t, f, "REAL-LOW")
	lowAgain(t, f, item)
	if f.ione(t, "SELECT count(*) FROM audit_log WHERE action='inventory.low_stock' AND user_id=0 AND target_type='inv_item' AND target_id=?", item) != 1 {
		t.Fatal("库存真实生产者未产生预警审计")
	}
	f.scan(t)
	tid := f.ione(t, "SELECT id FROM tasks WHERE target_type='inv_item' AND target_id=?", item)
	if f.ione(t, "SELECT assignee_id FROM tasks WHERE id=?", tid) != op2 || f.scalar(t, "SELECT priority FROM tasks WHERE id=?", tid) != "high" {
		t.Fatal("库存规则派人或优先级错误")
	}
	d := f.req(t, "GET", "/api/tasks/"+itoa(tid), nil, 200)
	if obj(d["target"])["label"] != "REAL-LOW 复核物料" || obj(d["target"])["path"] != "/inventory/items/"+itoa(item) {
		t.Fatal(d)
	}
	lowAgain(t, f, item)
	f.scan(t)
	if f.ione(t, "SELECT count(*) FROM tasks WHERE target_type='inv_item'") != 1 || f.ione(t, "SELECT count(*) FROM task_events WHERE task_id=? AND kind='repeat'", tid) != 1 {
		t.Fatal("重复预警未去重或未写时间线")
	}
	po, line := commercePO(t, f, item)
	receiveOne(t, f, po, line)
	if f.ione(t, "SELECT count(*) FROM audit_log WHERE action='purchase.receive' AND target_type='purchase_order' AND target_id=?", po) != 1 {
		t.Fatal("采购真实生产者审计错误")
	}
	f.scan(t)
	pt := f.ione(t, "SELECT id FROM tasks WHERE target_type='purchase_order' AND target_id=?", po)
	no := f.scalar(t, "SELECT no FROM purchase_orders WHERE id=?", po)
	if f.scalar(t, "SELECT title FROM tasks WHERE id=?", pt) != "到货复核："+no || f.ione(t, "SELECT assignee_id FROM tasks WHERE id=?", pt) != w1 {
		t.Fatal("到货复核文案或轮流派人错误")
	}
	pd := f.req(t, "GET", "/api/tasks/"+itoa(pt), nil, 200)
	if obj(pd["target"])["label"] != no || obj(pd["target"])["path"] != "/purchase-orders/"+itoa(po) || strings.Contains(core.JS(pd), "保密供应商") {
		t.Fatal(pd)
	}
	receiveOne(t, f, po, line)
	f.scan(t)
	if f.ione(t, "SELECT count(*) FROM task_events WHERE task_id=? AND kind='repeat'", pt) != 1 {
		t.Fatal("到货重复事件未写时间线")
	}
	f.req(t, "PATCH", "/api/tasks/"+itoa(pt), object{"status": "done"}, 200)
	f.req(t, "PATCH", "/api/tasks/"+itoa(tid), object{"status": "done"}, 200)
	// Restart keeps both audit cursor and round-robin position.
	before := f.ione(t, "SELECT count(*) FROM tasks")
	f.close()
	f = boot(t, dir, true, true)
	f.login(t, "admin")
	f.scan(t)
	if f.ione(t, "SELECT count(*) FROM tasks") != before {
		t.Fatal("重启重复派活")
	}
	receiveOne(t, f, po, line)
	f.scan(t)
	if f.ione(t, "SELECT assignee_id FROM tasks WHERE target_type='purchase_order' AND status='todo'") != w2 {
		t.Fatal("完成后新事件应派给下一个仓管")
	}
	lowAgain(t, f, item)
	f.scan(t)
	if f.ione(t, "SELECT count(*) FROM tasks WHERE target_type='inv_item'") != 2 {
		t.Fatal("完成后新预警应建新任务")
	}
	// Wrong audit name must never be consumed as a receipt event.
	before = f.ione(t, "SELECT count(*) FROM task_events")
	f.audit(t, "purchase.received", "purchase_order", po, `{}`)
	f.scan(t)
	if f.ione(t, "SELECT count(*) FROM task_events") != before {
		t.Fatal("错误地匹配了 purchase.received 审计")
	}
}

func TestCommerceRevocationDeletionAndQueuedNotices(t *testing.T) {
	for _, typ := range []string{"inv_item", "purchase_order"} {
		t.Run(typ, func(t *testing.T) {
			f := boot(t, "", true, true)
			f.login(t, "admin")
			// Separate roles let us revoke all participant classes independently.
			role := f.exec(t, "INSERT INTO roles(key,name,scope,perms,created_at) VALUES('participants','参与人','shops',?,datetime('now'))", core.JS([]string{core.TaskView, core.TaskManage, core.InventoryView, core.PurchaseManage}))
			owner := f.user(t, "owner", "创建人", "participants", 1)
			worker := f.user(t, "worker", "负责人", "participants", 1)
			watcher := f.user(t, "watcher", "协作人", "participants", 1)
			guest := f.user(t, "guest", "外部", "collaborator")
			f.exec(t, "UPDATE roles SET perms=? WHERE key='collaborator'", core.JS([]string{core.TaskView, core.TaskManage, core.InventoryView, core.PurchaseManage}))
			item := commerceItem(t, f, "SECRET")
			id := item
			if typ == "purchase_order" {
				id, _ = commercePO(t, f, item)
			}
			f.login(t, "owner")
			ch, cancel := f.m.hub.subscribe(worker)
			defer cancel()
			tid := createTestTask(t, f, object{"title": "保密标题", "body": "保密正文", "assignee_id": worker, "watcher_ids": []int64{watcher}, "target_type": typ, "target_id": id})
			var frame []byte
			select {
			case frame = <-ch:
			case <-time.After(time.Second):
				t.Fatal("缺新任务通知")
			}
			if !f.m.frameAllowed(worker, frame) {
				t.Fatal("有权限时通知不应被过滤")
			}
			for _, uid := range []int64{owner, worker, watcher} {
				u, _ := f.c.User(uid)
				vis, args := f.m.visibleWhere(u)
				var n int
				if err := f.c.DB.QueryRow("SELECT count(*) FROM tasks t WHERE t.id=? AND ("+vis+")", append([]any{tid}, args...)...).Scan(&n); err != nil || n != 1 {
					t.Fatal(n, err)
				}
			}
			f.login(t, "admin")
			f.req(t, "POST", "/api/tasks", object{"title": "不允许外部参与", "assignee_id": guest, "target_type": typ, "target_id": id}, 400)
			f.login(t, "guest")
			f.req(t, "GET", "/api/tasks/"+itoa(tid), nil, 404)
			f.req(t, "POST", "/api/tasks", object{"title": "外部绕过", "target_type": typ, "target_id": id}, 404)
			f.exec(t, "UPDATE roles SET perms=? WHERE id=?", core.JS([]string{core.TaskView, core.TaskManage}), role)
			if f.m.frameAllowed(worker, frame) {
				t.Fatal("已排队的通知泄露给被撤权负责人")
			}
			f.m.publish([]outgoing{{userID: worker, frame: frame}})
			select {
			case <-ch:
				t.Fatal("撤权后仍推送")
			default:
			}
			for _, name := range []string{"owner", "worker", "watcher"} {
				f.login(t, name)
				f.req(t, "GET", "/api/tasks/"+itoa(tid), nil, 404)
				f.req(t, "PATCH", "/api/tasks/"+itoa(tid), object{"status": "done"}, 404)
				f.req(t, "POST", "/api/tasks/"+itoa(tid)+"/comments", object{"body": "越权"}, 404)
				if num(f.req(t, "GET", "/api/tasks?view=all&q=保密", nil, 200)["total"]) != 0 {
					t.Fatal("列表泄露")
				}
				if num(f.req(t, "GET", "/api/notifications", nil, 200)["total"]) != 0 || num(f.req(t, "GET", "/api/notifications/unread-count", nil, 200)["count"]) != 0 {
					t.Fatal("通知泄露")
				}
				if num(f.req(t, "POST", "/api/notifications/read", object{"all": true}, 200)["updated"]) != 0 {
					t.Fatal("标已读数量泄露隐藏通知")
				}
				if assigneeOpen(t, f, "/api/tasks/assignees", worker) != 0 {
					t.Fatal("候选负载泄露")
				}
				for _, it := range arr(f.req(t, "GET", "/api/tasks/load", nil, 200)["items"]) {
					if num(obj(it)["open"]) != 0 {
						t.Fatal("负载泄露")
					}
				}
			}
			f.login(t, "worker")
			stream := f.openStream(t)
			if event, data, err := stream.next(time.Second); err != nil || event != "unread" || !strings.Contains(data, `"count":0`) {
				stream.body.Close()
				t.Fatal(event, data, err)
			}
			waitHub(t, f, worker, 2) // the test subscription and the HTTP stream
			// Bypass publish's guard to simulate a frame already queued before revoke.
			f.m.hub.publish(worker, frame)
			if _, data, err := stream.next(200 * time.Millisecond); err == nil {
				stream.body.Close()
				t.Fatal("SSE发送阶段泄露", data)
			}
			stream.body.Close()
			f.exec(t, "UPDATE roles SET perms=? WHERE id=?", core.JS([]string{core.TaskView, core.TaskManage, core.InventoryView, core.PurchaseManage}), role)
			// Rebinding a task cannot revive a notification containing its old target.
			f.login(t, "owner")
			f.req(t, "PATCH", "/api/tasks/"+itoa(tid), object{"target_type": nil, "target_id": nil}, 200)
			if f.m.frameAllowed(worker, frame) {
				t.Fatal("重绑目标复活旧通知")
			}
			f.req(t, "PATCH", "/api/tasks/"+itoa(tid), object{"target_type": typ, "target_id": id}, 200)
			table := "inv_items"
			if typ == "purchase_order" {
				table = "purchase_orders"
			}
			// Change the incarnation without changing the integer id.
			f.exec(t, "UPDATE "+table+" SET created_at=? WHERE id=?", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), id)
			f.req(t, "GET", "/api/tasks/"+itoa(tid), nil, 404)
			if f.m.frameAllowed(worker, frame) {
				t.Fatal("ID复用泄露旧通知")
			}
			f.exec(t, "UPDATE tasks SET status='done',done_at=? WHERE id=?", core.Now(), tid)
			f.login(t, "admin")
			for _, it := range arr(f.req(t, "GET", "/api/tasks/stats", nil, 200)["items"]) {
				if num(obj(it)["done"]) != 0 {
					t.Fatal("统计包含旧身份任务")
				}
			}
			f.login(t, "owner")
			// Physical deletion is hidden immediately, even before a scanner tick.
			f.exec(t, "PRAGMA foreign_keys=OFF")
			f.exec(t, "DELETE FROM "+table+" WHERE id=?", id)
			f.exec(t, "PRAGMA foreign_keys=ON")
			f.req(t, "GET", "/api/tasks/"+itoa(tid), nil, 404)
		})
	}
}

type legacyTasks struct{ *Module }

func (m legacyTasks) Migrations() []core.Migration { return m.Module.Migrations()[:3] }
func (legacyTasks) Routes(*core.Router)            {}

func TestCommerceUpgradePreservesBindingsAndSkipsHistoricalEvents(t *testing.T) {
	f := boot(t, "", false, true)
	f.login(t, "admin")
	if err := f.c.Migrate([]core.Module{legacyTasks{f.m}}); err != nil {
		t.Fatal(err)
	}
	pid, _ := f.product(t, "旧任务", 1, 1)
	tx, err := f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	tid, err := insertTask(tx, newTask{Title: "升级前", CreatedBy: 1, Assignee: 1, TargetType: "product", TargetID: pid}, core.Now())
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "INSERT INTO task_watchers(task_id,user_id) VALUES(?,1)", tid)
	f.exec(t, "INSERT INTO task_comments(task_id,user_id,body,created_at) VALUES(?,1,'旧评论',?)", tid, core.Now())
	f.exec(t, "INSERT INTO task_events(task_id,user_id,kind,created_at) VALUES(?,1,'created',?)", tid, core.Now())
	snapshot := f.scalar(t, "SELECT target_created_at FROM tasks WHERE id=?", tid)
	item := commerceItem(t, f, "HISTORY")
	lowAgain(t, f, item)
	po, line := commercePO(t, f, item)
	receiveOne(t, f, po, line)
	f.exec(t, "DELETE FROM roles WHERE key IN ('operator','warehouse')")
	if err = f.c.Migrate([]core.Module{f.m}); err != nil {
		t.Fatal(err)
	}
	if f.scalar(t, "SELECT target_created_at FROM tasks WHERE id=?", tid) != snapshot || !f.m.taskTargetAlive(taskRec{ID: tid, TargetType: "product", TargetID: pid}) {
		t.Fatal("升级破坏旧绑定")
	}
	for _, table := range []string{"task_watchers", "task_comments", "task_events"} {
		if f.ione(t, "SELECT count(*) FROM "+table+" WHERE task_id=?", tid) != 1 {
			t.Fatal("升级丢失关联", table)
		}
	}
	if f.ione(t, "SELECT count(*) FROM task_rules WHERE event IN ('inventory.low_stock','purchase.received') AND enabled=0 AND assign_role_id IS NULL") != 2 {
		t.Fatal("缺角色迁移不稳健")
	}
	f.exec(t, "UPDATE task_rules SET enabled=1 WHERE event IN ('inventory.low_stock','purchase.received')")
	f.scan(t)
	if f.ione(t, "SELECT count(*) FROM tasks") != 1 {
		t.Fatal("升级补派历史事件")
	}
	lowAgain(t, f, item)
	receiveOne(t, f, po, line)
	f.scan(t)
	if f.ione(t, "SELECT count(*) FROM tasks WHERE assignee_id IS NULL AND target_type IN ('inv_item','purchase_order')") != 2 {
		t.Fatal("缺角色应平稳创建未派任务")
	}
	rows, err := f.c.DB.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("升级引用损坏")
	}
}

func TestCommerceMissingModulesAndEligibility(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	f.exec(t, "UPDATE task_rules SET enabled=1")
	for _, v := range []struct{ action, typ string }{{"inventory.low_stock", "inv_item"}, {"purchase.receive", "purchase_order"}} {
		f.audit(t, v.action, v.typ, 99, `{}`)
		f.req(t, "POST", "/api/tasks", object{"title": "缺模块", "target_type": v.typ, "target_id": 99}, 404)
	}
	f.scan(t)
	if f.ione(t, "SELECT count(*) FROM tasks") != 0 {
		t.Fatal("缺模块不应派活")
	}
	// Exercise optional SQL in every aggregate path without either module.
	for _, path := range []string{"/api/tasks?view=all", "/api/tasks/load", "/api/tasks/stats", "/api/notifications", "/api/notifications/unread-count"} {
		f.raw(t, "GET", path, nil, 200)
	}
	g := boot(t, "", true, true)
	g.login(t, "admin")
	a := g.user(t, "a", "无库存权限", "operator", 1)
	b := g.user(t, "b", "有效仓管", "warehouse", 1)
	g.exec(t, "UPDATE roles SET perms=? WHERE key='operator'", core.JS([]string{core.TaskView, core.TaskManage}))
	item := commerceItem(t, g, "ELIGIBLE")
	// Fixed assignment also checks target visibility, even with task.view.
	g.exec(t, "UPDATE task_rules SET enabled=1,assign_mode='fixed',assign_user_id=? WHERE event='inventory.low_stock'", a)
	lowAgain(t, g, item)
	g.scan(t)
	if g.ione(t, "SELECT count(*) FROM tasks WHERE assignee_id IS NULL") != 1 {
		t.Fatal("派给了无对象权限的人")
	}
	g.req(t, "POST", "/api/tasks", object{"title": "不能派", "target_type": "inv_item", "target_id": item, "assignee_id": a}, 400)
	g.req(t, "POST", "/api/tasks", object{"title": "能派", "target_type": "inv_item", "target_id": item, "assignee_id": b}, 201)
	var people []object
	if err := json.Unmarshal(g.raw(t, "GET", "/api/tasks/assignees?target_type=inv_item&target_id="+itoa(item), nil, 200), &people); err != nil {
		t.Fatal(err)
	}
	for _, p := range people {
		if num(p["id"]) == a {
			t.Fatal("无权限候选人")
		}
	}
}

func TestCommerceDelayedEventCannotBindReusedID(t *testing.T) {
	f := boot(t, "", true, true)
	f.login(t, "admin")
	f.exec(t, "UPDATE task_rules SET enabled=1 WHERE event='inventory.low_stock'")
	item := commerceItem(t, f, "DELAYED")
	lowAgain(t, f, item)
	// The pending audit belongs to the previous incarnation, before scanner ran.
	f.exec(t, "UPDATE inv_items SET created_at=? WHERE id=?", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), item)
	f.scan(t)
	if f.ione(t, "SELECT count(*) FROM tasks") != 0 {
		t.Fatal("旧审计绑定到复用ID")
	}
}

func TestTasksInitialMigrationWithMissingBuiltinRoles(t *testing.T) {
	f := boot(t, "", false)
	f.exec(t, "DELETE FROM roles WHERE key IN ('photographer','designer','operator','warehouse')")
	if err := f.c.Migrate([]core.Module{f.m}); err != nil {
		t.Fatal(err)
	}
	if f.ione(t, "SELECT count(*) FROM task_rules") != 6 {
		t.Fatal("缺角色不应阻止初始化")
	}
}

func TestCommerceRuleModesValidateMergedPatch(t *testing.T) {
	f := boot(t, "", true, true)
	f.login(t, "admin")
	for _, event := range []string{"inventory.low_stock", "purchase.received"} {
		for _, mode := range []string{"product_owner", "job_user"} {
			f.req(t, "POST", "/api/task-rules", object{"name": "无效组合", "event": event, "title_tpl": "复核", "assign_mode": mode}, 400)
		}
		id := f.ione(t, "SELECT id FROM task_rules WHERE event=?", event)
		f.req(t, "PATCH", "/api/task-rules/"+itoa(id), object{"assign_mode": "job_user"}, 400)
		if f.scalar(t, "SELECT assign_mode FROM task_rules WHERE id=?", id) == "job_user" {
			t.Fatal("无效修改被写入")
		}
	}
	id := f.ione(t, "SELECT id FROM task_rules WHERE event='render.failed'")
	f.req(t, "PATCH", "/api/task-rules/"+itoa(id), object{"event": "inventory.low_stock"}, 400)
	f.req(t, "PATCH", "/api/task-rules/"+itoa(id), object{"event": "inventory.low_stock", "assign_mode": "fixed", "assign_user_id": int64(1)}, 200)
}
