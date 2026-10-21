package creative

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/modules/assets"
)

func (a *Module) outputImage(item Item) (*image.NRGBA, error) {
	ph, err := core.ScanPhoto(a.DB.QueryRow("SELECT "+core.PhotoColumns+" FROM assets WHERE id=?", item.OutputAssetID))
	if err != nil {
		return nil, problem(404, "成品素材不存在")
	}
	return loadImage(a.Dir(ph.ProductID), ph.File)
}
func (a *Module) savedPixelCheck(item Item, output *image.NRGBA) (PixelCheck, error) {
	if item.ProductMode != "pixel_lock" {
		return PixelCheck{Pass: true}, nil
	}
	canvas, err := loadImage(a.ListingDir(item.ListingID), item.CanvasFile)
	if err != nil {
		return PixelCheck{}, err
	}
	mask, err := loadImage(a.ListingDir(item.ListingID), strings.TrimSuffix(item.CanvasFile, ".png")+"-mask.png")
	if err != nil {
		return PixelCheck{}, err
	}
	return pixelCheck(output, canvas, alphaImage(mask)), nil
}
func (a *Module) inspect(ctx context.Context, item *Item, output *image.NRGBA, uid int64) error {
	product, err := a.PipelineProduct(item.ListingID)
	if err != nil {
		return err
	}
	var set Set
	if item.SetID != nil {
		set, err = a.set(ctx, *item.SetID)
		if err != nil {
			return err
		}
	}
	refs := []image.Image{}
	for _, id := range item.SourceAssetIDs {
		ph, err := core.ScanPhoto(a.DB.QueryRowContext(ctx, "SELECT "+core.PhotoColumns+" FROM assets WHERE id=? AND product_id=?", id, product.ID))
		if err != nil {
			return err
		}
		im, err := a.sourceImage(ph)
		if err != nil {
			return err
		}
		refs = append(refs, im)
	}
	pixel, err := a.savedPixelCheck(*item, output)
	if err != nil {
		return err
	}
	cutout := item.Hard.Cutout
	item.Hard, err = a.hardChecks(ctx, *item, product, output, pixel)
	item.Hard.Cutout = cutout
	item.Error = ""
	if err != nil {
		item.Error = a.SafeAIError(err.Error())
	}
	item.ReviewA, err = a.reviewOnce(ctx, *item, product, set.Params, refs, output, false)
	if err != nil {
		item.Error = a.SafeAIError(err.Error())
	}
	if item.ProductMode == "photo" {
		item.ReviewB = item.ReviewA
	} else {
		item.ReviewB, err = a.reviewOnce(ctx, *item, product, set.Params, refs, output, true)
		if err != nil {
			item.Error = a.SafeAIError(err.Error())
		}
	}
	a.decide(ctx, item, set)
	return nil
}
func (a *Module) verifyExisting(ctx context.Context, item Item, job core.Job) error {
	output, err := a.outputImage(item)
	if err != nil {
		return err
	}
	if err = a.inspect(ctx, &item, output, job.UserID); err != nil {
		return err
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = authorizeTx(ctx, tx, job.UserID, item.ListingID, core.CreativeRun); err != nil {
		return err
	}
	if err = saveItemResult(ctx, tx, item); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE creative_items SET human='',human_by=NULL,human_at='' WHERE id=? AND superseded=0", item.ID); err != nil {
		return err
	}
	if err = assets.MarkStale(ctx, tx, []int64{item.OutputAssetID}, false); err != nil {
		return err
	}
	if item.SetID != nil {
		if _, err = tx.ExecContext(ctx, "UPDATE creative_sets SET glanced_at='',glanced_by=NULL WHERE id=?", *item.SetID); err != nil {
			return err
		}
	}
	if err = auditTx(ctx, tx, job.UserID, "creative.verify", "creative_item", item.ID, core.Object{}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if item.Route == "auto_redraw" {
		next, err := a.replaceItem(ctx, item, job.UserID, true, strings.Join(item.RouteReasons, "；"))
		if err != nil {
			return err
		}
		return a.generateLoop(ctx, next, job)
	}
	if item.SetID != nil {
		return a.refreshSet(ctx, *item.SetID)
	}
	return nil
}
func textEditMask(item Item, key string, width, height int) (*image.Alpha, error) {
	var box []float64
	for _, check := range item.Hard.Text {
		if check.Key == key {
			box = check.Box
			break
		}
	}
	if !validBox(box) {
		return nil, problem(409, "没有这段文字的 OCR 位置，请先重跑复核或重画")
	}
	left := max(0, int((box[0]-box[2]*0.1)*float64(width)))
	top := max(0, int((box[1]-box[3]*0.1)*float64(height)))
	right := min(width, int((box[0]+box[2]*1.1)*float64(width))+1)
	bottom := min(height, int((box[1]+box[3]*1.1)*float64(height))+1)
	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	for index := range mask.Pix {
		mask.Pix[index] = 255
	}
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			mask.SetAlpha(x, y, color.Alpha{})
		}
	}
	return mask, nil
}
func (a *Module) retextItem(ctx context.Context, old Item, job core.Job) error {
	key, text := stringValue(job.Params["key"]), stringValue(job.Params["text"])
	index := -1
	for current, expected := range old.Texts {
		if expected.Key == key {
			index = current
			break
		}
	}
	if index < 0 {
		return problem(400, "文字 key 不在这张图的 texts 中")
	}
	output, err := a.outputImage(old)
	if err != nil {
		return err
	}
	mask, err := textEditMask(old, key, output.Rect.Dx(), output.Rect.Dy())
	if err != nil {
		return err
	}
	prompt, err := a.Prompt("生图-文字.md")
	if err != nil {
		return err
	}
	product, err := a.PipelineProduct(old.ListingID)
	if err != nil {
		return err
	}
	prompt = core.FillPrompt(prompt, product.Category)
	prompt = promptSection(prompt, "提示词")
	if prompt == "" {
		return fmt.Errorf("改字提示词缺少提示词节")
	}
	oldText := ""
	for _, check := range old.Hard.Text {
		if check.Key == key {
			oldText = check.Found
			break
		}
	}
	hint := ""
	if old.Texts[index].Critical || critical(text) {
		hint = "Numbers and units must be exactly as given."
	}
	prompt = strings.NewReplacer("{{text}}", text, "{{old_text}}", oldText, "{{hint}}", hint).Replace(prompt)
	item, err := a.replaceItem(ctx, old, job.UserID, false, "")
	if err != nil {
		return err
	}
	item.Texts[index].Text = text
	item.Texts[index].Critical = item.Texts[index].Critical || critical(text)
	item.CanvasFile = old.CanvasFile
	item.Hard.Cutout = old.Hard.Cutout
	item.SourceAssetIDs = old.SourceAssetIDs
	item.ModelID = old.ModelID
	item.ModelRefIDs = old.ModelRefIDs
	item.WearType = old.WearType
	item.Prompt = prompt
	if err = a.reserve(ctx, item, job.UserID, job.ID); err != nil {
		return err
	}
	info := core.ImageInfo{}
	data, err := a.EditImageMask(core.WithImageInfo(core.WithFeature(ctx, "creative"), &info), prompt, []image.Image{output}, mask, item.TemplateSnapshot.GenSize)
	if err != nil {
		a.recordError(ctx, item, job.UserID, err)
		return err
	}
	item.Model, item.ModelReturned = info.Model, info.Returned
	edited, err := decodePNG(data)
	if err != nil {
		return err
	}
	for y := 0; y < output.Rect.Dy(); y++ {
		for x := 0; x < output.Rect.Dx(); x++ {
			if mask.AlphaAt(x, y).A == 255 {
				edited.SetNRGBA(x, y, output.NRGBAAt(x, y))
			}
		}
	}
	if err = a.inspect(ctx, &item, edited, job.UserID); err != nil {
		return err
	}
	data, err = pngBytes(edited)
	if err != nil {
		return err
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = authorizeTx(ctx, tx, job.UserID, item.ListingID, core.CreativeRun); err != nil {
		return err
	}
	var parent int64
	if len(item.SourceAssetIDs) > 0 {
		parent = item.SourceAssetIDs[0]
	}
	op := core.Object{"item_id": item.ID, "template_key": item.TemplateKey, "template_version": item.TemplateVersion, "key": key, "text": text}
	if item.ModelID != 0 {
		op["worn"] = core.Object{"model_id": item.ModelID, "model_ref_ids": item.ModelRefIDs, "source_asset_ids": item.SourceAssetIDs}
	}
	item.OutputAssetID, err = assets.SaveOutput(ctx, tx, a.Core, assets.OutputRequest{ProductID: product.ID, ParentID: parent, PNG: data, Source: "ai_edited", Title: "AI 改字 " + item.Slot, Op: op, UserID: job.UserID})
	if err != nil {
		return err
	}
	if err = saveItemResult(ctx, tx, item); err != nil {
		return err
	}
	if err = auditTx(ctx, tx, job.UserID, "creative.retext", "creative_item", item.ID, job.Params); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if item.Route == "auto_redraw" {
		next, err := a.replaceItem(ctx, item, job.UserID, true, strings.Join(item.RouteReasons, "；"))
		if err != nil {
			return err
		}
		return a.generateLoop(ctx, next, job)
	}
	if item.SetID != nil {
		return a.refreshSet(ctx, *item.SetID)
	}
	return nil
}
