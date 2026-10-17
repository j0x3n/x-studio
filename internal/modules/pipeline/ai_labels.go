package pipeline

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (a *Module) annotateAI(id int64, content, main Object) error {
	return a.annotate(id, content, main, true)
}
func (a *Module) annotateForExport(p Product) error {
	return a.annotate(p.ID, p.Content, p.Main, p.AILabel)
}
func (a *Module) annotate(id int64, content, main Object, enabled bool) error {
	photos, e := a.Photos(id)
	if e != nil {
		return e
	}
	labels := map[string]string{}
	text, _ := a.Setting("ai_label")["text"].(string)
	if text == "" {
		text = "AI 生成"
	}
	for _, p := range photos {
		if p.Code == "" {
			continue
		}
		label := ""
		s := p.EffectiveSource()
		if enabled && (s == "ai_generated" || s == "ai_edited") {
			label = text
		}
		for _, ext := range []string{".jpg", "_白底.jpg", "_透明.png"} {
			labels[p.Code+ext] = label
		}
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if f, exists := x["f"]; exists {
				if label := labels[fmt.Sprint(f)]; label != "" {
					x["ai"] = label
				} else {
					delete(x, "ai")
				}
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

// Add the global style to the response; template source files remain untouched.
func (a *Module) serveTemplate(w http.ResponseWriter, r *http.Request, name string) {
	b, e := os.ReadFile(filepath.Join(a.Cfg.TemplateDir, name))
	if e != nil {
		fail(w, 404, "模板不存在")
		return
	}
	o := a.Setting("ai_label")
	pos, _ := o["position"].(string)
	vertical, horizontal := "bottom", "right"
	if strings.HasPrefix(pos, "top-") {
		vertical = "top"
	}
	if strings.HasSuffix(pos, "-left") {
		horizontal = "left"
	}
	color, _ := o["color"].(string)
	if color == "" {
		color = "#FFFFFF"
	}
	size, ok := o["size"].(float64)
	if !ok {
		size = 14
	}
	opacity, ok := o["opacity"].(float64)
	if !ok {
		opacity = .6
	}
	css := fmt.Sprintf(`<style>.ai-tag{top:auto!important;bottom:auto!important;left:auto!important;right:auto!important;%s:14px!important;%s:14px!important;font-size:%gpx!important;color:%s!important;opacity:%g!important}</style>`, vertical, horizontal, size, color, opacity)
	html := strings.Replace(string(b), "</head>", css+"</head>", 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}
