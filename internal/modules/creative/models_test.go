package creative

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"xstudio/internal/core"
)

func TestCreativeRecordsFallbackModelAndOriginalSize(t *testing.T) {
	fixture := newFixture(t)
	fixture.source(t)
	fixture.c.Cfg.ImageModel = "gpt-image-2.5-sunburst"
	fixture.fake.failImage.Store(1)
	fixture.fake.returnedSize = "1254x1254"
	set, _ := fixture.makeSet(t, testTemplate("redraw"), 1)
	if err := fixture.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, err := fixture.a.currentItems(context.Background(), set.ID)
	if err != nil {
		t.Fatal(err)
	}
	item := items[0]
	if item.Model != "gpt-image-2.5-flare" || item.ModelReturned != "1254x1254" || item.OutputAssetID == 0 {
		t.Fatal(item)
	}
	output, err := fixture.a.outputImage(item)
	if err != nil || output.Bounds().Dx() != 1024 || output.Bounds().Dy() != 1024 {
		t.Fatal(err)
	}
	response := fixture.request(t, "GET", fmt.Sprintf("/api/creative/sets/%d", set.ID), nil, 200)
	row := response["items"].([]any)[0].(map[string]any)
	if row["model"] != item.Model || row["model_returned"] != item.ModelReturned {
		t.Fatal(row)
	}
	usage, err := fixture.a.one(context.Background(), "SELECT model,sum(units) AS units FROM ai_usage WHERE kind='image'")
	if err != nil || usage["model"] != item.Model || number(usage["units"]) != 1 {
		t.Fatal(usage, err)
	}
	photoSet, _ := fixture.makeSet(t, testTemplate("photo"), 1)
	if err := fixture.run(t, "creative.detail", "creative_set", photoSet.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ = fixture.a.currentItems(context.Background(), photoSet.ID)
	if items[0].OutputAssetID == 0 || items[0].Model != "" || items[0].ModelReturned != "" || fixture.fake.images.Load() != 2 {
		t.Fatal(items)
	}
}

func TestAutomaticRedrawAlternatesModelsButManualUsesPrimary(t *testing.T) {
	fixture := newFixture(t)
	primary, alternate := "gpt-image-2.5-sunburst", "configured-alt"
	fixture.exec(t, "INSERT INTO settings(key,value,updated_at) VALUES('ai_models',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", core.JS(core.Object{"creative": primary, "image_alt": alternate}), core.Now())
	fixture.settings(t, core.Object{"max_auto_redraw": 2})
	review := passingReview()
	review.Checks["style"] = "fail"
	fixture.fake.review = &review
	set, _ := fixture.makeSet(t, testTemplate("none"), 1)
	if err := fixture.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	all, err := objects(context.Background(), fixture.c.DB, "SELECT model,auto_attempt FROM creative_items WHERE set_id=? ORDER BY attempt", set.ID)
	if err != nil || len(all) != 3 {
		t.Fatal(all, err)
	}
	for index, want := range []string{primary, alternate, primary} {
		if all[index]["model"] != want || int(number(all[index]["auto_attempt"])) != index {
			t.Fatal(all)
		}
	}
	fixture.fake.mu.Lock()
	fixture.fake.review = nil
	fixture.fake.mu.Unlock()
	fixture.settings(t, core.Object{"max_auto_redraw": 0})
	for _, judge := range []bool{false, true} {
		items, _ := fixture.a.currentItems(context.Background(), set.ID)
		path, status := itemPath(items[0].ID, "redraw"), 202
		body := core.Object{"note": "人工调整背景"}
		if judge {
			path, status = itemPath(items[0].ID, "judge"), 200
			body["verdict"] = "redraw"
		}
		queued := fixture.request(t, "POST", path, body, status)
		if err := fixture.runJob(t, int64(number(queued["job_id"]))); err != nil {
			t.Fatal(err)
		}
		items, _ = fixture.a.currentItems(context.Background(), set.ID)
		if items[0].Model != primary || items[0].AutoAttempt != 2 {
			t.Fatal(items)
		}
	}
}

func TestAutomaticRedrawUsesActualFallbackAsPreviousModel(t *testing.T) {
	fixture := newFixture(t)
	fixture.c.Cfg.ImageModel = "gpt-image-2.5-sunburst"
	fixture.fake.failImage.Store(1)
	fixture.settings(t, core.Object{"max_auto_redraw": 1})
	review := passingReview()
	review.Checks["style"] = "fail"
	fixture.fake.review = &review
	set, _ := fixture.makeSet(t, testTemplate("none"), 1)
	if err := fixture.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	fixture.fake.mu.Lock()
	models := append([]string{}, fixture.fake.models...)
	fixture.fake.mu.Unlock()
	if core.JS(models) != core.JS([]string{"gpt-image-2.5-sunburst", "gpt-image-2.5-flare", "gpt-image-2.5-sunburst"}) {
		t.Fatal(models)
	}
	items, _ := fixture.a.currentItems(context.Background(), set.ID)
	if items[0].Model != "gpt-image-2.5-sunburst" || items[0].AutoAttempt != 1 {
		t.Fatal(items)
	}
}

func TestCreativeStatsGroupsCurrentVisibleOutputsByModel(t *testing.T) {
	fixture := newFixture(t)
	set, items := fixture.makeSet(t, testTemplate("none"), 6)
	fixture.exec(t, "UPDATE creative_items SET model='primary',output_asset_id=1 WHERE set_id=?", set.ID)
	fixture.exec(t, "UPDATE creative_items SET route='auto_pass' WHERE id=?", items[0].ID)
	fixture.exec(t, "UPDATE creative_items SET human='pass' WHERE id=?", items[1].ID)
	fixture.exec(t, "UPDATE creative_items SET model='alternate',human='discard' WHERE id=?", items[2].ID)
	fixture.exec(t, "UPDATE creative_items SET superseded=1 WHERE id=?", items[3].ID)
	fixture.exec(t, "UPDATE creative_items SET output_asset_id=0 WHERE id=?", items[4].ID)
	fixture.exec(t, "UPDATE creative_items SET model='' WHERE id=?", items[5].ID)
	response := fixture.request(t, "GET", "/api/creative/stats", nil, 200)
	rows, err := typed[[]core.Object](response["by_model"])
	if err != nil || len(rows) != 3 || response["categories"] == nil || response["suggestions"] == nil {
		t.Fatal(response, err)
	}
	wants := map[string]core.Object{
		"primary":   {"model": "primary", "total": 2, "auto_pass": 1, "human_pass": 1, "discard": 0},
		"alternate": {"model": "alternate", "total": 1, "auto_pass": 0, "human_pass": 0, "discard": 1},
		"":          {"model": "", "total": 1, "auto_pass": 0, "human_pass": 0, "discard": 0},
	}
	for _, row := range rows {
		if core.JS(row) != core.JS(wants[stringValue(row["model"])]) {
			t.Fatal(row)
		}
	}
	fixture.exec(t, "INSERT INTO roles(key,name,builtin,scope,perms,created_at) VALUES('model_stats','test',0,'shops',?,?)", core.JS([]string{core.CreativeRun, core.CreativeReview}), core.Now())
	roleID := fixture.scalar(t, "SELECT id FROM roles WHERE key='model_stats'")
	fixture.request(t, "POST", "/api/users", core.Object{"username": "limited", "password": "password123", "role_id": roleID}, 201)
	fixture.request(t, "POST", "/api/login", core.Object{"username": "limited", "password": "password123"}, 200)
	response = fixture.request(t, "GET", "/api/creative/stats", nil, 200)
	if len(response["by_model"].([]any)) != 0 {
		t.Fatal("model statistics leaked other shops", response)
	}
}

func TestRedrawHeroBReordersPhotoAndSinglePhotoAddsAnglePrompt(t *testing.T) {
	fixture := newFixture(t)
	first := fixture.source(t)
	second := fixture.badSource(t, "A2")
	template := testTemplate("redraw")
	template.PhotoCodes = []string{"A1", "A2"}
	set, items := fixture.makeSet(t, template, 2)
	fixture.exec(t, "UPDATE creative_items SET variant='A' WHERE id=?", items[0].ID)
	fixture.exec(t, "UPDATE creative_items SET variant='B' WHERE id=?", items[1].ID)
	if err := fixture.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ = fixture.a.currentItems(context.Background(), set.ID)
	if core.JS(items[0].SourceAssetIDs) != core.JS([]int64{first, second}) || core.JS(items[1].SourceAssetIDs) != core.JS([]int64{second, first}) || items[0].OutputAssetID == 0 || items[1].OutputAssetID == 0 {
		t.Fatal(items)
	}
	fixture.fake.mu.Lock()
	colors := append([]uint8{}, fixture.fake.firstReference...)
	fixture.fake.mu.Unlock()
	if len(colors) != 2 || colors[0] == colors[1] || colors[1] != 20 {
		t.Fatal("provider did not receive the other photo first", colors)
	}
	template.PhotoCodes = []string{"A1"}
	set, items = fixture.makeSet(t, template, 1)
	fixture.exec(t, "UPDATE creative_items SET variant='B' WHERE id=?", items[0].ID)
	if err := fixture.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ = fixture.a.currentItems(context.Background(), set.ID)
	if core.JS(items[0].SourceAssetIDs) != core.JS([]int64{first}) || !strings.HasSuffix(items[0].Prompt, "Show the product from a different angle than variant A.") {
		t.Fatal(items)
	}
}
