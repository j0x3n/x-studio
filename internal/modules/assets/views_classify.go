package assets

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/imaging"
	"xstudio/internal/media"
)

type viewSuggestion struct {
	Code       string   `json:"code"`
	Confidence *float64 `json:"confidence"`
	Reason     string   `json:"reason"`
}

func auditViewTx(ctx context.Context, tx *sql.Tx, j core.Job, action string, id int64, detail any) error {
	r, _ := http.NewRequestWithContext(ctx, "POST", "http://localhost/", nil)
	if err := core.AuditTx(tx, r, action, "asset", id, detail); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE audit_log SET user_id=? WHERE id=last_insert_rowid()", j.UserID)
	return err
}

func (a *Module) executeViewsClassify(ctx context.Context, j core.Job) error {
	unlock := a.lockAI(j.UserID)
	defer unlock()
	var in viewJobRequest
	if err := decodeObject(j.Params, &in); err != nil {
		return err
	}
	_, items, err := a.jobAssets(ctx, j, in.AssetIDs, core.AssetUpload)
	if err != nil {
		return err
	}
	prompt, err := a.prompt("分拣-底图.md")
	if err != nil {
		return err
	}
	if err = a.checkQuota(j.UserID, 0, len(items)); err != nil {
		return err
	}
	results := []Object{}
	for _, item := range items {
		result := Object{"asset_id": item.ID}
		suggestion, e := a.classifyView(ctx, j, item, prompt)
		if e == nil {
			e = a.applyViewSuggestion(ctx, j, item, suggestion)
		}
		if e != nil {
			result["error"] = a.SafeAIError(e.Error())
		} else {
			result["suggest"] = suggestion
		}
		results = append(results, result)
	}
	return a.saveResults(j, results)
}

func (a *Module) classifyView(ctx context.Context, j core.Job, item Asset, prompt string) (viewSuggestion, error) {
	var out viewSuggestion
	fresh, err := a.freshViewAsset(ctx, j, item.ID, core.AssetUpload)
	if err != nil {
		return out, err
	}
	p, err := a.Product(j.TargetID)
	if err != nil {
		return out, err
	}
	im, ex, err := a.load(fresh)
	if err != nil {
		return out, err
	}
	part, err := imagePart(imaging.Geometry(im, ex.Orientation, fresh.Rotate, fresh.Crop))
	if err != nil {
		return out, err
	}
	content := []Object{{"type": "text", "text": "商品资料：" + js(p.Info) + "；允许的底图槽位：" + js(media.ProductViews(p))}, part}
	if err = a.AIJSON(core.WithFeature(ctx, "classify"), a.Cfg.VisionModel, prompt, content, &out); err != nil {
		return out, err
	}
	if out.Confidence == nil || math.IsNaN(*out.Confidence) || *out.Confidence < 0 || *out.Confidence > 1 || strings.TrimSpace(out.Reason) == "" || !bounded(out.Reason, 1000) || (out.Code != "" && !viewCodeAllowed(p, out.Code)) {
		return out, fmt.Errorf("AI 返回的槽位、置信度或原因不正确")
	}
	return out, nil
}

func (a *Module) applyViewSuggestion(ctx context.Context, j core.Job, original Asset, suggestion viewSuggestion) error {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	fresh, err := a.freshViewAsset(ctx, j, original.ID, core.AssetUpload)
	if err != nil {
		return err
	}
	if fresh.Code != original.Code || fresh.ViewCode != original.ViewCode || fresh.File != original.File {
		return fmt.Errorf("照片归属已变化，跳过分拣结果")
	}
	p, err := a.Product(j.TargetID)
	if err != nil {
		return err
	}
	if suggestion.Code != "" && !viewCodeAllowed(p, suggestion.Code) {
		return fmt.Errorf("品类底图槽位已变化")
	}
	threshold := numeric(a.SettingWithDefaults("views")["classify_confidence"])
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE product_id=? AND code=?", j.TargetID, suggestion.Code).Scan(&count); err != nil {
		return err
	}
	assigned := suggestion.Code != "" && suggestion.Confidence != nil && *suggestion.Confidence >= threshold && count == 0 && fresh.ViewCode == "" && fresh.Code == ""
	if assigned {
		check := fresh.Photo
		check.Code = suggestion.Code
		if err = validatePhoto(check); err != nil {
			return err
		}
		err = setCurrentTx(tx, fresh, suggestion.Code)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE assets SET suggest=?,updated_at=? WHERE id=?", js(suggestion), now(), fresh.ID)
	}
	if err != nil {
		return err
	}
	if err = auditViewTx(ctx, tx, j, "assets.views.classify", fresh.ID, Object{"suggest": suggestion, "assigned": assigned, "job_id": j.ID}); err != nil {
		return err
	}
	return tx.Commit()
}

func numeric(value any) float64 {
	switch number := value.(type) {
	case float64:
		return number
	case int:
		return float64(number)
	case int64:
		return float64(number)
	}
	return 0
}
