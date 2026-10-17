package pipeline

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func jpegExif(t *testing.T, im image.Image, orientation int, gps bool) []byte {
	t.Helper()
	var jpegData bytes.Buffer
	if e := jpeg.Encode(&jpegData, im, &jpeg.Options{Quality: 95}); e != nil {
		t.Fatal(e)
	}
	entries := 1
	if gps {
		entries++
	}
	tiff := make([]byte, 8+2+entries*12+4+2)
	copy(tiff, "II")
	binary.LittleEndian.PutUint16(tiff[2:], 42)
	binary.LittleEndian.PutUint32(tiff[4:], 8)
	binary.LittleEndian.PutUint16(tiff[8:], uint16(entries))
	binary.LittleEndian.PutUint16(tiff[10:], 0x0112)
	binary.LittleEndian.PutUint16(tiff[12:], 3)
	binary.LittleEndian.PutUint32(tiff[14:], 1)
	binary.LittleEndian.PutUint16(tiff[18:], uint16(orientation))
	if gps {
		binary.LittleEndian.PutUint16(tiff[22:], 0x8825)
		binary.LittleEndian.PutUint16(tiff[24:], 4)
		binary.LittleEndian.PutUint32(tiff[26:], 1)
		binary.LittleEndian.PutUint32(tiff[30:], uint32(len(tiff)-2))
	}
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	n := len(app1) + 2
	out := []byte{0xff, 0xd8, 0xff, 0xe1, byte(n >> 8), byte(n)}
	out = append(out, app1...)
	out = append(out, jpegData.Bytes()[2:]...)
	return out
}
func TestExifGeometryAndGPSRemoval(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(y*3 + x + 1), 0, 0, 255})
		}
	}
	cases := []struct {
		o    int
		want []uint8
		w, h int
	}{{1, []uint8{1, 2, 3, 4, 5, 6}, 3, 2}, {2, []uint8{3, 2, 1, 6, 5, 4}, 3, 2}, {3, []uint8{6, 5, 4, 3, 2, 1}, 3, 2}, {4, []uint8{4, 5, 6, 1, 2, 3}, 3, 2}, {5, []uint8{1, 4, 2, 5, 3, 6}, 2, 3}, {6, []uint8{4, 1, 5, 2, 6, 3}, 2, 3}, {7, []uint8{6, 3, 5, 2, 4, 1}, 2, 3}, {8, []uint8{3, 6, 2, 5, 1, 4}, 2, 3}}
	for _, tc := range cases {
		b := jpegExif(t, im, tc.o, true)
		ex := ReadExif(b)
		if ex.Orientation != tc.o || !ex.GPS {
			t.Fatalf("EXIF %d: %+v", tc.o, ex)
		}
		got := Geometry(im, tc.o, 0, nil)
		if got.Rect.Dx() != tc.w || got.Rect.Dy() != tc.h {
			t.Fatal(got.Rect)
		}
		for i, v := range tc.want {
			if got.NRGBAAt(i%tc.w, i/tc.w).R != v {
				t.Fatalf("orientation %d pixel %d", tc.o, i)
			}
		}
	}
	// EXIF 90° plus manual 270° cancels; crop then uses displayed coordinates.
	got := Geometry(im, 6, 270, []float64{1.0 / 3, 0, 2.0 / 3, 1})
	if got.Rect.Dx() != 2 || got.Rect.Dy() != 2 || got.NRGBAAt(0, 0).R != 2 || got.NRGBAAt(1, 1).R != 6 {
		t.Fatal(got.Rect, got.Pix)
	}
	f := newFixture(t, nil)
	f.login(t, "admin")
	id := f.product(t)
	photo := image.NewNRGBA(image.Rect(0, 0, 30, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 30; x++ {
			photo.SetNRGBA(x, y, color.NRGBA{190, 160, 170, 255})
		}
	}
	up := f.upload(t, id, "gps.jpg", jpegExif(t, photo, 6, true), 201)
	pid := int64(up["photos"].([]any)[0].(Object)["id"].(float64))
	f.req(t, "PATCH", "/api/assets/"+intString(pid), Object{"code": "E5", "rotate": 90, "crop": []float64{0, 0, .5, 1}}, 200)
	p, _ := f.a.product(id)
	if e := f.a.retouch(testActor(context.Background()), p); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(f.a.Dir(id), "照片", "E5.jpg"))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(b, []byte("Exif")) || ReadExif(b).GPS {
		t.Fatal("EXIF/GPS leaked")
	}
	decoded, _, e := image.Decode(bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	if decoded.Bounds().Dx() != 15 || decoded.Bounds().Dy() != 20 {
		t.Fatal("combined geometry", decoded.Bounds())
	}
	// Corrupt EXIF offsets must never panic or read outside the segment.
	for i := 0; i < len(b); i++ {
		_ = ReadExif(b[:i])
	}
}

func TestConservativeRetouch(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			c := color.NRGBA{120, 114, 106, 255}
			if x < 30 {
				c = color.NRGBA{205, 150, 171, 255}
			}
			if x > 90 {
				c = color.NRGBA{247, 247, 247, 255}
			}
			if y < 2 {
				c = color.NRGBA{255, 255, 255, 255}
			}
			im.SetNRGBA(x, y, c)
		}
	}
	got, log := Retouch(im, "B1")
	for _, gain := range log["wb_gain"].([3]float64) {
		if gain < .94 || gain > 1.06 {
			t.Fatal("WB gain", gain)
		}
	}
	if log["exposure_gain"].(float64) < 1 || log["exposure_gain"].(float64) > 1.25 {
		t.Fatal(log)
	}
	if clipRatio(got)-clipRatio(im) > .00500001 {
		t.Fatal("clipping increased", log)
	}
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			before, after := im.NRGBAAt(x, y), got.NRGBAAt(x, y)
			if x < 30 && hueDelta(before, after) >= 3 {
				t.Fatal("pink pearl hue changed", before, after)
			}
			if luminance(after)+.01 < luminance(before) {
				t.Fatal("darkened", before, after)
			}
		}
	}
	untouched, log := Retouch(im, "E5")
	if !bytes.Equal(untouched.Pix, im.Pix) || log["exposure_gain"] != 1 {
		t.Fatal("E5 was color corrected")
	}
	large := image.NewNRGBA(image.Rect(0, 0, 3000, 100))
	resized, _ := Retouch(large, "E5")
	if resized.Rect.Dx() != 2400 || resized.Rect.Dy() != 80 {
		t.Fatal(resized.Rect)
	}
}
func TestWhiteCompositePreservesOpaquePixelsAndFallback(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 100, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 100; x++ {
			c := color.NRGBA{245, 245, 245, 255}
			if x >= 30 && x < 70 && y >= 20 && y < 60 {
				c = color.NRGBA{uint8(140 + x%10), uint8(100 + y%20), 160, 255}
			}
			src.SetNRGBA(x, y, c)
		}
	}
	f := newFixture(t, nil)
	mask, mode := f.a.mask(testActor(context.Background()), src)
	if mode != "fallback" || mask.AlphaAt(0, 0).A != 0 || mask.AlphaAt(50, 40).A != 255 {
		t.Fatal("fallback failed", mode)
	}
	white, transparent, e := CompositeWhite(src, mask)
	if e != nil {
		t.Fatal(e)
	}
	if white.Rect.Dx() != 1200 || transparent.Rect.Dy() != 1200 {
		t.Fatal("white dimensions")
	}
	// Foreground bounding box is [30,20,70,60], scaled to 780x780 and centered.
	for y := 0; y < 780; y++ {
		for x := 0; x < 780; x++ {
			expected := src.NRGBAAt(30+x*40/780, 20+y*40/780)
			got := white.NRGBAAt(210+x, 210+y)
			if got != expected || transparent.NRGBAAt(210+x, 210+y) != expected {
				t.Fatalf("product pixel changed at %d,%d: %v %v", x, y, got, expected)
			}
		}
	}
	// Even malicious/rembg RGB output never replaces our source RGB.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/remove" || r.Method != "POST" {
			t.Error("wrong rembg request")
		}
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			t.Error(e)
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		fake := image.NewNRGBA(src.Rect)
		for y := 20; y < 60; y++ {
			for x := 30; x < 70; x++ {
				fake.SetNRGBA(x, y, color.NRGBA{0, 255, 0, 255})
			}
		}
		_ = png.Encode(w, fake)
	}))
	defer server.Close()
	f.a.Cfg.RembgURL = server.URL
	mask, mode = f.a.mask(testActor(context.Background()), src)
	if mode != "rembg" {
		t.Fatal(mode)
	}
	white, _, e = CompositeWhite(src, mask)
	if e != nil || white.NRGBAAt(600, 600) != src.NRGBAAt(50, 40) {
		t.Fatal("used rembg RGB")
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer bad.Close()
	f.a.Cfg.RembgURL = bad.URL
	_, mode = f.a.mask(testActor(context.Background()), src)
	if mode != "fallback" {
		t.Fatal("no fallback on HTTP error")
	}
}
func TestMetricsDetectDuplicateBlurAndGPS(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 30, 30))
	for i := 0; i < len(im.Pix); i += 4 {
		copy(im.Pix[i:i+4], []byte{255, 255, 255, 255})
	}
	m, h := Metrics(im, Exif{Orientation: 6, GPS: true})
	if m["blur_level"] != "模糊" || m["clip_high"] != 1.0 || m["has_gps"] != true {
		t.Fatal(m)
	}
	_, other := Metrics(im, Exif{})
	if h != other {
		t.Fatal("dHash unstable")
	}
}
