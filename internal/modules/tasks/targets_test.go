package tasks

import (
	"net/http/httptest"
	"strings"
	"testing"

	"xstudio/internal/core"
)

func TestTaskListQueryDoesNotReadAuditLog(t *testing.T) {
	f := boot(t, "", true)
	for _, scope := range []string{"shops", "shares"} {
		u := core.User{ID: 2, Role: core.Role{Key: "manager", Scope: scope, Perms: []string{core.TaskView, core.TaskManage}}}
		for _, view := range []string{"mine", "created", "watching", "all"} {
			r := httptest.NewRequest("GET", "/api/tasks?view="+view+"&status=open&q=test", nil)
			where, args, err := f.m.taskFilter(u, r)
			if err != nil {
				t.Fatal(err)
			}
			for _, columns := range []string{"count(*)", taskColumns} {
				query := "SELECT " + columns + " FROM tasks t WHERE " + where
				if strings.Contains(strings.ToLower(query), "audit_log") {
					t.Fatalf("列表及 total 查询不能依赖审计日志：%s", query)
				}
				rows, err := f.c.DB.Query("EXPLAIN QUERY PLAN "+query, args...)
				if err != nil {
					t.Fatal(err)
				}
				for rows.Next() {
					var id, parent, unused int
					var detail string
					if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
						rows.Close()
						t.Fatal(err)
					}
					if strings.Contains(strings.ToLower(detail), "audit_log") {
						rows.Close()
						t.Fatalf("查询计划不应访问审计日志：%s", detail)
					}
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestDeletionScanOnlyMarksAffectedBindings(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	pid, lid := f.product(t, "本次删除", 1, 1)
	aid := testAsset(t, f, pid, 1)
	other, _ := f.product(t, "没有删除事件", 1, 1)
	var affected []int64
	for typ, targetID := range map[string]int64{"product": pid, "listing": lid, "asset": aid} {
		affected = append(affected, createTestTask(t, f, object{"title": "受影响", "target_type": typ, "target_id": targetID}))
	}
	unrelated := createTestTask(t, f, object{"title": "无事件", "target_type": "product", "target_id": other})
	f.scan(t)
	f.req(t, "DELETE", "/api/products/"+itoa(pid), nil, 200)
	// A missing object without an event must still be hidden by snapshot reads,
	// but must not cause an unrelated full-table persistence sweep each scan.
	f.exec(t, "DELETE FROM products WHERE id=?", other)
	f.scan(t)
	for _, id := range affected {
		if f.ione(t, "SELECT target_deleted FROM tasks WHERE id=?", id) != 1 {
			t.Fatal("商品删除事件应标记商品及级联子对象任务")
		}
	}
	if f.ione(t, "SELECT target_deleted FROM tasks WHERE id=?", unrelated) != 0 {
		t.Fatal("增量扫描不应全表标记没有删除事件的目标")
	}
	if target := obj(f.req(t, "GET", "/api/tasks/"+itoa(unrelated), nil, 200)["target"]); target["label"] != "已删除" {
		t.Fatal("即使后台还没有删除事件，读取也应即时识别对象不存在")
	}
	f.scan(t)
	if f.ione(t, "SELECT target_deleted FROM tasks WHERE id=?", unrelated) != 0 {
		t.Fatal("空增量也不能重新执行全表标记")
	}
}

func TestDeletionMarkerAndAuditCursorCommitTogether(t *testing.T) {
	f := boot(t, "", true)
	f.login(t, "admin")
	pid, _ := f.product(t, "删除事务", 1, 1)
	id := createTestTask(t, f, object{"title": "重试标记", "target_type": "product", "target_id": pid})
	f.scan(t)
	before := f.scalar(t, "SELECT value FROM task_cursors WHERE name='audit_id'")
	f.req(t, "DELETE", "/api/products/"+itoa(pid), nil, 200)
	f.exec(t, `CREATE TRIGGER fail_target_marker BEFORE UPDATE OF target_deleted ON tasks
 WHEN NEW.target_deleted=1 BEGIN SELECT RAISE(ABORT,'test deletion marker failure'); END`)
	if err := f.m.scanAudit(); err == nil {
		t.Fatal("标记失败应该使扫描返回错误")
	}
	if f.scalar(t, "SELECT value FROM task_cursors WHERE name='audit_id'") != before || f.ione(t, "SELECT target_deleted FROM tasks WHERE id=?", id) != 0 {
		t.Fatal("标记失败时不能推进游标或部分提交")
	}
	f.exec(t, "DROP TRIGGER fail_target_marker")
	f.scan(t)
	if f.ione(t, "SELECT target_deleted FROM tasks WHERE id=?", id) != 1 || f.scalar(t, "SELECT value FROM task_cursors WHERE name='audit_id'") == before {
		t.Fatal("下一轮应重试删除事件并一起提交标记和游标")
	}
}
