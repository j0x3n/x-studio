package market

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"xstudio/internal/core"
)

type skipRow struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

type keywordRow struct {
	Word     string
	Date     string
	Category string
	Volume   *int64
	Clicks   *int64
	CTR      *int64
	CVR      *int64
	Comp     *int64
}

type competitorRow struct {
	ItemID    string
	Keyword   string
	Rank      *int64
	Title     string
	Price     *int64
	SalesText *string
	Sales     *int64
	ShopName  string
	ImageURL  string
	Material  string
	Params    string
	Date      string
}

type dailyRow struct {
	ListingID   int64
	Date        string
	Impressions *int64
	Clicks      *int64
	AddCart     *int64
	Orders      *int64
	GMV         *int64
	UV          *int64
	Terms       string
}

type adRow struct {
	ListingID   *int64
	Date        string
	Campaign    string
	Keyword     string
	Cost        *int64
	Impressions *int64
	Clicks      *int64
	Orders      *int64
	GMV         *int64
}

type prepared struct {
	Keywords    []keywordRow
	Competitors []competitorRow
	Daily       []dailyRow
	Ads         []adRow
	Skipped     []skipRow
	Sample      []core.Object
}

func (p *prepared) skip(i int, reason string) {
	p.Skipped = append(p.Skipped, skipRow{Index: i, Reason: reason})
}

func (p *prepared) sample(o core.Object) {
	if len(p.Sample) >= 50 {
		return
	}
	p.Sample = append(p.Sample, o)
}

func (p prepared) okCount() int {
	return len(p.Keywords) + len(p.Competitors) + len(p.Daily) + len(p.Ads)
}

type listingIndex struct {
	byItem    map[string]int64
	ambiguous map[string]bool
	ids       map[int64]bool
}

type queryer interface {
	Query(string, ...any) (*sql.Rows, error)
}

func loadListings(q queryer, shopID int64) (listingIndex, error) {
	idx := listingIndex{byItem: map[string]int64{}, ambiguous: map[string]bool{}, ids: map[int64]bool{}}
	rows, err := q.Query("SELECT id, url FROM listings WHERE shop_id=?", shopID)
	if err != nil {
		return idx, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var raw string
		if err = rows.Scan(&id, &raw); err != nil {
			return idx, err
		}
		idx.ids[id] = true
		item := itemIDFromURL(raw)
		if item == "" {
			continue
		}
		if prev, ok := idx.byItem[item]; ok && prev != id {
			idx.ambiguous[item] = true
			continue
		}
		idx.byItem[item] = id
	}
	return idx, rows.Err()
}

func itemIDFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	id := u.Query().Get("id")
	if !digitsOnly(id) {
		return ""
	}
	return id
}

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (idx listingIndex) matchItem(itemID string) (int64, string) {
	if idx.ambiguous[itemID] {
		return 0, "商品链接匹配到多个上架版本"
	}
	id, ok := idx.byItem[itemID]
	if !ok {
		return 0, "对不上上架版本，请在上架版本里填写商品链接"
	}
	return id, ""
}

func chinaLoc() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

func chinaToday() string {
	return time.Now().In(chinaLoc()).Format("2006-01-02")
}

func validDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	t, err := time.ParseInLocation("2006-01-02", s, chinaLoc())
	return err == nil && t.Format("2006-01-02") == s
}

func dateOK(s, today string) bool {
	return validDate(s) && s <= today
}

func dateFromCaptured(s string) (string, bool) {
	s = strings.TrimSpace(s)
	today := chinaToday()
	if s == "" {
		return today, true
	}
	if len(s) == 10 {
		if !validDate(s) {
			return "", false
		}
		return s, s <= today
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
	}
	if err != nil {
		return "", false
	}
	d := t.In(chinaLoc()).Format("2006-01-02")
	return d, d <= today
}

func addDays(date string, n int) string {
	t, err := time.ParseInLocation("2006-01-02", date, chinaLoc())
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

func optString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", true
	}
	s := strings.TrimSpace(string(raw))
	if s == "" || s[0] != '"' {
		return "", false
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	return strings.TrimSpace(v), true
}

func optInt(raw json.RawMessage) (*int64, bool, bool) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil, false, true
	}
	s := strings.TrimSpace(string(raw))
	if s == "" || s[0] == '"' {
		if s != "" && s[0] == '"' {
			var v string
			if json.Unmarshal(raw, &v) != nil {
				return nil, true, false
			}
			s = strings.TrimSpace(v)
		}
	}
	if s == "" || strings.ContainsAny(s, ".eE+") || strings.HasPrefix(s, "-") {
		return nil, true, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return nil, true, false
	}
	return &n, true, true
}

func rawNumber(raw json.RawMessage) (string, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "", false
	}
	if s[0] == '"' {
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return "", false
		}
		return strings.TrimSpace(v), true
	}
	return s, true
}

func splitDecimal(s string) (string, string, bool) {
	if s == "" || strings.ContainsAny(s, "+-eE ") {
		return "", "", false
	}
	digit := false
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digit = true
			break
		}
	}
	if !digit {
		return "", "", false
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return "", "", false
	}
	ip := parts[0]
	if ip == "" {
		ip = "0"
	}
	for _, c := range ip {
		if c < '0' || c > '9' {
			return "", "", false
		}
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
		for _, c := range frac {
			if c < '0' || c > '9' {
				return "", "", false
			}
		}
	}
	return ip, frac, true
}

func withinPercent(s string) bool {
	ip, frac, ok := splitDecimal(s)
	if !ok {
		return false
	}
	ip = strings.TrimLeft(ip, "0")
	if ip == "" {
		return true
	}
	if len(ip) > 3 {
		return false
	}
	n, err := strconv.Atoi(ip)
	if err != nil || n > 100 {
		return false
	}
	if n == 100 {
		for _, c := range frac {
			if c != '0' {
				return false
			}
		}
	}
	return true
}

func scaleDecimal(s string, scale int64) (int64, bool) {
	s = strings.TrimSpace(s)
	ip, frac, ok := splitDecimal(s)
	if !ok || scale < 1 {
		return 0, false
	}
	digits := 0
	for sc := scale; sc > 1; sc /= 10 {
		digits++
	}
	for len(frac) < digits+1 {
		frac += "0"
	}
	keep := frac[:digits]
	roundDigit := frac[digits]
	whole, err := strconv.ParseInt(ip, 10, 64)
	if err != nil {
		return 0, false
	}
	var part int64
	if keep != "" {
		part, err = strconv.ParseInt(keep, 10, 64)
		if err != nil {
			return 0, false
		}
	}
	if whole > (math.MaxInt64-part)/scale {
		return 0, false
	}
	v := whole*scale + part
	if roundDigit >= '5' {
		if v == math.MaxInt64 {
			return 0, false
		}
		v++
	}
	return v, true
}

func optScaled(raw json.RawMessage, scale int64, percent bool) (*int64, bool) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil, true
	}
	s, ok := rawNumber(raw)
	if !ok {
		return nil, false
	}
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "-") {
		return nil, false
	}
	if percent && !withinPercent(s) {
		return nil, false
	}
	v, ok := scaleDecimal(s, scale)
	if !ok {
		return nil, false
	}
	return &v, true
}

var salesPattern = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(万)?`)

func parseSales(s string) *int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	m := salesPattern.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	scale := int64(1)
	if m[2] == "万" {
		scale = 10000
	}
	v, ok := scaleDecimal(m[1], scale)
	if !ok {
		return nil
	}
	return &v
}

func materialFrom(title string, params map[string]any) string {
	var b strings.Builder
	b.WriteString(title)
	appendText(&b, params)
	s := b.String()
	switch {
	case strings.Contains(s, "施家"):
		return "施家"
	case strings.Contains(s, "贝珠"), strings.Contains(s, "贝壳珠"):
		return "贝珠"
	case strings.Contains(s, "仿"):
		return "仿"
	case strings.Contains(s, "淡水"):
		return "淡水"
	case strings.Contains(s, "天然"):
		return "天然"
	default:
		return "未写"
	}
}

func appendText(b *strings.Builder, v any) {
	switch t := v.(type) {
	case string:
		b.WriteString(t)
	case []any:
		for _, x := range t {
			appendText(b, x)
		}
	case map[string]any:
		for _, x := range t {
			appendText(b, x)
		}
	}
}

func boundText(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		return "", false
	}
	return s, true
}

func validImage(s string) bool {
	if s == "" {
		return true
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func parseParams(raw json.RawMessage) (map[string]any, string, bool) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil, "{}", true
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, "", false
	}
	return m, core.JS(m), true
}

func reviewCount(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return 0, true
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return 0, false
	}
	return len(arr), true
}

func imagesOK(raw json.RawMessage) bool {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return true
	}
	var arr []json.RawMessage
	return json.Unmarshal(raw, &arr) == nil
}

func parseTerms(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return "[]", true
	}
	var arr []map[string]json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return "", false
	}
	out := make([]core.Object, 0, len(arr))
	for _, t := range arr {
		term, ok := optString(t["term"])
		if !ok {
			return "", false
		}
		term, ok = boundText(term, 200)
		if !ok || term == "" {
			return "", false
		}
		uv, _, ok1 := optInt(t["uv"])
		clicks, _, ok2 := optInt(t["clicks"])
		orders, _, ok3 := optInt(t["orders"])
		if !ok1 || !ok2 || !ok3 {
			return "", false
		}
		out = append(out, core.Object{"term": term, "uv": uv, "clicks": clicks, "orders": orders})
	}
	return core.JS(out), true
}

func decodeItem(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil, false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}

func resolveListing(idx listingIndex, m map[string]json.RawMessage, required bool) (*int64, string) {
	lid, _, ok := optInt(m["listing_id"])
	if !ok {
		return nil, "上架版本不正确"
	}
	itemID, ok := optString(m["item_id"])
	if !ok {
		return nil, "商品编号不正确"
	}
	itemID = strings.TrimSpace(itemID)
	if lid != nil && *lid > 0 {
		if !idx.ids[*lid] {
			return nil, "上架版本不存在"
		}
		return lid, ""
	}
	if itemID == "" {
		if required {
			return nil, "缺少商品或上架版本"
		}
		return nil, ""
	}
	if !digitsOnly(itemID) {
		return nil, "对不上上架版本，请在上架版本里填写商品链接"
	}
	id, reason := idx.matchItem(itemID)
	if reason != "" {
		return nil, reason
	}
	return &id, ""
}

func parseItems(idx listingIndex, kind, category, batchDate, today string, items []json.RawMessage) prepared {
	out := prepared{Skipped: []skipRow{}, Sample: []core.Object{}}
	for i, raw := range items {
		m, ok := decodeItem(raw)
		if !ok {
			out.skip(i, "数据格式不正确")
			continue
		}
		switch kind {
		case "keyword":
			row, reason := parseKeyword(m, category, batchDate)
			if reason != "" {
				out.skip(i, reason)
				continue
			}
			out.Keywords = append(out.Keywords, row)
			out.sample(core.Object{"index": i, "word": row.Word, "date": row.Date, "category": row.Category, "search_volume": row.Volume, "clicks": row.Clicks, "ctr_bps": row.CTR, "cvr_bps": row.CVR, "competition": row.Comp})
		case "search", "item":
			row, reason := parseCompetitor(m, kind, batchDate)
			if reason != "" {
				out.skip(i, reason)
				continue
			}
			out.Competitors = append(out.Competitors, row)
			out.sample(core.Object{"index": i, "item_id": row.ItemID, "keyword": row.Keyword, "rank": row.Rank, "title": row.Title, "price_cents": row.Price, "sales_text": row.SalesText, "sales": row.Sales, "shop_name": row.ShopName, "image_url": row.ImageURL, "material": row.Material, "date": row.Date})
		case "my_listing":
			row, reason := parseDaily(idx, m, today)
			if reason != "" {
				out.skip(i, reason)
				continue
			}
			out.Daily = append(out.Daily, row)
			var terms any
			_ = json.Unmarshal([]byte(row.Terms), &terms)
			out.sample(core.Object{"index": i, "listing_id": row.ListingID, "date": row.Date, "impressions": row.Impressions, "clicks": row.Clicks, "add_cart": row.AddCart, "orders": row.Orders, "gmv_cents": row.GMV, "uv": row.UV, "search_terms": terms})
		case "ad":
			row, reason := parseAd(idx, m, today)
			if reason != "" {
				out.skip(i, reason)
				continue
			}
			out.Ads = append(out.Ads, row)
			out.sample(core.Object{"index": i, "listing_id": row.ListingID, "date": row.Date, "campaign": row.Campaign, "keyword": row.Keyword, "cost_cents": row.Cost, "impressions": row.Impressions, "clicks": row.Clicks, "orders": row.Orders, "gmv_cents": row.GMV})
		default:
			out.skip(i, "不支持的数据类型")
		}
	}
	return out
}

func parseKeyword(m map[string]json.RawMessage, category, date string) (keywordRow, string) {
	word, ok := optString(m["word"])
	if !ok {
		return keywordRow{}, "关键词不正确"
	}
	word, ok = boundText(word, 200)
	if !ok {
		return keywordRow{}, "关键词过长"
	}
	if word == "" {
		return keywordRow{}, "关键词为空"
	}
	vol, _, ok := optInt(m["search_volume"])
	if !ok {
		return keywordRow{}, "搜索量不正确"
	}
	clicks, _, ok := optInt(m["clicks"])
	if !ok {
		return keywordRow{}, "点击量不正确"
	}
	comp, _, ok := optInt(m["competition"])
	if !ok {
		return keywordRow{}, "竞争度不正确"
	}
	ctr, ok := optScaled(m["ctr"], 100, true)
	if !ok {
		return keywordRow{}, "点击率不正确"
	}
	cvr, ok := optScaled(m["cvr"], 100, true)
	if !ok {
		return keywordRow{}, "转化率不正确"
	}
	category, ok = boundText(category, 80)
	if !ok {
		return keywordRow{}, "品类过长"
	}
	return keywordRow{Word: word, Date: date, Category: category, Volume: vol, Clicks: clicks, CTR: ctr, CVR: cvr, Comp: comp}, ""
}

func parseCompetitor(m map[string]json.RawMessage, kind, date string) (competitorRow, string) {
	keyword, ok := optString(m["keyword"])
	if !ok {
		return competitorRow{}, "关键词不正确"
	}
	keyword, ok = boundText(keyword, 200)
	if !ok {
		return competitorRow{}, "关键词过长"
	}
	itemID, ok := optString(m["item_id"])
	if !ok {
		return competitorRow{}, "商品编号不正确"
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" || utf8.RuneCountInString(itemID) > 64 {
		return competitorRow{}, "商品编号不正确"
	}
	rank, _, ok := optInt(m["rank"])
	if !ok {
		return competitorRow{}, "排名不正确"
	}
	title, ok := optString(m["title"])
	if !ok {
		return competitorRow{}, "标题不正确"
	}
	title, ok = boundText(title, 200)
	if !ok {
		return competitorRow{}, "标题过长"
	}
	price, ok := optScaled(m["price"], 100, false)
	if !ok {
		return competitorRow{}, "价格不正确"
	}
	salesText, ok := optString(m["sales_text"])
	if !ok {
		return competitorRow{}, "销量原文不正确"
	}
	if utf8.RuneCountInString(salesText) > 200 {
		return competitorRow{}, "销量原文过长"
	}
	var salesPtr *string
	if strings.TrimSpace(salesText) != "" {
		s := strings.TrimSpace(salesText)
		salesPtr = &s
	}
	shopName, ok := optString(m["shop_name"])
	if !ok {
		return competitorRow{}, "店铺名不正确"
	}
	shopName, ok = boundText(shopName, 200)
	if !ok {
		return competitorRow{}, "店铺名过长"
	}
	imageURL, ok := optString(m["image_url"])
	if !ok || !validImage(imageURL) {
		return competitorRow{}, "图片地址不正确"
	}
	if utf8.RuneCountInString(imageURL) > 1000 {
		return competitorRow{}, "图片地址过长"
	}
	paramsJSON := "{}"
	params := map[string]any(nil)
	if kind == "item" {
		var pok bool
		params, paramsJSON, pok = parseParams(m["params"])
		if !pok {
			return competitorRow{}, "参数格式不正确"
		}
		n, rok := reviewCount(m["reviews"])
		if !rok {
			return competitorRow{}, "评价格式不正确"
		}
		if n > 50 {
			return competitorRow{}, "评价超过 50 条"
		}
		if !imagesOK(m["images"]) {
			return competitorRow{}, "图片格式不正确"
		}
	}
	return competitorRow{
		ItemID: itemID, Keyword: keyword, Rank: rank, Title: title, Price: price,
		SalesText: salesPtr, Sales: parseSales(salesText), ShopName: shopName, ImageURL: imageURL,
		Material: materialFrom(title, params), Params: paramsJSON, Date: date,
	}, ""
}

func parseDaily(idx listingIndex, m map[string]json.RawMessage, today string) (dailyRow, string) {
	date, ok := optString(m["date"])
	if !ok || !dateOK(strings.TrimSpace(date), today) {
		return dailyRow{}, "日期不正确"
	}
	lid, reason := resolveListing(idx, m, true)
	if reason != "" {
		return dailyRow{}, reason
	}
	impr, _, ok := optInt(m["impressions"])
	if !ok {
		return dailyRow{}, "曝光不正确"
	}
	clicks, _, ok := optInt(m["clicks"])
	if !ok {
		return dailyRow{}, "点击不正确"
	}
	cart, _, ok := optInt(m["add_cart"])
	if !ok {
		return dailyRow{}, "加购不正确"
	}
	orders, _, ok := optInt(m["orders"])
	if !ok {
		return dailyRow{}, "订单数不正确"
	}
	uv, _, ok := optInt(m["uv"])
	if !ok {
		return dailyRow{}, "访客数不正确"
	}
	gmv, ok := optScaled(m["gmv"], 100, false)
	if !ok {
		return dailyRow{}, "成交金额不正确"
	}
	terms, ok := parseTerms(m["search_terms"])
	if !ok {
		return dailyRow{}, "搜索词不正确"
	}
	return dailyRow{ListingID: *lid, Date: date, Impressions: impr, Clicks: clicks, AddCart: cart, Orders: orders, GMV: gmv, UV: uv, Terms: terms}, ""
}

func parseAd(idx listingIndex, m map[string]json.RawMessage, today string) (adRow, string) {
	date, ok := optString(m["date"])
	if !ok || !dateOK(strings.TrimSpace(date), today) {
		return adRow{}, "日期不正确"
	}
	campaign, ok := optString(m["campaign"])
	if !ok {
		return adRow{}, "计划名称不正确"
	}
	campaign, ok = boundText(campaign, 200)
	if !ok {
		return adRow{}, "计划名称过长"
	}
	keyword, ok := optString(m["keyword"])
	if !ok {
		return adRow{}, "关键词不正确"
	}
	keyword, ok = boundText(keyword, 200)
	if !ok {
		return adRow{}, "关键词过长"
	}
	lid, reason := resolveListing(idx, m, false)
	if reason != "" {
		return adRow{}, reason
	}
	cost, ok := optScaled(m["cost"], 100, false)
	if !ok {
		return adRow{}, "花费不正确"
	}
	impr, _, ok := optInt(m["impressions"])
	if !ok {
		return adRow{}, "曝光不正确"
	}
	clicks, _, ok := optInt(m["clicks"])
	if !ok {
		return adRow{}, "点击不正确"
	}
	orders, _, ok := optInt(m["orders"])
	if !ok {
		return adRow{}, "订单数不正确"
	}
	gmv, ok := optScaled(m["gmv"], 100, false)
	if !ok {
		return adRow{}, "成交金额不正确"
	}
	return adRow{ListingID: lid, Date: date, Campaign: campaign, Keyword: keyword, Cost: cost, Impressions: impr, Clicks: clicks, Orders: orders, GMV: gmv}, ""
}
