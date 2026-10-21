package props

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

func (module *Module) Routes(router *core.Router) {
	router.Handle("GET /api/props", core.ProductView, module.list)
	router.Handle("POST /api/props", core.CreativeTemplate, module.create)
	router.Handle("GET /api/props/{id}", core.ProductView, module.read)
	router.Handle("PATCH /api/props/{id}", core.CreativeTemplate, module.patch)
	router.Handle("DELETE /api/props/{id}", core.CreativeTemplate, module.remove)
	router.Handle("POST /api/props/{id}/refs", core.CreativeTemplate, module.upload)
	router.Handle("POST /api/props/{id}/refs/{ref}/lock", core.CreativeTemplate, module.lock)
	router.Handle("POST /api/props/{id}/refs/{ref}/discard", core.CreativeTemplate, module.discard)
	router.Handle("GET /api/props/{id}/refs/{ref}/file", core.ProductView, module.refFile)
	router.Handle("POST /api/props/{id}/generate", core.CreativeTemplate, module.generate)
	router.Handle("POST /api/props/{id}/views", core.CreativeTemplate, module.views)
}
func (module *Module) decorate(prop *Prop, withRefs bool) error {
	rows, err := module.DB.Query("SELECT view,count(*) FROM prop_refs WHERE prop_id=? AND status='locked' GROUP BY view", prop.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var view string
		var count int
		if err = rows.Scan(&view, &count); err != nil {
			break
		}
		prop.LockedViews[view] = count
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil || !withRefs {
		return err
	}
	rows, err = module.DB.Query("SELECT id,view,source,status,file,width,height,prompt,model,created_at FROM prop_refs WHERE prop_id=? ORDER BY id", prop.ID)
	if err != nil {
		return err
	}
	prop.Refs = []Ref{}
	for rows.Next() {
		var ref Ref
		if err = rows.Scan(&ref.ID, &ref.View, &ref.Source, &ref.Status, &ref.File, &ref.Width, &ref.Height, &ref.Prompt, &ref.Model, &ref.CreatedAt); err != nil {
			break
		}
		ref.URL = refURL(prop.ID, ref.ID)
		prop.Refs = append(prop.Refs, ref)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	return err
}
func (module *Module) read(writer http.ResponseWriter, request *http.Request) {
	prop, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	if err := module.decorate(&prop, true); err != nil {
		fail(writer, err)
		return
	}
	core.WriteJSON(writer, 200, core.Object{"prop": prop})
}
func (module *Module) list(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	archived := query.Get("archived")
	if archived == "" {
		archived = "0"
	}
	if archived != "0" && archived != "1" {
		core.Fail(writer, 400, "归档条件不正确")
		return
	}
	kind := query.Get("kind")
	if kind != "" && kindName(kind) == "" {
		core.Fail(writer, 400, "道具种类不正确")
		return
	}
	sqlText := "SELECT " + propColumns + " FROM props WHERE archived=?"
	args := []any{archived}
	if kind != "" {
		sqlText += " AND kind=?"
		args = append(args, kind)
	}
	rows, err := module.DB.QueryContext(request.Context(), sqlText+" ORDER BY id", args...)
	if err != nil {
		fail(writer, err)
		return
	}
	items := []Prop{}
	for rows.Next() {
		prop, scanErr := scanProp(rows)
		if scanErr != nil {
			err = scanErr
			break
		}
		if query.Get("q") != "" && !strings.Contains(strings.ToLower(prop.Name+" "+prop.Description), strings.ToLower(query.Get("q"))) {
			continue
		}
		items = append(items, prop)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		fail(writer, err)
		return
	}
	visible := []Prop{}
	user := core.CurrentUser(request)
	for _, prop := range items {
		if !module.visible(user, prop) {
			continue
		}
		if err = module.decorate(&prop, false); err != nil {
			fail(writer, err)
			return
		}
		visible = append(visible, prop)
	}
	core.WriteJSON(writer, 200, core.Object{"items": visible})
}
func (module *Module) create(writer http.ResponseWriter, request *http.Request) {
	var prop Prop
	if !core.Decode(writer, request, &prop) {
		return
	}
	prop.Archived = false
	prop.CoverRefID = nil
	if err := module.validateProp(&prop, core.CurrentUser(request)); err != nil {
		fail(writer, err)
		return
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(request.Context(), `INSERT INTO props(shop_id,name,kind,description,size_cm,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, prop.ShopID, prop.Name, prop.Kind, prop.Description, core.JS(prop.SizeCM), core.CurrentUser(request).ID, core.Now(), core.Now())
	if err == nil {
		prop.ID, err = result.LastInsertId()
	}
	if err == nil {
		err = core.AuditTx(tx, request, "prop.create", "prop", prop.ID, prop)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(writer, err)
		return
	}
	prop, err = get(request.Context(), module.DB, prop.ID)
	if err != nil {
		fail(writer, err)
		return
	}
	core.WriteJSON(writer, 201, core.Object{"prop": prop})
}
func (module *Module) patch(writer http.ResponseWriter, request *http.Request) {
	prop, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	old := prop
	var fields map[string]json.RawMessage
	if !core.Decode(writer, request, &fields) {
		return
	}
	for key := range fields {
		if !contains([]string{"shop_id", "name", "kind", "description", "size_cm", "archived"}, key) {
			core.Fail(writer, 400, "不能修改这个道具字段："+key)
			return
		}
		if key != "shop_id" && string(fields[key]) == "null" {
			core.Fail(writer, 400, "道具字段不能为空值")
			return
		}
	}
	if err := json.Unmarshal([]byte(core.JS(fields)), &prop); err != nil {
		core.Fail(writer, 400, "道具字段格式不正确")
		return
	}
	if err := module.validateProp(&prop, core.CurrentUser(request)); err != nil {
		fail(writer, err)
		return
	}
	if shopChanged(old.ShopID, prop.ShopID) {
		used, err := module.propReferenced(request.Context(), prop.ID)
		if err != nil {
			fail(writer, err)
			return
		}
		if used {
			core.Fail(writer, 409, "道具正在被场景使用，不能改所属店铺")
			return
		}
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(request.Context(), `UPDATE props SET shop_id=?,name=?,kind=?,description=?,size_cm=?,archived=?,updated_at=? WHERE id=?`, prop.ShopID, prop.Name, prop.Kind, prop.Description, core.JS(prop.SizeCM), prop.Archived, core.Now(), prop.ID)
	action := "prop.update"
	if old.Archived != prop.Archived {
		action = "prop.archive"
	}
	if err == nil {
		err = core.AuditTx(tx, request, action, "prop", prop.ID, fields)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(writer, err)
		return
	}
	module.read(writer, request)
}
func (module *Module) remove(writer http.ResponseWriter, request *http.Request) {
	prop, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	if module.Active("prop", prop.ID) {
		core.Fail(writer, 409, "这个道具正在生成，等任务结束再删除")
		return
	}
	used, err := module.propReferenced(request.Context(), prop.ID)
	if err != nil {
		fail(writer, err)
		return
	}
	if used {
		if !prop.Archived {
			tx, txErr := module.DB.BeginTx(request.Context(), nil)
			if txErr != nil {
				fail(writer, txErr)
				return
			}
			defer tx.Rollback()
			if _, err = tx.ExecContext(request.Context(), "UPDATE props SET archived=1,updated_at=? WHERE id=?", core.Now(), prop.ID); err != nil {
				fail(writer, err)
				return
			}
			if err = core.AuditTx(tx, request, "prop.archive", "prop", prop.ID, core.Object{"name": prop.Name, "reason": "scene"}); err != nil {
				fail(writer, err)
				return
			}
			if err = tx.Commit(); err != nil {
				fail(writer, err)
				return
			}
		}
		prop, err = get(request.Context(), module.DB, prop.ID)
		if err != nil {
			fail(writer, err)
			return
		}
		_ = module.decorate(&prop, false)
		core.WriteJSON(writer, 200, core.Object{"ok": true, "archived": true, "prop": prop})
		return
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(request.Context(), "SELECT file FROM prop_refs WHERE prop_id=?", prop.ID)
	if err != nil {
		fail(writer, err)
		return
	}
	files := []string{}
	for rows.Next() {
		var file string
		if err = rows.Scan(&file); err != nil {
			break
		}
		files = append(files, file)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		fail(writer, err)
		return
	}
	if _, err = tx.ExecContext(request.Context(), "DELETE FROM props WHERE id=?", prop.ID); err != nil {
		fail(writer, err)
		return
	}
	if err = core.AuditTx(tx, request, "prop.delete", "prop", prop.ID, core.Object{"name": prop.Name}); err != nil {
		fail(writer, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(writer, err)
		return
	}
	for _, file := range files {
		_ = module.removeFile(file)
	}
	root, err := module.fileRoot()
	if err == nil {
		defer root.Close()
		err = root.RemoveAll(strconv.FormatInt(prop.ID, 10))
	}
	if err != nil && !os.IsNotExist(err) {
		fail(writer, err)
		return
	}
	core.WriteJSON(writer, 200, core.Object{"ok": true, "deleted": true})
}
func contains(values []string, value string) bool {
	for _, part := range values {
		if part == value {
			return true
		}
	}
	return false
}
func (module *Module) lock(writer http.ResponseWriter, request *http.Request) {
	module.setRefStatus(writer, request, true)
}
func (module *Module) discard(writer http.ResponseWriter, request *http.Request) {
	module.setRefStatus(writer, request, false)
}
func (module *Module) setRefStatus(writer http.ResponseWriter, request *http.Request, lock bool) {
	prop, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	ref, err := module.reference(request, prop.ID)
	if err != nil {
		fail(writer, err)
		return
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	if lock {
		err = module.promote(tx, request, prop.ID, ref)
	} else {
		if ref.Status == "discarded" {
			core.Fail(writer, 409, "这张参考图已经丢弃")
			return
		}
		_, err = tx.ExecContext(request.Context(), "UPDATE prop_refs SET status='discarded' WHERE id=? AND prop_id=?", ref.ID, prop.ID)
		if err == nil {
			_, err = tx.ExecContext(request.Context(), "UPDATE props SET cover_ref_id=NULL,updated_at=? WHERE id=? AND cover_ref_id=?", core.Now(), prop.ID, ref.ID)
		}
		if err == nil {
			err = core.AuditTx(tx, request, "prop.ref_discard", "prop", prop.ID, core.Object{"ref_id": ref.ID, "view": ref.View})
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(writer, err)
		return
	}
	module.read(writer, request)
}
func (module *Module) promote(tx *sql.Tx, request *http.Request, propID int64, ref Ref) error {
	if _, err := tx.ExecContext(request.Context(), "UPDATE prop_refs SET status='discarded' WHERE prop_id=? AND view=? AND status='locked' AND id<>?", propID, ref.View, ref.ID); err != nil {
		return err
	}
	result, err := tx.ExecContext(request.Context(), "UPDATE prop_refs SET status='locked' WHERE id=? AND prop_id=?", ref.ID, propID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return invalid(404, "参考图不存在")
	}
	if ref.View == "main" {
		if _, err = tx.ExecContext(request.Context(), "UPDATE props SET cover_ref_id=?,updated_at=? WHERE id=?", ref.ID, core.Now(), propID); err != nil {
			return err
		}
	}
	return core.AuditTx(tx, request, "prop.ref_lock", "prop", propID, core.Object{"ref_id": ref.ID, "view": ref.View})
}
