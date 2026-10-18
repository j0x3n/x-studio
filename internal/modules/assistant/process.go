package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/media"
	"xstudio/internal/modules/assets"
)

func mergeParams(base, override object) object {
	out := object{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		if old, ok := out[k].(map[string]any); ok {
			if next, ok := v.(map[string]any); ok {
				out[k] = mergeParams(old, next)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func (a *Module) processAction(u core.User, card Action) (Action, bool) {
	var probe struct {
		IDs      []int64         `json:"asset_ids"`
		Ops      json.RawMessage `json:"ops"`
		PresetID *int64          `json:"preset_id"`
	}
	if parseArgs(json.RawMessage(core.JS(card.Params)), &probe) != nil || !validIDs(probe.IDs, 20) {
		return card, false
	}
	// Match assets.processRequest: resolve a visible preset, recursively overlay
	// explicit ops, then validate and check the permission for the effective ops.
	merged := card.Params
	if probe.PresetID != nil {
		if *probe.PresetID < 1 {
			return card, false
		}
		var params string
		if a.DB.QueryRow("SELECT params FROM asset_presets WHERE id=? AND (owner_id=? OR (shared=1 AND ?<>'shares'))", *probe.PresetID, u.ID, u.Role.Scope).Scan(&params) != nil {
			return card, false
		}
		merged = mergeParams(core.JSONObj(params), card.Params)
	}
	if ops, ok := merged["ops"].(map[string]any); !ok || ops == nil {
		return card, false
	}
	var in assets.ProcessRequest
	if parseArgs(json.RawMessage(core.JS(merged)), &in) != nil || validateOps(&in.Ops) != nil {
		return card, false
	}
	perm := core.AssetUpload
	if in.Ops.AI != nil {
		perm = core.AssetProcess
	} else if in.Ops.Upscale != nil {
		perm = core.AssetUpscale
	}
	if !u.Has(core.AssetUpload) || !u.Has(perm) {
		return card, false
	}
	var product int64
	for _, id := range in.AssetIDs {
		pid, visible := a.assetProduct(u, id)
		if !visible || (product > 0 && product != pid) || (u.Role.Scope == "shares" && !a.CanUpload(u, pid)) {
			return card, false
		}
		product = pid
	}
	if in.Ops.Color != nil && in.Ops.Color.MatchTo > 0 {
		ref := in.Ops.Color.MatchTo
		pid, visible := a.assetProduct(u, ref)
		if !visible || pid != product {
			return card, false
		}
		for _, id := range in.AssetIDs {
			if id == ref {
				return card, false
			}
		}
	}
	card.Params["ops"] = in.Ops
	calls := 0
	if in.Ops.AI != nil {
		calls++
	}
	if in.Ops.Upscale != nil {
		calls++
	}
	if calls > 0 {
		card.CostHint = fmt.Sprintf("约用 %d 张图额度", len(in.AssetIDs)*calls)
	}
	return card, true
}

// Validation and permission priority intentionally follow assets/params.go.
func validateOps(o *assets.Ops) error {
	bad := fmt.Errorf("处理参数不正确")
	if o.Geometry != nil {
		if err := media.ValidatePhoto(core.Photo{Rotate: o.Geometry.Rotate, Crop: o.Geometry.Crop}); err != nil {
			return err
		}
	}
	switch o.Aspect {
	case "", "free", "1:1", "3:4", "4:3", "16:9":
	default:
		return bad
	}
	if o.Color != nil && o.Color.MatchTo < 0 {
		return bad
	}
	if o.Cutout != nil && o.Cutout.Background != "white" && o.Cutout.Background != "transparent" {
		return bad
	}
	if o.Upscale != nil && o.Upscale.TargetLongSide != 2048 && o.Upscale.TargetLongSide != 3000 && o.Upscale.TargetLongSide != 4096 {
		return bad
	}
	if o.AI != nil {
		quick := []string{}
		seen := map[string]bool{}
		for _, key := range o.AI.Quick {
			key = strings.TrimSpace(key)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			quick = append(quick, key)
		}
		o.AI.Quick = quick
		o.AI.Extra = strings.TrimSpace(o.AI.Extra)
		if len(quick) == 0 && o.AI.Extra == "" || len([]rune(o.AI.Extra)) > 4000 {
			return bad
		}
		for _, key := range quick {
			switch key {
			case "dust", "glare", "bright", "wb", "bg_white", "bg_linen", "bg_velvet", "shadow":
			default:
				return bad
			}
		}
	}
	if o.Resize != nil && (o.Resize.LongSide < 1 || o.Resize.LongSide > 10000) {
		return bad
	}
	if o.Format == "" {
		o.Format = "jpg"
	}
	if o.Format != "jpg" && o.Format != "png" {
		return bad
	}
	return nil
}
