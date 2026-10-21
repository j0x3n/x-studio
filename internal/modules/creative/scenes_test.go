package creative

import (
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"xstudio/internal/core"
	"xstudio/internal/modules/scenes"
)

func TestSceneSnapshotIsFixedAcrossGenerationAndRedraw(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	scene := f.request(t, "POST", "/api/scenes", core.Object{"name": "暖色桌面", "still": "米白桌面", "wear": "亚麻衬衫", "light": "柔和日光", "props": "一本书"}, 201)["scene"].(map[string]any)
	sceneID := int64(number(scene["id"]))
	f.request(t, "PUT", "/api/scenes/bindings", core.Object{"listing_ids": []int64{f.listingID}, "scene_id": sceneID}, 200)
	template := testTemplate("none")
	template.Prompt = "背景 {场景}，穿搭 {穿搭}"
	f.mainTemplates(t, map[string]Template{"main1": template})
	created := f.request(t, "POST", fmt.Sprintf("/api/listings/%d/creative", f.listingID), core.Object{"parts": []string{"main"}}, 202)
	setJSON := created["set"].(map[string]any)
	setID := int64(number(setJSON["id"]))
	jobID := int64(number(created["jobs"].([]any)[0].(map[string]any)["id"]))
	if setJSON["scene"].(map[string]any)["name"] != "暖色桌面" {
		t.Fatal(setJSON)
	}
	for _, raw := range setJSON["items"].([]any) {
		item := raw.(map[string]any)
		if item["scene"].(map[string]any)["name"] != "暖色桌面" {
			t.Fatal(item)
		}
		if _, leaked := item["scene_snapshot"]; leaked {
			t.Fatal("快照文件路径出现在接口里")
		}
	}
	f.request(t, "PATCH", fmt.Sprintf("/api/scenes/%d", sceneID), core.Object{"name": "修改后的场景", "still": "黑色背景", "archived": true}, 200)
	if err := os.WriteFile(filepath.Join(f.c.Cfg.PromptDir, "生图-核对.md"), []byte("TEST_REVIEW {{order}} {场景} {穿搭}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.runJob(t, jobID); err != nil {
		t.Fatal(err)
	}
	items, err := f.a.currentItems(ctx, setID)
	if err != nil {
		t.Fatal(err)
	}
	first := items[0]
	if first.SceneSnapshot.Name != "暖色桌面" || !strings.Contains(first.Prompt, "米白桌面。柔和日光。一本书") || !strings.Contains(first.Prompt, "亚麻衬衫。柔和日光") || strings.Contains(first.Prompt, "{场景}") {
		t.Fatal(first)
	}
	f.fake.mu.Lock()
	reviewPrompts := append([]string{}, f.fake.orders...)
	f.fake.mu.Unlock()
	for _, prompt := range reviewPrompts {
		if !strings.Contains(prompt, "米白桌面。柔和日光。一本书") || strings.Contains(prompt, "{穿搭}") {
			t.Fatal(prompt)
		}
	}
	if err = f.run(t, "creative.item", "creative_item", first.ID, 1, core.Object{"note": "换个角度"}); err != nil {
		t.Fatal(err)
	}
	current, err := f.a.currentItems(ctx, setID)
	if err != nil {
		t.Fatal(err)
	}
	var next Item
	for _, item := range current {
		if item.Slot == first.Slot && item.Seq == first.Seq && item.Variant == first.Variant {
			next = item
		}
	}
	if next.Attempt != 2 || core.JS(next.SceneSnapshot) != core.JS(first.SceneSnapshot) || next.Scene.Name != "暖色桌面" {
		t.Fatal(next)
	}
	if stale := f.scalar(t, "SELECT stale FROM assets WHERE id=?", first.OutputAssetID); stale != 1 {
		t.Fatal(stale)
	}
	f.request(t, "POST", itemPath(next.ID, "judge"), core.Object{"verdict": "reject"}, 200)
	f.request(t, "POST", itemPath(next.ID, "judge"), core.Object{}, 400)
	if stale := f.scalar(t, "SELECT stale FROM assets WHERE id=?", next.OutputAssetID); stale != 1 {
		t.Fatal(stale)
	}
	f.request(t, "POST", itemPath(next.ID, "judge"), core.Object{"verdict": ""}, 200)
	if stale := f.scalar(t, "SELECT stale FROM assets WHERE id=?", next.OutputAssetID); stale != 0 {
		t.Fatal(stale)
	}
	f.request(t, "DELETE", fmt.Sprintf("/api/creative/sets/%d", setID), nil, 200)
	for _, item := range current {
		if stale := f.scalar(t, "SELECT stale FROM assets WHERE id=?", item.OutputAssetID); stale != 1 {
			t.Fatal(item.ID, stale)
		}
	}
	if f.scalar(t, "SELECT count(*) FROM creative_sets WHERE id=?", setID) != 0 {
		t.Fatal("套装没有删除")
	}
}
func TestSceneOverrideValidationAndEmptyPlaceholders(t *testing.T) {
	f := newFixture(t)
	f.mainTemplates(t, nil)
	ctx := context.Background()
	if _, err := StartSet(ctx, f.c, f.listingID, 1, []string{"main"}, Params{SceneID: 999}); err == nil || !strings.Contains(err.Error(), "场景不存在") {
		t.Fatal(err)
	}
	f.request(t, "POST", "/api/categories", core.Object{"key": "other", "name": "其他品类", "copy_from": "pearl"}, 201)
	otherCategory := "other"
	created := f.request(t, "POST", "/api/scenes", core.Object{"name": "其他品类场景", "still": "桌面", "categories": []string{otherCategory}}, 201)
	sceneID := int64(number(created["scene"].(map[string]any)["id"]))
	if _, err := StartSet(ctx, f.c, f.listingID, 1, []string{"main"}, Params{SceneID: sceneID}); err == nil || !strings.Contains(err.Error(), "品类") {
		t.Fatal(err)
	}
	f.request(t, "PATCH", fmt.Sprintf("/api/scenes/%d", sceneID), core.Object{"categories": []string{}}, 200)
	result, err := StartSet(ctx, f.c, f.listingID, 1, []string{"main"}, Params{SceneID: sceneID})
	if err != nil {
		t.Fatal(err)
	}
	items, err := f.a.currentItems(ctx, result.SetID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.SceneSnapshot.ID != sceneID {
			t.Fatal(item)
		}
	}
	product, err := f.a.PipelineProduct(f.listingID)
	if err != nil {
		t.Fatal(err)
	}
	template := testTemplate("none")
	template.Prompt = "开始{场景}中间{穿搭}结束"
	item := Item{ProductMode: "none", TemplateSnapshot: template, Seq: 1}
	prompt, err := f.a.generationPrompt(&item, product, Params{})
	if err != nil || !strings.Contains(prompt, "开始中间结束") {
		t.Fatal(prompt, err)
	}
}
func TestSceneReferenceOrderingAndModes(t *testing.T) {
	snapshot := scenes.Snapshot{ID: 1, Refs: []scenes.SnapshotRef{{Use: "wear", Path: "wear1.png"}, {Use: "still", Path: "still1.png"}, {Use: "still", Path: "still2.png"}}}
	for mode, want := range map[string]string{"model": "[\"wear1.png\"]", "redraw": "[\"still1.png\",\"still2.png\"]", "pixel_lock": "[\"still1.png\",\"still2.png\"]", "none": "[\"still1.png\",\"still2.png\"]", "photo": "[]"} {
		if got := core.JS(sceneStylePaths(snapshot, mode)); got != want {
			t.Fatal(mode, got, want)
		}
	}
	f := newFixture(t)
	template := testTemplate("none")
	template.StyleRefs = []string{"must-not-be-read.png"}
	set, items := f.makeSet(t, template, 1)
	sceneRefs := []scenes.SnapshotRef{}
	if err := os.MkdirAll(filepath.Join(f.c.Cfg.DataDir, "scenes"), 0700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		path := filepath.Join(f.c.Cfg.DataDir, "scenes", fmt.Sprintf("scene-style-%d.png", index))
		data, err := pngBytes(image.NewNRGBA(image.Rect(0, 0, 10, 10)))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		sceneRefs = append(sceneRefs, scenes.SnapshotRef{Use: "still", Path: path})
	}
	f.exec(t, "UPDATE creative_items SET scene_snapshot=? WHERE id=?", core.JS(scenes.Snapshot{ID: 1, Name: "三张场景参考", Refs: sceneRefs}), items[0].ID)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal("场景图应优先占满 styleLimit", err)
	}
}
func TestStaleBackfillRunsOnlyOnceAndKeepsPhotos(t *testing.T) {
	f := newFixture(t)
	photoID := f.source(t)
	set, items := f.makeSet(t, testTemplate("none"), 2)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ = f.a.currentItems(context.Background(), set.ID)
	f.exec(t, "UPDATE creative_items SET superseded=1 WHERE id=?", items[0].ID)
	f.exec(t, "UPDATE creative_items SET human='reject' WHERE id=?", items[1].ID)
	f.exec(t, "DELETE FROM settings WHERE key='creative.stale_backfill'")
	if err := f.a.backfillStale(); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if f.scalar(t, "SELECT stale FROM assets WHERE id=?", item.OutputAssetID) != 1 {
			t.Fatal(item)
		}
	}
	f.exec(t, "UPDATE assets SET stale=0 WHERE id=?", items[0].OutputAssetID)
	if err := f.a.backfillStale(); err != nil {
		t.Fatal(err)
	}
	if f.scalar(t, "SELECT stale FROM assets WHERE id=?", items[0].OutputAssetID) != 0 || f.scalar(t, "SELECT stale FROM assets WHERE id=?", photoID) != 0 {
		t.Fatal("回填重复执行或影响实拍")
	}
}
func TestTemplateSeedUpgradeProtectsCustomTemplates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dir := filepath.Join(f.c.Cfg.TemplateDir, "生图模板", "detail")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	template := testTemplate("none")
	template.Key = "upgrade"
	template.Source = "file"
	write := func() {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "upgrade.json"), []byte(core.JS(template)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if err := f.a.seedTemplates(); err != nil {
		t.Fatal(err)
	}
	template.Version = 2
	template.Prompt = "新提示词 {场景}"
	template.StyleRefs = []string{}
	template.Texts = []TextSpec{{Key: "brand", From: "literal:品牌"}}
	write()
	if err := f.a.seedTemplates(); err != nil {
		t.Fatal(err)
	}
	stored, err := f.a.template(ctx, template.Key)
	if err != nil || stored.Version != 2 || stored.Prompt != template.Prompt || stored.Source != "file" || len(stored.Texts) != 1 {
		t.Fatal(stored, err)
	}
	if f.scalar(t, "SELECT count(*) FROM audit_log WHERE action='creative.seed_upgrade'") != 1 {
		t.Fatal("没有升级审计")
	}
	f.request(t, "PATCH", "/api/creative/templates/upgrade", core.Object{"prompt": "人工改过"}, 200)
	template.Version = 99
	template.Prompt = "不应覆盖人工"
	write()
	if err = f.a.seedTemplates(); err != nil {
		t.Fatal(err)
	}
	stored, err = f.a.template(ctx, template.Key)
	if err != nil || stored.Prompt != "人工改过" || stored.Source != "custom" || stored.Version != 3 {
		t.Fatal(stored, err)
	}
}
func TestShopSettingsAffectExportAndAboutPrompt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.request(t, "PATCH", "/api/creative/shops/1/settings", core.Object{"main_ratios": []string{"1x1"}, "detail_width": 600, "about_text": "我们自己挑选、拍照和打包。"}, 200)
	f.request(t, "PATCH", "/api/creative/shops/1/settings", core.Object{"detail_width": 650}, 200)
	settings := f.request(t, "GET", "/api/creative/shops/1/settings", nil, 200)
	if core.JS(settings["main_ratios"]) != `["1x1"]` || settings["about_text"] != "我们自己挑选、拍照和打包。" {
		t.Fatal(settings)
	}
	for _, bad := range []core.Object{{"main_ratios": []string{"16x9"}}, {"main_ratios": []string{"1x1", "1x1"}}, {"detail_width": 479}, {"detail_width": 1501}, {"detail_width": 600.5}, {"about_text": strings.Repeat("中", 301)}} {
		f.request(t, "PATCH", "/api/creative/shops/1/settings", bad, 400)
	}
	template := testTemplate("none")
	template.Kind = "main"
	template.Slot = "main2"
	template.Export = []ExportSize{{Name: "3x4", W: 120, H: 160}, {Name: "1x1", W: 120, H: 120}}
	set, _ := f.makeSet(t, template, 1)
	if err := f.run(t, "creative.main", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ := f.a.currentItems(ctx, set.ID)
	files, _, err := f.a.makeExport(ctx, set, items)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["主图/1x1_120x120/02.jpg"]; !ok {
		t.Fatal(files)
	}
	if _, ok := files["主图/3x4_120x160/02.jpg"]; ok {
		t.Fatal("导出了未选择的比例")
	}
	f.request(t, "PATCH", "/api/creative/shops/1/settings", core.Object{"main_ratios": []string{"3x4"}}, 200)
	items[0].TemplateSnapshot.Export = []ExportSize{{Name: "1x1", W: 120, H: 120}}
	files, _, err = f.a.makeExport(ctx, set, items)
	if err != nil || len(files) != 2 {
		t.Fatal(len(files), err)
	}
	items[0].Slot = "d1"
	items[0].TemplateSnapshot.Export = []ExportSize{{Name: "750", W: 750, H: 0}}
	files, _, err = f.a.makeExport(ctx, set, items)
	if err != nil {
		t.Fatal(err)
	}
	data, ok := files["详情页/650/01.jpg"]
	if !ok {
		t.Fatal(files)
	}
	info, err := jpeg.DecodeConfig(strings.NewReader(string(data)))
	if err != nil || info.Width != 650 || info.Height != 650 {
		t.Fatal(info, err)
	}
	product, err := f.a.PipelineProduct(f.listingID)
	if err != nil {
		t.Fatal(err)
	}
	product.Category.Rules = "### about（关于我们）\n旧品类介绍\n### 合规\n保留规则"
	aboutTemplate := testTemplate("none")
	aboutTemplate.Kind = "detail"
	aboutTemplate.Slot = "d9"
	aboutTemplate.Prompt = "{{category_rules}} {文字}"
	aboutTemplate.Texts = []TextSpec{{Key: "about", From: "literal:旧文案"}}
	aboutItem := Item{Slot: "d9", ProductMode: "none", TemplateSnapshot: aboutTemplate, Seq: 1}
	prompt, err := f.a.generationPrompt(&aboutItem, product, Params{})
	if err != nil || !strings.Contains(prompt, "我们自己挑选、拍照和打包。") || strings.Contains(prompt, "旧品类介绍") || strings.Contains(prompt, "旧文案") || !strings.Contains(prompt, "保留规则") {
		t.Fatal(prompt, err)
	}
}
