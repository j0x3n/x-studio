package scenes

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/draw"
	"xstudio/internal/core"
)

type Layout struct {
	Size     string          `json:"size"`
	Elements []LayoutElement `json:"elements"`
}
type LayoutElement struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	PropID   int64      `json:"prop_id,omitempty"`
	Label    string     `json:"label"`
	Box      [4]float64 `json:"box"`
	Note     string     `json:"note"`
	PropName string     `json:"prop_name,omitempty"`
	CoverURL string     `json:"cover_url,omitempty"`
}

type region struct {
	Element LayoutElement
	Name    string
	Color   color.NRGBA
}

type cmSize struct {
	W float64 `json:"w,omitempty"`
	D float64 `json:"d,omitempty"`
	H float64 `json:"h,omitempty"`
}
type propCard struct {
	ID          int64
	ShopID      *int64
	Name        string
	Description string
	Size        cmSize
	Archived    bool
	CoverID     *int64
	File        string
	Found       bool
}

var sketchBackground = color.NRGBA{R: 232, G: 232, B: 232, A: 255}
var productColor = color.NRGBA{R: 255, G: 0, B: 255, A: 255}
var propColors = []struct {
	name string
	c    color.NRGBA
}{
	{"红", color.NRGBA{R: 220, G: 40, B: 40, A: 255}},
	{"蓝", color.NRGBA{R: 40, G: 90, B: 220, A: 255}},
	{"绿", color.NRGBA{R: 30, G: 160, B: 70, A: 255}},
	{"橙", color.NRGBA{R: 230, G: 120, B: 20, A: 255}},
	{"紫", color.NRGBA{R: 140, G: 50, B: 190, A: 255}},
	{"青", color.NRGBA{R: 20, G: 160, B: 180, A: 255}},
	{"棕", color.NRGBA{R: 140, G: 90, B: 40, A: 255}},
	{"灰", color.NRGBA{R: 90, G: 90, B: 90, A: 255}},
}

func validCamera(camera string) bool {
	switch camera {
	case "", "top", "angle45", "eye", "macro":
		return true
	default:
		return false
	}
}
func cameraLabel(camera string) string {
	switch camera {
	case "top":
		return "顶拍"
	case "angle45":
		return "俯拍 45°"
	case "eye":
		return "平视"
	case "macro":
		return "微距特写"
	default:
		return "未指定"
	}
}
func parseImageSize(size string) (int, int, bool) {
	var width, height int
	if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil {
		return 0, 0, false
	}
	if fmt.Sprintf("%dx%d", width, height) != size {
		return 0, 0, false
	}
	switch size {
	case "1024x1024", "1024x1536", "1536x1024":
		return width, height, true
	default:
		return 0, 0, false
	}
}
func normalizeLayout(layout *Layout) error {
	if strings.TrimSpace(layout.Size) == "" {
		layout.Size = "1024x1536"
	}
	if _, _, ok := parseImageSize(layout.Size); !ok {
		return invalid(400, "定场图尺寸只能是 1024x1024、1024x1536 或 1536x1024")
	}
	if layout.Elements == nil {
		layout.Elements = []LayoutElement{}
	}
	if len(layout.Elements) > 12 {
		return invalid(400, "布局元素最多 12 个")
	}
	seen := map[string]bool{}
	products := 0
	for index := range layout.Elements {
		element := &layout.Elements[index]
		element.ID = strings.TrimSpace(element.ID)
		element.Type = strings.TrimSpace(element.Type)
		element.Label = strings.TrimSpace(element.Label)
		element.Note = strings.TrimSpace(element.Note)
		if count := utf8.RuneCountInString(element.ID); count < 1 || count > 40 {
			return invalid(400, "布局元素编号要填 1–40 字")
		}
		if seen[element.ID] {
			return invalid(400, "布局元素编号重复")
		}
		seen[element.ID] = true
		if utf8.RuneCountInString(element.Label) > 40 {
			return invalid(400, "布局元素名称不能超过 40 字")
		}
		if utf8.RuneCountInString(element.Note) > 200 {
			return invalid(400, "布局备注不能超过 200 字")
		}
		switch element.Type {
		case "product":
			products++
			if products > 1 {
				return invalid(400, "布局里只能有一个商品框")
			}
			if element.PropID != 0 {
				return invalid(400, "商品框不能指定道具")
			}
		case "prop":
			if element.PropID <= 0 {
				return invalid(400, "道具元素要指定道具")
			}
		default:
			return invalid(400, "布局元素只能是道具或商品")
		}
		if err := validBox(element.Box); err != nil {
			return err
		}
	}
	return nil
}
func validBox(box [4]float64) error {
	for _, value := range box {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return invalid(400, "布局框数字不正确")
		}
	}
	x, y, w, h := box[0], box[1], box[2], box[3]
	if w <= 0 || h <= 0 {
		return invalid(400, "布局框的宽和高要大于 0")
	}
	const eps = 1e-6
	if x < -eps || y < -eps || x+w > 1+eps || y+h > 1+eps {
		return invalid(400, "布局框必须在画布内")
	}
	return nil
}
func canonicalLayout(layout Layout) string {
	clone := layout
	clone.Elements = make([]LayoutElement, len(layout.Elements))
	for index, element := range layout.Elements {
		element.PropName = ""
		element.CoverURL = ""
		element.ID = strings.TrimSpace(element.ID)
		element.Label = strings.TrimSpace(element.Label)
		element.Note = strings.TrimSpace(element.Note)
		if element.Type != "prop" {
			element.PropID = 0
		}
		clone.Elements[index] = element
	}
	if clone.Elements == nil {
		clone.Elements = []LayoutElement{}
	}
	return core.JS(clone)
}
func layoutsDiffer(current Layout, snapshot string) bool {
	var saved Layout
	if json.Unmarshal([]byte(snapshot), &saved) != nil {
		return true
	}
	return canonicalLayout(current) != canonicalLayout(saved)
}
func layoutRegions(layout Layout) []region {
	out := make([]region, 0, len(layout.Elements))
	propIndex := 0
	for _, element := range layout.Elements {
		if element.Type == "product" {
			out = append(out, region{Element: element, Name: "洋红", Color: productColor})
			continue
		}
		item := propColors[propIndex%len(propColors)]
		name := item.name
		if cycle := propIndex/len(propColors) + 1; cycle > 1 {
			name = fmt.Sprintf("%s（第%d个）", item.name, cycle)
		}
		propIndex++
		out = append(out, region{Element: element, Name: name, Color: item.c})
	}
	return out
}
func drawSketch(layout Layout) (*image.NRGBA, error) {
	width, height, ok := parseImageSize(layout.Size)
	if !ok {
		return nil, invalid(400, "定场图尺寸只能是 1024x1024、1024x1536 或 1536x1024")
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(sketchBackground), image.Point{}, draw.Src)
	for _, item := range layoutRegions(layout) {
		paintBox(img, item.Element.Box, item.Color)
	}
	return img, nil
}
func boxRect(box [4]float64, width, height int) image.Rectangle {
	x := int(math.Round(box[0] * float64(width)))
	y := int(math.Round(box[1] * float64(height)))
	w := int(math.Round(box[2] * float64(width)))
	h := int(math.Round(box[3] * float64(height)))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return image.Rect(x, y, x+w, y+h)
}
func paintBox(dst *image.NRGBA, box [4]float64, ink color.NRGBA) {
	rect := boxRect(box, dst.Bounds().Dx(), dst.Bounds().Dy()).Intersect(dst.Bounds())
	if rect.Empty() {
		return
	}
	fill := ink
	fill.A = 145
	draw.Draw(dst, rect, image.NewUniform(fill), image.Point{}, draw.Over)
	opaque := ink
	opaque.A = 255
	border := 12
	top := rect
	top.Max.Y = min(rect.Min.Y+border, rect.Max.Y)
	draw.Draw(dst, top, image.NewUniform(opaque), image.Point{}, draw.Src)
	bottom := rect
	bottom.Min.Y = max(rect.Max.Y-border, rect.Min.Y)
	draw.Draw(dst, bottom, image.NewUniform(opaque), image.Point{}, draw.Src)
	left := rect
	left.Max.X = min(rect.Min.X+border, rect.Max.X)
	draw.Draw(dst, left, image.NewUniform(opaque), image.Point{}, draw.Src)
	right := rect
	right.Min.X = max(rect.Max.X-border, rect.Min.X)
	draw.Draw(dst, right, image.NewUniform(opaque), image.Point{}, draw.Src)
}
func cmPhrase(size cmSize) string {
	parts := []string{}
	if size.W > 0 {
		parts = append(parts, "宽 "+strconv.FormatFloat(size.W, 'f', -1, 64)+" cm")
	}
	if size.D > 0 {
		parts = append(parts, "深 "+strconv.FormatFloat(size.D, 'f', -1, 64)+" cm")
	}
	if size.H > 0 {
		parts = append(parts, "高 "+strconv.FormatFloat(size.H, 'f', -1, 64)+" cm")
	}
	return strings.Join(parts, "、")
}
func regionLine(item region, card propCard, known bool) string {
	label := strings.TrimSpace(item.Element.Label)
	if item.Element.Type == "product" {
		if label == "" {
			label = "商品"
		}
		line := item.Name + "色区域：" + label
		if note := strings.TrimSpace(item.Element.Note); note != "" {
			line += "，" + note
		}
		return line + "。"
	}
	if known {
		if card.Name != "" {
			label = card.Name
		}
		if label == "" {
			label = "道具"
		}
		line := item.Name + "色区域：" + label
		if card.Description != "" {
			line += "，" + card.Description
		}
		if phrase := cmPhrase(card.Size); phrase != "" {
			line += "，真实尺寸约" + phrase
		}
		if note := strings.TrimSpace(item.Element.Note); note != "" {
			line += "。" + note
		}
		return line + "。"
	}
	if label == "" {
		label = "道具"
	}
	line := item.Name + "色区域：" + label
	if note := strings.TrimSpace(item.Element.Note); note != "" {
		line += "，" + note
	}
	return line + "。"
}
func (module *Module) loadProp(ctx context.Context, id int64) (propCard, error) {
	var card propCard
	var shop sql.NullInt64
	var cover sql.NullInt64
	var file sql.NullString
	var size string
	err := module.DB.QueryRowContext(ctx, `SELECT shop_id,name,description,size_cm,archived,cover_ref_id,(SELECT file FROM prop_refs WHERE prop_id=props.id AND view='main' AND status='locked' ORDER BY id DESC LIMIT 1) FROM props WHERE id=?`, id).Scan(&shop, &card.Name, &card.Description, &size, &card.Archived, &cover, &file)
	if err != nil {
		if errorsNoRows(err) {
			return propCard{}, nil
		}
		if strings.Contains(err.Error(), "no such table") {
			return propCard{}, nil
		}
		return propCard{}, err
	}
	card.ID = id
	card.Found = true
	if shop.Valid {
		value := shop.Int64
		card.ShopID = &value
	}
	if cover.Valid {
		value := cover.Int64
		card.CoverID = &value
	}
	if file.Valid {
		card.File = file.String
	}
	if strings.TrimSpace(size) != "" {
		_ = json.Unmarshal([]byte(size), &card.Size)
	}
	return card, nil
}
func errorsNoRows(err error) bool {
	return err == sql.ErrNoRows || (err != nil && strings.Contains(err.Error(), "no rows"))
}
func (module *Module) propVisible(user core.User, card propCard) bool {
	return card.Found && (card.ShopID == nil || module.CanShop(user, *card.ShopID))
}
func (module *Module) validateProps(user core.User, layout Layout) error {
	for _, element := range layout.Elements {
		if element.Type != "prop" {
			continue
		}
		card, err := module.loadProp(context.Background(), element.PropID)
		if err != nil {
			return err
		}
		if !module.propVisible(user, card) {
			return invalid(404, "道具不存在")
		}
		if card.Archived {
			return invalid(409, "道具已归档，不能放进场景")
		}
	}
	return nil
}

type sheetItem struct {
	img image.Image
	ink color.NRGBA
}

// propSheet 把多张道具参考图拼成一张：网格排列，每格加一圈布局示意图里同色的粗边框。
func propSheet(items []sheetItem, size string) *image.NRGBA {
	width, height, _ := parseImageSize(size)
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.NRGBA{R: 250, G: 250, B: 250, A: 255}), image.Point{}, draw.Src)
	cols := int(math.Ceil(math.Sqrt(float64(len(items)))))
	rows := (len(items) + cols - 1) / cols
	cellW, cellH := width/cols, height/rows
	const gap, border = 16, 12
	for index, item := range items {
		cell := image.Rect((index%cols)*cellW, (index/cols)*cellH, (index%cols+1)*cellW, (index/cols+1)*cellH).Inset(gap)
		draw.Draw(img, cell, image.NewUniform(item.ink), image.Point{}, draw.Src)
		inner := cell.Inset(border)
		draw.Draw(img, inner, image.NewUniform(color.NRGBA{R: 250, G: 250, B: 250, A: 255}), image.Point{}, draw.Src)
		src := item.img.Bounds()
		scale := math.Min(float64(inner.Dx())/float64(src.Dx()), float64(inner.Dy())/float64(src.Dy()))
		w, h := int(float64(src.Dx())*scale), int(float64(src.Dy())*scale)
		x, y := inner.Min.X+(inner.Dx()-w)/2, inner.Min.Y+(inner.Dy()-h)/2
		draw.CatmullRom.Scale(img, image.Rect(x, y, x+w, y+h), item.img, src, draw.Over, nil)
	}
	return img
}
