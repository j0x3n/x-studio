package assets

import (
	"xstudio/internal/core"
)

func opID(op Object, key string) int64 {
	switch v := op[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		if v > 0 && v <= 1e15 && v == float64(int64(v)) {
			return int64(v)
		}
	}
	return 0
}
func modelWorkbenchItem(v WorkbenchItem) bool {
	return v.Op["type"] == "tryon" || v.Op["type"] == "model_candidate"
}

// Archive is deliberately ignored here: historical results remain accessible.
func (a *Module) modelWorkbenchVisible(u core.User, v WorkbenchItem) bool {
	if !modelWorkbenchItem(v) {
		return true
	}
	sid, mid := opID(v.Op, "shop_id"), opID(v.Op, "model_id")
	if sid <= 0 || mid <= 0 || !a.shopAllowed(u, sid, false) {
		return false
	}
	m, e := a.getModel(u, mid, false)
	if e != nil || m.ShopID != sid {
		return false
	}
	if v.Op["type"] == "model_candidate" {
		return true
	}
	var inputs []workbenchInput
	if decodeObject(v.Op["inputs"], &inputs) != nil || len(inputs) < 1 || len(inputs) > 2 {
		return false
	}
	// Recheck every product reference, including the auxiliary image.
	for _, in := range inputs {
		if (in.AssetID == nil) == (in.InputID == nil) {
			return false
		}
		if in.AssetID != nil {
			p, err := a.asset(*in.AssetID)
			if err != nil || !a.CanProduct(u, p.ProductID) {
				return false
			}
		} else {
			p, err := a.workbenchItem(u.ID, *in.InputID)
			// Inputs may expire while their generated result is still within retention.
			// Deleted uploads convey no product access, but existing results can never be inputs.
			if err == nil && p.Kind != "input" {
				return false
			}
		}
	}
	if v.AssetID != nil {
		p, err := a.asset(*v.AssetID)
		if err != nil || !a.CanProduct(u, p.ProductID) {
			return false
		}
	}
	return true
}
