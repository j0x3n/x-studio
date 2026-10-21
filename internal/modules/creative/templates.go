package creative

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"xstudio/internal/core"
)

func (a *Module) AfterMigrate() error {
	if err := a.backfillStale(); err != nil {
		return err
	}
	if err := a.seedTemplates(); err != nil {
		return err
	}
	if err := a.scanReturns(a.Context()); err != nil {
		log.Printf("creative 售后扫描：%v", err)
	}
	a.startReturnScan()
	return nil
}

func validSlot(kind, slot string) bool {
	if kind == "main" {
		return slices.Contains([]string{"main1", "main2", "main3", "main4", "main5", "white", "sku"}, slot)
	}
	return kind == "detail" && slices.Contains([]string{"d1", "d2", "d3", "d4", "d5", "d6", "d7", "d8", "d9"}, slot)
}
func validateTemplate(t Template) error {
	if t.Key == "" || len(t.Key) > 100 || strings.ContainsAny(t.Key, "/\\:") || t.Name == "" || !validSlot(t.Kind, t.Slot) {
		return problem(400, "模板 key、名称或位置不正确")
	}
	if !slices.Contains([]string{"pixel_lock", "model", "redraw", "photo", "none"}, t.ProductMode) {
		return problem(400, "商品模式不正确")
	}
	if !slices.Contains([]string{"1024x1024", "1024x1536", "1536x1024"}, t.GenSize) || t.Count < 1 || t.Count > 3 {
		return problem(400, "生图尺寸或张数不正确")
	}
	if t.ProductMode == "pixel_lock" {
		if !validBox(t.ProductBox) {
			return problem(400, "商品位置必须是画布内的比例方框")
		}
	}
	if !slices.Contains([]string{"", "cutout", "rect"}, t.LockShape) {
		return problem(400, "像素锁定形状不正确")
	}
	if t.ProductMode == "model" && (t.Wear == nil || !slices.Contains([]string{"female_main", "male", "hand"}, t.Wear.Slot) || !slices.Contains([]string{"closeup", "half_body"}, t.Wear.Framing) || !slices.Contains([]string{"auto", "full_body", "upper_body", "hand", "wrist", "ear", "neck", "foot", "custom"}, t.Wear.BodyPart)) {
		return problem(400, "模特设置不正确")
	}
	for _, ref := range t.StyleRefs {
		if !fs.ValidPath(ref) || strings.ContainsAny(ref, "\\:") {
			return problem(400, "风格参考路径不合法")
		}
	}
	if len(t.Export) == 0 {
		return problem(400, "请设置导出尺寸")
	}
	names := map[string]bool{}
	for _, size := range t.Export {
		if size.Name == "" || !fs.ValidPath(size.Name) || strings.ContainsAny(size.Name, "/\\:") || names[size.Name] || size.W < 1 || size.W > 8000 || size.H < 0 || size.H > 8000 {
			return problem(400, "导出尺寸不正确")
		}
		names[size.Name] = true
	}
	keys := map[string]bool{}
	for _, text := range t.Texts {
		if text.Key == "" || keys[text.Key] || text.Max < 0 {
			return problem(400, "文字 key 或字数不正确")
		}
		keys[text.Key] = true
	}
	return nil
}
func validBox(box []float64) bool {
	return len(box) == 4 && box[0] >= 0 && box[1] >= 0 && box[2] > 0 && box[3] > 0 && box[0]+box[2] <= 1.000001 && box[1]+box[3] <= 1.000001
}

func resolveBodyPart(wear *Wear, wearType string) (string, error) {
	if wear == nil {
		return "", problem(400, "模特设置不正确")
	}
	if wear.BodyPart != "auto" {
		return wear.BodyPart, nil
	}
	body := map[string]string{"necklace": "neck", "earring": "ear", "bracelet": "wrist", "ring": "hand"}[wearType]
	if body == "" {
		return "", problem(400, "无法判断佩戴位置，请在模板中选择具体部位")
	}
	return body, nil
}

func (a *Module) seedTemplates() error {
	for _, kind := range []string{"main", "detail"} {
		dir := filepath.Join(a.Cfg.TemplateDir, "生图模板", kind)
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			log.Printf("creative 模板目录：%v", err)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			name := filepath.Join(dir, entry.Name())
			raw, err := os.ReadFile(name)
			var incoming map[string]any
			if err != nil || json.Unmarshal(raw, &incoming) != nil {
				log.Printf("creative 跳过格式错误的模板：%s", name)
				continue
			}
			if _, ok := incoming["count"]; !ok {
				incoming["count"] = 1
			}
			if _, ok := incoming["enabled"]; !ok {
				incoming["enabled"] = true
			}
			if number(incoming["version"]) < 1 {
				incoming["version"] = 1
			}
			t, err := typed[Template](incoming)
			if err != nil {
				log.Printf("creative 跳过模板：%s", name)
				continue
			}
			existing, findErr := a.one(context.Background(), "SELECT * FROM creative_templates WHERE key=?", t.Key)
			if findErr == nil && stringValue(existing["source"]) == "custom" {
				continue
			}
			if findErr == nil && stringValue(existing["source"]) == "file" && t.Version > int(number(existing["version"])) {
				if err = validateTemplate(t); err != nil {
					log.Printf("creative 跳过无效模板：%s", name)
					continue
				}
				t.Source = "file"
				if err = a.storeTemplate(context.Background(), t, 0); err != nil {
					return err
				}
				a.AuditUser(0, "", "creative.seed_upgrade", "creative_template", int64(number(existing["id"])), core.Object{"key": t.Key, "version": t.Version})
				continue
			}
			filled := core.Object{}
			if findErr == nil {
				for key, value := range existing {
					empty := value == nil || value == ""
					if list, ok := value.([]any); ok && len(list) == 0 {
						empty = true
					}
					if empty && incoming[key] != nil {
						existing[key] = incoming[key]
						if key != "id" && key != "key" && key != "updated_by" && key != "updated_at" && key != "source" {
							filled[key] = incoming[key]
						}
					}
				}
				t, err = typed[Template](existing)
			}
			if err != nil || validateTemplate(t) != nil {
				log.Printf("creative 跳过无效模板：%s", name)
				continue
			}
			if findErr != nil {
				t.Source = "file"
				if err = a.storeTemplate(context.Background(), t, 0); err != nil {
					return err
				}
				a.AuditUser(0, "", "creative.seed", "creative_template", 0, core.Object{"key": t.Key})
				continue
			}
			if len(filled) > 0 {
				assignments := []string{}
				args := []any{}
				for key, value := range filled {
					assignments = append(assignments, key+"=?")
					if jsonColumns[key] {
						value = core.JS(value)
					}
					args = append(args, value)
				}
				args = append(args, t.Key)
				if _, err = a.DB.Exec("UPDATE creative_templates SET "+strings.Join(assignments, ",")+" WHERE key=?", args...); err != nil {
					return err
				}
				a.AuditUser(0, "", "creative.seed_fill", "creative_template", t.ID, filled)
			}
		}
	}
	return nil
}

func (a *Module) storeTemplate(ctx context.Context, t Template, uid int64) error {
	if t.Source == "" {
		t.Source = "custom"
	}
	_, err := a.DB.ExecContext(ctx, `INSERT INTO creative_templates(key,name,kind,slot,categories,purpose,prompt,style_refs,texts,photo_codes,product_mode,product_box,gen_size,export,count,wear,version,enabled,source,updated_by,updated_at,lock_shape,size_compare,hero_style) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(key) DO UPDATE SET name=excluded.name,kind=excluded.kind,slot=excluded.slot,categories=excluded.categories,purpose=excluded.purpose,prompt=excluded.prompt,style_refs=excluded.style_refs,texts=excluded.texts,photo_codes=excluded.photo_codes,product_mode=excluded.product_mode,product_box=excluded.product_box,gen_size=excluded.gen_size,export=excluded.export,count=excluded.count,wear=excluded.wear,version=excluded.version,enabled=excluded.enabled,updated_by=excluded.updated_by,updated_at=excluded.updated_at,lock_shape=excluded.lock_shape,size_compare=excluded.size_compare,hero_style=excluded.hero_style,source=excluded.source`, t.Key, t.Name, t.Kind, t.Slot, core.JS(t.Categories), t.Purpose, t.Prompt, core.JS(t.StyleRefs), core.JS(t.Texts), core.JS(t.PhotoCodes), t.ProductMode, core.JS(t.ProductBox), t.GenSize, core.JS(t.Export), t.Count, core.JS(t.Wear), t.Version, t.Enabled, t.Source, uid, core.Now(), t.LockShape, t.SizeCompare, t.HeroStyle)
	return err
}
func (a *Module) template(ctx context.Context, key string) (Template, error) {
	obj, err := a.one(ctx, "SELECT * FROM creative_templates WHERE key=?", key)
	if err != nil {
		return Template{}, problem(404, "模板不存在")
	}
	return typed[Template](obj)
}
func (a *Module) Prompt(name string) (string, error) {
	root, err := os.OpenRoot(a.Cfg.PromptDir)
	if err != nil {
		return "", fmt.Errorf("缺少提示词文件：%s", name)
	}
	defer root.Close()
	data, err := root.ReadFile(name)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return "", fmt.Errorf("缺少或空提示词文件：%s", name)
	}
	return string(data), nil
}

func promptSection(document, title string) string {
	lines := []string{}
	active := false
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(line, "## ") {
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			active = heading == title || strings.HasPrefix(heading, title+" ")
			continue
		}
		if active {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
