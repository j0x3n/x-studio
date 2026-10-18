package core

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
)

func (a *Core) Setting(key string) Object {
	var s string
	if a.DB.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&s) != nil {
		return Object{}
	}
	return JSONObj(s)
}

// aiFeatures 是 ai_models 里可以单独指定模型的功能名，对应 WithFeature 的取值。
var aiFeatures = map[string]bool{"classify": true, "copy": true, "enhance": true, "fill": true, "tag": true, "upscale": true, "process": true, "copy_rewrite": true, "copy_check": true, "assistant": true}

func ValidateSetting(key string, v json.RawMessage) error {
	var o Object
	if json.Unmarshal(v, &o) != nil || o == nil {
		return fmt.Errorf("设置必须是 JSON 对象")
	}
	switch key {
	case "ai_models":
		for k, v := range o {
			if !aiFeatures[k] {
				return fmt.Errorf("不支持的 AI 功能")
			}
			if _, ok := v.(string); !ok {
				return fmt.Errorf("模型名称必须是文本")
			}
		}
	case "ai_prices":
		for _, v := range o {
			n, ok := v.(float64)
			if !ok || n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
				return fmt.Errorf("模型单价必须是非负数字")
			}
		}
	case "quota_default":
		for k, v := range o {
			if k != "month_images" && k != "month_text" {
				return fmt.Errorf("不支持的额度字段")
			}
			if v != nil {
				n, ok := v.(float64)
				if !ok || n < 0 || n != math.Trunc(n) || n > 1e9 {
					return fmt.Errorf("额度必须是非负整数或 null")
				}
			}
		}
	case "ai_label":
		for k, v := range o {
			switch k {
			case "text":
				s, ok := v.(string)
				if !ok || len([]rune(s)) < 1 || len([]rune(s)) > 50 {
					return fmt.Errorf("AI 标识文字需为 1–50 字")
				}
			case "position":
				if v != "bottom-right" && v != "bottom-left" && v != "top-right" && v != "top-left" {
					return fmt.Errorf("AI 标识位置不正确")
				}
			case "color":
				s, ok := v.(string)
				if !ok || !regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(s) {
					return fmt.Errorf("AI 标识颜色必须是十六进制颜色")
				}
			case "size":
				n, ok := v.(float64)
				if !ok || n < 8 || n > 72 {
					return fmt.Errorf("AI 标识字号必须在 8–72 之间")
				}
			case "opacity":
				n, ok := v.(float64)
				if !ok || n < 0 || n > 1 {
					return fmt.Errorf("AI 标识透明度必须在 0–1 之间")
				}
			default:
				return fmt.Errorf("不支持的 AI 标识样式")
			}
		}
	default:
		return fmt.Errorf("不支持的系统设置：%s", key)
	}
	return nil
}
