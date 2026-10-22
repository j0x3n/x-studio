package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"xstudio/internal/core"
)

var steps = []string{"check", "classify", "enhance", "retouch", "copy", "render"}

func (a *Module) run(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := idParam(r, "id")
	if !a.RequireListing(w, r, id, true) {
		return
	}
	q := r.URL.Query()
	step, from := q.Get("step"), q.Get("from")
	if step != "" && from != "" {
		fail(w, 400, "step 和 from 不能同时指定")
		return
	}
	selected := steps
	target := step
	if target == "" {
		target = from
	}
	if target != "" {
		index := -1
		for i, s := range steps {
			if s == target {
				index = i
			}
		}
		if index < 0 {
			fail(w, 400, "未知处理步骤")
			return
		}
		if step != "" {
			selected = []string{step}
		} else {
			selected = steps[index:]
		}
	}
	// check/classify/enhance/retouch share product files. Serialize these across
	// shops while allowing copy/render for separate listings to run concurrently.
	p, e := a.PipelineProduct(id)
	if e != nil {
		fail(w, 404, "上架版本不存在")
		return
	}
	if a.ActiveProduct(p.ID) {
		var n int
		_ = a.DB.QueryRow("SELECT count(*) FROM jobs j JOIN listings l ON j.target_id=l.id WHERE j.target_type='listing' AND l.product_id=? AND j.status IN ('queued','running') AND (instr(j.steps,'check')>0 OR instr(j.steps,'classify')>0 OR instr(j.steps,'enhance')>0 OR instr(j.steps,'retouch')>0)", p.ID).Scan(&n)
		shared := false
		for _, s := range selected {
			if s == "check" || s == "classify" || s == "enhance" || s == "retouch" {
				shared = true
			}
		}
		if n > 0 || shared {
			fail(w, 409, "此商品已有使用共用素材的任务")
			return
		}
	}
	tx, e := a.DB.Begin()
	if e != nil {
		fail(w, 500, "创建任务失败")
		return
	}
	defer tx.Rollback()
	res, e := tx.Exec("INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,created_at,steps) VALUES('pipeline','listing',?,?,?,'queued',?,?)", id, currentUser(r).ID, js(Object{}), now(), strings.Join(selected, ","))
	if e != nil {
		fail(w, 409, "此上架版本已有正在排队或运行的任务")
		return
	}
	jid, _ := res.LastInsertId()
	_, e = tx.Exec("UPDATE listings SET pipeline_status='processing',pipeline_error='',updated_at=? WHERE id=?", now(), id)
	if e != nil || tx.Commit() != nil {
		fail(w, 500, "保存任务失败")
		return
	}
	j, _ := scanJob(a.DB.QueryRow("SELECT "+core.JobColumns+" FROM jobs WHERE id=?", jid))
	a.Wake()
	writeJSON(w, 202, Object{"job": j})
}
func (a *Module) execute(ctx context.Context, j Job) error {
	warnings := []string{}
	message := func(s string) string {
		if len(warnings) > 0 {
			return s + "；" + strings.Join(warnings, "；")
		}
		return s
	}
	selected := strings.Split(j.Steps, ",")
	for i, step := range selected {
		if ctx.Err() != nil {
			return fmt.Errorf("服务重启中断")
		}
		_, e := a.DB.Exec("UPDATE jobs SET progress=?,message=? WHERE id=?", i*100/len(selected), message("正在执行："+step), j.ID)
		if e != nil {
			return e
		}
		if _, e = a.DB.Exec("UPDATE listings SET pipeline_step=?,updated_at=? WHERE id=?", step, now(), j.TargetID); e != nil {
			return e
		}
		p, e := a.PipelineProduct(j.TargetID)
		if e != nil {
			return e
		}
		switch step {
		case "check":
			e = a.check(ctx, p)
		case "classify":
			e = a.classify(core.WithFeature(ctx, "classify"), p)
		case "enhance":
			var notes []string
			notes, e = a.enhance(ctx, p)
			warnings = append(warnings, notes...)
		case "retouch":
			e = a.retouch(ctx, p)
		case "copy":
			e = a.copyText(core.WithFeature(ctx, "copy"), p)
		case "render":
			var notes []string
			notes, e = a.render(ctx, p)
			warnings = append(warnings, notes...)
		default:
			e = fmt.Errorf("未知任务步骤")
		}
		if e != nil {
			return fmt.Errorf("%s 步骤失败：%s", step, message(e.Error()))
		}
		_, e = a.DB.Exec("UPDATE jobs SET progress=?,message=? WHERE id=?", (i+1)*100/len(selected), message("已完成："+step), j.ID)
		if e != nil {
			return e
		}
	}
	status := "draft"
	if selected[len(selected)-1] == "render" {
		status = "ready"
	}
	if _, e := a.DB.Exec("UPDATE listings SET pipeline_status=?,status=?,pipeline_error='',updated_at=? WHERE id=?", status, status, now(), j.TargetID); e != nil {
		return e
	}
	_, e := a.DB.Exec("UPDATE jobs SET message=?,result=? WHERE id=?", message("处理完成"), js(Object{}), j.ID)
	return e
}
func (a *Module) getJob(w http.ResponseWriter, r *http.Request) {
	j, e := scanJob(a.DB.QueryRow("SELECT "+core.JobColumns+" FROM jobs WHERE id=?", idParam(r, "jid")))
	if e != nil {
		fail(w, 404, "任务不存在")
		return
	}
	u := currentUser(r)
	visible := false
	switch j.TargetType {
	case "user":
		visible = j.TargetID == u.ID
	case "listing":
		visible = a.CanListing(u, j.TargetID)
	case "creative_set", "creative_item":
		var listingID int64
		query := "SELECT listing_id FROM creative_sets WHERE id=?"
		if j.TargetType == "creative_item" {
			query = "SELECT COALESCE(i.listing_id,s.listing_id) FROM creative_items i LEFT JOIN creative_sets s ON s.id=i.set_id WHERE i.id=?"
		}
		if a.DB.QueryRow(query, j.TargetID).Scan(&listingID) == nil {
			visible = a.CanListing(u, listingID)
		}
	case "product":
		visible = a.CanProduct(u, j.TargetID)
	case "asset":
		var pid int64
		if a.DB.QueryRow("SELECT product_id FROM assets WHERE id=?", j.TargetID).Scan(&pid) == nil {
			visible = a.CanProduct(u, pid)
		}
	case "scene", "prop":
		// 定场图、道具生成：没有店铺的大家都能看，限定店铺的要有这个店的权限
		table := map[string]string{"scene": "scenes", "prop": "props"}[j.TargetType]
		var shop sql.NullInt64
		if a.DB.QueryRow("SELECT shop_id FROM "+table+" WHERE id=?", j.TargetID).Scan(&shop) == nil {
			visible = !shop.Valid || a.CanShop(u, shop.Int64)
		}
	}
	if !visible {
		fail(w, 404, "任务不存在")
		return
	}
	writeJSON(w, 200, Object{"job": j})
}
