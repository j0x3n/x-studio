package market

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"xstudio/internal/core"
)

const maxImportRows = 5000
const maxImportBytes = 8 << 20

type previewEntry struct {
	UserID int64
	ShopID int64
	Kind   string
	Raw    json.RawMessage
	Hash   string
	Total  int
	Prep   prepared
	At     time.Time
}

func (m *Module) ttl() time.Duration {
	if m.previewTTL > 0 {
		return m.previewTTL
	}
	return 30 * time.Minute
}

func (m *Module) importPreview(w http.ResponseWriter, r *http.Request) {
	body, ok := readLimit(w, r, maxImportBytes)
	if !ok {
		return
	}
	var in struct {
		ShopID int64           `json:"shop_id"`
		Kind   string          `json:"kind"`
		Rows   json.RawMessage `json:"rows"`
	}
	if !decodeJSON(w, body, &in) {
		return
	}
	if in.Kind != "my_listing" && in.Kind != "ad" {
		core.Fail(w, 400, "导入只支持我的商品或推广数据")
		return
	}
	if !m.requireShop(w, r, in.ShopID) {
		return
	}
	if len(in.Rows) == 0 || strings.TrimSpace(string(in.Rows)) == "null" {
		core.Fail(w, 400, "缺少 rows")
		return
	}
	if len(in.Rows) > maxIngestBytes {
		core.Fail(w, 400, "数据超过 2MB")
		return
	}
	var rows []json.RawMessage
	if json.Unmarshal(in.Rows, &rows) != nil {
		core.Fail(w, 400, "rows 必须是数组")
		return
	}
	if len(rows) > maxImportRows {
		core.Fail(w, 400, "一次最多 5000 行")
		return
	}
	idx, err := loadListings(m.DB, in.ShopID)
	if err != nil {
		core.Fail(w, 500, "读取上架版本失败")
		return
	}
	prep := parseItems(idx, in.Kind, "", chinaToday(), chinaToday(), rows)
	id, err := m.putPreview(&previewEntry{
		UserID: core.CurrentUser(r).ID,
		ShopID: in.ShopID,
		Kind:   in.Kind,
		Raw:    append(json.RawMessage(nil), in.Rows...),
		Hash:   payloadHash(in.Kind, "", in.Rows),
		Total:  len(rows),
		Prep:   prep,
		At:     time.Now(),
	})
	if err != nil {
		core.Fail(w, 500, "保存预览失败")
		return
	}
	sample := prep.Sample
	if sample == nil {
		sample = []core.Object{}
	}
	errors := prep.Skipped
	if errors == nil {
		errors = []skipRow{}
	}
	core.WriteJSON(w, 200, core.Object{"preview_id": id, "total": len(rows), "ok": prep.okCount(), "errors": errors, "sample": sample})
}

func (m *Module) putPreview(p *previewEntry) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf)
	m.previewMu.Lock()
	defer m.previewMu.Unlock()
	if m.previews == nil {
		m.previews = map[string]*previewEntry{}
	}
	now := time.Now()
	for k, v := range m.previews {
		if now.Sub(v.At) > m.ttl() {
			delete(m.previews, k)
		}
	}
	m.previews[id] = p
	return id, nil
}

func (m *Module) popPreview(id string, userID int64) *previewEntry {
	m.previewMu.Lock()
	defer m.previewMu.Unlock()
	p := m.previews[id]
	if p == nil {
		return nil
	}
	if time.Since(p.At) > m.ttl() {
		delete(m.previews, id)
		return nil
	}
	if p.UserID != userID {
		return nil
	}
	delete(m.previews, id)
	return p
}

func (m *Module) restorePreview(id string, p *previewEntry) {
	m.previewMu.Lock()
	defer m.previewMu.Unlock()
	if m.previews == nil {
		m.previews = map[string]*previewEntry{}
	}
	if _, ok := m.previews[id]; ok {
		return
	}
	m.previews[id] = p
}

func (m *Module) importConfirm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PreviewID string `json:"preview_id"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.PreviewID) == "" {
		core.Fail(w, 400, "缺少预览")
		return
	}
	entry := m.popPreview(in.PreviewID, core.CurrentUser(r).ID)
	if entry == nil {
		core.Fail(w, 409, "预览已过期或已确认")
		return
	}
	if !m.requireShop(w, r, entry.ShopID) {
		m.restorePreview(in.PreviewID, entry)
		return
	}
	resp, err := m.saveIngest(r, entry.ShopID, entry.Kind, "csv", "", core.Now(), entry.Hash, entry.Total, string(entry.Raw), entry.Prep)
	if err != nil {
		m.restorePreview(in.PreviewID, entry)
		log.Print("market import: ", err)
		core.Fail(w, 500, "保存导入数据失败")
		return
	}
	if !resp.Duplicate {
		m.scheduleDiagnose(entry.ShopID)
	}
	core.WriteJSON(w, 200, resp)
}
