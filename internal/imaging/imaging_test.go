package imaging

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGeometryResizeAndHash(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(x * 3), uint8(y * 5), 120, 255})
		}
	}
	got := Geometry(im, 6, 90, []float64{0, 0, .5, 1})
	if got.Bounds().Size() != image.Pt(40, 40) {
		t.Fatal(got.Bounds())
	}
	if Hash(im) != Hash(rgba(im)) {
		t.Fatal("相同像素哈希不一致")
	}
	if Resize(im, 20).Bounds().Size() != image.Pt(20, 10) {
		t.Fatal("重采样尺寸错误")
	}
}

func TestCopiedMaskCompositeAndColorLimits(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			c := color.NRGBA{255, 255, 255, 255}
			if x >= 12 && x < 28 && y >= 12 && y < 28 {
				c = color.NRGBA{190, 120, 150, 255}
			}
			im.SetNRGBA(x, y, c)
		}
	}
	mask, mode := Mask(context.Background(), im, "", http.DefaultClient)
	if mode != "fallback" {
		t.Fatal(mode)
	}
	white, transparent, err := CompositeWhite(im, mask)
	if err != nil || white.Bounds().Size() != image.Pt(1200, 1200) || transparent.NRGBAAt(0, 0).A != 0 || white.NRGBAAt(600, 600) != im.NRGBAAt(20, 20) {
		t.Fatal("抠图尺寸、透明或像素保留不正确", err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := image.NewNRGBA(im.Bounds())
		for y := 12; y < 28; y++ {
			for x := 12; x < 28; x++ {
				out.SetNRGBA(x, y, color.NRGBA{1, 2, 3, 255})
			}
		}
		png.Encode(w, out)
	}))
	defer s.Close()
	_, mode = Mask(context.Background(), im, s.URL, http.DefaultClient)
	if mode != "rembg" {
		t.Fatal("未使用假抠图服务")
	}
	dark, bright := image.NewNRGBA(image.Rect(0, 0, 10, 10)), image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			dark.SetNRGBA(x, y, color.NRGBA{90, 100, 110, 255})
			bright.SetNRGBA(x, y, color.NRGBA{220, 200, 180, 255})
		}
	}
	_, log := Color(dark, "B1", true, bright)
	wb := log["wb_gain"].([3]float64)
	for _, gain := range wb {
		if gain < .94 || gain > 1.06 {
			t.Fatal(wb)
		}
	}
	if gain := log["exposure_gain"].(float64); gain < 1 || gain > 1.25 {
		t.Fatal(gain)
	}
	unchanged, log := Color(dark, "E5", true, bright)
	if !bytes.Equal(unchanged.Pix, dark.Pix) || log["color_skipped"] != true {
		t.Fatal("E5 未跳过校色")
	}
	_, log = Color(bright, "B1", false, dark)
	if log["exposure_gain"] != 1.0 {
		t.Fatal("向暗图匹配时压暗了图片")
	}
}
func TestAIDiffAndTransparentAspect(t *testing.T) {
	a, b := image.NewNRGBA(image.Rect(0, 0, 64, 64)), image.NewNRGBA(image.Rect(0, 0, 64, 64))
	random := rand.New(rand.NewSource(1))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			c := color.NRGBA{uint8(random.Intn(256)), uint8(random.Intn(256)), uint8(random.Intn(256)), 255}
			a.SetNRGBA(x, y, c)
			b.SetNRGBA(x, y, color.NRGBA{255 - c.R, 255 - c.G, 255 - c.B, 255})
		}
	}
	diff := Diff(a, b)
	if diff["phash_distance"].(int) <= 16 || diff["color_delta"].(float64) <= 0 {
		t.Fatal(diff)
	}
	im := image.NewNRGBA(image.Rect(0, 0, 120, 40))
	for y := 10; y < 30; y++ {
		for x := 90; x < 110; x++ {
			im.SetNRGBA(x, y, color.NRGBA{200, 100, 150, 255})
		}
	}
	cropped := Aspect(im, 1)
	if cropped.Rect.Size() != image.Pt(40, 40) || cropped.NRGBAAt(20, 20).A == 0 {
		t.Fatal("比例裁剪未围绕透明图中的商品")
	}
}

func TestReplaceBackgroundPreservesGeometryAndSoftMask(t *testing.T) {
	src := image.NewNRGBA(image.Rect(5, 7, 8, 8))
	mask := image.NewAlpha(src.Rect)
	for x := 5; x < 8; x++ {
		src.SetNRGBA(x, 7, color.NRGBA{40, 80, 120, 255})
		mask.SetAlpha(x, 7, color.Alpha{uint8((x - 5) * 127)})
	}
	mask.SetAlpha(7, 7, color.Alpha{255})
	white, transparent := ReplaceBackground(src, mask, false), ReplaceBackground(src, mask, true)
	if white.Rect != src.Rect || transparent.Rect != src.Rect {
		t.Fatal("抠图修改了边界")
	}
	if white.NRGBAAt(5, 7) != (color.NRGBA{255, 255, 255, 255}) || transparent.NRGBAAt(5, 7).A != 0 || transparent.NRGBAAt(7, 7) != src.NRGBAAt(7, 7) {
		t.Fatal("背景或商品像素错误")
	}
	if transparent.NRGBAAt(6, 7) != (color.NRGBA{40, 80, 120, 127}) || white.NRGBAAt(6, 7) != (color.NRGBA{148, 168, 188, 255}) {
		t.Fatal("软边混合错误", white.NRGBAAt(6, 7), transparent.NRGBAAt(6, 7))
	}
}
