package imaging

import (
	"image"
	"image/draw"
	"math"
	"sort"

	xdraw "golang.org/x/image/draw"
)

type Object = map[string]any

func Resize(im image.Image, side int) *image.NRGBA     { return resize(im, side) }
func Load(filename string) (*image.NRGBA, Exif, error) { return loadPhoto(filename) }
func SaveJPEG(filename string, im image.Image) error   { return saveJPEG(filename, im, 92) }
func SavePNG(filename string, im image.Image) error    { return savePNG(filename, im) }

// Hash uses the median of 63 low-frequency DCT coefficients for a 64-bit pHash.
func Hash(im image.Image) uint64 {
	small := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	xdraw.CatmullRom.Scale(small, small.Bounds(), im, im.Bounds(), draw.Src, nil)
	var cosines [8][32]float64
	for k := range cosines {
		for x := range cosines[k] {
			cosines[k][x] = math.Cos(float64(2*x+1) * float64(k) * math.Pi / 64)
		}
	}
	var coefficients [64]float64
	for v := 0; v < 8; v++ {
		for u := 0; u < 8; u++ {
			for y := 0; y < 32; y++ {
				for x := 0; x < 32; x++ {
					coefficients[v*8+u] += luminance(small.NRGBAAt(x, y)) * cosines[u][x] * cosines[v][y]
				}
			}
		}
	}
	values := append([]float64(nil), coefficients[1:]...)
	sort.Float64s(values)
	median := values[len(values)/2]
	var hash uint64
	for i, v := range coefficients {
		if i > 0 && v > median {
			hash |= uint64(1) << i
		}
	}
	return hash
}
