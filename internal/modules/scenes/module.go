package scenes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
)

type Module struct{ *core.Core }

func New(app *core.Core) *Module { return &Module{app} }
func (*Module) Name() string     { return "scenes" }
func (module *Module) Jobs() map[string]core.JobHandler {
	return map[string]core.JobHandler{"scenes.plate": module.runPlate}
}
func (*Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE scenes(id INTEGER PRIMARY KEY,shop_id INTEGER REFERENCES shops(id),name TEXT NOT NULL,categories TEXT NOT NULL DEFAULT '[]',still TEXT NOT NULL DEFAULT '',wear TEXT NOT NULL DEFAULT '',light TEXT NOT NULL DEFAULT '',props TEXT NOT NULL DEFAULT '',note TEXT NOT NULL DEFAULT '',builtin_key TEXT UNIQUE,archived INTEGER NOT NULL DEFAULT 0,created_by INTEGER NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE scene_refs(id INTEGER PRIMARY KEY,scene_id INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,use TEXT NOT NULL CHECK(use IN ('still','wear')),file TEXT NOT NULL,width INTEGER NOT NULL,height INTEGER NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE scene_bindings(target_type TEXT NOT NULL CHECK(target_type IN ('shop','listing')),target_id INTEGER NOT NULL,scene_id INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,updated_by INTEGER NOT NULL,updated_at TEXT NOT NULL,PRIMARY KEY(target_type,target_id));
CREATE INDEX scenes_shop ON scenes(shop_id,archived);
CREATE INDEX scene_refs_scene ON scene_refs(scene_id);
CREATE INDEX scene_bindings_scene ON scene_bindings(scene_id);`)
		return err
	}}, {Version: 2, Up: func(tx *sql.Tx) error {
		if err := core.AddColumns(tx, "scenes", [][2]string{
			{"camera", "TEXT NOT NULL DEFAULT ''"},
			{"brief", "TEXT NOT NULL DEFAULT ''"},
			{"layout", `TEXT NOT NULL DEFAULT '{"size":"1024x1536","elements":[]}'`},
			{"plate_id", "INTEGER"},
		}); err != nil {
			return err
		}
		_, err := tx.Exec(`CREATE TABLE scene_plates(id INTEGER PRIMARY KEY,scene_id INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,status TEXT NOT NULL CHECK(status IN ('candidate','locked','discarded')),file TEXT NOT NULL,width INTEGER NOT NULL,height INTEGER NOT NULL,size TEXT NOT NULL,layout_snapshot TEXT NOT NULL,prompt TEXT NOT NULL DEFAULT '',model TEXT NOT NULL DEFAULT '',sketch_file TEXT NOT NULL DEFAULT '',created_by INTEGER NOT NULL,created_at TEXT NOT NULL);
CREATE INDEX scene_plates_scene ON scene_plates(scene_id,status);
CREATE UNIQUE INDEX scene_plate_one_locked ON scene_plates(scene_id) WHERE status='locked';`)
		return err
	}}}
}

type Scene struct {
	ID            int64       `json:"id"`
	ShopID        *int64      `json:"shop_id"`
	Name          string      `json:"name"`
	Categories    []string    `json:"categories"`
	Still         string      `json:"still"`
	Wear          string      `json:"wear"`
	Light         string      `json:"light"`
	Props         string      `json:"props"`
	Note          string      `json:"note"`
	Camera        string      `json:"camera"`
	Brief         string      `json:"brief"`
	Layout        Layout      `json:"layout"`
	PlateID       *int64      `json:"plate_id,omitempty"`
	ThumbURL      string      `json:"thumb_url"`
	Plate         *Plate      `json:"plate"`
	Plates        []Plate     `json:"plates"`
	PlateOutdated bool        `json:"plate_outdated"`
	BuiltinKey    *string     `json:"builtin_key"`
	IsDefault     bool        `json:"is_default"`
	Archived      bool        `json:"archived"`
	Refs          []Reference `json:"refs"`
	Usage         Usage       `json:"usage"`
	CreatedAt     string      `json:"created_at"`
	UpdatedAt     string      `json:"updated_at"`
}
type Usage struct {
	Listings int `json:"listings"`
	Shops    int `json:"shops"`
}
type Reference struct {
	ID     int64  `json:"id"`
	Use    string `json:"use"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	File   string `json:"-"`
}
type Snapshot struct {
	ID    int64          `json:"id"`
	Name  string         `json:"name"`
	Still string         `json:"still"`
	Wear  string         `json:"wear"`
	Light string         `json:"light"`
	Props string         `json:"props"`
	Refs  []SnapshotRef  `json:"refs"`
	Plate *SnapshotPlate `json:"plate,omitempty"`
}
type SnapshotPlate struct {
	Path   string      `json:"path"`
	Size   string      `json:"size"`
	Width  int         `json:"width"`
	Height int         `json:"height"`
	Box    *[4]float64 `json:"box,omitempty"`
}
type SnapshotRef struct {
	Use  string `json:"use"`
	Path string `json:"path"`
}
type sceneError struct {
	status  int
	message string
}

func (problem *sceneError) Error() string   { return problem.message }
func invalid(status int, text string) error { return &sceneError{status, text} }
func fail(writer http.ResponseWriter, err error) {
	var problem *sceneError
	if errors.As(err, &problem) {
		core.Fail(writer, problem.status, problem.message)
		return
	}
	core.Fail(writer, 500, "场景处理失败，请稍后重试")
}

const columns = `id,shop_id,name,categories,still,wear,light,props,note,camera,brief,layout,plate_id,builtin_key,archived,created_at,updated_at,EXISTS(SELECT 1 FROM settings WHERE key='scenes.default' AND CAST(value AS INTEGER)=scenes.id)`

func scan(scanner core.Scanner) (scene Scene, err error) {
	var categories, layoutText string
	var plateID sql.NullInt64
	err = scanner.Scan(&scene.ID, &scene.ShopID, &scene.Name, &categories, &scene.Still, &scene.Wear, &scene.Light, &scene.Props, &scene.Note, &scene.Camera, &scene.Brief, &layoutText, &plateID, &scene.BuiltinKey, &scene.Archived, &scene.CreatedAt, &scene.UpdatedAt, &scene.IsDefault)
	if err == nil {
		err = json.Unmarshal([]byte(categories), &scene.Categories)
	}
	if err == nil {
		if strings.TrimSpace(layoutText) == "" {
			layoutText = `{"size":"1024x1536","elements":[]}`
		}
		err = json.Unmarshal([]byte(layoutText), &scene.Layout)
	}
	if plateID.Valid {
		id := plateID.Int64
		scene.PlateID = &id
	}
	if scene.Layout.Elements == nil {
		scene.Layout.Elements = []LayoutElement{}
	}
	scene.Refs = []Reference{}
	scene.Plates = []Plate{}
	return
}
func get(ctx context.Context, db *sql.DB, id int64) (Scene, error) {
	scene, err := scan(db.QueryRowContext(ctx, "SELECT "+columns+" FROM scenes WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		err = invalid(404, "场景不存在")
	}
	return scene, err
}
func matches(scene Scene, shopID int64, category string) error {
	if scene.Archived {
		return invalid(400, "场景已归档，请换一个场景")
	}
	if scene.ShopID != nil && *scene.ShopID != shopID {
		return invalid(400, "场景不属于这个店铺")
	}
	if category != "" && len(scene.Categories) > 0 && !slices.Contains(scene.Categories, category) {
		return invalid(400, "场景不适用于这款商品的品类")
	}
	return nil
}
func listing(ctx context.Context, db *sql.DB, id int64) (int64, string, error) {
	var shopID int64
	var category string
	err := db.QueryRowContext(ctx, `SELECT l.shop_id,c.key FROM listings l JOIN products p ON p.id=l.product_id JOIN categories c ON c.id=p.category_id WHERE l.id=?`, id).Scan(&shopID, &category)
	if errors.Is(err, sql.ErrNoRows) {
		err = invalid(404, "上架版本不存在")
	}
	return shopID, category, err
}
func resolve(ctx context.Context, db *sql.DB, listingID, overrideID int64) (Snapshot, string, error) {
	var exists int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='scenes'").Scan(&exists); err != nil {
		return Snapshot{}, "none", err
	}
	if exists == 0 && overrideID == 0 {
		return Snapshot{}, "none", nil
	}
	if exists == 0 {
		return Snapshot{}, "none", invalid(404, "场景不存在")
	}
	shopID, category, err := listing(ctx, db, listingID)
	if err != nil {
		return Snapshot{}, "none", err
	}
	type candidate struct {
		id     int64
		source string
	}
	choices := []candidate{}
	if overrideID != 0 {
		choices = append(choices, candidate{overrideID, "listing"})
	} else {
		for _, target := range []struct {
			kind string
			id   int64
		}{{"listing", listingID}, {"shop", shopID}} {
			var id int64
			err = db.QueryRowContext(ctx, "SELECT scene_id FROM scene_bindings WHERE target_type=? AND target_id=?", target.kind, target.id).Scan(&id)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return Snapshot{}, "none", err
			}
			if err == nil {
				choices = append(choices, candidate{id, target.kind})
			}
		}
		var id int64
		err = db.QueryRowContext(ctx, "SELECT CAST(value AS INTEGER) FROM settings WHERE key='scenes.default'").Scan(&id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Snapshot{}, "none", err
		}
		if err == nil {
			choices = append(choices, candidate{id, "default"})
		}
	}
	for _, choice := range choices {
		scene, err := get(ctx, db, choice.id)
		if err == nil {
			err = matches(scene, shopID, category)
		}
		if err != nil {
			if overrideID != 0 {
				return Snapshot{}, "none", err
			}
			var problem *sceneError
			if errors.As(err, &problem) {
				continue
			}
			return Snapshot{}, "none", err
		}
		out := Snapshot{ID: scene.ID, Name: scene.Name, Still: scene.Still, Wear: scene.Wear, Light: scene.Light, Props: scene.Props, Refs: []SnapshotRef{}}
		rows, err := db.QueryContext(ctx, "SELECT use,file FROM scene_refs WHERE scene_id=? ORDER BY id", scene.ID)
		if err != nil {
			return Snapshot{}, "none", err
		}
		for rows.Next() {
			var ref SnapshotRef
			if err = rows.Scan(&ref.Use, &ref.Path); err != nil {
				break
			}
			ref.Path, err = filepath.Abs(ref.Path)
			if err != nil {
				break
			}
			out.Refs = append(out.Refs, ref)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return out, choice.source, err
		}
		out.Plate, err = plateSnapshot(ctx, db, scene)
		return out, choice.source, err
	}
	return Snapshot{}, "none", nil
}
func Resolve(ctx context.Context, db *sql.DB, listingID, overrideID int64) (Snapshot, error) {
	scene, _, err := resolve(ctx, db, listingID, overrideID)
	return scene, err
}
func validate(scene *Scene) error {
	scene.Name = strings.TrimSpace(scene.Name)
	if utf8.RuneCountInString(scene.Name) < 1 || utf8.RuneCountInString(scene.Name) > 40 {
		return invalid(400, "场景名称要填 1–40 字")
	}
	if strings.TrimSpace(scene.Still) == "" && strings.TrimSpace(scene.Wear) == "" {
		return invalid(400, "静物场景和上身穿搭至少填一项")
	}
	for _, field := range []string{scene.Still, scene.Wear, scene.Light, scene.Props} {
		if utf8.RuneCountInString(field) > 600 {
			return invalid(400, "静物、穿搭、光线和道具各不能超过 600 字")
		}
	}
	if utf8.RuneCountInString(scene.Note) > 300 {
		return invalid(400, "说明不能超过 300 字")
	}
	if scene.Categories == nil {
		scene.Categories = []string{}
	}
	scene.Camera = strings.TrimSpace(scene.Camera)
	scene.Brief = strings.TrimSpace(scene.Brief)
	if !validCamera(scene.Camera) {
		return invalid(400, "机位请选择顶拍、俯拍 45°、平视或微距")
	}
	if utf8.RuneCountInString(scene.Brief) > 1000 {
		return invalid(400, "摆放说明不能超过 1000 字")
	}
	return normalizeLayout(&scene.Layout)
}
func refURL(sceneID, refID int64) string {
	return fmt.Sprintf("/api/scenes/%d/refs/%d/file", sceneID, refID)
}
