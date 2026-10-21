package creative

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"xstudio/internal/core"
)

func (a *Module) itemJudge(w http.ResponseWriter, r *http.Request) {
	item, ok := a.itemScope(w, r)
	if !ok {
		return
	}
	if item.Superseded || item.OutputAssetID == 0 || a.itemBusy(item) {
		core.Fail(w, 409, "图片不可复核或正在处理中")
		return
	}
	var in struct {
		Verdict string `json:"verdict"`
		Note    string `json:"note"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if !slices.Contains([]string{"pass", "redraw", "discard"}, in.Verdict) || (in.Verdict == "redraw" && strings.TrimSpace(in.Note) == "") {
		core.Fail(w, 400, "判断不正确，重画必须带说明")
		return
	}
	if len([]rune(in.Note)) > 4000 {
		core.Fail(w, 400, "说明不能超过4000字")
		return
	}
	uid := core.CurrentUser(r).ID
	if in.Verdict == "redraw" {
		if _, err := a.authorize(r.Context(), uid, item.ListingID, core.CreativeRun); err != nil {
			fail(w, err)
			return
		}
	}
	settings := a.settings()
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if err = authorizeTx(r.Context(), tx, uid, item.ListingID, core.CreativeReview); err != nil {
		fail(w, err)
		return
	}
	if _, err = tx.ExecContext(r.Context(), "UPDATE creative_items SET human=?,human_note=?,human_by=?,human_at=? WHERE id=? AND superseded=0", in.Verdict, in.Note, uid, core.Now(), item.ID); err != nil {
		fail(w, err)
		return
	}
	if err = a.calibrate(r.Context(), tx, item, in.Verdict, settings); err != nil {
		fail(w, err)
		return
	}
	var job int64
	if in.Verdict == "redraw" {
		job, err = enqueueTx(r.Context(), tx, "creative.item", "creative_item", item.ID, uid, core.Object{"note": in.Note})
		if err != nil {
			fail(w, err)
			return
		}
	}
	if item.SetID != nil {
		if _, err = tx.ExecContext(r.Context(), "UPDATE creative_sets SET status='reviewing',glanced_at='',glanced_by=NULL,updated_at=? WHERE id=?", core.Now(), *item.SetID); err != nil {
			fail(w, err)
			return
		}
	}
	if err = auditTx(r.Context(), tx, uid, "creative.judge", "creative_item", item.ID, in); err != nil {
		fail(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	if item.SetID != nil {
		if err = a.refreshSet(r.Context(), *item.SetID); err != nil {
			fail(w, err)
			return
		}
	}
	if job != 0 {
		a.Wake()
	}
	core.WriteJSON(w, 200, core.Object{"ok": true, "job_id": job})
}
func (a *Module) itemChoose(w http.ResponseWriter, r *http.Request) {
	item, ok := a.itemScope(w, r)
	if !ok {
		return
	}
	if item.SetID == nil || item.Slot != "main1" || item.Superseded || item.OutputAssetID == 0 || a.itemBusy(item) {
		core.Fail(w, 409, "只有已完成的当前首图可以指定 A 版")
		return
	}
	var in struct {
		Variant string `json:"variant"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if in.Variant != "A" {
		core.Fail(w, 400, "只能指定 A 版")
		return
	}
	if item.Variant == "A" {
		core.WriteJSON(w, 200, core.Object{"ok": true})
		return
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	uid := core.CurrentUser(r).ID
	if err = authorizeTx(r.Context(), tx, uid, item.ListingID, core.CreativeReview); err != nil {
		fail(w, err)
		return
	}
	var previous int64
	_ = tx.QueryRowContext(r.Context(), "SELECT id FROM creative_items WHERE set_id=? AND slot='main1' AND seq=? AND variant='A' AND superseded=0", *item.SetID, item.Seq).Scan(&previous)
	for _, change := range []struct {
		variant string
		id      int64
	}{{"swap", item.ID}, {"B", previous}, {"A", item.ID}} {
		if _, err = tx.ExecContext(r.Context(), "UPDATE creative_items SET variant=? WHERE id=?", change.variant, change.id); err != nil {
			fail(w, err)
			return
		}
	}
	if _, err = tx.ExecContext(r.Context(), "UPDATE creative_sets SET hero_b_item_id=?,glanced_at='',glanced_by=NULL,status='reviewing',updated_at=? WHERE id=?", previous, core.Now(), *item.SetID); err != nil {
		fail(w, err)
		return
	}
	if err = auditTx(r.Context(), tx, uid, "creative.choose", "creative_item", item.ID, in); err != nil {
		fail(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	core.WriteJSON(w, 200, core.Object{"ok": true})
}
func (a *Module) visibleItems(r *http.Request, query string, args ...any) ([]core.Object, error) {
	items, err := objects(r.Context(), a.DB, query, args...)
	if err != nil {
		return nil, err
	}
	out := []core.Object{}
	u := core.CurrentUser(r)
	for _, item := range items {
		lid := int64(number(item["listing_id"]))
		if a.CanListing(u, lid) {
			out = append(out, item)
		}
	}
	return out, nil
}
func (a *Module) reviewQueue(w http.ResponseWriter, r *http.Request) {
	query := `SELECT i.*,l.shop_id,l.product_id,p.name AS product_name FROM creative_items i JOIN listings l ON l.id=i.listing_id JOIN products p ON p.id=l.product_id WHERE i.route='manual' AND i.human='' AND i.superseded=0`
	args := []any{}
	if shop := r.URL.Query().Get("shop_id"); shop != "" {
		var shopID int64
		if _, err := fmt.Sscan(shop, &shopID); err != nil || !a.CanShop(core.CurrentUser(r), shopID) {
			core.Fail(w, 404, "店铺不存在")
			return
		}
		query += " AND l.shop_id=?"
		args = append(args, shop)
	}
	if slot := r.URL.Query().Get("slot"); slot != "" {
		query += " AND i.slot=?"
		args = append(args, slot)
	}
	items, err := a.visibleItems(r, query+" ORDER BY i.risk DESC,i.id", args...)
	if err != nil {
		fail(w, err)
		return
	}
	for _, item := range items {
		if err = a.enrichItem(r.Context(), item); err != nil {
			fail(w, err)
			return
		}
	}
	core.PageItems(w, r, items)
}
func (a *Module) stats(w http.ResponseWriter, r *http.Request) {
	calibration, err := objects(r.Context(), a.DB, "SELECT * FROM creative_calibration ORDER BY category")
	if err != nil {
		fail(w, err)
		return
	}
	items, err := a.visibleItems(r, "SELECT * FROM creative_items WHERE superseded=0 AND output_asset_id>0")
	if err != nil {
		fail(w, err)
		return
	}
	byType := map[string][]core.Object{}
	byModel := map[string]core.Object{}
	for _, item := range items {
		category := stringValue(item["wear_type"])
		byType[category] = append(byType[category], item)
		model := stringValue(item["model"])
		row := byModel[model]
		if row == nil {
			row = core.Object{"model": model, "total": 0, "auto_pass": 0, "human_pass": 0, "discard": 0}
			byModel[model] = row
		}
		row["total"] = int(number(row["total"])) + 1
		for field, matches := range map[string]bool{"auto_pass": item["route"] == "auto_pass", "human_pass": item["human"] == "pass", "discard": item["human"] == "discard"} {
			if matches {
				row[field] = int(number(row[field])) + 1
			}
		}
	}
	known := map[string]bool{}
	for _, row := range calibration {
		known[stringValue(row["category"])] = true
	}
	for _, category := range []string{"necklace", "earring", "bracelet", "ring"} {
		if !known[category] {
			calibration = append(calibration, core.Object{"category": category, "total": 0, "agree": 0, "open": false, "tightened": false})
		}
	}
	settings := a.settings()
	categories := []core.Object{}
	for _, row := range calibration {
		category := stringValue(row["category"])
		current := byType[category]
		auto, judged, changed := 0, 0, 0
		for _, obj := range current {
			item, _ := typed[Item](obj)
			if item.Route == "auto_pass" {
				auto++
			}
			if item.Human != "" {
				judged++
				if autoEligible(item.Hard, item.ReviewA, item.ReviewB, settings) != (item.Human == "pass") {
					changed++
				}
			}
		}
		row["auto_pass_rate"] = ratio(auto, len(current))
		row["override_rate"] = ratio(changed, judged)
		if row["tightened_reason"] == nil {
			row["tightened_reason"] = ""
		}
		categories = append(categories, row)
	}
	suggestions, err := a.visibleItems(r, "SELECT s.*,p.name AS product_name FROM creative_suggestions s JOIN listings l ON l.id=s.listing_id JOIN products p ON p.id=l.product_id ORDER BY s.id DESC")
	if err != nil {
		fail(w, err)
		return
	}
	for _, category := range categories {
		count := 0
		seen := map[string]bool{}
		categoryKey := stringValue(category["category"])
		for _, suggestion := range suggestions {
			matches := strings.HasPrefix(categoryKey, "tight:") && strings.TrimPrefix(categoryKey, "tight:") == stringValue(suggestion["slot"])
			for _, item := range byType[categoryKey] {
				if number(item["listing_id"]) == number(suggestion["listing_id"]) {
					matches = true
					break
				}
			}
			key := fmt.Sprintf("%v:%v", suggestion["listing_id"], suggestion["reason_code"])
			if matches && !seen[key] {
				count += int(number(suggestion["count"]))
				seen[key] = true
			}
		}
		category["return_links"] = count
	}
	models := []core.Object{}
	for _, row := range byModel {
		models = append(models, row)
	}
	sort.Slice(models, func(first, second int) bool {
		return stringValue(models[first]["model"]) < stringValue(models[second]["model"])
	})
	core.WriteJSON(w, 200, core.Object{"categories": categories, "suggestions": suggestions, "by_model": models})
}
func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}
func (a *Module) suggestionUpdate(w http.ResponseWriter, r *http.Request) {
	obj, err := a.one(r.Context(), "SELECT * FROM creative_suggestions WHERE id=?", core.ID(r, "id"))
	if err != nil {
		core.Fail(w, 404, "建议不存在")
		return
	}
	if !a.listingScope(w, r, int64(number(obj["listing_id"]))) {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if !slices.Contains([]string{"done", "ignored"}, in.Status) {
		core.Fail(w, 400, "建议状态不正确")
		return
	}
	_, err = a.DB.ExecContext(r.Context(), "UPDATE creative_suggestions SET status=?,updated_at=? WHERE id=?", in.Status, core.Now(), core.ID(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	a.Audit(r, "creative.suggestion", "creative_suggestion", core.ID(r, "id"), in)
	core.WriteJSON(w, 200, core.Object{"ok": true})
}
func (a *Module) calibrationUpdate(w http.ResponseWriter, r *http.Request) {
	category := r.PathValue("category")
	if !slices.Contains([]string{"necklace", "earring", "bracelet", "ring"}, category) && !strings.HasPrefix(category, "tight:") {
		core.Fail(w, 400, "校准类别不正确")
		return
	}
	if strings.HasPrefix(category, "tight:") {
		slot := strings.TrimPrefix(category, "tight:")
		if !validSlot("main", slot) && !validSlot("detail", slot) {
			core.Fail(w, 400, "位置不正确")
			return
		}
	}
	var patch core.Object
	if !core.Decode(w, r, &patch) {
		return
	}
	if len(patch) == 0 {
		core.Fail(w, 400, "请指定 open 或 tightened")
		return
	}
	for key, value := range patch {
		if (key != "open" && key != "tightened") || value != false {
			core.Fail(w, 400, "只允许关闭自动通过或解除收紧")
			return
		}
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO creative_calibration(category,updated_at) VALUES(?,?) ON CONFLICT(category) DO NOTHING", category, core.Now()); err != nil {
		fail(w, err)
		return
	}
	for key := range patch {
		if _, err = tx.ExecContext(r.Context(), "UPDATE creative_calibration SET "+key+"=0,updated_at=? WHERE category=?", core.Now(), category); err != nil {
			fail(w, err)
			return
		}
	}
	if err = auditTx(r.Context(), tx, core.CurrentUser(r).ID, "creative.calibration", "creative_calibration", 0, core.Object{"category": category, "patch": patch}); err != nil {
		fail(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	core.WriteJSON(w, 200, core.Object{"ok": true})
}
