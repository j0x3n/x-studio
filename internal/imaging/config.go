package imaging

import (
	"bytes"
	"fmt"
	"image"
)

const maxPixels int64 = 40_000_000

func imageConfig(b []byte) (image.Config, string, error) {
	c, f, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || (f != "jpeg" && f != "png" && f != "webp") {
		return c, "", fmt.Errorf("只接受真实的 JPG、PNG、WebP 图片")
	}
	if c.Width < 1 || c.Height < 1 || int64(c.Width)*int64(c.Height) > maxPixels {
		return c, "", fmt.Errorf("照片像素过大，最多 4000 万像素")
	}
	return c, f, nil
}
