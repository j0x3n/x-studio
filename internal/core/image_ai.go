package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"golang.org/x/image/draw"
)

type imageModelKey struct{}
type imageInfoKey struct{}

type ImageInfo struct {
	Model    string
	Returned string
	Fallback bool
}

func WithImageModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, imageModelKey{}, model)
}

func WithImageInfo(ctx context.Context, info *ImageInfo) context.Context {
	return context.WithValue(ctx, imageInfoKey{}, info)
}

func FitToSize(im image.Image, size string) image.Image {
	var width, height int
	if im == nil || !validImageSize(size) {
		return im
	}
	_, _ = fmt.Sscanf(size, "%dx%d", &width, &height)
	if im.Bounds().Size() == image.Pt(width, height) {
		return im
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.NRGBA{R: 128, G: 128, B: 128, A: 255}), image.Point{}, draw.Src)
	scale := math.Min(float64(width)/float64(im.Bounds().Dx()), float64(height)/float64(im.Bounds().Dy()))
	fitWidth := max(1, int(math.Round(float64(im.Bounds().Dx())*scale)))
	fitHeight := max(1, int(math.Round(float64(im.Bounds().Dy())*scale)))
	left, top := (width-fitWidth)/2, (height-fitHeight)/2
	draw.CatmullRom.Scale(canvas, image.Rect(left, top, left+fitWidth, top+fitHeight), im, im.Bounds(), draw.Over, nil)
	return canvas
}

func ImageSize(im image.Image) string {
	ratio := float64(im.Bounds().Dx()) / float64(im.Bounds().Dy())
	best := "1024x1024"
	distance := math.Inf(1)
	for _, s := range []struct {
		name  string
		ratio float64
	}{{"1024x1024", 1}, {"1024x1536", 2.0 / 3}, {"1536x1024", 1.5}} {
		d := math.Abs(ratio - s.ratio)
		if d < distance {
			best, distance = s.name, d
		}
	}
	return best
}

// EditImage sends only PNG references, never URLs or generated-image references
// supplied by a model. The image client has its own timeout (not the text 120s).
func (a *Core) EditImage(ctx context.Context, prompt string, refs []image.Image) ([]byte, error) {
	if len(refs) == 0 || refs[0] == nil {
		return nil, fmt.Errorf("图像编辑需要 1–3 张参考图")
	}
	return a.EditImageSize(ctx, prompt, refs, ImageSize(refs[0]))
}

// EditImageSize preserves reference order and uses an explicit output size.
func (a *Core) EditImageSize(ctx context.Context, prompt string, refs []image.Image, size string) ([]byte, error) {
	return a.EditImageMask(ctx, prompt, refs, nil, size)
}

func (a *Core) EditImageMask(ctx context.Context, prompt string, refs []image.Image, mask image.Image, size string) ([]byte, error) {
	if !validImageSize(size) {
		return nil, fmt.Errorf("图片尺寸不正确")
	}
	if a.Cfg.AIKey == "" {
		return nil, fmt.Errorf("未配置 AI_API_KEY，无法生成或高清化照片")
	}
	if len(refs) == 0 || len(refs) > 3 {
		return nil, fmt.Errorf("图像编辑需要 1–3 张参考图")
	}
	for _, ref := range refs {
		if ref == nil {
			return nil, fmt.Errorf("参考图不能为空")
		}
	}
	if mask != nil && mask.Bounds().Size() != refs[0].Bounds().Size() {
		return nil, fmt.Errorf("mask 必须与第一张参考图同尺寸")
	}
	if mask == nil {
		refs = append([]image.Image(nil), refs...)
		refs[0] = FitToSize(refs[0], size)
	}
	return a.imageRequest(ctx, "/images/edits", size, func(model string) (string, []byte, error) {
		return imageEditBody(model, prompt, size, refs, mask)
	})
}

func imageEditBody(model, prompt, size string, refs []image.Image, mask image.Image) (string, []byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, v := range []struct{ k, v string }{{"model", model}, {"prompt", prompt}, {"size", size}, {"quality", "high"}} {
		if err := mw.WriteField(v.k, v.v); err != nil {
			return "", nil, fmt.Errorf("准备图像编辑请求失败")
		}
	}
	for i, im := range refs {
		// 图像服务按每段的 Content-Type 判断格式，CreateFormFile 给的是 octet-stream 会被拒（HTTP 400）
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename="reference-%d.png"`, i+1))
		h.Set("Content-Type", "image/png")
		part, err := mw.CreatePart(h)
		if err != nil {
			return "", nil, err
		}
		if err = png.Encode(part, im); err != nil {
			return "", nil, err
		}
	}
	if mask != nil {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="mask"; filename="mask.png"`)
		header.Set("Content-Type", "image/png")
		part, err := mw.CreatePart(header)
		if err != nil {
			return "", nil, err
		}
		if err = png.Encode(part, mask); err != nil {
			return "", nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return "", nil, err
	}
	return mw.FormDataContentType(), body.Bytes(), nil
}

func validImageSize(size string) bool {
	return size == "1024x1024" || size == "1024x1536" || size == "1536x1024"
}

// GenerateImage uses the same provider, quota reservation and response checks as edits.
func (a *Core) GenerateImage(ctx context.Context, prompt, size string, styleRefs ...image.Image) ([]byte, error) {
	if len(styleRefs) > 0 {
		return a.EditImageSize(ctx, prompt, styleRefs, size)
	}
	if !validImageSize(size) {
		return nil, fmt.Errorf("图片尺寸不正确")
	}
	if a.Cfg.AIKey == "" {
		return nil, fmt.Errorf("未配置 AI_API_KEY，无法生成照片")
	}
	return a.imageRequest(ctx, "/images/generations", size, func(model string) (string, []byte, error) {
		return "application/json", []byte(JS(Object{"model": model, "prompt": prompt, "size": size, "quality": "high", "n": 1})), nil
	})
}

func (a *Core) imageRequest(ctx context.Context, endpoint, size string, build func(string) (string, []byte, error)) ([]byte, error) {
	primary, alternate := a.ImageModels(ctx)
	client := *a.Client
	client.Timeout = a.Cfg.ImageTimeout
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		model := primary
		if attempt > 0 && alternate != "" {
			model = alternate
		}
		contentType, body, err := build(model)
		if err != nil {
			return nil, err
		}
		finish, err := a.beginUsage(ctx, "image", model)
		if err != nil {
			return nil, err
		}
		requestCtx, cancel := context.WithTimeout(ctx, a.Cfg.ImageTimeout)
		returned := ""
		data, err := func() ([]byte, error) {
			req, err := http.NewRequestWithContext(requestCtx, "POST", strings.TrimRight(a.Cfg.AIBaseURL, "/")+endpoint, bytes.NewReader(body))
			if err != nil {
				return nil, fmt.Errorf("图像接口地址不正确")
			}
			req.Header.Set("Content-Type", contentType)
			req.Header.Set("Authorization", "Bearer "+a.Cfg.AIKey)
			res, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("图像请求失败或超时")
			}
			defer res.Body.Close()
			if res.StatusCode < 200 || res.StatusCode >= 300 {
				return nil, fmt.Errorf("图像服务返回 HTTP %d", res.StatusCode)
			}
			b, err := io.ReadAll(io.LimitReader(res.Body, (128<<20)+1))
			if err != nil || len(b) > 128<<20 {
				return nil, fmt.Errorf("图像回复读取失败或过大")
			}
			var response struct {
				Data []struct {
					Base64 string `json:"b64_json"`
				} `json:"data"`
			}
			if json.Unmarshal(b, &response) != nil || len(response.Data) != 1 || response.Data[0].Base64 == "" {
				return nil, fmt.Errorf("图像服务没有返回 data[0].b64_json")
			}
			decoded, err := base64.StdEncoding.DecodeString(response.Data[0].Base64)
			if err != nil {
				return nil, fmt.Errorf("图像回复的 base64 不正确")
			}
			if cfg, format, err := image.DecodeConfig(bytes.NewReader(decoded)); err != nil || format != "png" || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
				return nil, fmt.Errorf("图像回复不是有效的 PNG 或像素超限")
			}
			im, err := png.Decode(bytes.NewReader(decoded))
			if err != nil {
				return nil, fmt.Errorf("图像服务返回的 PNG 已损坏")
			}
			cfg, _, _ := image.DecodeConfig(bytes.NewReader(decoded))
			returned = fmt.Sprintf("%dx%d", cfg.Width, cfg.Height)
			if returned != size {
				var width, height int
				_, _ = fmt.Sscanf(size, "%dx%d", &width, &height)
				if compatibleImageSize(width, height, cfg.Width, cfg.Height) {
					normalized := resizeImageOutput(im, width, height)
					var encoded bytes.Buffer
					if err := png.Encode(&encoded, normalized); err != nil {
						return nil, fmt.Errorf("图像回复编码失败")
					}
					return encoded.Bytes(), nil
				}
				return nil, fmt.Errorf("图像服务返回的尺寸不对：请求 %s，返回 %d×%d", strings.ReplaceAll(size, "x", "×"), cfg.Width, cfg.Height)
			}
			return decoded, nil
		}()
		cancel()
		if err == nil {
			if e := finish(true); e != nil {
				return nil, e
			}
			if info, _ := ctx.Value(imageInfoKey{}).(*ImageInfo); info != nil {
				*info = ImageInfo{Model: model, Returned: returned, Fallback: model != primary}
			}
			return data, nil
		}
		_ = finish(false)
		last = err
	}
	return nil, last
}

func compatibleImageSize(width, height, returnedWidth, returnedHeight int) bool {
	if (width < height && returnedWidth > returnedHeight) || (width > height && returnedWidth < returnedHeight) || min(returnedWidth, returnedHeight)*10 < min(width, height)*9 {
		return false
	}
	difference := int64(returnedWidth)*int64(height) - int64(width)*int64(returnedHeight)
	if difference < 0 {
		difference = -difference
	}
	return difference*100 <= int64(width)*int64(returnedHeight)
}

func resizeImageOutput(source image.Image, width, height int) image.Image {
	scale := math.Max(float64(width)/float64(source.Bounds().Dx()), float64(height)/float64(source.Bounds().Dy()))
	scaledWidth := int(math.Ceil(float64(source.Bounds().Dx()) * scale))
	scaledHeight := int(math.Ceil(float64(source.Bounds().Dy()) * scale))
	left, top := (width-scaledWidth)/2, (height-scaledHeight)/2
	output := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(output, image.Rect(left, top, left+scaledWidth, top+scaledHeight), source, source.Bounds(), draw.Src, nil)
	return output
}
