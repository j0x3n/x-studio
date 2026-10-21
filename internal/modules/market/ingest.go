package market

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"xstudio/internal/core"
)

const ingestPerMinute = 60
const maxIngestBytes = 2 << 20
const maxIngestItems = 2000

type ingestResponse struct {
	SnapshotID int64     `json:"snapshot_id"`
	Duplicate  bool      `json:"duplicate"`
	Inserted   int       `json:"inserted"`
	Updated    int       `json:"updated"`
	Skipped    []skipRow `json:"skipped"`
}

func payloadHash(kind, category string, raw json.RawMessage) string {
	h := sha256.New()
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(category))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

func argInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func (m *Module) allowIngest(key string) bool {
	m.limitMu.Lock()
	defer m.limitMu.Unlock()
	if m.hits == nil {
		m.hits = map[string][]time.Time{}
	}
	now := time.Now()
	cut := now.Add(-time.Minute)
	kept := make([]time.Time, 0, len(m.hits[key])+1)
	for _, ts := range m.hits[key] {
		if ts.After(cut) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= ingestPerMinute {
		m.hits[key] = kept
		return false
	}
	m.hits[key] = append(kept, now)
	return true
}

func (m *Module) limitKey(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return "t:" + hashToken(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
	}
	return "u:" + strconv.FormatInt(core.CurrentUser(r).ID, 10)
}

func readLimit(w http.ResponseWriter, r *http.Request, n int64) ([]byte, bool) {
	b, err := io.ReadAll(io.LimitReader(r.Body, n+1))
	if err != nil {
		core.Fail(w, 400, "读取请求失败")
		return nil, false
	}
	if int64(len(b)) > n {
		core.Fail(w, 400, "请求体过大")
		return nil, false
	}
	return b, true
}

func decodeJSON(w http.ResponseWriter, b []byte, v any) bool {
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(v); err != nil {
		core.Fail(w, 400, "JSON 格式或字段类型不正确")
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		core.Fail(w, 400, "只能提交一个 JSON 值")
		return false
	}
	return true
}

func knownKind(kind string) bool {
	switch kind {
	case "keyword", "search", "item", "my_listing", "ad":
		return true
	default:
		return false
	}
}

func (m *Module) ingest(w http.ResponseWriter, r *http.Request) {
	body, ok := readLimit(w, r, maxIngestBytes)
	if !ok {
		return
	}
	var in struct {
		ShopID     int64           `json:"shop_id"`
		Kind       string          `json:"kind"`
		SourceURL  string          `json:"source_url"`
		CapturedAt string          `json:"captured_at"`
		Category   string          `json:"category"`
		Items      json.RawMessage `json:"items"`
	}
	if !decodeJSON(w, body, &in) {
		return
	}
	if !m.requireShop(w, r, in.ShopID) {
		return
	}
	if !knownKind(in.Kind) {
		core.Fail(w, 400, "不支持的数据类型")
		return
	}
	category := strings.TrimSpace(in.Category)
	if utf8.RuneCountInString(category) > 80 {
		core.Fail(w, 400, "品类过长")
		return
	}
	sourceURL := strings.TrimSpace(in.SourceURL)
	if utf8.RuneCountInString(sourceURL) > 2000 {
		core.Fail(w, 400, "来源地址过长")
		return
	}
	batchDate, okDate := dateFromCaptured(in.CapturedAt)
	if !okDate {
		core.Fail(w, 400, "采集时间不正确或晚于今天")
		return
	}
	if len(in.Items) == 0 || strings.TrimSpace(string(in.Items)) == "null" {
		core.Fail(w, 400, "缺少 items")
		return
	}
	var items []json.RawMessage
	if json.Unmarshal(in.Items, &items) != nil {
		core.Fail(w, 400, "items 必须是数组")
		return
	}
	if len(items) > maxIngestItems {
		core.Fail(w, 400, "一次最多 2000 条")
		return
	}
	if !m.allowIngest(m.limitKey(r)) {
		core.Fail(w, 429, "采集过于频繁，请一分钟后再试")
		return
	}
	resp, err := m.accept(r, in.ShopID, in.Kind, "script", sourceURL, strings.TrimSpace(in.CapturedAt), category, batchDate, in.Items, items, string(body))
	if err != nil {
		log.Print("market ingest: ", err)
		core.Fail(w, 500, "保存采集数据失败")
		return
	}
	m.touchToken(r)
	core.WriteJSON(w, 200, resp)
}

func (m *Module) accept(r *http.Request, shopID int64, kind, source, sourceURL, capturedAt, category, batchDate string, rawItems json.RawMessage, items []json.RawMessage, raw string) (ingestResponse, error) {
	hash := payloadHash(kind, category, rawItems)
	if id, ok, err := m.findSnapshot(shopID, kind, hash); err != nil {
		return ingestResponse{}, err
	} else if ok {
		return ingestResponse{SnapshotID: id, Duplicate: true, Skipped: []skipRow{}}, nil
	}
	idx, err := loadListings(m.DB, shopID)
	if err != nil {
		return ingestResponse{}, err
	}
	prep := parseItems(idx, kind, category, batchDate, chinaToday(), items)
	if capturedAt == "" {
		capturedAt = core.Now()
	}
	resp, err := m.saveIngest(r, shopID, kind, source, sourceURL, capturedAt, hash, len(items), raw, prep)
	if err != nil {
		return ingestResponse{}, err
	}
	if !resp.Duplicate {
		m.scheduleDiagnose(shopID)
	}
	return resp, nil
}

func (m *Module) findSnapshot(shopID int64, kind, hash string) (int64, bool, error) {
	var id int64
	err := m.DB.QueryRow("SELECT id FROM market_snapshots WHERE shop_id=? AND kind=? AND payload_hash=?", shopID, kind, hash).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (m *Module) saveIngest(r *http.Request, shopID int64, kind, source, sourceURL, capturedAt, hash string, total int, raw string, prep prepared) (ingestResponse, error) {
	if len(raw) > maxIngestBytes {
		return ingestResponse{}, errTooBig
	}
	tx, err := m.DB.Begin()
	if err != nil {
		return ingestResponse{}, err
	}
	defer tx.Rollback()
	var existing int64
	err = tx.QueryRow("SELECT id FROM market_snapshots WHERE shop_id=? AND kind=? AND payload_hash=?", shopID, kind, hash).Scan(&existing)
	if err == nil {
		return ingestResponse{SnapshotID: existing, Duplicate: true, Skipped: []skipRow{}}, nil
	}
	if err != sql.ErrNoRows {
		return ingestResponse{}, err
	}
	now := core.Now()
	res, err := tx.Exec(`INSERT INTO market_snapshots(shop_id, kind, source, source_url, captured_at, payload_hash, "rows", raw, created_by, created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		shopID, kind, source, sourceURL, capturedAt, hash, total, raw, core.CurrentUser(r).ID, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			if tx.QueryRow("SELECT id FROM market_snapshots WHERE shop_id=? AND kind=? AND payload_hash=?", shopID, kind, hash).Scan(&existing) == nil {
				return ingestResponse{SnapshotID: existing, Duplicate: true, Skipped: []skipRow{}}, nil
			}
		}
		return ingestResponse{}, err
	}
	snap, _ := res.LastInsertId()
	inserted, updated := 0, 0
	switch kind {
	case "keyword":
		inserted, updated, err = applyKeywords(tx, shopID, snap, prep.Keywords, now)
	case "search", "item":
		inserted, updated, err = applyCompetitors(tx, shopID, snap, prep.Competitors)
	case "my_listing":
		inserted, updated, err = applyDaily(tx, snap, source, prep.Daily)
	case "ad":
		inserted, updated, err = applyAds(tx, shopID, snap, prep.Ads)
	}
	if err != nil {
		return ingestResponse{}, err
	}
	skipped := prep.Skipped
	if skipped == nil {
		skipped = []skipRow{}
	}
	if err = core.AuditTx(tx, r, "market."+sourceAction(source), "shop", shopID, core.Object{"snapshot_id": snap, "kind": kind, "inserted": inserted, "updated": updated, "skipped": len(skipped)}); err != nil {
		return ingestResponse{}, err
	}
	if err = tx.Commit(); err != nil {
		return ingestResponse{}, err
	}
	return ingestResponse{SnapshotID: snap, Inserted: inserted, Updated: updated, Skipped: skipped}, nil
}

func sourceAction(source string) string {
	if source == "csv" {
		return "import"
	}
	return "ingest"
}

var errTooBig = errString("数据超过 2MB")

type errString string

func (e errString) Error() string { return string(e) }

func applyKeywords(tx *sql.Tx, shopID, snap int64, rows []keywordRow, now string) (int, int, error) {
	ins, upd := 0, 0
	for _, row := range rows {
		var id int64
		err := tx.QueryRow("SELECT id FROM market_keywords WHERE shop_id=? AND word=? AND date=?", shopID, row.Word, row.Date).Scan(&id)
		if err == sql.ErrNoRows {
			_, err = tx.Exec(`INSERT INTO market_keywords(shop_id, category, word, date, search_volume, clicks, ctr_bps, cvr_bps, competition, snapshot_id, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
				shopID, row.Category, row.Word, row.Date, argInt(row.Volume), argInt(row.Clicks), argInt(row.CTR), argInt(row.CVR), argInt(row.Comp), snap, now)
			if err != nil {
				return 0, 0, err
			}
			ins++
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		if _, err = tx.Exec("UPDATE market_keywords SET category=?, search_volume=?, clicks=?, ctr_bps=?, cvr_bps=?, competition=?, snapshot_id=?, updated_at=? WHERE id=?",
			row.Category, argInt(row.Volume), argInt(row.Clicks), argInt(row.CTR), argInt(row.CVR), argInt(row.Comp), snap, now, id); err != nil {
			return 0, 0, err
		}
		upd++
	}
	return ins, upd, nil
}

func applyCompetitors(tx *sql.Tx, shopID, snap int64, rows []competitorRow) (int, int, error) {
	ins, upd := 0, 0
	for _, row := range rows {
		var id int64
		err := tx.QueryRow("SELECT id FROM competitor_items WHERE shop_id=? AND platform_item_id=? AND keyword=? AND date=?", shopID, row.ItemID, row.Keyword, row.Date).Scan(&id)
		if err == sql.ErrNoRows {
			_, err = tx.Exec(`INSERT INTO competitor_items(shop_id, platform_item_id, keyword, rank, title, price_cents, sales_text, sales, shop_name, image_url, material, params, date, snapshot_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				shopID, row.ItemID, row.Keyword, argInt(row.Rank), row.Title, argInt(row.Price), row.SalesText, argInt(row.Sales), row.ShopName, row.ImageURL, row.Material, row.Params, row.Date, snap)
			if err != nil {
				return 0, 0, err
			}
			ins++
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		if _, err = tx.Exec("UPDATE competitor_items SET rank=?, title=?, price_cents=?, sales_text=?, sales=?, shop_name=?, image_url=?, material=?, params=?, snapshot_id=? WHERE id=?",
			argInt(row.Rank), row.Title, argInt(row.Price), row.SalesText, argInt(row.Sales), row.ShopName, row.ImageURL, row.Material, row.Params, snap, id); err != nil {
			return 0, 0, err
		}
		upd++
	}
	return ins, upd, nil
}

func applyDaily(tx *sql.Tx, snap int64, source string, rows []dailyRow) (int, int, error) {
	ins, upd := 0, 0
	for _, row := range rows {
		var id int64
		err := tx.QueryRow("SELECT id FROM listing_daily WHERE listing_id=? AND date=?", row.ListingID, row.Date).Scan(&id)
		if err == sql.ErrNoRows {
			_, err = tx.Exec(`INSERT INTO listing_daily(listing_id, date, impressions, clicks, add_cart, orders, gmv_cents, uv, search_terms, source, snapshot_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
				row.ListingID, row.Date, argInt(row.Impressions), argInt(row.Clicks), argInt(row.AddCart), argInt(row.Orders), argInt(row.GMV), argInt(row.UV), row.Terms, source, snap)
			if err != nil {
				return 0, 0, err
			}
			ins++
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		if _, err = tx.Exec("UPDATE listing_daily SET impressions=?, clicks=?, add_cart=?, orders=?, gmv_cents=?, uv=?, search_terms=?, source=?, snapshot_id=? WHERE id=?",
			argInt(row.Impressions), argInt(row.Clicks), argInt(row.AddCart), argInt(row.Orders), argInt(row.GMV), argInt(row.UV), row.Terms, source, snap, id); err != nil {
			return 0, 0, err
		}
		upd++
	}
	return ins, upd, nil
}

func applyAds(tx *sql.Tx, shopID, snap int64, rows []adRow) (int, int, error) {
	ins, upd := 0, 0
	for _, row := range rows {
		key := int64(0)
		if row.ListingID != nil {
			key = *row.ListingID
		}
		var id int64
		err := tx.QueryRow("SELECT id FROM ad_daily WHERE shop_id=? AND date=? AND campaign=? AND keyword=? AND coalesce(listing_id,0)=?", shopID, row.Date, row.Campaign, row.Keyword, key).Scan(&id)
		if err == sql.ErrNoRows {
			_, err = tx.Exec(`INSERT INTO ad_daily(shop_id, listing_id, date, campaign, keyword, cost_cents, impressions, clicks, orders, gmv_cents, snapshot_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
				shopID, argInt(row.ListingID), row.Date, row.Campaign, row.Keyword, argInt(row.Cost), argInt(row.Impressions), argInt(row.Clicks), argInt(row.Orders), argInt(row.GMV), snap)
			if err != nil {
				return 0, 0, err
			}
			ins++
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		if _, err = tx.Exec("UPDATE ad_daily SET cost_cents=?, impressions=?, clicks=?, orders=?, gmv_cents=?, snapshot_id=? WHERE id=?",
			argInt(row.Cost), argInt(row.Impressions), argInt(row.Clicks), argInt(row.Orders), argInt(row.GMV), snap, id); err != nil {
			return 0, 0, err
		}
		upd++
	}
	return ins, upd, nil
}
