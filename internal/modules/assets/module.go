package assets

import (
	"net/http"
	"sync"
	"xstudio/internal/core"
	"xstudio/internal/media"
)

type Module struct {
	*core.Core
	aiUsers         sync.Map
	backfillOnce    sync.Once
	cleanupOnce     sync.Once
	workbenchFiles  sync.Map
	workbenchFileMu sync.Mutex
}

func New(c *core.Core) *Module { return &Module{Core: c} }
func (*Module) Name() string   { return "assets" }
func (a *Module) Jobs() map[string]core.JobHandler {
	return map[string]core.JobHandler{"assets.workbench": a.executeWorkbench, "assets.tag": a.executeTag, "assets.process": a.executeProcess}
}
func (a *Module) Routes(r *core.Router) {
	a.startHashBackfill()
	a.startWorkbenchCleanup()
	a.workbenchRoutes(r)
	r.Handle("GET /api/presets", core.AssetUpload, a.presets)
	r.Handle("POST /api/presets", core.AssetUpload, a.createPreset)
	r.Handle("PATCH /api/presets/{id}", core.Scoped, a.patchPreset)
	r.Handle("DELETE /api/presets/{id}", core.Scoped, a.deletePreset)
	r.Handle("POST /api/assets/process", core.Scoped, a.process)
	r.Handle("POST /api/assets/batch", core.Scoped, a.batch)
	r.Handle("POST /api/assets/download", core.ExportRun, a.download)
	r.Handle("POST /api/assets/{pid}/similar", core.ProductView, a.similar)
	r.Handle("POST /api/assets/tag", core.AssetUpload, a.tag)
	r.Handle("GET /api/assets", core.ProductView, a.library)
	r.Handle("GET /api/assets/{pid}", core.ProductView, a.detail)
	r.Handle("GET /api/products/{id}/assets", core.ProductView, a.list)
	r.Handle("POST /api/products/{id}/assets", core.AssetUpload, a.upload)
	r.Handle("PATCH /api/assets/{pid}", core.AssetUpload, a.patchPhoto)
	r.Handle("DELETE /api/assets/{pid}", core.Scoped, a.deletePhoto)
}

type Object = core.Object
type Photo = core.Photo

var js = core.JS
var now = core.Now
var fail = core.Fail
var writeJSON = core.WriteJSON
var decode = core.Decode
var idParam = core.ID
var currentUser = core.CurrentUser
var scanPhoto = core.ScanPhoto

const photoColumns = core.PhotoColumns

var validatePhoto = media.ValidatePhoto

func (a *Module) authorizedProduct(w http.ResponseWriter, r *http.Request, id int64, write bool) (core.Product, bool) {
	if !a.RequireProduct(w, r, id, write) {
		return core.Product{}, false
	}
	if !a.CanUpload(core.CurrentUser(r), id) {
		core.Fail(w, 403, "此商品不允许上传或修改素材")
		return core.Product{}, false
	}
	p, e := a.Product(id)
	return p, e == nil
}
func (a *Module) list(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !a.RequireProduct(w, r, id, false) {
		return
	}
	q := "SELECT " + core.PhotoColumns + " FROM assets WHERE product_id=?"
	args := []any{id}
	for _, key := range []string{"kind", "code"} {
		if s := r.URL.Query().Get(key); s != "" {
			q += " AND " + key + "=?"
			args = append(args, s)
		}
	}
	rows, e := a.DB.Query(q+" ORDER BY id", args...)
	if e != nil {
		core.Fail(w, 500, "读取素材失败")
		return
	}
	items := []Photo{}
	for rows.Next() {
		p, e := core.ScanPhoto(rows)
		if e != nil {
			rows.Close()
			core.Fail(w, 500, "读取素材失败")
			return
		}
		items = append(items, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		core.Fail(w, 500, "读取素材失败")
		return
	}
	core.PageItems(w, r, items)
}
