package core

import (
	"encoding/json"
	"fmt"
	"math"
)

func (a *Core) Setting(key string) Object {
	var s string
	if a.DB.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&s) != nil {
		return Object{}
	}
	return JSONObj(s)
}

// aiFeatures 是 ai_models 里可以单独指定模型的功能名，对应 WithFeature 的取值。
var aiFeatures = map[string]bool{"classify": true, "copy": true, "enhance": true, "fill": true, "tag": true, "upscale": true, "process": true, "copy_rewrite": true, "copy_check": true, "assistant": true, "model_create": true, "tryon_analyze": true, "tryon": true}

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
	default:
		return fmt.Errorf("不支持的系统设置：%s", key)
	}
	return nil
}
