package creative

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/media"
	"xstudio/internal/modules/assets"
)

var errLimit = errors.New("已达上限，等人确认")

func (a *Module) Jobs() map[string]core.JobHandler {
	return map[string]core.JobHandler{"creative.main": a.execute, "creative.detail": a.execute, "creative.item": a.execute, "creative.verify": a.execute}
}
func (a *Module) set(ctx context.Context, id int64) (Set, error) {
	obj, err := a.one(ctx, "SELECT * FROM creative_sets WHERE id=?", id)
	if err != nil {
		return Set{}, problem(404, "出图套装不存在")
	}
	return typed[Set](obj)
}
func (a *Module) item(ctx context.Context, id int64) (Item, error) {
	obj, err := a.one(ctx, "SELECT * FROM creative_items WHERE id=?", id)
	if err != nil {
		return Item{}, problem(404, "出图图片不存在")
	}
	return typed[Item](obj)
}
func (a *Module) currentItems(ctx context.Context, setID int64) ([]Item, error) {
	objs, err := objects(ctx, a.DB, "SELECT * FROM creative_items WHERE set_id=? AND superseded=0 ORDER BY slot,seq,variant", setID)
	if err != nil {
		return nil, err
	}
	return typed[[]Item](objs)
}
func enqueueTx(ctx context.Context, tx *sql.Tx, kind, target string, id, uid int64, params core.Object) (int64, error) {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE target_type=? AND target_id=? AND status IN ('queued','running')", target, id).Scan(&count); err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, problem(409, "这个目标正在排队或处理中")
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,steps,created_at) VALUES(?,?,?,?,?,'queued','',?)", kind, target, id, uid, core.JS(params), core.Now())
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
func (a *Module) reserve(ctx context.Context, item Item, uid, jobID int64) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = authorizeTx(ctx, tx, uid, item.ListingID, core.CreativeRun); err != nil {
		return err
	}
	if item.SetID != nil {
		result, err := tx.ExecContext(ctx, "UPDATE creative_sets SET images_used=images_used+1,status='generating',glanced_at='',glanced_by=NULL,updated_at=? WHERE id=? AND images_used<images_allowance", core.Now(), *item.SetID)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			if _, err = tx.ExecContext(ctx, "UPDATE creative_sets SET status='paused_limit',updated_at=? WHERE id=?", core.Now(), *item.SetID); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE jobs SET message=? WHERE id=?", errLimit.Error(), jobID); err != nil {
				return err
			}
			if err = auditTx(ctx, tx, uid, "creative.limit", "creative_set", *item.SetID, core.Object{}); err != nil {
				return err
			}
			if err = tx.Commit(); err != nil {
				return err
			}
			return errLimit
		}
	}
	if err = auditTx(ctx, tx, uid, "creative.generate", "creative_item", item.ID, core.Object{"attempt": item.Attempt}); err != nil {
		return err
	}
	return tx.Commit()
}
func (a *Module) execute(ctx context.Context, job core.Job) (executionErr error) {
	defer func() {
		if errors.Is(executionErr, errLimit) {
			_, _ = a.DB.ExecContext(ctx, "UPDATE jobs SET message=? WHERE id=?", errLimit.Error(), job.ID)
			executionErr = nil
		}
	}()
	ctx = core.WithAIActor(ctx, core.AIActor{UserID: job.UserID, JobID: job.ID})
	if job.TargetType == "creative_item" {
		item, err := a.item(ctx, job.TargetID)
		if err != nil {
			return err
		}
		if _, err = a.authorize(ctx, job.UserID, item.ListingID, core.CreativeRun); err != nil {
			return err
		}
		if item.Superseded {
			return problem(409, "图片已有新版本")
		}
		if job.Kind == "creative.verify" {
			return a.verifyExisting(ctx, item, job)
		}
		if stringValue(job.Params["operation"]) == "retext" {
			return a.retextItem(ctx, item, job)
		}
		if job.Params["new"] != true {
			item, err = a.replaceItem(ctx, item, job.UserID, false, stringValue(job.Params["note"]))
			if err != nil {
				return err
			}
		}
		err = a.generateLoop(ctx, item, job)
		return err
	}
	if job.TargetType != "creative_set" {
		return fmt.Errorf("任务目标不正确")
	}
	set, err := a.set(ctx, job.TargetID)
	if err != nil {
		return err
	}
	if _, err = a.authorize(ctx, job.UserID, set.ListingID, core.CreativeRun); err != nil {
		return err
	}
	items, err := a.currentItems(ctx, set.ID)
	if err != nil {
		return err
	}
	kind := strings.TrimPrefix(job.Kind, "creative.")
	for _, item := range items {
		if item.TemplateSnapshot.Kind != kind || (item.OutputAssetID != 0 && item.Route != "auto_redraw") || item.Human == "discard" || contains(item.RouteReasons, "缺少素材，未生成") {
			continue
		}
		if item.OutputAssetID != 0 {
			item, err = a.replaceItem(ctx, item, job.UserID, true, strings.Join(item.RouteReasons, "；"))
			if err != nil {
				return err
			}
		}
		if err = a.generateLoop(ctx, item, job); stopGeneration(ctx, err) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
	}
	for {
		current, readErr := a.currentItems(ctx, set.ID)
		if readErr != nil {
			return readErr
		}
		redrawn := false
		for _, candidate := range current {
			if candidate.TemplateSnapshot.Kind != kind || candidate.Route != "auto_redraw" || candidate.Human != "" {
				continue
			}
			next, replaceErr := a.replaceItem(ctx, candidate, job.UserID, true, strings.Join(candidate.RouteReasons, "；"))
			if replaceErr != nil {
				return replaceErr
			}
			if err = a.generateLoop(ctx, next, job); stopGeneration(ctx, err) {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return err
			}
			redrawn = true
		}
		if !redrawn {
			break
		}
	}
	if err = a.refreshSet(ctx, set.ID); err != nil {
		return err
	}
	if err = a.failureMessage(ctx, set.ID, kind, job.ID); err != nil {
		return err
	}
	if kind == "main" && contains(set.Parts, "detail") {
		tx, err := a.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = authorizeTx(ctx, tx, job.UserID, set.ListingID, core.CreativeRun); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE jobs SET status='done',progress=100,finished_at=? WHERE id=?", core.Now(), job.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE creative_sets SET status='generating',updated_at=? WHERE id=?", core.Now(), set.ID); err != nil {
			return err
		}
		if _, err = enqueueTx(ctx, tx, "creative.detail", "creative_set", set.ID, job.UserID, core.Object{}); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		a.Wake()
	}
	return nil
}
func stopGeneration(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, errLimit) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var status *statusError
	return errors.As(err, &status) && (status.status == 403 || status.status == 404)
}
func (a *Module) failureMessage(ctx context.Context, setID int64, kind string, jobID int64) error {
	items, err := a.currentItems(ctx, setID)
	if err != nil {
		return err
	}
	failures := []string{}
	for _, item := range items {
		if item.TemplateSnapshot.Kind != kind || item.Error == "" {
			continue
		}
		message := []rune(item.Slot + item.Variant + " " + item.Error)
		if len(message) > 60 {
			message = append(message[:59], '…')
		}
		failures = append(failures, string(message))
	}
	if len(failures) == 0 {
		return nil
	}
	_, err = a.DB.ExecContext(ctx, "UPDATE jobs SET message=? WHERE id=?", fmt.Sprintf("%d 张生成失败：%s", len(failures), strings.Join(failures, "；")), jobID)
	return err
}
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func (a *Module) generateLoop(ctx context.Context, item Item, job core.Job) error {
	for {
		if err := a.generateOne(ctx, &item, job); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !errors.Is(err, errLimit) {
				if _, authErr := a.authorize(ctx, job.UserID, item.ListingID, core.CreativeRun); authErr != nil {
					return authErr
				}
				a.recordError(ctx, item, job.UserID, err)
			}
			return err
		}
		if item.Route != "auto_redraw" {
			if item.SetID != nil {
				return a.refreshSet(ctx, *item.SetID)
			}
			return nil
		}
		note := strings.Join(item.RouteReasons, "；")
		for _, review := range []Review{item.ReviewA, item.ReviewB} {
			for _, issue := range review.Issues {
				note += "；" + issue.Reason
			}
		}
		next, err := a.replaceItem(ctx, item, job.UserID, true, note)
		if err != nil {
			return err
		}
		item = next
	}
}
func (a *Module) recordError(ctx context.Context, item Item, uid int64, cause error) {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	if authorizeTx(ctx, tx, uid, item.ListingID, core.CreativeRun) != nil {
		return
	}
	_, _ = tx.ExecContext(ctx, "UPDATE creative_items SET error=?,route='manual',route_reasons=? WHERE id=?", a.SafeAIError(cause.Error()), core.JS([]string{"生成失败"}), item.ID)
	if item.SetID != nil {
		_, _ = tx.ExecContext(ctx, "UPDATE creative_sets SET status='reviewing',updated_at=? WHERE id=? AND status<>'paused_limit'", core.Now(), *item.SetID)
	}
	_ = auditTx(ctx, tx, uid, "creative.failed", "creative_item", item.ID, core.Object{"error": a.SafeAIError(cause.Error())})
	_ = tx.Commit()
}
func (a *Module) selectedPhotos(product core.Product, t Template) ([]core.Photo, error) {
	if t.ProductMode == "none" {
		return []core.Photo{}, nil
	}
	photos, err := a.Photos(product.ID)
	if err != nil {
		return nil, err
	}
	selected := []core.Photo{}
	codes := t.PhotoCodes
	if t.Slot == "main4" && t.ProductMode == "photo" {
		codes = []string{"C1", "C2", "C3"}
	}
	// 重画位：某个编号没有实拍时，用这个编号的 AI 补图（A1_AI 等）顶上；模特位（试戴只收实拍）、实拍原图和像素锁定位只认实拍
	aiFill := t.ProductMode == "redraw"
	for _, code := range codes {
		var pick *core.Photo
		for i, ph := range photos {
			if ph.Code == code && !media.AIMade(ph) {
				pick = &photos[i]
				break
			}
		}
		for i, ph := range photos {
			if pick != nil || !aiFill {
				break
			}
			if base, ok := media.FillBase(ph.Code); ok && base == code && media.AIMade(ph) {
				pick = &photos[i]
			}
		}
		if pick != nil {
			selected = append(selected, *pick)
		}
		if len(selected) == 2 {
			break
		}
	}
	if t.ProductMode != "none" && len(selected) == 0 {
		return nil, problem(409, fmt.Sprintf("缺少实拍参考图（%s）", strings.Join(codes, "、")))
	}
	return selected, nil
}
func lookup(root core.Object, path string) string {
	var value any
	if json.Unmarshal([]byte(core.JS(root)), &value) != nil {
		return ""
	}
	for _, key := range strings.Split(path, ".") {
		switch current := value.(type) {
		case map[string]any:
			value = current[key]
		case []any:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(current) {
				return ""
			}
			value = current[index]
		default:
			return ""
		}
	}
	if value == nil {
		return ""
	}
	if _, ok := value.(map[string]any); ok {
		return core.JS(value)
	}
	if array, ok := value.([]any); ok {
		lines := []string{}
		for _, row := range array {
			if cells, ok := row.([]any); ok {
				words := []string{}
				for _, cell := range cells {
					words = append(words, stringValue(cell))
				}
				lines = append(lines, strings.Join(words, " "))
			} else {
				lines = append(lines, stringValue(row))
			}
		}
		return strings.Join(lines, "\n")
	}
	return stringValue(value)
}
func makeTexts(t Template, product core.Product, seq int, size *Size) []ExpectedText {
	out := []ExpectedText{}
	root := core.Object{"content": product.Content, "listing": product.Listing, "info": product.Info}
	if size != nil {
		root["size"] = core.Object{"label": size.Label}
	}
	for _, spec := range t.Texts {
		from := strings.ReplaceAll(spec.From, "{i}", strconv.Itoa(max(0, seq-1)))
		text := lookup(root, from)
		if strings.HasPrefix(spec.From, "literal:") {
			text = strings.TrimPrefix(spec.From, "literal:")
		}
		isCritical := spec.Critical || critical(text)
		runes := []rune(text)
		if spec.Max > 0 && len(runes) > spec.Max {
			text = string(runes[:spec.Max])
		}
		if text != "" {
			out = append(out, ExpectedText{Key: spec.Key, Text: text, Critical: isCritical})
		}
	}
	return out
}
func (a *Module) generationPrompt(item *Item, product core.Product, params Params) (string, error) {
	t := item.TemplateSnapshot
	if item.ProductMode == "photo" {
		item.Texts = []ExpectedText{}
		return "实拍原图，仅裁切和缩放", nil
	}
	name := "生图-主图.md"
	if t.Kind == "detail" {
		name = "生图-详情.md"
	}
	wrapper, err := a.Prompt(name)
	if err != nil {
		return "", err
	}
	sizes := expectedSizes(*item, params)
	var currentSize *Size
	if len(sizes) == 1 {
		currentSize = &sizes[0]
	}
	if len(item.Texts) == 0 {
		item.Texts = makeTexts(t, product, item.Seq, currentSize)
	}
	if item.SizeCompare && len(sizes) == 1 {
		label := fmt.Sprintf("%gmm", sizes[0].MM)
		found := false
		for _, spec := range t.Texts {
			if spec.From == "size.label" {
				found = true
			}
		}
		for index := range item.Texts {
			if item.Texts[index].Key == "size_mm" {
				item.Texts[index].Text = label
				found = true
			}
		}
		if !found {
			item.Texts = append(item.Texts, ExpectedText{Key: "size_mm", Text: label, Critical: true})
		}
	}
	textLines := []string{}
	if item.Slot == "sku" {
		if sku, ok := product.Listing["sku"].([]any); ok && item.Seq > 0 && item.Seq <= len(sku) {
			if spec, ok := sku[item.Seq-1].(map[string]any); ok {
				found := false
				for _, text := range item.Texts {
					if text.Key == "sku_name" {
						found = true
					}
				}
				if !found {
					item.Texts = append(item.Texts, ExpectedText{Key: "sku_name", Text: stringValue(spec["name"]), Critical: critical(stringValue(spec["name"]))})
				}
			}
		}
	}
	for _, text := range item.Texts {
		textLines = append(textLines, fmt.Sprintf("【%s】“%s”", text.Key, text.Text))
	}
	infoKeys := make([]string, 0, len(product.Info))
	for key := range product.Info {
		infoKeys = append(infoKeys, key)
	}
	sort.Strings(infoKeys)
	info := []string{}
	for _, key := range infoKeys {
		info = append(info, key+"："+stringValue(product.Info[key]))
	}
	modelDescription := ""
	if item.ModelID != 0 {
		_ = a.DB.QueryRow("SELECT description FROM shop_models WHERE id=?", item.ModelID).Scan(&modelDescription)
	}
	values := map[string]string{"款名": product.Name, "商品": strings.Join(info, "；"), "尺寸": core.JS(sizes), "颜色": stringValue(product.Info["color"]), "品类": product.Category.Name, "文字": strings.Join(textLines, " / "), "模特": modelDescription, "佩戴位置": placement(product), "i": strconv.Itoa(max(1, item.Seq)), "问题": item.Prompt}
	values["英文名"] = stringValue(product.Content["nameEn"])
	values["一句话"] = stringValue(product.Content["oneLiner"])
	if currentSize != nil {
		values["尺寸"] = fmt.Sprintf("%gmm（%s）", currentSize.MM, currentSize.Label)
	}
	mainDoc := wrapper
	if t.Kind == "detail" {
		mainDoc, err = a.Prompt("生图-主图.md")
		if err != nil {
			return "", err
		}
	}
	common := promptSection(wrapper, "通用要求")
	mode := promptSection(mainDoc, item.ProductMode)
	if common == "" || mode == "" {
		return "", fmt.Errorf("生图提示词缺少通用要求或商品模式：%s", item.ProductMode)
	}
	if t.Kind == "main" && len(textLines) > 0 {
		common = strings.ReplaceAll(common, "No text, no logo, no watermark, no border, no collage unless this prompt explicitly asks for text.", "Text is allowed only as explicitly specified below.")
	}
	sections := []string{common, mode, t.Prompt}
	if t.Kind == "detail" {
		sections = append(sections, promptSection(wrapper, "图上的字"))
	} else if len(textLines) > 0 {
		sections = append(sections, "Render the following Chinese text exactly, character by character, no extra characters, no typos, crisp and legible: {文字}")
	}
	if item.Prompt != "" {
		sections = append(sections, promptSection(mainDoc, "上一次的问题"))
	}
	prompt := core.FillPrompt(strings.Join(sections, "\n\n"), product.Category)
	for key, value := range values {
		prompt = strings.ReplaceAll(prompt, "{"+key+"}", value)
	}
	prompt += "\n" + params.Note
	if item.Variant == "B" {
		prompt += "\nThis is an alternative version B: use a clearly different camera angle and composition from version A while keeping the same style."
		if item.ProductMode == "redraw" && len(item.SourceAssetIDs) == 1 {
			prompt += "\nShow the product from a different angle than variant A."
		}
	}
	if item.SizeCompare {
		prompt += "\n多尺寸对比：所有图使用同一模特同一侧，构图距离固定，按本图尺寸真实还原。耳饰统一左耳，腕饰统一左腕，戒指统一左手中指，项链统一正面角度。"
	}
	for _, text := range item.Texts {
		if text.Critical || critical(text.Text) {
			prompt += "\nNumbers and units must be exactly as given."
			break
		}
	}
	return prompt, nil
}
func (a *Module) imageContext(ctx context.Context, item Item, info *core.ImageInfo) (context.Context, error) {
	ctx = core.WithFeature(ctx, "creative")
	if item.AutoAttempt > 0 && item.SetID != nil {
		var previousModel string
		var previousAutoAttempt int
		err := a.DB.QueryRowContext(ctx, `SELECT model,auto_attempt FROM creative_items WHERE set_id=? AND slot=? AND seq=? AND variant=? AND attempt<? ORDER BY attempt DESC,id DESC LIMIT 1`, *item.SetID, item.Slot, item.Seq, item.Variant, item.Attempt).Scan(&previousModel, &previousAutoAttempt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		primary, alternate := a.ImageModels(ctx)
		if err == nil && item.AutoAttempt > previousAutoAttempt && previousModel == primary && alternate != "" {
			ctx = core.WithImageModel(ctx, alternate)
		}
	}
	return core.WithImageInfo(ctx, info), nil
}

func (a *Module) generateOne(ctx context.Context, item *Item, job core.Job) error {
	uid := job.UserID
	if _, err := a.authorize(ctx, uid, item.ListingID, core.CreativeRun); err != nil {
		return err
	}
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
	t := item.TemplateSnapshot
	photos, err := a.selectedPhotos(product, t)
	if err != nil {
		return err
	}
	photos = variantPhotos(*item, photos)
	refs := []image.Image{}
	item.SourceAssetIDs = []int64{}
	var mask *image.Alpha
	var cutout *CutoutCheck
	cutoutErrors := []string{}
	for _, ph := range photos {
		im, err := a.sourceImage(ph)
		if err != nil {
			return err
		}
		if item.ProductMode == "pixel_lock" && t.LockShape != "rect" {
			var mode string
			mask, mode = a.ProductMask(ctx, nrgba(im))
			if err = ctx.Err(); err != nil {
				return err
			}
			coverage := maskCoverage(mask)
			if !trustedCutout(mode, coverage) {
				algorithm := "rembg"
				if mode == "fallback" {
					algorithm = "兜底算法"
				}
				cutoutErrors = append(cutoutErrors, fmt.Sprintf("%s 识别出的商品区域占画面 %.1f%%（%s）", ph.Code, coverage*100, algorithm))
				continue
			}
			cutout = &CutoutCheck{Mode: mode, Coverage: coverage, Code: ph.Code}
		}
		refs = append(refs, im)
		item.SourceAssetIDs = append(item.SourceAssetIDs, ph.ID)
		if item.ProductMode == "pixel_lock" {
			break
		}
	}
	if item.ProductMode == "pixel_lock" && len(refs) == 0 {
		return fmt.Errorf("抠图失败：%s，请配置 REMBG_URL 或补拍背景干净的白底实拍", strings.Join(cutoutErrors, "；"))
	}
	if item.ProductMode == "model" {
		item.ModelID, err = a.configuredModel(ctx, product, t)
		if err != nil {
			return err
		}
		if item.SizeCompare && item.SetID != nil {
			var fixedID int64
			if a.DB.QueryRowContext(ctx, "SELECT model_id FROM creative_items WHERE set_id=? AND slot=? AND superseded=0 AND model_id>0 ORDER BY seq LIMIT 1", *item.SetID, item.Slot).Scan(&fixedID) == nil {
				item.ModelID = fixedID
			}
		}
		item.WearType = wearType(stringValue(product.Info["style"]))
	}
	bodyPart := ""
	if item.ProductMode == "model" {
		bodyPart, err = resolveBodyPart(t.Wear, item.WearType)
		if err != nil {
			return err
		}
	}
	prompt, err := a.generationPrompt(item, product, set.Params)
	if err != nil {
		return err
	}
	item.Prompt = prompt
	styleLimit := 3 - len(refs)
	if item.ProductMode == "pixel_lock" {
		styleLimit = 2
	}
	if item.ProductMode == "model" {
		styleLimit = 2 - len(refs)
	}
	if item.ProductMode == "photo" {
		styleLimit = 0
	}
	styles := []image.Image{}
	for _, name := range t.StyleRefs {
		if len(styles) >= styleLimit {
			break
		}
		im, err := loadImage(filepath.Join(a.Cfg.TemplateDir, "生图模板"), name)
		if err != nil {
			return fmt.Errorf("读取风格参考失败：%w", err)
		}
		styles = append(styles, im)
	}
	aiRefs := append(append([]image.Image{}, refs...), styles...)
	width, height := imageSize(t.GenSize)
	info := core.ImageInfo{}
	featureCtx, err := a.imageContext(ctx, *item, &info)
	if err != nil {
		return err
	}
	pixel := PixelCheck{Pass: true}
	var output *image.NRGBA
	var data []byte
	op := core.Object{"item_id": item.ID, "template_key": t.Key, "template_version": t.Version, "prompt_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(prompt)))}
	if err = a.reserve(ctx, *item, uid, job.ID); err != nil {
		return err
	}
	switch item.ProductMode {
	case "pixel_lock":
		src := nrgba(refs[0])
		if t.LockShape == "rect" {
			mask = image.NewAlpha(src.Rect)
			draw.Draw(mask, mask.Rect, image.NewUniform(color.Alpha{A: 255}), image.Point{}, draw.Src)
		}
		canvas, alpha, err := lockCanvas(src, mask, t.ProductBox, width, height, t.LockShape)
		if err != nil {
			return err
		}
		name := fmt.Sprintf("AI出图/canvas-%d.png", item.ID)
		item.CanvasFile = name
		for suffix, im := range map[string]image.Image{"": canvas, "-mask": alpha, "-source": src, "-source-mask": mask} {
			if err = writePNG(a.ListingDir(item.ListingID), strings.TrimSuffix(name, ".png")+suffix+".png", im); err != nil {
				return err
			}
		}
		maskedRefs := []image.Image{canvas}
		for _, style := range styles {
			if len(maskedRefs) >= 3 {
				break
			}
			maskedRefs = append(maskedRefs, style)
		}
		data, err = a.EditImageMask(featureCtx, prompt, maskedRefs, alpha, t.GenSize)
		if err != nil {
			return err
		}
		generated, decodeErr := decodePNG(data)
		if decodeErr != nil {
			return decodeErr
		}
		output = pasteLocked(generated, canvas, alpha)
		pixel = pixelCheck(output, canvas, alpha)
	case "model":
		data, opWorn, err := assets.GenerateWorn(featureCtx, a.Core, assets.WornRequest{UserID: uid, ModelID: item.ModelID, ProductID: product.ID, SourceAssets: item.SourceAssetIDs, BodyPart: bodyPart, Framing: t.Wear.Framing, Size: t.GenSize, Prompt: prompt, StyleRefs: styles})
		if err != nil {
			return err
		}
		item.ModelRefIDs, _ = typed[[]int64](opWorn["model_ref_ids"])
		op["worn"] = opWorn
		output, err = decodePNG(data)
		if err != nil {
			return err
		}
	case "redraw":
		data, err = a.EditImageSize(featureCtx, prompt, aiRefs, t.GenSize)
	case "none":
		data, err = a.GenerateImage(featureCtx, prompt, t.GenSize, styles...)
	case "photo":
		output = resizeCrop(nrgba(refs[0]), width, height)
	default:
		return fmt.Errorf("商品模式不正确")
	}
	if err != nil {
		return err
	}
	if output == nil {
		output, err = decodePNG(data)
		if err != nil {
			return err
		}
	}
	item.Model, item.ModelReturned = info.Model, info.Returned
	if _, err = a.authorize(ctx, uid, item.ListingID, core.CreativeRun); err != nil {
		return err
	}
	hard, hardErr := a.hardChecks(ctx, *item, product, output, pixel)
	hard.Cutout = cutout
	item.Hard = hard
	if hardErr != nil {
		item.Error = a.SafeAIError(hardErr.Error())
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
	if item.SetID == nil && item.Route == "auto_redraw" {
		item.Route = "manual"
	}
	data, err = pngBytes(output)
	if err != nil {
		return err
	}
	source := "ai_generated"
	if item.ProductMode == "pixel_lock" || item.ProductMode == "photo" {
		source = "ai_edited"
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = authorizeTx(ctx, tx, uid, item.ListingID, core.CreativeRun); err != nil {
		return err
	}
	var current bool
	if err = tx.QueryRowContext(ctx, "SELECT superseded FROM creative_items WHERE id=?", item.ID).Scan(&current); err != nil {
		return err
	}
	if current {
		return problem(409, "图片已有新版本")
	}
	var parent int64
	if len(item.SourceAssetIDs) > 0 {
		parent = item.SourceAssetIDs[0]
	}
	item.OutputAssetID, err = assets.SaveOutput(ctx, tx, a.Core, assets.OutputRequest{ProductID: product.ID, ParentID: parent, PNG: data, Source: source, Title: "AI " + item.Slot + item.Variant, Op: op, UserID: uid})
	if err != nil {
		return err
	}
	if err = saveItemResult(ctx, tx, *item); err != nil {
		return err
	}
	if err = auditTx(ctx, tx, uid, "creative.output", "creative_item", item.ID, core.Object{"asset_id": item.OutputAssetID, "route": item.Route}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if item.SizeCompare && item.SetID != nil {
		if err = a.compareSizes(ctx, *item.SetID, uid); err != nil {
			return err
		}
		*item, err = a.item(ctx, item.ID)
		return err
	}
	return nil
}
func decodePNG(data []byte) (*image.NRGBA, error) {
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return nrgba(im), nil
}
func saveItemResult(ctx context.Context, tx *sql.Tx, item Item) error {
	_, err := tx.ExecContext(ctx, `UPDATE creative_items SET source_asset_ids=?,model_id=?,model_ref_ids=?,wear_type=?,prompt=?,texts=?,output_asset_id=?,canvas_file=?,hard=?,review_a=?,review_b=?,route=?,route_reasons=?,risk=?,error=?,model=?,model_returned=? WHERE id=?`, core.JS(item.SourceAssetIDs), item.ModelID, core.JS(item.ModelRefIDs), item.WearType, item.Prompt, core.JS(item.Texts), item.OutputAssetID, item.CanvasFile, core.JS(item.Hard), core.JS(item.ReviewA), core.JS(item.ReviewB), item.Route, core.JS(item.RouteReasons), item.Risk, item.Error, item.Model, item.ModelReturned, item.ID)
	return err
}
func (a *Module) replaceItem(ctx context.Context, old Item, uid int64, automatic bool, note string) (Item, error) {
	if old.SetID != nil {
		set, err := a.set(ctx, *old.SetID)
		if err != nil {
			return Item{}, err
		}
		if set.ImagesUsed >= set.ImagesAllowance {
			_, _ = a.DB.ExecContext(ctx, "UPDATE creative_sets SET status='paused_limit' WHERE id=?", set.ID)
			return Item{}, errLimit
		}
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()
	if err = authorizeTx(ctx, tx, uid, old.ListingID, core.CreativeRun); err != nil {
		return Item{}, err
	}
	result, err := tx.ExecContext(ctx, "UPDATE creative_items SET superseded=1 WHERE id=? AND superseded=0", old.ID)
	if err != nil {
		return Item{}, err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return Item{}, problem(409, "图片已有新版本")
	}
	next := old
	next.ID = 0
	next.Attempt++
	if automatic {
		next.AutoAttempt++
	}
	next.Prompt = note
	next.OutputAssetID = 0
	next.Model = ""
	next.ModelReturned = ""
	next.CanvasFile = ""
	next.Hard = Hard{}
	next.ReviewA = Review{}
	next.ReviewB = Review{}
	next.Route = "pending"
	next.RouteReasons = []string{}
	next.Human = ""
	next.Error = ""
	next.CreatedBy = uid
	id, err := insertItem(ctx, tx, next)
	if err != nil {
		return Item{}, err
	}
	next.ID = id
	if old.SetID != nil {
		_, err = tx.ExecContext(ctx, "UPDATE creative_sets SET glanced_at='',glanced_by=NULL,status='generating',hero_b_item_id=CASE WHEN hero_b_item_id=? THEN ? ELSE hero_b_item_id END,updated_at=? WHERE id=?", old.ID, id, core.Now(), *old.SetID)
		if err != nil {
			return Item{}, err
		}
	}
	if err = auditTx(ctx, tx, uid, "creative.redraw", "creative_item", id, core.Object{"previous_id": old.ID, "automatic": automatic, "note": note}); err != nil {
		return Item{}, err
	}
	return next, tx.Commit()
}
func insertItem(ctx context.Context, tx *sql.Tx, item Item) (int64, error) {
	if item.Route == "" {
		item.Route = "pending"
	}
	if item.RouteReasons == nil {
		item.RouteReasons = []string{}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO creative_items(set_id,listing_id,slot,seq,template_key,template_version,template_snapshot,attempt,auto_attempt,variant,product_mode,prompt,texts,size_compare,created_by,created_at,error,route,route_reasons) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.SetID, item.ListingID, item.Slot, item.Seq, item.TemplateKey, item.TemplateVersion, core.JS(item.TemplateSnapshot), item.Attempt, item.AutoAttempt, item.Variant, item.ProductMode, item.Prompt, core.JS(item.Texts), item.SizeCompare, item.CreatedBy, core.Now(), item.Error, item.Route, core.JS(item.RouteReasons))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
func (a *Module) refreshSet(ctx context.Context, id int64) error {
	set, err := a.set(ctx, id)
	if err != nil {
		return err
	}
	if set.Status == "paused_limit" {
		return nil
	}
	items, err := a.currentItems(ctx, id)
	if err != nil {
		return err
	}
	status := "ready"
	for _, item := range items {
		if item.OutputAssetID == 0 || item.Human == "discard" || !hardPass(item.Hard) || (item.Route != "auto_pass" && item.Human != "pass") {
			status = "reviewing"
			break
		}
	}
	_, err = a.DB.ExecContext(ctx, "UPDATE creative_sets SET status=?,updated_at=? WHERE id=?", status, core.Now(), id)
	return err
}
func (a *Module) compareSizes(ctx context.Context, setID, uid int64) error {
	set, err := a.set(ctx, setID)
	if err != nil {
		return err
	}
	items, err := a.currentItems(ctx, setID)
	if err != nil {
		return err
	}
	siblings := []Item{}
	for _, item := range items {
		if item.SizeCompare && item.OutputAssetID != 0 {
			siblings = append(siblings, item)
		}
	}
	tolerance := number(a.settings()["size_compare_tolerance"])
	for _, item := range siblings {
		changed := false
		for _, other := range siblings {
			if item.ID == other.ID || item.Seq > len(set.Params.Sizes) || other.Seq > len(set.Params.Sizes) {
				continue
			}
			expected := set.Params.Sizes[item.Seq-1].MM / set.Params.Sizes[other.Seq-1].MM
			for _, pair := range [][2]*Review{{&item.ReviewA, &other.ReviewA}, {&item.ReviewB, &other.ReviewB}} {
				if len(pair[0].SizeMM) == 1 && len(pair[1].SizeMM) == 1 && pair[1].SizeMM[0] > 0 && ratioDeviation(pair[0].SizeMM[0]/pair[1].SizeMM[0]/expected) > tolerance {
					pair[0].Checks["size"] = "fail"
					changed = true
				}
			}
		}
		if changed {
			a.decide(ctx, &item, set)
			item.RouteReasons = append(item.RouteReasons, "多尺寸图比例偏差超出上限")
			item.Human = ""
			tx, err := a.DB.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			if err = authorizeTx(ctx, tx, uid, item.ListingID, core.CreativeRun); err == nil {
				err = saveItemResult(ctx, tx, item)
			}
			if err == nil {
				_, err = tx.ExecContext(ctx, "UPDATE creative_items SET human='',human_by=NULL,human_at='' WHERE id=?", item.ID)
			}
			if err == nil {
				_, err = tx.ExecContext(ctx, "UPDATE creative_sets SET glanced_at='',glanced_by=NULL WHERE id=?", setID)
			}
			if err == nil {
				err = auditTx(ctx, tx, uid, "creative.size_compare", "creative_item", item.ID, core.Object{})
			}
			if err == nil {
				err = tx.Commit()
			} else {
				_ = tx.Rollback()
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}
func ratioDeviation(value float64) float64 {
	if value <= 0 {
		return 1
	}
	return max(value, 1/value) - 1
}
func alphaImage(im *image.NRGBA) *image.Alpha {
	alpha := image.NewAlpha(im.Rect)
	for y := 0; y < im.Rect.Dy(); y++ {
		for x := 0; x < im.Rect.Dx(); x++ {
			alpha.SetAlpha(x, y, color.Alpha{A: im.NRGBAAt(x, y).A})
		}
	}
	return alpha
}
