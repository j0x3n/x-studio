package imaging

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"strings"
)

// fallbackMask removes only background connected to an image edge. Enclosed
// pearl highlights are never independently classified as background.
func fallbackMask(src *image.NRGBA) *image.Alpha {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	mask := image.NewAlpha(src.Rect)
	for i := range mask.Pix {
		mask.Pix[i] = 255
	}
	var avg [3]float64
	points := []image.Point{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}}
	for _, p := range points {
		c := src.NRGBAAt(p.X, p.Y)
		avg[0] += float64(c.R) / 4
		avg[1] += float64(c.G) / 4
		avg[2] += float64(c.B) / 4
	}
	seen := make([]bool, w*h)
	queue := make([]int, 0, w*h/4)
	add := func(x, y int) {
		i := y*w + x
		if seen[i] {
			return
		}
		seen[i] = true
		c := src.NRGBAAt(x, y)
		dist := math.Sqrt(math.Pow(float64(c.R)-avg[0], 2) + math.Pow(float64(c.G)-avg[1], 2) + math.Pow(float64(c.B)-avg[2], 2))
		if dist <= 35 || c.A == 0 {
			mask.Pix[mask.PixOffset(x, y)] = 0
			queue = append(queue, i)
		}
	}
	for x := 0; x < w; x++ {
		add(x, 0)
		add(x, h-1)
	}
	for y := 0; y < h; y++ {
		add(0, y)
		add(w-1, y)
	}
	for head := 0; head < len(queue); head++ {
		i := queue[head]
		x, y := i%w, i/w
		if x > 0 {
			add(x-1, y)
		}
		if x < w-1 {
			add(x+1, y)
		}
		if y > 0 {
			add(x, y-1)
		}
		if y < h-1 {
			add(x, y+1)
		}
	}
	return mask
}
func Mask(ctx context.Context, src *image.NRGBA, address string, client *http.Client) (*image.Alpha, string) {
	if strings.TrimSpace(address) == "" {
		return fallbackMask(src), "fallback"
	}
	ctx, cancel := context.WithTimeout(ctx, 60e9)
	defer cancel()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	f, e := mw.CreateFormFile("file", "photo.png")
	if e == nil {
		e = png.Encode(f, src)
	}
	if e == nil {
		e = mw.Close()
	}
	if e != nil {
		return fallbackMask(src), "fallback"
	}
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(address, "/")+"/api/remove", &body)
	if e != nil {
		return fallbackMask(src), "fallback"
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, e := client.Do(req)
	if e != nil {
		return fallbackMask(src), "fallback"
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fallbackMask(src), "fallback"
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if e != nil {
		return fallbackMask(src), "fallback"
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || cfg.Width != src.Rect.Dx() || cfg.Height != src.Rect.Dy() {
		return fallbackMask(src), "fallback"
	}
	im, _, e := image.Decode(bytes.NewReader(b))
	if e != nil {
		return fallbackMask(src), "fallback"
	}
	m := image.NewAlpha(src.Rect)
	for y := 0; y < src.Rect.Dy(); y++ {
		for x := 0; x < src.Rect.Dx(); x++ {
			_, _, _, v := im.At(x+im.Bounds().Min.X, y+im.Bounds().Min.Y).RGBA()
			m.SetAlpha(x, y, color.Alpha{uint8(v >> 8)})
		}
	}
	return m, "rembg"
}

// ReplaceBackground preserves the source geometry and the mask's soft edges.
func ReplaceBackground(src *image.NRGBA, mask *image.Alpha, transparent bool) *image.NRGBA {
	out := image.NewNRGBA(src.Rect)
	for y := src.Rect.Min.Y; y < src.Rect.Max.Y; y++ {
		for x := src.Rect.Min.X; x < src.Rect.Max.X; x++ {
			c := src.NRGBAAt(x, y)
			alpha := mask.AlphaAt(x, y).A
			if transparent {
				c.A = alpha
			} else {
				a := uint32(alpha)
				blend := func(v uint8) uint8 { return uint8((uint32(v)*a + 255*(255-a) + 127) / 255) }
				c = color.NRGBA{blend(c.R), blend(c.G), blend(c.B), 255}
			}
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}

// CompositeWhite uses nearest-neighbor geometry deliberately: a fully opaque
// pixel is copied exactly from the retouched source, without interpolation or
// any color from rembg. PNG retains this guarantee; JPEG is inherently lossy.
func CompositeWhite(src *image.NRGBA, mask *image.Alpha) (*image.NRGBA, *image.NRGBA, error) {
	box := image.Rectangle{}
	first := true
	for y := 0; y < src.Rect.Dy(); y++ {
		for x := 0; x < src.Rect.Dx(); x++ {
			if mask.AlphaAt(x, y).A > 0 {
				r := image.Rect(x, y, x+1, y+1)
				if first {
					box = r
					first = false
				} else {
					box = box.Union(r)
				}
			}
		}
	}
	if first {
		return nil, nil, fmt.Errorf("抠图没有识别出商品，请改用背景反差更大的照片")
	}
	scale := 780 / float64(max(box.Dx(), box.Dy()))
	w := max(1, int(math.Round(float64(box.Dx())*scale)))
	h := max(1, int(math.Round(float64(box.Dy())*scale)))
	left, top := (1200-w)/2, (1200-h)/2
	white := image.NewNRGBA(image.Rect(0, 0, 1200, 1200))
	transparent := image.NewNRGBA(white.Rect)
	for i := range white.Pix {
		white.Pix[i] = 255
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := box.Min.X + min(box.Dx()-1, x*box.Dx()/w)
			sy := box.Min.Y + min(box.Dy()-1, y*box.Dy()/h)
			c := src.NRGBAAt(sx, sy)
			alpha := mask.AlphaAt(sx, sy).A
			tx, ty := left+x, top+y
			c.A = alpha
			transparent.SetNRGBA(tx, ty, c)
			if alpha == 255 {
				white.SetNRGBA(tx, ty, c)
			} else if alpha > 0 {
				a := uint32(alpha)
				blend := func(v uint8) uint8 { return uint8((uint32(v)*a + 255*(255-a) + 127) / 255) }
				white.SetNRGBA(tx, ty, color.NRGBA{blend(c.R), blend(c.G), blend(c.B), 255})
			}
		}
	}
	return white, transparent, nil
}
