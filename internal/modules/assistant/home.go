package assistant

import (
	"fmt"
	"sort"
	"strings"
	"xstudio/internal/core"
)

type todo struct {
	Kind      string  `json:"kind"`
	Level     string  `json:"level"`
	Title     string  `json:"title"`
	Detail    string  `json:"detail"`
	Count     int     `json:"count"`
	ProductID int64   `json:"product_id,omitempty"`
	ListingID int64   `json:"listing_id,omitempty"`
	Action    *Action `json:"action"`
	at        string
}

func openAction(label, path string) *Action {
	return &Action{Type: "open", Label: label, Params: object{"path": path}}
}
func names(items []object, total int) string {
	n := []string{}
	for _, it := range items[:min(2, len(items))] {
		n = append(n, it["product_name"].(string))
	}
	detail := strings.Join(n, "、")
	if total > 2 {
		detail += " 等"
	}
	return detail
}

const listingJoin = ` FROM listings l JOIN products p ON p.id=l.product_id JOIN shops s ON s.id=l.shop_id LEFT JOIN listing_render lr ON lr.listing_id=l.id `

// Count in SQLite and materialize only the rows needed for a home card. The
// same filter drives both queries so totals are independent of the row limit.
func (a *Module) homeRows(columns, from, order string, limit int, args ...any) (int, []object, error) {
	var total int
	if err := a.DB.QueryRow("SELECT count(*)"+from, args...).Scan(&total); err != nil {
		return 0, nil, err
	}
	if total == 0 {
		return 0, []object{}, nil
	}
	listArgs := append(append([]any{}, args...), limit)
	rows, err := core.QueryObjects(a.DB, "SELECT "+columns+from+" ORDER BY "+order+" LIMIT ?", listArgs...)
	return total, rows, err
}

func (a *Module) today(u core.User, recent bool) (object, error) {
	ps, pa := productScope(u)
	ls, la := listingScope(u)
	counts := object{"in_progress": 0, "review_pending": nil, "failed": 0, "no_photo": 0, "copy_unapplied": nil}
	items := []todo{}
	var progress int
	if err := a.DB.QueryRow("SELECT count(*) FROM products p WHERE p.status IN ('sampling','pending') AND "+ps, pa...).Scan(&progress); err != nil {
		return nil, err
	}
	counts["in_progress"] = progress
	failedCount, failed, err := a.homeRows("l.id,l.product_id,p.name product_name,s.name shop_name,l.pipeline_error,l.updated_at", listingJoin+" WHERE l.pipeline_status='failed' AND "+ls, "l.updated_at DESC,l.id DESC", 10, la...)
	if err != nil {
		return nil, err
	}
	counts["failed"] = failedCount
	for _, v := range failed {
		t := todo{Kind: "render_failed", Level: "danger", Title: v["product_name"].(string) + " · 出图失败", Detail: v["shop_name"].(string) + " · " + clip(v["pipeline_error"].(string), 40), Count: 1, ProductID: number(v["product_id"]), ListingID: number(v["id"]), at: v["updated_at"].(string)}
		if u.Has(core.RenderRun) {
			t.Action = &Action{Type: "run_listing", Label: "重跑出图", Summary: clip(t.Title, 60), Params: object{"listing_id": t.ListingID, "ai_label": true}}
		}
		items = append(items, t)
	}
	if u.Has(core.RenderReview) {
		reviewCount, reviews, e := a.homeRows("p.name product_name,lr.updated_at", listingJoin+" WHERE lr.review_status='pending' AND "+ls, "lr.updated_at DESC,l.id DESC", 2, la...)
		if e != nil {
			return nil, e
		}
		counts["review_pending"] = reviewCount
		if len(reviews) > 0 {
			items = append(items, todo{Kind: "review_pending", Level: "warning", Title: fmt.Sprintf("审核 %d 款成品图", reviewCount), Detail: names(reviews, reviewCount), Count: reviewCount, Action: openAction("去审核", "/review"), at: reviews[0]["updated_at"].(string)})
		}
	}
	if u.Has(core.CopyView) {
		copyCount, copies, e := a.homeRows("l.id,p.name product_name,cv.created_at", ` FROM copy_versions cv JOIN listings l ON l.id=cv.listing_id JOIN products p ON p.id=l.product_id WHERE cv.id=(SELECT max(v.id) FROM copy_versions v WHERE v.listing_id=l.id) AND cv.source IN ('ai_generate','ai_rewrite') AND coalesce(cv.applied_at,'')='' AND `+ls, "cv.created_at DESC,cv.id DESC", 2, la...)
		if e != nil {
			return nil, e
		}
		counts["copy_unapplied"] = copyCount
		if len(copies) > 0 {
			items = append(items, todo{Kind: "copy_unapplied", Level: "info", Title: fmt.Sprintf("确认 AI 写好的 %d 份文案", copyCount), Detail: names(copies, copyCount), Count: copyCount, Action: openAction("去确认", fmt.Sprintf("/copy?listing=%d", number(copies[0]["id"]))), at: copies[0]["created_at"].(string)})
		}
	}
	emptyCount, empty, err := a.homeRows("p.name product_name,p.updated_at", " FROM products p WHERE p.status<>'archived' AND NOT EXISTS(SELECT 1 FROM assets a WHERE a.product_id=p.id) AND "+ps, "p.updated_at DESC,p.id DESC", 2, pa...)
	if err != nil {
		return nil, err
	}
	counts["no_photo"] = emptyCount
	if len(empty) > 0 {
		items = append(items, todo{Kind: "no_photo", Level: "info", Title: fmt.Sprintf("%d 款还没传照片", emptyCount), Detail: names(empty, emptyCount), Count: emptyCount, Action: openAction("去传照片", "/products?tab=sampling"), at: empty[0]["updated_at"].(string)})
	}
	levels := map[string]int{"danger": 0, "warning": 1, "info": 2}
	sort.SliceStable(items, func(i, j int) bool {
		if levels[items[i].Level] != levels[items[j].Level] {
			return levels[items[i].Level] < levels[items[j].Level]
		}
		return items[i].at > items[j].at
	})
	out := object{"counts": counts, "todos": items[:min(10, len(items))]}
	if recent {
		photos, e := core.QueryObjects(a.DB, "SELECT a.id,a.product_id,p.name product_name,a.title,a.source,a.file,a.created_at FROM assets a JOIN products p ON p.id=a.product_id WHERE "+ps+" ORDER BY a.created_at DESC,a.id DESC LIMIT 8", pa...)
		if e != nil {
			return nil, e
		}
		for _, v := range photos {
			v["thumb_url"] = fmt.Sprintf("/files/products/%d/%s?w=240", number(v["product_id"]), v["file"])
			delete(v, "file")
		}
		out["recent_assets"] = photos
	}
	return out, nil
}

func (a *Module) reviewRows(u core.User, shop int64, limit int) ([]object, error) {
	scope, args := listingScope(u)
	q := `SELECT l.id,l.id listing_id,l.product_id,p.name product_name,l.shop_id,s.name shop_name,lr.detail_template,lr.main_template,lr.ai_label,lr.review_status,lr.rendered_job_id,lr.rendered_template,lr.updated_at` + listingJoin + " WHERE lr.review_status='pending' AND " + scope
	if shop > 0 {
		q += " AND l.shop_id=?"
		args = append(args, shop)
	}
	q += " ORDER BY lr.updated_at DESC,l.id DESC"
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	return core.QueryObjects(a.DB, q, args...)
}
