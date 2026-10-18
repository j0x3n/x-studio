package copywriting

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"xstudio/internal/core"
)

func cloneObject(o core.Object) core.Object {
	if o == nil {
		return core.Object{}
	}
	b, _ := json.Marshal(o)
	var out core.Object
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = core.Object{}
	}
	return out
}

func normalizeCopy(c, m, l core.Object) {
	for _, k := range []string{"tags", "details", "sizeWear", "params", "related", "care", "service", "about"} {
		if c[k] == nil {
			c[k] = []any{}
		}
	}
	for _, k := range []string{"photos", "real", "wear", "pack"} {
		if c[k] == nil {
			c[k] = core.Object{}
		}
	}
	if r, ok := c["real"].(core.Object); ok {
		for _, k := range []string{"cols", "rows"} {
			if r[k] == nil {
				r[k] = []any{}
			}
		}
	}
	if p, ok := c["pack"].(core.Object); ok {
		if p["items"] == nil {
			p["items"] = []any{}
		}
	}
	if m["imgs"] == nil {
		m["imgs"] = []any{}
	}
	for _, k := range []string{"title_alts", "attrs", "sku", "compliance"} {
		if l[k] == nil {
			l[k] = []any{}
		}
	}
}

type fieldSpec map[string]any

var photoSpec = fieldSpec{"f": "file", "need": "string", "pos": "string", "pos11": "string", "ai": "ai", "stamp": "string", "cap": "string", "mm": "string", "t": "string", "s": "string"}
var contentSpec = fieldSpec{
	"series": "string", "name": "string", "nameEn": "string", "oneLiner": "string", "tags": []any{"string"},
	"story": "string", "storySign": "string", "issueNo": "string", "sticker": "string", "festival": "string",
	"photos": fieldSpec{"*": photoSpec}, "real": fieldSpec{"title": "string", "lead": "string", "cols": []any{"string"}, "rows": []any{[]any{"string"}}, "note": "string"},
	"detailsLead": "string", "details": []any{fieldSpec{"photo": photoSpec, "title": "string", "text": "string"}},
	"wear": fieldSpec{"lead": "string", "big": photoSpec, "a": photoSpec, "b": photoSpec}, "sizeWear": []any{photoSpec}, "sizeLead": "string", "params": []any{[]any{"string"}}, "sizeTip": "string",
	"relatedLead": "string", "related": []any{fieldSpec{"photo": photoSpec, "name": "string", "text": "string"}}, "pack": fieldSpec{"lead": "string", "items": []any{"string"}}, "care": []any{"string"}, "service": []any{"string"}, "about": []any{"string"},
}
var mainSpec = fieldSpec{"name": "string", "imgs": []any{fieldSpec{"n": "number", "layout": "string", "f": "file", "need": "string", "pos": "string", "pos11": "string", "ai": "ai", "mark": "bool", "darkMark": "bool", "lightLabel": "bool", "label": "string", "text": "text", "small": "string", "size": "string", "sizeNote": "string", "items": []any{photoSpec}}}}
var listingSpec = fieldSpec{"title": "string", "title_alts": []any{"string"}, "attrs": []any{[]any{"string"}}, "sku": []any{fieldSpec{"name": "string", "price": "nullable_number"}}, "compliance": []any{fieldSpec{"level": "string", "text": "string"}}}

func validateValue(v, spec any, path string) error {
	bad := func() error { return fmt.Errorf("文案字段类型不正确：%s", path) }
	switch s := spec.(type) {
	case string:
		switch s {
		case "ai":
			switch v.(type) {
			case bool, string:
			default:
				return bad()
			}
		case "file":
			if v == nil {
				return nil
			}
			if _, ok := v.(string); !ok {
				return bad()
			}
		case "string":
			if _, ok := v.(string); !ok {
				return bad()
			}
		case "bool":
			if _, ok := v.(bool); !ok {
				return bad()
			}
		case "number", "nullable_number":
			if s == "nullable_number" && v == nil {
				return nil
			}
			n, ok := v.(float64)
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
				return bad()
			}
		case "text":
			switch t := v.(type) {
			case string:
			case []any:
				for _, x := range t {
					if _, ok := x.(string); !ok {
						return bad()
					}
				}
			default:
				return bad()
			}
		}
	case fieldSpec:
		if v == nil {
			if _, photo := s["f"]; photo {
				return nil
			}
		}
		o, ok := v.(map[string]any)
		if !ok {
			return bad()
		}
		for k, val := range o {
			sp, known := s[k]
			if !known {
				sp, known = s["*"]
			}
			if known {
				if e := validateValue(val, sp, path+"."+k); e != nil {
					return e
				}
			}
		}
	case []any:
		arr, ok := v.([]any)
		if !ok {
			return bad()
		}
		for i, val := range arr {
			if e := validateValue(val, s[0], fmt.Sprintf("%s[%d]", path, i)); e != nil {
				return e
			}
		}
	}
	return nil
}

func validateCopy(c, m, l core.Object) error {
	for _, v := range []struct {
		o core.Object
		s fieldSpec
		p string
	}{{c, contentSpec, "content"}, {m, mainSpec, "main"}, {l, listingSpec, "meta"}} {
		if v.o == nil {
			return fmt.Errorf("文案必须是 JSON 对象")
		}
		if _, ok := v.o["f"]; ok {
			return fmt.Errorf("照片位不能放在文案对象根节点")
		}
		if e := validateValue(v.o, v.s, v.p); e != nil {
			return e
		}
	}
	seen := map[int]bool{}
	if imgs, ok := m["imgs"].([]any); ok {
		for _, v := range imgs {
			o := v.(core.Object)
			n, ok := o["n"].(float64)
			if !ok || n < 1 || n > 99 || n != math.Trunc(n) || seen[int(n)] {
				return fmt.Errorf("主图编号 n 必须是 1–99 且不能重复")
			}
			seen[int(n)] = true
			if layout, ok := o["layout"].(string); ok {
				switch layout {
				case "photo", "ruler", "sizes", "variants":
				default:
					return fmt.Errorf("主图 layout 不受支持")
				}
			}
		}
	}
	return nil
}

func (a *Module) validPhotoSet(productID int64) map[string]bool {
	valid := map[string]bool{}
	dir := filepath.Join(a.Dir(productID), "照片")
	if entries, e := os.ReadDir(dir); e == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				valid[entry.Name()] = true
			}
		}
	}
	if photos, e := a.Photos(productID); e == nil {
		for _, ph := range photos {
			if ph.Code != "" {
				valid[ph.Code+".jpg"] = true
				if ph.WhiteBG {
					valid[ph.Code+"_白底.jpg"] = true
					valid[ph.Code+"_透明.png"] = true
				}
			}
		}
	}
	return valid
}

func (a *Module) hasPhotoForProduct(productID int64, name string, valid map[string]bool) bool {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\:") || strings.Contains(name, "..") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".jpg" && ext != ".png" && ext != ".webp" {
		return false
	}
	if valid[name] {
		return true
	}
	dir := filepath.Join(a.Dir(productID), "照片")
	root, e := os.OpenRoot(dir)
	if e != nil {
		return false
	}
	defer root.Close()
	f, e := root.Open(name)
	if e != nil {
		return false
	}
	defer f.Close()
	st, e := f.Stat()
	return e == nil && st.Mode().IsRegular()
}

func (a *Module) checkReferencedPhotosExist(productID int64, content, main core.Object) error {
	valid := a.validPhotoSet(productID)
	var walk func(any) error
	walk = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			if f, exists := x["f"]; exists && f != nil {
				s, ok := f.(string)
				if !ok {
					return fmt.Errorf("照片文件名必须是文本")
				}
				if s != "" && !a.hasPhotoForProduct(productID, s, valid) {
					return fmt.Errorf("照片位引用的照片不存在：%s", s)
				}
			}
			for _, child := range x {
				if e := walk(child); e != nil {
					return e
				}
			}
		case []any:
			for _, child := range x {
				if e := walk(child); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if e := walk(content); e != nil {
		return e
	}
	return walk(main)
}

func (a *Module) cleanMainForProduct(productID int64, main core.Object) core.Object {
	valid := a.validPhotoSet(productID)
	out := core.Object{}
	for k, v := range main {
		out[k] = v
	}
	imgs, _ := main["imgs"].([]any)
	kept := []any{}
	for _, v := range imgs {
		m, ok := v.(core.Object)
		if !ok {
			continue
		}
		image := core.Object{}
		for k, v := range m {
			image[k] = v
		}
		layout, _ := m["layout"].(string)
		switch layout {
		case "", "photo", "ruler":
			f, _ := m["f"].(string)
			if !a.hasPhotoForProduct(productID, f, valid) {
				continue
			}
		case "sizes", "variants":
			items, _ := m["items"].([]any)
			validItems := []any{}
			for _, v := range items {
				item, ok := v.(core.Object)
				if !ok {
					continue
				}
				f, _ := item["f"].(string)
				if a.hasPhotoForProduct(productID, f, valid) {
					validItems = append(validItems, item)
				}
			}
			if len(validItems) < 2 {
				continue
			}
			image["items"] = validItems
		default:
			continue
		}
		image["n"] = float64(len(kept) + 1)
		kept = append(kept, image)
	}
	out["imgs"] = kept
	return out
}

func (a *Module) cleanObjectForProduct(productID int64, o core.Object) core.Object {
	valid := a.validPhotoSet(productID)
	var clean func(any) any
	clean = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			if f, exists := x["f"]; exists {
				s, ok := f.(string)
				if !ok || s == "" || !a.hasPhotoForProduct(productID, s, valid) {
					return nil
				}
			}
			res := core.Object{}
			for k, val := range x {
				if c := clean(val); c != nil {
					res[k] = c
				}
			}
			return res
		case []any:
			res := []any{}
			for _, val := range x {
				if c := clean(val); c != nil {
					res = append(res, c)
				}
			}
			return res
		default:
			return v
		}
	}
	v, ok := clean(o).(core.Object)
	if !ok {
		return core.Object{}
	}
	return v
}

func (a *Module) annotateAI(productID int64, content, main core.Object) error {
	photos, e := a.Photos(productID)
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

func titleLength(s string) int {
	n := 0
	for _, r := range s {
		if r <= 255 {
			n++
		} else {
			n += 2
		}
	}
	return n
}

func appendCompliance(content, main, listing core.Object, words string) []any {
	items := []any{}
	if prior, ok := listing["compliance"].([]any); ok {
		for _, v := range prior {
			if o, ok := v.(core.Object); ok && o["source"] == "local" {
				continue
			}
			items = append(items, v)
		}
	}
	listingText := core.Object{}
	for k, v := range listing {
		if k != "compliance" {
			listingText[k] = v
		}
	}
	all := core.JS(core.Object{"content": content, "main": main, "listing": listingText})
	seen := map[string]bool{}
	for _, line := range strings.Split(words, "\n") {
		word := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if word == "" || core.HasPrefix(word, "#", "//") {
			continue
		}
		level, description := "error", ""
		columns := strings.SplitN(strings.ReplaceAll(word, `\t`, "\t"), "\t", 3)
		if len(columns) >= 2 && (columns[0] == "error" || columns[0] == "warn") {
			level = columns[0]
			word = strings.TrimSpace(columns[1])
			if len(columns) == 3 {
				description = columns[2]
			}
		}
		if word == "" {
			continue
		}
		if strings.Contains(all, word) && !seen[word] {
			text := "含违禁词：" + word
			if description != "" {
				text += "（" + description + "）"
			}
			items = append(items, core.Object{"level": level, "text": text, "source": "local"})
			seen[word] = true
		}
	}
	if title, ok := listing["title"].(string); ok && titleLength(title) > 60 {
		items = append(items, core.Object{"level": "error", "text": "标题超过 60 字符（汉字算 2），请缩短后再上架", "source": "local"})
	}
	return items
}
