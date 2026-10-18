package tasks

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"xstudio/internal/core"
)

type auditRow struct {
	ID         int64
	At         string
	Action     string
	TargetType string
	TargetID   int64
	Detail     string
}

type jobRow struct {
	ID         int64
	Kind       string
	TargetType string
	Message    string
	Created    string
	Finished   string
	Status     string
	TargetID   int64
	UserID     int64
}

type happen struct {
	Event      string
	TargetType string
	Reason     string
	TargetID   int64
	JobUser    int64
	At         time.Time
	Exists     bool
}

type prepared struct {
	rule       rule
	ev         happen
	title      string
	body       string
	due        string
	assignee   int64
	pick       string
	candidates []int64
	managers   []int64
}

func (a *Module) scanAudit() error {
	cur, err := a.cursorInt("audit_id")
	if err != nil {
		return err
	}
	rows, err := a.DB.Query("SELECT id, at, action, target_type, target_id, detail FROM audit_log WHERE id>? ORDER BY id", cur)
	if err != nil {
		return err
	}
	var list []auditRow
	for rows.Next() {
		var v auditRow
		if err = rows.Scan(&v.ID, &v.At, &v.Action, &v.TargetType, &v.TargetID, &v.Detail); err != nil {
			rows.Close()
			return err
		}
		list = append(list, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, row := range list {
		if err = a.applyAudit(row); err != nil {
			return err
		}
	}
	return nil
}

func (a *Module) applyAudit(row auditRow) error {
	evs, notes, err := a.interpret(row)
	if err != nil {
		return err
	}
	var plans []prepared
	for _, ev := range evs {
		rules, e := a.enabledRules(ev.Event)
		if e != nil {
			return e
		}
		for _, rl := range rules {
			plans = append(plans, a.prepareRule(rl, ev))
		}
	}
	return a.commitPlans(plans, notes, func(tx *sql.Tx, ts string) error {
		if err := applyTargetAudit(tx, row); err != nil {
			return err
		}
		return setCursor(tx, "audit_id", strconv.FormatInt(row.ID, 10), ts)
	})
}

func (a *Module) interpret(row auditRow) ([]happen, []note, error) {
	at, ok := parseTS(row.At)
	if !ok {
		at = a.now()
	}
	switch row.Action {
	case "product.create":
		if row.TargetType != "product" || row.TargetID <= 0 {
			return nil, nil, nil
		}
		return []happen{a.makeHappen("product.created", "product", row.TargetID, "", 0, at)}, nil, nil
	case "assets.upload":
		if row.TargetType != "product" || row.TargetID <= 0 {
			return nil, nil, nil
		}
		var n int
		if err := a.DB.QueryRow("SELECT count(*) FROM audit_log WHERE action='assets.upload' AND target_type='product' AND target_id=? AND id<?", row.TargetID, row.ID).Scan(&n); err != nil {
			return nil, nil, err
		}
		if n > 0 {
			return nil, nil, nil
		}
		return []happen{a.makeHappen("assets.first_upload", "product", row.TargetID, "", 0, at)}, nil, nil
	case "studio.review":
		if row.TargetType != "listing" || row.TargetID <= 0 {
			return nil, nil, nil
		}
		var detail struct {
			Status string `json:"status"`
			Note   string `json:"note"`
			JobID  int64  `json:"rendered_job_id"`
		}
		_ = json.Unmarshal([]byte(row.Detail), &detail)
		if detail.Status != "approved" && detail.Status != "rejected" {
			return nil, nil, nil
		}
		var notes []note
		var evs []happen
		jobUser := int64(0)
		if detail.JobID > 0 {
			err := a.DB.QueryRow("SELECT user_id FROM jobs WHERE id=?", detail.JobID).Scan(&jobUser)
			if errors.Is(err, sql.ErrNoRows) {
				jobUser = 0
			} else if err != nil {
				return nil, nil, err
			}
		}
		if jobUser > 0 && a.canNotify(jobUser, "listing", row.TargetID) {
			product, shop, pid, exists := a.targetNames("listing", row.TargetID)
			if !exists {
				product, shop = "已删除", "已删除"
			}
			title := "出图已通过：" + product
			if detail.Status == "rejected" {
				title = "出图被退回：" + product + " · " + shop
			}
			link := ""
			if exists && pid > 0 {
				link = "/products/" + strconv.FormatInt(pid, 10)
			}
			notes = append(notes, note{UserID: jobUser, Type: "review_result", Title: clip(title, 100), Body: detail.Note, Link: link, TargetType: "listing", TargetID: row.TargetID})
		}
		if detail.Status == "rejected" {
			evs = append(evs, a.makeHappen("review.rejected", "listing", row.TargetID, detail.Note, jobUser, at))
		}
		return evs, notes, nil
	default:
		return nil, nil, nil
	}
}

func (a *Module) makeHappen(event, typ string, id int64, reason string, jobUser int64, at time.Time) happen {
	return happen{Event: event, TargetType: typ, TargetID: id, Reason: reason, JobUser: jobUser, At: at, Exists: a.targetExists(typ, id)}
}

func (a *Module) scanJobs() error {
	rows, err := a.DB.Query("SELECT id, kind, target_type, target_id, user_id, message, created_at, finished_at, status FROM jobs WHERE status IN ('done','failed') AND id NOT IN (SELECT job_id FROM task_job_seen) ORDER BY finished_at, id")
	if err != nil {
		return err
	}
	var list []jobRow
	for rows.Next() {
		var v jobRow
		if err = rows.Scan(&v.ID, &v.Kind, &v.TargetType, &v.TargetID, &v.UserID, &v.Message, &v.Created, &v.Finished, &v.Status); err != nil {
			rows.Close()
			return err
		}
		list = append(list, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, row := range list {
		if err = a.applyJob(row); err != nil {
			return err
		}
	}
	return nil
}

func (a *Module) applyJob(j jobRow) error {
	link := a.objectPath(j.TargetType, j.TargetID)
	var notes []note
	if j.UserID > 0 && a.canNotify(j.UserID, j.TargetType, j.TargetID) && j.Status == "failed" {
		notes = append(notes, note{UserID: j.UserID, Type: "job_failed", Title: "后台任务失败", Body: j.Message, Link: link, TargetType: j.TargetType, TargetID: j.TargetID})
	}
	if j.UserID > 0 && a.canNotify(j.UserID, j.TargetType, j.TargetID) && j.Status == "done" && jobDuration(j.Created, j.Finished) >= 30*time.Second {
		notes = append(notes, note{UserID: j.UserID, Type: "job_done", Title: "后台任务完成", Body: j.Message, Link: link, TargetType: j.TargetType, TargetID: j.TargetID})
	}
	var plans []prepared
	if j.Kind == "pipeline" && j.Status == "failed" && j.TargetType == "listing" && j.TargetID > 0 {
		at, ok := parseTS(j.Finished)
		if !ok {
			at, ok = parseTS(j.Created)
		}
		if !ok {
			at = a.now()
		}
		ev := a.makeHappen("render.failed", "listing", j.TargetID, j.Message, j.UserID, at)
		rules, err := a.enabledRules(ev.Event)
		if err != nil {
			return err
		}
		for _, rl := range rules {
			plans = append(plans, a.prepareRule(rl, ev))
		}
	}
	return a.commitPlans(plans, notes, func(tx *sql.Tx, ts string) error {
		if _, err := tx.Exec("INSERT INTO task_job_seen(job_id, finished_at) VALUES(?,?)", j.ID, j.Finished); err != nil {
			return err
		}
		return bumpJobCursor(tx, j.ID, ts)
	})
}

func jobDuration(created, finished string) time.Duration {
	c, ok1 := parseTS(created)
	f, ok2 := parseTS(finished)
	if !ok1 || !ok2 {
		return 0
	}
	d := f.Sub(c)
	if d < 0 {
		return 0
	}
	return d
}

func (a *Module) objectPath(typ string, id int64) string {
	_, _, pid, ok := a.targetNames(typ, id)
	if !ok || pid <= 0 {
		return ""
	}
	return "/products/" + strconv.FormatInt(pid, 10)
}

func (a *Module) prepareRule(rl rule, ev happen) prepared {
	product, shop, _, exists := a.targetNames(ev.TargetType, ev.TargetID)
	if !ev.Exists || !exists {
		product = "已删除"
		if ev.TargetType == "listing" {
			shop = "已删除"
		} else {
			shop = ""
		}
	}
	title := clip(strings.TrimSpace(fillTpl(rl.TitleTpl, product, shop, ev.Reason)), 100)
	if title == "" {
		title = "未命名任务"
	}
	p := prepared{
		rule:     rl,
		ev:       ev,
		title:    title,
		body:     clip(fillTpl(rl.BodyTpl, product, shop, ev.Reason), 5000),
		managers: a.managersFor(ev),
	}
	if rl.DueHours.Valid {
		at := ev.At
		if at.IsZero() {
			at = a.now()
		}
		p.due = at.Add(time.Duration(rl.DueHours.Int64) * time.Hour).UTC().Format(time.RFC3339Nano)
	}
	if !ev.Exists {
		return p
	}
	switch rl.AssignMode {
	case "fixed":
		if rl.AssignUser.Valid && a.eligible(rl.AssignUser.Int64, ev) {
			p.assignee = rl.AssignUser.Int64
		}
	case "product_owner":
		if id := a.productOwner(ev.TargetType, ev.TargetID); a.eligible(id, ev) {
			p.assignee = id
		}
	case "job_user":
		if a.eligible(ev.JobUser, ev) {
			p.assignee = ev.JobUser
		}
	case "role_round_robin", "role_least_loaded":
		if rl.AssignRole.Valid {
			p.candidates = a.roleCandidates(rl.AssignRole.Int64, ev)
		}
		if len(p.candidates) > 0 {
			if rl.AssignMode == "role_round_robin" {
				p.pick = "rr"
			} else {
				p.pick = "least"
			}
		}
	}
	return p
}

func fillTpl(tpl, product, shop, reason string) string {
	s := strings.ReplaceAll(tpl, "{product}", product)
	s = strings.ReplaceAll(s, "{shop}", shop)
	return strings.ReplaceAll(s, "{reason}", reason)
}

func (a *Module) canNotify(uid int64, typ string, id int64) bool {
	if uid <= 0 {
		return false
	}
	u, err := a.User(uid)
	if err != nil || u.Disabled {
		return false
	}
	if typ == "" && id == 0 {
		return true
	}
	if typ == "user" {
		return uid == id
	}
	if !a.targetExists(typ, id) {
		return u.Role.Key == "admin"
	}
	return a.targetVisible(u, typ, id)
}

func (a *Module) eligible(id int64, ev happen) bool {
	if id <= 0 {
		return false
	}
	u, err := a.User(id)
	if err != nil || u.Disabled || !u.Has(core.TaskView) {
		return false
	}
	if ev.TargetType == "" || ev.TargetID == 0 {
		return true
	}
	if !ev.Exists {
		return false
	}
	return a.targetVisible(u, ev.TargetType, ev.TargetID)
}

func (a *Module) productOwner(typ string, id int64) int64 {
	var owner int64
	var err error
	switch typ {
	case "product":
		err = a.DB.QueryRow("SELECT created_by FROM products WHERE id=?", id).Scan(&owner)
	case "listing":
		err = a.DB.QueryRow("SELECT p.created_by FROM listings l JOIN products p ON p.id=l.product_id WHERE l.id=?", id).Scan(&owner)
	case "asset":
		err = a.DB.QueryRow("SELECT p.created_by FROM assets ast JOIN products p ON p.id=ast.product_id WHERE ast.id=?", id).Scan(&owner)
	default:
		return 0
	}
	if err != nil {
		return 0
	}
	return owner
}

func (a *Module) roleCandidates(roleID int64, ev happen) []int64 {
	if roleID <= 0 {
		return nil
	}
	rows, err := a.DB.Query("SELECT id FROM users WHERE disabled=0 AND role_id=? ORDER BY id", roleID)
	if err != nil {
		return nil
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	var out []int64
	for _, id := range ids {
		if a.eligible(id, ev) {
			out = append(out, id)
		}
	}
	return out
}

func (a *Module) managersFor(ev happen) []int64 {
	rows, err := a.DB.Query("SELECT id FROM users WHERE disabled=0 ORDER BY id")
	if err != nil {
		return nil
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	var out []int64
	for _, id := range ids {
		u, err := a.User(id)
		if err != nil || u.Disabled || !u.Has(core.TaskManage) || !u.Has(core.TaskView) {
			continue
		}
		if u.Role.Key == "admin" || (ev.TargetType == "" && ev.TargetID == 0) || (ev.Exists && a.targetVisible(u, ev.TargetType, ev.TargetID)) {
			out = append(out, id)
		}
	}
	return out
}

func (a *Module) commitPlans(plans []prepared, extra []note, after func(*sql.Tx, string) error) error {
	tx, err := a.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := a.ts()
	var out []outgoing
	for _, p := range plans {
		list, e := a.writePlan(tx, p, ts)
		if e != nil {
			return e
		}
		out = append(out, list...)
	}
	for _, n := range extra {
		o, e := a.insertNote(tx, n, ts)
		if e != nil {
			return e
		}
		if o != nil {
			out = append(out, *o)
		}
	}
	if after != nil {
		if err = after(tx, ts); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.publish(out)
	return nil
}

func (a *Module) writePlan(tx *sql.Tx, p prepared, ts string) ([]outgoing, error) {
	var existing int64
	incarnation := liveTarget
	if !p.ev.Exists {
		incarnation = "NOT (" + liveTarget + ")"
	}
	err := tx.QueryRow("SELECT id FROM tasks t WHERE rule_id=? AND target_type=? AND target_id=? AND status IN ('todo','doing','waiting') AND ("+incarnation+") ORDER BY id LIMIT 1", p.rule.ID, p.ev.TargetType, p.ev.TargetID).Scan(&existing)
	if err == nil {
		at := ""
		if !p.ev.At.IsZero() {
			at = p.ev.At.UTC().Format(time.RFC3339Nano)
		}
		return nil, insertEvent(tx, existing, 0, "repeat", core.Object{"event": p.ev.Event, "at": at}, ts)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	assignee := p.assignee
	err = nil
	if p.pick == "rr" {
		assignee, err = nextRound(tx, p.rule.ID, p.candidates)
	} else if p.pick == "least" {
		assignee, err = leastLoaded(tx, p.candidates)
	}
	if err != nil {
		return nil, err
	}
	id, err := insertTask(tx, newTask{Title: p.title, Body: p.body, Status: "todo", Priority: p.rule.Priority, Assignee: assignee, CreatedBy: 0, Due: p.due, TargetType: p.ev.TargetType, TargetID: p.ev.TargetID, RuleID: p.rule.ID}, ts)
	if err != nil {
		return nil, err
	}
	if err = insertEvent(tx, id, 0, "created", core.Object{"title": p.title, "assignee_id": assignee}, ts); err != nil {
		return nil, err
	}
	if p.pick == "rr" && assignee > 0 {
		if err = setCursor(tx, fmt.Sprintf("rr:%d", p.rule.ID), strconv.FormatInt(assignee, 10), ts); err != nil {
			return nil, err
		}
	}
	link := "/tasks/" + strconv.FormatInt(id, 10)
	var out []outgoing
	if assignee > 0 {
		o, e := a.insertNote(tx, note{TaskID: id, UserID: assignee, Type: "task_assigned", Title: clip("新任务："+p.title, 100), Body: p.body, Link: link, TargetType: p.ev.TargetType, TargetID: p.ev.TargetID}, ts)
		if e != nil {
			return nil, e
		}
		if o != nil {
			out = append(out, *o)
		}
		return out, nil
	}
	for _, uid := range p.managers {
		o, e := a.insertNote(tx, note{TaskID: id, UserID: uid, Type: "task_unassigned", Title: clip("没派出去："+p.title, 100), Body: "没有可派的人", Link: link, TargetType: p.ev.TargetType, TargetID: p.ev.TargetID}, ts)
		if e != nil {
			return nil, e
		}
		if o != nil {
			out = append(out, *o)
		}
	}
	return out, nil
}

func nextRound(q dbx, ruleID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var raw string
	err := q.QueryRow("SELECT value FROM task_cursors WHERE name=?", fmt.Sprintf("rr:%d", ruleID)).Scan(&raw)
	last := int64(0)
	if err == nil {
		last, _ = strconv.ParseInt(raw, 10, 64)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	for _, id := range ids {
		if id > last {
			return id, nil
		}
	}
	return ids[0], nil
}

func leastLoaded(q dbx, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	best := ids[0]
	bestN := int64(-1)
	for _, id := range ids {
		var n int64
		if err := q.QueryRow("SELECT count(*) FROM tasks WHERE assignee_id=? AND status IN ('todo','doing','waiting')", id).Scan(&n); err != nil {
			return 0, err
		}
		if bestN < 0 || n < bestN {
			bestN = n
			best = id
		}
	}
	return best, nil
}

func (a *Module) cursorInt(name string) (int64, error) {
	var raw string
	err := a.DB.QueryRow("SELECT value FROM task_cursors WHERE name=?", name).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(raw, 10, 64)
}

func bumpJobCursor(tx *sql.Tx, id int64, ts string) error {
	var raw string
	err := tx.QueryRow("SELECT value FROM task_cursors WHERE name='job_id'").Scan(&raw)
	cur := int64(0)
	if err == nil {
		cur, _ = strconv.ParseInt(raw, 10, 64)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if id <= cur {
		return nil
	}
	return setCursor(tx, "job_id", strconv.FormatInt(id, 10), ts)
}
