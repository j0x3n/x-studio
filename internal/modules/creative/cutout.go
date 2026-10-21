package creative

import (
	"image"
	"xstudio/internal/core"
)

func variantPhotos(item Item, photos []core.Photo) []core.Photo {
	if item.Variant == "B" && (item.ProductMode == "pixel_lock" || item.ProductMode == "model" || item.ProductMode == "redraw") && len(photos) > 1 {
		photos[0], photos[1] = photos[1], photos[0]
	}
	return photos
}

func maskCoverage(mask *image.Alpha) float64 {
	if mask == nil || mask.Rect.Empty() {
		return 0
	}
	opaque := 0
	for row := mask.Rect.Min.Y; row < mask.Rect.Max.Y; row++ {
		for column := mask.Rect.Min.X; column < mask.Rect.Max.X; column++ {
			if mask.AlphaAt(column, row).A >= 128 {
				opaque++
			}
		}
	}
	return float64(opaque) / float64(mask.Rect.Dx()*mask.Rect.Dy())
}

func trustedCutout(mode string, coverage float64) bool {
	return coverage >= 0.003 && coverage <= 0.85 && (mode != "fallback" || coverage <= 0.6)
}
