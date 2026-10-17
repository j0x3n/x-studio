package pipeline

import (
	"context"

	"image"
	"net/http"
	"xstudio/internal/core"
)

type Module struct {
	*core.Core
	browser *Browser
}

func New(c *core.Core) *Module {
	a := &Module{Core: c, browser: NewBrowser(c.Cfg.ChromePath)}
	c.PrepareListing = func(p core.Product) (core.Product, error) { return CleanListing(c, p) }
	c.AssetCoverage = a.coverage
	c.OnClose(a.browser.Close)
	return a
}
func (*Module) Name() string                 { return "pipeline" }
func (*Module) Migrations() []core.Migration { return nil }
func (a *Module) Jobs() map[string]core.JobHandler {
	return map[string]core.JobHandler{"pipeline": a.execute}
}
func (a *Module) Routes(r *core.Router) {
	r.Handle("POST /api/listings/{id}/run", core.RenderRun, a.run)
	r.Handle("GET /api/listings/{id}/download", core.ExportRun, a.download)
	r.Handle("GET /api/jobs/{jid}", core.Scoped, a.getJob)
	r.Handle("GET /api/slots", core.LoggedIn, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(a.Slots)
	})
	r.Handle("GET /api/codes", core.LoggedIn, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, Object{"codes": codes}) })
	r.Raw("GET /files/products/{id}/{path...}", a.files)
	r.Raw("GET /files/listings/{id}/{path...}", a.listingFiles)
	r.Raw("GET /render/{path...}", a.renderRoute)
}

type Object = core.Object
type Product = core.Product
type Photo = core.Photo
type Job = core.Job
type Config = core.Config

var js = core.JS
var now = core.Now
var writeJSON = core.WriteJSON
var fail = core.Fail
var idParam = core.ID
var hasPrefix = core.HasPrefix
var currentUser = core.CurrentUser
var scanJob = core.ScanJob
var scanPhoto = core.ScanPhoto

const photoColumns = core.PhotoColumns

func (a *Module) photos(id int64) ([]Photo, error) { return a.Photos(id) }
func (a *Module) safeAIError(s string) string      { return a.SafeAIError(s) }
func (a *Module) aiJSON(ctx context.Context, model, prompt string, content, out any) error {
	return a.AIJSON(ctx, model, prompt, content, out)
}
func (a *Module) editImage(ctx context.Context, prompt string, refs []image.Image) ([]byte, error) {
	return a.EditImage(ctx, prompt, refs)
}
func (a *Module) getBaseURL() string                       { return a.BaseURL() }
func (a *Module) renderToken() string                      { return a.RenderToken() }
func (a *Module) removePhotoOutputs(id int64, code string) { RemovePhotoOutputs(a.Core, id, code) }

func CleanListing(c *core.Core, p Product) (Product, error) {
	a := &Module{Core: c}
	p.Main = cleanMain(p.Main, c.Dir(p.ID)+"/照片")
	if e := a.annotateAI(p.ID, p.Content, p.Main); e != nil {
		return p, e
	}
	if e := validateCopy(p.Content, p.Main, p.Listing); e != nil {
		return p, e
	}
	p.Content = cleanObject(p.Content, c.Dir(p.ID)+"/照片")
	if words, e := a.prompt("违禁词.txt"); e == nil {
		p.Listing["compliance"] = appendCompliance(p.Content, p.Main, p.Listing, words)
	}
	return p, nil
}
