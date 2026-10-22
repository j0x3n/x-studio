package creative

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
)

type PlanConfig struct {
	TemplateSet  map[string]string  `json:"template_set"`
	Models       map[string][]int64 `json:"models"`
	SceneID      *int64             `json:"scene_id"`
	DetailLayout string             `json:"detail_layout"`
}

type Plan struct {
	PlanConfig
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Note       string   `json:"note"`
	Categories []string `json:"categories"`
	Archived   bool     `json:"archived"`
	CreatedBy  int64    `json:"created_by"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
}

type ResolvedPlan struct {
	Plan      *Plan       `json:"plan"`
	Overrides core.Object `json:"overrides"`
	Effective PlanConfig  `json:"effective"`
}

func (a *Module) plan(ctx context.Context, id int64) (*Plan, error) {
	obj, err := a.one(ctx, "SELECT * FROM creative_plans WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	p, err := typed[Plan](obj)
	if err == nil {
		a.resolvePlanScene(ctx, &p.PlanConfig)
	}
	return &p, err
}

func (a *Module) resolvePlanScene(ctx context.Context, config *PlanConfig) {
	if config.SceneID == nil {
		return
	}
	var count int
	if a.DB.QueryRowContext(ctx, "SELECT count(*) FROM scenes WHERE id=? AND archived=0", *config.SceneID).Scan(&count) != nil || count == 0 {
		config.SceneID = nil
	}
}

func (a *Module) EffectivePlan(ctx context.Context, productID int64) (ResolvedPlan, error) {
	out := ResolvedPlan{Overrides: core.Object{}, Effective: PlanConfig{TemplateSet: map[string]string{}, Models: map[string][]int64{}}}
	var planID *int64
	var raw string
	if err := a.DB.QueryRowContext(ctx, "SELECT plan_id,plan_overrides FROM products WHERE id=?", productID).Scan(&planID, &raw); err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(raw), &out.Overrides); err != nil {
		return out, err
	}
	if planID != nil {
		p, err := a.plan(ctx, *planID)
		if err != nil {
			return out, err
		}
		out.Plan = p
		out.Effective = p.PlanConfig
	}
	base := core.JSONObj(core.JS(out.Effective))
	for key, value := range out.Overrides {
		if key == "template_set" || key == "models" {
			merged, ok := base[key].(map[string]any)
			if !ok {
				merged = map[string]any{}
			}
			if entries, ok := value.(map[string]any); ok {
				for slot, item := range entries {
					merged[slot] = item
				}
			}
			base[key] = merged
		} else {
			base[key] = value
		}
	}
	config, err := typed[PlanConfig](base)
	if err != nil {
		return out, err
	}
	a.resolvePlanScene(ctx, &config)
	out.Effective = config
	return out, nil
}

func (a *Module) productPlan(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !a.RequireProduct(w, r, id, false) {
		return
	}
	out, err := a.EffectivePlan(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	core.WriteJSON(w, 200, out)
}

func (a *Module) plansList(w http.ResponseWriter, r *http.Request) {
	rows, err := objects(r.Context(), a.DB, "SELECT * FROM creative_plans WHERE archived=? ORDER BY updated_at DESC,id DESC", r.URL.Query().Get("archived") == "1")
	if err != nil {
		fail(w, err)
		return
	}
	items := []Plan{}
	for _, obj := range rows {
		p, err := typed[Plan](obj)
		if err != nil {
			fail(w, err)
			return
		}
		a.resolvePlanScene(r.Context(), &p.PlanConfig)
		items = append(items, p)
	}
	core.PageItems(w, r, items)
}

func (a *Module) planGet(w http.ResponseWriter, r *http.Request) {
	p, err := a.plan(r.Context(), core.ID(r, "id"))
	if err != nil {
		core.Fail(w, 404, "方案不存在")
		return
	}
	core.WriteJSON(w, 200, core.Object{"plan": p})
}

func (a *Module) planWrite(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	var raw map[string]json.RawMessage
	if !core.Decode(w, r, &raw) {
		return
	}
	if len(raw) == 0 {
		core.Fail(w, 400, "没有要保存的字段")
		return
	}
	p := Plan{Categories: []string{}, PlanConfig: PlanConfig{TemplateSet: map[string]string{}, Models: map[string][]int64{}}}
	creating := r.Method == "POST"
	if !creating {
		old, err := a.plan(r.Context(), core.ID(r, "id"))
		if err != nil {
			core.Fail(w, 404, "方案不存在")
			return
		}
		p = *old
	}
	config := map[string]json.RawMessage{}
	base := core.JSONObj(core.JS(p))
	for key, value := range raw {
		switch key {
		case "name", "note", "categories", "archived":
			if string(value) == "null" {
				core.Fail(w, 400, "方案字段不能是 null")
				return
			}
		case "template_set", "models", "scene_id", "detail_layout":
			config[key] = value
		default:
			core.Fail(w, 400, "不支持的方案字段："+key)
			return
		}
		var decoded any
		if json.Unmarshal(value, &decoded) != nil {
			core.Fail(w, 400, "方案字段格式不正确")
			return
		}
		base[key] = decoded
	}
	parsed, err := typed[Plan](base)
	if err != nil {
		core.Fail(w, 400, "方案字段类型不正确")
		return
	}
	p = parsed
	if strings.TrimSpace(p.Name) == "" || utf8.RuneCountInString(p.Name) > 120 || utf8.RuneCountInString(p.Note) > 4000 {
		core.Fail(w, 400, "方案名称或备注长度不正确")
		return
	}
	if err = a.ValidatePlanFields(r.Context(), core.CurrentUser(r), config); err != nil {
		core.Fail(w, 400, err.Error())
		return
	}
	seen := map[string]bool{}
	for _, key := range p.Categories {
		if _, err = a.CategoryByKey(key); err != nil || seen[key] {
			core.Fail(w, 400, "品类不存在或重复")
			return
		}
		seen[key] = true
	}
	a.savePlan(w, r, p, creating)
}
