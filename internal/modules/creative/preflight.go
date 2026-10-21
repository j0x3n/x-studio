package creative

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"xstudio/internal/core"
)

type Missing struct {
	Slot        string `json:"slot"`
	Label       string `json:"label"`
	TemplateKey string `json:"template_key"`
	Reason      string `json:"reason"`
}

func (a *Module) configuredModel(ctx context.Context, product core.Product, t Template) (int64, error) {
	obj, err := a.one(ctx, "SELECT models FROM creative_shop_settings WHERE shop_id=?", product.ShopID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	models, _ := typed[map[string]int64](obj["models"])
	if t.Wear == nil || models[t.Wear.Slot] == 0 {
		return 0, problem(409, "先在 AI 出图设置里给店铺选模特")
	}
	return models[t.Wear.Slot], nil
}

func (a *Module) preflight(ctx context.Context, product core.Product, items []Item) ([]Missing, error) {
	missing := []Missing{}
	for index := range items {
		item := &items[index]
		_, photoErr := a.selectedPhotos(product, item.TemplateSnapshot)
		reasons := []string{}
		checks := []error{photoErr}
		if item.ProductMode == "model" {
			_, modelErr := a.configuredModel(ctx, product, item.TemplateSnapshot)
			checks = append(checks, modelErr)
		}
		for _, err := range checks {
			if err == nil {
				continue
			}
			var status *statusError
			if !errors.As(err, &status) || status.status != 409 {
				return nil, err
			}
			reasons = append(reasons, err.Error())
		}
		if len(reasons) == 0 {
			continue
		}
		item.Error = strings.Join(reasons, "；")
		item.Route = "manual"
		item.RouteReasons = []string{"缺少素材，未生成"}
		missing = append(missing, Missing{Slot: item.Slot, Label: slotLabel(item.Slot), TemplateKey: item.TemplateKey, Reason: item.Error})
	}
	return missing, nil
}

func writeMissing(w http.ResponseWriter, missing []Missing) {
	messages := []string{}
	for _, item := range missing {
		messages = append(messages, item.Label+"："+item.Reason)
	}
	core.WriteJSON(w, 409, core.Object{"error": strings.Join(messages, "；"), "missing": missing})
}

func (a *Module) preflightItem(w http.ResponseWriter, r *http.Request, item Item) bool {
	product, err := a.PipelineProduct(item.ListingID)
	if err != nil {
		fail(w, err)
		return false
	}
	missing, err := a.preflight(r.Context(), product, []Item{item})
	if err != nil {
		fail(w, err)
		return false
	}
	if len(missing) > 0 {
		writeMissing(w, missing)
		return false
	}
	return true
}
