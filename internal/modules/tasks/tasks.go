package tasks

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"xstudio/internal/core"
)

func (a *Module) listTasks(w http.ResponseWriter, r *http.Request) {
	u := core.CurrentUser(r)
	where, args, err := a.taskFilter(u, r)
	if err != nil {
		writeErr(w, err, "筛选任务失败：", "读取任务失败")
		return
	}
	var total int
	if err = a.DB.QueryRow("SELECT count(*) FROM tasks t WHERE "+where, args...).Scan(&total); err != nil {
		writeErr(w, err, "统计任务失败：", "读取任务失败")
		return
	}
	offset, size := core.Page(r)
	qargs := append(append([]any{}, args...), sqlTime(a.now()), size, offset)
	rows, err := a.DB.Query("SELECT "+taskColumns+" FROM tasks t WHERE "+where+" ORDER BY CASE WHEN t.status IN ('todo','doing','waiting') AND t.due_at IS NOT NULL AND "+dueExpr+" < julianday(?) THEN 0 ELSE 1 END, CASE WHEN t.due_at IS NULL THEN 1 ELSE 0 END, t.due_at, CASE t.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END, t.updated_at DESC, t.id DESC LIMIT ? OFFSET ?", qargs...)
	if err != nil {
		writeErr(w, err, "读取任务失败：", "读取任务失败")
		return
	}
	list := []taskRec{}
	for rows.Next() {
		t, e := scanTask(rows)
		if e != nil {
			rows.Close()
			writeErr(w, e, "读取任务失败：", "读取任务失败")
			return
		}
		list = append(list, t)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeErr(w, err, "读取任务失败：", "读取任务失败")
		return
	}
	rows.Close()
	ids := make([]int64, len(list))
	for i, t := range list {
		ids[i] = t.ID
	}
	watch := a.watchersOf(ids)
	counts := a.commentCounts(ids)
	items := []core.Object{}
	for _, t := range list {
		items = append(items, a.taskJSON(u, t, watch[t.ID], counts[t.ID]))
	}
	core.WriteJSON(w, 200, core.Object{"items": items, "total": total})
}

func (a *Module) commentCounts(ids []int64) map[int64]int {
	out := map[int64]int{}
	if len(ids) == 0 {
		return out
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	rows, err := a.DB.Query("SELECT task_id, count(*) FROM task_comments WHERE task_id IN ("+strings.Join(ph, ",")+") GROUP BY task_id", args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if rows.Scan(&id, &n) == nil {
			out[id] = n
		}
	}
	return out
}

func (a *Module) taskFilter(u core.User, r *http.Request) (string, []any, error) {
	vis, args := a.visibleWhere(u)
	q := r.URL.Query()
	parts := []string{vis}
	view := q.Get("view")
	if view == "" {
		view = "mine"
	}
	switch view {
	case "mine":
		parts = append(parts, "t.assignee_id=?")
		args = append(args, u.ID)
	case "created":
		parts = append(parts, "t.created_by=?")
		args = append(args, u.ID)
	case "watching":
		parts = append(parts, "EXISTS(SELECT 1 FROM task_watchers w WHERE w.task_id=t.id AND w.user_id=?)")
		args = append(args, u.ID)
	case "all":
	default:
		return "", nil, bad(400, "view 不正确")
	}
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		var set []string
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "open" {
				set = append(set, "todo", "doing", "waiting")
				continue
			}
			if !statuses[s] {
				return "", nil, bad(400, "状态不正确")
			}
			set = append(set, s)
		}
		ph := make([]string, len(set))
		for i, s := range set {
			ph[i] = "?"
			args = append(args, s)
		}
		parts = append(parts, "t.status IN ("+strings.Join(ph, ",")+")")
	}
	if raw := q.Get("assignee_id"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			return "", nil, bad(400, "负责人不正确")
		}
		if n == 0 {
			parts = append(parts, "t.assignee_id IS NULL")
		} else {
			parts = append(parts, "t.assignee_id=?")
			args = append(args, n)
		}
	}
	if raw := q.Get("priority"); raw != "" {
		if !priorities[raw] {
			return "", nil, bad(400, "优先级不正确")
		}
		parts = append(parts, "t.priority=?")
		args = append(args, raw)
	}
	tt, tid := q.Get("target_type"), q.Get("target_id")
	if tt != "" || tid != "" {
		if !targetTypes[tt] {
			return "", nil, bad(400, "对象类型或编号不正确")
		}
		id, err := strconv.ParseInt(tid, 10, 64)
		if err != nil || id <= 0 {
			return "", nil, bad(400, "对象类型或编号不正确")
		}
		parts = append(parts, "t.target_type=? AND t.target_id=?")
		args = append(args, tt, id)
	}
	if raw := strings.TrimSpace(q.Get("q")); raw != "" {
		parts = append(parts, "t.title LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLike(raw)+"%")
	}
	if raw := q.Get("due"); raw != "" {
		loc := shanghai()
		local := a.now().In(loc)
		start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
		switch raw {
		case "overdue":
			parts = append(parts, "t.status IN ('todo','doing','waiting') AND t.due_at IS NOT NULL AND "+dueExpr+" < julianday(?)")
			args = append(args, sqlTime(a.now()))
		case "today":
			parts = append(parts, "t.due_at IS NOT NULL AND "+dueExpr+" >= julianday(?) AND "+dueExpr+" < julianday(?)")
			args = append(args, sqlTime(start), sqlTime(start.AddDate(0, 0, 1)))
		case "week":
			delta := (int(start.Weekday()) + 6) % 7
			week := start.AddDate(0, 0, -delta)
			parts = append(parts, "t.due_at IS NOT NULL AND "+dueExpr+" >= julianday(?) AND "+dueExpr+" < julianday(?)")
			args = append(args, sqlTime(week), sqlTime(week.AddDate(0, 0, 7)))
		default:
			return "", nil, bad(400, "截止时间筛选不正确")
		}
	}
	return strings.Join(parts, " AND "), args, nil
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (a *Module) getTask(w http.ResponseWriter, r *http.Request) {
	u := core.CurrentUser(r)
	t, err := a.loadTask(core.ID(r, "id"))
	if err != nil || !a.canSeeTask(u, t) {
		core.Fail(w, 404, "任务不存在")
		return
	}
	obj := a.taskJSON(u, t, a.watchersOf([]int64{t.ID})[t.ID], a.commentCount(t.ID))
	comments, err := a.commentsOf(t.ID)
	if err != nil {
		writeErr(w, err, "读取评论失败：", "读取任务失败")
		return
	}
	events, err := a.eventsOf(t.ID)
	if err != nil {
		writeErr(w, err, "读取时间线失败：", "读取任务失败")
		return
	}
	obj["comments"] = comments
	obj["events"] = events
	core.WriteJSON(w, 200, obj)
}

type taskIn struct {
	Title      string      `json:"title"`
	Body       string      `json:"body"`
	Status     string      `json:"status"`
	Priority   string      `json:"priority"`
	AssigneeID *int64      `json:"assignee_id"`
	DueAt      *string     `json:"due_at"`
	TargetType string      `json:"target_type"`
	TargetID   int64       `json:"target_id"`
	Checklist  []checkItem `json:"checklist"`
	WatcherIDs []int64     `json:"watcher_ids"`
}

func (a *Module) createTask(w http.ResponseWriter, r *http.Request) {
	a.Core.Mu.Lock()
	defer a.Core.Mu.Unlock()
	u := core.CurrentUser(r)
	var in taskIn
	if !core.Decode(w, r, &in) {
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || runeLen(in.Title) > 100 {
		core.Fail(w, 400, "标题需要 1–100 字")
		return
	}
	if runeLen(in.Body) > 5000 {
		core.Fail(w, 400, "说明不能超过 5000 字")
		return
	}
	if in.Status == "" {
		in.Status = "todo"
	}
	if !statuses[in.Status] {
		core.Fail(w, 400, "状态不正确")
		return
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if !priorities[in.Priority] {
		core.Fail(w, 400, "优先级不正确")
		return
	}
	if err := validateChecks(in.Checklist); err != nil {
		writeErr(w, err, "", "")
		return
	}
	if err := a.requireTarget(u, in.TargetType, in.TargetID); err != nil {
		writeErr(w, err, "", "")
		return
	}
	assignee := u.ID
	if in.AssigneeID != nil {
		assignee = *in.AssigneeID
	}
	if assignee != u.ID && !u.Has(core.TaskManage) {
		core.Fail(w, 403, "只有任务管理员可以把任务派给别人")
		return
	}
	if assignee > 0 {
		if err := a.checkMember(assignee, "负责人", in.TargetType, in.TargetID); err != nil {
			writeErr(w, err, "", "")
			return
		}
	} else if !u.Has(core.TaskManage) {
		core.Fail(w, 403, "只有任务管理员可以把任务派给别人")
		return
	}
	watchers := dedupe(in.WatcherIDs)
	for _, id := range watchers {
		if err := a.checkMember(id, "协作人", in.TargetType, in.TargetID); err != nil {
			writeErr(w, err, "", "")
			return
		}
	}
	due := ""
	if in.DueAt != nil {
		var err error
		due, err = normDue(strings.TrimSpace(*in.DueAt))
		if err != nil {
			writeErr(w, err, "", "")
			return
		}
	}
	done := ""
	if in.Status == "done" {
		done = a.ts()
	}
	tx, err := a.DB.Begin()
	if err != nil {
		core.Fail(w, 500, "创建任务失败")
		return
	}
	defer tx.Rollback()
	ts := a.ts()
	id, err := insertTask(tx, newTask{Title: in.Title, Body: in.Body, Status: in.Status, Priority: in.Priority, Assignee: assignee, CreatedBy: u.ID, Due: due, TargetType: in.TargetType, TargetID: in.TargetID, Checklist: in.Checklist, DoneAt: done}, ts)
	if err != nil {
		writeErr(w, err, "创建任务失败：", "创建任务失败")
		return
	}
	for _, wid := range watchers {
		if _, err = tx.Exec("INSERT INTO task_watchers(task_id,user_id) VALUES(?,?)", id, wid); err != nil {
			writeErr(w, err, "创建任务失败：", "创建任务失败")
			return
		}
	}
	if err = insertEvent(tx, id, u.ID, "created", core.Object{"title": in.Title, "assignee_id": assignee}, ts); err != nil {
		writeErr(w, err, "创建任务失败：", "创建任务失败")
		return
	}
	if err = tx.Commit(); err != nil {
		core.Fail(w, 500, "创建任务失败")
		return
	}
	a.Audit(r, "task.create", "task", id, core.Object{"title": in.Title, "assignee_id": assignee, "target_type": in.TargetType, "target_id": in.TargetID})
	if assignee > 0 && assignee != u.ID {
		a.deliver(note{TaskID: id, UserID: assignee, Type: "task_assigned", Title: clip("新任务："+in.Title, 100), Body: in.Body, Link: "/tasks/" + strconv.FormatInt(id, 10), TargetType: in.TargetType, TargetID: in.TargetID})
	}
	t, _ := a.loadTask(id)
	core.WriteJSON(w, 201, core.Object{"task": a.taskJSON(u, t, a.watchersOf([]int64{id})[id], 0)})
}

func (a *Module) patchTask(w http.ResponseWriter, r *http.Request) {
	a.Core.Mu.Lock()
	defer a.Core.Mu.Unlock()
	u := core.CurrentUser(r)
	t, err := a.loadTask(core.ID(r, "id"))
	if err != nil || !a.canSeeTask(u, t) {
		core.Fail(w, 404, "任务不存在")
		return
	}
	var raw map[string]json.RawMessage
	if !core.Decode(w, r, &raw) {
		return
	}
	if len(raw) == 0 {
		core.Fail(w, 400, "没有要修改的字段")
		return
	}
	meta := t.CreatedBy == u.ID || u.Has(core.TaskManage)
	statusOK := meta || t.AssigneeID == u.ID
	changed := false
	var events []struct {
		kind   string
		detail any
	}
	var notes []note
	next := t
	for k := range raw {
		switch k {
		case "status", "checklist":
			if !statusOK {
				core.Fail(w, 403, "不能修改这个字段")
				return
			}
		case "title", "body", "assignee_id", "due_at", "priority", "target_type", "target_id", "watcher_ids":
			if !meta {
				core.Fail(w, 403, "不能修改这个字段")
				return
			}
		default:
			core.Fail(w, 400, "不支持这个字段")
			return
		}
	}
	if _, ok := raw["target_type"]; ok != (raw["target_id"] != nil) {
		core.Fail(w, 400, "对象类型和编号要一起改")
		return
	}
	if v, ok := raw["title"]; ok {
		var s string
		if json.Unmarshal(v, &s) != nil {
			core.Fail(w, 400, "标题需要 1–100 字")
			return
		}
		s = strings.TrimSpace(s)
		if s == "" || runeLen(s) > 100 {
			core.Fail(w, 400, "标题需要 1–100 字")
			return
		}
		next.Title = s
		changed = true
	}
	if v, ok := raw["body"]; ok {
		var s string
		if json.Unmarshal(v, &s) != nil || runeLen(s) > 5000 {
			core.Fail(w, 400, "说明不能超过 5000 字")
			return
		}
		next.Body = s
		changed = true
	}
	if v, ok := raw["priority"]; ok {
		var s string
		if json.Unmarshal(v, &s) != nil || !priorities[s] {
			core.Fail(w, 400, "优先级不正确")
			return
		}
		next.Priority = s
		changed = true
	}
	if v, ok := raw["status"]; ok {
		var s string
		if json.Unmarshal(v, &s) != nil || !statuses[s] {
			core.Fail(w, 400, "状态不正确")
			return
		}
		if s != t.Status {
			if t.Status == "done" {
				next.Reopen = t.Reopen + 1
				next.DoneAt = ""
			}
			if s == "done" {
				next.DoneAt = a.ts()
			}
			events = append(events, struct {
				kind   string
				detail any
			}{"status", core.Object{"from": t.Status, "to": s}})
			if s == "done" && t.CreatedBy > 0 && t.CreatedBy != u.ID {
				notes = append(notes, note{TaskID: t.ID, UserID: t.CreatedBy, Type: "task_done", Title: clip("任务已完成："+next.Title, 100), Body: "", Link: "/tasks/" + strconv.FormatInt(t.ID, 10)})
			}
		}
		next.Status = s
		changed = true
	}
	if v, ok := raw["due_at"]; ok {
		var p *string
		if json.Unmarshal(v, &p) != nil {
			core.Fail(w, 400, "截止时间格式不正确")
			return
		}
		due := ""
		if p != nil {
			var e error
			due, e = normDue(strings.TrimSpace(*p))
			if e != nil {
				writeErr(w, e, "", "")
				return
			}
		}
		if due != t.DueAt {
			events = append(events, struct {
				kind   string
				detail any
			}{"due", core.Object{"from": t.DueAt, "to": due}})
		}
		next.DueAt = due
		changed = true
	}
	if v, ok := raw["checklist"]; ok {
		var items []checkItem
		if json.Unmarshal(v, &items) != nil {
			core.Fail(w, 400, "检查清单格式不正确")
			return
		}
		if items == nil {
			items = []checkItem{}
		}
		if err = validateChecks(items); err != nil {
			writeErr(w, err, "", "")
			return
		}
		next.Checklist = core.JS(items)
		changed = true
	}
	if raw["target_type"] != nil {
		var typ *string
		var id *int64
		if json.Unmarshal(raw["target_type"], &typ) != nil || json.Unmarshal(raw["target_id"], &id) != nil {
			core.Fail(w, 400, "对象类型或编号不正确")
			return
		}
		nt, nid := "", int64(0)
		if typ != nil {
			nt = *typ
		}
		if id != nil {
			nid = *id
		}
		if nt == "" || nid == 0 {
			nt, nid = "", 0
		}
		if err = a.requireTarget(u, nt, nid); err != nil {
			writeErr(w, err, "", "")
			return
		}
		next.TargetType, next.TargetID = nt, nid
		changed = true
	}
	if v, ok := raw["assignee_id"]; ok {
		var p *int64
		if json.Unmarshal(v, &p) != nil {
			core.Fail(w, 400, "负责人不正确")
			return
		}
		id := int64(0)
		if p != nil {
			id = *p
		}
		if id < 0 {
			core.Fail(w, 400, "负责人不正确")
			return
		}
		if id != u.ID && !u.Has(core.TaskManage) {
			core.Fail(w, 403, "只有任务管理员可以把任务派给别人")
			return
		}
		if id != t.AssigneeID {
			var from, to any
			from, to = nil, nil
			if t.AssigneeID > 0 {
				from = t.AssigneeID
			}
			if id > 0 {
				to = id
			}
			events = append(events, struct {
				kind   string
				detail any
			}{"assignee", core.Object{"from": from, "to": to}})
			if id > 0 && id != u.ID {
				notes = append(notes, note{TaskID: t.ID, UserID: id, Type: "task_assigned", Title: clip("新任务："+next.Title, 100), Body: next.Body, Link: "/tasks/" + strconv.FormatInt(t.ID, 10)})
			}
		}
		next.AssigneeID = id
		changed = true
	}
	oldWatchers := a.watcherIDs(t.ID)
	watchers := oldWatchers
	setWatchers := false
	if v, ok := raw["watcher_ids"]; ok {
		if json.Unmarshal(v, &watchers) != nil {
			core.Fail(w, 400, "协作人不正确")
			return
		}
		watchers = dedupe(watchers)
		setWatchers = true
		changed = true
	}
	// Validate the final state, rather than members that this PATCH replaces.
	// Status-only updates remain available to original members of deleted targets.
	if raw["target_type"] != nil || raw["assignee_id"] != nil || setWatchers {
		typ, targetID := next.TargetType, next.TargetID
		if raw["target_type"] == nil && !a.taskTargetAlive(t) {
			// Never validate participants against a new object that reused the id.
			typ, targetID = "", 0
			addingMember := next.AssigneeID > 0 && next.AssigneeID != t.AssigneeID
			oldMembers := map[int64]bool{}
			for _, id := range oldWatchers {
				oldMembers[id] = true
			}
			for _, id := range watchers {
				addingMember = addingMember || !oldMembers[id]
			}
			if next.TargetType != "" && addingMember {
				core.Fail(w, 400, "对象已删除，请先更换或清除对象")
				return
			}
		}
		if next.AssigneeID > 0 {
			if err = a.checkMember(next.AssigneeID, "负责人", typ, targetID); err != nil {
				writeErr(w, err, "", "")
				return
			}
		}
		for _, id := range watchers {
			if err = a.checkMember(id, "协作人", typ, targetID); err != nil {
				writeErr(w, err, "", "")
				return
			}
		}
	}
	if !changed {
		core.Fail(w, 400, "没有要修改的字段")
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		core.Fail(w, 500, "保存任务失败")
		return
	}
	defer tx.Rollback()
	ts := a.ts()
	if _, err = tx.Exec(`UPDATE tasks SET title=?,body=?,status=?,priority=?,assignee_id=?,due_at=?,target_type=?,target_id=?,checklist=?,reopen_count=?,updated_at=?,done_at=? WHERE id=?`,
		next.Title, next.Body, next.Status, next.Priority, nullableID(next.AssigneeID), nullStr(next.DueAt), nullTarget(next.TargetType, next.TargetID), nullTargetID(next.TargetType, next.TargetID), next.Checklist, next.Reopen, ts, nullStr(next.DoneAt), t.ID); err != nil {
		writeErr(w, err, "保存任务失败：", "保存任务失败")
		return
	}
	if raw["target_type"] != nil {
		if err = bindTaskTarget(tx, t.ID); err != nil {
			writeErr(w, err, "保存任务对象失败：", "保存任务失败")
			return
		}
	}
	for _, ev := range events {
		if err = insertEvent(tx, t.ID, u.ID, ev.kind, ev.detail, ts); err != nil {
			writeErr(w, err, "保存任务失败：", "保存任务失败")
			return
		}
	}
	if setWatchers {
		if _, err = tx.Exec("DELETE FROM task_watchers WHERE task_id=?", t.ID); err != nil {
			writeErr(w, err, "保存任务失败：", "保存任务失败")
			return
		}
		for _, id := range watchers {
			if _, err = tx.Exec("INSERT INTO task_watchers(task_id,user_id) VALUES(?,?)", t.ID, id); err != nil {
				writeErr(w, err, "保存任务失败：", "保存任务失败")
				return
			}
		}
	}
	if err = tx.Commit(); err != nil {
		core.Fail(w, 500, "保存任务失败")
		return
	}
	a.Audit(r, "task.update", "task", t.ID, core.Object{"fields": keys(raw)})
	for _, n := range notes {
		n.TargetType, n.TargetID = next.TargetType, next.TargetID
		a.deliver(n)
	}
	saved, _ := a.loadTask(t.ID)
	core.WriteJSON(w, 200, core.Object{"task": a.taskJSON(u, saved, a.watchersOf([]int64{t.ID})[t.ID], a.commentCount(t.ID))})
}

func nullTarget(typ string, id int64) any {
	if typ == "" || id <= 0 {
		return nil
	}
	return typ
}
func nullTargetID(typ string, id int64) any {
	if typ == "" || id <= 0 {
		return nil
	}
	return id
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (a *Module) watcherIDs(taskID int64) []int64 {
	rows, err := a.DB.Query("SELECT user_id FROM task_watchers WHERE task_id=? ORDER BY user_id", taskID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

func (a *Module) eventsOf(taskID int64) ([]core.Object, error) {
	rows, err := a.DB.Query("SELECT id,user_id,kind,detail,created_at FROM task_events WHERE task_id=? ORDER BY id", taskID)
	if err != nil {
		return nil, err
	}
	type row struct {
		id, uid          int64
		kind, detail, at string
	}
	var raw []row
	for rows.Next() {
		var v row
		if err = rows.Scan(&v.id, &v.uid, &v.kind, &v.detail, &v.at); err != nil {
			rows.Close()
			return nil, err
		}
		raw = append(raw, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []core.Object{}
	for _, v := range raw {
		detail := core.JSONObj(v.detail)
		if v.kind == "assignee" {
			for _, key := range []string{"from", "to"} {
				name := ""
				if id, ok := detail[key].(float64); ok && id > 0 {
					name = a.userName(int64(id))
				}
				detail[key+"_name"] = name
			}
		}
		out = append(out, core.Object{"id": v.id, "user": a.userBrief(v.uid), "kind": v.kind, "detail": detail, "created_at": v.at})
	}
	return out, nil
}
