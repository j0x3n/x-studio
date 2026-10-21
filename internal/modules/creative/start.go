package creative

import (
	"context"
	"slices"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/modules/scenes"
)

type StartResult struct {
	SetID   int64
	JobID   int64
	Missing []Missing
}

type missingError struct {
	Items []Missing
}

func (e *missingError) Error() string {
	messages := make([]string, 0, len(e.Items))
	for _, item := range e.Items {
		messages = append(messages, item.Label+"："+item.Reason)
	}
	return strings.Join(messages, "；")
}

type createDetail struct {
	Parts  []string `json:"parts"`
	Params Params   `json:"params"`
	Source string   `json:"source,omitempty"`
}

func StartSet(ctx context.Context, c *core.Core, listingID, userID int64, parts []string, params Params) (StartResult, error) {
	return New(c).startSet(ctx, listingID, userID, parts, params, "pipeline")
}

func (a *Module) startSet(ctx context.Context, listingID, userID int64, parts []string, params Params, source string) (StartResult, error) {
	if len(parts) == 0 || len(parts) > 2 {
		return StartResult{}, problem(400, "请选择 main 或 detail")
	}
	seen := map[string]bool{}
	for _, part := range parts {
		if !slices.Contains([]string{"main", "detail"}, part) || seen[part] {
			return StartResult{}, problem(400, "出图部分不正确或重复")
		}
		seen[part] = true
	}
	if params.SceneID < 0 {
		return StartResult{}, problem(400, "场景编号不正确")
	}
	if len(params.Sizes) > 40 || len([]rune(params.Note)) > 4000 {
		return StartResult{}, problem(400, "尺寸数量或补充要求过长")
	}
	for _, size := range params.Sizes {
		if size.MM <= 0 {
			return StartResult{}, problem(400, "尺寸必须大于 0")
		}
	}
	product, err := a.PipelineProduct(listingID)
	if err != nil {
		return StartResult{}, err
	}
	if _, err = a.authorize(ctx, userID, listingID, core.CreativeRun); err != nil {
		return StartResult{}, err
	}
	scene, err := scenes.Resolve(ctx, a.DB, listingID, params.SceneID)
	if err != nil {
		return StartResult{}, problem(400, err.Error())
	}
	if source == "pipeline" {
		// 一键出图重复跑时，上一套还在生成就不再开新的一套
		var active int
		if err = a.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs j JOIN creative_sets s ON j.target_type='creative_set' AND j.target_id=s.id WHERE s.listing_id=? AND j.status IN ('queued','running')`, listingID).Scan(&active); err != nil {
			return StartResult{}, err
		}
		if active > 0 {
			return StartResult{}, problem(409, "这款已有一套 AI 出图在生成，等它跑完再出图")
		}
	}
	templates, err := a.selectTemplates(ctx, product, parts, params)
	if err != nil {
		return StartResult{}, err
	}
	keys := map[string]string{}
	for _, template := range templates {
		keys[template.Slot] = template.Key
	}
	allowance := int(number(a.settings()["max_images_per_listing"]))
	planned := planItems(0, listingID, userID, templates, params, product)
	for index := range planned {
		planned[index].SceneSnapshot = scene
	}
	missing, err := a.preflight(ctx, product, planned)
	if err != nil {
		return StartResult{}, err
	}
	if len(missing) > 0 && (!params.SkipMissing || len(missing) == len(planned)) {
		return StartResult{}, &missingError{Items: missing}
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return StartResult{}, err
	}
	defer tx.Rollback()
	if err = authorizeTx(ctx, tx, userID, listingID, core.CreativeRun); err != nil {
		return StartResult{}, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT coalesce(max(version),0)+1 FROM creative_sets WHERE listing_id=?", listingID).Scan(&version); err != nil {
		return StartResult{}, err
	}
	inserted, err := tx.ExecContext(ctx, `INSERT INTO creative_sets(listing_id,version,status,template_keys,params,parts,images_allowance,created_by,created_at,updated_at) VALUES(?,?,'generating',?,?,?,?,?,?,?)`, listingID, version, core.JS(keys), core.JS(params), core.JS(parts), allowance, userID, core.Now(), core.Now())
	if err != nil {
		return StartResult{}, err
	}
	id, _ := inserted.LastInsertId()
	for _, item := range planned {
		item.SetID = &id
		itemID, err := insertItem(ctx, tx, item)
		if err != nil {
			return StartResult{}, err
		}
		if item.Variant == "B" {
			if _, err = tx.ExecContext(ctx, "UPDATE creative_sets SET hero_b_item_id=? WHERE id=?", itemID, id); err != nil {
				return StartResult{}, err
			}
		}
	}
	kind := "creative.detail"
	if contains(parts, "main") {
		kind = "creative.main"
	}
	jobID, err := enqueueTx(ctx, tx, kind, "creative_set", id, userID, core.Object{})
	if err != nil {
		return StartResult{}, err
	}
	if err = auditTx(ctx, tx, userID, "creative.create", "creative_set", id, createDetail{Parts: parts, Params: params, Source: source}); err != nil {
		return StartResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return StartResult{}, err
	}
	a.Wake()
	return StartResult{SetID: id, JobID: jobID, Missing: missing}, nil
}
