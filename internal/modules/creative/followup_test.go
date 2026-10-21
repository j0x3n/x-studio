package creative

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"xstudio/internal/core"
)

func TestCreativeResponseImagesJobsAndStats(t *testing.T) {
	f := newFixture(t)
	aid := f.source(t)
	tpl := testTemplate("photo")
	set, _ := f.makeSet(t, tpl, 2)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ := f.a.currentItems(context.Background(), set.ID)
	f.exec(t, "UPDATE creative_items SET route='manual',wear_type='earring',human='pass' WHERE id=?", items[0].ID)
	f.exec(t, "UPDATE creative_items SET wear_type='earring' WHERE id=?", items[1].ID)
	f.exec(t, "INSERT INTO creative_calibration(category,total,agree,open,updated_at) VALUES('earring',10,9,0,?)", core.Now())
	f.exec(t, "INSERT INTO creative_suggestions(listing_id,set_id,slot,reason_code,count,created_at,updated_at) VALUES(?,?,'d1','smaller',2,?,?)", f.listingID, set.ID, core.Now(), core.Now())
	stats := f.request(t, "GET", "/api/creative/stats", nil, 200)
	if len(stats) != 2 || stats["categories"] == nil || stats["suggestions"] == nil {
		t.Fatal(stats)
	}
	for _, value := range stats["categories"].([]any) {
		row := value.(map[string]any)
		for _, key := range []string{"total", "agree", "open", "tightened", "tightened_reason", "auto_pass_rate", "override_rate", "return_links"} {
			if _, exists := row[key]; !exists {
				t.Fatalf("missing %s: %v", key, row)
			}
		}
		if row["category"] == "earring" && (number(row["auto_pass_rate"]) != 0.5 || number(row["override_rate"]) != 0 || number(row["return_links"]) != 2) {
			t.Fatal(row)
		}
	}
	if stats["suggestions"].([]any)[0].(map[string]any)["product_name"] != "离线测试珍珠耳钉" {
		t.Fatal(stats)
	}
	f.exec(t, "UPDATE creative_items SET route='manual',human='' WHERE id=?", items[1].ID)
	queue := f.request(t, "GET", "/api/creative/review", nil, 200)
	row := queue["items"].([]any)[0].(map[string]any)
	checkItem := func(row map[string]any) {
		t.Helper()
		if !strings.HasPrefix(stringValue(row["output_url"]), fmt.Sprintf("/files/products/%d/", f.productID)) || len(row["source_urls"].([]any)) != 1 || number(row["listing_id"]) != float64(f.listingID) || number(row["product_id"]) != float64(f.productID) || number(row["shop_id"]) != 1 || row["product_name"] != "离线测试珍珠耳钉" {
			t.Fatal(row)
		}
		for _, key := range []string{"superseded", "size_compare"} {
			if _, ok := row[key].(bool); !ok {
				t.Fatalf("%s is not boolean: %v", key, row[key])
			}
		}
		if row["source_urls"].([]any)[0] != fmt.Sprintf("/files/products/%d/原图/source.png", f.productID) {
			t.Fatal(row)
		}
	}
	checkItem(row)
	if number(row["source_asset_ids"].([]any)[0]) != float64(aid) {
		t.Fatal(row)
	}
	f.exec(t, "UPDATE creative_items SET superseded=1 WHERE id=?", items[0].ID)
	f.exec(t, "INSERT INTO jobs(target_id,target_type,user_id,kind,status,created_at,steps) VALUES(?,'creative_item',1,'creative.verify','queued',?,'')", items[1].ID, core.Now())
	listing := f.request(t, "GET", fmt.Sprintf("/api/listings/%d/creative", f.listingID), nil, 200)
	sets := listing["items"].([]any)
	if number(listing["total"]) != 1 || len(sets) != 1 {
		t.Fatal(listing)
	}
	listed := sets[0].(map[string]any)
	if len(listed["items"].([]any)) != 2 || len(listed["jobs"].([]any)) != 2 {
		t.Fatal(listed)
	}
	for _, value := range listed["items"].([]any) {
		checkItem(value.(map[string]any))
	}
	if listed["items"].([]any)[0].(map[string]any)["superseded"] != true {
		t.Fatal("superseded item omitted", listed)
	}
	for _, value := range listed["jobs"].([]any) {
		if len(value.(map[string]any)) != 5 {
			t.Fatal(value)
		}
	}
	detail := f.request(t, "GET", fmt.Sprintf("/api/creative/sets/%d", set.ID), nil, 200)
	checkItem(detail["items"].([]any)[1].(map[string]any))
	if err := f.a.storeTemplate(context.Background(), tpl, 1); err != nil {
		t.Fatal(err)
	}
	trial := f.request(t, "POST", "/api/creative/templates/"+tpl.Key+"/trial", core.Object{"listing_id": f.listingID}, 202)
	trialItem := trial["item"].(map[string]any)
	if trialItem["output_url"] != "" || len(trialItem["source_urls"].([]any)) != 0 || trialItem["superseded"] != false || trialItem["size_compare"] != false {
		t.Fatal(trial)
	}
}

func TestTemplateSelectionDefaultsOverridesAndCategory(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	product, err := f.a.PipelineProduct(f.listingID)
	if err != nil {
		t.Fatal(err)
	}
	add := func(key, slot, mode, style string, compare, enabled bool, categories ...string) {
		t.Helper()
		tpl := testTemplate(mode)
		tpl.Key, tpl.Slot, tpl.HeroStyle, tpl.SizeCompare, tpl.Enabled, tpl.Categories = key, slot, style, compare, enabled, categories
		if strings.HasPrefix(slot, "main") {
			tpl.Kind = "main"
		}
		if err := f.a.storeTemplate(ctx, tpl, 1); err != nil {
			t.Fatal(err)
		}
	}
	for _, slot := range []string{"main2", "main3", "main5", "d3", "d4", "d5", "d7", "d8", "d9"} {
		add(slot, slot, "none", "", false, true)
	}
	add("main_hero_stone", "main1", "pixel_lock", "", false, true)
	add("aaa_hero_model", "main1", "model", "", false, true)
	add("a_compare_disabled", "main4", "model", "", true, false)
	add("b_compare", "main4", "model", "", true, true)
	add("z_compare", "main4", "model", "", true, true)
	add("a_ruler", "main4", "photo", "", false, true)
	add("a_vertical", "d1", "pixel_lock", "vertical", false, true)
	add("a_window_disabled", "d1", "pixel_lock", "window", false, false)
	add("b_window", "d1", "pixel_lock", "window", false, true)
	add("z_window", "d1", "pixel_lock", "window", false, true)
	add("a_wrong_category", "d2", "pixel_lock", "", false, true, "other")
	add("b_category", "d2", "pixel_lock", "", false, true, product.Category.Key)
	add("z_category", "d2", "pixel_lock", "", false, true, product.Category.Key)
	add("d6_disabled", "d6", "none", "", false, false)
	selectKeys := func(params Params) map[string]string {
		t.Helper()
		templates, err := f.a.selectTemplates(ctx, product, []string{"main", "detail"}, params)
		if err != nil {
			t.Fatal(err)
		}
		keys := map[string]string{}
		for _, tpl := range templates {
			keys[tpl.Slot] = tpl.Key
		}
		return keys
	}
	keys := selectKeys(Params{HeroStyle: "window"})
	if keys["main1"] != "main_hero_stone" || keys["main4"] != "a_ruler" || keys["d1"] != "b_window" || keys["d2"] != "b_category" || keys["d6"] != "" {
		t.Fatal(keys)
	}
	keys = selectKeys(Params{HeroStyle: "vertical", Sizes: []Size{{MM: 8}, {MM: 10}}})
	if keys["main4"] != "b_compare" || keys["d1"] != "a_vertical" {
		t.Fatal(keys)
	}
	f.exec(t, "INSERT INTO creative_shop_settings(shop_id,template_set,updated_at) VALUES(1,?,?)", core.JS(core.Object{"main1": "aaa_hero_model", "main4": "z_compare", "d1": "z_window"}), core.Now())
	keys = selectKeys(Params{HeroStyle: "vertical", Sizes: []Size{{MM: 8}, {MM: 10}}})
	if keys["main1"] != "aaa_hero_model" || keys["main4"] != "z_compare" || keys["d1"] != "z_window" {
		t.Fatal(keys)
	}
	f.exec(t, "UPDATE creative_shop_settings SET template_set=? WHERE shop_id=1", core.JS(core.Object{"main4": "a_compare_disabled"}))
	if _, err := f.a.selectTemplates(ctx, product, []string{"main"}, Params{}); err == nil {
		t.Fatal("disabled explicit template selected")
	}
}

func TestIndexedTextAndPromptSections(t *testing.T) {
	f := newFixture(t)
	product, err := f.a.PipelineProduct(f.listingID)
	if err != nil {
		t.Fatal(err)
	}
	product.Content = core.Object{"details": []any{core.Object{"title": "第一张"}, core.Object{"title": "第二张"}}}
	tpl := testTemplate("none")
	tpl.Prompt = "SCREEN {i} {文字}"
	tpl.Texts = []TextSpec{{Key: "title", From: "content.details.{i}.title"}, {Key: "mm", From: "size.label"}}
	item := Item{TemplateSnapshot: tpl, ProductMode: "none", Seq: 2, SizeCompare: true, Prompt: "文字太小"}
	prompt, err := f.a.generationPrompt(&item, product, Params{Sizes: []Size{{Label: "6–7 mm", MM: 6.5}, {Label: "8–9 mm", MM: 8.5}}})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"第二张", "8–9 mm"}
	if len(item.Texts) != len(expected) {
		t.Fatal(item.Texts)
	}
	for index, want := range expected {
		if item.Texts[index].Text != want {
			t.Fatalf("%d: %q != %q", index, item.Texts[index].Text, want)
		}
	}
	for _, want := range []string{"TEST_DETAIL", "NONE", "SCREEN 2", "TEXT 【title】“第二张”", "FIX 文字太小", "Numbers and units must be exactly as given."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "NEVER_SEND_RULES") || strings.Contains(prompt, "TEST_MAIN") || strings.Contains(prompt, "{i}") {
		t.Fatal(prompt)
	}
	if strings.Index(prompt, "TEST_DETAIL") > strings.Index(prompt, "NONE") || strings.Index(prompt, "NONE") > strings.Index(prompt, "SCREEN 2") || strings.Index(prompt, "SCREEN 2") > strings.Index(prompt, "TEXT 【title】") {
		t.Fatal("incorrect section order", prompt)
	}
	main := "## 通用要求\nMAIN_COMMON {{category_name}}. No text, no logo, no watermark, no border, no collage unless this prompt explicitly asks for text.\n## none 不放商品\nMAIN_NONE\n## 上一次的问题\nFIX {问题}\n## 规则（后端照做，不给模型看）\nNEVER_SEND_RULES"
	if err = os.WriteFile(filepath.Join(f.c.Cfg.PromptDir, "生图-主图.md"), []byte(main), 0600); err != nil {
		t.Fatal(err)
	}
	item.TemplateSnapshot.Kind = "main"
	item.Variant = "B"
	item.Prompt = "调整排版"
	prompt, err = f.a.generationPrompt(&item, product, Params{})
	if err != nil || !strings.Contains(prompt, "MAIN_COMMON "+product.Category.Name) || !strings.Contains(prompt, "Render the following Chinese text exactly") || !strings.Contains(prompt, "alternative version B") || !strings.Contains(prompt, "FIX 调整排版") || strings.Contains(prompt, "No text,") || strings.Contains(prompt, "NEVER_SEND_RULES") {
		t.Fatal(prompt, err)
	}
}

func TestTextArraysRenderAsLines(t *testing.T) {
	product := core.Product{Content: core.Object{"care": []string{"避开香水", "擦干收好"}, "params": [][]string{{"珠径", "8 mm"}, {"材质", "925 银"}}}}
	tpl := testTemplate("none")
	tpl.Texts = []TextSpec{{Key: "care", From: "content.care"}, {Key: "params", From: "content.params"}}
	texts := makeTexts(tpl, product, 1, nil)
	if len(texts) != 2 || texts[0].Text != "避开香水\n擦干收好" || texts[1].Text != "珠径 8 mm\n材质 925 银" || !texts[1].Critical {
		t.Fatal(texts)
	}
}

func TestRectLockCoversPhotoAndRepastesSquareExport(t *testing.T) {
	f := newFixture(t)
	f.source(t)
	var cutoutCalls atomic.Int64
	cutout := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { cutoutCalls.Add(1); w.WriteHeader(503) }))
	defer cutout.Close()
	f.c.Cfg.RembgURL = cutout.URL
	tpl := testTemplate("pixel_lock")
	tpl.LockShape = "rect"
	tpl.GenSize = "1024x1536"
	tpl.ProductBox = []float64{0.2, 0.25, 0.6, 0.25}
	tpl.Export = []ExportSize{{Name: "1x1", W: 1440, H: 1440}}
	set, _ := f.makeSet(t, tpl, 1)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ := f.a.currentItems(context.Background(), set.ID)
	item := items[0]
	if cutoutCalls.Load() != 0 || !item.Hard.Pixel.Pass || item.Hard.Pixel.MaxDiff > 2 {
		t.Fatal(cutoutCalls.Load(), item.Hard)
	}
	base := strings.TrimSuffix(item.CanvasFile, ".png")
	root := f.c.ListingDir(item.ListingID)
	canvas, err := loadImage(root, item.CanvasFile)
	if err != nil {
		t.Fatal(err)
	}
	maskImage, err := loadImage(root, base+"-mask.png")
	if err != nil {
		t.Fatal(err)
	}
	mask := alphaImage(maskImage)
	area := image.Rect(204, 384, 819, 768)
	for y := 0; y < 1536; y++ {
		for x := 0; x < 1024; x++ {
			want := uint8(0)
			if image.Pt(x, y).In(area) {
				want = 255
			}
			if mask.AlphaAt(x, y).A != want {
				t.Fatalf("mask (%d,%d): %d != %d", x, y, mask.AlphaAt(x, y).A, want)
			}
		}
	}
	if canvas.NRGBAAt(area.Min.X+10, area.Min.Y+10) != (color.NRGBA{255, 255, 255, 255}) {
		t.Fatal("photo background was cut out")
	}
	source, err := loadImage(root, base+"-source.png")
	if err != nil {
		t.Fatal(err)
	}
	sourceMask, err := loadImage(root, base+"-source-mask.png")
	if err != nil {
		t.Fatal(err)
	}
	if sourceBox, destination := lockGeometry(source, alphaImage(sourceMask), tpl.ProductBox, 1024, 1536, "rect"); destination != area || sourceBox.Dy() >= source.Rect.Dy() {
		t.Fatal("rect did not cover-crop photograph", sourceBox, destination)
	}
	output, _ := f.a.outputImage(item)
	if output.NRGBAAt(512, 576) != canvas.NRGBAAt(512, 576) {
		t.Fatal("generated image changed locked product")
	}
	exported, width, height, err := f.a.exportImage(item, tpl.Export[0])
	if err != nil || width != 1440 || height != 1440 {
		t.Fatal(width, height, err)
	}
	_, check := exportLocked(output, source, alphaImage(sourceMask), tpl.ProductBox, 1440, 1440, "rect")
	if !check.Pass || check.MaxDiff > 2 || exported.Rect != image.Rect(0, 0, 1440, 1440) {
		t.Fatal(check)
	}
	f.request(t, "POST", setPath(set.ID, "export"), core.Object{}, 200)
	var at string
	if err = f.c.DB.QueryRow("SELECT exported_at FROM creative_sets WHERE id=?", set.ID).Scan(&at); err != nil || len(at) != 30 {
		t.Fatal(at, err)
	}
	parsed, err := time.Parse(exportTimeLayout, at)
	if err != nil || parsed.UTC().Format(exportTimeLayout) != at {
		t.Fatal(at, err)
	}
}

func TestAutoBodyPartMappingAndExtendedTemplateSeeds(t *testing.T) {
	for wearType, want := range map[string]string{"necklace": "neck", "earring": "ear", "bracelet": "wrist", "ring": "hand"} {
		body, err := resolveBodyPart(&Wear{BodyPart: "auto"}, wearType)
		if err != nil || body != want {
			t.Fatal(wearType, body, err)
		}
	}
	if _, err := resolveBodyPart(&Wear{BodyPart: "auto"}, ""); err == nil {
		t.Fatal("unknown body part accepted")
	}
	f := newFixture(t)
	tpl := testTemplate("model")
	tpl.Wear = &Wear{Slot: "female_main", BodyPart: "auto", Framing: "closeup"}
	tpl.SizeCompare, tpl.HeroStyle, tpl.LockShape = true, "window", "rect"
	dir := filepath.Join(f.c.Cfg.TemplateDir, "生图模板", "detail")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auto.json"), []byte(core.JS(tpl)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.a.seedTemplates(); err != nil {
		t.Fatal(err)
	}
	stored, err := f.a.template(context.Background(), tpl.Key)
	if err != nil || stored.Wear.BodyPart != "auto" || !stored.SizeCompare || stored.HeroStyle != "window" || stored.LockShape != "rect" {
		t.Fatal(stored, err)
	}
	f.exec(t, "UPDATE creative_templates SET size_compare=NULL,hero_style='',lock_shape='' WHERE key=?", tpl.Key)
	if err := f.a.seedTemplates(); err != nil {
		t.Fatal(err)
	}
	stored, _ = f.a.template(context.Background(), tpl.Key)
	if !stored.SizeCompare || stored.HeroStyle != "window" || stored.LockShape != "rect" {
		t.Fatal(stored)
	}
	f.exec(t, "UPDATE creative_templates SET size_compare=0,hero_style='album',lock_shape='cutout' WHERE key=?", tpl.Key)
	if err := f.a.seedTemplates(); err != nil {
		t.Fatal(err)
	}
	stored, _ = f.a.template(context.Background(), tpl.Key)
	if stored.SizeCompare || stored.HeroStyle != "album" || stored.LockShape != "cutout" {
		t.Fatal("seed overwrote nonempty fields", stored)
	}
}

func TestMigrationNormalizesExportTimeForOrderComparison(t *testing.T) {
	f := newFixture(t)
	set, _ := f.makeSet(t, testTemplate("none"), 1)
	tx, err := f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`ALTER TABLE creative_templates DROP COLUMN lock_shape;
ALTER TABLE creative_templates DROP COLUMN size_compare;
ALTER TABLE creative_templates DROP COLUMN hero_style;`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec("UPDATE creative_sets SET exported_at='2026-10-21T12:00:00Z' WHERE id=?", set.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.a.Migrations()[1].Up(tx); err != nil {
		t.Fatal(err)
	}
	var at string
	if err = tx.QueryRow("SELECT exported_at FROM creative_sets WHERE id=?", set.ID).Scan(&at); err != nil || at != "2026-10-21T12:00:00.000000000Z" {
		t.Fatal(at, err)
	}
	var eligible bool
	if err = tx.QueryRow("SELECT exported_at<=? FROM creative_sets WHERE id=?", "2026-10-21T12:00:00.100000000Z", set.ID).Scan(&eligible); err != nil || !eligible {
		t.Fatal("order comparison rejected earlier export", eligible, err)
	}
}
