package assets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/media"
)

type GeometryOp struct {
	Rotate int       `json:"rotate"`
	Crop   []float64 `json:"crop,omitempty"`
}
type ColorOp struct {
	AutoWB  bool  `json:"auto_wb"`
	MatchTo int64 `json:"match_to,omitempty"`
}
type CutoutOp struct {
	Background string `json:"background"`
}
type UpscaleOp struct {
	TargetLongSide int `json:"target_long_side"`
}
type AIOp struct {
	Quick []string `json:"quick,omitempty"`
	Extra string   `json:"extra,omitempty"`
}
type ResizeOp struct {
	LongSide int `json:"long_side"`
}
type Ops struct {
	Geometry *GeometryOp `json:"geometry,omitempty"`
	Aspect   string      `json:"aspect,omitempty"`
	Color    *ColorOp    `json:"color,omitempty"`
	Cutout   *CutoutOp   `json:"cutout,omitempty"`
	Upscale  *UpscaleOp  `json:"upscale,omitempty"`
	AI       *AIOp       `json:"ai,omitempty"`
	Resize   *ResizeOp   `json:"resize,omitempty"`
	Format   string      `json:"format,omitempty"`
}
type ProcessRequest struct {
	AssetIDs   []int64 `json:"asset_ids"`
	Ops        Ops     `json:"ops"`
	PresetID   *int64  `json:"preset_id,omitempty"`
	SetCurrent bool    `json:"set_current"`
}

func strictDecode(v any, out any) error {
	d := json.NewDecoder(bytes.NewBufferString(js(v)))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return fmt.Errorf("处理参数字段或类型不正确")
	}
	return nil
}
func validateOps(o *Ops) error {
	if o.Geometry != nil {
		if err := media.ValidatePhoto(core.Photo{Rotate: o.Geometry.Rotate, Crop: o.Geometry.Crop}); err != nil {
			return err
		}
	}
	switch o.Aspect {
	case "", "free", "1:1", "3:4", "4:3", "16:9":
	default:
		return fmt.Errorf("图片比例不正确")
	}
	if o.Color != nil && o.Color.MatchTo < 0 {
		return fmt.Errorf("校色参考图编号不正确")
	}
	if o.Cutout != nil && o.Cutout.Background != "white" && o.Cutout.Background != "transparent" {
		return fmt.Errorf("抠图背景只能是 white 或 transparent")
	}
	if o.Upscale != nil && o.Upscale.TargetLongSide != 2048 && o.Upscale.TargetLongSide != 3000 && o.Upscale.TargetLongSide != 4096 {
		return fmt.Errorf("高清化长边只能是 2048、3000 或 4096")
	}
	if o.AI != nil {
		o.AI.Quick = normalizeTags(o.AI.Quick)
		o.AI.Extra = strings.TrimSpace(o.AI.Extra)
		if len(o.AI.Quick) == 0 && o.AI.Extra == "" {
			return fmt.Errorf("AI 处理需求不能为空")
		}
		if len([]rune(o.AI.Extra)) > 4000 {
			return fmt.Errorf("额外需求最多 4000 字")
		}
		for _, key := range o.AI.Quick {
			switch key {
			case "dust", "glare", "bright", "wb", "bg_white", "bg_linen", "bg_velvet", "shadow":
			default:
				return fmt.Errorf("不支持的快捷需求：%s", key)
			}
		}
	}
	if o.Resize != nil && (o.Resize.LongSide < 1 || o.Resize.LongSide > 10000) {
		return fmt.Errorf("缩放长边必须在 1–10000 之间")
	}
	if o.Format == "" {
		o.Format = "jpg"
	}
	if o.Format != "jpg" && o.Format != "png" {
		return fmt.Errorf("输出格式只能是 jpg 或 png")
	}
	return nil
}
func processPermission(o Ops) string {
	if o.AI != nil {
		return core.AssetProcess
	}
	if o.Upscale != nil {
		return core.AssetUpscale
	}
	return core.AssetUpload
}
func mergeParams(base, override Object) Object {
	out := Object{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		if a, ok := out[k].(map[string]any); ok {
			if b, ok := v.(map[string]any); ok {
				out[k] = mergeParams(a, b)
				continue
			}
		}
		out[k] = v
	}
	return out
}
func (a *Module) resolveProcessPreset(raw Object, u core.User) (Object, error) {
	var probe struct {
		PresetID *int64 `json:"preset_id"`
	}
	if err := decodeObject(raw, &probe); err != nil {
		return nil, fmt.Errorf("预设编号不正确")
	}
	merged := raw
	if probe.PresetID != nil {
		var params string
		if a.DB.QueryRow("SELECT params FROM asset_presets WHERE id=? AND (owner_id=? OR (shared=1 AND ?<>'shares'))", *probe.PresetID, u.ID, u.Role.Scope).Scan(&params) != nil {
			return nil, fmt.Errorf("预设不存在")
		}
		merged = mergeParams(core.JSONObj(params), raw)
	}
	return merged, nil
}
func (a *Module) processRequest(raw Object, u core.User) (ProcessRequest, error) {
	merged, err := a.resolveProcessPreset(raw, u)
	if err != nil {
		return ProcessRequest{}, err
	}
	var in ProcessRequest
	if err := strictDecode(merged, &in); err != nil {
		return in, err
	}
	if err := validIDs(in.AssetIDs); err != nil {
		return in, err
	}
	if err := validateOps(&in.Ops); err != nil {
		return in, err
	}
	if in.Ops.Color != nil && in.Ops.Color.MatchTo > 0 {
		for _, id := range in.AssetIDs {
			if id == in.Ops.Color.MatchTo {
				return in, fmt.Errorf("校色参考图不能同时参与本批处理")
			}
		}
	}
	return in, nil
}
