package tasks

import (
	"database/sql"
	"net/http"
	"strings"
	"xstudio/internal/core"
)

const ruleColumns = "id,name,enabled,event,title_tpl,body_tpl,assign_mode,assign_user_id,assign_role_id,due_hours,priority,created_at,updated_at"

func scanRule(s core.Scanner) (rule, error) {
	var v rule
	var enabled int
	err := s.Scan(&v.ID, &v.Name, &enabled, &v.Event, &v.TitleTpl, &v.BodyTpl, &v.AssignMode, &v.AssignUser, &v.AssignRole, &v.DueHours, &v.Priority, &v.CreatedAt, &v.UpdatedAt)
	v.Enabled = enabled != 0
	return v, err
}

func (a *Module) ruleJSON(v rule) core.Object {
	var userID, roleID, hours any
	if v.AssignUser.Valid {
		userID = v.AssignUser.Int64
	}
	if v.AssignRole.Valid {
		roleID = v.AssignRole.Int64
	}
	if v.DueHours.Valid {
		hours = v.DueHours.Int64
	}
	return core.Object{
		"id": v.ID, "name": v.Name, "enabled": v.Enabled, "event": v.Event,
		"title_tpl": v.TitleTpl, "body_tpl": v.BodyTpl, "assign_mode": v.AssignMode,
		"assign_user_id": userID, "assign_role_id": roleID, "due_hours": hours,
		"priority": v.Priority, "created_at": v.CreatedAt, "updated_at": v.UpdatedAt,
	}
}

type ruleIn struct {
	Name       *string `json:"name"`
	Enabled    *bool   `json:"enabled"`
	Event      *string `json:"event"`
	TitleTpl   *string `json:"title_tpl"`
	BodyTpl    *string `json:"body_tpl"`
	AssignMode *string `json:"assign_mode"`
	AssignUser *int64  `json:"assign_user_id"`
	AssignRole *int64  `json:"assign_role_id"`
	DueHours   *int64  `json:"due_hours"`
	Priority   *string `json:"priority"`
}

func (a *Module) applyRuleIn(v rule, in ruleIn, partial bool) (rule, error) {
	if !partial || in.Name != nil {
		if in.Name == nil {
			return v, bad(400, "规则名称需要 1–100 字")
		}
		v.Name = strings.TrimSpace(*in.Name)
	}
	if !partial || in.Enabled != nil {
		if in.Enabled != nil {
			v.Enabled = *in.Enabled
		}
	}
	if !partial || in.Event != nil {
		if in.Event == nil || !ruleEvents[*in.Event] {
			return v, bad(400, "不支持这个事件")
		}
		v.Event = *in.Event
	}
	if !partial || in.TitleTpl != nil {
		if in.TitleTpl == nil {
			return v, bad(400, "标题模板需要 1–100 字")
		}
		v.TitleTpl = strings.TrimSpace(*in.TitleTpl)
	}
	if !partial || in.BodyTpl != nil {
		if in.BodyTpl == nil {
			v.BodyTpl = ""
		} else {
			v.BodyTpl = *in.BodyTpl
		}
	}
	if !partial || in.AssignMode != nil {
		if in.AssignMode == nil || !assignModes[*in.AssignMode] {
			return v, bad(400, "派活方式不正确")
		}
		v.AssignMode = *in.AssignMode
	}
	if !partial || in.AssignUser != nil || in.AssignRole != nil || in.AssignMode != nil {
		if in.AssignUser != nil {
			if *in.AssignUser <= 0 {
				v.AssignUser = sql.NullInt64{}
			} else {
				v.AssignUser = sql.NullInt64{Int64: *in.AssignUser, Valid: true}
			}
		} else if !partial {
			v.AssignUser = sql.NullInt64{}
		}
		if in.AssignRole != nil {
			if *in.AssignRole <= 0 {
				v.AssignRole = sql.NullInt64{}
			} else {
				v.AssignRole = sql.NullInt64{Int64: *in.AssignRole, Valid: true}
			}
		} else if !partial {
			v.AssignRole = sql.NullInt64{}
		}
	}
	if !partial || in.DueHours != nil {
		if in.DueHours == nil || *in.DueHours < 0 {
			if in.DueHours != nil && *in.DueHours < 0 {
				return v, bad(400, "截止小时数不正确")
			}
			if !partial || in.DueHours != nil {
				v.DueHours = sql.NullInt64{}
			}
		} else {
			if *in.DueHours > 8760 {
				return v, bad(400, "截止小时数不正确")
			}
			v.DueHours = sql.NullInt64{Int64: *in.DueHours, Valid: true}
		}
	}
	if !partial || in.Priority != nil {
		if in.Priority == nil || *in.Priority == "" {
			v.Priority = "normal"
		} else if !priorities[*in.Priority] {
			return v, bad(400, "优先级不正确")
		} else {
			v.Priority = *in.Priority
		}
	}
	if v.Name == "" || runeLen(v.Name) > 100 {
		return v, bad(400, "规则名称需要 1–100 字")
	}
	if v.TitleTpl == "" || runeLen(v.TitleTpl) > 100 {
		return v, bad(400, "标题模板需要 1–100 字")
	}
	if runeLen(v.BodyTpl) > 5000 {
		return v, bad(400, "说明模板不能超过 5000 字")
	}
	if !ruleEvents[v.Event] {
		return v, bad(400, "不支持这个事件")
	}
	if !assignModes[v.AssignMode] {
		return v, bad(400, "派活方式不正确")
	}
	if !priorities[v.Priority] {
		return v, bad(400, "优先级不正确")
	}
	switch v.AssignMode {
	case "fixed":
		if !v.AssignUser.Valid {
			return v, bad(400, "负责人不存在")
		}
		var n int
		if a.DB.QueryRow("SELECT count(*) FROM users WHERE id=?", v.AssignUser.Int64).Scan(&n) != nil || n == 0 {
			return v, bad(400, "负责人不存在")
		}
		v.AssignRole = sql.NullInt64{}
	case "role_round_robin", "role_least_loaded":
		if !v.AssignRole.Valid {
			return v, bad(400, "角色不存在")
		}
		var n int
		if a.DB.QueryRow("SELECT count(*) FROM roles WHERE id=?", v.AssignRole.Int64).Scan(&n) != nil || n == 0 {
			return v, bad(400, "角色不存在")
		}
		v.AssignUser = sql.NullInt64{}
	default:
		v.AssignUser = sql.NullInt64{}
		v.AssignRole = sql.NullInt64{}
	}
	return v, nil
}

func (a *Module) listRules(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query("SELECT " + ruleColumns + " FROM task_rules ORDER BY id")
	if err != nil {
		writeErr(w, err, "读取规则失败：", "读取规则失败")
		return
	}
	defer rows.Close()
	items := []core.Object{}
	for rows.Next() {
		v, e := scanRule(rows)
		if e != nil {
			writeErr(w, e, "读取规则失败：", "读取规则失败")
			return
		}
		items = append(items, a.ruleJSON(v))
	}
	if err = rows.Err(); err != nil {
		writeErr(w, err, "读取规则失败：", "读取规则失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"items": items, "total": len(items)})
}

func (a *Module) createRule(w http.ResponseWriter, r *http.Request) {
	var in ruleIn
	if !core.Decode(w, r, &in) {
		return
	}
	v, err := a.applyRuleIn(rule{}, in, false)
	if err != nil {
		writeErr(w, err, "", "")
		return
	}
	ts := a.ts()
	res, err := a.DB.Exec(`INSERT INTO task_rules(name,enabled,event,title_tpl,body_tpl,assign_mode,assign_user_id,assign_role_id,due_hours,priority,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.Name, boolInt(v.Enabled), v.Event, v.TitleTpl, v.BodyTpl, v.AssignMode, nullInt(v.AssignUser), nullInt(v.AssignRole), nullInt(v.DueHours), v.Priority, ts, ts)
	if err != nil {
		writeErr(w, err, "创建规则失败：", "创建规则失败")
		return
	}
	v.ID, _ = res.LastInsertId()
	v.CreatedAt, v.UpdatedAt = ts, ts
	a.Audit(r, "task_rule.create", "task_rule", v.ID, a.ruleJSON(v))
	core.WriteJSON(w, 201, core.Object{"rule": a.ruleJSON(v)})
}

func (a *Module) patchRule(w http.ResponseWriter, r *http.Request) {
	v, err := scanRule(a.DB.QueryRow("SELECT "+ruleColumns+" FROM task_rules WHERE id=?", core.ID(r, "id")))
	if err != nil {
		core.Fail(w, 404, "规则不存在")
		return
	}
	var in ruleIn
	if !core.Decode(w, r, &in) {
		return
	}
	v, err = a.applyRuleIn(v, in, true)
	if err != nil {
		writeErr(w, err, "", "")
		return
	}
	ts := a.ts()
	if _, err = a.DB.Exec(`UPDATE task_rules SET name=?,enabled=?,event=?,title_tpl=?,body_tpl=?,assign_mode=?,assign_user_id=?,assign_role_id=?,due_hours=?,priority=?,updated_at=? WHERE id=?`,
		v.Name, boolInt(v.Enabled), v.Event, v.TitleTpl, v.BodyTpl, v.AssignMode, nullInt(v.AssignUser), nullInt(v.AssignRole), nullInt(v.DueHours), v.Priority, ts, v.ID); err != nil {
		writeErr(w, err, "保存规则失败：", "保存规则失败")
		return
	}
	v.UpdatedAt = ts
	a.Audit(r, "task_rule.update", "task_rule", v.ID, a.ruleJSON(v))
	core.WriteJSON(w, 200, core.Object{"rule": a.ruleJSON(v)})
}

func (a *Module) deleteRule(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	res, err := a.DB.Exec("DELETE FROM task_rules WHERE id=?", id)
	if err != nil {
		writeErr(w, err, "删除规则失败：", "删除规则失败")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		core.Fail(w, 404, "规则不存在")
		return
	}
	a.Audit(r, "task_rule.delete", "task_rule", id, core.Object{})
	core.WriteJSON(w, 200, core.Object{"ok": true})
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func nullInt(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func (a *Module) enabledRules(event string) ([]rule, error) {
	rows, err := a.DB.Query("SELECT "+ruleColumns+" FROM task_rules WHERE enabled=1 AND event=? ORDER BY id", event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rule
	for rows.Next() {
		v, e := scanRule(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
