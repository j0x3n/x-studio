package creative

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"xstudio/internal/core"
)

func (f *fixture) mainTemplates(t *testing.T, overrides map[string]Template) {
	t.Helper()
	keys := map[string]string{}
	for seq := 1; seq <= 5; seq++ {
		slot := fmt.Sprintf("main%d", seq)
		tpl, ok := overrides[slot]
		if !ok {
			tpl = testTemplate("none")
		}
		tpl.Key, tpl.Slot, tpl.Kind = slot, slot, "main"
		keys[slot] = tpl.Key
		if err := f.a.storeTemplate(context.Background(), tpl, 1); err != nil {
			t.Fatal(err)
		}
	}
	f.exec(t, "INSERT INTO creative_shop_settings(shop_id,template_set,updated_at) VALUES(1,?,?) ON CONFLICT(shop_id) DO UPDATE SET template_set=excluded.template_set", core.JS(keys), core.Now())
}

func (f *fixture) scalar(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var value int64
	if err := f.c.DB.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func (f *fixture) assertNoGeneration(t *testing.T, sets, items, jobs, used int64) {
	t.Helper()
	for query, want := range map[string]int64{
		"SELECT count(*) FROM creative_sets":                     sets,
		"SELECT count(*) FROM creative_items":                    items,
		"SELECT count(*) FROM jobs":                              jobs,
		"SELECT coalesce(sum(images_used),0) FROM creative_sets": used,
	} {
		if got := f.scalar(t, query); got != want {
			t.Fatalf("%s: got %d want %d", query, got, want)
		}
	}
	if f.fake.images.Load() != 0 {
		t.Fatal("unexpected image generation")
	}
}

func TestPreflightRejectsMissingBeforeCreatingAnything(t *testing.T) {
	f := newFixture(t)
	tpl := testTemplate("redraw")
	tpl.PhotoCodes = []string{"A1", "A2", "B2"}
	f.mainTemplates(t, map[string]Template{"main2": tpl})
	path := fmt.Sprintf("/api/listings/%d/creative", f.listingID)
	response := f.request(t, "POST", path, core.Object{"parts": []string{"main"}}, 409)
	missing, err := typed[[]Missing](response["missing"])
	if err != nil || len(missing) != 1 || missing[0].Slot != "main2" || missing[0].Label != slotLabel("main2") || missing[0].TemplateKey != "main2" || missing[0].Reason != "缺少实拍参考图（A1、A2、B2）" || response["error"] != slotLabel("main2")+"："+missing[0].Reason {
		t.Fatal(response, err)
	}
	f.assertNoGeneration(t, 0, 0, 0, 0)
}

func TestPreflightSkipMissingGeneratesRemainingItems(t *testing.T) {
	f := newFixture(t)
	f.mainTemplates(t, map[string]Template{"main2": testTemplate("redraw")})
	response := f.request(t, "POST", fmt.Sprintf("/api/listings/%d/creative", f.listingID), core.Object{"parts": []string{"main"}, "params": core.Object{"skip_missing": true}}, 202)
	sid := int64(number(response["set"].(map[string]any)["id"]))
	jid := int64(number(response["jobs"].([]any)[0].(map[string]any)["id"]))
	items, _ := f.a.currentItems(context.Background(), sid)
	for _, item := range items {
		if item.Slot == "main2" && (item.Error != "缺少实拍参考图（A1）" || item.Route != "manual" || core.JS(item.RouteReasons) != `["缺少素材，未生成"]`) {
			t.Fatal(item)
		}
	}
	if err := f.runJob(t, jid); err != nil {
		t.Fatal(err)
	}
	items, _ = f.a.currentItems(context.Background(), sid)
	for _, item := range items {
		if item.Slot == "main2" {
			if item.OutputAssetID != 0 || item.Error == "" || item.Attempt != 1 {
				t.Fatal(item)
			}
		} else if item.OutputAssetID == 0 || item.Error != "" {
			t.Fatal(item)
		}
	}
	set, _ := f.a.set(context.Background(), sid)
	if len(items) != 6 || set.ImagesUsed != 5 || f.fake.images.Load() != 5 || set.Status != "reviewing" {
		t.Fatal(set, f.fake.images.Load())
	}
}

func TestPreflightAllMissingEvenWithSkip(t *testing.T) {
	f := newFixture(t)
	overrides := map[string]Template{}
	for seq := 1; seq <= 5; seq++ {
		overrides[fmt.Sprintf("main%d", seq)] = testTemplate("redraw")
	}
	f.mainTemplates(t, overrides)
	response := f.request(t, "POST", fmt.Sprintf("/api/listings/%d/creative", f.listingID), core.Object{"parts": []string{"main"}, "params": core.Object{"skip_missing": true}}, 409)
	if len(response["missing"].([]any)) != 6 || !strings.Contains(stringValue(response["error"]), "；") {
		t.Fatal(response)
	}
	f.assertNoGeneration(t, 0, 0, 0, 0)
}

func TestPreflightRedrawAndTrialRejectMissing(t *testing.T) {
	f := newFixture(t)
	tpl := testTemplate("redraw")
	if err := f.a.storeTemplate(context.Background(), tpl, 1); err != nil {
		t.Fatal(err)
	}
	_, items := f.makeSet(t, tpl, 1)
	for _, path := range []string{itemPath(items[0].ID, "redraw"), "/api/creative/templates/" + tpl.Key + "/trial"} {
		response := f.request(t, "POST", path, core.Object{"listing_id": f.listingID}, 409)
		if len(response["missing"].([]any)) != 1 {
			t.Fatal(response)
		}
	}
	f.assertNoGeneration(t, 1, 1, 0, 0)
}

func TestPreflightChecksModelsAndPhotoModeCodes(t *testing.T) {
	f := newFixture(t)
	f.source(t)
	model := testTemplate("model")
	model.Wear = &Wear{Slot: "female_main", BodyPart: "ear", Framing: "closeup"}
	f.mainTemplates(t, map[string]Template{"main2": model, "main4": testTemplate("photo")})
	response := f.request(t, "POST", fmt.Sprintf("/api/listings/%d/creative", f.listingID), core.Object{"parts": []string{"main"}}, 409)
	missing, _ := typed[[]Missing](response["missing"])
	if len(missing) != 2 || missing[0].Reason != "先在 AI 出图设置里给店铺选模特" || missing[1].Reason != "缺少实拍参考图（C1、C2、C3）" {
		t.Fatal(response)
	}
	f.assertNoGeneration(t, 0, 0, 0, 0)
}

func TestPreflightExpandsHeroAndSizesWithoutCuttingOut(t *testing.T) {
	f := newFixture(t)
	rembg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("preflight called rembg")
		w.WriteHeader(500)
	}))
	defer rembg.Close()
	f.c.Cfg.RembgURL = rembg.URL
	model := testTemplate("model")
	model.SizeCompare = true
	model.Wear = &Wear{Slot: "female_main", BodyPart: "ear", Framing: "closeup"}
	f.mainTemplates(t, map[string]Template{"main1": testTemplate("pixel_lock"), "main4": model})
	request := core.Object{"parts": []string{"main"}, "params": core.Object{"hero_b": false, "sizes": []Size{{Label: "小", MM: 6}, {Label: "中", MM: 8}, {Label: "大", MM: 10}}}}
	response := f.request(t, "POST", fmt.Sprintf("/api/listings/%d/creative", f.listingID), request, 409)
	if len(response["missing"].([]any)) != 4 {
		t.Fatal(response)
	}
	f.source(t)
	f.badSource(t, "A2")
	f.exec(t, `UPDATE creative_shop_settings SET models='{"female_main":1}' WHERE shop_id=1`)
	response = f.request(t, "POST", fmt.Sprintf("/api/listings/%d/creative", f.listingID), request, 202)
	if f.fake.images.Load() != 0 || number(response["set"].(map[string]any)["images_used"]) != 0 {
		t.Fatal(response)
	}
}

func TestPreflightRedrawAndTrialRequireConfiguredModel(t *testing.T) {
	f := newFixture(t)
	f.source(t)
	tpl := testTemplate("model")
	tpl.Wear = &Wear{Slot: "female_main", BodyPart: "ear", Framing: "closeup"}
	if err := f.a.storeTemplate(context.Background(), tpl, 1); err != nil {
		t.Fatal(err)
	}
	_, items := f.makeSet(t, tpl, 1)
	for _, path := range []string{itemPath(items[0].ID, "redraw"), "/api/creative/templates/" + tpl.Key + "/trial"} {
		response := f.request(t, "POST", path, core.Object{"listing_id": f.listingID}, 409)
		missing, _ := typed[[]Missing](response["missing"])
		if len(missing) != 1 || missing[0].Reason != "先在 AI 出图设置里给店铺选模特" {
			t.Fatal(response)
		}
	}
	f.assertNoGeneration(t, 1, 1, 0, 0)
}

func TestSetContinuesAfterAIErrorAndQueuesDetail(t *testing.T) {
	f := newFixture(t)
	f.fake.failImage.Store(2)
	tpl := testTemplate("none")
	tpl.Kind, tpl.Slot = "main", "main2"
	set, _ := f.makeSet(t, tpl, 2)
	f.exec(t, `UPDATE creative_sets SET parts='["main","detail"]' WHERE id=?`, set.ID)
	if err := f.run(t, "creative.main", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ := f.a.currentItems(context.Background(), set.ID)
	if items[0].Error == "" || items[0].Route != "manual" || items[0].OutputAssetID != 0 || items[1].OutputAssetID == 0 {
		t.Fatal(items)
	}
	job, _ := f.a.one(context.Background(), "SELECT status,message FROM jobs WHERE kind='creative.main'")
	if job["status"] != "done" || !strings.HasPrefix(stringValue(job["message"]), "1 张生成失败：main2 ") || f.scalar(t, "SELECT count(*) FROM jobs WHERE kind='creative.detail' AND status='queued'") != 1 {
		t.Fatal(job)
	}
}

func TestSetStopsImmediatelyAfterPermissionRevocation(t *testing.T) {
	f := newFixture(t)
	set, _ := f.makeSet(t, testTemplate("none"), 2)
	f.fake.before = func() {
		_, err := f.c.DB.Exec("UPDATE users SET disabled=1 WHERE id=1")
		if err != nil {
			t.Error(err)
		}
	}
	err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{})
	var status *statusError
	if !errors.As(err, &status) || status.status != 403 || f.fake.images.Load() != 1 || f.scalar(t, "SELECT count(*) FROM assets WHERE kind='output'") != 0 {
		t.Fatal(err, f.fake.images.Load())
	}
	set, _ = f.a.set(context.Background(), set.ID)
	if set.ImagesUsed != 1 {
		t.Fatal(set)
	}
}

func TestLaterAutomaticRedrawErrorDoesNotStopOtherItems(t *testing.T) {
	f := newFixture(t)
	set, original := f.makeSet(t, testTemplate("none"), 3)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "UPDATE creative_items SET output_asset_id=0 WHERE id=?", original[2].ID)
	f.fake.before = func() {
		if f.fake.images.Load() == 4 {
			_, err := f.c.DB.Exec("UPDATE creative_items SET route='auto_redraw' WHERE id IN (?,?)", original[0].ID, original[1].ID)
			if err != nil {
				t.Error(err)
			}
		}
	}
	f.fake.failImageFrom.Store(5)
	f.fake.failImage.Store(6)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ := f.a.currentItems(context.Background(), set.ID)
	if len(items) != 3 || items[0].Attempt != 2 || items[0].Error == "" || items[1].Attempt != 2 || items[1].OutputAssetID == 0 || items[2].OutputAssetID == 0 || f.fake.images.Load() != 7 {
		t.Fatal(items, f.fake.images.Load())
	}
}

func TestSetStopsWhenContextIsCanceled(t *testing.T) {
	f := newFixture(t)
	set, _ := f.makeSet(t, testTemplate("none"), 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.fake.before = cancel
	err := f.a.execute(ctx, core.Job{Kind: "creative.detail", TargetType: "creative_set", TargetID: set.ID, UserID: 1})
	if !errors.Is(err, context.Canceled) || f.fake.images.Load() != 1 {
		t.Fatal(err, f.fake.images.Load())
	}
}

func (f *fixture) badSource(t *testing.T, code string) int64 {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 160, 120))
	draw.Draw(im, im.Rect, image.NewUniform(color.NRGBA{R: 20, G: 70, B: 30, A: 255}), image.Point{}, draw.Src)
	for _, point := range []image.Point{{0, 0}, {159, 0}, {0, 119}, {159, 119}} {
		im.Set(point.X, point.Y, color.White)
	}
	name := "原图/bad-" + code + ".png"
	if err := writePNG(f.c.Dir(f.productID), name, im); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "INSERT INTO assets(product_id,file,orig_name,width,height,code,created_by,created_at) VALUES(?,?,'bad',160,120,?,1,?)", f.productID, name, code, core.Now())
	return f.scalar(t, "SELECT id FROM assets WHERE product_id=? AND code=?", f.productID, code)
}

func TestUntrustedFallbackSkipsAIAndAllowance(t *testing.T) {
	f := newFixture(t)
	f.badSource(t, "A1")
	set, _ := f.makeSet(t, testTemplate("pixel_lock"), 1)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ := f.a.currentItems(context.Background(), set.ID)
	item := items[0]
	if !strings.HasPrefix(item.Error, "抠图失败：A1") || !strings.Contains(item.Error, "兜底算法") || !strings.Contains(item.Error, "REMBG_URL") || item.Route != "manual" {
		t.Fatal(item)
	}
	f.assertNoGeneration(t, 1, 1, 1, 0)
}

func TestCutoutUsesSecondPhotoAndRecordsCoverage(t *testing.T) {
	f := newFixture(t)
	good := f.source(t)
	f.badSource(t, "E4")
	tpl := testTemplate("pixel_lock")
	tpl.PhotoCodes = []string{"E4", "A1"}
	tpl.Texts = []TextSpec{{Key: "title", From: "literal:珍珠"}}
	f.fake.read = []OCRText{{Text: "珍珠", Box: []float64{0.05, 0.05, 0.1, 0.1}}}
	set, _ := f.makeSet(t, tpl, 1)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ := f.a.currentItems(context.Background(), set.ID)
	item := items[0]
	if item.OutputAssetID == 0 || len(item.SourceAssetIDs) != 1 || item.SourceAssetIDs[0] != good || item.Hard.Cutout == nil || item.Hard.Cutout.Mode != "fallback" || item.Hard.Cutout.Code != "A1" || item.Hard.Cutout.Coverage != 0.25 || f.fake.images.Load() != 1 {
		t.Fatal(item)
	}
	if err := f.run(t, "creative.verify", "creative_item", item.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	item, _ = f.a.item(context.Background(), item.ID)
	if item.Hard.Cutout == nil || item.Hard.Cutout.Coverage != 0.25 {
		t.Fatal(item.Hard)
	}
	f.fake.mu.Lock()
	f.fake.read = []OCRText{{Text: "淡水珍珠", Box: []float64{0.05, 0.05, 0.1, 0.1}}}
	f.fake.mu.Unlock()
	queued := f.request(t, "POST", itemPath(item.ID, "retext"), core.Object{"key": "title", "text": "淡水珍珠"}, 202)
	if err := f.runJob(t, int64(number(queued["job_id"]))); err != nil {
		t.Fatal(err)
	}
	items, _ = f.a.currentItems(context.Background(), set.ID)
	if items[0].Hard.Cutout == nil || items[0].Hard.Cutout.Coverage != 0.25 {
		t.Fatal(items[0].Hard)
	}
}

func TestRectLockBypassesCutoutAndSinglePhotoBKeepsAnglePrompt(t *testing.T) {
	f := newFixture(t)
	source := f.badSource(t, "A1")
	tpl := testTemplate("pixel_lock")
	tpl.LockShape = "rect"
	set, items := f.makeSet(t, tpl, 1)
	f.exec(t, "UPDATE creative_items SET variant='B',prompt='背景不合格' WHERE id=?", items[0].ID)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ = f.a.currentItems(context.Background(), set.ID)
	item := items[0]
	if item.OutputAssetID == 0 || item.Hard.Cutout != nil || core.JS(item.SourceAssetIDs) != core.JS([]int64{source}) || !strings.Contains(item.Prompt, "FIX 背景不合格") || !strings.Contains(item.Prompt, "clearly different camera angle") || f.fake.images.Load() != 1 {
		t.Fatal(item)
	}
}

func TestRembgCoverageAndThresholds(t *testing.T) {
	for _, test := range []struct {
		name     string
		coverage float64
		want     bool
	}{{"fallback", 0.6, true}, {"fallback", 0.6001, false}, {"rembg", 0.85, true}, {"rembg", 0.8501, false}, {"rembg", 0.003, true}, {"rembg", 0.0029, false}, {"fallback", 0, false}} {
		if trustedCutout(test.name, test.coverage) != test.want {
			t.Fatal(test)
		}
	}
	for _, opaque := range []int{0, 60, 90} {
		t.Run(fmt.Sprint(opaque), func(t *testing.T) {
			f := newFixture(t)
			f.source(t)
			rembg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/remove" {
					t.Error(r.URL.Path)
				}
				mask := image.NewNRGBA(image.Rect(0, 0, 160, 120))
				draw.Draw(mask, image.Rect(0, 0, 160, opaque*120/100), image.NewUniform(color.NRGBA{A: 255}), image.Point{}, draw.Src)
				_ = png.Encode(w, mask)
			}))
			defer rembg.Close()
			f.c.Cfg.RembgURL = rembg.URL
			set, _ := f.makeSet(t, testTemplate("pixel_lock"), 1)
			if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
				t.Fatal(err)
			}
			items, _ := f.a.currentItems(context.Background(), set.ID)
			if opaque == 60 {
				cutout := items[0].Hard.Cutout
				if cutout == nil || cutout.Mode != "rembg" || cutout.Coverage != 0.6 || f.fake.images.Load() != 1 {
					t.Fatal(items[0])
				}
			} else {
				f.assertNoGeneration(t, 1, 1, 1, 0)
				if !strings.Contains(items[0].Error, "rembg") {
					t.Fatal(items[0])
				}
			}
		})
	}
}

func TestHeroBUsesSecondPhoto(t *testing.T) {
	f := newFixture(t)
	first := f.source(t)
	f.exec(t, "INSERT INTO assets(product_id,file,orig_name,width,height,code,created_by,created_at) SELECT product_id,file,orig_name,width,height,'A2',created_by,created_at FROM assets WHERE id=?", first)
	second := f.scalar(t, "SELECT id FROM assets WHERE code='A2'")
	tpl := testTemplate("pixel_lock")
	tpl.PhotoCodes = []string{"A1", "A2"}
	set, items := f.makeSet(t, tpl, 2)
	f.exec(t, "UPDATE creative_items SET variant='A' WHERE id=?", items[0].ID)
	f.exec(t, "UPDATE creative_items SET variant='B' WHERE id=?", items[1].ID)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ = f.a.currentItems(context.Background(), set.ID)
	if core.JS(items[0].SourceAssetIDs) != core.JS([]int64{first}) || core.JS(items[1].SourceAssetIDs) != core.JS([]int64{second}) || items[1].Hard.Cutout.Code != "A2" {
		t.Fatal(items)
	}
	modelID := f.configureModel(t)
	tpl = testTemplate("model")
	tpl.PhotoCodes = []string{"A1", "A2"}
	tpl.Wear = &Wear{Slot: "female_main", BodyPart: "ear", Framing: "closeup"}
	set, items = f.makeSet(t, tpl, 2)
	f.exec(t, "UPDATE creative_items SET variant='A' WHERE id=?", items[0].ID)
	f.exec(t, "UPDATE creative_items SET variant='B' WHERE id=?", items[1].ID)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ = f.a.currentItems(context.Background(), set.ID)
	if items[0].OutputAssetID == 0 || items[1].OutputAssetID == 0 || items[1].ModelID != modelID || core.JS(items[0].SourceAssetIDs) != core.JS([]int64{first, second}) || core.JS(items[1].SourceAssetIDs) != core.JS([]int64{second, first}) {
		t.Fatal(items)
	}
}

func (f *fixture) configureModel(t *testing.T) int64 {
	t.Helper()
	f.exec(t, "INSERT INTO shop_models(shop_id,name,description,created_by,created_at,updated_at) VALUES(1,'固定模特','保持身份',1,?,?)", core.Now(), core.Now())
	modelID := f.scalar(t, "SELECT max(id) FROM shop_models")
	photos, err := f.a.Photos(f.productID)
	if err != nil || len(photos) == 0 {
		t.Fatal(photos, err)
	}
	source, err := f.a.sourceImage(photos[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := writePNG(filepath.Join(f.c.Cfg.DataDir, "models", fmt.Sprint(modelID)), "reference.png", source); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "INSERT INTO model_references(model_id,body_part,source,file,orig_name,width,height,created_by,created_at) VALUES(?,'ear','original','reference.png','reference',160,120,1,?)", modelID, core.Now())
	f.exec(t, "INSERT INTO creative_shop_settings(shop_id,models,updated_at) VALUES(1,?,?) ON CONFLICT(shop_id) DO UPDATE SET models=excluded.models", core.JS(core.Object{"female_main": modelID}), core.Now())
	return modelID
}
