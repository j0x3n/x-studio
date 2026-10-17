package app

// The server owns AI labels; model or employee supplied labels cannot suppress
// provenance, and an ordinary real photo cannot be relabeled as AI either.
func (a *App) annotateAI(id int64, content, main Object) error {
	photos, err := a.photos(id)
	if err != nil {
		return err
	}
	labels := map[string]string{}
	for _, ph := range photos {
		if ph.Code == "" {
			continue
		}
		label := ""
		// 只给 AI 生成的补图打标识；实拍图做高清化属于修图，不打标（用户 2026-10-18 决定）
		if ph.AIGenerated {
			label = "AI 生成"
		}
		for _, suffix := range []string{".jpg", "_白底.jpg", "_透明.png"} {
			labels[ph.Code+suffix] = label
		}
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if f, exists := x["f"]; exists {
				file, _ := f.(string)
				if label := labels[file]; label != "" {
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
