package studio

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

type listingRenderRow struct {
	ListingID        int64
	DetailTemplate   string
	MainTemplate     string
	AILabel          bool
	ReviewStatus     string
	ReviewNote       string
	ReviewedBy       sql.NullInt64
	ReviewedAt       string
	RenderedJobID    sql.NullInt64
	RenderedTemplate string
	RenderNote       string
	UpdatedAt        string
}

func (a *Module) ensureListingRender(listingID int64) (listingRenderRow, error) {
	var aiLabel int
	var updatedAt string
	if e := a.DB.QueryRow("SELECT ai_label,updated_at FROM listings WHERE id=?", listingID).Scan(&aiLabel, &updatedAt); e != nil {
		return listingRenderRow{}, e
	}
	_, _ = a.DB.Exec(
		"INSERT OR IGNORE INTO listing_render(listing_id,detail_template,main_template,ai_label,review_status,updated_at) VALUES(?, '', 'default', ?, 'none', ?)",
		listingID, aiLabel, updatedAt,
	)
	var lr listingRenderRow
	var aiInt int
	e := a.DB.QueryRow(
		"SELECT listing_id,detail_template,main_template,ai_label,review_status,review_note,reviewed_by,reviewed_at,rendered_job_id,rendered_template,render_note,updated_at FROM listing_render WHERE listing_id=?",
		listingID,
	).Scan(
		&lr.ListingID, &lr.DetailTemplate, &lr.MainTemplate, &aiInt,
		&lr.ReviewStatus, &lr.ReviewNote, &lr.ReviewedBy, &lr.ReviewedAt,
		&lr.RenderedJobID, &lr.RenderedTemplate, &lr.RenderNote, &lr.UpdatedAt,
	)
	lr.AILabel = aiInt != 0
	return lr, e
}

func (a *Module) outputs(id int64) core.Object {
	o := core.Object{"main34": []string{}, "main11": []string{}, "detail": []string{}, "white": "", "zip": ""}
	for key, sub := range map[string]string{"main34": "主图/3x4", "main11": "主图/1x1", "detail": "详情页"} {
		files, _ := filepath.Glob(filepath.Join(a.ListingDir(id), "导出", filepath.FromSlash(sub), "*.jpg"))
		urls := []string{}
		for _, f := range files {
			urls = append(urls, fmt.Sprintf("/files/listings/%d/导出/%s/%s", id, sub, filepath.Base(f)))
		}
		o[key] = urls
	}
	if _, e := os.Stat(filepath.Join(a.ListingDir(id), "导出", "白底.jpg")); e == nil {
		o["white"] = fmt.Sprintf("/files/listings/%d/导出/白底.jpg", id)
	}
	if _, e := os.Stat(filepath.Join(a.ListingDir(id), "导出.zip")); e == nil {
		o["zip"] = fmt.Sprintf("/api/listings/%d/download", id)
	}
	return o
}

func (a *Module) renderStateJSON(listingID, shopID int64) (core.Object, error) {
	lr, e := a.ensureListingRender(listingID)
	if e != nil {
		return nil, e
	}
	effectiveKey, detailFile := a.resolveListingDetailFile(listingID, shopID)
	var reviewedBy any
	if lr.ReviewedBy.Valid {
		reviewedBy = lr.ReviewedBy.Int64
	}
	var renderedJobID any
	if lr.RenderedJobID.Valid {
		renderedJobID = lr.RenderedJobID.Int64
	}
	settings := core.Object{
		"detail_template":           lr.DetailTemplate,
		"effective_detail_template": effectiveKey,
		"main_template":             lr.MainTemplate,
		"rendered_job_id":           renderedJobID,
		"rendered_template":         lr.RenderedTemplate,
		"render_note":               lr.RenderNote,
		"updated_at":                lr.UpdatedAt,
	}
	review := core.Object{
		"status": lr.ReviewStatus,
		"note":   lr.ReviewNote,
		"by":     reviewedBy,
		"at":     lr.ReviewedAt,
	}
	previewURL := fmt.Sprintf("/render/详情页模板/%s?p=%d", detailFile, listingID)
	return core.Object{
		"listing_id":        listingID,
		"settings":          settings,
		"review":            review,
		"outputs":           a.outputs(listingID),
		"preview_url":       previewURL,
		"detail_template":   lr.DetailTemplate,
		"main_template":     lr.MainTemplate,
		"review_status":     lr.ReviewStatus,
		"review_note":       lr.ReviewNote,
		"reviewed_by":       reviewedBy,
		"reviewed_at":       lr.ReviewedAt,
		"rendered_job_id":   renderedJobID,
		"rendered_template": lr.RenderedTemplate,
		"render_note":       lr.RenderNote,
	}, nil
}

func (a *Module) getListingRender(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, false) {
		return
	}
	var shopID int64
	if a.DB.QueryRow("SELECT shop_id FROM listings WHERE id=?", id).Scan(&shopID) != nil {
		core.Fail(w, 404, "上架版本不存在")
		return
	}
	out, e := a.renderStateJSON(id, shopID)
	if e != nil {
		core.Fail(w, 500, "读取出图设置失败")
		return
	}
	core.WriteJSON(w, 200, out)
}

func parseAILabelValue(raw json.RawMessage) (bool, error) {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b, nil
	}
	var n int
	if json.Unmarshal(raw, &n) == nil && (n == 0 || n == 1) {
		return n == 1, nil
	}
	return false, fmt.Errorf("值只能是 0/1 或布尔值")
}

func (a *Module) validateShopDetailTemplate(shopID int64, tpl string) bool {
	tpl = strings.TrimSpace(tpl)
	if tpl == "" {
		return true
	}
	list, manifestDef := a.readManifest()
	_, disabled := a.shopTemplatePrefs(shopID, manifestDef)
	found := false
	for _, item := range list {
		if item.Key == tpl {
			found = true
			break
		}
	}
	return found && !slices.Contains(disabled, tpl)
}

func (a *Module) putListingRender(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, false) {
		return
	}
	var shopID int64
	if a.DB.QueryRow("SELECT shop_id FROM listings WHERE id=?", id).Scan(&shopID) != nil {
		core.Fail(w, 404, "上架版本不存在")
		return
	}
	lr, e := a.ensureListingRender(id)
	if e != nil {
		core.Fail(w, 500, "读取出图设置失败")
		return
	}
	var in map[string]json.RawMessage
	if !core.Decode(w, r, &in) {
		return
	}
	for k, v := range in {
		switch k {
		case "detail_template":
			var tpl string
			if json.Unmarshal(v, &tpl) != nil {
				core.Fail(w, 400, "detail_template 必须是文本")
				return
			}
			tpl = strings.TrimSpace(tpl)
			if !a.validateShopDetailTemplate(shopID, tpl) {
				core.Fail(w, 400, "模板不存在或对该店铺已停用")
				return
			}
			lr.DetailTemplate = tpl
		case "main_template":
			var tpl string
			if json.Unmarshal(v, &tpl) != nil || (tpl != "" && tpl != "default") {
				core.Fail(w, 400, "不支持的主图模板")
				return
			}
			if tpl == "" {
				tpl = "default"
			}
			lr.MainTemplate = tpl
		default:
			core.Fail(w, 400, "不支持的出图设置字段："+k)
			return
		}
	}
	ts := core.Now()
	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "保存出图设置失败")
		return
	}
	defer tx.Rollback()
	if _, e = tx.Exec(
		"UPDATE listing_render SET detail_template=?,main_template=?,updated_at=? WHERE listing_id=?",
		lr.DetailTemplate, lr.MainTemplate, ts, id,
	); e == nil {
		e = core.AuditTx(tx, r, "studio.render_settings", "listing", id, core.Object{
			"detail_template": lr.DetailTemplate,
			"main_template":   lr.MainTemplate,
		})
	}
	if e != nil || tx.Commit() != nil {
		core.Fail(w, 500, "保存出图设置失败")
		return
	}
	out, _ := a.renderStateJSON(id, shopID)
	core.WriteJSON(w, 200, out)
}

func (a *Module) hasSharedActiveJob(productID int64) bool {
	if !a.ActiveProduct(productID) {
		return false
	}
	var n int
	_ = a.DB.QueryRow(
		"SELECT count(*) FROM jobs j JOIN listings l ON j.target_id=l.id WHERE j.target_type='listing' AND l.product_id=? AND j.status IN ('queued','running') AND (instr(j.steps,'check')>0 OR instr(j.steps,'classify')>0 OR instr(j.steps,'enhance')>0 OR instr(j.steps,'retouch')>0)",
		productID,
	).Scan(&n)
	return n > 0
}

func (a *Module) runListingRender(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, true) {
		return
	}
	p, e := a.PipelineProduct(id)
	if e != nil {
		core.Fail(w, 404, "上架版本不存在")
		return
	}
	if a.hasSharedActiveJob(p.ID) {
		core.Fail(w, 409, "此商品已有使用共用素材的任务")
		return
	}
	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "创建任务失败")
		return
	}
	defer tx.Rollback()
	ts := core.Now()
	res, e := tx.Exec(
		"INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,created_at,steps) VALUES('pipeline','listing',?,?,?,'queued',?,'render')",
		id, core.CurrentUser(r).ID, core.JS(core.Object{}), ts,
	)
	if e != nil {
		core.Fail(w, 409, "此上架版本已有正在排队或运行的任务")
		return
	}
	jid, _ := res.LastInsertId()
	_, e = tx.Exec("UPDATE listings SET pipeline_status='processing',pipeline_error='',updated_at=? WHERE id=?", ts, id)
	if e == nil {
		e = core.AuditTx(tx, r, "studio.render", "listing", id, core.Object{"job_id": jid})
	}
	if e != nil || tx.Commit() != nil {
		core.Fail(w, 500, "保存任务失败")
		return
	}
	j, _ := core.ScanJob(a.DB.QueryRow("SELECT "+core.JobColumns+" FROM jobs WHERE id=?", jid))
	a.Wake()
	core.WriteJSON(w, 202, core.Object{"job": j})
}

func (a *Module) runBatchRender(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	var in struct {
		ListingIDs     []int64 `json:"listing_ids"`
		DetailTemplate string  `json:"detail_template"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if len(in.ListingIDs) == 0 {
		core.Fail(w, 400, "请提供要出图的上架版本列表")
		return
	}
	if len(in.ListingIDs) > 50 {
		core.Fail(w, 400, "单次批量出图最多 50 个上架版本")
		return
	}
	in.DetailTemplate = strings.TrimSpace(in.DetailTemplate)
	u := core.CurrentUser(r)
	results := make([]core.Object, 0, len(in.ListingIDs))
	woke := false

	for _, lid := range in.ListingIDs {
		if lid < 1 || !a.CanListing(u, lid) {
			results = append(results, core.Object{"listing_id": lid, "skipped_reason": "上架版本不存在或无权限"})
			continue
		}
		p, e := a.PipelineProduct(lid)
		if e != nil {
			results = append(results, core.Object{"listing_id": lid, "skipped_reason": "上架版本不存在"})
			continue
		}
		if a.Active("listing", lid) || a.hasSharedActiveJob(p.ID) {
			results = append(results, core.Object{"listing_id": lid, "skipped_reason": "此上架版本正在排队或处理中"})
			continue
		}
		lr, e := a.ensureListingRender(lid)
		if e != nil {
			results = append(results, core.Object{"listing_id": lid, "skipped_reason": "读取出图设置失败"})
			continue
		}
		if in.DetailTemplate != "" && !a.validateShopDetailTemplate(p.ShopID, in.DetailTemplate) {
			results = append(results, core.Object{"listing_id": lid, "skipped_reason": "模板不存在或对该店铺已停用"})
			continue
		}
		tx, e := a.DB.Begin()
		if e != nil {
			results = append(results, core.Object{"listing_id": lid, "skipped_reason": "创建任务失败"})
			continue
		}
		ts := core.Now()
		if in.DetailTemplate != "" {
			if _, e := tx.Exec("UPDATE listing_render SET detail_template=?,updated_at=? WHERE listing_id=?", in.DetailTemplate, ts, lid); e != nil {
				_ = tx.Rollback()
				results = append(results, core.Object{"listing_id": lid, "skipped_reason": "保存出图设置失败"})
				continue
			}
		}
		res, e := tx.Exec(
			"INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,created_at,steps) VALUES('pipeline','listing',?,?,?,'queued',?,'render')",
			lid, u.ID, core.JS(core.Object{}), ts,
		)
		if e != nil {
			_ = tx.Rollback()
			results = append(results, core.Object{"listing_id": lid, "skipped_reason": "此上架版本已有正在排队或运行的任务"})
			continue
		}
		jid, _ := res.LastInsertId()
		_, e = tx.Exec("UPDATE listings SET pipeline_status='processing',pipeline_error='',updated_at=? WHERE id=?", ts, lid)
		if e == nil && in.DetailTemplate != "" {
			e = core.AuditTx(tx, r, "studio.render_settings", "listing", lid, core.Object{
				"detail_template": in.DetailTemplate,
				"main_template":   lr.MainTemplate,
			})
		}
		if e == nil {
			e = core.AuditTx(tx, r, "studio.render", "listing", lid, core.Object{"job_id": jid})
		}
		if e != nil || tx.Commit() != nil {
			_ = tx.Rollback()
			results = append(results, core.Object{"listing_id": lid, "skipped_reason": "保存任务失败"})
			continue
		}
		woke = true
		results = append(results, core.Object{"listing_id": lid, "job_id": jid})
	}
	a.Audit(r, "studio.render_batch", "listing", 0, core.Object{
		"listing_ids":     in.ListingIDs,
		"detail_template": in.DetailTemplate,
		"items":           results,
	})
	if woke {
		a.Wake()
	}
	core.WriteJSON(w, 200, core.Object{
		"items": results,
		"total": len(results),
	})
}

func (a *Module) reviewListing(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, true) {
		return
	}
	var shopID int64
	if a.DB.QueryRow("SELECT shop_id FROM listings WHERE id=?", id).Scan(&shopID) != nil {
		core.Fail(w, 404, "上架版本不存在")
		return
	}
	var in struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if in.Status != "approved" && in.Status != "rejected" {
		core.Fail(w, 400, "审核状态只能是 approved 或 rejected")
		return
	}
	lr, e := a.ensureListingRender(id)
	if e != nil {
		core.Fail(w, 500, "读取出图状态失败")
		return
	}
	_, zipErr := os.Stat(filepath.Join(a.ListingDir(id), "导出.zip"))
	if lr.ReviewStatus == "none" && !lr.RenderedJobID.Valid && zipErr != nil {
		core.Fail(w, 409, "尚未出图，无法审核")
		return
	}
	ts := core.Now()
	uid := core.CurrentUser(r).ID
	if _, e := a.DB.Exec(
		"UPDATE listing_render SET review_status=?,review_note=?,reviewed_by=?,reviewed_at=?,updated_at=? WHERE listing_id=?",
		in.Status, in.Note, uid, ts, ts, id,
	); e != nil {
		core.Fail(w, 500, "保存审核结果失败")
		return
	}
	var rjid any
	if lr.RenderedJobID.Valid {
		rjid = lr.RenderedJobID.Int64
	}
	a.Audit(r, "studio.review", "listing", id, core.Object{
		"status":          in.Status,
		"note":            in.Note,
		"rendered_job_id": rjid,
	})
	out, _ := a.renderStateJSON(id, shopID)
	core.WriteJSON(w, 200, out)
}

func (a *Module) reviewQueue(w http.ResponseWriter, r *http.Request) {
	u := core.CurrentUser(r)
	qShop := strings.TrimSpace(r.URL.Query().Get("shop_id"))
	var filterShopID int64
	if qShop != "" {
		sid, e := strconv.ParseInt(qShop, 10, 64)
		if e != nil || sid < 1 || !a.CanShop(u, sid) {
			core.PageItems(w, r, []core.Object{})
			return
		}
		filterShopID = sid
	}
	rows, e := a.DB.Query(`
		SELECT lr.listing_id, l.product_id, p.name, l.shop_id, s.name,
		       lr.detail_template, lr.main_template, lr.review_status,
		       lr.rendered_job_id, lr.rendered_template, lr.updated_at
		FROM listing_render lr
		JOIN listings l ON l.id = lr.listing_id
		JOIN products p ON p.id = l.product_id
		JOIN shops s ON s.id = l.shop_id
		WHERE lr.review_status = 'pending'
		ORDER BY lr.updated_at DESC, lr.listing_id DESC
	`)
	if e != nil {
		core.Fail(w, 500, "读取待审核队列失败")
		return
	}
	defer rows.Close()

	type rowItem struct {
		listingID, productID, shopID int64
		productName, shopName        string
		detailTpl, mainTpl           string
		reviewStatus, renderedTpl    string
		renderedJobID                sql.NullInt64
		updatedAt                    string
	}
	var rawList []rowItem
	for rows.Next() {
		var it rowItem
		if rows.Scan(
			&it.listingID, &it.productID, &it.productName, &it.shopID, &it.shopName,
			&it.detailTpl, &it.mainTpl, &it.reviewStatus,
			&it.renderedJobID, &it.renderedTpl, &it.updatedAt,
		) == nil {
			rawList = append(rawList, it)
		}
	}
	rows.Close()

	items := []core.Object{}
	for _, it := range rawList {
		if filterShopID > 0 && it.shopID != filterShopID {
			continue
		}
		if !a.CanListing(u, it.listingID) {
			continue
		}
		_, detailFile := a.resolveListingDetailFile(it.listingID, it.shopID)
		var rjid any
		if it.renderedJobID.Valid {
			rjid = it.renderedJobID.Int64
		}
		items = append(items, core.Object{
			"id":                it.listingID,
			"listing_id":        it.listingID,
			"product_id":        it.productID,
			"product_name":      it.productName,
			"shop_id":           it.shopID,
			"shop_name":         it.shopName,
			"detail_template":   it.detailTpl,
			"main_template":     it.mainTpl,
			"review_status":     it.reviewStatus,
			"rendered_job_id":   rjid,
			"rendered_template": it.renderedTpl,
			"updated_at":        it.updatedAt,
			"outputs":           a.outputs(it.listingID),
			"preview_url":       fmt.Sprintf("/render/详情页模板/%s?p=%d", detailFile, it.listingID),
		})
	}
	core.PageItems(w, r, items)
}

func (a *Module) requireReviewSetting() bool {
	var raw string
	if a.DB.QueryRow("SELECT value FROM studio_settings WHERE key='require_review'").Scan(&raw) != nil {
		return true
	}
	raw = strings.TrimSpace(raw)
	return raw != "false" && raw != "0"
}

func (a *Module) getSettings(w http.ResponseWriter, r *http.Request) {
	core.WriteJSON(w, 200, core.Object{
		"require_review": a.requireReviewSetting(),
	})
}

func (a *Module) patchSettings(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	var in map[string]json.RawMessage
	if !core.Decode(w, r, &in) {
		return
	}
	if len(in) == 0 {
		core.Fail(w, 400, "没有要更新的设置")
		return
	}
	var reqReview bool
	for k, v := range in {
		if k != "require_review" {
			core.Fail(w, 400, "不支持的出图设置项："+k)
			return
		}
		val, e := parseAILabelValue(v)
		if e != nil {
			core.Fail(w, 400, "require_review 必须是布尔值")
			return
		}
		reqReview = val
	}
	ts := core.Now()
	uid := core.CurrentUser(r).ID
	valStr := "false"
	if reqReview {
		valStr = "true"
	}
	if _, e := a.DB.Exec(
		"INSERT INTO studio_settings(key,value,updated_by,updated_at) VALUES('require_review',?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_by=excluded.updated_by,updated_at=excluded.updated_at",
		valStr, uid, ts,
	); e != nil {
		core.Fail(w, 500, "保存出图设置失败")
		return
	}
	a.Audit(r, "studio.settings", "settings", 0, core.Object{"require_review": reqReview})
	core.WriteJSON(w, 200, core.Object{"require_review": reqReview})
}
