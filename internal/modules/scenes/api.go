package scenes

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

func (module *Module) Routes(request *core.Router) {
	request.Handle("GET /api/scenes", core.ProductView, module.list)
	request.Handle("POST /api/scenes", core.CreativeTemplate, module.create)
	request.Handle("GET /api/scenes/bindings", core.ProductView, module.bindingsGet)
	request.Handle("PUT /api/scenes/bindings", core.LoggedIn, module.bindingsPut)
	request.Handle("GET /api/scenes/{id}", core.ProductView, module.read)
	request.Handle("PATCH /api/scenes/{id}", core.CreativeTemplate, module.patch)
	request.Handle("DELETE /api/scenes/{id}", core.CreativeTemplate, module.remove)
	request.Handle("POST /api/scenes/{id}/refs", core.CreativeTemplate, module.upload)
	request.Handle("DELETE /api/scenes/{id}/refs/{ref}", core.CreativeTemplate, module.refDelete)
	request.Handle("GET /api/scenes/{id}/refs/{ref}/file", core.ProductView, module.refFile)
	request.Handle("POST /api/scenes/{id}/plates", core.CreativeTemplate, module.plateCreate)
	request.Handle("POST /api/scenes/{id}/plates/{plate}/lock", core.CreativeTemplate, module.plateLock)
	request.Handle("POST /api/scenes/{id}/plates/{plate}/discard", core.CreativeTemplate, module.plateDiscard)
	request.Handle("GET /api/scenes/{id}/plates/{plate}/file", core.ProductView, module.plateFile)
	request.Handle("GET /api/scenes/{id}/plates/{plate}/sketch", core.ProductView, module.plateSketch)
}
func (module *Module) visible(user core.User, scene Scene) bool {
	return scene.ShopID == nil || module.CanShop(user, *scene.ShopID)
}
func (module *Module) scoped(writer http.ResponseWriter, request *http.Request) (Scene, bool) {
	scene, err := get(request.Context(), module.DB, core.ID(request, "id"))
	if err != nil {
		fail(writer, err)
		return scene, false
	}
	if !module.visible(core.CurrentUser(request), scene) {
		core.Fail(writer, 404, "场景不存在")
		return scene, false
	}
	return scene, true
}
func (module *Module) enrich(scene *Scene, user core.User) error {
	rows, err := module.DB.Query("SELECT id,use,file,width,height FROM scene_refs WHERE scene_id=? ORDER BY id", scene.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var ref Reference
		if err = rows.Scan(&ref.ID, &ref.Use, &ref.File, &ref.Width, &ref.Height); err != nil {
			break
		}
		ref.URL = refURL(scene.ID, ref.ID)
		scene.Refs = append(scene.Refs, ref)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	query := `SELECT b.target_type,coalesce(l.shop_id,b.target_id) FROM scene_bindings b LEFT JOIN listings l ON b.target_type='listing' AND l.id=b.target_id WHERE b.scene_id=?`
	rows, err = module.DB.Query(query, scene.ID)
	if err != nil {
		return err
	}
	type binding struct {
		kind string
		shop int64
	}
	bindings := []binding{}
	for rows.Next() {
		var binding binding
		if err = rows.Scan(&binding.kind, &binding.shop); err != nil {
			break
		}
		bindings = append(bindings, binding)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if module.CanShop(user, binding.shop) {
			if binding.kind == "listing" {
				scene.Usage.Listings++
			} else {
				scene.Usage.Shops++
			}
		}
	}
	if err != nil {
		return err
	}
	return module.attachPlates(scene, user)
}
func (module *Module) read(writer http.ResponseWriter, request *http.Request) {
	scene, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	if err := module.enrich(&scene, core.CurrentUser(request)); err != nil {
		fail(writer, err)
		return
	}
	core.WriteJSON(writer, 200, core.Object{"scene": scene})
}
func (module *Module) list(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	shopID := int64(0)
	if raw := query.Get("shop_id"); raw != "" {
		var err error
		shopID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || shopID <= 0 {
			core.Fail(writer, 400, "店铺不正确")
			return
		}
		if !module.CanShop(core.CurrentUser(request), shopID) {
			core.Fail(writer, 404, "店铺不存在")
			return
		}
	}
	archived := query.Get("archived")
	if archived == "" {
		archived = "0"
	}
	if archived != "0" && archived != "1" {
		core.Fail(writer, 400, "归档条件不正确")
		return
	}
	rows, err := module.DB.QueryContext(request.Context(), "SELECT "+columns+" FROM scenes WHERE archived=? ORDER BY id", archived)
	if err != nil {
		fail(writer, err)
		return
	}
	items := []Scene{}
	for rows.Next() {
		scene, scanErr := scan(rows)
		if scanErr != nil {
			err = scanErr
			break
		}
		if shopID != 0 && scene.ShopID != nil && *scene.ShopID != shopID {
			continue
		}
		if query.Get("q") != "" && !strings.Contains(strings.ToLower(scene.Name+" "+scene.Note), strings.ToLower(query.Get("q"))) {
			continue
		}
		if category := query.Get("category"); category != "" && len(scene.Categories) > 0 && !contains(scene.Categories, category) {
			continue
		}
		items = append(items, scene)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		fail(writer, err)
		return
	}
	visible := []Scene{}
	for _, scene := range items {
		if !module.visible(core.CurrentUser(request), scene) {
			continue
		}
		if err = module.enrich(&scene, core.CurrentUser(request)); err != nil {
			fail(writer, err)
			return
		}
		visible = append(visible, scene)
	}
	core.WriteJSON(writer, 200, core.Object{"items": visible})
}
func contains(values []string, value string) bool {
	for _, part := range values {
		if part == value {
			return true
		}
	}
	return false
}
func (module *Module) validateInput(scene *Scene, user core.User) error {
	if err := validate(scene); err != nil {
		return err
	}
	if scene.ShopID != nil {
		var count int
		if *scene.ShopID <= 0 || !module.CanShop(user, *scene.ShopID) {
			return invalid(404, "店铺不存在")
		}
		if err := module.DB.QueryRow("SELECT count(*) FROM shops WHERE id=? AND archived=0", *scene.ShopID).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return invalid(404, "店铺不存在或已归档")
		}
	}
	for _, category := range scene.Categories {
		if _, err := module.CategoryByKey(category); err != nil {
			return invalid(400, "场景品类不存在："+category)
		}
	}
	return nil
}
func (module *Module) create(writer http.ResponseWriter, request *http.Request) {
	var scene Scene
	if !core.Decode(writer, request, &scene) {
		return
	}
	if err := module.validateInput(&scene, core.CurrentUser(request)); err != nil {
		fail(writer, err)
		return
	}
	if err := module.validateProps(core.CurrentUser(request), scene.Layout); err != nil {
		fail(writer, err)
		return
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(request.Context(), `INSERT INTO scenes(shop_id,name,categories,still,wear,light,props,note,camera,brief,layout,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, scene.ShopID, scene.Name, core.JS(scene.Categories), scene.Still, scene.Wear, scene.Light, scene.Props, scene.Note, scene.Camera, scene.Brief, canonicalLayout(scene.Layout), core.CurrentUser(request).ID, core.Now(), core.Now())
	if err == nil {
		scene.ID, err = result.LastInsertId()
	}
	if err == nil {
		err = core.AuditTx(tx, request, "scene.create", "scene", scene.ID, scene)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(writer, err)
		return
	}
	scene, err = get(request.Context(), module.DB, scene.ID)
	if err != nil {
		fail(writer, err)
		return
	}
	core.WriteJSON(writer, 201, core.Object{"scene": scene})
}
func (module *Module) patch(writer http.ResponseWriter, request *http.Request) {
	scene, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	old := scene
	var fields map[string]json.RawMessage
	if !core.Decode(writer, request, &fields) {
		return
	}
	for key := range fields {
		if !contains([]string{"shop_id", "name", "categories", "still", "wear", "light", "props", "note", "archived", "camera", "brief", "layout"}, key) {
			core.Fail(writer, 400, "不能修改这个场景字段："+key)
			return
		}
		if key != "shop_id" && string(fields[key]) == "null" {
			core.Fail(writer, 400, "场景字段不能为空值")
			return
		}
	}
	if err := json.Unmarshal([]byte(core.JS(fields)), &scene); err != nil {
		core.Fail(writer, 400, "场景字段格式不正确")
		return
	}
	if err := module.validateInput(&scene, core.CurrentUser(request)); err != nil {
		fail(writer, err)
		return
	}
	if _, ok := fields["layout"]; ok {
		if err := module.validateProps(core.CurrentUser(request), scene.Layout); err != nil {
			fail(writer, err)
			return
		}
	}
	if (old.ShopID == nil) != (scene.ShopID == nil) || (old.ShopID != nil && scene.ShopID != nil && *old.ShopID != *scene.ShopID) {
		var count int
		err := module.DB.QueryRow("SELECT count(*) FROM scene_bindings WHERE scene_id=?", scene.ID).Scan(&count)
		if err != nil {
			fail(writer, err)
			return
		}
		if count > 0 || scene.IsDefault {
			core.Fail(writer, 409, "场景正在使用，先清除绑定再修改所属店铺")
			return
		}
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(request.Context(), `UPDATE scenes SET shop_id=?,name=?,categories=?,still=?,wear=?,light=?,props=?,note=?,camera=?,brief=?,layout=?,archived=?,updated_at=? WHERE id=?`, scene.ShopID, scene.Name, core.JS(scene.Categories), scene.Still, scene.Wear, scene.Light, scene.Props, scene.Note, scene.Camera, scene.Brief, canonicalLayout(scene.Layout), scene.Archived, core.Now(), scene.ID)
	action := "scene.update"
	if old.Archived != scene.Archived {
		action = "scene.archive"
	}
	if err == nil {
		err = core.AuditTx(tx, request, action, "scene", scene.ID, fields)
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
	scene, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	if !scene.Archived {
		core.Fail(writer, 409, "先归档再删除")
		return
	}
	if module.Active("scene", scene.ID) {
		core.Fail(writer, 409, "场景正在生成定场图，等任务结束再删除")
		return
	}
	if scene.IsDefault {
		core.Fail(writer, 409, "兜底场景不能删除")
		return
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	for _, query := range []string{"DELETE FROM scene_plates WHERE scene_id=?", "DELETE FROM scene_bindings WHERE scene_id=?", "DELETE FROM scene_refs WHERE scene_id=?", "DELETE FROM scenes WHERE id=?"} {
		if _, err = tx.ExecContext(request.Context(), query, scene.ID); err != nil {
			fail(writer, err)
			return
		}
	}
	if err = core.AuditTx(tx, request, "scene.delete", "scene", scene.ID, core.Object{"name": scene.Name}); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(writer, err)
		return
	}
	root, err := module.fileRoot()
	if err == nil {
		defer root.Close()
		err = root.RemoveAll(strconv.FormatInt(scene.ID, 10))
	}
	if err != nil && !os.IsNotExist(err) {
		fail(writer, err)
		return
	}
	core.WriteJSON(writer, 200, core.Object{"ok": true})
}
