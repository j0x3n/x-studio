package app

import (
	"fmt"
	"math"
)

// Optional fields may be absent; supplied fields must match the template.
// Defaults supply structure only, never invented product claims.
func normalizeCopy(c, m, l Object) {
	for _, k := range []string{"tags", "details", "sizeWear", "params", "related", "care", "service", "about"} {
		if c[k] == nil {
			c[k] = []any{}
		}
	}
	for _, k := range []string{"photos", "real", "wear", "pack"} {
		if c[k] == nil {
			c[k] = Object{}
		}
	}
	if r, ok := c["real"].(Object); ok {
		for _, k := range []string{"cols", "rows"} {
			if r[k] == nil {
				r[k] = []any{}
			}
		}
	}
	if p, ok := c["pack"].(Object); ok {
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
func validateCopy(c, m, l Object) error {
	for _, v := range []struct {
		o Object
		s fieldSpec
		p string
	}{{c, contentSpec, "content"}, {m, mainSpec, "main"}, {l, listingSpec, "listing"}} {
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
			o := v.(Object)
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
