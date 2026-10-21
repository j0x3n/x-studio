package core

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestImageFallbackRebuildsRequestAndChargesActualModel(t *testing.T) {
	for _, edit := range []bool{false, true} {
		name := "generations"
		if edit {
			name = "edits"
		}
		t.Run(name, func(t *testing.T) {
			returned := imageTestPNG(t, "1254x1254")
			var calls atomic.Int64
			models := make(chan string, 2)
			instance := imageTestCore(t, func(writer http.ResponseWriter, request *http.Request) {
				var model string
				if edit {
					if err := request.ParseMultipartForm(16 << 20); err != nil {
						t.Error(err)
						writer.WriteHeader(400)
						return
					}
					defer request.MultipartForm.RemoveAll()
					model = request.FormValue("model")
					if len(request.MultipartForm.File["image"]) != 1 || len(request.MultipartForm.File["mask"]) != 1 || request.FormValue("size") != "1024x1024" || request.FormValue("prompt") != "test" {
						t.Error("multipart references, mask or fields changed")
					}
				} else {
					var body Object
					if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					model, _ = body["model"].(string)
					if body["size"] != "1024x1024" || body["prompt"] != "test" {
						t.Error(body)
					}
				}
				models <- model
				if calls.Add(1) == 1 {
					writer.WriteHeader(500)
					return
				}
				WriteJSON(writer, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(returned)}}})
			})
			if _, err := instance.DB.Exec("INSERT INTO settings(key,value,updated_at) VALUES('ai_models',?,?),('ai_prices',?,?)", JS(Object{"process": "gpt-image-2.5-sunburst", "image_alt": "configured-alt"}), Now(), JS(Object{"gpt-image-2.5-sunburst": 3, "configured-alt": 7}), Now()); err != nil {
				t.Fatal(err)
			}
			info := ImageInfo{}
			ctx := WithImageInfo(imageTestActor(), &info)
			var output []byte
			var err error
			if edit {
				bounds := image.Rect(0, 0, 20, 20)
				output, err = instance.EditImageMask(ctx, "test", []image.Image{image.NewNRGBA(bounds)}, image.NewAlpha(bounds), "1024x1024")
			} else {
				output, err = instance.GenerateImage(ctx, "test", "1024x1024")
			}
			if err != nil {
				t.Fatal(err)
			}
			cfg, format, err := image.DecodeConfig(bytes.NewReader(output))
			if err != nil || format != "png" || cfg.Width != 1024 || cfg.Height != 1024 || info != (ImageInfo{Model: "configured-alt", Returned: "1254x1254", Fallback: true}) {
				t.Fatal(cfg, info, err)
			}
			if <-models != "gpt-image-2.5-sunburst" || <-models != "configured-alt" || calls.Load() != 2 {
				t.Fatal("models were not rebuilt for fallback", calls.Load())
			}
			var model string
			var units int
			var cost float64
			if err := instance.DB.QueryRow("SELECT model,sum(units),sum(est_cost) FROM ai_usage").Scan(&model, &units, &cost); err != nil || model != "configured-alt" || units != 1 || cost != 7 {
				t.Fatal(model, units, cost, err)
			}
			if _, err := instance.GenerateImage(ctx, "test", "1024x1024"); err == nil || calls.Load() != 2 {
				t.Fatal("quota was not enforced", err, calls.Load())
			}
		})
	}
}

func TestImageInferredAndForcedModelPairs(t *testing.T) {
	for _, test := range []struct {
		name, primary, forced, alternate, wantFirst, wantSecond string
		fallback                                                bool
	}{
		{name: "sunburst", primary: "vendor/sunburst", wantFirst: "vendor/sunburst", wantSecond: "gpt-image-2.5-flare", fallback: true},
		{name: "flare", primary: "gpt-image-2.5-flare", wantFirst: "gpt-image-2.5-flare", wantSecond: "gpt-image-2.5-sunburst", fallback: true},
		{name: "forced", primary: "configured-primary", forced: " gpt-image-2.5-flare ", alternate: "configured-alt", wantFirst: "gpt-image-2.5-flare", wantSecond: "gpt-image-2.5-sunburst", fallback: true},
		{name: "no alternate", primary: "custom", wantFirst: "custom", wantSecond: "custom"},
		{name: "forced custom", primary: "gpt-image-2.5-flare", forced: "custom", alternate: "configured-alt", wantFirst: "custom", wantSecond: "custom"},
	} {
		t.Run(test.name, func(t *testing.T) {
			returned := imageTestPNG(t)
			models := make(chan string, 2)
			var calls atomic.Int64
			instance := imageTestCore(t, func(writer http.ResponseWriter, request *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				models <- body.Model
				if calls.Add(1) == 1 {
					writer.WriteHeader(500)
					return
				}
				WriteJSON(writer, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(returned)}}})
			})
			instance.Cfg.ImageModel = test.primary
			if _, err := instance.DB.Exec("INSERT INTO settings(key,value,updated_at) VALUES('ai_models',?,?)", JS(Object{"image_alt": test.alternate}), Now()); err != nil {
				t.Fatal(err)
			}
			info := ImageInfo{}
			ctx := WithImageInfo(imageTestActor(), &info)
			if test.forced != "" {
				ctx = WithImageModel(ctx, test.forced)
			}
			if _, err := instance.GenerateImage(ctx, "test", "1024x1024"); err != nil {
				t.Fatal(err)
			}
			if <-models != test.wantFirst || <-models != test.wantSecond || info.Model != test.wantSecond || info.Fallback != test.fallback {
				t.Fatal(info)
			}
		})
	}
}

func TestImageSizeNormalizationBoundaries(t *testing.T) {
	for _, test := range []struct {
		returned, requested string
		accept              bool
	}{
		{"1254x1254", "1024x1024", true},
		{"922x922", "1024x1024", true},
		{"921x921", "1024x1024", false},
		{"1014x1024", "1024x1024", true},
		{"1013x1024", "1024x1024", false},
		{"1034x1024", "1024x1024", true},
		{"1035x1024", "1024x1024", false},
		{"922x1383", "1024x1536", true},
		{"920x1380", "1024x1536", false},
		{"1536x1024", "1024x1536", false},
	} {
		t.Run(test.returned+" to "+test.requested, func(t *testing.T) {
			returned := imageTestPNG(t, test.returned)
			var calls atomic.Int64
			instance := imageTestCore(t, func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				WriteJSON(writer, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(returned)}}})
			})
			info := ImageInfo{}
			output, err := instance.GenerateImage(WithImageInfo(imageTestActor(), &info), "test", test.requested)
			var usage int
			if scanErr := instance.DB.QueryRow("SELECT coalesce(sum(units),0) FROM ai_usage").Scan(&usage); scanErr != nil {
				t.Fatal(scanErr)
			}
			if !test.accept {
				if err == nil || !strings.Contains(err.Error(), "图像服务返回的尺寸不对") || calls.Load() != 2 || usage != 0 || info.Model != "" {
					t.Fatal(err, info, calls.Load(), usage)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			im, err := png.Decode(bytes.NewReader(output))
			if err != nil || ImageSize(im) != test.requested || info.Returned != test.returned || calls.Load() != 1 || usage != 1 {
				t.Fatal(info, err, calls.Load(), usage)
			}
			var width, height int
			if test.requested == "1024x1024" {
				width, height = 1024, 1024
			} else {
				width, height = 1024, 1536
			}
			if im.Bounds().Size() != image.Pt(width, height) {
				t.Fatal(im.Bounds())
			}
		})
	}
}

func TestImageFallbackAfterInvalidResponseOrTimeout(t *testing.T) {
	for _, failure := range []string{"invalid PNG", "wrong orientation", "timeout"} {
		t.Run(failure, func(t *testing.T) {
			good := imageTestPNG(t, "1024x1536")
			bad := []byte("invalid PNG")
			if failure == "wrong orientation" {
				bad = imageTestPNG(t, "1536x1024")
			}
			var calls atomic.Int64
			instance := imageTestCore(t, func(writer http.ResponseWriter, request *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if calls.Add(1) == 1 {
					if body.Model != "gpt-image-2.5-sunburst" {
						t.Error(body.Model)
					}
					if failure == "timeout" {
						_, _ = io.Copy(io.Discard, request.Body)
						select {
						case <-request.Context().Done():
						case <-time.After(time.Second):
						}
						return
					}
					WriteJSON(writer, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(bad)}}})
					return
				}
				if body.Model != "gpt-image-2.5-flare" {
					t.Error(body.Model)
				}
				WriteJSON(writer, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(good)}}})
			})
			instance.Cfg.ImageModel = "gpt-image-2.5-sunburst"
			instance.Cfg.ImageTimeout = 100 * time.Millisecond
			info := ImageInfo{}
			if _, err := instance.GenerateImage(WithImageInfo(imageTestActor(), &info), "test", "1024x1536"); err != nil || !info.Fallback || info.Model != "gpt-image-2.5-flare" || calls.Load() != 2 {
				t.Fatal(err, info, calls.Load())
			}
		})
	}
}
