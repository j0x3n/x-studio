package products

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
)

var categoryKey = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)
var categoryCode = regexp.MustCompile(`^[A-Z][0-9]{1,2}$`)

func (a *Module) validateCategory(c *core.Category) error {
	if !categoryKey.MatchString(c.Key) {
		return fmt.Errorf("品类 key 格式不正确")
	}
	if strings.TrimSpace(c.Name) == "" || utf8.RuneCountInString(c.Name) > 20 {
		return fmt.Errorf("品类名称须为 1–20 字")
	}
	if utf8.RuneCountInString(c.Rules) > 20000 {
		return fmt.Errorf("品类规则不能超过 20000 字")
	}
	keys := map[string]bool{}
	for _, v := range c.Attrs {
		if !categoryKey.MatchString(v.Key) || keys[v.Key] || strings.TrimSpace(v.Label) == "" {
			return fmt.Errorf("属性 key 或 label 不正确或重复")
		}
		keys[v.Key] = true
		switch v.Type {
		case "text", "textarea", "number", "bool":
		case "select", "multi":
			if len(v.Options) == 0 {
				return fmt.Errorf("%s 必须有 options", v.Label)
			}
		default:
			return fmt.Errorf("%s 的属性类型不正确", v.Label)
		}
	}
	codes := map[string]bool{}
	for _, v := range c.Codes {
		if !categoryCode.MatchString(v.Code) || codes[v.Code] {
			return fmt.Errorf("拍摄编号格式不正确或重复")
		}
		codes[v.Code] = true
	}
	var slots map[string]json.RawMessage
	if json.Unmarshal(c.Slots, &slots) != nil || slots == nil {
		return fmt.Errorf("slots 必须是对象")
	}
	for key, raw := range slots {
		if key == "说明" {
			var note string
			if json.Unmarshal(raw, &note) != nil {
				return fmt.Errorf("槽位说明必须是文本")
			}
			continue
		}
		if key != "主图" && key != "详情页" {
			return fmt.Errorf("不支持的槽位分组：%s", key)
		}
		var entries []struct {
			Position   any      `json:"位"`
			Screen     any      `json:"屏"`
			Name       string   `json:"名"`
			Candidates []string `json:"候选"`
			Required   bool     `json:"必需"`
			Minimum    int      `json:"最少"`
			Note       string   `json:"备注"`
		}
		if json.Unmarshal(raw, &entries) != nil || entries == nil {
			return fmt.Errorf("%s 槽位必须是数组", key)
		}
		for _, v := range entries {
			if v.Name == "" || v.Candidates == nil || v.Minimum < 0 {
				return fmt.Errorf("%s 槽位格式不正确", key)
			}
			for _, pos := range []any{v.Position, v.Screen} {
				switch n := pos.(type) {
				case nil:
				case string:
					if n == "" {
						return fmt.Errorf("槽位位置不能为空")
					}
				case float64:
					if n < 1 || math.Trunc(n) != n {
						return fmt.Errorf("槽位位置必须是正整数或文本")
					}
				default:
					return fmt.Errorf("槽位位置必须是正整数或文本")
				}
			}
			for _, candidate := range v.Candidates {
				if !codes[candidate] {
					return fmt.Errorf("槽位 %s 引用了不存在的编号 %s", v.Name, candidate)
				}
			}
		}
	}
	if c.DefaultTemplate != "" && !a.templateExists(c.DefaultTemplate) {
		return fmt.Errorf("默认模板不存在")
	}
	return nil
}
func (a *Module) templateExists(key string) bool {
	list, _ := a.DetailTemplates()
	for _, t := range list {
		if t.Key == key {
			return true
		}
	}
	return false
}
func validateCategoryInfo(info core.Object, c *core.Category) error {
	for _, attr := range c.Attrs {
		v, exists := info[attr.Key]
		if !exists || v == nil || v == "" {
			continue
		}
		valid := false
		switch attr.Type {
		case "text", "textarea", "select":
			if s, ok := v.(string); ok {
				limit := 200
				if attr.Type == "textarea" {
					limit = 2000
				}
				valid = utf8.RuneCountInString(s) <= limit
				if attr.Type == "select" && !attr.Free && !slices.Contains(attr.Options, s) {
					valid = false
				}
			}
		case "number":
			_, valid = v.(float64)
		case "bool":
			_, valid = v.(bool)
		case "multi":
			if values, ok := v.([]any); ok {
				valid = true
				for _, item := range values {
					s, ok := item.(string)
					if !ok || utf8.RuneCountInString(s) > 200 || (!attr.Free && !slices.Contains(attr.Options, s)) {
						valid = false
					}
				}
			}
		}
		if !valid {
			return fmt.Errorf("%s 的类型、长度或选项不正确", attr.Label)
		}
	}
	return nil
}
