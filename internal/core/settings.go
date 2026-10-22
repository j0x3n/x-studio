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
var aiFeatures = map[string]bool{"classify": true, "copy": true, "enhance": true, "fill": true, "tag": true, "upscale": true, "process": true, "copy_rewrite": true, "copy_check": true, "assistant": true, "model_create": true, "tryon_analyze": true, "tryon": true, "document_analyze": true, "finance_explain": true, "creative": true, "creative_verify": true, "creative_text": true, "title_suggest": true, "ops_advice": true, "prop_generate": true, "prop_views": true, "scene_plate": true, "image_alt": true}

func ValidateSetting(key string, v json.RawMessage) error {
	var o Object
	if json.Unmarshal(v, &o) != nil || o == nil {
		return fmt.Errorf("设置必须是 JSON 对象")
	}
	switch key {
	case "item_code_rule":
		_, err := ParseItemCodeRule(v)
		return err
	case "ai_models":
		for k, v := range o {
			if !aiFeatures[k] {
				return fmt.Errorf("不支持的 AI 功能")
			}
			if _, ok := v.(string); !ok {
				if k == "image_alt" {
					return fmt.Errorf("生图备用模型名称必须是文本")
				}
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
	case "creative", "market", "views":
		return validateFields(key, o)
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

type settingField struct {
	kind     string // bool、int、ratio（0–1 小数）、hhmm
	min, max float64
}

// settingFields 是 creative、market 两组设置能改的字段；缺省值见 SettingDefaults。
var settingFields = map[string]map[string]settingField{
	"views": {"classify_confidence": {kind: "ratio"}, "auto_fidelity": {kind: "int", min: 0, max: 100}},
	"creative": {
		"final_glance":            {kind: "bool"},
		"max_images_per_listing":  {kind: "int", min: 1, max: 500},
		"auto_pass_score":         {kind: "int", min: 0, max: 100},
		"manual_score_min":        {kind: "int", min: 0, max: 100},
		"max_auto_redraw":         {kind: "int", min: 0, max: 5},
		"calibration_count":       {kind: "int", min: 0, max: 500},
		"calibration_agreement":   {kind: "ratio"},
		"spot_check_rate":         {kind: "ratio"},
		"size_tolerance":          {kind: "ratio"},
		"size_compare_tolerance":  {kind: "ratio"},
		"return_reason_threshold": {kind: "int", min: 1, max: 100},
	},
	"market": {
		"remind_enabled":    {kind: "bool"},
		"remind_time":       {kind: "hhmm"},
		"title_change_days": {kind: "int", min: 0, max: 90},
		"min_paid_price":    {kind: "int", min: 0, max: 100000},
		"new_listing_days":  {kind: "int", min: 0, max: 365},
	},
}

// SettingDefaults 是启动时补进 settings 表的缺省值（已有的不覆盖）。
var SettingDefaults = map[string]Object{
	"views":    {"classify_confidence": 0.8, "auto_fidelity": 85},
	"creative": {"final_glance": true, "max_images_per_listing": 40, "auto_pass_score": 90, "manual_score_min": 60, "max_auto_redraw": 2, "calibration_count": 20, "calibration_agreement": 0.95, "spot_check_rate": 0.1, "size_tolerance": 0.2, "size_compare_tolerance": 0.1, "return_reason_threshold": 2},
	"market":   {"remind_enabled": true, "remind_time": "10:00", "title_change_days": 7, "min_paid_price": 99, "new_listing_days": 14},
}

func validateFields(group string, o Object) error {
	fields := settingFields[group]
	for k, v := range o {
		f, ok := fields[k]
		if !ok {
			return fmt.Errorf("不支持的设置项：%s.%s", group, k)
		}
		switch f.kind {
		case "bool":
			if _, ok := v.(bool); !ok {
				return fmt.Errorf("%s.%s 必须是开或关", group, k)
			}
		case "int":
			n, ok := v.(float64)
			if !ok || n != math.Trunc(n) || n < f.min || n > f.max {
				return fmt.Errorf("%s.%s 必须是 %g–%g 的整数", group, k, f.min, f.max)
			}
		case "ratio":
			n, ok := v.(float64)
			if !ok || math.IsNaN(n) || n < 0 || n > 1 {
				return fmt.Errorf("%s.%s 必须是 0–1 之间的小数", group, k)
			}
		case "hhmm":
			s, ok := v.(string)
			var h, m int
			if !ok || len(s) != 5 || s[2] != ':' {
				return fmt.Errorf("%s.%s 必须是 HH:MM", group, k)
			}
			if _, e := fmt.Sscanf(s, "%02d:%02d", &h, &m); e != nil || h > 23 || m > 59 {
				return fmt.Errorf("%s.%s 必须是 HH:MM", group, k)
			}
		}
	}
	return nil
}

// SettingWithDefaults 读一组设置，缺的字段用缺省值补齐。
func (a *Core) SettingWithDefaults(key string) Object {
	out := Object{}
	for k, v := range SettingDefaults[key] {
		out[k] = v
	}
	for k, v := range a.Setting(key) {
		out[k] = v
	}
	return out
}
