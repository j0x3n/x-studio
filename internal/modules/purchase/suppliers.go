package purchase

import (
	"net/http"
	"strings"
	"xstudio/internal/core"
)

const supplierColumns = "id,name,contact,phone,wechat,address,products,settlement,payment_days,note,archived,created_at,updated_at"

type supplier struct {
	ID          int64
	Name        string
	Contact     string
	Phone       string
	Wechat      string
	Address     string
	Products    string
	Settlement  string
	PaymentDays int64
	Note        string
	Archived    bool
	CreatedAt   string
	UpdatedAt   string
}

func scanSupplier(s core.Scanner) (supplier, error) {
	var v supplier
	var archived int
	err := s.Scan(&v.ID, &v.Name, &v.Contact, &v.Phone, &v.Wechat, &v.Address, &v.Products, &v.Settlement, &v.PaymentDays, &v.Note, &archived, &v.CreatedAt, &v.UpdatedAt)
	v.Archived = archived != 0
	return v, err
}

func (a *Module) supplierJSON(v supplier) core.Object {
	return core.Object{
		"id":           v.ID,
		"name":         v.Name,
		"contact":      v.Contact,
		"phone":        v.Phone,
		"wechat":       v.Wechat,
		"address":      v.Address,
		"products":     v.Products,
		"settlement":   v.Settlement,
		"payment_days": v.PaymentDays,
		"note":         v.Note,
		"archived":     v.Archived,
		"created_at":   v.CreatedAt,
		"updated_at":   v.UpdatedAt,
	}
}

type supplierIn struct {
	Name        *string `json:"name"`
	Contact     *string `json:"contact"`
	Phone       *string `json:"phone"`
	Wechat      *string `json:"wechat"`
	Address     *string `json:"address"`
	Products    *string `json:"products"`
	Settlement  *string `json:"settlement"`
	PaymentDays *int64  `json:"payment_days"`
	Note        *string `json:"note"`
	Archived    *bool   `json:"archived"`
}

func (a *Module) applySupplierIn(v supplier, in supplierIn, partial bool) (supplier, error) {
	if !partial || in.Name != nil {
		if in.Name == nil {
			return v, bad(400, "供应商名称需要 1–50 字")
		}
		v.Name = strings.TrimSpace(*in.Name)
	}
	if !partial || in.Contact != nil {
		if in.Contact != nil {
			v.Contact = strings.TrimSpace(*in.Contact)
		}
	}
	if !partial || in.Phone != nil {
		if in.Phone != nil {
			v.Phone = strings.TrimSpace(*in.Phone)
		}
	}
	if !partial || in.Wechat != nil {
		if in.Wechat != nil {
			v.Wechat = strings.TrimSpace(*in.Wechat)
		}
	}
	if !partial || in.Address != nil {
		if in.Address != nil {
			v.Address = strings.TrimSpace(*in.Address)
		}
	}
	if !partial || in.Products != nil {
		if in.Products != nil {
			v.Products = strings.TrimSpace(*in.Products)
		}
	}
	if !partial || in.Settlement != nil {
		if in.Settlement == nil || (!partial && *in.Settlement == "") {
			v.Settlement = "cash"
		} else if *in.Settlement != "cash" && *in.Settlement != "monthly" {
			return v, bad(400, "结算方式只能是 cash 或 monthly")
		} else {
			v.Settlement = *in.Settlement
		}
	}
	if !partial || in.PaymentDays != nil {
		if in.PaymentDays == nil {
			v.PaymentDays = 0
		} else if *in.PaymentDays < 0 || *in.PaymentDays > 365 {
			return v, bad(400, "账期需要在 0–365 天之间")
		} else {
			v.PaymentDays = *in.PaymentDays
		}
	}
	if !partial || in.Note != nil {
		if in.Note != nil {
			v.Note = strings.TrimSpace(*in.Note)
		}
	}
	if !partial || in.Archived != nil {
		if in.Archived != nil {
			v.Archived = *in.Archived
		}
	}
	if v.Name == "" || runeLen(v.Name) > 50 {
		return v, bad(400, "供应商名称需要 1–50 字")
	}
	if runeLen(v.Contact) > 200 || runeLen(v.Phone) > 200 || runeLen(v.Wechat) > 200 || runeLen(v.Address) > 200 || runeLen(v.Products) > 200 {
		return v, bad(400, "文本字段不能超过 200 字")
	}
	if runeLen(v.Note) > 1000 {
		return v, bad(400, "备注不能超过 1000 字")
	}
	if v.Settlement != "cash" && v.Settlement != "monthly" {
		return v, bad(400, "结算方式只能是 cash 或 monthly")
	}
	if v.PaymentDays < 0 || v.PaymentDays > 365 {
		return v, bad(400, "账期需要在 0–365 天之间")
	}
	var dup int
	if err := a.DB.QueryRow("SELECT count(*) FROM suppliers WHERE name=? AND id<>?", v.Name, v.ID).Scan(&dup); err != nil {
		return v, err
	}
	if dup > 0 {
		return v, bad(409, "已有同名供应商")
	}
	return v, nil
}

func canViewSupplier(u core.User) bool {
	if u.Role.Key != "admin" && u.Role.Scope == "shares" {
		return false
	}
	return u.Has(core.PurchaseManage) || u.Has(core.InventoryView) || u.Has(core.FinanceView)
}

func canManageSupplier(u core.User) bool {
	if u.Role.Key != "admin" && u.Role.Scope == "shares" {
		return false
	}
	return u.Has(core.PurchaseManage)
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (a *Module) listSuppliers(w http.ResponseWriter, r *http.Request) {
	u := core.CurrentUser(r)
	if !canViewSupplier(u) {
		core.Fail(w, 403, "没有此操作的权限")
		return
	}
	q := r.URL.Query()
	parts := []string{"1=1"}
	var args []any
	if q.Get("archived") != "1" {
		parts = append(parts, "archived=0")
	}
	if raw := strings.TrimSpace(q.Get("q")); raw != "" {
		pat := "%" + escapeLike(raw) + "%"
		parts = append(parts, "(name LIKE ? ESCAPE '\\' OR contact LIKE ? ESCAPE '\\' OR products LIKE ? ESCAPE '\\')")
		args = append(args, pat, pat, pat)
	}
	where := strings.Join(parts, " AND ")
	var total int
	if err := a.DB.QueryRow("SELECT count(*) FROM suppliers WHERE "+where, args...).Scan(&total); err != nil {
		writeErr(w, err, "统计供应商失败：", "读取供应商失败")
		return
	}
	offset, size := core.Page(r)
	qargs := append(append([]any{}, args...), size, offset)
	rows, err := a.DB.Query("SELECT "+supplierColumns+" FROM suppliers WHERE "+where+" ORDER BY name, id LIMIT ? OFFSET ?", qargs...)
	if err != nil {
		writeErr(w, err, "读取供应商失败：", "读取供应商失败")
		return
	}
	defer rows.Close()
	items := []core.Object{}
	for rows.Next() {
		v, e := scanSupplier(rows)
		if e != nil {
			writeErr(w, e, "读取供应商失败：", "读取供应商失败")
			return
		}
		items = append(items, a.supplierJSON(v))
	}
	if err = rows.Err(); err != nil {
		writeErr(w, err, "读取供应商失败：", "读取供应商失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"items": items, "total": total})
}

func (a *Module) getSupplier(w http.ResponseWriter, r *http.Request) {
	u := core.CurrentUser(r)
	if !canViewSupplier(u) {
		core.Fail(w, 403, "没有此操作的权限")
		return
	}
	v, err := scanSupplier(a.DB.QueryRow("SELECT "+supplierColumns+" FROM suppliers WHERE id=?", core.ID(r, "id")))
	if err != nil {
		core.Fail(w, 404, "供应商不存在")
		return
	}
	core.WriteJSON(w, 200, core.Object{"supplier": a.supplierJSON(v)})
}

func (a *Module) createSupplier(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	if !canManageSupplier(core.CurrentUser(r)) {
		core.Fail(w, 403, "没有此操作的权限")
		return
	}
	var in supplierIn
	if !core.Decode(w, r, &in) {
		return
	}
	v, err := a.applySupplierIn(supplier{}, in, false)
	if err != nil {
		writeErr(w, err, "", "")
		return
	}
	ts := core.Now()
	res, err := a.DB.Exec(`INSERT INTO suppliers(name,contact,phone,wechat,address,products,settlement,payment_days,note,archived,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.Name, v.Contact, v.Phone, v.Wechat, v.Address, v.Products, v.Settlement, v.PaymentDays, v.Note, boolInt(v.Archived), ts, ts)
	if err != nil {
		writeErr(w, err, "创建供应商失败：", "创建供应商失败")
		return
	}
	v.ID, _ = res.LastInsertId()
	v.CreatedAt, v.UpdatedAt = ts, ts
	a.Audit(r, "supplier.create", "supplier", v.ID, a.supplierJSON(v))
	core.WriteJSON(w, 201, core.Object{"supplier": a.supplierJSON(v)})
}

func (a *Module) patchSupplier(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	if !canManageSupplier(core.CurrentUser(r)) {
		core.Fail(w, 403, "没有此操作的权限")
		return
	}
	v, err := scanSupplier(a.DB.QueryRow("SELECT "+supplierColumns+" FROM suppliers WHERE id=?", core.ID(r, "id")))
	if err != nil {
		core.Fail(w, 404, "供应商不存在")
		return
	}
	var in supplierIn
	if !core.Decode(w, r, &in) {
		return
	}
	v, err = a.applySupplierIn(v, in, true)
	if err != nil {
		writeErr(w, err, "", "")
		return
	}
	ts := core.Now()
	if _, err = a.DB.Exec(`UPDATE suppliers SET name=?,contact=?,phone=?,wechat=?,address=?,products=?,settlement=?,payment_days=?,note=?,archived=?,updated_at=? WHERE id=?`,
		v.Name, v.Contact, v.Phone, v.Wechat, v.Address, v.Products, v.Settlement, v.PaymentDays, v.Note, boolInt(v.Archived), ts, v.ID); err != nil {
		writeErr(w, err, "保存供应商失败：", "保存供应商失败")
		return
	}
	v.UpdatedAt = ts
	a.Audit(r, "supplier.update", "supplier", v.ID, a.supplierJSON(v))
	core.WriteJSON(w, 200, core.Object{"supplier": a.supplierJSON(v)})
}
