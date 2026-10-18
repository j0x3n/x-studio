package products

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"xstudio/internal/core"
)

func emptyCategory() core.Category {
	return core.Category{Attrs: []core.CategoryAttr{}, Codes: []core.CategoryCode{}, Slots: json.RawMessage(`{}`), Banned: []string{}}
}
func applyCategory(c *core.Category, in map[string]json.RawMessage, creating bool) error {
	for k, v := range in {
		var target any
		switch k {
		case "key":
			if !creating {
				return fmt.Errorf("品类 key 不能修改")
			}
			target = &c.Key
		case "name":
			target = &c.Name
		case "sort":
			target = &c.Sort
		case "disabled":
			target = &c.Disabled
		case "attrs":
			target = &c.Attrs
		case "codes":
			target = &c.Codes
		case "slots":
			target = &c.Slots
		case "rules":
			target = &c.Rules
		case "banned":
			target = &c.Banned
		case "default_template":
			target = &c.DefaultTemplate
		case "copy_from":
			if creating {
				continue
			}
			fallthrough
		default:
			return fmt.Errorf("不支持的品类字段：%s", k)
		}
		if string(v) == "null" || json.Unmarshal(v, target) != nil {
			return fmt.Errorf("品类字段 %s 格式不正确", k)
		}
	}
	return nil
}
func categoryValues(c *core.Category) []any {
	return []any{c.Name, c.Sort, c.Disabled, core.JS(c.Attrs), core.JS(c.Codes), string(c.Slots), c.Rules, core.JS(c.Banned), c.DefaultTemplate, core.Now()}
}
func insertCategory(tx *sql.Tx, c *core.Category) (int64, error) {
	vals := append([]any{c.Key}, categoryValues(c)...)
	vals = append(vals, core.Now())
	res, e := tx.Exec(`INSERT INTO categories(key,name,sort,disabled,attrs,codes,slots,rules,banned,default_template,updated_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, vals...)
	if e != nil {
		return 0, e
	}
	return res.LastInsertId()
}
func (a *Module) listCategories(w http.ResponseWriter, r *http.Request) {
	items, e := a.Categories(r.URL.Query().Get("all") == "1")
	if e != nil {
		core.Fail(w, 500, "读取品类失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"items": items})
}
func (a *Module) createCategory(w http.ResponseWriter, r *http.Request) { a.writeCategory(w, r, true) }
func (a *Module) patchCategory(w http.ResponseWriter, r *http.Request)  { a.writeCategory(w, r, false) }
func (a *Module) writeCategory(w http.ResponseWriter, r *http.Request, creating bool) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	var in map[string]json.RawMessage
	if !core.Decode(w, r, &in) {
		return
	}
	if len(in) == 0 {
		core.Fail(w, 400, "没有要保存的品类字段")
		return
	}
	c := emptyCategory()
	if creating {
		if raw, ok := in["copy_from"]; ok {
			var key string
			if json.Unmarshal(raw, &key) != nil {
				core.Fail(w, 400, "copy_from 格式不正确")
				return
			}
			base, e := a.CategoryByKey(key)
			if e != nil {
				core.Fail(w, 400, "复制的品类不存在")
				return
			}
			c.Attrs, c.Codes, c.Slots, c.Rules, c.Banned, c.DefaultTemplate = base.Attrs, base.Codes, base.Slots, base.Rules, base.Banned, base.DefaultTemplate
		}
	} else {
		old, e := a.Category(core.ID(r, "id"))
		if e != nil {
			core.Fail(w, 404, "品类不存在")
			return
		}
		c = *old
	}
	if e := applyCategory(&c, in, creating); e != nil {
		core.Fail(w, 400, e.Error())
		return
	}
	if e := a.validateCategory(&c); e != nil {
		core.Fail(w, 400, e.Error())
		return
	}
	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "保存品类失败")
		return
	}
	defer tx.Rollback()
	if c.Disabled {
		var n int
		if e = tx.QueryRow("SELECT count(*) FROM categories WHERE disabled=0 AND id<>?", c.ID).Scan(&n); e != nil {
			core.Fail(w, 500, "读取品类失败")
			return
		}
		if n == 0 {
			core.Fail(w, 400, "最后一个启用的品类不能停用")
			return
		}
	}
	action := "category.update"
	if creating {
		action = "category.create"
		c.ID, e = insertCategory(tx, &c)
	} else {
		vals := append(categoryValues(&c), c.ID)
		_, e = tx.Exec(`UPDATE categories SET name=?,sort=?,disabled=?,attrs=?,codes=?,slots=?,rules=?,banned=?,default_template=?,updated_at=? WHERE id=?`, vals...)
	}
	if e != nil {
		core.Fail(w, 400, "品类 key 已存在或字段不正确")
		return
	}
	fields := mapKeys(in)
	sort.Strings(fields)
	if core.AuditTx(tx, r, action, "category", c.ID, core.Object{"fields": fields}) != nil || tx.Commit() != nil {
		core.Fail(w, 500, "保存品类失败")
		return
	}
	saved, _ := a.Category(c.ID)
	status := 200
	if creating {
		status = 201
	}
	core.WriteJSON(w, status, core.Object{"category": saved})
}

// AfterMigrate runs on every startup, including databases already at v2.
func (a *Module) AfterMigrate() error {
	files, e := filepath.Glob(filepath.Join(a.Cfg.PromptDir, "品类", "*.json"))
	if e != nil {
		return e
	}
	for _, file := range files {
		if e := a.seedCategory(file); e != nil {
			log.Printf("跳过品类种子 %s：%v", filepath.Base(file), e)
		}
	}
	return nil
}
func (a *Module) seedCategory(file string) error {
	b, e := os.ReadFile(file)
	if e != nil {
		return e
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil || raw == nil {
		return fmt.Errorf("需要品类对象")
	}
	c := emptyCategory()
	if e = applyCategory(&c, raw, true); e != nil {
		return e
	}
	if e = a.validateCategory(&c); e != nil {
		return e
	}
	old, e := a.CategoryByKey(c.Key)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	tx, e2 := a.DB.Begin()
	if e2 != nil {
		return e2
	}
	defer tx.Rollback()
	if e == sql.ErrNoRows {
		_, e = insertCategory(tx, &c)
	} else {
		before := categoryValues(old)
		if len(old.Attrs) == 0 {
			old.Attrs = c.Attrs
		}
		if len(old.Codes) == 0 {
			old.Codes = c.Codes
		}
		if len(old.Banned) == 0 {
			old.Banned = c.Banned
		}
		var slots map[string]any
		_ = json.Unmarshal(old.Slots, &slots)
		if len(slots) == 0 {
			old.Slots = c.Slots
		}
		if old.Rules == "" {
			old.Rules = c.Rules
		}
		if old.DefaultTemplate == "" {
			old.DefaultTemplate = c.DefaultTemplate
		}
		// Validate the merged object as well as the seed, since existing slots may reference different codes.
		if e = a.validateCategory(old); e != nil {
			return e
		}
		after := categoryValues(old)
		fields := []string{"attrs", "codes", "slots", "rules", "banned", "default_template"}
		sets := []string{}
		args := []any{}
		for i, field := range fields {
			index := i + 3
			if before[index] != after[index] {
				sets = append(sets, field+"=?")
				args = append(args, after[index])
			}
		}
		if len(sets) == 0 {
			return nil
		}
		args = append(args, core.Now(), old.ID)
		_, e = tx.Exec("UPDATE categories SET "+strings.Join(sets, ",")+",updated_at=? WHERE id=?", args...)

	}
	if e != nil {
		return e
	}
	return tx.Commit()
}
