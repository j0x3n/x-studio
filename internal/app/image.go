package app

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type Exif struct {
	Orientation int
	GPS         bool
}

// ReadExif intentionally parses only JPEG APP1/TIFF IFD0 orientation and the
// GPS IFD pointer. Every offset is checked before use, including corrupt EXIF.
func ReadExif(b []byte) Exif {
	e := Exif{Orientation: 1}
	if len(b) < 2 || b[0] != 0xff || b[1] != 0xd8 {
		return e
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xff {
			break
		}
		for i < len(b) && b[i] == 0xff {
			i++
		}
		if i >= len(b) {
			break
		}
		marker := b[i]
		i++
		if marker == 0xda || marker == 0xd9 {
			break
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if i+2 > len(b) {
			break
		}
		n := int(binary.BigEndian.Uint16(b[i : i+2]))
		if n < 2 || i+n > len(b) {
			break
		}
		seg := b[i+2 : i+n]
		i += n
		if marker != 0xe1 || len(seg) < 14 || !bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			continue
		}
		t := seg[6:]
		var order binary.ByteOrder
		switch string(t[:2]) {
		case "II":
			order = binary.LittleEndian
		case "MM":
			order = binary.BigEndian
		default:
			continue
		}
		if order.Uint16(t[2:4]) != 42 {
			continue
		}
		off := uint64(order.Uint32(t[4:8]))
		if off+2 > uint64(len(t)) {
			continue
		}
		count := uint64(order.Uint16(t[off : off+2]))
		off += 2
		for k := uint64(0); k < count; k++ {
			p := off + 12*k
			if p+12 > uint64(len(t)) {
				break
			}
			tag := order.Uint16(t[p : p+2])
			typ := order.Uint16(t[p+2 : p+4])
			num := order.Uint32(t[p+4 : p+8])
			if tag == 0x0112 && typ == 3 && num == 1 {
				v := int(order.Uint16(t[p+8 : p+10]))
				if v >= 1 && v <= 8 {
					e.Orientation = v
				}
			}
			if tag == 0x8825 && typ == 4 && num == 1 {
				ptr := uint64(order.Uint32(t[p+8 : p+12]))
				e.GPS = ptr > 0 && ptr+2 <= uint64(len(t))
			}
		}
	}
	return e
}
func rgba(src image.Image) *image.NRGBA {
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
	return out
}
func oriented(src image.Image, o int) *image.NRGBA {
	s := rgba(src)
	w, h := s.Bounds().Dx(), s.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 && o <= 8 {
		dw, dh = h, w
	}
	d := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			sx, sy := x, y
			switch o {
			case 2:
				sx = w - 1 - x
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sy = h - 1 - y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			d.SetNRGBA(x, y, s.NRGBAAt(sx, sy))
		}
	}
	return d
}

// Coordinates for crop refer to the displayed image after EXIF normalization
// and the clockwise manual rotation. This same ordering is used for AI input.
func Geometry(src image.Image, exif int, rotate int, crop []float64) *image.NRGBA {
	s := oriented(src, exif)
	switch rotate {
	case 90:
		s = oriented(s, 6)
	case 180:
		s = oriented(s, 3)
	case 270:
		s = oriented(s, 8)
	}
	if len(crop) == 4 {
		w, h := s.Bounds().Dx(), s.Bounds().Dy()
		x := int(math.Round(crop[0] * float64(w)))
		y := int(math.Round(crop[1] * float64(h)))
		cw := max(1, int(math.Round(crop[2]*float64(w))))
		ch := max(1, int(math.Round(crop[3]*float64(h))))
		rect := image.Rect(x, y, min(w, x+cw), min(h, y+ch)).Intersect(s.Bounds())
		if !rect.Empty() {
			s = rgba(s.SubImage(rect))
		}
	}
	return s
}
func resize(src image.Image, maxSide int) *image.NRGBA {
	b := src.Bounds()
	if max(b.Dx(), b.Dy()) <= maxSide {
		return rgba(src)
	}
	scale := float64(maxSide) / float64(max(b.Dx(), b.Dy()))
	d := image.NewNRGBA(image.Rect(0, 0, max(1, int(math.Round(float64(b.Dx())*scale))), max(1, int(math.Round(float64(b.Dy())*scale)))))
	xdraw.CatmullRom.Scale(d, d.Bounds(), src, b, draw.Src, nil)
	return d
}
func loadPhoto(filename string) (*image.NRGBA, Exif, error) {
	b, e := os.ReadFile(filename)
	if e != nil {
		return nil, Exif{}, e
	}
	if _, _, e = imageConfig(b); e != nil {
		return nil, Exif{}, e
	}
	im, _, e := image.Decode(bytes.NewReader(b))
	if e != nil {
		return nil, Exif{}, e
	}
	return rgba(im), ReadExif(b), nil
}
func saveJPEG(filename string, im image.Image, q int) error {
	f, e := os.Create(filename)
	if e != nil {
		return e
	}
	e = jpeg.Encode(f, im, &jpeg.Options{Quality: q})
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}
func savePNG(filename string, im image.Image) error {
	f, e := os.Create(filename)
	if e != nil {
		return e
	}
	e = png.Encode(f, im)
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}
func luminance(c color.NRGBA) float64 {
	return .2126*float64(c.R) + .7152*float64(c.G) + .0722*float64(c.B)
}
func clipped(c color.NRGBA) bool { return c.R >= 250 && c.G >= 250 && c.B >= 250 }
func clipRatio(im *image.NRGBA) float64 {
	n := 0
	for y := 0; y < im.Rect.Dy(); y++ {
		for x := 0; x < im.Rect.Dx(); x++ {
			if clipped(im.NRGBAAt(x, y)) {
				n++
			}
		}
	}
	return float64(n) / float64(im.Rect.Dx()*im.Rect.Dy())
}
func hue(c color.NRGBA) (float64, float64) {
	r, g, b := float64(c.R), float64(c.G), float64(c.B)
	hi, lo := max(r, g, b), min(r, g, b)
	d := hi - lo
	if d == 0 {
		return 0, 0
	}
	var h float64
	switch hi {
	case r:
		h = 60 * math.Mod((g-b)/d, 6)
	case g:
		h = 60 * ((b-r)/d + 2)
	default:
		h = 60 * ((r-g)/d + 4)
	}
	if h < 0 {
		h += 360
	}
	return h, d / hi
}
func hueDelta(a, b color.NRGBA) float64 {
	h, s := hue(a)
	h2, s2 := hue(b)
	if s < .04 || s2 < .04 {
		return 0
	}
	v := math.Abs(h - h2)
	return min(v, 360-v)
}
func clamp(v, lo, hi float64) float64 { return max(lo, min(hi, v)) }
func roundByte(v float64) uint8       { return uint8(math.Round(clamp(v, 0, 255))) }

func corrected(c color.NRGBA, wb [3]float64, exposure float64) color.NRGBA {
	base := c
	// Per-pixel guard protects naturally pink pearls from a gray-world shift.
	for amount := 1.0; amount >= 1.0/64; amount /= 2 {
		n := color.NRGBA{roundByte(float64(c.R) * (1 + (wb[0]-1)*amount)), roundByte(float64(c.G) * (1 + (wb[1]-1)*amount)), roundByte(float64(c.B) * (1 + (wb[2]-1)*amount)), c.A}
		if luminance(n)+.001 >= luminance(c) && hueDelta(c, n) < 2.8 {
			base = n
			break
		}
	}
	// Approach the requested exposure without clipping channels or shifting hue.
	factor := min(exposure, 255/float64(max(1, base.R, base.G, base.B)))
	n := color.NRGBA{roundByte(float64(base.R) * factor), roundByte(float64(base.G) * factor), roundByte(float64(base.B) * factor), base.A}
	if hueDelta(c, n) >= 2.9 {
		return c
	}
	return n
}
func Retouch(src *image.NRGBA, code string) (*image.NRGBA, Object) {
	src = resize(src, 2400)
	wb := [3]float64{1, 1, 1}
	exp := 1.0
	if code != "E5" {
		var sums [3]float64
		var count int
		var lum float64
		for y := 0; y < src.Rect.Dy(); y++ {
			for x := 0; x < src.Rect.Dx(); x++ {
				c := src.NRGBAAt(x, y)
				lum += luminance(c)
				lo, hi := min(c.R, c.G, c.B), max(c.R, c.G, c.B)
				if lo > 30 && hi < 235 && float64(hi-lo)/float64(hi) < .15 {
					sums[0] += float64(c.R)
					sums[1] += float64(c.G)
					sums[2] += float64(c.B)
					count++
				}
			}
		}
		if count > 0 {
			target := (sums[0] + sums[1] + sums[2]) / 3
			for k := range wb {
				wb[k] = clamp(target/sums[k], .94, 1.06)
			}
		}
		mean := lum / float64(src.Rect.Dx()*src.Rect.Dy())
		if mean > 0 {
			exp = clamp(150/mean, 1, 1.25)
		}
	}
	baseClip := clipRatio(src)
	apply := func(wb [3]float64, g float64) *image.NRGBA {
		d := image.NewNRGBA(src.Rect)
		for y := 0; y < src.Rect.Dy(); y++ {
			for x := 0; x < src.Rect.Dx(); x++ {
				d.SetNRGBA(x, y, corrected(src.NRGBAAt(x, y), wb, g))
			}
		}
		return d
	}
	if code == "E5" {
		return src, Object{"wb_gain": wb, "exposure_gain": 1, "clip_before": baseClip, "clip_after": baseClip, "exif_removed": true, "color_skipped": true}
	}
	d := apply(wb, 1)
	if clipRatio(d) > baseClip+.005 {
		wb = [3]float64{1, 1, 1}
	}
	d = apply(wb, exp)
	if clipRatio(d) > baseClip+.005 {
		lo, hi := 1.0, exp
		for i := 0; i < 12; i++ {
			mid := (lo + hi) / 2
			if clipRatio(apply(wb, mid)) <= baseClip+.005 {
				lo = mid
			} else {
				hi = mid
			}
		}
		exp = lo
		d = apply(wb, exp)
	}
	return d, Object{"wb_gain": wb, "exposure_gain": exp, "clip_before": baseClip, "clip_after": clipRatio(d), "exif_removed": true, "color_skipped": false, "hue_guard_degrees": 2.9}
}
func Metrics(im *image.NRGBA, exif Exif) (Object, uint64) {
	s := resize(im, 512)
	w, h := s.Rect.Dx(), s.Rect.Dy()
	var sum, sum2 float64
	var cnt, dark int
	var channels [3]float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := s.NRGBAAt(x, y)
			channels[0] += float64(c.R)
			channels[1] += float64(c.G)
			channels[2] += float64(c.B)
			if luminance(c) < 60 {
				dark++
			}
			if x > 0 && y > 0 && x < w-1 && y < h-1 {
				v := 4*luminance(c) - luminance(s.NRGBAAt(x-1, y)) - luminance(s.NRGBAAt(x+1, y)) - luminance(s.NRGBAAt(x, y-1)) - luminance(s.NRGBAAt(x, y+1))
				sum += v
				sum2 += v * v
				cnt++
			}
		}
	}
	variance := 0.0
	if cnt > 0 {
		variance = sum2/float64(cnt) - math.Pow(sum/float64(cnt), 2)
	}
	blur := "清晰"
	if variance < 100 {
		blur = "模糊"
	} else if variance < 500 {
		blur = "一般"
	}
	clip := clipRatio(im)
	level := "正常"
	if clip > .05 {
		level = "严重"
	} else if clip > .01 {
		level = "偏多"
	}
	cast := "中性"
	if channels[0] > channels[2]*1.12 {
		cast = "偏黄"
	} else if channels[2] > channels[0]*1.12 {
		cast = "偏蓝"
	}
	var hash uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			a := s.NRGBAAt(x*(w-1)/8, y*(h-1)/7)
			b := s.NRGBAAt((x+1)*(w-1)/8, y*(h-1)/7)
			if luminance(a) > luminance(b) {
				hash |= 1 << uint(y*8+x)
			}
		}
	}
	return Object{"blur": variance, "blur_level": blur, "clip_high": clip, "clip_level": level, "dark": float64(dark) / float64(w*h), "cast_note": cast, "dup_of": nil, "has_gps": exif.GPS, "orientation": exif.Orientation, "error": ""}, hash
}
