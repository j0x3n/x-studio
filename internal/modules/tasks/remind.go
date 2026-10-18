package tasks

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type dueRow struct {
	id       int64
	title    string
	due      string
	target   string
	assignee int64
	targetID int64
}

func (a *Module) remindDue() error {
	rows, err := a.DB.Query("SELECT id, title, assignee_id, due_at, target_type, target_id FROM tasks WHERE status IN ('todo','doing','waiting') AND assignee_id IS NOT NULL AND due_at IS NOT NULL")
	if err != nil {
		return err
	}
	var list []dueRow
	for rows.Next() {
		var v dueRow
		var target sql.NullString
		var tid sql.NullInt64
		if err = rows.Scan(&v.id, &v.title, &v.assignee, &v.due, &target, &tid); err != nil {
			rows.Close()
			return err
		}
		if target.Valid {
			v.target = target.String
		}
		if tid.Valid {
			v.targetID = tid.Int64
		}
		list = append(list, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	now := a.now()
	limit := now.Add(24 * time.Hour)
	var notes []note
	for _, t := range list {
		if t.assignee <= 0 {
			continue
		}
		due, ok := parseTS(t.due)
		if !ok {
			continue
		}
		kind, key, title := "", "", ""
		if !due.Before(now) && !due.After(limit) {
			kind = "task_due"
			key = fmt.Sprintf("due:%d", t.id)
			title = "即将到期：" + t.title
		} else if due.Before(now) {
			kind = "task_overdue"
			key = fmt.Sprintf("overdue:%d", t.id)
			title = "已逾期：" + t.title
		} else {
			continue
		}
		notes = append(notes, note{TaskID: t.id, UserID: t.assignee, Type: kind, Title: clip(title, 100), Link: "/tasks/" + fmt.Sprint(t.id), TargetType: t.target, TargetID: t.targetID, OnceKey: key})
	}
	return a.commitPlans(nil, notes, nil)
}

func (a *Module) remindQuota() error {
	loc := shanghai()
	local := a.now().In(loc)
	month := local.Format("2006-01")
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, 0)
	rows, err := a.DB.Query("SELECT id FROM users WHERE disabled=0 ORDER BY id")
	if err != nil {
		return err
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
		return err
	}
	var notes []note
	for _, uid := range ids {
		for _, kind := range []string{"image", "text"} {
			col := "month_text"
			label := "文本"
			if kind == "image" {
				col = "month_images"
				label = "生图"
			}
			var quota sql.NullInt64
			err = a.DB.QueryRow("SELECT "+col+" FROM quotas WHERE user_id=?", uid).Scan(&quota)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if !quota.Valid || quota.Int64 <= 0 {
				continue
			}
			var used int64
			err = a.DB.QueryRow("SELECT coalesce(sum(units),0) FROM ai_usage WHERE user_id=? AND kind=? AND julianday(substr(replace(replace(at,'T',' '),'Z',''),1,19))>=julianday(?) AND julianday(substr(replace(replace(at,'T',' '),'Z',''),1,19))<julianday(?)", uid, kind, sqlTime(start), sqlTime(end)).Scan(&used)
			if err != nil {
				return err
			}
			if used*5 < quota.Int64*4 {
				continue
			}
			notes = append(notes, note{
				UserID:  uid,
				Type:    "quota_80",
				Title:   "本月" + label + "额度已用到 80%",
				Body:    fmt.Sprintf("已用 %d / %d", used, quota.Int64),
				Link:    "/settings",
				OnceKey: fmt.Sprintf("quota:%d:%s:%s", uid, kind, month),
			})
		}
	}
	return a.commitPlans(nil, notes, nil)
}
