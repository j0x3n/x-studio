package assets

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"time"
	"xstudio/internal/core"
	"xstudio/internal/imaging"
)

// processImage shares transformations and provenance rules with both publishers.
// output owns file access, allowing the workbench to use an os.Root throughout.
func (a *Module) processImage(ctx context.Context, raw *image.NRGBA, ex imaging.Exif, inputSource, code string, in ProcessRequest, prompts processPrompts, ref *image.NRGBA, output func(image.Image, string) (string, error)) (*stagedProcess, error) {
	started := time.Now()
	var err error
	original := imaging.Geometry(raw, ex.Orientation, 0, nil)
	im := original
	o := in.Ops
	op := Object{"ops": o, "preset_id": in.PresetID, "set_current": in.SetCurrent, "prompts": prompts.names, "models": Object{}}
	warnings := []string{}
	models := op["models"].(Object)
	source := inputSource
	if o.Geometry != nil {
		im = imaging.Geometry(raw, ex.Orientation, o.Geometry.Rotate, o.Geometry.Crop)
	}
	if ratio, ok := map[string]float64{"1:1": 1, "3:4": .75, "4:3": 4.0 / 3, "16:9": 16.0 / 9}[o.Aspect]; ok {
		im = imaging.Aspect(im, ratio)
	}
	if o.Color != nil {
		var params Object
		im, params = imaging.Color(im, code, o.Color.AutoWB, ref)
		op["color"] = params
	}
	if o.Cutout != nil {
		mask, mode := imaging.Mask(ctx, im, a.Cfg.RembgURL, a.Client)
		op["cutout"] = mode
		im = imaging.ReplaceBackground(im, mask, o.Cutout.Background == "transparent")
		if mode == "fallback" {
			warnings = append(warnings, "抠图服务不可用，已使用边缘连通算法")
		}
	}
	edit := func(feature, prompt string) error {
		models[feature] = a.Model(feature, a.Cfg.ImageModel)
		data, e := a.EditImage(core.WithFeature(ctx, feature), prompt, []image.Image{im})
		if e != nil {
			return e
		}
		if _, _, e = imageConfig(data); e != nil {
			return fmt.Errorf("AI 图片格式或像素数量不符合要求")
		}
		decoded, _, e := image.Decode(bytes.NewReader(data))
		if e != nil {
			return fmt.Errorf("AI 图片无法解码")
		}
		im = imaging.Geometry(decoded, 1, 0, nil)
		return nil
	}
	if o.Upscale != nil {
		if err = edit("upscale", prompts.upscale); err != nil {
			return nil, err
		}
		im, err = imaging.Resample(im, o.Upscale.TargetLongSide)
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, "高清化采用 AI 重绘后重采样，并非真正的无损放大")
		source = composeSource(source, "ai_enhanced")
	}
	if o.AI != nil {
		if err = edit("process", prompts.ai); err != nil {
			return nil, err
		}
		kind := "ai_enhanced"
		if prompts.edited {
			kind = "ai_edited"
		}
		source = composeSource(source, kind)
	}
	if o.Upscale != nil || o.AI != nil {
		diff := imaging.Diff(original, im)
		op["diff"] = diff
		if diff["phash_distance"].(int) > 16 {
			warnings = append(warnings, "商品外观可能被改动，请对照原图")
		}
	}
	if o.Resize != nil {
		im, err = imaging.Resample(im, o.Resize.LongSide)
		if err != nil {
			return nil, err
		}
	}
	if o.Format == "jpg" {
		im = imaging.Flatten(im)
		if o.Cutout != nil && o.Cutout.Background == "transparent" {
			warnings = append(warnings, "JPG 不支持透明，已合成白底")
		}
	}
	name, err := output(im, o.Format)
	if err != nil {
		return nil, err
	}
	op["warnings"] = warnings
	op["elapsed_ms"] = time.Since(started).Milliseconds()
	return &stagedProcess{width: im.Rect.Dx(), height: im.Rect.Dy(), phash: fmt.Sprintf("%016x", imaging.Hash(im)), file: name, source: source, op: op}, nil
}
