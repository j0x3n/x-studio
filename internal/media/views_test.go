package media

import (
	"testing"
	"xstudio/internal/core"
)

func TestViewGroupAndEnhancedEvidence(t *testing.T) {
	for style, want := range map[string]string{"耳钉": "small", "戒指": "small", "吊坠": "small", "未知": "small", "项链": "strand", "手串": "strand", "手镯": "strand", "耳饰配项链": "strand"} {
		if got := ViewGroup(style); got != want {
			t.Fatalf("%s: %s, want %s", style, got, want)
		}
	}
	for _, test := range []struct {
		photo core.Photo
		ai    bool
	}{
		{core.Photo{Source: "original"}, false},
		{core.Photo{Source: "ai_enhanced"}, true},
		{core.Photo{Source: "ai_enhanced", EnhanceCheck: core.Object{"verdict": "fail"}}, true},
		{core.Photo{Source: "ai_enhanced", EnhanceCheck: core.Object{"verdict": "pass"}}, false},
		{core.Photo{Source: "ai_edited", EnhanceCheck: core.Object{"verdict": "pass"}}, true},
		{core.Photo{Source: "ai_generated", EnhanceCheck: core.Object{"verdict": "pass"}}, true},
		{core.Photo{Source: "original", AIEnhanced: true, EnhancedFile: "unverified.png"}, false},
	} {
		if got := AIMade(test.photo); got != test.ai {
			t.Fatalf("%+v: %v", test.photo, got)
		}
	}
}
