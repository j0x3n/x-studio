package core

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	_ "time/tzdata"
)

func WithFeature(ctx context.Context, feature string) context.Context {
	v := Actor(ctx)
	v.Feature = feature
	return WithAIActor(ctx, v)
}
func MonthRange(month string) (string, string, error) {
	loc, e := time.LoadLocation("Asia/Shanghai")
	if e != nil {
		return "", "", fmt.Errorf("无法加载上海时区")
	}
	if month == "" {
		month = time.Now().In(loc).Format("2006-01")
	}
	start, e := time.ParseInLocation("2006-01", month, loc)
	if e != nil {
		return "", "", fmt.Errorf("月份必须是 YYYY-MM")
	}
	return start.UTC().Format(time.RFC3339), start.AddDate(0, 1, 0).UTC().Format(time.RFC3339), nil
}

// Reservations cover concurrent calls, including different listing jobs owned
// by the same user. Failed requests release their units without charging usage.
func (a *Core) beginUsage(ctx context.Context, kind, model string) (func(bool) error, error) {
	actor := Actor(ctx)
	if actor.UserID <= 0 {
		return nil, fmt.Errorf("AI 调用缺少触发用户")
	}
	u, e := a.User(actor.UserID)
	if e != nil || u.Disabled {
		return nil, fmt.Errorf("AI 调用用户不存在或已禁用")
	}
	start, end, e := MonthRange("")
	if e != nil {
		return nil, e
	}
	a.aiMu.Lock()
	defer a.aiMu.Unlock()
	col := "month_text"
	label := "文本"
	if kind == "image" {
		col = "month_images"
		label = "生图"
	}
	var quota sql.NullInt64
	e = a.DB.QueryRow("SELECT "+col+" FROM quotas WHERE user_id=?", actor.UserID).Scan(&quota)
	if e != nil && e != sql.ErrNoRows {
		return nil, fmt.Errorf("读取 AI 额度失败")
	}
	var used int64
	if e = a.DB.QueryRow("SELECT coalesce(sum(units),0) FROM ai_usage WHERE user_id=? AND kind=? AND julianday(at)>=julianday(?) AND julianday(at)<julianday(?)", actor.UserID, kind, start, end).Scan(&used); e != nil {
		return nil, fmt.Errorf("读取 AI 用量失败")
	}
	key := fmt.Sprintf("%d:%s:%s", actor.UserID, kind, start)
	if quota.Valid && used+int64(a.inflight[key])+1 > quota.Int64 {
		return nil, fmt.Errorf("本月 AI %s额度已用完，请找管理员", label)
	}
	a.inflight[key]++
	prices := a.Setting("ai_prices")
	price, _ := prices[model].(float64)
	return func(success bool) error {
		a.aiMu.Lock()
		defer a.aiMu.Unlock()
		defer func() {
			a.inflight[key]--
			if a.inflight[key] == 0 {
				delete(a.inflight, key)
			}
		}()
		if !success {
			return nil
		}
		var jid any
		if actor.JobID > 0 {
			jid = actor.JobID
		}
		_, e := a.DB.Exec("INSERT INTO ai_usage(at,user_id,feature,model,kind,units,est_cost,job_id) VALUES(?,?,?,?,?,1,?,?)", Now(), actor.UserID, actor.Feature, model, kind, price, jid)
		if e != nil {
			return fmt.Errorf("保存 AI 用量失败：%w", e)
		}
		return nil
	}, nil
}
func (a *Core) Model(feature, fallback string) string {
	if s, ok := a.Setting("ai_models")[feature].(string); ok && s != "" {
		return s
	}
	return fallback
}
