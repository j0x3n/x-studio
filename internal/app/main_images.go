package app

// cleanMain applies layout-specific photo requirements. It returns new maps so
// filtering and renumbering do not mutate a caller's unfiltered input.
func cleanMain(main Object, dir string) Object {
	out := Object{}
	for k, v := range main {
		out[k] = v
	}
	imgs, _ := main["imgs"].([]any)
	kept := []any{}
	for _, v := range imgs {
		m, ok := v.(Object)
		if !ok {
			continue
		}
		image := Object{}
		for k, v := range m {
			image[k] = v
		}
		layout, _ := m["layout"].(string)
		switch layout {
		case "", "photo", "ruler":
			f, _ := m["f"].(string)
			if !safePhotoFile(dir, f) {
				continue
			}
		case "sizes", "variants":
			items, _ := m["items"].([]any)
			valid := []any{}
			for _, v := range items {
				item, ok := v.(Object)
				if !ok {
					continue
				}
				f, _ := item["f"].(string)
				if safePhotoFile(dir, f) {
					valid = append(valid, item)
				}
			}
			if len(valid) < 2 {
				continue
			}
			image["items"] = valid
		default:
			continue
		}
		image["n"] = float64(len(kept) + 1)
		kept = append(kept, image)
	}
	out["imgs"] = kept
	return out
}
