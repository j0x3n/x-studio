package market

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"xstudio/internal/core"
)

type trendPoint struct {
	Date         string `json:"date"`
	SearchVolume *int64 `json:"search_volume"`
}

type keywordHit struct {
	ID           int64        `json:"id"`
	Category     string       `json:"category"`
	Word         string       `json:"word"`
	Date         string       `json:"date"`
	SearchVolume *int64       `json:"search_volume"`
	Clicks       *int64       `json:"clicks"`
	CTRBps       *int64       `json:"ctr_bps"`
	CVRBps       *int64       `json:"cvr_bps"`
	Competition  *int64       `json:"competition"`
	Score        *float64     `json:"score"`
	Trend        []trendPoint `json:"trend"`
}

type competitorHit struct {
	ID         int64   `json:"id"`
	ItemID     string  `json:"platform_item_id"`
	Keyword    string  `json:"keyword"`
	Rank       *int64  `json:"rank"`
	Title      string  `json:"title"`
	PriceCents *int64  `json:"price_cents"`
	SalesText  *string `json:"sales_text"`
	Sales      *int64  `json:"sales"`
	ShopName   string  `json:"shop_name"`
	ImageURL   string  `json:"image_url"`
	Material   string  `json:"material"`
	Params     any     `json:"params"`
	Date       string  `json:"date"`
}

type dailyHit struct {
	Date        string `json:"date"`
	Impressions *int64 `json:"impressions"`
	Clicks      *int64 `json:"clicks"`
	AddCart     *int64 `json:"add_cart"`
	Orders      *int64 `json:"orders"`
	GMVCents    *int64 `json:"gmv_cents"`
	UV          *int64 `json:"uv"`
	SearchTerms any    `json:"search_terms"`
	Source      string `json:"source"`
}

type adHit struct {
	ID          int64  `json:"id"`
	ListingID   *int64 `json:"listing_id"`
	Date        string `json:"date"`
	Campaign    string `json:"campaign"`
	Keyword     string `json:"keyword"`
	CostCents   *int64 `json:"cost_cents"`
	Impressions *int64 `json:"impressions"`
	Clicks      *int64 `json:"clicks"`
	Orders      *int64 `json:"orders"`
	GMVCents    *int64 `json:"gmv_cents"`
}

type snapshotHit struct {
	ID         int64  `json:"id"`
	ShopID     int64  `json:"shop_id"`
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	SourceURL  string `json:"source_url"`
	CapturedAt string `json:"captured_at"`
	Rows       int64  `json:"rows"`
	CreatedBy  int64  `json:"created_by"`
	CreatedAt  string `json:"created_at"`
}

func daysWindow(r *http.Request, def int) (string, string, bool) {
	to := chinaToday()
	n := def
	if raw := r.URL.Query().Get("days"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 366 {
			return "", "", false
		}
		n = v
	}
	return addDays(to, -(n - 1)), to, true
}

func dateSpan(r *http.Request) (string, string, bool) {
	today := chinaToday()
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" {
		from = addDays(today, -29)
	}
	if to == "" {
		to = today
	}
	if !validDate(from) || !validDate(to) || from > to {
		return "", "", false
	}
	return from, to, true
}

func queryShop(r *http.Request) int64 {
	n, _ := strconv.ParseInt(r.URL.Query().Get("shop_id"), 10, 64)
	return n
}

func likePattern(q string) string {
	q = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	return "%" + q + "%"
}

func lessIntDesc(a, b *int64, aw, bw string) bool {
	if a == nil && b == nil {
		return aw < bw
	}
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	if *a != *b {
		return *a > *b
	}
	return aw < bw
}

func (m *Module) listKeywords(w http.ResponseWriter, r *http.Request) {
	shopID := queryShop(r)
	if !m.requireShop(w, r, shopID) {
		return
	}
	from, to, ok := daysWindow(r, 30)
	if !ok {
		core.Fail(w, 400, "天数不正确")
		return
	}
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	query := "SELECT id, category, word, date, search_volume, clicks, ctr_bps, cvr_bps, competition FROM market_keywords WHERE shop_id=? AND date>=? AND date<=?"
	args := []any{shopID, from, to}
	if category != "" {
		query += " AND category=?"
		args = append(args, category)
	}
	if q != "" {
		query += " AND word LIKE ? ESCAPE '\\'"
		args = append(args, likePattern(q))
	}
	rows, err := m.DB.Query(query, args...)
	if err != nil {
		core.Fail(w, 500, "读取关键词失败")
		return
	}
	best := map[string]keywordHit{}
	for rows.Next() {
		var hit keywordHit
		var vol, clicks, ctr, cvr, comp sql.NullInt64
		if err = rows.Scan(&hit.ID, &hit.Category, &hit.Word, &hit.Date, &vol, &clicks, &ctr, &cvr, &comp); err != nil {
			rows.Close()
			core.Fail(w, 500, "读取关键词失败")
			return
		}
		hit.SearchVolume = nullInt(vol)
		hit.Clicks = nullInt(clicks)
		hit.CTRBps = nullInt(ctr)
		hit.CVRBps = nullInt(cvr)
		hit.Competition = nullInt(comp)
		hit.Score = keywordScore(hit.SearchVolume, hit.CVRBps)
		hit.Trend = []trendPoint{}
		old, exists := best[hit.Word]
		if !exists || hit.Date > old.Date || (hit.Date == old.Date && hit.ID > old.ID) {
			best[hit.Word] = hit
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		core.Fail(w, 500, "读取关键词失败")
		return
	}
	items := make([]keywordHit, 0, len(best))
	for _, hit := range best {
		items = append(items, hit)
	}
	mode := r.URL.Query().Get("sort")
	if mode != "volume" && mode != "cvr" {
		mode = "score"
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch mode {
		case "volume":
			return lessIntDesc(a.SearchVolume, b.SearchVolume, a.Word, b.Word)
		case "cvr":
			return lessIntDesc(a.CVRBps, b.CVRBps, a.Word, b.Word)
		default:
			if a.Score == nil && b.Score == nil {
				return a.Word < b.Word
			}
			if a.Score == nil {
				return false
			}
			if b.Score == nil {
				return true
			}
			if *a.Score != *b.Score {
				return *a.Score > *b.Score
			}
			return a.Word < b.Word
		}
	})
	page, total := pageSlice(r, items)
	if err = m.attachTrend(shopID, addDays(chinaToday(), -6), chinaToday(), page); err != nil {
		core.Fail(w, 500, "读取关键词失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"items": page, "total": total})
}

func pageSlice[T any](r *http.Request, items []T) ([]T, int) {
	total := len(items)
	offset, size := core.Page(r)
	if offset > total {
		offset = total
	}
	end := offset + size
	if end > total {
		end = total
	}
	page := items[offset:end]
	if page == nil {
		page = []T{}
	}
	return page, total
}

func (m *Module) attachTrend(shopID int64, from, to string, items []keywordHit) error {
	if len(items) == 0 {
		return nil
	}
	args := []any{shopID, from, to}
	marks := make([]string, len(items))
	index := map[string]int{}
	for i, hit := range items {
		marks[i] = "?"
		args = append(args, hit.Word)
		index[hit.Word] = i
		items[i].Trend = []trendPoint{}
	}
	rows, err := m.DB.Query("SELECT word, date, search_volume FROM market_keywords WHERE shop_id=? AND date>=? AND date<=? AND word IN ("+strings.Join(marks, ",")+") ORDER BY date, id", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var word, date string
		var vol sql.NullInt64
		if err = rows.Scan(&word, &date, &vol); err != nil {
			return err
		}
		i, ok := index[word]
		if !ok {
			continue
		}
		items[i].Trend = append(items[i].Trend, trendPoint{Date: date, SearchVolume: nullInt(vol)})
	}
	return rows.Err()
}

func (m *Module) listCompetitors(w http.ResponseWriter, r *http.Request) {
	shopID := queryShop(r)
	if !m.requireShop(w, r, shopID) {
		return
	}
	from, to, ok := daysWindow(r, 7)
	if !ok {
		core.Fail(w, 400, "天数不正确")
		return
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	material := strings.TrimSpace(r.URL.Query().Get("material"))
	query := "SELECT id, platform_item_id, keyword, rank, title, price_cents, sales_text, sales, shop_name, image_url, material, params, date FROM competitor_items WHERE shop_id=? AND date>=? AND date<=?"
	args := []any{shopID, from, to}
	if keyword != "" {
		query += " AND keyword=?"
		args = append(args, keyword)
	}
	rows, err := m.DB.Query(query, args...)
	if err != nil {
		core.Fail(w, 500, "读取竞品失败")
		return
	}
	type key struct{ item, word string }
	best := map[key]competitorHit{}
	for rows.Next() {
		hit, errScan := scanCompetitor(rows)
		if errScan != nil {
			rows.Close()
			core.Fail(w, 500, "读取竞品失败")
			return
		}
		k := key{hit.ItemID, hit.Keyword}
		old, exists := best[k]
		if !exists || hit.Date > old.Date || (hit.Date == old.Date && hit.ID > old.ID) {
			best[k] = hit
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		core.Fail(w, 500, "读取竞品失败")
		return
	}
	items := []competitorHit{}
	for _, hit := range best {
		if material != "" && hit.Material != material {
			continue
		}
		items = append(items, hit)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].Rank, items[j].Rank
		if a == nil && b == nil {
			return items[i].ID < items[j].ID
		}
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		if *a != *b {
			return *a < *b
		}
		return items[i].ID < items[j].ID
	})
	page, total := pageSlice(r, items)
	core.WriteJSON(w, 200, core.Object{"items": page, "total": total})
}

func scanCompetitor(s interface{ Scan(...any) error }) (competitorHit, error) {
	var hit competitorHit
	var rank, price, sales sql.NullInt64
	var salesText sql.NullString
	var params string
	err := s.Scan(&hit.ID, &hit.ItemID, &hit.Keyword, &rank, &hit.Title, &price, &salesText, &sales, &hit.ShopName, &hit.ImageURL, &hit.Material, &params, &hit.Date)
	if err != nil {
		return hit, err
	}
	hit.Rank = nullInt(rank)
	hit.PriceCents = nullInt(price)
	hit.Sales = nullInt(sales)
	if salesText.Valid {
		v := salesText.String
		hit.SalesText = &v
	}
	if json.Unmarshal([]byte(params), &hit.Params) != nil || hit.Params == nil {
		hit.Params = core.Object{}
	}
	return hit, nil
}

func (m *Module) listingDaily(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !m.RequireListing(w, r, id, false) {
		return
	}
	from, to, ok := dateSpan(r)
	if !ok {
		core.Fail(w, 400, "日期不正确")
		return
	}
	rows, err := m.DB.Query("SELECT date, impressions, clicks, add_cart, orders, gmv_cents, uv, search_terms, source FROM listing_daily WHERE listing_id=? AND date>=? AND date<=? ORDER BY date", id, from, to)
	if err != nil {
		core.Fail(w, 500, "读取每日数据失败")
		return
	}
	defer rows.Close()
	items := []dailyHit{}
	for rows.Next() {
		var hit dailyHit
		var impr, clicks, cart, orders, gmv, uv sql.NullInt64
		var terms string
		if err = rows.Scan(&hit.Date, &impr, &clicks, &cart, &orders, &gmv, &uv, &terms, &hit.Source); err != nil {
			core.Fail(w, 500, "读取每日数据失败")
			return
		}
		hit.Impressions = nullInt(impr)
		hit.Clicks = nullInt(clicks)
		hit.AddCart = nullInt(cart)
		hit.Orders = nullInt(orders)
		hit.GMVCents = nullInt(gmv)
		hit.UV = nullInt(uv)
		if json.Unmarshal([]byte(terms), &hit.SearchTerms) != nil || hit.SearchTerms == nil {
			hit.SearchTerms = []any{}
		}
		items = append(items, hit)
	}
	if err = rows.Err(); err != nil {
		core.Fail(w, 500, "读取每日数据失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"items": items})
}

func (m *Module) listAds(w http.ResponseWriter, r *http.Request) {
	shopID := queryShop(r)
	if !m.requireShop(w, r, shopID) {
		return
	}
	from, to, ok := dateSpan(r)
	if !ok {
		core.Fail(w, 400, "日期不正确")
		return
	}
	listingID, _ := strconv.ParseInt(r.URL.Query().Get("listing_id"), 10, 64)
	if listingID > 0 {
		var shop int64
		if m.DB.QueryRow("SELECT shop_id FROM listings WHERE id=?", listingID).Scan(&shop) != nil || shop != shopID {
			core.Fail(w, 404, "上架版本不存在")
			return
		}
	}
	query := "SELECT id, listing_id, date, campaign, keyword, cost_cents, impressions, clicks, orders, gmv_cents FROM ad_daily WHERE shop_id=? AND date>=? AND date<=?"
	args := []any{shopID, from, to}
	if listingID > 0 {
		query += " AND listing_id=?"
		args = append(args, listingID)
	}
	rows, err := m.DB.Query(query+" ORDER BY date, campaign, keyword, id", args...)
	if err != nil {
		core.Fail(w, 500, "读取推广数据失败")
		return
	}
	defer rows.Close()
	items := []adHit{}
	for rows.Next() {
		var hit adHit
		var listing sql.NullInt64
		var cost, impr, clicks, orders, gmv sql.NullInt64
		if err = rows.Scan(&hit.ID, &listing, &hit.Date, &hit.Campaign, &hit.Keyword, &cost, &impr, &clicks, &orders, &gmv); err != nil {
			core.Fail(w, 500, "读取推广数据失败")
			return
		}
		hit.ListingID = nullInt(listing)
		hit.CostCents = nullInt(cost)
		hit.Impressions = nullInt(impr)
		hit.Clicks = nullInt(clicks)
		hit.Orders = nullInt(orders)
		hit.GMVCents = nullInt(gmv)
		items = append(items, hit)
	}
	if err = rows.Err(); err != nil {
		core.Fail(w, 500, "读取推广数据失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"items": items})
}

func (m *Module) listSnapshots(w http.ResponseWriter, r *http.Request) {
	shopID := queryShop(r)
	if !m.requireShop(w, r, shopID) {
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	query := `SELECT id, shop_id, kind, source, source_url, captured_at, "rows", created_by, created_at FROM market_snapshots WHERE shop_id=?`
	args := []any{shopID}
	if kind != "" {
		query += " AND kind=?"
		args = append(args, kind)
	}
	rows, err := m.DB.Query(query+" ORDER BY id DESC", args...)
	if err != nil {
		core.Fail(w, 500, "读取快照失败")
		return
	}
	defer rows.Close()
	items := []snapshotHit{}
	for rows.Next() {
		var hit snapshotHit
		if err = rows.Scan(&hit.ID, &hit.ShopID, &hit.Kind, &hit.Source, &hit.SourceURL, &hit.CapturedAt, &hit.Rows, &hit.CreatedBy, &hit.CreatedAt); err != nil {
			core.Fail(w, 500, "读取快照失败")
			return
		}
		items = append(items, hit)
	}
	if err = rows.Err(); err != nil {
		core.Fail(w, 500, "读取快照失败")
		return
	}
	page, total := pageSlice(r, items)
	core.WriteJSON(w, 200, core.Object{"items": page, "total": total})
}

func (m *Module) getSnapshot(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	var hit snapshotHit
	var raw string
	err := m.DB.QueryRow(`SELECT id, shop_id, kind, source, source_url, captured_at, "rows", created_by, created_at, raw FROM market_snapshots WHERE id=?`, id).Scan(&hit.ID, &hit.ShopID, &hit.Kind, &hit.Source, &hit.SourceURL, &hit.CapturedAt, &hit.Rows, &hit.CreatedBy, &hit.CreatedAt, &raw)
	if err != nil || id == 0 {
		core.Fail(w, 404, "快照不存在")
		return
	}
	if !m.requireShop(w, r, hit.ShopID) {
		return
	}
	var parsed any
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		parsed = raw
	}
	core.WriteJSON(w, 200, core.Object{
		"id": hit.ID, "shop_id": hit.ShopID, "kind": hit.Kind, "source": hit.Source, "source_url": hit.SourceURL,
		"captured_at": hit.CapturedAt, "rows": hit.Rows, "created_by": hit.CreatedBy, "created_at": hit.CreatedAt, "raw": parsed,
	})
}

func (m *Module) getShopSettings(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !m.requireShop(w, r, id) {
		return
	}
	var cents sql.NullInt64
	var updated string
	err := m.DB.QueryRow("SELECT daily_budget_cents, updated_at FROM market_shop_settings WHERE shop_id=?", id).Scan(&cents, &updated)
	if err == sql.ErrNoRows {
		core.WriteJSON(w, 200, core.Object{"shop_id": id, "daily_budget_cents": nil})
		return
	}
	if err != nil {
		core.Fail(w, 500, "读取店铺设置失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"shop_id": id, "daily_budget_cents": nullInt(cents), "updated_at": updated})
}

func (m *Module) putShopSettings(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !m.requireShop(w, r, id) {
		return
	}
	var raw map[string]json.RawMessage
	if !core.Decode(w, r, &raw) {
		return
	}
	v, ok := raw["daily_budget_cents"]
	if !ok {
		core.Fail(w, 400, "请填写每日预算")
		return
	}
	n, _, ok := optInt(v)
	if !ok || n == nil {
		core.Fail(w, 400, "每日预算不正确")
		return
	}
	now := core.Now()
	if _, err := m.DB.Exec(`INSERT INTO market_shop_settings(shop_id, daily_budget_cents, updated_by, updated_at) VALUES(?,?,?,?) ON CONFLICT(shop_id) DO UPDATE SET daily_budget_cents=excluded.daily_budget_cents, updated_by=excluded.updated_by, updated_at=excluded.updated_at`, id, *n, core.CurrentUser(r).ID, now); err != nil {
		core.Fail(w, 500, "保存店铺设置失败")
		return
	}
	m.Audit(r, "market.settings", "shop", id, core.Object{"daily_budget_cents": *n})
	core.WriteJSON(w, 200, core.Object{"shop_id": id, "daily_budget_cents": *n, "updated_at": now})
}
