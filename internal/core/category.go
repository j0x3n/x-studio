package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

type CategoryAttr struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Options  []string `json:"options,omitempty"`
	Free     bool     `json:"free,omitempty"`
	Required bool     `json:"required,omitempty"`
	Unit     string   `json:"unit,omitempty"`
	Hint     string   `json:"hint,omitempty"`
}
type CategoryCode struct {
	Code   string `json:"code"`
	Group  string `json:"group"`
	Name   string `json:"name"`
	Use    string `json:"use,omitempty"`
	Desc   string `json:"desc,omitempty"`
	Suffix string `json:"suffix,omitempty"`
}
type Category struct {
	ID              int64           `json:"id"`
	Key             string          `json:"key"`
	Name            string          `json:"name"`
	Sort            int             `json:"sort"`
	Disabled        bool            `json:"disabled"`
	Attrs           []CategoryAttr  `json:"attrs"`
	Codes           []CategoryCode  `json:"codes"`
	Views           []CategoryView  `json:"views"`
	Slots           json.RawMessage `json:"slots"`
	Rules           string          `json:"rules"`
	Banned          []string        `json:"banned"`
	DefaultTemplate string          `json:"default_template"`
	ProductCount    int64           `json:"product_count"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

type CategoryView struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Code     string   `json:"code"`
	Groups   []string `json:"groups"`
	Required []string `json:"required"`
	Hint     string   `json:"hint"`
}

const categoryQuery = `SELECT c.id,c.key,c.name,c.sort,c.disabled,c.attrs,c.codes,c.slots,c.rules,c.banned,c.default_template,c.created_at,c.updated_at,(SELECT count(*) FROM products p WHERE p.category_id=c.id),c.views FROM categories c `

func ScanCategory(s Scanner) (c Category, err error) {
	var attrs, codes, slots, banned, views string
	err = s.Scan(&c.ID, &c.Key, &c.Name, &c.Sort, &c.Disabled, &attrs, &codes, &slots, &c.Rules, &banned, &c.DefaultTemplate, &c.CreatedAt, &c.UpdatedAt, &c.ProductCount, &views)
	if err != nil {
		return
	}
	c.Attrs = []CategoryAttr{}
	c.Codes = []CategoryCode{}
	c.Views = []CategoryView{}
	c.Banned = []string{}
	if err = json.Unmarshal([]byte(attrs), &c.Attrs); err != nil {
		return
	}
	if err = json.Unmarshal([]byte(codes), &c.Codes); err != nil {
		return
	}
	err = json.Unmarshal([]byte(banned), &c.Banned)
	if err == nil {
		err = json.Unmarshal([]byte(views), &c.Views)
	}
	c.Slots = json.RawMessage(slots)
	return
}
func (a *Core) Category(id int64) (*Category, error) {
	c, e := ScanCategory(a.DB.QueryRow(categoryQuery+"WHERE c.id=?", id))
	return &c, e
}
func (a *Core) CategoryByKey(key string) (*Category, error) {
	c, e := ScanCategory(a.DB.QueryRow(categoryQuery+"WHERE c.key=?", key))
	return &c, e
}
func (a *Core) ProductCategory(id int64) (*Category, error) {
	c, e := ScanCategory(a.DB.QueryRow(categoryQuery+"WHERE c.id=(SELECT category_id FROM products WHERE id=?)", id))
	return &c, e
}
func (a *Core) Categories(all bool) ([]Category, error) {
	q := categoryQuery
	if !all {
		q += "WHERE c.disabled=0 "
	}
	q += "ORDER BY c.sort,c.id"
	rows, e := a.DB.Query(q)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		c, e := ScanCategory(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (c *Category) Brief() Object {
	if c == nil {
		return Object{}
	}
	return Object{"key": c.Key, "name": c.Name}
}
func FillPrompt(text string, c *Category) string {
	name, rules, table, banned := "", "", "", ""
	if c != nil {
		name, rules, banned = c.Name, c.Rules, strings.Join(c.Banned, "、")
		table = "| 编号 | 是什么 | 用在哪 |\n| --- | --- | --- |\n"
		escape := func(s string) string { return strings.NewReplacer("|", "\\|", "\r", " ", "\n", " ").Replace(s) }
		for _, v := range c.Codes {
			code := v.Code
			if v.Suffix != "" {
				code += "_<" + v.Suffix + ">"
			}
			desc := v.Desc
			if desc == "" {
				desc = v.Name
			}
			table += fmt.Sprintf("| %s | %s | %s |\n", escape(code), escape(desc), escape(v.Use))
		}
	}
	return strings.NewReplacer("{{category_name}}", name, "{{category_rules}}", rules, "{{codes}}", table, "{{banned_words}}", banned).Replace(text)
}

// CategoryBannedLines retains the existing local compliance parser's format.
func CategoryBannedLines(words string, c *Category) string {
	prefix := ""
	if c != nil {
		for _, w := range c.Banned {
			if strings.TrimSpace(w) != "" {
				prefix += "error\t" + w + "\t" + c.Name + "违禁词\n"
			}
		}
	}
	return prefix + words
}
