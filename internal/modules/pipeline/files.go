package pipeline

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

func serveRoot(w http.ResponseWriter, r *http.Request, root, name string) {
	core.ServeRoot(w, r, root, name)
}
func (a *Module) files(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	if !a.RequireProduct(w, r, id, false) {
		return
	}
	name := r.PathValue("path")
	if !hasPrefix(name, "原图/", "照片/") {
		fail(w, 404, "文件不存在")
		return
	}
	if a.serveThumbnail(w, r, id, name) {
		return
	}
	serveRoot(w, r, a.Dir(id), name)
}
func (a *Module) listingFiles(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	if !a.RequireListing(w, r, id, false) {
		return
	}
	if !hasPrefix(r.PathValue("path"), "导出/") {
		fail(w, 404, "文件不存在")
		return
	}
	if a.serveThumbnailRoot(w, r, a.ListingDir(id), r.PathValue("path")) {
		return
	}
	serveRoot(w, r, a.ListingDir(id), r.PathValue("path"))
}
func (a *Module) download(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	if !a.RequireListing(w, r, id, false) {
		return
	}
	if _, ok := labelOption(w, r); !ok {
		return
	}
	var label bool
	if a.DB.QueryRow("SELECT ai_label FROM listings WHERE id=?", id).Scan(&label) != nil {
		fail(w, 404, "上架版本不存在")
		return
	}
	if _, e := os.Stat(filepath.Join(a.ListingDir(id), "导出.zip")); e != nil {
		fail(w, 404, "导出文件不存在")
		return
	}
	if label {
		w.Header().Set("X-AI-Label", "1")
	} else {
		w.Header().Set("X-AI-Label", "0")
	}
	action := "export.run"
	if !label || r.URL.Query().Get("ai_label") == "0" {
		action = "export.no_ai_label"
	}
	a.Audit(r, action, "listing", id, Object{"ai_label": label, "stage": "download"})
	w.Header().Set("Content-Disposition", `attachment; filename="export.zip"; filename*=UTF-8''%E5%AF%BC%E5%87%BA.zip`)
	serveRoot(w, r, a.ListingDir(id), "导出.zip")
}
func (a *Module) renderRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
	name := r.PathValue("path")
	parts := strings.Split(name, "/")
	token := a.RenderAuth(w, r)
	if len(parts) >= 4 && parts[0] == "详情页模板" && parts[1] == "商品" {
		id, e := strconv.ParseInt(parts[2], 10, 64)
		if e != nil || id < 1 {
			fail(w, 404, "上架版本不存在")
			return
		}
		if !token && !a.RequireListing(w, r, id, false) {
			return
		}
		p, e := a.PipelineProduct(id)
		if e != nil {
			fail(w, 404, "上架版本不存在")
			return
		}
		if len(parts) == 4 && (parts[3] == "内容.js" || parts[3] == "主图.js") {
			if !token && !currentUser(r).Has(core.CopyView) {
				fail(w, 403, "没有查看文案的权限")
				return
			}
			_ = a.DB.QueryRow("SELECT render_ai_label FROM listings WHERE id=?", id).Scan(&p.AILabel)
			if e = a.annotateForExport(p); e != nil {
				fail(w, 500, "读取素材来源失败")
				return
			}
			normalizeCopy(p.Content, p.Main, p.Listing)
			v, key := p.Content, "PRODUCT"
			if parts[3] == "主图.js" {
				v, key = p.Main, "MAIN"
			}
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			_, _ = fmt.Fprintf(w, "window.%s = %s;", key, js(v))
			return
		}
		if len(parts) == 5 && parts[3] == "照片" {
			serveRoot(w, r, filepath.Join(a.Dir(p.ID), "照片"), parts[4])
			return
		}
		fail(w, 404, "渲染文件不存在")
		return
	}
	if len(parts) == 2 && (parts[0] == "主图模板" || parts[0] == "详情页模板") && validFilePath(parts[1]) && !strings.Contains(parts[1], "..") {
		ext := strings.ToLower(filepath.Ext(parts[1]))
		if contentType, ok := map[string]string{
			".js":    "application/javascript; charset=utf-8",
			".css":   "text/css; charset=utf-8",
			".png":   "image/png",
			".jpg":   "image/jpeg",
			".svg":   "image/svg+xml",
			".woff2": "font/woff2",
		}[ext]; ok {
			w.Header().Set("Content-Type", contentType)
			serveRoot(w, r, a.Cfg.TemplateDir, name)
			return
		}
		if ext != ".html" {
			fail(w, 404, "渲染文件不存在")
			return
		}
		id, _ := strconv.ParseInt(r.URL.Query().Get("p"), 10, 64)
		if id < 1 {
			fail(w, 404, "上架版本不存在")
			return
		}
		if !token && !a.RequireListing(w, r, id, false) {
			return
		}
		if !token && !currentUser(r).Has(core.CopyView) {
			fail(w, 403, "没有查看文案的权限")
			return
		}
		a.serveTemplate(w, r, name)
		return
	}
	if len(parts) == 4 && strings.Join(parts[:3], "/") == "店铺/店标/矢量标志" && strings.HasSuffix(parts[3], ".png") {
		serveRoot(w, r, a.Cfg.TemplateDir, name)
		return
	}
	fail(w, 404, "渲染文件不存在")
}
func RemovePhotoOutputs(c *core.Core, id int64, code string) {
	if code == "" || !codePattern.MatchString(code) {
		return
	}
	for _, ext := range []string{".jpg", "_白底.jpg", "_透明.png"} {
		_ = os.Remove(filepath.Join(c.Dir(id), "照片", code+ext))
	}
}
