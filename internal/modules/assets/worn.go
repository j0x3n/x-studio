package assets

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"xstudio/internal/core"
	"xstudio/internal/media"
)

type WornRequest struct {
	UserID       int64
	ModelID      int64
	ProductID    int64
	SourceAssets []int64
	BodyPart     string
	Framing      string
	Size         string
	Prompt       string
	StyleRefs    []image.Image
}

type WornError struct {
	Status  int
	Message string
}

func (e *WornError) Error() string               { return e.Message }
func wornError(status int, message string) error { return &WornError{status, message} }

func GenerateWorn(ctx context.Context, c *core.Core, req WornRequest) ([]byte, core.Object, error) {
	a := New(c)
	validate := func() (core.User, ShopModel, error) {
		u, err := c.User(req.UserID)
		if err != nil || u.Disabled || u.Role.Scope == "shares" || !u.Has(core.ModelView) || !u.Has(core.AssetProcess) {
			return u, ShopModel{}, wornError(403, "没有模特或图片处理权限")
		}
		if !c.CanProduct(u, req.ProductID) {
			return u, ShopModel{}, wornError(404, "商品不存在")
		}
		m, err := a.getModel(u, req.ModelID, true)
		if err != nil {
			return u, m, wornError(404, "模特不存在或已归档")
		}
		var count int
		err = c.DB.QueryRowContext(ctx, "SELECT count(*) FROM listings WHERE product_id=? AND shop_id=?", req.ProductID, m.ShopID).Scan(&count)
		if err != nil || count == 0 {
			return u, m, wornError(404, "商品不属于模特店铺")
		}
		return u, m, nil
	}
	_, m, err := validate()
	if err != nil {
		return nil, nil, err
	}
	if !validBodyPart(req.BodyPart) || (req.Framing != "closeup" && req.Framing != "half_body") || (req.Size != "1024x1024" && req.Size != "1024x1536" && req.Size != "1536x1024") || len(req.SourceAssets) < 1 || len(req.SourceAssets) > 2 {
		return nil, nil, wornError(400, "商品参考、身体部位或画幅不正确")
	}
	refs := []image.Image{}
	seen := map[int64]bool{}
	for _, id := range req.SourceAssets {
		ph, e := a.asset(id)
		if e != nil || ph.ProductID != req.ProductID || ph.Archived || seen[id] {
			return nil, nil, wornError(404, "商品参考图不存在")
		}
		if media.AIMade(ph.Photo) {
			return nil, nil, wornError(400, "商品参考必须是实拍或抠图")
		}
		seen[id] = true
		if ph.EnhancedFile != "" {
			ph.File = ph.EnhancedFile
		}
		im, e := a.sourceImage(workbenchSource{input: workbenchInput{AssetID: &id}, asset: ph}, req.UserID)
		if e != nil {
			return nil, nil, e
		}
		refs = append(refs, im)
	}
	var rid int64
	err = c.DB.QueryRowContext(ctx, `SELECT id FROM model_references WHERE model_id=? AND deleted_at='' ORDER BY (body_part=?) DESC,(id=?) DESC,id LIMIT 1`, m.ID, req.BodyPart, m.CoverReferenceID).Scan(&rid)
	if err != nil {
		return nil, nil, wornError(400, "模特还没有固定参考图")
	}
	ref, err := a.referenceImage(m.ID, rid)
	if err != nil {
		return nil, nil, err
	}
	refs = append(refs, ref)
	for _, style := range req.StyleRefs {
		if len(refs) >= 3 {
			break
		}
		if style == nil {
			return nil, nil, wornError(400, "风格参考图不正确")
		}
		refs = append(refs, style)
	}
	if _, _, err = validate(); err != nil {
		return nil, nil, err
	}
	actor := core.Actor(ctx)
	actor.UserID = req.UserID
	ctx = core.WithAIActor(ctx, actor)
	data, err := c.EditImageSize(ctx, req.Prompt, refs, req.Size)
	if err != nil {
		return nil, nil, err
	}
	if _, _, err = validate(); err != nil {
		return nil, nil, err
	}
	for _, id := range req.SourceAssets {
		ph, e := a.asset(id)
		if e != nil || ph.ProductID != req.ProductID || ph.Archived || media.AIMade(ph.Photo) {
			return nil, nil, wornError(404, "商品参考图已变化或不存在")
		}
	}
	if _, err = a.reference(m.ID, rid); err != nil {
		return nil, nil, wornError(404, "模特参考图不存在")
	}
	op := core.Object{"source_asset_ids": req.SourceAssets, "model_id": m.ID, "model_ref_ids": []int64{rid}, "model": c.Model(core.Actor(ctx).Feature, c.Cfg.ImageModel), "size": req.Size, "body_part": req.BodyPart, "framing": req.Framing}
	return data, op, nil
}

type OutputRequest struct {
	ProductID int64
	ParentID  int64
	PNG       []byte
	Source    string
	Title     string
	Tags      []string
	Op        core.Object
	UserID    int64
}

func SaveOutput(ctx context.Context, tx *sql.Tx, c *core.Core, req OutputRequest) (int64, error) {
	u, err := core.ScanUser(tx.QueryRowContext(ctx, core.UserQuery+" WHERE u.id=?", req.UserID))
	if err != nil || u.Disabled || u.Role.Scope == "shares" || !u.Has(core.CreativeRun) {
		return 0, wornError(http.StatusForbidden, "没有 AI 出图权限")
	}
	var allowed int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM products p WHERE p.id=? AND (? OR p.created_by=? OR EXISTS(SELECT 1 FROM listings l JOIN user_shops us ON us.shop_id=l.shop_id WHERE l.product_id=p.id AND us.user_id=?))`, req.ProductID, u.Role.Key == "admin", u.ID, u.ID).Scan(&allowed)
	if err != nil {
		return 0, err
	}
	if allowed == 0 {
		return 0, wornError(404, "商品不存在")
	}
	if err = validateWornOutputTx(ctx, tx, u, req); err != nil {
		return 0, err
	}
	if req.Source != "ai_generated" && req.Source != "ai_edited" {
		return 0, wornError(400, "成品来源不正确")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(req.PNG))
	if err != nil || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
		return 0, wornError(400, "成品不是有效 PNG")
	}
	if _, err = png.Decode(bytes.NewReader(req.PNG)); err != nil {
		return 0, wornError(400, "成品 PNG 已损坏")
	}
	var parent any
	if req.ParentID != 0 {
		var pid int64
		if err = tx.QueryRowContext(ctx, "SELECT product_id FROM assets WHERE id=?", req.ParentID).Scan(&pid); err != nil || pid != req.ProductID {
			return 0, wornError(404, "主要参考图不存在")
		}
		parent = req.ParentID
	}
	found := false
	for _, tag := range req.Tags {
		if tag == "AI出图" {
			found = true
		}
	}
	if !found {
		req.Tags = append(req.Tags, "AI出图")
	}
	name := fmt.Sprintf("原图/AI出图/%x.png", randomOutputID())
	if err = os.MkdirAll(filepath.Join(c.Dir(req.ProductID), "原图", "AI出图"), 0700); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(c.Dir(req.ProductID))
	if err != nil {
		return 0, err
	}
	defer root.Close()
	if err = root.WriteFile(name, req.PNG, 0600); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO assets(product_id,parent_id,file,orig_name,width,height,source,kind,tags,op,created_by,created_at,updated_at,title,ai_generated) VALUES(?,?,?,?,?,?,?,'output',?,?,?,?,?,?,1)`, req.ProductID, parent, name, req.Title, cfg.Width, cfg.Height, req.Source, core.JS(req.Tags), core.JS(req.Op), req.UserID, core.Now(), core.Now(), req.Title)
	if err != nil {
		_ = root.Remove(name)
		return 0, err
	}
	id, err := result.LastInsertId()
	if err == nil {
		request, _ := http.NewRequestWithContext(ctx, "POST", "http://localhost/", nil)
		err = core.AuditTx(tx, request, "assets.creative_output", "asset", id, req.Op)
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE audit_log SET user_id=? WHERE id=last_insert_rowid()", req.UserID)
		}
	}
	return id, err
}

func randomOutputID() []byte {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		panic(err)
	}
	return token
}

func validateWornOutputTx(ctx context.Context, tx *sql.Tx, u core.User, req OutputRequest) error {
	op := req.Op
	if nested, ok := op["worn"].(map[string]any); ok {
		op = nested
	}
	var worn struct {
		ModelID      int64   `json:"model_id"`
		ReferenceIDs []int64 `json:"model_ref_ids"`
		SourceIDs    []int64 `json:"source_asset_ids"`
	}
	if err := json.Unmarshal([]byte(core.JS(op)), &worn); err != nil {
		return wornError(400, "模特来源记录不正确")
	}
	if worn.ModelID == 0 {
		return nil
	}
	if !u.Has(core.ModelView) || !u.Has(core.AssetProcess) {
		return wornError(403, "没有模特或图片处理权限")
	}
	var count int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM shop_models m JOIN shops s ON s.id=m.shop_id WHERE m.id=? AND m.archived=0 AND s.archived=0 AND EXISTS(SELECT 1 FROM listings l WHERE l.shop_id=m.shop_id AND l.product_id=?) AND (? OR EXISTS(SELECT 1 FROM user_shops us WHERE us.shop_id=m.shop_id AND us.user_id=?))`, worn.ModelID, req.ProductID, u.Role.Key == "admin", u.ID).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return wornError(404, "模特不存在或已归档")
	}
	for _, id := range worn.ReferenceIDs {
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM model_references WHERE model_id=? AND id=? AND deleted_at=''", worn.ModelID, id).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return wornError(404, "模特参考图不存在")
		}
	}
	for _, id := range worn.SourceIDs {
		ph, err := core.ScanPhoto(tx.QueryRowContext(ctx, "SELECT "+core.PhotoColumns+" FROM assets WHERE id=? AND product_id=? AND archived=0", id, req.ProductID))
		if err != nil || media.AIMade(ph) {
			return wornError(404, "商品参考图已变化或不存在")
		}
	}
	return nil
}
