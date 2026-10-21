package creative

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"xstudio/internal/core"
)

func (a *Module) enrichItem(ctx context.Context, item core.Object) error {
	listing, err := a.one(ctx, `SELECT l.product_id,l.shop_id,p.name AS product_name FROM listings l JOIN products p ON p.id=l.product_id WHERE l.id=?`, item["listing_id"])
	if err != nil {
		return err
	}
	for key, value := range listing {
		item[key] = value
	}
	assetURL := func(id any) (string, error) {
		if number(id) == 0 {
			return "", nil
		}
		asset, err := a.one(ctx, "SELECT file FROM assets WHERE id=? AND product_id=?", id, item["product_id"])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("/files/products/%d/%s", int64(number(item["product_id"])), stringValue(asset["file"])), nil
	}
	item["output_url"], err = assetURL(item["output_asset_id"])
	if err != nil {
		return err
	}
	ids, err := typed[[]int64](item["source_asset_ids"])
	if err != nil {
		return err
	}
	urls := []string{}
	for _, id := range ids {
		url, err := assetURL(id)
		if err != nil {
			return err
		}
		urls = append(urls, url)
	}
	item["source_urls"] = urls
	return nil
}

func (a *Module) setObject(ctx context.Context, id int64, all bool) (core.Object, error) {
	obj, err := a.one(ctx, "SELECT * FROM creative_sets WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	query := "SELECT * FROM creative_items WHERE set_id=?"
	if !all {
		query += " AND superseded=0"
	}
	items, err := objects(ctx, a.DB, query+" ORDER BY slot,seq,variant,attempt", id)
	if err != nil {
		return nil, err
	}
	obj["items"] = items
	jobs, err := objects(ctx, a.DB, `SELECT id,kind,status,progress,message FROM jobs WHERE (target_type='creative_set' AND target_id=?) OR (target_type='creative_item' AND target_id IN (SELECT id FROM creative_items WHERE set_id=?)) ORDER BY CASE WHEN status IN ('queued','running') THEN 0 ELSE 1 END,id DESC`, id, id)
	if err != nil {
		return nil, err
	}
	recent := []core.Object{}
	finished := 0
	for _, job := range jobs {
		if job["status"] != "queued" && job["status"] != "running" {
			finished++
			if finished > 20 {
				continue
			}
		}
		recent = append(recent, job)
	}
	obj["jobs"] = recent
	complete, total := 0, 0
	for _, item := range items {
		if err = a.enrichItem(ctx, item); err != nil {
			return nil, err
		}
		if item["superseded"] == true {
			continue
		}
		total++
		if number(item["output_asset_id"]) > 0 {
			complete++
		}
	}
	obj["progress"] = core.Object{"total": total, "completed": complete}
	return obj, nil
}
func (a *Module) setsList(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !a.listingScope(w, r, id) {
		return
	}
	sets, err := objects(r.Context(), a.DB, "SELECT * FROM creative_sets WHERE listing_id=? ORDER BY version DESC", id)
	if err != nil {
		fail(w, err)
		return
	}
	for index, set := range sets {
		sets[index], err = a.setObject(r.Context(), int64(number(set["id"])), true)
		if err != nil {
			fail(w, err)
			return
		}
	}
	core.WriteJSON(w, 200, core.Object{"items": sets, "total": len(sets)})
}
func (a *Module) setGet(w http.ResponseWriter, r *http.Request) {
	set, ok := a.setScope(w, r)
	if !ok {
		return
	}
	obj, err := a.setObject(r.Context(), set.ID, true)
	if err != nil {
		fail(w, err)
		return
	}
	core.WriteJSON(w, 200, obj)
}
func (a *Module) selectTemplates(ctx context.Context, product core.Product, parts []string, params Params) ([]Template, error) {
	defaults := map[string]string{}
	shop, err := a.one(ctx, "SELECT template_set FROM creative_shop_settings WHERE shop_id=?", product.ShopID)
	if err == nil {
		defaults, _ = typed[map[string]string](shop["template_set"])
	}
	rows, err := objects(ctx, a.DB, "SELECT * FROM creative_templates WHERE enabled=1 ORDER BY key")
	if err != nil {
		return nil, err
	}
	templates, err := typed[[]Template](rows)
	if err != nil {
		return nil, err
	}
	slots := []string{}
	if contains(parts, "main") {
		slots = append(slots, "main1", "main2", "main3", "main4", "main5")
		if sku, ok := product.Listing["sku"].([]any); ok && len(sku) > 0 {
			slots = append(slots, "sku")
		}
	}
	if contains(parts, "detail") {
		for seq := 1; seq <= 9; seq++ {
			slots = append(slots, fmt.Sprintf("d%d", seq))
		}
	}
	out := []Template{}
	for _, slot := range slots {
		key := defaults[slot]
		var chosen *Template
		for _, candidate := range templates {
			if candidate.Slot != slot || (len(candidate.Categories) > 0 && !slices.Contains(candidate.Categories, product.Category.Key)) {
				continue
			}
			if key != "" && candidate.Key != key {
				continue
			}
			if key == "" {
				if slot == "main1" && candidate.Key != "main_hero_stone" {
					continue
				}
				if slot == "main4" && ((len(params.Sizes) >= 2 && !candidate.SizeCompare) || (len(params.Sizes) < 2 && candidate.ProductMode != "photo")) {
					continue
				}
				if slot == "d1" && candidate.HeroStyle != params.HeroStyle {
					continue
				}
			}
			copy := candidate
			chosen = &copy
			break
		}
		if chosen == nil {
			if slot == "d6" && key == "" {
				continue
			}
			return nil, problem(400, "缺少适用的生图模板："+slot)
		}
		out = append(out, *chosen)
	}
	return out, nil
}
func planItems(setID, lid, uid int64, templates []Template, params Params, product core.Product) []Item {
	items := []Item{}
	for _, t := range templates {
		count := t.Count
		if t.SizeCompare && len(params.Sizes) >= 2 {
			count = len(params.Sizes)
		}
		if t.Slot == "sku" {
			if sku, ok := product.Listing["sku"].([]any); ok {
				count = len(sku)
			}
		}
		count = min(count, max(1, indexedTextCount(t, product, count)))
		for seq := 1; seq <= count; seq++ {
			variants := []string{""}
			if t.Slot == "main1" {
				variants = []string{"A"}
				if params.HeroB == nil || *params.HeroB {
					variants = append(variants, "B")
				}
			}
			for _, variant := range variants {
				items = append(items, Item{SetID: &setID, ListingID: lid, Slot: t.Slot, Seq: seq, TemplateKey: t.Key, TemplateVersion: t.Version, TemplateSnapshot: t, Attempt: 1, Variant: variant, ProductMode: t.ProductMode, SizeCompare: t.SizeCompare && len(params.Sizes) >= 2, CreatedBy: uid})
			}
		}
	}
	return items
}
func (a *Module) setsCreate(w http.ResponseWriter, r *http.Request) {
	lid := core.ID(r, "id")
	if !a.listingScope(w, r, lid) {
		return
	}
	var in struct {
		Parts  []string `json:"parts"`
		Params Params   `json:"params"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	result, err := a.startSet(r.Context(), lid, core.CurrentUser(r).ID, in.Parts, in.Params, "")
	if err != nil {
		var missing *missingError
		if errors.As(err, &missing) {
			writeMissing(w, missing.Items)
			return
		}
		fail(w, err)
		return
	}
	obj, err := a.setObject(r.Context(), result.SetID, false)
	if err != nil {
		fail(w, err)
		return
	}
	job, err := a.one(r.Context(), "SELECT * FROM jobs WHERE id=?", result.JobID)
	if err != nil {
		fail(w, err)
		return
	}
	core.WriteJSON(w, 202, core.Object{"set": obj, "jobs": []core.Object{job}})
}
func (a *Module) setContinue(w http.ResponseWriter, r *http.Request) {
	set, ok := a.setScope(w, r)
	if !ok {
		return
	}
	if set.Status != "paused_limit" || a.setBusy(set.ID) {
		core.Fail(w, 409, "只有已达上限且任务结束的套装可以继续")
		return
	}
	allowance := int(number(a.settings()["max_images_per_listing"]))
	uid := core.CurrentUser(r).ID
	last, err := a.one(r.Context(), `SELECT * FROM jobs WHERE message=? AND ((target_type='creative_set' AND target_id=?) OR (target_type='creative_item' AND target_id IN(SELECT id FROM creative_items WHERE set_id=?))) ORDER BY id DESC LIMIT 1`, errLimit.Error(), set.ID, set.ID)
	kind, target, targetID, params := "creative.main", "creative_set", set.ID, core.Object{}
	if !contains(set.Parts, "main") {
		kind = "creative.detail"
	}
	if err == nil {
		kind = stringValue(last["kind"])
		target = stringValue(last["target_type"])
		targetID = int64(number(last["target_id"]))
		params, _ = typed[core.Object](last["params"])
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if err = authorizeTx(r.Context(), tx, uid, set.ListingID, core.CreativeRun); err != nil {
		fail(w, err)
		return
	}
	if _, err = tx.ExecContext(r.Context(), "UPDATE creative_sets SET images_allowance=images_allowance+?,status='generating',updated_at=? WHERE id=?", allowance, core.Now(), set.ID); err != nil {
		fail(w, err)
		return
	}
	job, err := enqueueTx(r.Context(), tx, kind, target, targetID, uid, params)
	if err != nil {
		fail(w, err)
		return
	}
	if err = auditTx(r.Context(), tx, uid, "creative.continue", "creative_set", set.ID, core.Object{"allowance_added": allowance}); err != nil {
		fail(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	a.Wake()
	core.WriteJSON(w, 202, core.Object{"job_id": job})
}
func (a *Module) setGlance(w http.ResponseWriter, r *http.Request) {
	set, ok := a.setScope(w, r)
	if !ok {
		return
	}
	if a.setBusy(set.ID) {
		core.Fail(w, 409, "请等待出图完成再整套过目")
		return
	}
	_, err := a.DB.ExecContext(r.Context(), "UPDATE creative_sets SET glanced_by=?,glanced_at=?,updated_at=? WHERE id=?", core.CurrentUser(r).ID, core.Now(), core.Now(), set.ID)
	if err != nil {
		fail(w, err)
		return
	}
	a.Audit(r, "creative.glance", "creative_set", set.ID, core.Object{})
	core.WriteJSON(w, 200, core.Object{"ok": true})
}
func (a *Module) enqueueItem(w http.ResponseWriter, r *http.Request, item Item, params core.Object) {
	if item.Superseded {
		core.Fail(w, 409, "图片已有新版本")
		return
	}
	if a.itemBusy(item) {
		core.Fail(w, 409, "套装或图片正在处理中")
		return
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	uid := core.CurrentUser(r).ID
	if err = authorizeTx(r.Context(), tx, uid, item.ListingID, core.CreativeRun); err != nil {
		fail(w, err)
		return
	}
	busy, err := itemBusyTx(r.Context(), tx, item)
	if err != nil {
		fail(w, err)
		return
	}
	if busy {
		core.Fail(w, 409, "套装或图片正在处理中")
		return
	}
	id, err := enqueueTx(r.Context(), tx, "creative.item", "creative_item", item.ID, uid, params)
	if err != nil {
		fail(w, err)
		return
	}
	if item.SetID != nil {
		if _, err = tx.ExecContext(r.Context(), "UPDATE creative_sets SET glanced_at='',glanced_by=NULL,status='generating',updated_at=? WHERE id=?", core.Now(), *item.SetID); err != nil {
			fail(w, err)
			return
		}
	}
	if err = auditTx(r.Context(), tx, uid, "creative.enqueue_item", "creative_item", item.ID, params); err != nil {
		fail(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	a.Wake()
	core.WriteJSON(w, 202, core.Object{"job_id": id})
}
func (a *Module) itemRedraw(w http.ResponseWriter, r *http.Request) {
	item, ok := a.itemScope(w, r)
	if !ok {
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if len([]rune(in.Note)) > 4000 {
		core.Fail(w, 400, "补充要求不能超过4000字")
		return
	}
	if !a.preflightItem(w, r, item) {
		return
	}
	a.enqueueItem(w, r, item, core.Object{"note": strings.TrimSpace(in.Note)})
}
func (a *Module) itemRetext(w http.ResponseWriter, r *http.Request) {
	item, ok := a.itemScope(w, r)
	if !ok {
		return
	}
	var in struct {
		Key  string `json:"key"`
		Text string `json:"text"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	found := false
	for _, text := range item.Texts {
		if text.Key == in.Key {
			found = true
		}
	}
	if !found || strings.TrimSpace(in.Text) == "" || len([]rune(in.Text)) > 4000 {
		core.Fail(w, 400, "文字 key 不存在或文字不正确")
		return
	}
	if _, err := textEditMask(item, in.Key, 1, 1); err != nil {
		fail(w, err)
		return
	}
	a.enqueueItem(w, r, item, core.Object{"operation": "retext", "key": in.Key, "text": in.Text})
}

// indexedTextCount 数模板里按 {i} 取第几条文案的位置，前几张有文案可写；模板没有 {i} 时不限制。
// 文案只有 1 条细节时，细节屏不再展开成 3 张一样的图。
func indexedTextCount(t Template, product core.Product, count int) int {
	indexed := false
	for _, spec := range t.Texts {
		if strings.Contains(spec.From, "{i}") {
			indexed = true
		}
	}
	if !indexed {
		return count
	}
	n := 0
	for seq := 1; seq <= count; seq++ {
		for _, text := range makeTexts(t, product, seq, nil) {
			spec := ""
			for _, s := range t.Texts {
				if s.Key == text.Key {
					spec = s.From
				}
			}
			if strings.Contains(spec, "{i}") {
				n = seq
				break
			}
		}
	}
	return n
}
