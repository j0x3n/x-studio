package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}
func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
func (a *Core) SafeAIError(s string) string {
	if a.Cfg.AIKey != "" {
		s = strings.ReplaceAll(s, a.Cfg.AIKey, "[密钥已隐藏]")
	}
	return s
}
func (a *Core) AIJSON(ctx context.Context, model, prompt string, content any, out any) error {
	if a.Cfg.AIKey == "" {
		return fmt.Errorf("未配置 AI_API_KEY，无法执行 AI 步骤")
	}
	model = a.Model(Actor(ctx).Feature, model)
	if model == "" {
		return fmt.Errorf("未配置 AI 模型，请设置 AI_MODEL_VISION / AI_MODEL_TEXT")
	}
	finish, err := a.beginUsage(ctx, "text", model)
	if err != nil {
		return err
	}
	charged := false
	defer func() {
		if !charged {
			_ = finish(false)
		}
	}()
	request := Object{"model": model, "messages": []Object{{"role": "system", "content": prompt + "\n只返回合法 JSON，不要解释。"}, {"role": "user", "content": content}}, "response_format": Object{"type": "json_object"}}
	if a.Cfg.ReasoningEffort != "" {
		request["reasoning_effort"] = a.Cfg.ReasoningEffort
	}
	body := JS(request)
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			return fmt.Errorf("AI 任务已取消")
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		raw, e := func() (string, error) {
			req, e := http.NewRequestWithContext(attemptCtx, "POST", strings.TrimRight(a.Cfg.AIBaseURL, "/")+"/chat/completions", strings.NewReader(body))
			if e != nil {
				return "", fmt.Errorf("AI 接口地址不正确")
			}
			req.Header.Set("Authorization", "Bearer "+a.Cfg.AIKey)
			req.Header.Set("Content-Type", "application/json")
			res, e := a.Client.Do(req)
			if e != nil {
				return "", fmt.Errorf("AI 请求失败或超时")
			}
			defer res.Body.Close()
			if res.StatusCode < 200 || res.StatusCode >= 300 {
				return "", fmt.Errorf("AI 服务返回 HTTP %d", res.StatusCode)
			}
			b, e := io.ReadAll(io.LimitReader(res.Body, 8<<20))
			if e != nil {
				return "", fmt.Errorf("读取 AI 回复失败")
			}
			var response struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			if json.Unmarshal(b, &response) != nil || len(response.Choices) == 0 {
				return "", fmt.Errorf("AI 回复没有有效 choices")
			}
			return response.Choices[0].Message.Content, nil
		}()
		cancel()
		if e == nil {
			// Decode into a fresh intermediate value before writing out. Failed
			// attempts must not leave partially decoded fields in the next attempt.
			var value any
			e = json.Unmarshal([]byte(stripFence(raw)), &value)
			if e == nil {
				b, _ := json.Marshal(value)
				e = json.Unmarshal(b, out)
			}
			if e == nil {
				charged = true
				return finish(true)
			}
			e = fmt.Errorf("AI JSON 解析失败，原始回复（前 500 字）：%s", firstRunes(a.SafeAIError(raw), 500))
		}
		last = e
	}
	return last
}
