package core

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

func (a *Core) ValidatePlanFields(ctx context.Context, u User, fields map[string]json.RawMessage) error {
	for key, raw := range fields {
		switch key {
		case "template_set":
			var templates map[string]string
			if json.Unmarshal(raw, &templates) != nil || templates == nil {
				return fmt.Errorf("template_set 必须是位置到模板 key 的对象")
			}
			for slot, template := range templates {
				var count int
				if err := a.DB.QueryRowContext(ctx, "SELECT count(*) FROM creative_templates WHERE key=? AND slot=? AND enabled=1", template, slot).Scan(&count); err != nil {
					return fmt.Errorf("读取模板失败：%w", err)
				}
				if count != 1 {
					return fmt.Errorf("模板不存在、已停用或位置不匹配：%s", template)
				}
			}
		case "models":
			var models map[string][]int64
			if json.Unmarshal(raw, &models) != nil || models == nil {
				return fmt.Errorf("models 必须是位置到模特 ID 数组的对象")
			}
			for slot, ids := range models {
				if !slices.Contains([]string{"female_main", "male", "hand"}, slot) || ids == nil {
					return fmt.Errorf("模特位置或列表不正确")
				}
				seen := map[int64]bool{}
				for _, id := range ids {
					if id < 1 || seen[id] || !a.VisibleModel(ctx, u, id, true) {
						return fmt.Errorf("模特不存在、不可见、已归档或重复")
					}
					seen[id] = true
				}
			}
		case "scene_id":
			if string(raw) == "null" {
				continue
			}
			var id int64
			if json.Unmarshal(raw, &id) != nil || id < 1 {
				return fmt.Errorf("scene_id 必须是正整数或 null")
			}
		case "detail_layout":
			var key string
			if string(raw) == "null" || json.Unmarshal(raw, &key) != nil {
				return fmt.Errorf("detail_layout 必须是文本")
			}
			if key == "" {
				continue
			}
			templates, _ := a.DetailTemplates()
			found := false
			for _, template := range templates {
				if template.Key == key {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("详情版式不存在")
			}
		default:
			return fmt.Errorf("不支持的方案覆盖字段：%s", key)
		}
	}
	return nil
}

func (a *Core) VisibleModel(ctx context.Context, u User, id int64, active bool) bool {
	if u.Disabled || u.Role.Scope == "shares" || !u.Has(ModelView) {
		return false
	}
	var shop *int64
	var archived bool
	if a.DB.QueryRowContext(ctx, "SELECT shop_id,archived FROM shop_models WHERE id=?", id).Scan(&shop, &archived) != nil || (active && archived) {
		return false
	}
	if shop == nil {
		return true
	}
	if !a.CanShop(u, *shop) {
		return false
	}
	if active {
		if a.DB.QueryRowContext(ctx, "SELECT archived FROM shops WHERE id=?", *shop).Scan(&archived) != nil || archived {
			return false
		}
	}
	return true
}
