package core

import (
	"context"
	"fmt"
	"time"
)

type aiActorKey struct{}
type AIActor struct {
	UserID, JobID int64
	Feature       string
}

func WithAIActor(ctx context.Context, actor AIActor) context.Context {
	return context.WithValue(ctx, aiActorKey{}, actor)
}
func Actor(ctx context.Context) AIActor { v, _ := ctx.Value(aiActorKey{}).(AIActor); return v }
func (a *Core) Wake() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}
func (a *Core) claim() *Job {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	j, e := ScanJob(a.DB.QueryRow("SELECT " + JobColumns + " FROM jobs WHERE status='queued' ORDER BY id LIMIT 1"))
	if e != nil {
		return nil
	}
	res, e := a.DB.Exec("UPDATE jobs SET status='running',message='开始处理' WHERE id=? AND status='queued'", j.ID)
	if e != nil {
		return nil
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil
	}
	j.Status = "running"
	return &j
}
func (a *Core) worker() {
	defer a.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if a.ctx.Err() != nil {
			return
		}
		if a.BaseURL() != "" {
			if j := a.claim(); j != nil {
				a.execute(*j)
				continue
			}
		}
		select {
		case <-a.ctx.Done():
			return
		case <-a.wake:
		case <-ticker.C:
		}
	}
}
func (a *Core) execute(j Job) {
	var err error
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("任务处理异常")
		}
		if a.ctx.Err() != nil {
			err = fmt.Errorf("服务重启中断")
		}
		if err != nil {
			msg := a.SafeAIError(err.Error())
			_, _ = a.DB.Exec("UPDATE jobs SET status='failed',message=?,finished_at=? WHERE id=?", msg, Now(), j.ID)
			if j.TargetType == "listing" {
				_, _ = a.DB.Exec("UPDATE listings SET pipeline_status='failed',pipeline_error=?,updated_at=? WHERE id=?", msg, Now(), j.TargetID)
			}
		} else {
			_, _ = a.DB.Exec("UPDATE jobs SET status='done',progress=100,finished_at=? WHERE id=?", Now(), j.ID)
		}
	}()
	h, ok := a.jobHandlers[j.Kind]
	if !ok {
		err = fmt.Errorf("任务类型未注册：%s", j.Kind)
		return
	}
	err = h(WithAIActor(a.ctx, AIActor{UserID: j.UserID, JobID: j.ID}), j)
}
