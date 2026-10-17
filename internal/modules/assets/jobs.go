package assets

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"xstudio/internal/core"
)

func (a *Module) lockAI(uid int64) func() {
	v, _ := a.aiUsers.LoadOrStore(uid, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
func (a *Module) checkQuota(uid int64, images, text int) error {
	start, end, err := core.MonthRange("")
	if err != nil {
		return err
	}
	for _, v := range []struct {
		kind, col, label string
		count            int
	}{{"image", "month_images", "生图", images}, {"text", "month_text", "文本", text}} {
		if v.count == 0 {
			continue
		}
		var limit sql.NullInt64
		err = a.DB.QueryRow("SELECT "+v.col+" FROM quotas WHERE user_id=?", uid).Scan(&limit)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("读取 AI 额度失败")
		}
		if !limit.Valid {
			continue
		}
		var used int
		if err = a.DB.QueryRow("SELECT coalesce(sum(units),0) FROM ai_usage WHERE user_id=? AND kind=? AND julianday(at)>=julianday(?) AND julianday(at)<julianday(?)", uid, v.kind, start, end).Scan(&used); err != nil {
			return fmt.Errorf("读取 AI 用量失败")
		}
		if used+v.count > int(limit.Int64) {
			return fmt.Errorf("本月 AI %s额度不够完成整批处理，请找管理员", v.label)
		}
	}
	return nil
}
func (a *Module) prompt(name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(a.Cfg.PromptDir, name))
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return "", fmt.Errorf("缺少提示词 %s", name)
	}
	return string(b), nil
}
func (a *Module) enqueue(kind string, pid, uid int64, params any) (core.Job, error) {
	res, err := a.DB.Exec("INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,steps,created_at,result) VALUES(?,'product',?,?,?,'queued',?,?,?)", kind, pid, uid, js(params), kind, now(), js(Object{"items": []Object{}}))
	if err != nil {
		return core.Job{}, fmt.Errorf("此商品已有排队或运行中的任务")
	}
	id, _ := res.LastInsertId()
	j, err := core.ScanJob(a.DB.QueryRow("SELECT "+core.JobColumns+" FROM jobs WHERE id=?", id))
	a.Wake()
	return j, err
}
func (a *Module) saveResults(j core.Job, items []Object) error {
	_, err := a.DB.Exec("UPDATE jobs SET result=?,progress=100,message='处理完成' WHERE id=?", js(Object{"items": items}), j.ID)
	return err
}
func jobSelection(j core.Job) ([]int64, error) {
	var in selection
	if err := decodeObject(j.Params, &in); err != nil {
		return nil, err
	}
	return in.AssetIDs, validIDs(in.AssetIDs)
}
func (a *Module) jobAssets(ctx context.Context, j core.Job, ids []int64, perm string) (core.User, []Asset, error) {
	u, err := a.User(j.UserID)
	if err != nil || u.Disabled || !u.Has(perm) {
		return u, nil, fmt.Errorf("任务发起人已停用或没有处理权限")
	}
	items, err := a.selected(u, ids)
	if err != nil {
		return u, nil, err
	}
	for _, p := range items {
		if p.ProductID != j.TargetID {
			return u, nil, fmt.Errorf("任务素材与目标商品不一致")
		}
		if u.Role.Scope == "shares" && !a.CanUpload(u, p.ProductID) {
			return u, nil, fmt.Errorf("分享已取消或不允许上传")
		}
	}
	return u, items, nil
}
