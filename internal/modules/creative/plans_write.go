package creative

import (
	"net/http"
	"xstudio/internal/core"
)

func (a *Module) savePlan(w http.ResponseWriter, r *http.Request, p Plan, creating bool) {
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	args := []any{p.Name, p.Note, core.JS(p.Categories), core.JS(p.TemplateSet), core.JS(p.Models), p.SceneID, p.DetailLayout, p.Archived, core.Now()}
	action, status := "creative.plan.update", 200
	if creating {
		action, status = "creative.plan.create", 201
		args = append(args, core.CurrentUser(r).ID, core.Now())
		res, e := tx.ExecContext(r.Context(), `INSERT INTO creative_plans(name,note,categories,template_set,models,scene_id,detail_layout,archived,updated_at,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, args...)
		err = e
		if err == nil {
			p.ID, err = res.LastInsertId()
		}
	} else {
		args = append(args, p.ID)
		_, err = tx.ExecContext(r.Context(), `UPDATE creative_plans SET name=?,note=?,categories=?,template_set=?,models=?,scene_id=?,detail_layout=?,archived=?,updated_at=? WHERE id=?`, args...)
	}
	if err == nil {
		err = core.AuditTx(tx, r, action, "creative_plan", p.ID, p)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(w, err)
		return
	}
	stored, err := a.plan(r.Context(), p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	core.WriteJSON(w, status, core.Object{"plan": stored})
}

func (a *Module) planDelete(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	p, err := a.plan(r.Context(), id)
	if err != nil {
		core.Fail(w, 404, "方案不存在")
		return
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM products WHERE plan_id=?", id).Scan(&count); err != nil {
		fail(w, err)
		return
	}
	action := "creative.plan.delete"
	if count > 0 {
		action = "creative.plan.archive"
		_, err = tx.ExecContext(r.Context(), "UPDATE creative_plans SET archived=1,updated_at=? WHERE id=?", core.Now(), id)
	} else {
		_, err = tx.ExecContext(r.Context(), "DELETE FROM creative_plans WHERE id=?", id)
	}
	if err == nil {
		err = core.AuditTx(tx, r, action, "creative_plan", id, core.Object{"name": p.Name})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(w, err)
		return
	}
	core.WriteJSON(w, 200, core.Object{"archived": count > 0, "deleted": count == 0})
}
