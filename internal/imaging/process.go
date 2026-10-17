package imaging

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/bits"

	xdraw "golang.org/x/image/draw"
)

func Resample(src image.Image, side int) (*image.NRGBA, error) {
	b := src.Bounds()
	scale := float64(side) / float64(max(b.Dx(), b.Dy()))
	w, h := max(1, int(math.Round(float64(b.Dx())*scale))), max(1, int(math.Round(float64(b.Dy())*scale)))
	if side < 1 || int64(w)*int64(h) > maxPixels {
		return nil, fmt.Errorf("输出图片最多 4000 万像素")
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(out, out.Bounds(), src, b, draw.Src, nil)
	return out, nil
}
func Aspect(src *image.NRGBA, ratio float64) *image.NRGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	cw, ch := w, h
	if float64(w)/float64(h) > ratio {
		cw = max(1, int(math.Round(float64(h)*ratio)))
	} else {
		ch = max(1, int(math.Round(float64(w)/ratio)))
	}
	cx, cy := w/2, h/2
	// Existing transparency provides a product mask without another service call.
	transparent := false
	box := image.Rectangle{}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			alpha := src.NRGBAAt(x, y).A
			if alpha < 255 {
				transparent = true
			}
			if alpha > 0 {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if transparent && !box.Empty() {
		cx = (box.Min.X + box.Max.X) / 2
		cy = (box.Min.Y + box.Max.Y) / 2
	}
	left, top := max(0, min(w-cw, cx-cw/2)), max(0, min(h-ch, cy-ch/2))
	return rgba(src.SubImage(image.Rect(left, top, left+cw, top+ch)))
}
func Color(src *image.NRGBA, code string, auto bool, ref *image.NRGBA) (*image.NRGBA, Object) {
	if code == "E5" {
		return Retouch(src, code)
	}
	if ref == nil {
		if auto {
			return Retouch(src, code)
		}
		return src, Object{"color_skipped": true}
	}
	mean := func(im *image.NRGBA) ([3]float64, float64) {
		var channels [3]float64
		var lum float64
		n := float64(im.Rect.Dx() * im.Rect.Dy())
		for y := 0; y < im.Rect.Dy(); y++ {
			for x := 0; x < im.Rect.Dx(); x++ {
				c := im.NRGBAAt(x, y)
				channels[0] += float64(c.R) / n
				channels[1] += float64(c.G) / n
				channels[2] += float64(c.B) / n
				lum += luminance(c) / n
			}
		}
		return channels, lum
	}
	from, fl := mean(src)
	to, tl := mean(ref)
	wb := [3]float64{1, 1, 1}
	if fl > 0 && tl > 0 {
		for k := range wb {
			if from[k] > 0 {
				wb[k] = clamp((to[k]/tl)/(from[k]/fl), .94, 1.06)
			}
		}
	}
	gain := 1.0
	if fl > 0 {
		gain = clamp(tl/fl, 1, 1.25)
	}
	baseClip := clipRatio(src)
	apply := func(w [3]float64, g float64) *image.NRGBA {
		out := image.NewNRGBA(src.Rect)
		for y := 0; y < src.Rect.Dy(); y++ {
			for x := 0; x < src.Rect.Dx(); x++ {
				out.SetNRGBA(x, y, corrected(src.NRGBAAt(x, y), w, g))
			}
		}
		return out
	}
	if clipRatio(apply(wb, 1)) > baseClip+.005 {
		wb = [3]float64{1, 1, 1}
	}
	out := apply(wb, gain)
	if clipRatio(out) > baseClip+.005 {
		lo, hi := 1.0, gain
		for i := 0; i < 12; i++ {
			mid := (lo + hi) / 2
			if clipRatio(apply(wb, mid)) <= baseClip+.005 {
				lo = mid
			} else {
				hi = mid
			}
		}
		gain = lo
		out = apply(wb, gain)
	}
	return out, Object{"wb_gain": wb, "exposure_gain": gain, "clip_before": baseClip, "clip_after": clipRatio(out), "color_skipped": false, "match_to": true}
}
func Flatten(src *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(src.Rect)
	for y := 0; y < src.Rect.Dy(); y++ {
		for x := 0; x < src.Rect.Dx(); x++ {
			c := src.NRGBAAt(x, y)
			a := uint32(c.A)
			blend := func(v uint8) uint8 { return uint8((uint32(v)*a + 255*(255-a) + 127) / 255) }
			out.SetNRGBA(x, y, color.NRGBA{blend(c.R), blend(c.G), blend(c.B), 255})
		}
	}
	return out
}
func Diff(a, b image.Image) Object {
	fit := func(im image.Image) *image.NRGBA {
		width := 256
		h := max(1, min(4096, int(math.Round(float64(im.Bounds().Dy())*256/float64(im.Bounds().Dx())))))
		out := image.NewNRGBA(image.Rect(0, 0, width, h))
		xdraw.CatmullRom.Scale(out, out.Bounds(), im, im.Bounds(), draw.Src, nil)
		return out
	}
	x, y := fit(a), fit(b)
	var delta float64
	for j := 0; j < x.Rect.Dy(); j++ {
		for i := 0; i < 256; i++ {
			c, d := x.NRGBAAt(i, j), y.NRGBAAt(i, min(y.Rect.Dy()-1, j*y.Rect.Dy()/x.Rect.Dy()))
			delta += math.Sqrt(math.Pow(float64(c.R)-float64(d.R), 2) + math.Pow(float64(c.G)-float64(d.G), 2) + math.Pow(float64(c.B)-float64(d.B), 2))
		}
	}
	return Object{"phash_distance": bits.OnesCount64(Hash(x) ^ Hash(y)), "color_delta": delta / float64(x.Rect.Dx()*x.Rect.Dy())}
}
