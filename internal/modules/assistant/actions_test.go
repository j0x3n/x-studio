package assistant

import (
	"encoding/json"
	"strings"
	"testing"
	"xstudio/internal/core"
)

func TestListingActionPermissionsParametersAndAILabel(t *testing.T) {
	f := newFixture(t)
	_, lid := f.product(t, "珠蝶", 1)
	uid := f.user(t, "runner", "shops", []string{core.RenderRun, core.CopyEdit, core.RenderReview}, 1)
	u := mustUser(t, f, uid)
	validate := func(kind string, params object) (Action, bool) {
		return f.m.validateAction(u, json.RawMessage(core.JS(card(kind, params))))
	}
	for _, kind := range []string{"run_listing", "render_listing", "generate_copy"} {
		params := object{"listing_id": lid}
		if kind == "generate_copy" {
			params["instructions"] = "写短一点"
		}
		c, ok := validate(kind, params)
		if !ok {
			t.Fatal(kind)
		}
		if kind == "run_listing" && c.Params["ai_label"] != true {
			t.Fatal(c)
		}
	}
	c, ok := validate("run_listing", object{"listing_id": lid, "ai_label": false})
	if !ok || c.Params["ai_label"] != true {
		t.Fatal(c, ok)
	}
	u.Role.Perms = append(u.Role.Perms, core.ExportNoAILabel)
	c, ok = validate("run_listing", object{"listing_id": lid, "ai_label": false})
	if !ok || c.Params["ai_label"] != false {
		t.Fatal(c, ok)
	}
	if _, ok = validate("render_batch", object{"listing_ids": []int64{lid}}); !ok {
		t.Fatal("valid batch rejected")
	}
	if _, ok = validate("review", object{"listing_id": lid, "status": "approved"}); !ok {
		t.Fatal("approval rejected")
	}
	if _, ok = validate("review", object{"listing_id": lid, "status": "rejected", "note": "图像有瑕疵"}); !ok {
		t.Fatal("rejection rejected")
	}
	for _, tc := range []struct {
		kind   string
		params object
	}{
		{"unknown", object{}}, {"run_listing", object{}}, {"run_listing", object{"listing_id": "1"}}, {"run_listing", object{"listing_id": 999}}, {"run_listing", object{"listing_id": lid, "ai_label": "false"}}, {"render_listing", object{"listing_id": lid, "ai_label": false}}, {"render_batch", object{"listing_ids": []int64{lid, 999}}}, {"render_batch", object{"listing_ids": make([]int64, 51)}}, {"render_batch", object{"listing_ids": []int64{lid, lid}}}, {"review", object{"listing_id": lid, "status": "pending"}}, {"review", object{"listing_id": lid, "status": "rejected", "note": " "}}, {"generate_copy", object{"listing_id": lid, "instructions": 12}},
	} {
		if _, ok := validate(tc.kind, tc.params); ok {
			t.Fatal("invalid action accepted", tc)
		}
	}
	for _, perm := range []string{core.RenderRun, core.CopyEdit, core.RenderReview} {
		without := u
		without.Role.Perms = []string{}
		for _, p := range u.Role.Perms {
			if p != perm {
				without.Role.Perms = append(without.Role.Perms, p)
			}
		}
		for _, kind := range map[string][]string{core.RenderRun: {"run_listing", "render_listing", "render_batch"}, core.CopyEdit: {"generate_copy"}, core.RenderReview: {"review"}}[perm] {
			params := object{"listing_id": lid, "status": "approved"}
			if kind == "render_batch" {
				params = object{"listing_ids": []int64{lid}}
			}
			if _, ok := f.m.validateAction(without, json.RawMessage(core.JS(card(kind, params)))); ok {
				t.Fatal("permission bypass", kind)
			}
		}
	}
}

func TestProcessActionEffectiveOpsPermissionPresetScopeAndCost(t *testing.T) {
	f := newFixture(t)
	p1, _ := f.product(t, "珠蝶", 1)
	p2, _ := f.product(t, "另一款", 1)
	a1 := f.asset(t, p1, "original")
	a2 := f.asset(t, p1, "original")
	a3 := f.asset(t, p2, "original")
	uid := f.user(t, "photographer2", "shops", []string{core.AssetUpload, core.AssetUpscale}, 1)
	guest := f.user(t, "collaborator2", "shares", []string{core.AssetUpload, core.AssetUpscale, core.AssetProcess})
	f.exec(t, "INSERT INTO shares(product_id,user_id,can_upload,created_by,created_at) VALUES(?,?,0,1,?)", p1, guest, core.Now())
	preset := f.exec(t, "INSERT INTO asset_presets(name,owner_id,shared,params,created_at,updated_at) VALUES('AI预设',1,1,?, ?,?)", `{"ops":{"ai":{"extra":"去灰尘"},"upscale":{"target_long_side":2048}},"set_current":true}`, core.Now(), core.Now())
	u := mustUser(t, f, uid)
	validate := func(params object) (Action, bool) {
		return f.m.validateAction(u, json.RawMessage(core.JS(card("process_assets", params))))
	}
	base := object{"asset_ids": []int64{a1, a2}, "ops": object{"resize": object{"long_side": 1000}}}
	c, ok := validate(base)
	if !ok || c.CostHint != "" {
		t.Fatal(c, ok)
	}
	c, ok = validate(object{"asset_ids": []int64{a1, a2}, "ops": object{"upscale": object{"target_long_side": 2048}}})
	if !ok || c.CostHint != "约用 2 张图额度" {
		t.Fatal(c, ok)
	}
	if _, ok = validate(object{"asset_ids": []int64{a1}, "preset_id": preset}); ok {
		t.Fatal("preset bypassed AI permission")
	}
	if _, ok = validate(object{"asset_ids": []int64{a1}, "ops": object{"ai": object{"extra": "修图"}}}); ok {
		t.Fatal("AI bypassed permission")
	}
	// AI takes priority over upscale, exactly as in assets.processPermission.
	u.Role.Perms = []string{core.AssetUpload, core.AssetProcess}
	c, ok = validate(object{"asset_ids": []int64{a1}, "preset_id": preset, "ops": object{"ai": object{"extra": "改背景"}}})
	if !ok || c.CostHint != "约用 2 张图额度" || !strings.Contains(core.JS(c.Params), "改背景") {
		t.Fatal(c, ok)
	}
	u.Role.Perms = []string{core.AssetUpscale, core.AssetProcess}
	if _, ok = validate(base); ok {
		t.Fatal("processing requires upload")
	}
	u = mustUser(t, f, 1)
	for _, params := range []object{
		{"asset_ids": []int64{a1}},
		{"asset_ids": []int64{a1}, "ops": nil},
		{"asset_ids": []int64{a1, a3}, "ops": object{}},
		{"asset_ids": []int64{999}, "ops": object{}},
		{"asset_ids": []int64{a1, a1}, "ops": object{}},
		{"asset_ids": make([]int64, 21), "ops": object{}},
		{"asset_ids": []int64{a1}, "ops": object{"unknown": true}},
		{"asset_ids": []int64{a1}, "ops": object{"upscale": object{"target_long_side": 800}}},
		{"asset_ids": []int64{a1}, "ops": object{"ai": object{"quick": []string{"unknown"}}}},
		{"asset_ids": []int64{a1}, "ops": object{"ai": object{}}},
		{"asset_ids": []int64{a1}, "ops": object{"geometry": object{"rotate": 45}}},
		{"asset_ids": []int64{a1}, "ops": object{"color": object{"match_to": a3}}},
		{"asset_ids": []int64{a1}, "ops": object{"color": object{"match_to": a1}}},
		{"asset_ids": []int64{a1}, "preset_id": 999},
		{"asset_ids": []int64{a1}, "ops": object{}, "set_current": true},
	} {
		if _, ok := validate(params); ok {
			t.Fatal("invalid process action accepted", params)
		}
	}
	if _, ok = validate(object{"asset_ids": []int64{a1}, "ops": object{"color": object{"match_to": a2}}}); !ok {
		t.Fatal("same product color reference rejected")
	}
	u = mustUser(t, f, guest)
	if _, ok = validate(base); ok {
		t.Fatal("share without upload allowed processing")
	}
	f.exec(t, "UPDATE shares SET can_upload=1 WHERE user_id=?", guest)
	if _, ok = validate(base); !ok {
		t.Fatal("share upload rejected")
	}
	if _, ok = validate(object{"asset_ids": []int64{a1}, "preset_id": preset}); ok {
		t.Fatal("shared preset leaked to collaborator")
	}
}

func TestRefsAndContextRespectListingAssetAndJobVisibility(t *testing.T) {
	f := newFixture(t)
	shop := f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES('隐藏店','other','en',?)", core.Now())
	p1, l1 := f.product(t, "珠蝶", 1)
	p2, l2 := f.product(t, "隐藏款", shop)
	a1 := f.asset(t, p1, "original")
	a2 := f.asset(t, p2, "original")
	uid := f.user(t, "viewer", "shops", []string{core.ProductView}, 1)
	j1 := f.exec(t, "INSERT INTO jobs(kind,target_type,target_id,user_id,status,steps,created_at) VALUES('test','listing',?,1,'done','',?)", l1, core.Now())
	j2 := f.exec(t, "INSERT INTO jobs(kind,target_type,target_id,user_id,status,steps,created_at) VALUES('test','listing',?,1,'done','',?)", l2, core.Now())
	u := mustUser(t, f, uid)
	for _, v := range []struct {
		kind            string
		visible, hidden int64
	}{{"product", p1, p2}, {"listing", l1, l2}, {"asset", a1, a2}, {"job", j1, j2}} {
		if path, label, ok := f.m.reference(u, v.kind, v.visible); !ok || path == "" || label == "" {
			t.Fatal(v, path, label, ok)
		}
		if _, _, ok := f.m.reference(u, v.kind, v.hidden); ok {
			t.Fatal("hidden ref", v)
		}
		if v.kind != "job" {
			ctx := &chatContext{}
			ctx.Selection = &struct {
				Type string  `json:"type"`
				IDs  []int64 `json:"ids"`
			}{v.kind, []int64{v.visible, v.hidden}}
			prompt, err := f.m.prompt(u, ctx)
			if err != nil || strings.Contains(prompt, "隐藏款") || strings.Contains(prompt, "隐藏店") {
				t.Fatal(prompt, err)
			}
		}
	}
}

func TestRoutesRequireLogin(t *testing.T) {
	f := newFixture(t)
	f.client.Jar = nil
	f.req(t, "GET", "/api/home", nil, 401)
	f.req(t, "POST", "/api/assistant/chat", chatBody("好"), 401)
}

func TestOpenActionsCheckObjectVisibility(t *testing.T) {
	f := newFixture(t)
	shop := f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES('隐藏店','other','en',?)", core.Now())
	p1, _ := f.product(t, "珠蝶", 1)
	p2, _ := f.product(t, "隐藏款", shop)
	f.asset(t, p1, "original")
	f.asset(t, p2, "original")
	uid := f.user(t, "viewer", "shops", []string{core.ProductView}, 1)
	u := mustUser(t, f, uid)
	for _, tc := range []struct {
		path  string
		valid bool
	}{
		{"/products/1", true}, {"/copy?listing=1", true}, {"/assets?asset=1", true}, {"/products?tab=sampling", true},
		{"/products/2", false}, {"/copy?listing=2", false}, {"/assets?asset=2", false}, {"/assets?product_id=2", false},
	} {
		_, ok := f.m.validateAction(u, json.RawMessage(core.JS(card("open", object{"path": tc.path}))))
		if ok != tc.valid {
			t.Fatal(tc, ok)
		}
	}
}
