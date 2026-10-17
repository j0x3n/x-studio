package assets_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"xstudio/internal/core"
	"xstudio/internal/modules/assets"
)

func TestAssetWriteProbeAndDeletePathGuard(t *testing.T) {
	f := newFixture(t, nil)
	shop := id(f.req(t, "POST", "/api/shops", obj{"name": "隐藏店铺", "platform": "other", "language": "en"}, 201)["shop"])
	hidden := f.product(t, "隐藏", shop)
	aid := f.upload(t, hidden, imageBytes(t, 20, 20))
	f.user(t, "worker", 4, 1)
	wbExec(t, f, "INSERT INTO jobs(kind,target_type,target_id,user_id,status,steps,created_at) SELECT 'pipeline','listing',id,1,'running','check',? FROM listings WHERE product_id=?", core.Now(), hidden)
	f.login(t, "worker")
	for _, method := range []string{"PATCH", "DELETE"} {
		real := f.req(t, method, fmt.Sprintf("/api/assets/%d", aid), obj{"note": "probe"}, 404)
		missing := f.req(t, method, "/api/assets/9999999", obj{"note": "probe"}, 404)
		if core.JS(real) != core.JS(missing) || real["error"] != "素材不存在" {
			t.Fatal("素材存在性泄漏", real, missing)
		}
	}
	f.login(t, "admin")
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), obj{"note": "busy"}, 409)
	wbExec(t, f, "UPDATE jobs SET status='done' WHERE kind='pipeline'")
	marker := filepath.Join(f.c.Cfg.DataDir, "sentinel.png")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	wbExec(t, f, "UPDATE assets SET file='../../sentinel.png',enhanced_file='../../sentinel.png' WHERE id=?", aid)
	f.req(t, "DELETE", fmt.Sprintf("/api/assets/%d", aid), nil, 200)
	if b, err := os.ReadFile(marker); err != nil || string(b) != "keep" {
		t.Fatal("删除越界文件", err)
	}
}
func TestCollaboratorPresetPrivacyAndPermissionBeforeLookup(t *testing.T) {
	f := newFixture(t, nil)
	pid := f.product(t, "共享款", 1)
	aid := f.upload(t, pid, imageBytes(t, 30, 20))
	shared := id(f.req(t, "POST", "/api/presets", obj{"name": "管理员共享", "shared": true, "params": obj{"ops": obj{"format": "png"}}}, 201)["preset"])
	private := id(f.req(t, "POST", "/api/presets", obj{"name": "管理员私有", "params": obj{}}, 201)["preset"])
	guest := f.user(t, "guest", 5)
	f.req(t, "POST", fmt.Sprintf("/api/products/%d/shares", pid), obj{"user_id": guest, "can_upload": true}, 201)
	f.login(t, "guest")
	own := id(f.req(t, "POST", "/api/presets", obj{"name": "个人预设", "params": obj{"ops": obj{"format": "png"}}}, 201)["preset"])
	wbExec(t, f, "UPDATE asset_presets SET shared=1 WHERE id=?", own)
	listed := f.req(t, "GET", "/api/presets", nil, 200)
	if listed["total"] != float64(1) || id(listed["items"].([]any)[0]) != own {
		t.Fatal("协作者看见共享预设", listed)
	}
	input := wbInput(t, f)
	for _, preset := range []int64{shared, private, 999999} {
		for _, method := range []string{"PATCH", "DELETE"} {
			if o := f.req(t, method, fmt.Sprintf("/api/presets/%d", preset), obj{"name": "probe"}, 404); o["error"] != "预设不存在" {
				t.Fatal(o)
			}
		}
		f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{aid}, "preset_id": preset}, 404)
		f.req(t, "POST", "/api/workbench/process", obj{"inputs": []obj{{"input_id": input}}, "preset_id": preset}, 404)
	}
	job := f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{aid}, "preset_id": own}, 202)
	newID(t, f.wait(t, id(job["job"])))
	job = f.req(t, "POST", "/api/workbench/process", obj{"inputs": []obj{{"input_id": input}}, "preset_id": own}, 202)
	rid := wbResult(t, f.wait(t, id(job["job"])))
	got := f.req(t, "GET", fmt.Sprintf("/api/workbench/items/%d", rid), nil, 200)["item"].(obj)
	if got["op"].(obj)["ops"].(obj)["format"] != "png" {
		t.Fatal(got)
	}
	f.login(t, "admin")
	// Even admin cannot mutate someone else's preset under the new ownership rule.
	f.req(t, "DELETE", fmt.Sprintf("/api/presets/%d", own), nil, 404)
	// Keep asset.process but remove asset.upload: neither existing nor missing
	// presets may be queried before this permission is enforced.
	wbExec(t, f, "UPDATE roles SET perms=? WHERE id=5", core.JS([]string{core.ProductView, core.AssetProcess}))
	f.login(t, "guest")
	for _, preset := range []int64{shared, private, 999999} {
		f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{aid}, "preset_id": preset}, 403)
		f.req(t, "POST", "/api/workbench/process", obj{"inputs": []obj{{"input_id": input}}, "preset_id": preset}, 403)
	}
}
func TestColorReferenceProductAndInputBoundariesRechecked(t *testing.T) {
	f := newFixture(t, nil)
	p1 := f.product(t, "第一款", 1)
	p2 := f.product(t, "第二款", 1)
	a1 := f.upload(t, p1, imageBytes(t, 20, 20))
	a2 := f.upload(t, p2, imageBytes(t, 20, 20))
	ref1 := f.upload(t, p1, imageBytes(t, 20, 20))
	o := f.req(t, "POST", "/api/assets/process", obj{"asset_ids": []int64{a1}, "ops": obj{"color": obj{"match_to": a2}}}, 400)
	if o["error"] != "校色参考图必须是本批同一款的图" {
		t.Fatal(o)
	}
	input := wbInput(t, f)
	for _, ref := range []int64{a2, ref1} {
		o = f.req(t, "POST", "/api/workbench/process", obj{"inputs": []obj{{"asset_id": a1}, {"input_id": input}}, "ops": obj{"color": obj{"match_to": ref}}}, 400)
		if o["error"] != "校色参考图必须是本次输入里的一张素材" {
			t.Fatal(o)
		}
	}
	// Cross-product batches are allowed when the reference is one of their assets.
	j := wbJob(t, f, []obj{{"asset_id": a1}, {"asset_id": a2}, {"input_id": input}}, obj{"color": obj{"match_to": a2}})
	if j.Status != "done" || len(j.Result["items"].([]any)) != 3 {
		t.Fatal(j)
	}
	for _, row := range j.Result["items"].([]any) {
		if row.(obj)["error"] != nil {
			t.Fatal(row)
		}
	}
	// Call the registered handlers directly with already persisted job params to
	// prove the same rules apply at execution, independently of HTTP validation.
	handlers := assets.New(f.c).Jobs()
	params := core.JSONObj(core.JS(obj{"asset_ids": []int64{a1}, "ops": obj{"color": obj{"match_to": a2}, "format": "jpg"}}))
	err := handlers["assets.process"](context.Background(), core.Job{UserID: 1, TargetType: "product", TargetID: p1, Params: params})
	if err == nil || err.Error() != "校色参考图必须是本批同一款的图" {
		t.Fatal("执行未复查同款", err)
	}
	params = core.JSONObj(core.JS(obj{"inputs": []obj{{"asset_id": a1}}, "ops": obj{"color": obj{"match_to": a2}, "format": "jpg"}}))
	err = handlers["assets.workbench"](context.Background(), core.Job{UserID: 1, TargetType: "user", TargetID: 1, Params: params})
	if err == nil || err.Error() != "校色参考图必须是本次输入里的一张素材" {
		t.Fatal("执行未复查输入", err)
	}
}
func TestWorkbenchSaveScopeAndSourceMetadataRevocation(t *testing.T) {
	f := newFixture(t, nil)
	pid := f.product(t, "授权款", 1)
	hidden := f.product(t, "不可见保存目标")
	aid := f.upload(t, pid, imageBytes(t, 20, 20))
	guest := f.user(t, "guest", 5)
	f.req(t, "POST", fmt.Sprintf("/api/products/%d/shares", pid), obj{"user_id": guest, "can_upload": false}, 201)
	f.login(t, "guest")
	rid := wbResult(t, wbJob(t, f, []obj{{"asset_id": aid}}, obj{}))
	path := fmt.Sprintf("/api/workbench/items/%d", rid)
	v := f.req(t, "GET", path, nil, 200)["item"].(obj)
	if v["asset"] == nil {
		t.Fatal("缺少可见来源", v)
	}
	f.req(t, "POST", path+"/save", obj{}, 403)
	f.req(t, "POST", path+"/save", obj{"product_id": hidden}, 404)
	wbExec(t, f, "DELETE FROM shares WHERE user_id=? AND product_id=?", guest, pid)
	v = f.req(t, "GET", path, nil, 200)["item"].(obj)
	if v["asset"] != nil {
		t.Fatal("撤权后泄漏来源", v)
	}
	f.req(t, "POST", path+"/save", obj{}, 404)
	wbBinary(t, f, "GET", path+"/file", nil, 200)
}
