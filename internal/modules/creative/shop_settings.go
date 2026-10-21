package creative

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

type ShopSettings struct {
	Models      map[string]int64  `json:"models"`
	TemplateSet map[string]string `json:"template_set"`
	MainRatios  []string          `json:"main_ratios"`
	DetailWidth int               `json:"detail_width"`
	AboutText   string            `json:"about_text"`
}

func (module *Module) shopSettings(ctx context.Context, shopID int64) (ShopSettings, error) {
	obj, err := module.one(ctx, "SELECT models,template_set,main_ratios,detail_width,about_text FROM creative_shop_settings WHERE shop_id=?", shopID)
	if errors.Is(err, sql.ErrNoRows) {
		return ShopSettings{Models: map[string]int64{}, TemplateSet: map[string]string{}, MainRatios: []string{}}, nil
	}
	if err != nil {
		return ShopSettings{}, err
	}
	return typed[ShopSettings](obj)
}
func validateShopSettings(settings ShopSettings) error {
	seen := map[string]bool{}
	for _, ratio := range settings.MainRatios {
		if !slices.Contains([]string{"3x4", "1x1"}, ratio) || seen[ratio] {
			return problem(400, "主图比例只能选 3x4、1x1，不能重复")
		}
		seen[ratio] = true
	}
	if settings.DetailWidth != 0 && (settings.DetailWidth < 480 || settings.DetailWidth > 1500) {
		return problem(400, "详情图宽度要填 480–1500 的整数，填 0 表示按模板")
	}
	if utf8.RuneCountInString(settings.AboutText) > 300 {
		return problem(400, "关于我们不能超过 300 字")
	}
	return nil
}
func exportSizes(item Item, settings ShopSettings) []ExportSize {
	sizes := append([]ExportSize{}, item.TemplateSnapshot.Export...)
	if strings.HasPrefix(item.Slot, "main") && len(settings.MainRatios) > 0 {
		selected := []ExportSize{}
		for _, size := range sizes {
			if slices.Contains(settings.MainRatios, size.Name) {
				selected = append(selected, size)
			}
		}
		if len(selected) > 0 {
			sizes = selected
		}
	}
	if strings.HasPrefix(item.Slot, "d") && settings.DetailWidth != 0 {
		for index := range sizes {
			if sizes[index].H > 0 {
				sizes[index].H = max(1, int(float64(sizes[index].H)*float64(settings.DetailWidth)/float64(sizes[index].W)+0.5))
			}
			sizes[index].W = settings.DetailWidth
		}
	}
	return sizes
}
