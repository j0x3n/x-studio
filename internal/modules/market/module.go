// Package market 是数据与运营模块：采集数据接收、报表导入、关键词和竞品、
// 诊断规则和运营建议、个人采集令牌。
package market

import (
	"net/http"
	"sync"
	"time"

	"xstudio/internal/core"
)

type Module struct {
	*core.Core
	mu         sync.Mutex
	limitMu    sync.Mutex
	hits       map[string][]time.Time
	previewMu  sync.Mutex
	previews   map[string]*previewEntry
	previewTTL time.Duration
}

func New(c *core.Core) *Module {
	return &Module{Core: c, hits: map[string][]time.Time{}, previews: map[string]*previewEntry{}}
}
func (*Module) Name() string                     { return "market" }
func (*Module) Jobs() map[string]core.JobHandler { return nil }

func (m *Module) Routes(r *core.Router) {
	m.TokenAuth = m.authenticateToken
	g := m.guard
	r.Handle("POST /api/market/ingest", core.MarketImport, g(m.ingest))
	r.Handle("POST /api/market/import/preview", core.MarketImport, g(m.importPreview))
	r.Handle("POST /api/market/import/confirm", core.MarketImport, g(m.importConfirm))
	r.Handle("GET /api/market/keywords", core.MarketView, g(m.listKeywords))
	r.Handle("GET /api/market/competitors", core.MarketView, g(m.listCompetitors))
	r.Handle("GET /api/market/listings/{id}/daily", core.MarketView, g(m.listingDaily))
	r.Handle("GET /api/market/ads", core.MarketView, g(m.listAds))
	r.Handle("GET /api/market/snapshots", core.MarketView, g(m.listSnapshots))
	r.Handle("GET /api/market/snapshots/{id}", core.MarketView, g(m.getSnapshot))
	r.Handle("GET /api/market/shops/{id}/settings", core.MarketView, g(m.getShopSettings))
	r.Handle("PUT /api/market/shops/{id}/settings", core.MarketImport, g(m.putShopSettings))
	r.Handle("GET /api/market/tokens", core.MarketImport, g(m.listTokens))
	r.Handle("POST /api/market/tokens", core.MarketImport, g(m.createToken))
	r.Handle("DELETE /api/market/tokens/{id}", core.MarketImport, g(m.revokeToken))
}

func (m *Module) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := core.CurrentUser(r)
		if u.ID == 0 {
			core.Fail(w, 401, "请先登录")
			return
		}
		if u.Role.Scope == "shares" {
			core.Fail(w, 403, "没有此操作的权限")
			return
		}
		h(w, r)
	}
}

func (m *Module) requireShop(w http.ResponseWriter, r *http.Request, id int64) bool {
	if id < 1 {
		core.Fail(w, 400, "店铺不正确")
		return false
	}
	var n int
	if err := m.DB.QueryRow("SELECT count(*) FROM shops WHERE id=?", id).Scan(&n); err != nil {
		core.Fail(w, 500, "读取店铺失败")
		return false
	}
	if n == 0 {
		core.Fail(w, 404, "店铺不存在")
		return false
	}
	u := core.CurrentUser(r)
	if !m.CanShop(u, id) {
		core.Fail(w, 403, "没有此店铺的权限")
		return false
	}
	return true
}

func (m *Module) scheduleDiagnose(shopID int64) {}
