package pipeline

import (
	"context"
	"image"
	"xstudio/internal/core"
)

func (a *Module) mask(ctx context.Context, src *image.NRGBA) (*image.Alpha, string) {
	return a.ProductMask(ctx, src)
}
func CompositeWhite(src *image.NRGBA, mask *image.Alpha) (*image.NRGBA, *image.NRGBA, error) {
	return core.CompositeWhite(src, mask)
}
