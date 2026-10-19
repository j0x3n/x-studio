package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
)

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
	if len(refs) == 0 {
		return nil, fmt.Errorf("图像编辑需要 1–3 张参考图")
	}
	return a.EditImageSize(ctx, prompt, refs, ImageSize(refs[0]))
}

// EditImageSize preserves reference order and uses an explicit output size.
func (a *Core) EditImageSize(ctx context.Context, prompt string, refs []image.Image, size string) ([]byte, error) {
	if !validImageSize(size) {
		return nil, fmt.Errorf("图片尺寸不正确")
	}
	if a.Cfg.AIKey == "" {
		return nil, fmt.Errorf("未配置 AI_API_KEY，无法生成或高清化照片")
	}
	if len(refs) == 0 || len(refs) > 3 {
		return nil, fmt.Errorf("图像编辑需要 1–3 张参考图")
	}
	model := a.Model(Actor(ctx).Feature, a.Cfg.ImageModel)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, v := range []struct{ k, v string }{{"model", model}, {"prompt", prompt}, {"size", size}, {"quality", "high"}} {
		if err := mw.WriteField(v.k, v.v); err != nil {
			return nil, fmt.Errorf("准备图像编辑请求失败")
		}
	}
	for i, im := range refs {
		// 图像服务按每段的 Content-Type 判断格式，CreateFormFile 给的是 octet-stream 会被拒（HTTP 400）
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename="reference-%d.png"`, i+1))
		h.Set("Content-Type", "image/png")
		part, err := mw.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if err = png.Encode(part, im); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	finish, err := a.beginUsage(ctx, "image", model)
	if err != nil {
		return nil, err
	}
	return a.imageRequest(ctx, "/images/edits", mw.FormDataContentType(), body.Bytes(), finish)
}

func validImageSize(size string) bool {
	return size == "1024x1024" || size == "1024x1536" || size == "1536x1024"
}

// GenerateImage uses the same provider, quota reservation and response checks as edits.
func (a *Core) GenerateImage(ctx context.Context, prompt, size string) ([]byte, error) {
	if !validImageSize(size) {
		return nil, fmt.Errorf("图片尺寸不正确")
	}
	if a.Cfg.AIKey == "" {
		return nil, fmt.Errorf("未配置 AI_API_KEY，无法生成照片")
	}
	model := a.Model(Actor(ctx).Feature, a.Cfg.ImageModel)
	finish, err := a.beginUsage(ctx, "image", model)
	if err != nil {
		return nil, err
	}
	body := []byte(JS(Object{"model": model, "prompt": prompt, "size": size, "quality": "high", "n": 1}))
	return a.imageRequest(ctx, "/images/generations", "application/json", body, finish)
}

func (a *Core) imageRequest(ctx context.Context, endpoint, contentType string, body []byte, finish func(bool) error) ([]byte, error) {
	charged := false
	defer func() {
		if !charged {
			_ = finish(false)
		}
	}()
	client := *a.Client
	client.Timeout = a.Cfg.ImageTimeout
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		requestCtx, cancel := context.WithTimeout(ctx, a.Cfg.ImageTimeout)
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
			if _, err = png.Decode(bytes.NewReader(decoded)); err != nil {
				return nil, fmt.Errorf("图像服务返回的 PNG 已损坏")
			}
			return decoded, nil
		}()
		cancel()
		if err == nil {
			charged = true
			if e := finish(true); e != nil {
				return nil, e
			}
			return data, nil
		}
		last = err
	}
	return nil, last
}
