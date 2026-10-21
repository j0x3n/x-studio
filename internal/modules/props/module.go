package props

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
)

type Module struct{ *core.Core }

func New(app *core.Core) *Module { return &Module{app} }
func (*Module) Name() string     { return "props" }
func (module *Module) Jobs() map[string]core.JobHandler {
	return map[string]core.JobHandler{
		"props.generate": module.runGenerate,
		"props.views":    module.runViews,
	}
}
func (*Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE props(id INTEGER PRIMARY KEY,shop_id INTEGER REFERENCES shops(id),name TEXT NOT NULL,kind TEXT NOT NULL CHECK(kind IN ('table','container','fabric','plant','backdrop','ornament','other')),description TEXT NOT NULL DEFAULT '',size_cm TEXT NOT NULL DEFAULT '{}',cover_ref_id INTEGER,archived INTEGER NOT NULL DEFAULT 0,created_by INTEGER NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE prop_refs(id INTEGER PRIMARY KEY,prop_id INTEGER NOT NULL REFERENCES props(id) ON DELETE CASCADE,view TEXT NOT NULL CHECK(view IN ('main','left','right','top','back','other')),source TEXT NOT NULL CHECK(source IN ('upload','ai')),status TEXT NOT NULL CHECK(status IN ('candidate','locked','discarded')),file TEXT NOT NULL,width INTEGER NOT NULL,height INTEGER NOT NULL,prompt TEXT NOT NULL DEFAULT '',model TEXT NOT NULL DEFAULT '',created_by INTEGER NOT NULL,created_at TEXT NOT NULL);
CREATE INDEX props_shop ON props(shop_id,archived);
CREATE INDEX prop_refs_prop ON prop_refs(prop_id,view,status);
CREATE UNIQUE INDEX prop_refs_one_locked_view ON prop_refs(prop_id,view) WHERE status='locked';`)
		return err
	}}}
}

type SizeCM struct {
	W float64 `json:"w,omitempty"`
	D float64 `json:"d,omitempty"`
	H float64 `json:"h,omitempty"`
}
type Prop struct {
	ID          int64          `json:"id"`
	ShopID      *int64         `json:"shop_id"`
	Name        string         `json:"name"`
	Kind        string         `json:"kind"`
	Description string         `json:"description"`
	SizeCM      SizeCM         `json:"size_cm"`
	CoverRefID  *int64         `json:"cover_ref_id"`
	CoverURL    string         `json:"cover_url"`
	LockedViews map[string]int `json:"locked_views"`
	Archived    bool           `json:"archived"`
	Refs        []Ref          `json:"refs,omitempty"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}
type Ref struct {
	ID        int64  `json:"id"`
	View      string `json:"view"`
	Source    string `json:"source"`
	Status    string `json:"status"`
	URL       string `json:"url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Prompt    string `json:"prompt,omitempty"`
	Model     string `json:"model,omitempty"`
	CreatedAt string `json:"created_at"`
	File      string `json:"-"`
}
type propError struct {
	status  int
	message string
}

func (problem *propError) Error() string    { return problem.message }
func invalid(status int, text string) error { return &propError{status, text} }
func fail(writer http.ResponseWriter, err error) {
	var problem *propError
	if errors.As(err, &problem) {
		core.Fail(writer, problem.status, problem.message)
		return
	}
	core.Fail(writer, 500, "道具处理失败，请稍后重试")
}
func busy(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique") || strings.Contains(text, "constraint")
}

var propKinds = map[string]string{
	"table": "桌面台面", "container": "盒子托盘", "fabric": "布料", "plant": "花植",
	"backdrop": "背景板", "ornament": "小摆件", "other": "其他",
}
var propViews = map[string]bool{"main": true, "left": true, "right": true, "top": true, "back": true, "other": true}
var angleNames = map[string]string{"left": "左侧", "right": "右侧", "top": "正上方", "back": "背面", "other": "另一角度"}

func kindName(kind string) string { return propKinds[kind] }
func refURL(propID, refID int64) string {
	return fmt.Sprintf("/api/props/%d/refs/%d/file", propID, refID)
}

const propColumns = `id,shop_id,name,kind,description,size_cm,cover_ref_id,archived,created_at,updated_at`

func scanProp(scanner core.Scanner) (Prop, error) {
	var prop Prop
	var size string
	err := scanner.Scan(&prop.ID, &prop.ShopID, &prop.Name, &prop.Kind, &prop.Description, &size, &prop.CoverRefID, &prop.Archived, &prop.CreatedAt, &prop.UpdatedAt)
	if err != nil {
		return prop, err
	}
	if strings.TrimSpace(size) == "" {
		size = "{}"
	}
	if err = json.Unmarshal([]byte(size), &prop.SizeCM); err != nil {
		return prop, err
	}
	prop.LockedViews = map[string]int{}
	if prop.CoverRefID != nil {
		prop.CoverURL = refURL(prop.ID, *prop.CoverRefID)
	}
	return prop, nil
}
func get(ctx context.Context, db *sql.DB, id int64) (Prop, error) {
	prop, err := scanProp(db.QueryRowContext(ctx, "SELECT "+propColumns+" FROM props WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		err = invalid(404, "道具不存在")
	}
	return prop, err
}
func validSize(size SizeCM) error {
	for _, value := range []float64{size.W, size.D, size.H} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 10000 {
			return invalid(400, "尺寸要在 0 到 10000 厘米之间")
		}
	}
	if size.W <= 0 && size.D <= 0 && size.H <= 0 {
		return invalid(400, "宽、深、高至少填一个")
	}
	return nil
}
func sizePhrase(size SizeCM) string {
	parts := []string{}
	if size.W > 0 {
		parts = append(parts, "宽 "+formatCM(size.W)+" cm")
	}
	if size.D > 0 {
		parts = append(parts, "深 "+formatCM(size.D)+" cm")
	}
	if size.H > 0 {
		parts = append(parts, "高 "+formatCM(size.H)+" cm")
	}
	if len(parts) == 0 {
		return "尺寸未填"
	}
	return strings.Join(parts, "、")
}
func formatCM(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
func (module *Module) validateProp(prop *Prop, user core.User) error {
	prop.Name = strings.TrimSpace(prop.Name)
	prop.Description = strings.TrimSpace(prop.Description)
	prop.Kind = strings.TrimSpace(prop.Kind)
	if utf8.RuneCountInString(prop.Name) < 1 || utf8.RuneCountInString(prop.Name) > 40 {
		return invalid(400, "道具名称要填 1–40 字")
	}
	if kindName(prop.Kind) == "" {
		return invalid(400, "道具种类不正确")
	}
	if utf8.RuneCountInString(prop.Description) < 1 || utf8.RuneCountInString(prop.Description) > 1000 {
		return invalid(400, "外观描述要填 1–1000 字")
	}
	if err := validSize(prop.SizeCM); err != nil {
		return err
	}
	if prop.ShopID != nil {
		if *prop.ShopID <= 0 || !module.CanShop(user, *prop.ShopID) {
			return invalid(404, "店铺不存在")
		}
		var count int
		if err := module.DB.QueryRow("SELECT count(*) FROM shops WHERE id=? AND archived=0", *prop.ShopID).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return invalid(404, "店铺不存在或已归档")
		}
	}
	return nil
}
func (module *Module) visible(user core.User, prop Prop) bool {
	return prop.ShopID == nil || module.CanShop(user, *prop.ShopID)
}
func (module *Module) scoped(writer http.ResponseWriter, request *http.Request) (Prop, bool) {
	prop, err := get(request.Context(), module.DB, core.ID(request, "id"))
	if err != nil {
		fail(writer, err)
		return prop, false
	}
	if !module.visible(core.CurrentUser(request), prop) {
		core.Fail(writer, 404, "道具不存在")
		return prop, false
	}
	return prop, true
}
func shopChanged(left, right *int64) bool {
	if (left == nil) != (right == nil) {
		return true
	}
	return left != nil && *left != *right
}
func (module *Module) propReferenced(ctx context.Context, id int64) (bool, error) {
	rows, err := module.DB.QueryContext(ctx, "SELECT layout FROM scenes")
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return false, nil
		}
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return false, err
		}
		var layout struct {
			Elements []struct {
				Type   string `json:"type"`
				PropID int64  `json:"prop_id"`
			} `json:"elements"`
		}
		if json.Unmarshal([]byte(raw), &layout) != nil {
			continue
		}
		for _, element := range layout.Elements {
			if element.Type == "prop" && element.PropID == id {
				return true, nil
			}
		}
	}
	return false, rows.Err()
}
