package scenes

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

func (module *Module) bindingsGet(writer http.ResponseWriter, request *http.Request) {
	ids := []int64{}
	if raw := request.URL.Query().Get("listing_ids"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || id <= 0 {
				core.Fail(writer, 400, "上架版本编号不正确")
				return
			}
			ids = append(ids, id)
		}
	}
	shopIDs := []int64{}
	if raw := request.URL.Query().Get("shop_ids"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || id <= 0 {
				core.Fail(writer, 400, "店铺编号不正确")
				return
			}
			shopIDs = append(shopIDs, id)
		}
	}
	if len(ids) > 200 || len(shopIDs) > 200 {
		core.Fail(writer, 400, "一次最多查询 200 个上架版本或店铺")
		return
	}
	listings := []core.Object{}
	shops := []core.Object{}
	seen := map[int64]bool{}
	// 店铺默认场景：listing 所在的店和 shop_ids 里的店各查一次
	addShop := func(shopID int64) error {
		if seen[shopID] {
			return nil
		}
		seen[shopID] = true
		var boundID int64
		var name string
		err := module.DB.QueryRowContext(request.Context(), `SELECT s.id,s.name FROM scene_bindings b JOIN scenes s ON s.id=b.scene_id WHERE b.target_type='shop' AND b.target_id=?`, shopID).Scan(&boundID, &name)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err == nil {
			shops = append(shops, core.Object{"shop_id": shopID, "scene_id": boundID, "scene_name": name})
		}
		return err
	}
	for _, id := range ids {
		shopID, _, err := listing(request.Context(), module.DB, id)
		if err != nil || !module.CanShop(core.CurrentUser(request), shopID) {
			core.Fail(writer, 404, "上架版本不存在")
			return
		}
		snapshot, source, err := resolve(request.Context(), module.DB, id, 0)
		if err != nil {
			fail(writer, err)
			return
		}
		var sceneID any
		if snapshot.ID != 0 {
			sceneID = snapshot.ID
		}
		listings = append(listings, core.Object{"listing_id": id, "scene_id": sceneID, "source": source, "scene_name": snapshot.Name})
		if err = addShop(shopID); err != nil {
			fail(writer, err)
			return
		}
	}
	for _, shopID := range shopIDs {
		if !module.CanShop(core.CurrentUser(request), shopID) {
			core.Fail(writer, 404, "店铺不存在")
			return
		}
		if err := addShop(shopID); err != nil {
			fail(writer, err)
			return
		}
	}
	core.WriteJSON(writer, 200, core.Object{"listings": listings, "shops": shops})
}
func (module *Module) bindingsPut(writer http.ResponseWriter, request *http.Request) {
	var in struct {
		ListingIDs []int64 `json:"listing_ids"`
		ShopID     *int64  `json:"shop_id"`
		SceneID    *int64  `json:"scene_id"`
	}
	if !core.Decode(writer, request, &in) {
		return
	}
	if (in.ShopID != nil) == (len(in.ListingIDs) > 0) || len(in.ListingIDs) > 200 {
		core.Fail(writer, 400, "请选择一个店铺，或 1–200 个上架版本")
		return
	}
	user := core.CurrentUser(request)
	permission := core.CreativeRun
	kind := "listing"
	ids := in.ListingIDs
	if in.ShopID != nil {
		permission = core.CreativeTemplate
		kind = "shop"
		ids = []int64{*in.ShopID}
	}
	if !user.Has(permission) {
		core.Fail(writer, 403, "没有设置场景的权限")
		return
	}
	var scene Scene
	if in.SceneID != nil {
		var err error
		scene, err = get(request.Context(), module.DB, *in.SceneID)
		if err != nil {
			fail(writer, err)
			return
		}
		if !module.visible(user, scene) {
			core.Fail(writer, 404, "场景不存在")
			return
		}
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			core.Fail(writer, 400, "店铺或上架版本编号不正确或重复")
			return
		}
		seen[id] = true
		shopID, category := id, ""
		if kind == "listing" {
			var err error
			shopID, category, err = listing(request.Context(), module.DB, id)
			if err != nil {
				fail(writer, err)
				return
			}
		} else {
			var count int
			if err := module.DB.QueryRowContext(request.Context(), "SELECT count(*) FROM shops WHERE id=?", id).Scan(&count); err != nil {
				fail(writer, err)
				return
			}
			if count == 0 {
				core.Fail(writer, 404, "店铺不存在")
				return
			}
		}
		if !module.CanShop(user, shopID) {
			core.Fail(writer, 404, "店铺或上架版本不存在")
			return
		}
		if in.SceneID != nil {
			if err := matches(scene, shopID, category); err != nil {
				fail(writer, err)
				return
			}
		}
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	for _, id := range ids {
		if in.SceneID == nil {
			_, err = tx.ExecContext(request.Context(), "DELETE FROM scene_bindings WHERE target_type=? AND target_id=?", kind, id)
		} else {
			_, err = tx.ExecContext(request.Context(), `INSERT INTO scene_bindings(target_type,target_id,scene_id,updated_by,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(target_type,target_id) DO UPDATE SET scene_id=excluded.scene_id,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, kind, id, *in.SceneID, user.ID, core.Now())
		}
		if err != nil {
			fail(writer, err)
			return
		}
	}
	if err = core.AuditTx(tx, request, "scene.bind", "scene", scene.ID, in); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(writer, err)
		return
	}
	core.WriteJSON(writer, 200, core.Object{"ok": true})
}
