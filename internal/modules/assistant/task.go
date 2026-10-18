package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
	"xstudio/internal/core"
)

// Task suggestions accept exact field names and reject explicit nulls. Optional
// fields should be omitted so the displayed card and POST payload agree.
func parseTaskArgs(raw json.RawMessage, out any, keys ...string) error {
	if err := parseArgs(raw, out); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("参数字段或类型不正确")
	}
	allowed := map[string]bool{}
	for _, key := range keys {
		allowed[key] = true
	}
	for key, value := range fields {
		if !allowed[key] || string(value) == "null" {
			return fmt.Errorf("参数字段或类型不正确")
		}
	}
	return nil
}

// Resolve through Ref to check both existence and visibility (including stale
// shares), and to use the same trusted label in the preview.
func (a *Module) taskTarget(u core.User, typ string, id int64, fields object) (string, bool) {
	_, hasType := fields["target_type"]
	_, hasID := fields["target_id"]
	if !hasType && !hasID {
		return "", true
	}
	if !hasType || !hasID || id <= 0 || (typ != "product" && typ != "listing" && typ != "asset") {
		return "", false
	}
	_, label, ok := a.reference(u, typ, id)
	return label, ok
}

// Mirrors tasks.checkMember; no dependency on the tasks package.
func (a *Module) taskMember(id int64, typ string, targetID int64) (core.User, bool) {
	u, err := a.User(id)
	if id <= 0 || err != nil || u.Disabled || !u.Has(core.TaskView) {
		return u, false
	}
	if typ != "" {
		_, _, ok := a.reference(u, typ, targetID)
		return u, ok
	}
	return u, true
}

func taskUserName(u core.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func (a *Module) listAssignees(u core.User, raw json.RawMessage) ([]object, error) {
	var in struct {
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
	}
	if err := parseTaskArgs(raw, &in, "target_type", "target_id"); err != nil {
		return nil, err
	}
	var fields object
	_ = json.Unmarshal(raw, &fields)
	if _, ok := a.taskTarget(u, in.TargetType, in.TargetID, fields); !ok {
		return nil, fmt.Errorf("对象参数不正确、对象不存在或无权限")
	}
	query := "SELECT id FROM users WHERE disabled=0"
	args := []any{}
	if !u.Has(core.TaskManage) {
		query += " AND id=?"
		args = append(args, u.ID)
	}
	// Close the rows before loading users: SQLite uses one connection.
	ids, err := core.QueryObjects(a.DB, query+" ORDER BY id", args...)
	if err != nil {
		return nil, err
	}
	vis, visArgs := taskCountScope(u)
	items := []object{}
	for _, row := range ids {
		person, ok := a.taskMember(number(row["id"]), in.TargetType, in.TargetID)
		if !ok {
			continue
		}
		var open int
		args := append([]any{person.ID}, visArgs...)
		if err = a.DB.QueryRow("SELECT count(*) FROM tasks t WHERE t.assignee_id=? AND t.status IN ('todo','doing','waiting') AND ("+vis+")", args...).Scan(&open); err != nil {
			return nil, err
		}
		items = append(items, object{"id": person.ID, "name": taskUserName(person), "username": person.Username, "role_name": person.Role.Name, "open_count": open})
		if len(items) == 50 {
			break
		}
	}
	return items, nil
}

// Mirrors tasks.visibleWhere and liveTarget for assignee workload counts. A
// deleted or reused target must not expose tasks to an unrelated task manager.
func taskCountScope(u core.User) (string, []any) {
	if u.Role.Key == "admin" {
		return "1=1", nil
	}
	part := "(t.assignee_id=? OR t.created_by=? OR EXISTS(SELECT 1 FROM task_watchers w WHERE w.task_id=t.id AND w.user_id=?))"
	args := []any{u.ID, u.ID, u.ID}
	if !u.Has(core.TaskManage) {
		return part, args
	}
	pw, pa := productScope(u)
	lw, la := listingScope(u)
	live := `t.target_deleted=0 AND (
 (t.target_type='product' AND EXISTS(SELECT 1 FROM products p WHERE p.id=t.target_id AND p.created_at=t.target_created_at)) OR
 (t.target_type='listing' AND EXISTS(SELECT 1 FROM listings l WHERE l.id=t.target_id AND l.created_at=t.target_created_at)) OR
 (t.target_type='asset' AND EXISTS(SELECT 1 FROM assets ast WHERE ast.id=t.target_id AND ast.created_at=t.target_created_at)))`
	clause := part + " OR t.target_type IS NULL OR ((" + live + ") AND ((t.target_type='product' AND EXISTS(SELECT 1 FROM products p WHERE p.id=t.target_id AND " + pw + ")) OR (t.target_type='listing' AND EXISTS(SELECT 1 FROM listings l WHERE l.id=t.target_id AND " + lw + ")) OR (t.target_type='asset' AND EXISTS(SELECT 1 FROM assets ast JOIN products p ON p.id=ast.product_id WHERE ast.id=t.target_id AND " + pw + "))))"
	args = append(args, pa...)
	args = append(args, la...)
	args = append(args, pa...)
	return "(" + clause + ")", args
}

func (a *Module) createTaskAction(u core.User, card Action) (Action, bool) {
	var in struct {
		Title      string            `json:"title"`
		Body       string            `json:"body"`
		AssigneeID *int64            `json:"assignee_id"`
		Priority   *string           `json:"priority"`
		DueAt      *string           `json:"due_at"`
		TargetType string            `json:"target_type"`
		TargetID   int64             `json:"target_id"`
		Checklist  []json.RawMessage `json:"checklist"`
		WatcherIDs []int64           `json:"watcher_ids"`
	}
	if parseTaskArgs(json.RawMessage(core.JS(card.Params)), &in, "title", "body", "assignee_id", "priority", "due_at", "target_type", "target_id", "checklist", "watcher_ids") != nil {
		return card, false
	}
	in.Title = strings.TrimSpace(in.Title)
	if n := utf8.RuneCountInString(in.Title); n < 1 || n > 100 || utf8.RuneCountInString(in.Body) > 1000 {
		return card, false
	}
	if in.Priority != nil && *in.Priority != "low" && *in.Priority != "normal" && *in.Priority != "high" && *in.Priority != "urgent" {
		return card, false
	}
	target, ok := a.taskTarget(u, in.TargetType, in.TargetID, card.Params)
	if !ok {
		return card, false
	}
	assignee := u.ID
	if in.AssigneeID != nil {
		assignee = *in.AssigneeID
	}
	if assignee < 0 || (assignee != u.ID && !u.Has(core.TaskManage)) {
		return card, false
	}
	name := "暂不派人"
	if assignee > 0 {
		person, ok := a.taskMember(assignee, in.TargetType, in.TargetID)
		if !ok {
			return card, false
		}
		name = taskUserName(person)
		if assignee == u.ID {
			name = "自己"
		}
	}
	due := ""
	if in.DueAt != nil {
		t, err := time.Parse(time.RFC3339, *in.DueAt)
		if err != nil || t.Before(time.Now().Add(-time.Hour)) {
			return card, false
		}
		loc, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			return card, false
		}
		t = t.In(loc)
		days := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
		due = t.Format("01-02") + " " + days[t.Weekday()] + " " + t.Format("15:04")
	}
	if len(in.Checklist) > 20 || len(in.WatcherIDs) > 10 {
		return card, false
	}
	for _, raw := range in.Checklist {
		var item struct {
			Text string `json:"text"`
			Done bool   `json:"done"`
		}
		if parseTaskArgs(raw, &item, "text", "done") != nil || item.Done || strings.TrimSpace(item.Text) == "" || utf8.RuneCountInString(item.Text) > 100 {
			return card, false
		}
	}
	seen := map[int64]bool{assignee: true}
	for _, id := range in.WatcherIDs {
		if seen[id] {
			return card, false
		}
		if _, ok := a.taskMember(id, in.TargetType, in.TargetID); !ok {
			return card, false
		}
		seen[id] = true
	}
	card.Params["title"] = in.Title
	card.Params["assignee_id"] = assignee
	card.Preview = object{"assignee": name, "due": due, "target": target}
	parts := []string{in.Title, name}
	for _, s := range []string{due, target} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	card.Summary = clip(strings.Join(parts, " · "), 60)
	if card.Label == "" {
		card.Label = "建任务"
	}
	return card, true
}
