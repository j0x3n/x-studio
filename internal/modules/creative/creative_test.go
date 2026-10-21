package creative

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"xstudio/internal/core"
)

func TestPixelLockRestoresProviderChangesAndExport(t *testing.T) {
	f := newFixture(t)
	f.source(t)
	tpl := testTemplate("pixel_lock")
	tpl.Export = []ExportSize{{Name: "3x4", W: 1440, H: 1920}}
	tpl.GenSize = "1024x1536"
	set, _ := f.makeSet(t, tpl, 1)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, err := f.a.currentItems(context.Background(), set.ID)
	if err != nil {
		t.Fatal(err)
	}
	item := items[0]
	if item.Hard.Pixel.MaxDiff > 2 || !item.Hard.Pixel.Pass || item.Route != "auto_pass" {
		t.Fatalf("pixel lock failed: %+v", item)
	}
	output, err := f.a.outputImage(item)
	if err != nil {
		t.Fatal(err)
	}
	if output.NRGBAAt(512, 768).R == 210 {
		t.Fatal("provider modification survived product paste")
	}
	exported, width, height, err := f.a.exportImage(item, tpl.Export[0])
	if err != nil || width != 1440 || height != 1920 || exported == nil {
		t.Fatal(width, height, err)
	}
	base := strings.TrimSuffix(item.CanvasFile, ".png")
	source, err := loadImage(f.c.ListingDir(item.ListingID), base+"-source.png")
	if err != nil {
		t.Fatal(err)
	}
	mask, err := loadImage(f.c.ListingDir(item.ListingID), base+"-source-mask.png")
	if err != nil {
		t.Fatal(err)
	}
	_, check := exportLocked(output, source, alphaImage(mask), tpl.ProductBox, 1440, 1920)
	if !check.Pass || check.MaxDiff > 2 {
		t.Fatal(check)
	}
	f.request(t, "POST", setPath(set.ID, "export"), core.Object{}, 200)
	fake := f.fake
	fake.mu.Lock()
	orders := append([]string{}, fake.orders...)
	fake.mu.Unlock()
	if len(orders) != 2 || !strings.Contains(orders[0], "最后一张") || !strings.Contains(orders[1], "第一张") {
		t.Fatal(orders)
	}
}

func TestCriticalTextBlocksExportUntilRetext(t *testing.T) {
	f := newFixture(t)
	tpl := testTemplate("none")
	tpl.Texts = []TextSpec{{Key: "size", From: "literal:8 mm", Critical: false}, {Key: "title", From: "literal:珍珠", Critical: false}}
	f.fake.read = []OCRText{{Text: "7mm", Box: []float64{0.1, 0.1, 0.3, 0.1}}}
	set, _ := f.makeSet(t, tpl, 1)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ := f.a.currentItems(context.Background(), set.ID)
	item := items[0]
	if !item.Texts[0].Critical || item.Hard.Text[0].Pass || !item.Hard.Text[1].Warn {
		t.Fatalf("wrong text checks: %+v", item.Hard)
	}
	f.request(t, "POST", itemPath(item.ID, "judge"), core.Object{"verdict": "pass"}, 200)
	blocked := f.request(t, "POST", setPath(set.ID, "export"), core.Object{}, 409)
	if !strings.Contains(stringValue(blocked["error"]), "关键文字未核对通过") {
		t.Fatal(blocked)
	}
	f.request(t, "POST", itemPath(item.ID, "retext"), core.Object{"key": "unknown", "text": "8mm"}, 400)
	f.fake.mu.Lock()
	f.fake.read = []OCRText{{Text: "8mm", Box: []float64{0.1, 0.1, 0.3, 0.1}}}
	f.fake.mu.Unlock()
	queued := f.request(t, "POST", itemPath(item.ID, "retext"), core.Object{"key": "size", "text": "8 mm"}, 202)
	if err := f.runJob(t, int64(number(queued["job_id"]))); err != nil {
		t.Fatal(err)
	}
	items, _ = f.a.currentItems(context.Background(), set.ID)
	if !items[0].Hard.Text[0].Pass || !items[0].Hard.Text[1].Warn {
		t.Fatal(items[0].Hard)
	}
	if !strings.Contains(items[0].Prompt, "OLD 7mm") || !strings.Contains(items[0].Prompt, "Numbers and units must be exactly as given.") || strings.Contains(items[0].Prompt, "NEVER_SEND_RULES") || strings.Contains(items[0].Prompt, "{{") {
		t.Fatal(items[0].Prompt)
	}
	f.request(t, "POST", setPath(set.ID, "export"), core.Object{}, 200)
	res, err := f.client.Get(f.server.URL + setPath(set.ID, "export"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode, string(data))
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range reader.File {
		if file.Name == "详情页/750/01.jpg" {
			stream, _ := file.Open()
			cfg, _, err := image.DecodeConfig(stream)
			stream.Close()
			if err != nil || cfg.Width != 750 || cfg.Height != 750 {
				t.Fatal(cfg, err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("export missing detail image")
	}
}

func TestRoutingForcedDisagreementRedrawAndSampling(t *testing.T) {
	settings := core.Object{}
	for key, value := range core.SettingDefaults["creative"] {
		settings[key] = value
	}
	settings["spot_check_rate"] = 0
	pass := passingReview()
	base := Item{Slot: "d1", Hard: Hard{Pixel: PixelCheck{Pass: true}, Format: FormatCheck{Pass: true}}, ReviewA: pass, ReviewB: pass}
	for _, reason := range []string{"首图强制人工", "多尺寸对比强制人工", "校准期", "售后收紧"} {
		route, _, _ := routeItem(base, settings, []string{reason}, true, 0.5)
		if route != "manual" {
			t.Fatal(reason, route)
		}
	}
	route, _, _ := routeItem(base, settings, nil, true, 0.5)
	if route != "auto_pass" {
		t.Fatal(route)
	}
	settings["spot_check_rate"] = 1
	route, _, _ = routeItem(base, settings, nil, true, 0.99)
	if route != "manual" {
		t.Fatal(route)
	}
	settings["spot_check_rate"] = 0
	disagree := base
	disagree.ReviewB = unsureReview()
	route, _, _ = routeItem(disagree, settings, nil, true, 0.5)
	if route != "manual" {
		t.Fatal(route)
	}
	failReview := passingReview()
	failReview.Checks["style"] = "fail"
	failed := base
	failed.ReviewA = failReview
	failed.ReviewB = failReview
	for attempt := 0; attempt <= 2; attempt++ {
		failed.AutoAttempt = attempt
		route, _, _ = routeItem(failed, settings, []string{"首图强制人工"}, true, 0.5)
		want := "auto_redraw"
		if attempt == 2 {
			want = "manual"
		}
		if route != want {
			t.Fatal(attempt, route)
		}
	}
	f := newFixture(t)
	f.settings(t, core.Object{"max_auto_redraw": 2})
	f.fake.review = &failReview
	tpl := testTemplate("none")
	tpl.Kind = "main"
	tpl.Slot = "main1"
	set, _ := f.makeSet(t, tpl, 1)
	if err := f.run(t, "creative.main", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	items, _ := f.a.currentItems(context.Background(), set.ID)
	if f.fake.images.Load() != 3 || items[0].AutoAttempt != 2 || items[0].Route != "manual" {
		t.Fatal(f.fake.images.Load(), items)
	}
	if err := f.run(t, "creative.item", "creative_item", items[0].ID, 1, core.Object{"note": "人工重画"}); err != nil {
		t.Fatal(err)
	}
	items, _ = f.a.currentItems(context.Background(), set.ID)
	if f.fake.images.Load() != 4 || items[0].AutoAttempt != 2 {
		t.Fatal("manual redraw reset automatic redraw budget", f.fake.images.Load(), items[0].AutoAttempt)
	}
}

func TestCalibrationOpensAndClosesOnAgreement(t *testing.T) {
	f := newFixture(t)
	settings := f.a.settings()
	settings["calibration_count"] = 2
	settings["calibration_agreement"] = 0.95
	review := passingReview()
	item := Item{ProductMode: "model", WearType: "earring", Hard: Hard{Pixel: PixelCheck{Pass: true}, Format: FormatCheck{Pass: true}}, ReviewA: review, ReviewB: review}
	for index, verdict := range []string{"pass", "pass", "discard"} {
		tx, err := f.c.DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err = f.a.calibrate(context.Background(), tx, item, verdict, settings); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		var total, agree int
		var open bool
		if err = f.c.DB.QueryRow("SELECT total,agree,open FROM creative_calibration WHERE category='earring'").Scan(&total, &agree, &open); err != nil {
			t.Fatal(err)
		}
		if total != index+1 || agree != min(index+1, 2) || open != (index == 1) {
			t.Fatal(total, agree, open)
		}
	}
}

func TestImageLimitPausesBeforeAIAndContinueResumes(t *testing.T) {
	f := newFixture(t)
	set, _ := f.makeSet(t, testTemplate("none"), 2)
	f.exec(t, "UPDATE creative_sets SET images_used=39 WHERE id=?", set.ID)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, 1, core.Object{}); err != nil {
		t.Fatal(err)
	}
	paused, err := f.a.set(context.Background(), set.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != "paused_limit" || paused.ImagesUsed != 40 || f.fake.images.Load() != 1 {
		t.Fatal(paused, f.fake.images.Load())
	}
	queued := f.request(t, "POST", setPath(set.ID, "continue"), core.Object{}, 202)
	if err = f.runJob(t, int64(number(queued["job_id"]))); err != nil {
		t.Fatal(err)
	}
	continued, _ := f.a.set(context.Background(), set.ID)
	if continued.ImagesAllowance != 80 || continued.ImagesUsed != 41 || f.fake.images.Load() != 2 {
		t.Fatal(continued, f.fake.images.Load())
	}
	current, _ := f.a.currentItems(context.Background(), set.ID)
	f.exec(t, "UPDATE creative_sets SET images_used=80 WHERE id=?", set.ID)
	redraw := f.request(t, "POST", itemPath(current[0].ID, "redraw"), core.Object{"note": "保留补充要求"}, 202)
	if err = f.runJob(t, int64(number(redraw["job_id"]))); err != nil {
		t.Fatal(err)
	}
	if f.fake.images.Load() != 2 {
		t.Fatal("paused single-item job called AI")
	}
	resume := f.request(t, "POST", setPath(set.ID, "continue"), core.Object{}, 202)
	if err = f.runJob(t, int64(number(resume["job_id"]))); err != nil {
		t.Fatal(err)
	}
	current, _ = f.a.currentItems(context.Background(), set.ID)
	if f.fake.images.Load() != 3 || !strings.Contains(current[0].Prompt, "保留补充要求") {
		t.Fatal("single-item continuation lost the request", current[0].Prompt)
	}
}

func TestCreativePermissionScopeAndJobRevocation(t *testing.T) {
	f := newFixture(t)
	set, _ := f.makeSet(t, testTemplate("none"), 1)
	f.exec(t, "INSERT INTO roles(key,name,builtin,scope,perms,created_at) VALUES('creative_test','test',0,'shops','[]',?)", core.Now())
	var roleID int64
	_ = f.c.DB.QueryRow("SELECT id FROM roles WHERE key='creative_test'").Scan(&roleID)
	user := f.request(t, "POST", "/api/users", core.Object{"username": "operator", "password": "password123", "role_id": roleID, "shop_ids": []int64{1}}, 201)
	uid := int64(number(user["user"].(map[string]any)["id"]))
	f.request(t, "POST", "/api/login", core.Object{"username": "operator", "password": "password123"}, 200)
	f.request(t, "POST", fmt.Sprintf("/api/listings/%d/creative", f.listingID), core.Object{"parts": []string{"detail"}}, 403)
	f.exec(t, "UPDATE roles SET perms=? WHERE id=?", core.JS([]string{core.CreativeRun}), roleID)
	f.exec(t, "DELETE FROM user_shops WHERE user_id=?", uid)
	f.request(t, "GET", fmt.Sprintf("/api/creative/sets/%d", set.ID), nil, 404)
	f.exec(t, "UPDATE roles SET scope='shares' WHERE id=?", roleID)
	f.request(t, "GET", "/api/creative/templates", nil, 403)
	f.exec(t, "UPDATE roles SET scope='shops' WHERE id=?", roleID)
	f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(?,1)", uid)
	f.exec(t, "UPDATE roles SET perms='[]' WHERE id=?", roleID)
	if err := f.run(t, "creative.detail", "creative_set", set.ID, uid, core.Object{}); err == nil {
		t.Fatal("revoked queued job ran")
	}
	if f.fake.images.Load() != 0 {
		t.Fatal("revoked job called AI")
	}
	f.exec(t, "UPDATE roles SET perms=? WHERE id=?", core.JS([]string{core.CreativeRun}), roleID)
	f.fake.before = func() {
		_, err := f.c.DB.Exec("UPDATE roles SET perms='[]' WHERE id=?", roleID)
		if err != nil {
			t.Error(err)
		}
	}
	if err := f.run(t, "creative.detail", "creative_set", set.ID, uid, core.Object{}); err == nil {
		t.Fatal("revoked result published")
	}
	var outputs int
	_ = f.c.DB.QueryRow("SELECT count(*) FROM assets WHERE kind='output'").Scan(&outputs)
	if outputs != 0 {
		t.Fatal("unauthorized output persisted")
	}
}

func TestTemplateSeedsFillOnlyEmptyAndVersionBumps(t *testing.T) {
	f := newFixture(t)
	tpl := testTemplate("none")
	tpl.Key = "seed"
	dir := filepath.Join(f.c.Cfg.TemplateDir, "生图模板", "detail")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "seed.json")
	write := func(tpl Template) {
		t.Helper()
		if err := os.WriteFile(file, []byte(core.JS(tpl)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(tpl)
	if err := f.a.seedTemplates(); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "UPDATE creative_templates SET name='人工名称',prompt='人工提示',purpose='',version=7 WHERE key='seed'")
	tpl.Name = "文件新名称"
	tpl.Prompt = "文件新提示"
	tpl.Purpose = "填补用途"
	write(tpl)
	if err := f.a.seedTemplates(); err != nil {
		t.Fatal(err)
	}
	got, err := f.a.template(context.Background(), "seed")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "人工名称" || got.Prompt != "人工提示" || got.Purpose != "填补用途" || got.Version != 7 {
		t.Fatal(got)
	}
	f.request(t, "PATCH", "/api/creative/templates/seed", core.Object{"prompt": "修改提示"}, 200)
	got, _ = f.a.template(context.Background(), "seed")
	if got.Version != 8 {
		t.Fatal(got.Version)
	}
	f.request(t, "PATCH", "/api/creative/templates/seed", core.Object{"name": "只改名称"}, 200)
	got, _ = f.a.template(context.Background(), "seed")
	if got.Version != 8 {
		t.Fatal(got.Version)
	}
	if err = os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = f.a.seedTemplates(); err != nil {
		t.Fatal(err)
	}
}

func TestReturnFeedbackWatermarkThresholdAndOther(t *testing.T) {
	f := newFixture(t)
	set, _ := f.makeSet(t, testTemplate("none"), 1)
	f.exec(t, "CREATE TABLE order_return_lines(return_id INTEGER,line_id INTEGER,PRIMARY KEY(return_id,line_id))")
	if err := f.a.scanReturns(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "ALTER TABLE order_return_lines ADD COLUMN reason_code TEXT;ALTER TABLE order_return_lines ADD COLUMN creative_set_id INTEGER")
	f.exec(t, "INSERT INTO order_return_lines VALUES(1,1,'smaller',?)", set.ID)
	if err := f.a.scanReturns(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	_ = f.c.DB.QueryRow("SELECT count(*) FROM creative_suggestions").Scan(&count)
	if count != 0 {
		t.Fatal("first return triggered")
	}
	f.exec(t, "INSERT INTO order_return_lines VALUES(2,1,'smaller',?),(3,1,'other',?),(4,1,'other',?)", set.ID, set.ID, set.ID)
	for index := 0; index < 2; index++ {
		if err := f.a.scanReturns(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	_ = f.c.DB.QueryRow("SELECT count(*) FROM creative_suggestions").Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	var tight bool
	var returns int
	_ = f.c.DB.QueryRow("SELECT tightened FROM creative_calibration WHERE category='tight:main4'").Scan(&tight)
	_ = f.c.DB.QueryRow("SELECT count FROM creative_return_counts WHERE reason_code='smaller'").Scan(&returns)
	if !tight || returns != 2 {
		t.Fatal(tight, returns)
	}
}

func TestExpectedTextNormalizationAndCriticalRules(t *testing.T) {
	checks := checkTexts([]ExpectedText{{Key: "size", Text: "8 mm"}, {Key: "title", Text: "珍珠"}}, []OCRText{{Text: "8，mm", Box: []float64{0, 0, 0.2, 0.1}}})
	if !checks[0].Pass || !checks[0].Critical || !checks[1].Warn {
		t.Fatal(checks)
	}
	for _, text := range []string{"925银", "¥99", "99元", "约1cm"} {
		if !critical(text) {
			t.Fatal(text)
		}
	}
}
