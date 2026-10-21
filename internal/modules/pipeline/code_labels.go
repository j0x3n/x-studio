package pipeline

import (
	"strings"
	"xstudio/internal/core"
)

func photoCodeLabels(category *core.Category, codes ...string) string {
	labels := make([]string, 0, len(codes))
	for _, code := range codes {
		label := code
		if category != nil {
			for _, entry := range category.Codes {
				if entry.Name != "" && (code == entry.Code || strings.HasPrefix(code, entry.Code+"_")) {
					label = entry.Name + "（" + code + "）"
					break
				}
			}
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, "、")
}
