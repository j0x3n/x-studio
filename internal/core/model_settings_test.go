package core

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestImageAlternateSettingValidation(t *testing.T) {
	for _, alternate := range []any{"", "gpt-image-2.5-flare", 123, nil} {
		err := ValidateSetting("ai_models", json.RawMessage(JS(Object{"image_alt": alternate})))
		if _, valid := alternate.(string); valid {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "生图备用模型") {
			t.Fatal(err)
		}
	}
}

func TestModelSettingsInheritance(t *testing.T) {
	a := imageTestCore(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected AI call") })
	for _, tc := range []struct {
		settings      Object
		feature, want string
	}{
		{Object{"process": "new-image", "classify": "new-vision"}, "tryon", "new-image"},
		{Object{"process": "new-image"}, "model_create", "new-image"},
		{Object{"classify": "new-vision"}, "tryon_analyze", "new-vision"},
		{Object{"process": "new-image", "tryon": " explicit "}, "tryon", "explicit"},
		{Object{"process": "new-image", "tryon": " "}, "tryon", "new-image"},
		{Object{}, "tryon", "fallback"},
	} {
		if _, err := a.DB.Exec("INSERT OR REPLACE INTO settings(key,value,updated_at) VALUES('ai_models',?,?)", JS(tc.settings), Now()); err != nil {
			t.Fatal(err)
		}
		if got := a.Model(tc.feature, "fallback"); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.feature, got, tc.want)
		}
	}
}
