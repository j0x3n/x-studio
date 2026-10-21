package creative

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"image"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"unicode"
	"xstudio/internal/core"
)

func normalized(text string) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsSpace(char) || unicode.Is(unicode.P, char) || unicode.Is(unicode.Z, char) {
			return -1
		}
		return char
	}, text)
}
func critical(text string) bool {
	for _, char := range text {
		if unicode.IsDigit(char) {
			return true
		}
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "mm") || strings.Contains(lower, "cm") || strings.ContainsAny(text, "元¥￥")
}
func checkTexts(expected []ExpectedText, read []OCRText) []TextCheck {
	found := ""
	for _, part := range read {
		found += part.Text
	}
	normalizedFound := normalized(found)
	checks := []TextCheck{}
	for _, text := range expected {
		isCritical := text.Critical || critical(text.Text)
		pass := strings.Contains(normalizedFound, normalized(text.Text))
		box := textBox(normalizedFound, normalized(text.Text), read)
		checks = append(checks, TextCheck{Key: text.Key, Expect: text.Text, Found: found, Pass: pass, Warn: !pass && !isCritical, Critical: isCritical, Box: box})
	}
	return checks
}

func textBox(all, expected string, read []OCRText) []float64 {
	start := strings.Index(all, expected)
	offset := 0
	var box []float64
	if start >= 0 && expected != "" {
		for _, part := range read {
			length := len(normalized(part.Text))
			if offset < start+len(expected) && offset+length > start && validBox(part.Box) {
				if box == nil {
					box = append([]float64{}, part.Box...)
				} else {
					right := max(box[0]+box[2], part.Box[0]+part.Box[2])
					bottom := max(box[1]+box[3], part.Box[1]+part.Box[3])
					box[0] = min(box[0], part.Box[0])
					box[1] = min(box[1], part.Box[1])
					box[2] = right - box[0]
					box[3] = bottom - box[1]
				}
			}
			offset += length
		}
		return box
	}
	best := 0.3
	for _, part := range read {
		if !validBox(part.Box) {
			continue
		}
		remaining := []rune(normalized(part.Text))
		matches := 0
		for _, char := range []rune(expected) {
			for index, found := range remaining {
				if char == found {
					matches++
					remaining = append(remaining[:index], remaining[index+1:]...)
					break
				}
			}
		}
		score := float64(2*matches) / float64(max(1, len([]rune(expected))+len([]rune(normalized(part.Text)))))
		if score > best {
			best = score
			box = append([]float64{}, part.Box...)
		}
	}
	return box
}
func hardPass(h Hard) bool {
	if !h.Pixel.Pass || !h.Format.Pass || len(h.Banned) > 0 || h.Error != "" {
		return false
	}
	for _, text := range h.Text {
		if text.Critical && !text.Pass {
			return false
		}
	}
	return true
}
func allPass(review Review) bool {
	if len(review.Checks) != 5 {
		return false
	}
	for _, value := range review.Checks {
		if value != "pass" && value != "na" {
			return false
		}
	}
	return true
}
func sameChecks(first, second Review) bool {
	if len(first.Checks) != 5 || len(second.Checks) != 5 {
		return false
	}
	for key, value := range first.Checks {
		if second.Checks[key] != value {
			return false
		}
	}
	return true
}
func hasUnsure(review Review) bool {
	for _, value := range review.Checks {
		if value == "unsure" {
			return true
		}
	}
	return false
}
func autoEligible(h Hard, first, second Review, settings core.Object) bool {
	return hardPass(h) && allPass(first) && allPass(second) && sameChecks(first, second) && float64(first.Score) >= number(settings["auto_pass_score"]) && float64(second.Score) >= number(settings["auto_pass_score"])
}
func routeItem(item Item, settings core.Object, forced []string, allowRedraw bool, random float64) (string, []string, int) {
	first, second := item.ReviewA, item.ReviewB
	hardFailed := !hardPass(item.Hard)
	commonFail := false
	reasons := append([]string{}, forced...)
	for key, value := range first.Checks {
		if value == "fail" && second.Checks[key] == "fail" {
			commonFail = true
			reasons = append(reasons, "两次复核均不通过："+key)
		}
	}
	if hardFailed {
		reasons = append(reasons, "硬检查不通过")
	}
	low := float64(first.Score) < number(settings["manual_score_min"]) && float64(second.Score) < number(settings["manual_score_min"])
	if low {
		reasons = append(reasons, "两次复核分数偏低")
	}
	risk := 100 - min(first.Score, second.Score)
	if hardFailed {
		risk += 100
	} else if item.Slot == "main1" || item.SizeCompare {
		risk += 80
	} else if !sameChecks(first, second) || hasUnsure(first) || hasUnsure(second) {
		risk += 60
	}
	if hardFailed || commonFail || low {
		if allowRedraw && float64(item.AutoAttempt) < number(settings["max_auto_redraw"]) {
			return "auto_redraw", reasons, risk
		}
		return "manual", reasons, risk
	}
	if autoEligible(item.Hard, first, second, settings) {
		if len(forced) > 0 {
			return "manual", reasons, risk
		}
		if random < number(settings["spot_check_rate"]) {
			return "manual", []string{"抽检"}, risk + 20
		}
		return "auto_pass", []string{}, risk
	}
	return "manual", append(reasons, "复核不确定、结论不一致或分数不足"), risk
}
func imagePart(im image.Image) (core.Object, error) {
	data, err := pngBytes(im)
	if err != nil {
		return nil, err
	}
	return core.Object{"type": "image_url", "image_url": core.Object{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)}}, nil
}
func (a *Module) readText(ctx context.Context, im image.Image, product core.Product) ([]OCRText, error) {
	prompt, err := a.Prompt("生图-文字核对.md")
	if err != nil {
		return nil, err
	}
	part, err := imagePart(im)
	if err != nil {
		return nil, err
	}
	var response struct {
		Texts []OCRText `json:"texts"`
	}
	err = a.AIJSON(core.WithFeature(ctx, "creative_text"), a.Cfg.VisionModel, core.FillPrompt(prompt, product.Category), []core.Object{part}, &response)
	if err == nil && response.Texts == nil {
		err = fmt.Errorf("文字核对回复缺少 texts")
	}
	for _, text := range response.Texts {
		if !validBox(text.Box) {
			return nil, fmt.Errorf("文字核对坐标不正确")
		}
	}
	return response.Texts, err
}
func (a *Module) hardChecks(ctx context.Context, item Item, product core.Product, output *image.NRGBA, pixel PixelCheck) (Hard, error) {
	width, height := imageSize(item.TemplateSnapshot.GenSize)
	hard := Hard{Pixel: pixel, Format: FormatCheck{Pass: output.Rect.Dx() == width && output.Rect.Dy() == height}, Text: []TextCheck{}, Banned: []string{}, OCR: []OCRText{}}
	if item.ProductMode == "photo" {
		return hard, nil
	}
	read, err := a.readText(ctx, output, product)
	if err != nil {
		hard.Error = err.Error()
		return hard, err
	}
	hard.OCR = read
	hard.Text = checkTexts(item.Texts, read)
	words, err := a.Prompt("违禁词.txt")
	if err != nil {
		hard.Error = err.Error()
		return hard, err
	}
	all := ""
	for _, part := range read {
		all += part.Text
	}
	for _, line := range strings.Split(core.CategoryBannedLines(words, product.Category), "\n") {
		word := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if word == "" || strings.HasPrefix(word, "#") || strings.HasPrefix(word, "//") {
			continue
		}
		parts := strings.SplitN(strings.ReplaceAll(word, `\t`, "\t"), "\t", 3)
		if len(parts) >= 2 && (parts[0] == "error" || parts[0] == "warn") {
			word = strings.TrimSpace(parts[1])
		}
		if word != "" && strings.Contains(all, word) && !slices.Contains(hard.Banned, word) {
			hard.Banned = append(hard.Banned, word)
		}
	}
	return hard, nil
}
func validReview(value Review) bool {
	if value.Score < 0 || value.Score > 100 || len(value.Checks) != 5 || value.Issues == nil || value.SizeMM == nil {
		return false
	}
	for _, key := range []string{"product", "size", "body", "placement", "style"} {
		allowed := []string{"pass", "fail", "unsure"}
		if key != "product" && key != "style" {
			allowed = append(allowed, "na")
		}
		if !slices.Contains(allowed, value.Checks[key]) {
			return false
		}
	}
	for _, estimate := range value.SizeMM {
		if estimate <= 0 || math.IsNaN(estimate) || math.IsInf(estimate, 0) {
			return false
		}
	}
	for _, issue := range value.Issues {
		if issue.Reason == "" || !slices.Contains([]string{"product", "size", "body", "placement", "style"}, issue.Check) || !validBox(issue.Box) {
			return false
		}
	}
	return true
}
func unsureReview() Review {
	return Review{Checks: map[string]string{"product": "unsure", "size": "unsure", "body": "unsure", "placement": "unsure", "style": "unsure"}, Score: 60, SizeMM: []float64{}, Issues: []Issue{}}
}
func (a *Module) reviewOnce(ctx context.Context, item Item, product core.Product, params Params, refs []image.Image, output image.Image, reverse bool) (Review, error) {
	prompt, err := a.Prompt("生图-核对.md")
	if err != nil {
		return unsureReview(), err
	}
	order := "最后一张是生成图，前面的图片是实拍原图。"
	images := append([]image.Image{}, refs...)
	if item.ProductMode == "photo" || item.ProductMode == "none" {
		images = nil
		order = "唯一一张是成品，只检查视觉规范；商品、尺寸、人体和佩戴不适用。"
	}
	if reverse {
		images = append([]image.Image{output}, images...)
		if len(images) > 1 {
			order = "第一张是生成图，后面的图片是实拍原图。"
		}
	} else {
		images = append(images, output)
	}
	input := core.Object{"slot": item.Slot, "product_mode": item.ProductMode, "wear_type": item.WearType, "size_compare": item.SizeCompare, "expected": core.Object{"sizes": expectedSizes(item, params), "texts": item.Texts, "placement": placement(product)}, "product": core.Object{"name": product.Name, "info": product.Info}, "category": product.Category.Brief()}
	parts := []core.Object{{"type": "text", "text": core.JS(input)}}
	for _, im := range images {
		part, err := imagePart(im)
		if err != nil {
			return unsureReview(), err
		}
		parts = append(parts, part)
	}
	prompt = strings.ReplaceAll(core.FillPrompt(prompt, product.Category), "{{order}}", order)
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		var raw core.Object
		err = a.AIJSON(core.WithFeature(ctx, "creative_verify"), a.Cfg.VisionModel, prompt, parts, &raw)
		if err == nil {
			value, decodeErr := typed[Review](raw)
			_, hasScore := raw["score"]
			if decodeErr == nil && hasScore && validReview(value) {
				if item.ProductMode != "model" {
					value.Checks["size"] = "na"
					value.Checks["body"] = "na"
					value.Checks["placement"] = "na"
				} else {
					a.checkSize(&value, item, params)
				}
				if item.ProductMode == "photo" || item.ProductMode == "none" {
					value.Checks["product"] = "pass"
				}
				return value, nil
			}
			err = fmt.Errorf("复核回复格式不正确")
		}
		last = err
	}
	return unsureReview(), last
}
func expectedSizes(item Item, params Params) []Size {
	if item.SizeCompare && item.Seq > 0 && item.Seq <= len(params.Sizes) {
		return []Size{params.Sizes[item.Seq-1]}
	}
	return params.Sizes
}
func (a *Module) checkSize(review *Review, item Item, params Params) {
	sizes := expectedSizes(item, params)
	if len(sizes) == 0 || len(review.SizeMM) != len(sizes) {
		review.Checks["size"] = "unsure"
		return
	}
	tolerance := number(a.settings()["size_tolerance"])
	for index, size := range sizes {
		if size.MM <= 0 || math.Abs(review.SizeMM[index]/size.MM-1) > tolerance {
			review.Checks["size"] = "fail"
		}
	}
}
func (a *Module) forced(ctx context.Context, item Item) []string {
	reasons := []string{}
	if item.Slot == "main1" {
		reasons = append(reasons, "首图强制人工")
	}
	if item.SizeCompare {
		reasons = append(reasons, "多尺寸对比强制人工")
	}
	if item.ProductMode == "model" && item.WearType != "" {
		var open bool
		_ = a.DB.QueryRowContext(ctx, "SELECT open FROM creative_calibration WHERE category=?", item.WearType).Scan(&open)
		if !open {
			reasons = append(reasons, "校准期")
		}
	}
	var tight bool
	_ = a.DB.QueryRowContext(ctx, "SELECT tightened FROM creative_calibration WHERE category=?", "tight:"+item.Slot).Scan(&tight)
	if tight {
		reasons = append(reasons, "售后收紧")
	}
	return reasons
}
func (a *Module) decide(ctx context.Context, item *Item, set Set) {
	item.Route, item.RouteReasons, item.Risk = routeItem(*item, a.settings(), a.forced(ctx, *item), item.SetID != nil && set.ImagesUsed < set.ImagesAllowance, rand.Float64())
}
func (a *Module) calibrate(ctx context.Context, tx *sql.Tx, item Item, verdict string, settings core.Object) error {
	if item.ProductMode != "model" || item.WearType == "" {
		return nil
	}
	agree := autoEligible(item.Hard, item.ReviewA, item.ReviewB, settings) == (verdict == "pass")
	_, err := tx.ExecContext(ctx, `INSERT INTO creative_calibration(category,total,agree,updated_at) VALUES(?,1,?,?) ON CONFLICT(category) DO UPDATE SET total=total+1,agree=agree+excluded.agree,updated_at=excluded.updated_at`, item.WearType, agree, core.Now())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE creative_calibration SET open=(total>=? AND CAST(agree AS REAL)/total>=?) WHERE category=?`, int(number(settings["calibration_count"])), number(settings["calibration_agreement"]), item.WearType)
	return err
}
func placement(product core.Product) string {
	style := stringValue(product.Info["style"])
	switch wearType(style) {
	case "necklace":
		return "40cm贴锁骨上沿；45cm锁骨下2–3cm；50cm胸口上方，颈围32–36cm"
	case "earring":
		return "耳钉位于耳垂正中，45度侧面，头发别到耳后；耳坠从耳垂自然下垂不贴脸；耳垂高15–20mm"
	case "bracelet":
		return "腕骨外侧，手放松，搭桌或拿杯，手围15–16cm"
	case "ring":
		return "食指或中指第二节与第三节之间，指宽15–18mm"
	}
	return ""
}
func wearType(style string) string {
	switch {
	case strings.Contains(style, "项链") || strings.Contains(style, "颈链") || strings.Contains(style, "锁骨链"):
		return "necklace"
	case strings.Contains(style, "耳"):
		return "earring"
	case strings.Contains(style, "手链") || strings.Contains(style, "手串") || strings.Contains(style, "手镯"):
		return "bracelet"
	case strings.Contains(style, "戒"):
		return "ring"
	}
	return ""
}
