package assets

import (
	"context"
	"fmt"
	"image"
	"os"
	"xstudio/internal/core"
	"xstudio/internal/imaging"
	"xstudio/internal/media"
)

func (a *Module) executeViewsEnhance(ctx context.Context, j core.Job) error {
	unlock := a.lockAI(j.UserID)
	defer unlock()
	var in viewJobRequest
	if err := decodeObject(j.Params, &in); err != nil {
		return err
	}
	_, items, err := a.jobAssets(ctx, j, in.AssetIDs, core.AssetUpscale)
	if err != nil {
		return err
	}
	if in.Replace != "auto" && in.Replace != "confirm" {
		return fmt.Errorf("高清化替换模式不正确")
	}
	request := ProcessRequest{Ops: Ops{Upscale: &UpscaleOp{TargetLongSide: 2048}, Format: "png"}}
	prompts, err := a.preparePrompts(request.Ops)
	if err != nil {
		return err
	}
	verifyPrompt, err := a.prompt("高清化-核对.md")
	if err != nil {
		return err
	}
	if err = a.checkQuota(j.UserID, len(items), len(items)); err != nil {
		return err
	}
	results := []Object{}
	for _, item := range items {
		result := Object{"asset_id": item.ID}
		id, e := a.enhanceView(ctx, j, item, in.Replace, request, prompts, verifyPrompt)
		if e != nil {
			result["error"] = a.SafeAIError(e.Error())
		} else {
			result["new_asset_id"] = id
		}
		results = append(results, result)
		if _, err = a.DB.Exec("UPDATE jobs SET result=?,progress=? WHERE id=?", js(Object{"items": results}), len(results)*100/len(items), j.ID); err != nil {
			return err
		}
	}
	return a.saveResults(j, results)
}

func (a *Module) enhanceView(ctx context.Context, j core.Job, original Asset, replace string, request ProcessRequest, prompts processPrompts, verifyPrompt string) (int64, error) {
	fresh, err := a.freshViewAsset(ctx, j, original.ID, core.AssetUpscale)
	if err != nil {
		return 0, err
	}
	p, err := a.Product(j.TargetID)
	if err != nil {
		return 0, err
	}
	original = fresh
	staged, err := a.processOne(ctx, original, request, prompts, nil)
	if err != nil {
		return 0, err
	}
	defer os.Remove(staged.file)
	before, ex, err := a.load(original)
	if err != nil {
		return 0, err
	}
	before = imaging.Geometry(before, ex.Orientation, 0, nil)
	f, err := os.Open(staged.file)
	if err != nil {
		return 0, err
	}
	decoded, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return 0, err
	}
	after := imaging.Geometry(decoded, 1, 0, nil)
	beforeMetrics, _ := imaging.Metrics(before, imaging.Exif{Orientation: 1})
	afterMetrics, _ := imaging.Metrics(after, imaging.Exif{Orientation: 1})
	check := Object{"blur_before": beforeMetrics["blur"], "blur_after": afterMetrics["blur"], "fidelity": 0, "issues": []string{}, "verdict": "fail", "pending": true}
	var verification struct {
		Fidelity *float64 `json:"fidelity"`
		Issues   []string `json:"issues"`
	}
	first, err := imagePart(before)
	if err != nil {
		return 0, err
	}
	second, err := imagePart(after)
	if err != nil {
		return 0, err
	}
	feature := "creative_verify"
	if model, ok := a.Setting("ai_models")[feature].(string); !ok || model == "" {
		feature = "classify"
	}
	content := []Object{{"type": "text", "text": "商品资料：" + js(p.Info) + "。第一张是原图，第二张是高清化候选。"}, first, second}
	err = a.AIJSON(core.WithFeature(ctx, feature), a.Cfg.VisionModel, verifyPrompt, content, &verification)
	valid := err == nil && verification.Fidelity != nil && *verification.Fidelity >= 0 && *verification.Fidelity <= 100 && verification.Issues != nil
	if valid {
		check["fidelity"], check["issues"] = *verification.Fidelity, verification.Issues
		threshold := numeric(a.SettingWithDefaults("views")["auto_fidelity"])
		if *verification.Fidelity >= threshold && len(verification.Issues) == 0 && !media.AIMade(original.Photo) {
			check["verdict"] = "pass"
		}
	} else {
		check["issues"] = []string{"保真核对未完成或返回无效，需人工确认"}
		if err != nil {
			check["verify_error"] = a.SafeAIError(err.Error())
		}
	}
	staged.op["views_enhance"] = true
	staged.metrics, staged.enhanceCheck = afterMetrics, check
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	id, err := a.publishProcess(j, request, *staged)
	if err != nil {
		return 0, err
	}
	if err = a.applyEnhanceCheck(ctx, j, original, id, replace, check, afterMetrics); err != nil {
		return id, err
	}
	return id, nil
}

func (a *Module) applyEnhanceCheck(ctx context.Context, j core.Job, original Asset, id int64, replace string, check Object, metrics Object) error {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	candidate, err := a.freshViewAsset(ctx, j, id, core.AssetUpscale)
	if err != nil {
		return err
	}
	source, err := a.asset(original.ID)
	if err != nil || source.ProductID != j.TargetID || source.Archived {
		return fmt.Errorf("原图已变化")
	}
	threshold := numeric(a.SettingWithDefaults("views")["auto_fidelity"])
	better := numeric(check["blur_after"]) >= max(numeric(check["blur_before"])*1.15, numeric(check["blur_before"])+25)
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentID int64
	if err = tx.QueryRowContext(ctx, "SELECT coalesce(max(id),0) FROM assets WHERE product_id=? AND code=?", j.TargetID, candidate.ViewCode).Scan(&currentID); err != nil {
		return err
	}
	auto := candidate.ViewCode != "" && replace == "auto" && check["verdict"] == "pass" && numeric(check["fidelity"]) >= threshold && better && source.ViewCode == original.ViewCode && (currentID == 0 || (original.Code == original.ViewCode && currentID == original.ID))
	check["pending"] = !auto
	if _, err = tx.ExecContext(ctx, "UPDATE assets SET enhance_check=?,metrics=?,updated_at=? WHERE id=?", js(check), js(metrics), now(), id); err != nil {
		return err
	}
	if auto {
		if err = setCurrentTx(tx, candidate, candidate.ViewCode); err != nil {
			return err
		}
	}
	if err = auditViewTx(ctx, tx, j, "assets.views.enhance", id, Object{"job_id": j.ID, "check": check, "replaced": auto}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if auto {
		a.removePhotoOutputs(j.TargetID, candidate.ViewCode)
	}
	return nil
}
