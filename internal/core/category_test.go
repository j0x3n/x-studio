package core

import (
	"strings"
	"testing"
)

func TestFillPrompt(t *testing.T) {
	text := "{{category_name}}\n{{category_rules}}\n{{codes}}\n{{banned_words}}\n{{today}}"
	c := &Category{Name: "水晶", Rules: "水晶规则", Codes: []CategoryCode{{Code: "X1", Name: "名字", Desc: "晶体纹路", Use: "细节"}, {Code: "X2", Name: "尺寸", Suffix: "尺寸"}}, Banned: []string{"转运", "治病"}}
	result := FillPrompt(text, c)
	for _, want := range []string{"水晶规则", "| X1 | 晶体纹路 | 细节 |", "| X2_<尺寸> | 尺寸 |", "转运、治病", "{{today}}"} {
		if !strings.Contains(result, want) {
			t.Fatalf("missing %q: %s", want, result)
		}
	}
	if got := FillPrompt(text, nil); got != "\n\n\n\n{{today}}" {
		t.Fatal(got)
	}
	if strings.Contains(result, "{{codes}}") {
		t.Fatal(result)
	}
}
