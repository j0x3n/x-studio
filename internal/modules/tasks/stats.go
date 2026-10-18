package tasks

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
	"xstudio/internal/core"
)

func (a *Module) assignees(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tt, rawID := q.Get("target_type"), q.Get("target_id")
	filter := tt != "" || rawID != ""
	var tid int64
	if filter {
		if !targetTypes[tt] {
			core.Fail(w, 400, "对象类型或编号不正确")
			return
		}
		n, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || n <= 0 {
			core.Fail(w, 400, "对象类型或编号不正确")
			return
		}
		tid = n
		if err := a.requireTarget(core.CurrentUser(r), tt, tid); err != nil {
			writeErr(w, err, "", "")
			return
		}
	}
	rows, err := a.DB.Query("SELECT u.id FROM users u WHERE u.disabled=0 ORDER BY u.id")
	if err != nil {
		writeErr(w, err, "读取可派人员失败：", "读取可派人员失败")
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		writeErr(w, err, "读取可派人员失败：", "读取可派人员失败")
		return
	}
	items := []core.Object{}
	vis, visArgs := a.visibleWhere(core.CurrentUser(r))
	for _, id := range ids {
		u, e := a.User(id)
		if e != nil || u.Disabled || !u.Has(core.TaskView) {
			continue
		}
		if filter && !a.targetVisible(u, tt, tid) {
			continue
		}
		var open int
		args := append([]any{id}, visArgs...)
		if e = a.DB.QueryRow("SELECT count(*) FROM tasks t WHERE t.assignee_id=? AND t.status IN ('todo','doing','waiting') AND ("+vis+")", args...).Scan(&open); e != nil {
			writeErr(w, e, "读取可派人员失败：", "读取可派人员失败")
			return
		}
		items = append(items, core.Object{"id": id, "name": a.userName(id), "role_name": u.Role.Name, "open_count": open})
	}
	core.WriteJSON(w, 200, items)
}

func (a *Module) load(w http.ResponseWriter, r *http.Request) {
	vis, args := a.visibleWhere(core.CurrentUser(r))
	loc := shanghai()
	local := a.now().In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	qargs := []any{sqlTime(a.now()), sqlTime(start), sqlTime(start.AddDate(0, 0, 1)), sqlTime(a.now().Add(-7 * 24 * time.Hour))}
	qargs = append(qargs, args...)
	rows, err := a.DB.Query(`SELECT t.assignee_id,
SUM(CASE WHEN t.status IN ('todo','doing','waiting') THEN 1 ELSE 0 END),
SUM(CASE WHEN t.status IN ('todo','doing','waiting') AND t.due_at IS NOT NULL AND `+dueExpr+` < julianday(?) THEN 1 ELSE 0 END),
SUM(CASE WHEN t.status IN ('todo','doing','waiting') AND t.due_at IS NOT NULL AND `+dueExpr+` >= julianday(?) AND `+dueExpr+` < julianday(?) THEN 1 ELSE 0 END),
SUM(CASE WHEN t.status='done' AND t.done_at IS NOT NULL AND `+doneExpr+` >= julianday(?) THEN 1 ELSE 0 END)
FROM tasks t WHERE t.assignee_id IS NOT NULL AND (`+vis+`) GROUP BY t.assignee_id`, qargs...)
	if err != nil {
		writeErr(w, err, "读取负载失败：", "读取负载失败")
		return
	}
	type counts struct{ open, overdue, today, done int }
	got := map[int64]counts{}
	for rows.Next() {
		var id int64
		var c counts
		if err = rows.Scan(&id, &c.open, &c.overdue, &c.today, &c.done); err != nil {
			rows.Close()
			writeErr(w, err, "读取负载失败：", "读取负载失败")
			return
		}
		got[id] = c
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		writeErr(w, err, "读取负载失败：", "读取负载失败")
		return
	}
	people, err := a.taskPeople()
	if err != nil {
		writeErr(w, err, "读取负载失败：", "读取负载失败")
		return
	}
	items := []core.Object{}
	for _, p := range people {
		c := got[p.id]
		items = append(items, core.Object{"id": p.id, "name": p.name, "role_name": p.role, "open": c.open, "overdue": c.overdue, "due_today": c.today, "done_7d": c.done})
	}
	core.WriteJSON(w, 200, core.Object{"items": items})
}

func (a *Module) stats(w http.ResponseWriter, r *http.Request) {
	u := core.CurrentUser(r)
	if u.Role.Key != "admin" {
		core.Fail(w, 403, "只有管理员可以看统计")
		return
	}
	q := r.URL.Query()
	from, hasFrom, err := parseBound(q.Get("from"), false)
	if err != nil {
		writeErr(w, err, "", "")
		return
	}
	to, hasTo, err := parseBound(q.Get("to"), true)
	if err != nil {
		writeErr(w, err, "", "")
		return
	}
	where := "status='done' AND assignee_id IS NOT NULL AND done_at IS NOT NULL"
	var args []any
	if hasFrom {
		where += " AND " + doneExpr + " >= julianday(?)"
		args = append(args, sqlTime(from))
	}
	if hasTo {
		where += " AND " + doneExpr + " < julianday(?)"
		args = append(args, sqlTime(to))
	}
	rows, err := a.DB.Query("SELECT assignee_id, count(*), SUM(CASE WHEN due_at IS NOT NULL AND "+doneExpr+" <= "+dueExpr+" THEN 1 ELSE 0 END), SUM(CASE WHEN due_at IS NOT NULL THEN 1 ELSE 0 END), coalesce(SUM(reopen_count),0) FROM tasks WHERE "+where+" GROUP BY assignee_id", args...)
	if err != nil {
		writeErr(w, err, "读取统计失败：", "读取统计失败")
		return
	}
	type agg struct{ done, onTime, withDue, reopen int }
	got := map[int64]agg{}
	for rows.Next() {
		var id int64
		var v agg
		if err = rows.Scan(&id, &v.done, &v.onTime, &v.withDue, &v.reopen); err != nil {
			rows.Close()
			writeErr(w, err, "读取统计失败：", "读取统计失败")
			return
		}
		got[id] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		writeErr(w, err, "读取统计失败：", "读取统计失败")
		return
	}
	people, err := a.taskPeople()
	if err != nil {
		writeErr(w, err, "读取统计失败：", "读取统计失败")
		return
	}
	items := []core.Object{}
	for _, p := range people {
		v := got[p.id]
		var rate any
		if v.withDue > 0 {
			rate = float64(v.onTime) / float64(v.withDue)
		}
		items = append(items, core.Object{"id": p.id, "name": p.name, "role_name": p.role, "done": v.done, "on_time_rate": rate, "reopen_count": v.reopen})
	}
	core.WriteJSON(w, 200, core.Object{"items": items})
}

type person struct {
	id   int64
	name string
	role string
}

func (a *Module) taskPeople() ([]person, error) {
	rows, err := a.DB.Query("SELECT u.id, CASE WHEN u.display_name!='' THEN u.display_name ELSE u.username END, r.name, r.key, r.perms FROM users u JOIN roles r ON r.id=u.role_id WHERE u.disabled=0 ORDER BY u.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []person
	for rows.Next() {
		var p person
		var key, raw string
		if err = rows.Scan(&p.id, &p.name, &p.role, &key, &raw); err != nil {
			return nil, err
		}
		var perms []string
		_ = json.Unmarshal([]byte(raw), &perms)
		if !(core.User{Role: core.Role{Key: key, Perms: perms}}).Has(core.TaskView) {
			continue
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func parseBound(s string, end bool) (time.Time, bool, error) {
	if s == "" {
		return time.Time{}, false, nil
	}
	loc := shanghai()
	if len(s) == 10 {
		t, err := time.ParseInLocation("2006-01-02", s, loc)
		if err != nil {
			return time.Time{}, false, bad(400, "时间范围不正确")
		}
		if end {
			t = t.AddDate(0, 0, 1)
		}
		return t, true, nil
	}
	t, ok := parseTS(s)
	if !ok {
		return time.Time{}, false, bad(400, "时间范围不正确")
	}
	return t, true, nil
}
