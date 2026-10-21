package creative

import (
	"bytes"
	"fmt"
	xdraw "golang.org/x/image/draw"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/imaging"
)

func imageSize(size string) (int, int) {
	var width, height int
	_, _ = fmt.Sscanf(size, "%dx%d", &width, &height)
	return width, height
}
func nrgba(src image.Image) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	return out
}
func pngBytes(src image.Image) ([]byte, error) {
	var buf bytes.Buffer
	err := png.Encode(&buf, src)
	return buf.Bytes(), err
}
func loadImage(rootPath, name string) (*image.NRGBA, error) {
	if strings.ContainsAny(name, "\\:") {
		return nil, fmt.Errorf("图片路径不合法")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	im, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	return nrgba(im), nil
}
func (a *Module) sourceImage(ph core.Photo) (*image.NRGBA, error) {
	name := ph.File
	if ph.EnhancedFile != "" {
		name = ph.EnhancedFile
	}
	root, err := os.OpenRoot(a.Dir(ph.ProductID))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := root.ReadFile(name)
	if err != nil {
		return nil, err
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if ph.EnhancedFile != "" {
		return nrgba(im), nil
	}
	ex := imaging.ReadExif(data)
	return imaging.Geometry(im, ex.Orientation, ph.Rotate, ph.Crop), nil
}
func alphaBounds(mask *image.Alpha) image.Rectangle {
	box := image.Rectangle{}
	for y := mask.Rect.Min.Y; y < mask.Rect.Max.Y; y++ {
		for x := mask.Rect.Min.X; x < mask.Rect.Max.X; x++ {
			if mask.AlphaAt(x, y).A > 0 {
				point := image.Rect(x, y, x+1, y+1)
				if box.Empty() {
					box = point
				} else {
					box = box.Union(point)
				}
			}
		}
	}
	return box
}
func productRect(srcBox image.Rectangle, box []float64, width, height int) image.Rectangle {
	area := image.Rect(int(box[0]*float64(width)), int(box[1]*float64(height)), int((box[0]+box[2])*float64(width)), int((box[1]+box[3])*float64(height)))
	scale := math.Min(float64(area.Dx())/float64(srcBox.Dx()), float64(area.Dy())/float64(srcBox.Dy()))
	pw, ph := max(1, int(math.Round(float64(srcBox.Dx())*scale))), max(1, int(math.Round(float64(srcBox.Dy())*scale)))
	left, top := area.Min.X+(area.Dx()-pw)/2, area.Min.Y+(area.Dy()-ph)/2
	return image.Rect(left, top, left+pw, top+ph)
}
func placeProduct(src *image.NRGBA, mask *image.Alpha, srcBox, dest image.Rectangle, width, height int) (*image.NRGBA, *image.Alpha) {
	canvas := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Rect, image.NewUniform(color.NRGBA{128, 128, 128, 255}), image.Point{}, draw.Src)
	alpha := image.NewAlpha(canvas.Rect)
	for y := dest.Min.Y; y < dest.Max.Y; y++ {
		for x := dest.Min.X; x < dest.Max.X; x++ {
			if !image.Pt(x, y).In(canvas.Rect) {
				continue
			}
			sx := srcBox.Min.X + min(srcBox.Dx()-1, (x-dest.Min.X)*srcBox.Dx()/dest.Dx())
			sy := srcBox.Min.Y + min(srcBox.Dy()-1, (y-dest.Min.Y)*srcBox.Dy()/dest.Dy())
			pixel := src.NRGBAAt(sx, sy)
			pixel.A = 255
			canvas.SetNRGBA(x, y, pixel)
			alpha.SetAlpha(x, y, mask.AlphaAt(sx, sy))
		}
	}
	return canvas, alpha
}
func lockGeometry(src *image.NRGBA, mask *image.Alpha, box []float64, width, height int, shape string) (image.Rectangle, image.Rectangle) {
	if shape == "rect" {
		area := image.Rect(int(box[0]*float64(width)), int(box[1]*float64(height)), int((box[0]+box[2])*float64(width)), int((box[1]+box[3])*float64(height)))
		return cropFor(src.Rect, max(1, area.Dx()), max(1, area.Dy())), area
	}
	bounds := alphaBounds(mask)
	return bounds, productRect(bounds, box, width, height)
}
func lockCanvas(src *image.NRGBA, mask *image.Alpha, box []float64, width, height int, shapes ...string) (*image.NRGBA, *image.Alpha, error) {
	bounds := alphaBounds(mask)
	if bounds.Empty() {
		return nil, nil, fmt.Errorf("抠图没有识别出商品")
	}
	if !validBox(box) {
		return nil, nil, fmt.Errorf("商品位置不正确")
	}
	shape := ""
	if len(shapes) > 0 {
		shape = shapes[0]
	}
	bounds, destination := lockGeometry(src, mask, box, width, height, shape)
	canvas, alpha := placeProduct(src, mask, bounds, destination, width, height)
	return canvas, alpha, nil
}
func pasteLocked(background, canvas *image.NRGBA, alpha *image.Alpha) *image.NRGBA {
	out := nrgba(background)
	for y := 0; y < out.Rect.Dy(); y++ {
		for x := 0; x < out.Rect.Dx(); x++ {
			value := int(alpha.AlphaAt(x, y).A)
			if value == 0 {
				continue
			}
			edge := 3
			for offsetY := -2; offsetY <= 2; offsetY++ {
				for offsetX := -2; offsetX <= 2; offsetX++ {
					if alpha.AlphaAt(x+offsetX, y+offsetY).A == 0 {
						edge = min(edge, max(abs(offsetX), abs(offsetY)))
					}
				}
			}
			if edge < 3 {
				value = value * edge / 3
			}
			source := canvas.NRGBAAt(x, y)
			base := out.NRGBAAt(x, y)
			blend := func(front, back uint8) uint8 { return uint8((int(front)*value + int(back)*(255-value) + 127) / 255) }
			out.SetNRGBA(x, y, color.NRGBA{blend(source.R, base.R), blend(source.G, base.G), blend(source.B, base.B), 255})
		}
	}
	return out
}
func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
func pixelCheck(result, canvas *image.NRGBA, alpha *image.Alpha) PixelCheck {
	check := PixelCheck{Pass: true}
	if result.Rect != canvas.Rect || alpha.Rect != canvas.Rect {
		return PixelCheck{Pass: false, MaxDiff: 255}
	}
	for y := 3; y < result.Rect.Dy()-3; y++ {
		for x := 3; x < result.Rect.Dx()-3; x++ {
			if alpha.AlphaAt(x, y).A < 253 {
				continue
			}
			interior := true
			for offsetY := -3; offsetY <= 3 && interior; offsetY++ {
				for offsetX := -3; offsetX <= 3; offsetX++ {
					if alpha.AlphaAt(x+offsetX, y+offsetY).A < 253 {
						interior = false
						break
					}
				}
			}
			if !interior {
				continue
			}
			actual, expected := result.NRGBAAt(x, y), canvas.NRGBAAt(x, y)
			check.MaxDiff = max(check.MaxDiff, abs(int(actual.R)-int(expected.R)), abs(int(actual.G)-int(expected.G)), abs(int(actual.B)-int(expected.B)))
		}
	}
	check.Pass = check.MaxDiff <= 2
	return check
}
func cropFor(src image.Rectangle, width, height int) image.Rectangle {
	ratio := float64(width) / float64(height)
	if float64(src.Dx())/float64(src.Dy()) > ratio {
		w := int(math.Round(float64(src.Dy()) * ratio))
		src.Min.X += (src.Dx() - w) / 2
		src.Max.X = src.Min.X + w
	} else {
		h := int(math.Round(float64(src.Dx()) / ratio))
		src.Min.Y += (src.Dy() - h) / 2
		src.Max.Y = src.Min.Y + h
	}
	return src
}
func resizeCrop(src *image.NRGBA, width, height int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(out, out.Rect, src, cropFor(src.Rect, width, height), draw.Src, nil)
	return out
}
func exportLocked(result, original *image.NRGBA, mask *image.Alpha, box []float64, width, height int, shapes ...string) (*image.NRGBA, PixelCheck) {
	crop := cropFor(result.Rect, width, height)
	bounds := alphaBounds(mask)
	if bounds.Empty() {
		return nil, PixelCheck{Pass: false, MaxDiff: 255}
	}
	shape := ""
	if len(shapes) > 0 {
		shape = shapes[0]
	}
	bounds, position := lockGeometry(original, mask, box, result.Rect.Dx(), result.Rect.Dy(), shape)
	transformX := func(x int) int { return int(math.Round(float64(x-crop.Min.X) * float64(width) / float64(crop.Dx()))) }
	transformY := func(y int) int { return int(math.Round(float64(y-crop.Min.Y) * float64(height) / float64(crop.Dy()))) }
	destination := image.Rect(transformX(position.Min.X), transformY(position.Min.Y), transformX(position.Max.X), transformY(position.Max.Y))
	canvas, alpha := placeProduct(original, mask, bounds, destination, width, height)
	out := pasteLocked(resizeCrop(result, width, height), canvas, alpha)
	return out, pixelCheck(out, canvas, alpha)
}
func writePNG(rootPath, name string, im image.Image) error {
	if err := os.MkdirAll(filepath.Dir(filepath.Join(rootPath, filepath.FromSlash(name))), 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer root.Close()
	data, err := pngBytes(im)
	if err != nil {
		return err
	}
	return root.WriteFile(name, data, 0600)
}
func jpegBytes(im image.Image) ([]byte, error) {
	for quality := 92; quality >= 10; quality -= 2 {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, im, &jpeg.Options{Quality: quality}); err != nil {
			return nil, err
		}
		if buf.Len() <= 900*1024 {
			return buf.Bytes(), nil
		}
	}
	return nil, fmt.Errorf("JPEG 超过 900KB")
}
