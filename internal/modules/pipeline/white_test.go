package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func encodePNG(t *testing.T, im image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func fillNRGBA(im *image.NRGBA, c color.NRGBA) {
	for y := 0; y < im.Bounds().Dy(); y++ {
		for x := 0; x < im.Bounds().Dx(); x++ {
			im.SetNRGBA(x, y, c)
		}
	}
}

func whiteScene(w, h int) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	fillNRGBA(im, color.NRGBA{255, 255, 255, 255})
	dark := color.NRGBA{30, 40, 90, 255}
	for y := h * 3 / 10; y < h*7/10; y++ {
		for x := w * 3 / 10; x < w*7/10; x++ {
			im.SetNRGBA(x, y, dark)
		}
	}
	for y := h * 45 / 100; y < h*55/100; y++ {
		for x := w * 45 / 100; x < w*55/100; x++ {
			im.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
		}
	}
	return im
}

func assertWhiteScene(t *testing.T, white, transparent *image.NRGBA) {
	t.Helper()
	if white.Bounds().Dx() != 1200 || white.Bounds().Dy() != 1200 || transparent.Bounds() != white.Bounds() {
		t.Fatal(white.Bounds(), transparent.Bounds())
	}
	for _, p := range []image.Point{{0, 0}, {1199, 0}, {0, 1199}, {1199, 1199}} {
		if transparent.NRGBAAt(p.X, p.Y).A != 0 {
			t.Fatalf("corner %v alpha %d", p, transparent.NRGBAAt(p.X, p.Y).A)
		}
	}
	dark := transparent.NRGBAAt(400, 600)
	if dark.A != 255 || dark.R != 30 || dark.G != 40 || dark.B != 90 {
		t.Fatal("dark block", dark)
	}
	dot := transparent.NRGBAAt(600, 600)
	if dot.A != 255 || dot.R != 255 || dot.G != 255 || dot.B != 255 {
		t.Fatal("enclosed white", dot)
	}
}

func pureWhite(c color.NRGBA) bool {
	return c.R == 255 && c.G == 255 && c.B == 255 && c.A == 255
}

func TestWhiteFitKnockoutAndReturnedSizes(t *testing.T) {
	square, transparent, returned, err := whiteOutputs(encodePNG(t, whiteScene(1254, 1254)))
	if err != nil || returned != "1254x1254" {
		t.Fatal(err, returned)
	}
	assertWhiteScene(t, square, transparent)

	tallSrc := image.NewNRGBA(image.Rect(0, 0, 1024, 1536))
	fillNRGBA(tallSrc, color.NRGBA{12, 18, 40, 255})
	tall, tallAlpha, returned, err := whiteOutputs(encodePNG(t, tallSrc))
	if err != nil || returned != "1024x1536" {
		t.Fatal(err, returned)
	}
	if tall.Bounds().Dx() != 1200 || tall.Bounds().Dy() != 1200 {
		t.Fatal(tall.Bounds())
	}
	if !pureWhite(tall.NRGBAAt(0, 0)) || !pureWhite(tall.NRGBAAt(150, 600)) || !pureWhite(tall.NRGBAAt(1100, 600)) {
		t.Fatal("portrait was not letterboxed", tall.NRGBAAt(0, 0), tall.NRGBAAt(150, 600), tall.NRGBAAt(1100, 600))
	}
	mid := tall.NRGBAAt(600, 600)
	if mid.R != 12 || mid.G != 18 || mid.B != 40 {
		t.Fatal("portrait center", mid)
	}
	if tallAlpha.NRGBAAt(0, 0).A != 0 || tallAlpha.NRGBAAt(150, 600).A != 0 || tallAlpha.NRGBAAt(1100, 600).A != 0 || tallAlpha.NRGBAAt(600, 600).A != 255 {
		t.Fatal("portrait alpha", tallAlpha.NRGBAAt(0, 0).A, tallAlpha.NRGBAAt(150, 600).A, tallAlpha.NRGBAAt(600, 600).A)
	}

	red := color.NRGBA{200, 10, 10, 255}
	for _, tc := range []struct {
		w, h int
		pad  bool
	}{{97, 100, false}, {103, 100, false}, {96, 100, true}, {104, 100, true}} {
		im := image.NewNRGBA(image.Rect(0, 0, tc.w, tc.h))
		fillNRGBA(im, red)
		got := fitWhiteSquare(im, 1200)
		corner := pureWhite(got.NRGBAAt(0, 0))
		center := got.NRGBAAt(600, 600)
		if corner != tc.pad || center != red {
			t.Fatalf("%dx%d pad=%v cornerWhite=%v center=%v", tc.w, tc.h, tc.pad, corner, center)
		}
	}

	small := image.NewNRGBA(image.Rect(0, 0, 21, 21))
	fillNRGBA(small, color.NRGBA{255, 255, 255, 255})
	for y := 6; y <= 14; y++ {
		for x := 6; x <= 14; x++ {
			small.SetNRGBA(x, y, color.NRGBA{20, 20, 20, 255})
		}
	}
	small.SetNRGBA(10, 10, color.NRGBA{255, 255, 255, 255})
	small.SetNRGBA(0, 2, color.NRGBA{244, 255, 255, 255})
	small.SetNRGBA(1, 0, color.NRGBA{245, 245, 245, 255})
	out := knockoutWhite(small)
	if out.NRGBAAt(0, 0).A != 0 || out.NRGBAAt(20, 20).A != 0 {
		t.Fatal("corners", out.NRGBAAt(0, 0).A, out.NRGBAAt(20, 20).A)
	}
	if out.NRGBAAt(10, 8).A != 255 || out.NRGBAAt(10, 8).R != 20 {
		t.Fatal("block", out.NRGBAAt(10, 8))
	}
	if c := out.NRGBAAt(10, 10); c.A != 255 || c.R != 255 {
		t.Fatal("enclosed white", c)
	}
	if c := out.NRGBAAt(5, 6); c.A != 128 || c.R != 255 {
		t.Fatal("feather", c)
	}
	if out.NRGBAAt(0, 2).A != 255 || out.NRGBAAt(1, 0).A == 255 {
		t.Fatal("threshold", out.NRGBAAt(0, 2).A, out.NRGBAAt(1, 0).A)
	}
	if _, _, _, err = whiteOutputs([]byte("not an image")); err == nil {
		t.Fatal("bad image accepted")
	}
}

func writeWhitePrompt(t *testing.T, a *Module) {
	t.Helper()
	if err := os.MkdirAll(a.Cfg.PromptDir, 0700); err != nil {
		t.Fatal(err)
	}
	body := "## 通用要求\nCOMMON {info}\n## A1 白底图\nWHITE_A1 {info}\n"
	if err := os.WriteFile(filepath.Join(a.Cfg.PromptDir, "补图.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func markWhite(t *testing.T, f *fixture, id int64, code string, w, h int) Photo {
	t.Helper()
	ph := addRealPhoto(t, f, id, code, w, h)
	f.req(t, "PATCH", "/api/assets/"+intString(ph.ID), Object{"white_bg": true}, 200)
	got, err := scanPhoto(f.a.DB.QueryRow("SELECT "+photoColumns+" FROM assets WHERE id=?", ph.ID))
	if err != nil || !got.WhiteBG || got.Code != code {
		t.Fatal(err, got.WhiteBG, got.Code)
	}
	return got
}

func photoLog(t *testing.T, f *fixture, id int64) Photo {
	t.Helper()
	ph, err := scanPhoto(f.a.DB.QueryRow("SELECT "+photoColumns+" FROM assets WHERE id=?", id))
	if err != nil {
		t.Fatal(err)
	}
	return ph
}

func TestWhiteBackgroundRetouchUsesAINotRembg(t *testing.T) {
	var calls atomic.Int32
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		got := readImageRequest(t, r)
		if got.size != "1024x1024" || len(got.dimensions) != 1 || got.dimensions[0] != image.Pt(1024, 1024) {
			t.Errorf("image request %+v", got)
		}
		if !strings.Contains(got.prompt, "WHITE_A1") || !strings.Contains(got.prompt, "COMMON") || !strings.Contains(got.prompt, "款名=测试珍珠") || strings.Contains(got.prompt, "{info}") {
			t.Errorf("prompt %s", got.prompt)
		}
		var width, height int
		if _, err := fmt.Sscanf(got.size, "%dx%d", &width, &height); err != nil || width < 1 || height < 1 {
			t.Errorf("size %s", got.size)
			w.WriteHeader(500)
			return
		}
		imageReply(w, encodePNG(t, whiteScene(width, height)))
	}))
	defer ai.Close()
	rembg := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("rembg was called")
	}))
	defer rembg.Close()
	f := newFixture(t, func(c *Config) {
		c.AIKey = "fake"
		c.AIBaseURL = ai.URL + "/v1"
		c.RembgURL = rembg.URL
	})
	writeWhitePrompt(t, f.a)
	f.login(t, "admin")
	id := f.product(t)
	whitePhoto := markWhite(t, f, id, "A1", 80, 120)
	addRealPhoto(t, f, id, "B1", 90, 90)
	p, err := f.a.product(id)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.a.retouch(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	dir := filepath.Join(f.a.Dir(id), "照片")
	jpg := filepath.Join(dir, "A1_白底.jpg")
	pngName := filepath.Join(dir, "A1_透明.png")
	plain, err := os.ReadFile(filepath.Join(dir, "A1.jpg"))
	if err != nil || len(plain) == 0 {
		t.Fatal("plain jpg", err)
	}
	if _, err = os.Stat(filepath.Join(dir, "B1_白底.jpg")); !os.IsNotExist(err) {
		t.Fatal("non-white photo got a white file", err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(mustRead(t, jpg)))
	if err != nil || cfg.Width != 1200 || cfg.Height != 1200 {
		t.Fatal(cfg, err)
	}
	decoded, _, err := loadPhoto(pngName)
	if err != nil {
		t.Fatal(err)
	}
	white, _, err := loadPhoto(jpg)
	if err != nil {
		t.Fatal(err)
	}
	assertWhiteScene(t, white, decoded)
	log := photoLog(t, f, whitePhoto.ID).RetouchLog
	if log["white_source"] != "ai" || log["white_size"] != float64(1200) || log["white_returned"] != "1024x1024" || log["white_model"] != "gpt-image-2.5-sunburst" || log["white_key"] == "" {
		t.Fatal(log)
	}
	if _, ok := log["cutout"]; ok {
		t.Fatal("cutout", log["cutout"])
	}
	if _, ok := log["opaque_pixels_preserved"]; ok {
		t.Fatal("opaque_pixels_preserved")
	}
	var used int
	var feature, kind, model string
	if err = f.a.DB.QueryRow("SELECT count(*) FROM ai_usage").Scan(&used); err != nil || used != 1 {
		t.Fatal(err, used)
	}
	if err = f.a.DB.QueryRow("SELECT feature, kind, model FROM ai_usage").Scan(&feature, &kind, &model); err != nil || feature != "fill" || kind != "image" || model != "gpt-image-2.5-sunburst" {
		t.Fatal(err, feature, kind, model)
	}
	first, _ := os.ReadFile(jpg)
	firstPNG, _ := os.ReadFile(pngName)
	p, _ = f.a.product(id)
	if err = f.a.retouch(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || usedAfter(t, f) != 1 {
		t.Fatal("reused white still called AI", calls.Load(), usedAfter(t, f))
	}
	if !bytes.Equal(first, mustRead(t, jpg)) || !bytes.Equal(firstPNG, mustRead(t, pngName)) {
		t.Fatal("reused files changed")
	}
	again := photoLog(t, f, whitePhoto.ID).RetouchLog
	if again["white_key"] != log["white_key"] || again["white_source"] != "ai" || again["white_returned"] != "1024x1024" {
		t.Fatal(again)
	}
	if _, err = f.a.DB.Exec("UPDATE assets SET crop=? WHERE id=?", "[0,0,0.5,1]", whitePhoto.ID); err != nil {
		t.Fatal(err)
	}
	p, _ = f.a.product(id)
	if err = f.a.retouch(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || usedAfter(t, f) != 2 {
		t.Fatal("changed crop did not regenerate", calls.Load(), usedAfter(t, f))
	}
	changed := photoLog(t, f, whitePhoto.ID).RetouchLog
	if changed["white_key"] == log["white_key"] || changed["white_error"] != nil {
		t.Fatal(changed)
	}
}

func usedAfter(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.a.DB.QueryRow("SELECT count(*) FROM ai_usage").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWhiteAIFailureKeepsThePhotoAndTheJob(t *testing.T) {
	var calls atomic.Int32
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	}))
	defer ai.Close()
	rembg := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("rembg was called")
	}))
	defer rembg.Close()
	f := newFixture(t, func(c *Config) {
		c.AIKey = "fake-secret"
		c.AIBaseURL = ai.URL + "/v1"
		c.RembgURL = rembg.URL
	})
	writeWhitePrompt(t, f.a)
	f.login(t, "admin")
	id := f.product(t)
	ph := markWhite(t, f, id, "A1", 40, 50)
	var listingID int64
	if err := f.a.DB.QueryRow("SELECT id FROM listings WHERE product_id=?", id).Scan(&listingID); err != nil {
		t.Fatal(err)
	}
	res, err := f.a.DB.Exec("INSERT INTO jobs(target_id,target_type,user_id,kind,status,created_at,steps) VALUES(?,'listing',1,'pipeline','queued',?,'retouch')", listingID, now())
	if err != nil {
		t.Fatal(err)
	}
	jid, _ := res.LastInsertId()
	f.a.Wake()
	job := waitJob(t, f.a, jid)
	if job.Status != "done" {
		t.Fatal(job)
	}
	if calls.Load() == 0 {
		t.Fatal("AI was not called")
	}
	dir := filepath.Join(f.a.Dir(id), "照片")
	if _, err = os.Stat(filepath.Join(dir, "A1.jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "A1_白底.jpg")); !os.IsNotExist(err) {
		t.Fatal("white jpg exists", err)
	}
	if _, err = os.Stat(filepath.Join(dir, "A1_透明.png")); !os.IsNotExist(err) {
		t.Fatal("transparent png exists", err)
	}
	log := photoLog(t, f, ph.ID).RetouchLog
	message, _ := log["white_error"].(string)
	if !strings.Contains(message, "500") || strings.Contains(message, "fake-secret") {
		t.Fatal(log)
	}
	if _, ok := log["cutout"]; ok {
		t.Fatal(log)
	}
	var used int
	if err = f.a.DB.QueryRow("SELECT count(*) FROM ai_usage").Scan(&used); err != nil || used != 0 {
		t.Fatal(err, used)
	}

	f.req(t, "PATCH", "/api/assets/"+intString(ph.ID), Object{"white_bg": false}, 200)
	if _, err = f.a.DB.Exec("INSERT INTO quotas(user_id, month_images) VALUES(1, 0)"); err != nil {
		t.Fatal(err)
	}
	other := f.product(t)
	quotaPhoto := markWhite(t, f, other, "A2", 30, 40)
	before := calls.Load()
	p, err := f.a.product(other)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.a.retouch(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Fatal("quota failure still called AI", calls.Load())
	}
	qlog := photoLog(t, f, quotaPhoto.ID).RetouchLog
	qmsg, _ := qlog["white_error"].(string)
	if !strings.Contains(qmsg, "额度") {
		t.Fatal(qlog)
	}
	if _, err = os.Stat(filepath.Join(f.a.Dir(other), "照片", "A2.jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(f.a.Dir(other), "照片", "A2_白底.jpg")); !os.IsNotExist(err) {
		t.Fatal(err)
	}

	os.Remove(filepath.Join(f.a.Cfg.PromptDir, "补图.md"))
	third := f.product(t)
	missing := markWhite(t, f, third, "E4", 30, 40)
	p, _ = f.a.product(third)
	if err = f.a.retouch(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	mlog := photoLog(t, f, missing.ID).RetouchLog
	mmsg, _ := mlog["white_error"].(string)
	if !strings.Contains(mmsg, "补图.md") {
		t.Fatal(mlog)
	}
	if err = os.WriteFile(filepath.Join(f.a.Cfg.PromptDir, "补图.md"), []byte("## 通用要求\nONLY\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fourth := f.product(t)
	partial := markWhite(t, f, fourth, "B2", 30, 40)
	p, _ = f.a.product(fourth)
	if err = f.a.retouch(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	plog := photoLog(t, f, partial.ID).RetouchLog
	pmsg, _ := plog["white_error"].(string)
	if !strings.Contains(pmsg, "A1") {
		t.Fatal(plog)
	}
}
