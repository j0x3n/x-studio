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

func New(app *core.Core) *Module                 { return &Module{app} }
func (*Module) Name() string                     { return "scenes" }
func (*Module) Jobs() map[string]core.JobHandler { return nil }
func (*Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE scenes(id INTEGER PRIMARY KEY,shop_id INTEGER REFERENCES shops(id),name TEXT NOT NULL,categories TEXT NOT NULL DEFAULT '[]',still TEXT NOT NULL DEFAULT '',wear TEXT NOT NULL DEFAULT '',light TEXT NOT NULL DEFAULT '',props TEXT NOT NULL DEFAULT '',note TEXT NOT NULL DEFAULT '',builtin_key TEXT UNIQUE,archived INTEGER NOT NULL DEFAULT 0,created_by INTEGER NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE scene_refs(id INTEGER PRIMARY KEY,scene_id INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,use TEXT NOT NULL CHECK(use IN ('still','wear')),file TEXT NOT NULL,width INTEGER NOT NULL,height INTEGER NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE scene_bindings(target_type TEXT NOT NULL CHECK(target_type IN ('shop','listing')),target_id INTEGER NOT NULL,scene_id INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,updated_by INTEGER NOT NULL,updated_at TEXT NOT NULL,PRIMARY KEY(target_type,target_id));
CREATE INDEX scenes_shop ON scenes(shop_id,archived);
CREATE INDEX scene_refs_scene ON scene_refs(scene_id);
CREATE INDEX scene_bindings_scene ON scene_bindings(scene_id);`)
		return err
	}}}
}

type Scene struct {
	ID         int64       `json:"id"`
	ShopID     *int64      `json:"shop_id"`
	Name       string      `json:"name"`
	Categories []string    `json:"categories"`
	Still      string      `json:"still"`
	Wear       string      `json:"wear"`
	Light      string      `json:"light"`
	Props      string      `json:"props"`
	Note       string      `json:"note"`
	BuiltinKey *string     `json:"builtin_key"`
	IsDefault  bool        `json:"is_default"`
	Archived   bool        `json:"archived"`
	Refs       []Reference `json:"refs"`
	Usage      Usage       `json:"usage"`
	CreatedAt  string      `json:"created_at"`
	UpdatedAt  string      `json:"updated_at"`
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
	ID    int64         `json:"id"`
	Name  string        `json:"name"`
	Still string        `json:"still"`
	Wear  string        `json:"wear"`
	Light string        `json:"light"`
	Props string        `json:"props"`
	Refs  []SnapshotRef `json:"refs"`
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

const columns = `id,shop_id,name,categories,still,wear,light,props,note,builtin_key,archived,created_at,updated_at,EXISTS(SELECT 1 FROM settings WHERE key='scenes.default' AND CAST(value AS INTEGER)=scenes.id)`

func scan(scanner core.Scanner) (scene Scene, err error) {
	var categories string
	err = scanner.Scan(&scene.ID, &scene.ShopID, &scene.Name, &categories, &scene.Still, &scene.Wear, &scene.Light, &scene.Props, &scene.Note, &scene.BuiltinKey, &scene.Archived, &scene.CreatedAt, &scene.UpdatedAt, &scene.IsDefault)
	if err == nil {
		err = json.Unmarshal([]byte(categories), &scene.Categories)
	}
	scene.Refs = []Reference{}
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
	return nil
}
func refURL(sceneID, refID int64) string {
	return fmt.Sprintf("/api/scenes/%d/refs/%d/file", sceneID, refID)
}
