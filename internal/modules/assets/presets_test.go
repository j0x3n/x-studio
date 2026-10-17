package assets_test

import (
	"fmt"
	"testing"
)

func TestPresetVisibilityOwnershipValidationAndOverrides(t *testing.T) {
	f := newFixture(t, nil)
	pid := f.product(t, "预设应用", 1)
	aid := f.upload(t, pid, imageBytes(t, 80, 40))
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), obj{"code": "B1"}, 200)
	shared := id(f.req(t, "POST", "/api/presets", obj{"name": "共享", "shared": true, "params": obj{"ops": obj{"geometry": obj{"rotate": 90, "crop": []float64{0, 0, .5, 1}}, "resize": obj{"long_side": 80}, "format": "png"}, "set_current": true}}, 201)["preset"])
	private := id(f.req(t, "POST", "/api/presets", obj{"name": "私有", "params": obj{"ops": obj{}}}, 201)["preset"])
	f.user(t, "photo", 4, 1)
	f.login(t, "photo")
	o := f.req(t, "GET", "/api/presets", nil, 200)
	if o["total"] != float64(1) {
		t.Fatal("泄漏私有预设", o)
	}
	f.req(t, "PATCH", fmt.Sprintf("/api/presets/%d", shared), obj{"name": "篡改"}, 403)
	f.req(t, "DELETE", fmt.Sprintf("/api/presets/%d", shared), nil, 403)
	f.req(t, "PATCH", fmt.Sprintf("/api/presets/%d", private), obj{"name": "窥探"}, 404)
	f.req(t, "POST", "/api/presets", obj{"name": "无权限共享", "shared": true, "params": obj{}}, 403)
	f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{aid}, "preset_id": private}, 404)
	o = f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{aid}, "preset_id": shared, "set_current": false, "ops": obj{"geometry": obj{"rotate": 0}}}, 202)
	nid := newID(t, f.wait(t, id(o["job"])))
	var w, h int
	var code string
	f.c.DB.QueryRow("SELECT width,height,code FROM assets WHERE id=?", nid).Scan(&w, &h, &code)
	if w != 80 || h != 80 || code != "" {
		t.Fatal("显式参数未覆盖预设", w, h, code)
	}
	own := id(f.req(t, "POST", "/api/presets", obj{"name": "个人", "params": obj{}}, 201)["preset"])
	f.req(t, "PATCH", fmt.Sprintf("/api/presets/%d", own), obj{"name": "个人修改"}, 200)
	f.req(t, "DELETE", fmt.Sprintf("/api/presets/%d", own), nil, 200)
	for _, params := range []obj{{"asset_ids": []int{1}}, {"ops": obj{"unknown": true}}, {"ops": obj{"upscale": obj{"target_long_side": 2000}}}, {"ops": obj{"geometry": obj{"crop": []float64{0, 0, 2, 1}}}}, {"preset_id": shared}} {
		f.req(t, "POST", "/api/presets", obj{"name": "非法", "params": params}, 400)
	}
	f.login(t, "admin")
	f.req(t, "PATCH", fmt.Sprintf("/api/presets/%d", private), obj{"shared": true}, 200)
	f.req(t, "DELETE", fmt.Sprintf("/api/presets/%d", private), nil, 200)
	f.assertAudit(t, "assets.preset.create", "assets.preset.update", "assets.preset.delete")
}
