package inventory

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"xstudio/internal/core"
)

type unitConversion struct {
	Name   string `json:"name"`
	Factor int64  `json:"factor"`
}
type itemInput struct {
	Code        string           `json:"code"`
	Name        string           `json:"name"`
	Kind        string           `json:"kind"`
	ProductID   *int64           `json:"product_id"`
	CategoryID  *int64           `json:"category_id"`
	Spec        map[string]any   `json:"spec"`
	Unit        string           `json:"unit"`
	Units       []unitConversion `json:"units"`
	SafetyStock int64            `json:"safety_stock"`
	Note        string           `json:"note"`
	Archived    bool             `json:"archived"`
}

func length(s string, lo, hi int) bool { n := utf8.RuneCountInString(s); return n >= lo && n <= hi }

// SQLite NOCASE covers ASCII only. This private key preserves the displayed
// code while giving all Unicode codes the same comparison as strings.EqualFold.
func codeKey(s string) string {
	return strings.Map(func(r rune) rune {
		key := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			key = min(key, next)
		}
		return key
	}, s)
}

func nextItemCode(tx *sql.Tx) (string, error) {
	var raw string
	if err := tx.QueryRow("SELECT value FROM settings WHERE key='item_code_rule'").Scan(&raw); err != nil {
		return "", err
	}
	rule, err := core.ParseItemCodeRule(json.RawMessage(raw))
	if err != nil {
		return "", err
	}
	return allocateItemCode(tx, rule, time.Now())
}

func allocateItemCode(tx *sql.Tx, rule core.ItemCodeRule, now time.Time) (string, error) {
	parts := []string{}
	if rule.Prefix != "" {
		parts = append(parts, rule.Prefix)
	}
	if layout := map[string]string{"day": "20060102", "month": "200601", "year": "2006"}[rule.Date]; layout != "" {
		parts = append(parts, now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format(layout))
	}
	stem := strings.Join(parts, rule.Separator)
	if stem != "" {
		stem += rule.Separator
	}
	namespace := codeKey(stem)
	var last int64
	err := tx.QueryRow("SELECT last_value FROM inv_code_sequences WHERE namespace=?", namespace).Scan(&last)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	for seq := max(last+1, rule.Start); seq <= 999999999999; seq++ {
		code := fmt.Sprintf("%s%0*d", stem, rule.Digits, seq)
		var exists bool
		if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM inv_items WHERE code_key=?)", codeKey(code)).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			_, err := tx.Exec("INSERT INTO inv_code_sequences(namespace,last_value) VALUES(?,?) ON CONFLICT(namespace) DO UPDATE SET last_value=excluded.last_value", namespace, seq)
			return code, err
		}
	}
	return "", bad(409, "当前编码序号已用完，请修改编码前缀")
}

func (a *Module) writeItem(w http.ResponseWriter, r *http.Request) (any, error) {
	var raw map[string]json.RawMessage
	if e := decode(w, r, &raw); e != nil {
		return nil, e
	}
	if raw == nil {
		return nil, bad(400, "物料不能为空")
	}
	// CanProduct uses a.DB, so authorize an explicitly changed binding before opening the transaction.
	if v, ok := raw["product_id"]; ok && string(v) != "null" {
		var id int64
		if e := json.Unmarshal(v, &id); e != nil || id <= 0 {
			return nil, bad(400, "商品编号不正确")
		}
		if !a.CanProduct(core.CurrentUser(r), id) {
			return nil, bad(404, "商品不存在")
		}
	}
	id := core.ID(r, "id")
	return a.change(r, "item.update", "inv_item", func(tx *sql.Tx) (int64, any, error) {
		in := itemInput{Kind: "sku", Spec: map[string]any{}, Units: []unitConversion{}}
		oldUnit := ""
		if r.Method == "PATCH" {
			o, e := one(tx, "SELECT * FROM inv_items WHERE id=?", id)
			if e != nil {
				return 0, nil, e
			}
			if e = json.Unmarshal([]byte(core.JS(o)), &in); e != nil {
				return 0, nil, e
			}
			oldUnit = in.Unit
		}
		if e := json.Unmarshal([]byte(core.JS(raw)), &in); e != nil {
			return 0, nil, bad(400, "物料字段类型不正确")
		}
		in.Code = strings.TrimSpace(in.Code)
		if r.Method == "POST" && in.Code == "" {
			var err error
			in.Code, err = nextItemCode(tx)
			if err != nil {
				return 0, nil, err
			}
		}
		in.Name = strings.TrimSpace(in.Name)
		in.Unit = strings.TrimSpace(in.Unit)
		if !length(in.Code, 1, 64) || !length(in.Name, 1, 100) || !length(in.Unit, 1, 8) || !length(in.Note, 0, 500) || len(in.Spec) > 10 || len(in.Units) > 5 || in.SafetyStock < 0 {
			return 0, nil, bad(400, "物料字段超出范围")
		}
		if in.Kind != "sku" && in.Kind != "material" && in.Kind != "packaging" || in.Kind != "sku" && in.ProductID != nil {
			return 0, nil, bad(400, "物料类型或关联商品不正确")
		}
		if in.Spec == nil {
			in.Spec = map[string]any{}
		}
		if in.Units == nil {
			in.Units = []unitConversion{}
		}
		for _, u := range in.Units {
			if !length(strings.TrimSpace(u.Name), 1, 8) || u.Factor <= 0 {
				return 0, nil, bad(400, "换算单位不正确")
			}
		}
		if in.CategoryID != nil {
			var n int
			if e := tx.QueryRow("SELECT count(*) FROM categories WHERE id=?", *in.CategoryID).Scan(&n); e != nil {
				return 0, nil, e
			}
			if n == 0 {
				return 0, nil, bad(400, "品类不存在")
			}
		}
		if oldUnit != "" && oldUnit != in.Unit {
			var n int
			if e := tx.QueryRow("SELECT count(*) FROM inv_ledger WHERE item_id=?", id).Scan(&n); e != nil {
				return 0, nil, e
			}
			if n > 0 {
				return 0, nil, bad(409, "已有出入库记录，不能改基本单位")
			}
		}
		if in.Archived && id > 0 {
			var occupied bool
			if e := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM inv_batches WHERE item_id=? AND qty_left>0) OR EXISTS(SELECT 1 FROM inv_reservations WHERE item_id=?) OR EXISTS(SELECT 1 FROM inv_dropship WHERE item_id=? AND (qty IS NULL OR qty>0))`, id, id, id).Scan(&occupied); e != nil {
				return 0, nil, e
			}
			if occupied {
				return 0, nil, bad(409, "有库存或占用，不能归档")
			}
		}
		now := core.Now()
		args := []any{in.Code, codeKey(in.Code), in.Name, in.Kind, in.ProductID, in.CategoryID, core.JS(in.Spec), in.Unit, core.JS(in.Units), in.SafetyStock, in.Note, in.Archived, now}
		if r.Method == "POST" {
			args = append(args, core.CurrentUser(r).ID, now)
			res, e := tx.Exec(`INSERT INTO inv_items(code,code_key,name,kind,product_id,category_id,spec,unit,units,safety_stock,note,archived,updated_at,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
			if e != nil {
				return 0, nil, e
			}
			id, e = res.LastInsertId()
			if e != nil {
				return 0, nil, e
			}
		} else {
			args = append(args, id)
			if _, e := tx.Exec(`UPDATE inv_items SET code=?,code_key=?,name=?,kind=?,product_id=?,category_id=?,spec=?,unit=?,units=?,safety_stock=?,note=?,archived=?,updated_at=? WHERE id=?`, args...); e != nil {
				return 0, nil, e
			}
		}
		if e := AfterChange(tx, id); e != nil {
			return 0, nil, e
		}
		return id, core.Object{"id": id}, nil
	})
}
func (a *Module) enrich(r *http.Request, o core.Object, warehouseID int64) error {
	id := o["id"].(int64)
	s, e := Stock(a.DB, id)
	if e != nil {
		return e
	}
	if warehouseID > 0 {
		if e = a.DB.QueryRow("SELECT coalesce(sum(qty_left),0) FROM inv_batches WHERE item_id=? AND warehouse_id=?", id, warehouseID).Scan(&s.OnHand); e != nil {
			return e
		}
	}
	o["on_hand"] = s.OnHand
	o["dropship"] = s.Dropship
	o["unlimited"] = s.Unlimited
	o["reserved"] = s.Reserved
	o["available"] = s.Available
	o["low"] = !s.Unlimited && o["safety_stock"].(int64) > 0 && s.Available < o["safety_stock"].(int64)
	if costAllowed(r) {
		var cost int64
		if e = a.DB.QueryRow("SELECT coalesce(sum(cost_left),0) FROM inv_batches WHERE item_id=?", id).Scan(&cost); e != nil {
			return e
		}
		o["cost_value"] = cost
	}
	if pid, ok := o["product_id"].(int64); ok {
		name := ""
		if a.CanProduct(core.CurrentUser(r), pid) {
			if e = a.DB.QueryRow("SELECT name FROM products WHERE id=?", pid).Scan(&name); e != nil {
				return e
			}
		}
		o["product"] = core.Object{"id": pid, "name": name}
	} else {
		o["product"] = nil
	}
	return nil
}
func (a *Module) filteredItems(r *http.Request, lookup bool) ([]core.Object, error) {
	v := r.URL.Query()
	query := "SELECT * FROM inv_items WHERE 1=1"
	args := []any{}
	if lookup {
		codes := strings.Split(v.Get("codes"), ",")
		if len(codes) > 200 {
			return nil, bad(400, "一次最多查询 200 个编码")
		}
		query += " AND code_key IN (" + strings.TrimSuffix(strings.Repeat("?,", len(codes)), ",") + ")"
		for _, c := range codes {
			args = append(args, codeKey(strings.TrimSpace(c)))
		}
	} else {
		if v.Get("archived") != "1" {
			query += " AND archived=0"
		}
		if q := v.Get("q"); q != "" {
			query += " AND (instr(lower(code),lower(?))>0 OR instr(lower(name),lower(?))>0)"
			args = append(args, q, q)
		}
		for _, k := range []string{"kind", "category_id", "product_id"} {
			if s := v.Get(k); s != "" {
				query += " AND " + k + "=?"
				args = append(args, s)
			}
		}
	}
	rows, e := objects(a.DB, query+" ORDER BY id DESC", args...)
	if e != nil {
		return nil, e
	}
	wid, _ := strconv.ParseInt(v.Get("warehouse_id"), 10, 64)
	out := []core.Object{}
	for _, o := range rows {
		if e = a.enrich(r, o, wid); e != nil {
			return nil, e
		}
		if v.Get("low") == "1" && !o["low"].(bool) {
			continue
		}
		if wid > 0 {
			var n bool
			if e = a.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM inv_batches WHERE item_id=? AND warehouse_id=? AND qty_left>0) OR EXISTS(SELECT 1 FROM inv_dropship WHERE item_id=? AND warehouse_id=?)`, o["id"], wid, o["id"], wid).Scan(&n); e != nil {
				return nil, e
			}
			if !n {
				continue
			}
		}
		out = append(out, o)
	}
	return out, nil
}
func (a *Module) listItems(_ http.ResponseWriter, r *http.Request) (any, error) {
	rows, e := a.filteredItems(r, false)
	if e != nil {
		return nil, e
	}
	return paged(r, rows), nil
}
func (a *Module) lookup(_ http.ResponseWriter, r *http.Request) (any, error) {
	rows, e := a.filteredItems(r, true)
	if e != nil {
		return nil, e
	}
	return paged(r, rows), nil
}
func (a *Module) getItem(_ http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	o, e := one(a.DB, "SELECT * FROM inv_items WHERE id=?", id)
	if e != nil {
		return nil, e
	}
	if e = a.enrich(r, o, 0); e != nil {
		return nil, e
	}
	for _, s := range []struct{ k, q string }{
		{"levels", `SELECT w.id AS warehouse_id,w.name AS warehouse_name,w.type,coalesce(sum(b.qty_left),0) AS on_hand FROM inv_warehouses w LEFT JOIN inv_batches b ON b.warehouse_id=w.id AND b.item_id=? WHERE w.type IN ('own','stall') GROUP BY w.id ORDER BY w.sort,w.id`},
		{"dropship", `SELECT d.*,w.name AS warehouse_name FROM inv_dropship d JOIN inv_warehouses w ON w.id=d.warehouse_id WHERE d.item_id=?`},
		{"batches", `SELECT * FROM inv_batches WHERE item_id=? AND qty_left>0 ORDER BY received_at,id`},
		{"reservations", `SELECT * FROM inv_reservations WHERE item_id=? ORDER BY id`},
		{"ledger", ledgerSelect + ` WHERE l.item_id=? ORDER BY l.id DESC LIMIT 50`},
	} {
		rows, err := objects(a.DB, s.q, id)
		if err != nil {
			return nil, err
		}
		if s.k == "ledger" || s.k == "batches" || s.k == "reservations" {
			if err = a.documentRefs(r, rows); err != nil {
				return nil, err
			}
		}
		o[s.k] = redact(r, rows)
	}
	return o, nil
}
func (a *Module) export(w http.ResponseWriter, r *http.Request) (any, error) {
	rows, e := a.filteredItems(r, false)
	if e != nil {
		return nil, e
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="inventory.csv"`)
	if _, e = w.Write([]byte{0xef, 0xbb, 0xbf}); e != nil {
		return nil, e
	}
	cw := csv.NewWriter(w)
	if e = cw.Write([]string{"编码", "名称", "规格", "可售数"}); e != nil {
		return nil, e
	}
	for _, o := range rows {
		available := fmt.Sprint(o["available"])
		if o["unlimited"].(bool) {
			available = "不限"
		}
		spec := o["spec"].(map[string]any)
		keys := make([]string, 0, len(spec))
		for key := range spec {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprint(key, " ", spec[key]))
		}
		if e = cw.Write([]string{o["code"].(string), o["name"].(string), strings.Join(parts, " · "), available}); e != nil {
			return nil, e
		}
	}
	cw.Flush()
	return nil, cw.Error()
}
