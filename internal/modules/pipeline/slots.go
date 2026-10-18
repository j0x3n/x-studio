package pipeline

import (
	"encoding/json"
	"net/http"
	"strconv"
	"xstudio/internal/core"
)

type slot struct {
	Position   any      `json:"位,omitempty"`
	Screen     any      `json:"屏,omitempty"`
	Name       string   `json:"名"`
	Candidates []string `json:"候选"`
	Required   bool     `json:"必需"`
	Minimum    int      `json:"最少,omitempty"`
	Note       string   `json:"备注,omitempty"`
}

func (a *Module) coverage(photos []Photo) Object {
	id := int64(0)
	if len(photos) > 0 {
		id = photos[0].ProductID
	}
	if id > 0 {
		if c, e := a.ProductCategory(id); e == nil {
			return coverageSlots(photos, c.Slots)
		}
	}
	if c, e := a.CategoryByKey("pearl"); e == nil {
		return coverageSlots(photos, c.Slots)
	}
	return coverageSlots(photos, a.Slots)
}
func (a *Module) productCoverage(id int64, photos []Photo) (Object, error) {
	c, e := a.ProductCategory(id)
	if e != nil {
		return nil, e
	}
	return coverageSlots(photos, c.Slots), nil
}
func coverageSlots(photos []Photo, raw []byte) Object {
	var input map[string]json.RawMessage
	_ = json.Unmarshal(raw, &input)
	out := Object{}
	need := []Object{}
	for key, cn := range map[string]string{"main": "主图", "detail": "详情页"} {
		var slots []slot
		_ = json.Unmarshal(input[cn], &slots)
		items := []Object{}
		for _, s := range slots {
			hits := []string{}
			ai := false
			for _, ph := range slotHits(s, photos) {
				hits = append(hits, ph.Code)
				if ph.AIGenerated || ph.AIEnhanced {
					ai = true
				}
			}
			min := s.Minimum
			if min == 0 {
				min = 1
			}
			ok := len(hits) >= min
			pos := s.Position
			if pos == nil {
				pos = s.Screen
			}
			hit := ""
			if len(hits) > 0 {
				hit = hits[0]
			}
			entry := Object{"position": pos, "name": s.Name, "candidates": s.Candidates, "required": s.Required, "matched": hits, "hit": hit, "covered": ok, "note": s.Note, "ai": ai}
			items = append(items, entry)
			if s.Required && !ok && !slotAllowsFill(s) {
				need = append(need, Object{"type": key, "position": pos, "name": s.Name, "candidates": s.Candidates})
			}
		}
		out[key] = items
	}
	out["need_reshoot"] = need
	return out
}
func (a *Module) requestCategory(w http.ResponseWriter, r *http.Request) *core.Category {
	q := r.URL.Query()
	if raw := q.Get("product_id"); raw != "" {
		id, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || id < 1 {
			fail(w, 400, "product_id 格式不正确")
			return nil
		}
		if !a.RequireProduct(w, r, id, false) {
			return nil
		}
		c, e := a.ProductCategory(id)
		if e != nil {
			fail(w, 404, "品类不存在")
			return nil
		}
		return c
	}
	if raw := q.Get("category_id"); raw != "" {
		id, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || id < 1 {
			fail(w, 400, "category_id 格式不正确")
			return nil
		}
		c, e := a.Category(id)
		if e != nil {
			fail(w, 404, "品类不存在")
			return nil
		}
		return c
	}
	c, e := a.CategoryByKey("pearl")
	if e != nil {
		fail(w, 404, "品类不存在")
		return nil
	}
	return c
}
func (a *Module) getSlots(w http.ResponseWriter, r *http.Request) {
	c := a.requestCategory(w, r)
	if c == nil {
		return
	}
	writeJSON(w, 200, c.Slots)
}
func (a *Module) getCodes(w http.ResponseWriter, r *http.Request) {
	c := a.requestCategory(w, r)
	if c == nil {
		return
	}
	writeJSON(w, 200, Object{"codes": c.Codes})
}
