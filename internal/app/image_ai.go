package app

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
	"os"
	"path/filepath"
	"strings"
)

func imageSize(im image.Image) string {
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

// editImage sends only PNG references, never URLs or generated-image references
// supplied by a model. The image client has its own timeout (not the text 120s).
func (a *App) editImage(ctx context.Context, prompt string, refs []image.Image) ([]byte, error) {
	if a.cfg.AIKey == "" {
		return nil, fmt.Errorf("未配置 AI_API_KEY，无法生成或高清化照片")
	}
	if len(refs) == 0 || len(refs) > 3 {
		return nil, fmt.Errorf("图像编辑需要 1–3 张参考图")
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, v := range []struct{ k, v string }{{"model", a.cfg.ImageModel}, {"prompt", prompt}, {"size", imageSize(refs[0])}, {"quality", "high"}} {
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
	client := *a.client
	client.Timeout = a.cfg.ImageTimeout
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		requestCtx, cancel := context.WithTimeout(ctx, a.cfg.ImageTimeout)
		data, err := func() ([]byte, error) {
			req, err := http.NewRequestWithContext(requestCtx, "POST", strings.TrimRight(a.cfg.AIBaseURL, "/")+"/images/edits", bytes.NewReader(body.Bytes()))
			if err != nil {
				return nil, fmt.Errorf("图像接口地址不正确")
			}
			req.Header.Set("Content-Type", mw.FormDataContentType())
			req.Header.Set("Authorization", "Bearer "+a.cfg.AIKey)
			res, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("图像请求失败或超时")
			}
			defer res.Body.Close()
			if res.StatusCode < 200 || res.StatusCode >= 300 {
				msg, _ := io.ReadAll(io.LimitReader(res.Body, 600))
				return nil, fmt.Errorf("图像服务返回 HTTP %d：%s", res.StatusCode, strings.TrimSpace(string(msg)))
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
			if json.Unmarshal(b, &response) != nil || len(response.Data) == 0 || response.Data[0].Base64 == "" {
				return nil, fmt.Errorf("图像服务没有返回 data[0].b64_json")
			}
			decoded, err := base64.StdEncoding.DecodeString(response.Data[0].Base64)
			if err != nil {
				return nil, fmt.Errorf("图像回复的 base64 不正确")
			}
			if _, format, err := imageConfig(decoded); err != nil || format != "png" {
				return nil, fmt.Errorf("图像回复不是有效的 PNG 或像素超限")
			}
			if _, err = png.Decode(bytes.NewReader(decoded)); err != nil {
				return nil, fmt.Errorf("图像服务返回的 PNG 已损坏")
			}
			return decoded, nil
		}()
		cancel()
		if err == nil {
			return data, nil
		}
		last = err
	}
	return nil, last
}

// Write beside the destination first so interrupted requests never leave a
// partially written image. Actual returned dimensions are preserved.
func writeAIFile(filename string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".ai-*.png")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, filename)
}

func enhancementSections(doc string) map[string]string {
	out := map[string]string{}
	key := ""
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "## ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			key = ""
			for _, candidate := range []string{"通用要求", "高清化", "A1", "E1", "E2", "E3", "D4"} {
				if strings.HasPrefix(title, candidate) {
					key = candidate
					break
				}
			}
			continue
		}
		if key != "" {
			out[key] += line + "\n"
		}
	}
	return out
}
func enhancementPrompt(parts map[string]string, kind string, p Product) (string, error) {
	common, section := strings.TrimSpace(parts["通用要求"]), strings.TrimSpace(parts[kind])
	if common == "" || section == "" {
		return "", fmt.Errorf("补图.md 缺少通用要求或 %s 段落", kind)
	}
	fields := []string{"款名=" + p.Name}
	for _, key := range []string{"category", "size", "colors", "highlights"} {
		if v, ok := p.Info[key]; ok {
			fields = append(fields, key+"="+strings.ReplaceAll(fmt.Sprint(v), "\n", " "))
		}
	}
	size, _ := p.Info["size"].(string)
	if strings.TrimSpace(size) == "" {
		size = "【待定】"
	}
	replacer := strings.NewReplacer("{info}", strings.Join(fields, "；"), "{size}", size)
	return replacer.Replace(common + "\n\n" + section), nil
}
