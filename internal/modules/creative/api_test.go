package creative

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"xstudio/internal/core"
)

func TestCompleteSetAPIJobsHeroGlanceAndTrial(t *testing.T) {
	f := newFixture(t)
	f.settings(t, core.Object{"final_glance": true})
	aid := f.source(t)
	f.exec(t, "INSERT INTO assets(product_id,file,orig_name,width,height,code,created_by,created_at) SELECT product_id,file,orig_name,width,height,'C1',created_by,created_at FROM assets WHERE id=?", aid)
	for _, kind := range []string{"main", "detail"} {
		count := 5
		if kind == "detail" {
			count = 9
		}
		for seq := 1; seq <= count; seq++ {
			tpl := testTemplate("none")
			tpl.Kind = kind
			tpl.Slot = fmt.Sprintf("%s%d", kind, seq)
			if kind == "detail" {
				tpl.Slot = fmt.Sprintf("d%d", seq)
			}
			tpl.Key = tpl.Slot
			if tpl.Slot == "main4" {
				tpl.ProductMode = "photo"
				tpl.PhotoCodes = []string{"C1"}
			}
			if err := f.a.storeTemplate(context.Background(), tpl, 1); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.request(t, "PUT", "/api/creative/shops/1/settings", core.Object{"models": core.Object{}, "template_set": core.Object{"main1": "main1", "d1": "d1"}}, 200)
	f.request(t, "GET", "/api/creative/shops/1/settings", nil, 200)
	response := f.request(t, "POST", fmt.Sprintf("/api/listings/%d/creative", f.listingID), core.Object{"parts": []string{"main", "detail"}}, 202)
	sid := int64(number(response["set"].(map[string]any)["id"]))
	jobs := response["jobs"].([]any)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	first := int64(number(jobs[0].(map[string]any)["id"]))
	if err := f.runJob(t, first); err != nil {
		t.Fatal(err)
	}
	next, err := f.a.one(context.Background(), "SELECT id FROM jobs WHERE target_type='creative_set' AND target_id=? AND status='queued'", sid)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.runJob(t, int64(number(next["id"]))); err != nil {
		t.Fatal(err)
	}
	items, err := f.a.currentItems(context.Background(), sid)
	if err != nil || len(items) != 15 {
		t.Fatal(len(items), err)
	}
	var heroB int64
	for _, item := range items {
		if item.Variant == "B" {
			heroB = item.ID
		}
		if item.Slot == "main4" && item.ProductMode != "photo" {
			t.Fatal("single-size position did not use ruler photo")
		}
	}
	if f.fake.images.Load() != 14 {
		t.Fatal("ruler photo should not call image service", f.fake.images.Load())
	}
	f.request(t, "POST", itemPath(heroB, "choose"), core.Object{"variant": "A"}, 200)
	chosen, _ := f.a.item(context.Background(), heroB)
	if chosen.Variant != "A" {
		t.Fatal(chosen.Variant)
	}
	queue := f.request(t, "GET", "/api/creative/review?shop_id=1&slot=main1", nil, 200)
	if number(queue["total"]) != 2 {
		t.Fatal(queue)
	}
	for _, item := range items {
		if item.Slot == "main1" {
			f.request(t, "POST", itemPath(item.ID, "judge"), core.Object{"verdict": "pass"}, 200)
		}
	}
	f.request(t, "POST", setPath(sid, "export"), core.Object{}, 409)
	f.request(t, "POST", setPath(sid, "glance"), core.Object{}, 200)
	detail := f.request(t, "GET", fmt.Sprintf("/api/creative/sets/%d", sid), nil, 200)
	if detail["glanced_at"] == "" {
		t.Fatal(detail)
	}
	f.request(t, "GET", fmt.Sprintf("/api/listings/%d/creative", f.listingID), nil, 200)
	f.request(t, "GET", "/api/creative/stats", nil, 200)
	trial := f.request(t, "POST", "/api/creative/templates/d1/trial", core.Object{"listing_id": f.listingID}, 202)
	if err = f.runJob(t, int64(number(trial["job_id"]))); err != nil {
		t.Fatal(err)
	}
	trialItem, err := f.a.item(context.Background(), int64(number(trial["item_id"])))
	if err != nil || trialItem.SetID != nil || trialItem.OutputAssetID == 0 {
		t.Fatal(trialItem, err)
	}
}

func TestModelGenerationUsesFixedReferencesAndComparison(t *testing.T) {
	fixture := newFixture(t)
	fixture.source(t)
	result, err := fixture.c.DB.Exec("INSERT INTO shop_models(shop_id,name,description,created_by,created_at,updated_at) VALUES(1,'固定模特','保持身份',1,?,?)", core.Now(), core.Now())
	if err != nil {
		t.Fatal(err)
	}
	modelID, _ := result.LastInsertId()
	dir := filepath.Join(fixture.c.Cfg.DataDir, "models", fmt.Sprint(modelID))
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(fixture.c.Dir(fixture.productID), "原图", "source.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "reference.png"), source, 0600); err != nil {
		t.Fatal(err)
	}
	fixture.exec(t, "INSERT INTO model_references(model_id,body_part,source,file,orig_name,width,height,created_by,created_at) VALUES(?,'ear','original','reference.png','reference',160,120,1,?)", modelID, core.Now())
	fixture.request(t, "PUT", "/api/creative/shops/1/settings", core.Object{"models": core.Object{"female_main": modelID}, "template_set": core.Object{}}, 200)
	template := testTemplate("model")
	template.Kind = "main"
	template.Slot = "main4"
	template.Wear = &Wear{Slot: "female_main", BodyPart: "auto", Framing: "closeup"}
	set, _ := fixture.makeSet(t, template, 2)
	fixture.exec(t, "UPDATE creative_sets SET params=? WHERE id=?", core.JS(Params{Sizes: []Size{{Label: "small", MM: 8}, {Label: "large", MM: 10}}}), set.ID)
	fixture.exec(t, "UPDATE creative_items SET size_compare=1 WHERE set_id=?", set.ID)
	review := passingReview()
	review.Checks["size"] = "pass"
	review.Checks["body"] = "pass"
	review.Checks["placement"] = "pass"
	review.SizeMM = []float64{8}
	fixture.fake.review = &review
	fixture.fake.read = []OCRText{{Text: "8mm 10mm", Box: []float64{0.1, 0.1, 0.3, 0.1}}}
	if err = fixture.run(t, "creative.main", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, err := fixture.a.currentItems(context.Background(), set.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ModelID != modelID || len(item.ModelRefIDs) != 1 || item.WearType != "earring" || item.Route != "manual" || item.ReviewA.Checks["size"] != "fail" {
			t.Fatalf("invalid model result: %+v", item)
		}
	}
}

func TestSizeCompareProgramRatiosAndForcedRoutes(t *testing.T) {
	f := newFixture(t)
	tpl := testTemplate("model")
	tpl.Slot = "main4"
	tpl.Kind = "main"
	set, items := f.makeSet(t, tpl, 2)
	f.exec(t, "UPDATE creative_sets SET params=? WHERE id=?", core.JS(Params{Sizes: []Size{{Label: "small", MM: 8}, {Label: "large", MM: 10}}}), set.ID)
	for index, item := range items {
		review := passingReview()
		review.Checks["size"] = "pass"
		review.Checks["body"] = "pass"
		review.Checks["placement"] = "pass"
		review.SizeMM = []float64{8 + float64(index)}
		hard := Hard{Pixel: PixelCheck{Pass: true}, Format: FormatCheck{Pass: true}}
		f.exec(t, "UPDATE creative_items SET size_compare=1,wear_type='earring',output_asset_id=1,hard=?,review_a=?,review_b=? WHERE id=?", core.JS(hard), core.JS(review), core.JS(review), item.ID)
	}
	if err := f.a.compareSizes(context.Background(), set.ID, 1); err != nil {
		t.Fatal(err)
	}
	current, _ := f.a.currentItems(context.Background(), set.ID)
	for _, item := range current {
		if item.ReviewA.Checks["size"] != "fail" || item.Route != "manual" {
			t.Fatal(item.ReviewA, item.Route)
		}
		forced := f.a.forced(context.Background(), item)
		if !contains(forced, "多尺寸对比强制人工") || !contains(forced, "校准期") {
			t.Fatal(forced)
		}
	}
	f.exec(t, "INSERT INTO creative_calibration(category,tightened,updated_at) VALUES('tight:main4',1,?)", core.Now())
	current[0].SizeCompare = false
	current[0].WearType = ""
	if !contains(f.a.forced(context.Background(), current[0]), "售后收紧") {
		t.Fatal("return tightening not enforced")
	}
}
