package purchase

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"xstudio/internal/core"
)

type orderIn struct {
	SupplierID  int64    `json:"supplier_id"`
	Currency    string   `json:"currency"`
	ShippingFee int64    `json:"shipping_fee"`
	ExpectedAt  string   `json:"expected_at"`
	ProductID   *int64   `json:"product_id"`
	IsSample    bool     `json:"is_sample"`
	Note        string   `json:"note"`
	Lines       []lineIn `json:"lines"`
}
type lineIn struct {
	ItemID int64 `json:"item_id"`
	Qty    int64 `json:"qty"`
	Amount int64 `json:"amount"`
}

func (a *Module) listOrders(_ http.ResponseWriter, r *http.Request) (any, error) {
	query := `SELECT p.*,s.name AS supplier_name,(SELECT coalesce(sum(amount),0) FROM purchase_lines l WHERE l.po_id=p.id) AS goods_amount FROM purchase_orders p JOIN suppliers s ON s.id=p.supplier_id WHERE 1=1`
	args := []any{}
	v := r.URL.Query()
	for _, k := range []string{"status", "supplier_id", "is_sample"} {
		if s := v.Get(k); s != "" {
			query += " AND p." + k + "=?"
			args = append(args, s)
		}
	}
	if s := v.Get("item_id"); s != "" {
		query += " AND EXISTS(SELECT 1 FROM purchase_lines l WHERE l.po_id=p.id AND l.item_id=?)"
		args = append(args, s)
	}
	if s := v.Get("q"); s != "" {
		query += " AND instr(p.no,?)>0"
		args = append(args, s)
	}
	var total int64
	if err := a.DB.QueryRow("SELECT count(*) FROM ("+query+")", args...).Scan(&total); err != nil {
		return nil, err
	}
	offset, size := core.Page(r)
	args = append(args, size, offset)
	rows, err := objects(a.DB, query+" ORDER BY p.id DESC LIMIT ? OFFSET ?", args...)
	return core.Object{"items": rows, "total": total}, err
}
func (a *Module) getOrder(_ http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	p, err := one(a.DB, `SELECT p.*,s.name AS supplier_name FROM purchase_orders p JOIN suppliers s ON s.id=p.supplier_id WHERE p.id=?`, id)
	if err != nil {
		return nil, err
	}
	p["lines"], err = objects(a.DB, `SELECT l.*,i.code,i.name,i.unit FROM purchase_lines l JOIN inv_items i ON i.id=l.item_id WHERE l.po_id=? ORDER BY l.id`, id)
	if err != nil {
		return nil, err
	}
	receipts, err := objects(a.DB, `SELECT r.*,w.name AS warehouse_name FROM purchase_receipts r JOIN inv_warehouses w ON w.id=r.warehouse_id WHERE po_id=? ORDER BY r.id`, id)
	if err != nil {
		return nil, err
	}
	for _, receipt := range receipts {
		receipt["lines"], err = objects(a.DB, "SELECT * FROM purchase_receipt_lines WHERE receipt_id=? ORDER BY id", receipt["id"])
		if err != nil {
			return nil, err
		}
	}
	p["receipts"] = receipts
	p["returns"], err = objects(a.DB, "SELECT * FROM purchase_returns WHERE po_id=? ORDER BY id", id)
	return p, err
}
func validateOrder(tx *sql.Tx, u core.User, in *orderIn) error {
	in.Note = strings.TrimSpace(in.Note)
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	if !validCurrency(in.Currency) || in.ShippingFee < 0 || !validDate(in.ExpectedAt) || runeLen(in.Note) > 1000 {
		return bad(400, "币种、运费、预计到货日期或备注不正确")
	}
	var archived bool
	if err := tx.QueryRow("SELECT archived FROM suppliers WHERE id=?", in.SupplierID).Scan(&archived); err != nil {
		return bad(400, "供应商不存在")
	}
	if archived {
		return bad(409, "供应商已归档")
	}
	if in.ProductID != nil && *in.ProductID == 0 {
		in.ProductID = nil
	}
	if in.ProductID != nil {
		var n int
		query := "SELECT count(*) FROM products p WHERE p.id=?"
		args := []any{*in.ProductID}
		if u.Role.Key != "admin" {
			query += " AND (p.created_by=? OR EXISTS(SELECT 1 FROM listings l JOIN user_shops us ON us.shop_id=l.shop_id WHERE l.product_id=p.id AND us.user_id=?))"
			args = append(args, u.ID, u.ID)
		}
		if err := tx.QueryRow(query, args...).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return bad(404, "商品不存在")
		}
	}
	if len(in.Lines) == 0 || len(in.Lines) > 200 {
		return bad(400, "采购单需要 1–200 行明细")
	}
	var total int64
	for _, line := range in.Lines {
		if line.Qty <= 0 || line.Amount < 0 {
			return bad(400, "数量必须为正整数，金额不能为负")
		}
		var err error
		total, err = sum(total, line.Amount)
		if err != nil {
			return err
		}
		var arch bool
		if err = tx.QueryRow("SELECT archived FROM inv_items WHERE id=?", line.ItemID).Scan(&arch); err != nil {
			return bad(400, "物料不存在")
		}
		if arch {
			return bad(409, "物料已归档")
		}
	}
	if _, err := sum(total, in.ShippingFee); err != nil {
		return err
	}
	if total == 0 && in.ShippingFee > 0 {
		return bad(400, "明细金额为零时不能按金额分摊运费")
	}
	return nil
}
func saveLines(tx *sql.Tx, id int64, in orderIn) error {
	if _, err := tx.Exec("DELETE FROM purchase_lines WHERE po_id=?", id); err != nil {
		return err
	}
	var total, cumulative, allocated int64
	for _, l := range in.Lines {
		total += l.Amount
	}
	for _, l := range in.Lines {
		cumulative += l.Amount
		share := int64(0)
		if total > 0 {
			n, err := ratio(in.ShippingFee, cumulative, total)
			if err != nil {
				return err
			}
			share = n - allocated
			allocated = n
		}
		if _, err := tx.Exec("INSERT INTO purchase_lines(po_id,item_id,qty,amount,shipping_share) VALUES(?,?,?,?,?)", id, l.ItemID, l.Qty, l.Amount, share); err != nil {
			return err
		}
	}
	return nil
}
func (a *Module) createOrder(w http.ResponseWriter, r *http.Request) (any, error) {
	var in orderIn
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	return a.change(r, "create", func(tx *sql.Tx) (int64, any, any, error) {
		if err := validateOrder(tx, core.CurrentUser(r), &in); err != nil {
			return 0, nil, nil, err
		}
		no, err := number(tx, "PO", "purchase_orders", 3)
		if err != nil {
			return 0, nil, nil, err
		}
		now := core.Now()
		res, err := tx.Exec(`INSERT INTO purchase_orders(no,supplier_id,currency,shipping_fee,expected_at,product_id,is_sample,note,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, no, in.SupplierID, in.Currency, in.ShippingFee, in.ExpectedAt, in.ProductID, in.IsSample, in.Note, core.CurrentUser(r).ID, now, now)
		if err != nil {
			return 0, nil, nil, err
		}
		id, err := res.LastInsertId()
		if err == nil {
			err = saveLines(tx, id, in)
		}
		return id, core.Object{"id": id, "no": no}, core.Object{"no": no}, err
	})
}
func (a *Module) patchOrder(w http.ResponseWriter, r *http.Request) (any, error) {
	var patch map[string]json.RawMessage
	if err := decode(w, r, &patch); err != nil {
		return nil, err
	}
	id := core.ID(r, "id")
	return a.change(r, "update", func(tx *sql.Tx) (int64, any, any, error) {
		p, err := one(tx, "SELECT * FROM purchase_orders WHERE id=?", id)
		if err != nil {
			return 0, nil, nil, err
		}
		allowed := map[string]bool{"supplier_id": true, "currency": true, "shipping_fee": true, "expected_at": true, "product_id": true, "is_sample": true, "note": true, "lines": true}
		for k := range patch {
			if !allowed[k] {
				return 0, nil, nil, bad(400, "不支持修改字段："+k)
			}
		}
		if p["status"] == "done" || p["status"] == "canceled" {
			return 0, nil, nil, bad(409, "已完成或已取消的采购单不能修改")
		}
		if p["status"] != "draft" {
			for k := range patch {
				if k != "note" && k != "expected_at" {
					return 0, nil, nil, bad(409, "下单后只能修改备注和预计到货日期")
				}
			}
		}
		var in orderIn
		b, _ := json.Marshal(p)
		if err = json.Unmarshal(b, &in); err != nil {
			return 0, nil, nil, err
		}
		b, _ = json.Marshal(patch)
		if err = json.Unmarshal(b, &in); err != nil {
			return 0, nil, nil, bad(400, "字段类型不正确")
		}
		if p["status"] == "draft" {
			if _, ok := patch["lines"]; !ok {
				ls, e := objects(tx, "SELECT item_id,qty,amount FROM purchase_lines WHERE po_id=? ORDER BY id", id)
				if e != nil {
					return 0, nil, nil, e
				}
				b, _ = json.Marshal(ls)
				if err = json.Unmarshal(b, &in.Lines); err != nil {
					return 0, nil, nil, err
				}
			}
			if err = validateOrder(tx, core.CurrentUser(r), &in); err != nil {
				return 0, nil, nil, err
			}
			if err = saveLines(tx, id, in); err != nil {
				return 0, nil, nil, err
			}
		} else if !validDate(in.ExpectedAt) || runeLen(in.Note) > 1000 {
			return 0, nil, nil, bad(400, "日期或备注不正确")
		}
		_, err = tx.Exec(`UPDATE purchase_orders SET supplier_id=?,currency=?,shipping_fee=?,expected_at=?,product_id=?,is_sample=?,note=?,updated_at=? WHERE id=?`, in.SupplierID, in.Currency, in.ShippingFee, in.ExpectedAt, in.ProductID, in.IsSample, in.Note, core.Now(), id)
		return id, core.Object{"id": id}, core.Object{"id": id}, err
	})
}
func (a *Module) orderAction(_ http.ResponseWriter, r *http.Request) (any, error) {
	action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	id := core.ID(r, "id")
	return a.change(r, action, func(tx *sql.Tx) (int64, any, any, error) {
		p, err := one(tx, "SELECT * FROM purchase_orders WHERE id=?", id)
		if err != nil {
			return 0, nil, nil, err
		}
		status := p["status"]
		next := ""
		stamp := ""
		switch action {
		case "order":
			if status == "draft" {
				next = "ordered"
				stamp = "ordered_at"
			}
		case "finish":
			if status == "received" || status == "partial" {
				next = "done"
				stamp = "done_at"
			}
		case "cancel":
			if status == "draft" || status == "ordered" {
				next = "canceled"
				stamp = "done_at"
			}
		}
		if next == "" {
			return 0, nil, nil, bad(409, "当前采购状态不能执行此操作")
		}
		if action == "order" {
			var n int
			if err = tx.QueryRow("SELECT count(*) FROM suppliers WHERE id=? AND archived=0", p["supplier_id"]).Scan(&n); err != nil {
				return 0, nil, nil, err
			}
			if n == 0 {
				return 0, nil, nil, bad(409, "供应商已归档")
			}
			if err = tx.QueryRow("SELECT count(*) FROM purchase_lines l JOIN inv_items i ON i.id=l.item_id WHERE l.po_id=? AND i.archived=1", id).Scan(&n); err != nil {
				return 0, nil, nil, err
			}
			if n > 0 {
				return 0, nil, nil, bad(409, "明细中有已归档物料")
			}
		}
		_, err = tx.Exec("UPDATE purchase_orders SET status=?,updated_at=?,"+stamp+"=? WHERE id=?", next, core.Now(), core.Now(), id)
		return id, core.Object{"id": id, "status": next}, core.Object{"no": p["no"]}, err
	})
}
