package app

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

func serveRoot(w http.ResponseWriter, r *http.Request, root, name string) {
	// os.Root also blocks symlink escapes, including Windows junctions.
	if !validFilePath(name) {
		fail(w, 400, "文件路径不合法")
		return
	}
	rt, e := os.OpenRoot(root)
	if e != nil {
		fail(w, 404, "文件不存在")
		return
	}
	defer rt.Close()
	f, e := rt.Open(name)
	if e != nil {
		fail(w, 404, "文件不存在")
		return
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		fail(w, 404, "文件不存在")
		return
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}
func (a *App) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405, "请求方法不正确")
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}
	if !fs.ValidPath(p) || strings.ContainsAny(p, "\\:") {
		fail(w, 404, "页面不存在")
		return
	}
	if strings.Contains(p, "/") && !hasPrefix(p, "css/", "js/") {
		fail(w, 404, "页面不存在")
		return
	}
	b, e := fs.ReadFile(a.web, p)
	if e != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(404)
		_, _ = w.Write([]byte("<!doctype html><meta charset=utf-8><title>页面待同步</title><p>前端页面尚未同步，后端服务已启动。请使用 API 或同步前端后重新编译。</p>"))
		return
	}
	if strings.HasSuffix(p, ".css") {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	} else if strings.HasSuffix(p, ".js") {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	_, _ = w.Write(b)
}
func (a *App) files(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	if _, ok := a.authorizedProduct(w, r, id, false); !ok {
		return
	}
	name := r.PathValue("path")
	if !hasPrefix(name, "原图/", "照片/", "导出/") {
		fail(w, 404, "文件不存在")
		return
	}
	if a.serveThumbnail(w, r, id, name) {
		return
	}
	serveRoot(w, r, a.dir(id), name)
}
func (a *App) download(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	if _, ok := a.authorizedProduct(w, r, id, false); !ok {
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="export.zip"; filename*=UTF-8''%E5%AF%BC%E5%87%BA.zip`)
	serveRoot(w, r, a.dir(id), "导出.zip")
}
func (a *App) renderRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
	name := r.PathValue("path")
	parts := strings.Split(name, "/")
	if len(parts) >= 4 && parts[0] == "详情页模板" && parts[1] == "商品" {
		id, e := strconv.ParseInt(parts[2], 10, 64)
		if e != nil || id < 1 {
			fail(w, 404, "款式不存在")
			return
		}
		p, e := a.product(id)
		if e != nil {
			fail(w, 404, "款式不存在")
			return
		}
		if len(parts) == 4 && (parts[3] == "内容.js" || parts[3] == "主图.js") {
			if e := a.annotateAI(id, p.Content, p.Main); e != nil {
				fail(w, 500, "读取照片标记失败")
				return
			}
			v := p.Content
			key := "PRODUCT"
			if parts[3] == "主图.js" {
				v = p.Main
				key = "MAIN"
			}
			normalizeCopy(p.Content, p.Main, p.Listing)
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			_, _ = fmt.Fprintf(w, "window.%s = %s;", key, js(v))
			return
		}
		if len(parts) == 5 && parts[3] == "照片" {
			serveRoot(w, r, filepath.Join(a.dir(id), "照片"), parts[4])
			return
		}
		fail(w, 404, "渲染文件不存在")
		return
	}
	if name == "主图模板/模板.html" || name == "详情页模板/模板.html" {
		serveRoot(w, r, a.cfg.TemplateDir, name)
		return
	}
	if len(parts) == 4 && strings.Join(parts[:3], "/") == "店铺/店标/矢量标志" && strings.HasSuffix(parts[3], ".png") {
		serveRoot(w, r, a.cfg.TemplateDir, name)
		return
	}
	fail(w, 404, "渲染文件不存在")
}
func (a *App) outputs(id int64) Object {
	o := Object{"main34": []string{}, "main11": []string{}, "detail": []string{}, "white": "", "zip": ""}
	for key, dir := range map[string]string{"main34": "主图/3x4", "main11": "主图/1x1", "detail": "详情页"} {
		files, _ := filepath.Glob(filepath.Join(a.dir(id), "导出", filepath.FromSlash(dir), "*.jpg"))
		urls := []string{}
		for _, f := range files {
			urls = append(urls, fmt.Sprintf("/files/products/%d/导出/%s/%s", id, dir, path.Base(filepath.ToSlash(f))))
		}
		o[key] = urls
	}
	if st, e := os.Stat(filepath.Join(a.dir(id), "导出", "白底.jpg")); e == nil && st.Mode().IsRegular() {
		o["white"] = fmt.Sprintf("/files/products/%d/导出/白底.jpg", id)
	}
	if _, e := os.Stat(filepath.Join(a.dir(id), "导出.zip")); e == nil {
		o["zip"] = fmt.Sprintf("/api/products/%d/download", id)
	}
	return o
}

type slot struct {
	Position   any      `json:"位,omitempty"`
	Screen     any      `json:"屏,omitempty"`
	Name       string   `json:"名"`
	Candidates []string `json:"候选"`
	Required   bool     `json:"必需"`
	Minimum    int      `json:"最少,omitempty"`
	Note       string   `json:"备注,omitempty"`
}

func (a *App) coverage(photos []Photo) Object {
	var input map[string]json.RawMessage
	_ = json.Unmarshal(a.slots, &input)
	out := Object{}
	need := []Object{}
	for key, cn := range map[string]string{"main": "主图", "detail": "详情页"} {
		var slots []slot
		_ = json.Unmarshal(input[cn], &slots)
		items := []Object{}
		for _, s := range slots {
			hits := []string{}
			ai := false
			for _, ph := range slotHits(s, photos) {
				hits = append(hits, ph.Code)
				if ph.AIGenerated || ph.AIEnhanced {
					ai = true
				}
			}
			min := s.Minimum
			if min == 0 {
				min = 1
			}
			ok := len(hits) >= min
			pos := s.Position
			if pos == nil {
				pos = s.Screen
			}
			hit := ""
			if len(hits) > 0 {
				hit = hits[0]
			}
			entry := Object{"position": pos, "name": s.Name, "candidates": s.Candidates, "required": s.Required, "matched": hits, "hit": hit, "covered": ok, "note": s.Note, "ai": ai}
			items = append(items, entry)
			if s.Required && !ok && !slotAllowsFill(s) {
				need = append(need, Object{"type": key, "position": pos, "name": s.Name, "candidates": s.Candidates})
			}
		}
		out[key] = items
	}
	out["need_reshoot"] = need
	return out
}

var codes = func() []Object {
	groups := []struct {
		g     string
		names []string
	}{{"A 白底", []string{"一对平放正面", "一对立起 45°", "每个颜色或规格"}}, {"B 微距", []string{"珍珠表面生长纹", "珍珠光泽和反光", "配件做工", "925 印记", "耳针或耳夹结构", "珍珠微瑕"}}, {"C 尺寸", []string{"钢尺参照", "一元硬币参照", "指尖或手心", "同一只耳朵各尺寸上耳"}}, {"D 上耳", []string{"侧脸正对耳朵", "微侧 45°", "正脸半脸", "生活感上耳", "耳饰配颈链"}}, {"E 场景", []string{"深蓝丝绒", "米白亚麻布", "大理石纹场景", "打开的包装盒", "手托托盘批量实拍（不校色）"}}, {"F 包装", []string{"礼盒和品牌卡", "打开的礼盒", "打包好的快递"}}}
	out := []Object{}
	for _, g := range groups {
		for i, n := range g.names {
			out = append(out, Object{"code": fmt.Sprintf("%c%d", g.g[0], i+1), "group": g.g, "name": n})
		}
	}
	return out
}()
