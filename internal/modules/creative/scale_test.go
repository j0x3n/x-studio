package creative

import (
	"strings"
	"testing"

	"xstudio/internal/core"
)

func TestBeadScale(t *testing.T) {
	category := &core.Category{Attrs: []core.CategoryAttr{{Key: "style", Label: "款式"}, {Key: "size", Label: "珠径", Unit: "mm"}}}
	product := core.Product{Category: category, Info: core.Object{"style": "项链", "size": "3.3–3.8", "auto_fill": false, "highlights": "40 / 43 cm 两种长度"}, Content: core.Object{"params": []any{[]any{"链长", "40 cm / 43 cm"}}}}
	en, zh := beadScale(product)
	for _, want := range []string{"3.5 mm", "40 cm strand", "about 113 beads", "about 56 beads", "never fewer and larger"} {
		if !strings.Contains(en, want) {
			t.Fatalf("英文比例缺 %q：%s", want, en)
		}
	}
	if !strings.Contains(zh, "满串约 113 颗") || !strings.Contains(zh, "约 56 颗") {
		t.Fatalf("中文比例不对：%s", zh)
	}
	if facts := productFacts(product); facts != "款式：项链；珠径：3.3–3.8 mm；highlights：40 / 43 cm 两种长度" {
		t.Fatalf("资料：%s", facts)
	}
	product.Content = core.Object{}
	if strandLength(product) != 40 {
		t.Fatalf("亮点里的链长应取 40")
	}
	product.Info = core.Object{"style": "耳钉", "size": "10–11"}
	if en, _ = beadScale(product); strings.Contains(en, "strand") || !strings.Contains(en, "earlobe") {
		t.Fatalf("耳钉比例：%s", en)
	}
	product.Info = core.Object{"style": "吊坠", "size": "12×8"}
	if en, zh = beadScale(product); en != "" || zh != "" {
		t.Fatalf("长×宽不算珠径")
	}
}
