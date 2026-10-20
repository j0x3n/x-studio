package core

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Listen, DataDir, TemplateDir, PromptDir  string
	AdminUser, AdminPassword, SessionSecret  string
	AIBaseURL, AIKey, VisionModel, TextModel string
	ReasoningEffort, ImageModel              string
	ImageTimeout                             time.Duration
	AutoUpscale, AutoFill                    bool
	UpscaleMinSide                           int
	RembgURL, ChromePath                     string
	Workers                                  int
	SecureCookie                             bool
}

func EnvConfig() (Config, error) {
	get := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	c := Config{Listen: get("LISTEN", ":8080"), DataDir: get("DATA_DIR", "./data"), TemplateDir: get("TEMPLATE_DIR", "./templates"), PromptDir: get("PROMPT_DIR", "./prompts"), AdminUser: os.Getenv("ADMIN_USER"), AdminPassword: os.Getenv("ADMIN_PASSWORD"), SessionSecret: os.Getenv("SESSION_SECRET"), AIBaseURL: get("AI_BASE_URL", "https://subapi.xcc.im/v1"), AIKey: os.Getenv("AI_API_KEY"), VisionModel: get("AI_MODEL_VISION", "gpt-6-sol"), TextModel: get("AI_MODEL_TEXT", "gpt-6-sol"), RembgURL: get("REMBG_URL", "http://rembg:7000"), ChromePath: os.Getenv("CHROME_PATH"), SecureCookie: os.Getenv("COOKIE_SECURE") == "1", Workers: 2, ReasoningEffort: "medium", ImageModel: get("IMAGE_MODEL", "gpt-image-2.5-sunburst"), ImageTimeout: 240 * time.Second, AutoUpscale: true, AutoFill: true, UpscaleMinSide: 900}
	if v, exists := os.LookupEnv("AI_REASONING_EFFORT"); exists {
		c.ReasoningEffort = v
	}
	for key, dst := range map[string]*bool{"AUTO_UPSCALE": &c.AutoUpscale, "AUTO_FILL": &c.AutoFill} {
		if v, exists := os.LookupEnv(key); exists {
			switch v {
			case "1":
				*dst = true
			case "0":
				*dst = false
			default:
				return c, fmt.Errorf("%s 必须是 0 或 1", key)
			}
		}
	}
	if v, exists := os.LookupEnv("UPSCALE_MIN_SIDE"); exists {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 10000 {
			return c, fmt.Errorf("UPSCALE_MIN_SIDE 必须是 1–10000 的整数")
		}
		c.UpscaleMinSide = n
	}
	if v, exists := os.LookupEnv("IMAGE_TIMEOUT"); exists {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 3600 {
			return c, fmt.Errorf("IMAGE_TIMEOUT 必须是 1–3600 的整数秒")
		}
		c.ImageTimeout = time.Duration(n) * time.Second
	}
	if v := os.Getenv("WORKERS"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 32 {
			return c, fmt.Errorf("WORKERS 必须是 1–32 的整数")
		}
		c.Workers = n
	}
	return c, nil
}

func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
