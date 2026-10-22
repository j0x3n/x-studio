package media

import (
	"slices"
	"strings"
	"xstudio/internal/core"
)

func ViewGroup(style string) string {
	switch {
	case strings.Contains(style, "项链"), strings.Contains(style, "颈链"), strings.Contains(style, "锁骨链"):
		return "strand"
	case strings.Contains(style, "耳"):
		return "small"
	case strings.Contains(style, "手链"), strings.Contains(style, "手串"), strings.Contains(style, "手镯"):
		return "strand"
	default:
		return "small"
	}
}

func ProductViews(p core.Product) []core.CategoryView {
	style, _ := p.Info["style"].(string)
	group := ViewGroup(style)
	out := []core.CategoryView{}
	if p.Category != nil {
		for _, view := range p.Category.Views {
			if slices.Contains(view.Groups, group) {
				out = append(out, view)
			}
		}
	}
	return out
}
