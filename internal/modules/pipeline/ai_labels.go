package pipeline

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

func (a *Module) annotateAI(id int64, content, main Object) error {
	return a.annotate(id, content, main)
}
func (a *Module) annotateForExport(p Product) error {
	return a.annotate(p.ID, p.Content, p.Main)
}
func (a *Module) annotate(id int64, content, main Object) error {
	photos, e := a.Photos(id)
	if e != nil {
		return e
	}
	files := map[string]bool{}
	for _, p := range photos {
		if p.Code == "" {
			continue
		}
		for _, ext := range []string{".jpg", "_白底.jpg", "_透明.png"} {
			files[p.Code+ext] = true
		}
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if f, exists := x["f"]; exists && files[fmt.Sprint(f)] {
				delete(x, "ai")
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(content)
	walk(main)
	return nil
}

func (a *Module) serveTemplate(w http.ResponseWriter, r *http.Request, name string) {
	b, e := os.ReadFile(filepath.Join(a.Cfg.TemplateDir, name))
	if e != nil {
		fail(w, 404, "模板不存在")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}
