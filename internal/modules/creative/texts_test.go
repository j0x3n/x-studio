package creative

import (
	"testing"
	"xstudio/internal/core"
)

func TestShortenText(t *testing.T) {
	for _, c := range []struct {
		in   string
		max  int
		want string
	}{
		{"淡水小米珠锁骨链（实拍版）", 6, "小米珠锁骨链"},
		{"淡水小米珠锁骨链（实拍版）", 8, "淡水小米珠锁骨链"},
		{"海蓝宝手串 4mm（实拍版）", 6, "海蓝宝手串"},
		{"FRESHWATER SEED PEARL COLLARBONE NECKLACE", 24, "FRESHWATER SEED PEARL"},
		{"AQUAMARINE BRACELET 4MM", 24, "AQUAMARINE BRACELET 4MM"},
		{"SUPERCALIFRAGILISTIC", 5, "SUPER"},
	} {
		if got := shortenText(c.in, c.max); got != c.want {
			t.Errorf("shortenText(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestIndexedTextCountCapsDetailScreens(t *testing.T) {
	macro := Template{Slot: "d3", Count: 3, Texts: []TextSpec{{Key: "title", From: "content.details.{i}.title"}, {Key: "text", From: "content.details.{i}.text"}}}
	one := core.Product{Content: core.Object{"details": []any{core.Object{"title": "蓝色圆珠", "text": "约 4 mm 圆珠"}}}}
	if got := planItems(0, 1, 1, []Template{macro}, Params{}, one); len(got) != 1 {
		t.Fatalf("1 条细节应只出 1 张，得到 %d", len(got))
	}
	two := core.Product{Content: core.Object{"details": []any{core.Object{"title": "a"}, core.Object{"title": "b"}}}}
	if got := planItems(0, 1, 1, []Template{macro}, Params{}, two); len(got) != 2 {
		t.Fatalf("2 条细节应出 2 张，得到 %d", len(got))
	}
	if got := planItems(0, 1, 1, []Template{macro}, Params{}, core.Product{}); len(got) != 1 {
		t.Fatalf("没有细节文案时至少保留 1 张，得到 %d", len(got))
	}
	plain := Template{Slot: "d4", Count: 2, Texts: []TextSpec{{Key: "title", From: "content.wear.lead"}}}
	if got := planItems(0, 1, 1, []Template{plain}, Params{}, one); len(got) != 2 {
		t.Fatalf("没有 {i} 的模板不受限制，得到 %d", len(got))
	}
}

func TestFillNotApplicableAcceptsBrandScreenReview(t *testing.T) {
	brand := Review{Score: 88, Checks: map[string]string{"product": "na", "style": "pass"}}
	if validReview(brand) {
		t.Fatal("补齐前应判格式不对")
	}
	fillNotApplicable(&brand, "none")
	if !validReview(brand) || brand.Checks["product"] != "pass" || brand.Checks["body"] != "na" {
		t.Fatal(brand)
	}
	model := Review{Score: 88, Checks: map[string]string{"product": "na", "style": "pass"}}
	fillNotApplicable(&model, "model")
	if validReview(model) {
		t.Fatal("上身图不该被补齐")
	}
}
