package pipeline

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
)

func whiteParamKey(ph Photo) string {
	crop := "null"
	if len(ph.Crop) == 4 {
		parts := make([]string, 4)
		for i, v := range ph.Crop {
			parts[i] = strconv.FormatFloat(v, 'g', -1, 64)
		}
		crop = strings.Join(parts, ",")
	}
	return crop + "|" + strconv.Itoa(ph.Rotate) + "|" + ph.EnhancedFile
}

func nearSquare(w, h int) bool {
	if w < 1 || h < 1 {
		return false
	}
	ratio := float64(w) / float64(h)
	return ratio >= 0.97 && ratio <= 1.03
}

func fitWhiteSquare(src image.Image, side int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, side, side))
	for i := 0; i < len(dst.Pix); i += 4 {
		dst.Pix[i] = 255
		dst.Pix[i+1] = 255
		dst.Pix[i+2] = 255
		dst.Pix[i+3] = 255
	}
	if src == nil || side < 1 {
		return dst
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return dst
	}
	if nearSquare(w, h) {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
		return dst
	}
	scale := math.Min(float64(side)/float64(w), float64(side)/float64(h))
	fw := max(1, int(math.Round(float64(w)*scale)))
	fh := max(1, int(math.Round(float64(h)*scale)))
	if fw > side {
		fw = side
	}
	if fh > side {
		fh = side
	}
	left, top := (side-fw)/2, (side-fh)/2
	xdraw.CatmullRom.Scale(dst, image.Rect(left, top, left+fw, top+fh), src, b, draw.Over, nil)
	return dst
}

func nearPureWhite(c color.NRGBA) bool {
	return c.R >= 245 && c.G >= 245 && c.B >= 245
}

func knockoutWhite(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	if w < 1 || h < 1 {
		return dst
	}
	bg := make([]bool, w*h)
	seen := make([]bool, w*h)
	qx := make([]int, 0, w*2)
	qy := make([]int, 0, w*2)
	push := func(x, y int) {
		if x < 0 || y < 0 || x >= w || y >= h {
			return
		}
		i := y*w + x
		if seen[i] {
			return
		}
		seen[i] = true
		c := src.NRGBAAt(b.Min.X+x, b.Min.Y+y)
		dst.SetNRGBA(x, y, c)
		if nearPureWhite(c) {
			bg[i] = true
			qx = append(qx, x)
			qy = append(qy, y)
		}
	}
	for x := 0; x < w; x++ {
		push(x, 0)
		if h > 1 {
			push(x, h-1)
		}
	}
	for y := 0; y < h; y++ {
		push(0, y)
		if w > 1 {
			push(w-1, y)
		}
	}
	for head := 0; head < len(qx); head++ {
		x, y := qx[head], qy[head]
		push(x-1, y)
		push(x+1, y)
		push(x, y-1)
		push(x, y+1)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			if !seen[i] {
				dst.SetNRGBA(x, y, src.NRGBAAt(b.Min.X+x, b.Min.Y+y))
			}
			c := dst.NRGBAAt(x, y)
			if !bg[i] {
				c.A = 255
				dst.SetNRGBA(x, y, c)
				continue
			}
			feather := false
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nx, ny := x+d[0], y+d[1]
				if nx < 0 || ny < 0 || nx >= w || ny >= h {
					continue
				}
				if !bg[ny*w+nx] {
					feather = true
					break
				}
			}
			if feather {
				c.A = 128
			} else {
				c.A = 0
			}
			dst.SetNRGBA(x, y, c)
		}
	}
	return dst
}

func whiteOutputs(data []byte) (white, transparent *image.NRGBA, returned string, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 {
		return nil, nil, "", fmt.Errorf("白底图无法读取")
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, "", fmt.Errorf("白底图无法读取")
	}
	white = fitWhiteSquare(im, 1200)
	return white, knockoutWhite(white), fmt.Sprintf("%dx%d", cfg.Width, cfg.Height), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		return err
	}
	return cerr
}
