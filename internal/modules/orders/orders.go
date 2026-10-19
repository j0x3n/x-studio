package orders

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/modules/inventory"
)

type lineIn struct {
	ItemID int64  `json:"item_id"`
	Qty    int64  `json:"qty"`
	Price  int64  `json:"price"`
	Amount *int64 `json:"amount"`
}
type orderIn struct {
	ShopID      int64    `json:"shop_id"`
	PlatformNo  string   `json:"platform_no"`
	BuyerName   string   `json:"buyer_name"`
	BuyerPhone  string   `json:"buyer_phone"`
	Address     string   `json:"address"`
	BuyerNote   string   `json:"buyer_note"`
	SellerNote  string   `json:"seller_note"`
	Currency    string   `json:"currency"`
	ShippingFee int64    `json:"shipping_fee"`
	Discount    int64    `json:"discount"`
	PaidAmount  *int64   `json:"paid_amount"`
	PaidAt      string   `json:"paid_at"`
	AllowShort  bool     `json:"allow_short"`
	Lines       []lineIn `json:"lines"`
}

// A forced order is short while its reservation plus free stock cannot cover
// the entire order. Replenishment can clear the flag without rewriting orders.
const shortExpr = `CASE WHEN o.status='paid' AND EXISTS (
 SELECT 1 FROM order_lines l WHERE l.order_id=o.id
 AND NOT EXISTS(SELECT 1 FROM inv_dropship d JOIN inv_warehouses w ON w.id=d.warehouse_id WHERE d.item_id=l.item_id AND d.qty IS NULL AND w.archived=0)
 AND (SELECT sum(l2.qty) FROM order_lines l2 WHERE l2.order_id=o.id AND l2.item_id=l.item_id) >
 coalesce((SELECT sum(r.qty) FROM inv_reservations r WHERE r.item_id=l.item_id AND r.ref_type='order' AND r.ref_id=o.id),0) +
 max(0,coalesce((SELECT sum(b.qty_left) FROM inv_batches b WHERE b.item_id=l.item_id),0) +
 coalesce((SELECT sum(d.qty) FROM inv_dropship d JOIN inv_warehouses w ON w.id=d.warehouse_id WHERE d.item_id=l.item_id AND w.archived=0),0) -
 coalesce((SELECT sum(r.qty) FROM inv_reservations r WHERE r.item_id=l.item_id),0))
 ) THEN 1 ELSE 0 END`

func (a *Module) list(_ http.ResponseWriter, r *http.Request) (any, error) {
	where, args := scope(core.CurrentUser(r), "o")
	query := "SELECT o.*,s.name AS shop_name," + shortExpr + " AS short FROM orders o JOIN shops s ON s.id=o.shop_id WHERE " + where
	v := r.URL.Query()
	for _, k := range []string{"shop_id", "status"} {
		if s := v.Get(k); s != "" {
			query += " AND o." + k + "=?"
			args = append(args, s)
		}
	}
	if s := v.Get("q"); s != "" {
		query += " AND (instr(o.no,?)>0 OR instr(o.platform_no,?)>0 OR instr(o.buyer_name,?)>0 OR instr(o.buyer_phone,?)>0 OR instr(o.tracking_no,?)>0)"
		for i := 0; i < 5; i++ {
			args = append(args, s)
		}
	}
	if s := v.Get("from"); s != "" {
		start, err := orderTime(s)
		if err != nil {
			return nil, bad(400, "开始日期不正确")
		}
		query += " AND o.paid_at>=?"
		args = append(args, orderTimestamp(start))
	}
	if s := v.Get("to"); s != "" {
		end, err := orderTime(s)
		if err != nil {
			return nil, bad(400, "结束日期不正确")
		}
		if len(s) == 10 {
			// A date includes the entire Chinese calendar day, at nanosecond precision.
			query += " AND o.paid_at<?"
			end = end.AddDate(0, 0, 1)
		} else {
			query += " AND o.paid_at<=?"
		}
		args = append(args, orderTimestamp(end))
	}
	if v.Get("short") == "1" {
		query += " AND (" + shortExpr + ")=1"
	}
	var total int64
	if err := a.DB.QueryRow("SELECT count(*) FROM ("+query+")", args...).Scan(&total); err != nil {
		return nil, err
	}
	offset, size := core.Page(r)
	rows, err := objects(a.DB, query+" ORDER BY o.id DESC LIMIT ? OFFSET ?", append(args, size, offset)...)
	return core.Object{"items": rows, "total": total}, err
}
func (a *Module) get(_ http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	u := core.CurrentUser(r)
	o, err := accessible(a.DB, u, id)
	if err != nil {
		return nil, err
	}
	short, err := orderShort(a.DB, id)
	if err != nil {
		return nil, err
	}
	o["short"] = short
	lines, err := objects(a.DB, "SELECT * FROM order_lines WHERE order_id=? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	for _, l := range lines {
		picks, e := objects(a.DB, "SELECT * FROM order_picks WHERE line_id=? ORDER BY id", l["id"])
		if e != nil {
			return nil, e
		}
		redact(u, picks)
		l["picks"] = picks
	}
	o["lines"] = lines
	returns, err := objects(a.DB, "SELECT * FROM order_returns WHERE order_id=? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	for _, ret := range returns {
		ret["lines"], err = objects(a.DB, "SELECT * FROM order_return_lines WHERE return_id=? ORDER BY line_id", ret["id"])
		if err != nil {
			return nil, err
		}
	}
	o["returns"] = returns
	return o, nil
}
func validate(in *orderIn) (int64, int64, error) {
	in.PlatformNo = strings.TrimSpace(in.PlatformNo)
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	if in.PaidAt == "" {
		in.PaidAt = core.Now()
	}
	if len(in.Currency) != 3 || strings.Trim(in.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" || !validDate(in.PaidAt) {
		return 0, 0, bad(400, "币种或付款日期不正确")
	}
	paidAt, err := orderTime(in.PaidAt)
	if err != nil {
		return 0, 0, bad(400, "付款日期不正确")
	}
	in.PaidAt = orderTimestamp(paidAt)
	for _, field := range []struct {
		value string
		limit int
	}{{in.PlatformNo, 100}, {in.BuyerName, 100}, {in.BuyerPhone, 100}, {in.Address, 1000}, {in.BuyerNote, 1000}, {in.SellerNote, 1000}} {
		if runeLen(field.value) > field.limit {
			return 0, 0, bad(400, "文本字段过长")
		}
	}
	if len(in.Lines) == 0 || len(in.Lines) > 200 || in.ShippingFee < 0 || in.Discount < 0 {
		return 0, 0, bad(400, "订单需要 1–200 行明细，金额不能为负")
	}
	var goods int64
	for i := range in.Lines {
		l := &in.Lines[i]
		if l.Qty <= 0 || l.Price < 0 {
			return 0, 0, bad(400, "数量必须为正整数，单价不能为负")
		}
		full, err := ratio(l.Price, l.Qty, 1)
		if err != nil {
			return 0, 0, err
		}
		if l.Amount == nil {
			l.Amount = &full
		}
		if *l.Amount < 0 {
			return 0, 0, bad(400, "实收金额不能为负")
		}
		goods, err = sum(goods, *l.Amount)
		if err != nil {
			return 0, 0, err
		}
	}
	total, err := sum(goods, in.ShippingFee)
	if err != nil {
		return 0, 0, err
	}
	if in.Discount > total {
		return 0, 0, bad(400, "优惠不能超过商品和运费合计")
	}
	paid := total - in.Discount
	if in.PaidAmount != nil {
		paid = *in.PaidAmount
	}
	if paid < 0 {
		return 0, 0, bad(400, "付款金额不能为负")
	}
	return goods, paid, nil
}
func writeLines(tx *sql.Tx, id int64, in orderIn) (bool, error) {
	if err := inventory.Release(tx, "order", id, 0); err != nil {
		return false, err
	}
	if _, err := tx.Exec("DELETE FROM order_lines WHERE order_id=?", id); err != nil {
		return false, err
	}
	quantities := map[int64]int64{}
	for _, l := range in.Lines {
		var code, name, spec, kind string
		var arch bool
		if err := tx.QueryRow("SELECT code,name,spec,kind,archived FROM inv_items WHERE id=?", l.ItemID).Scan(&code, &name, &spec, &kind, &arch); err != nil {
			return false, bad(400, "物料不存在")
		}
		if arch || kind != "sku" {
			return false, bad(400, "订单明细必须是未归档的成品 SKU")
		}
		details := map[string]json.RawMessage{}
		if err := json.Unmarshal([]byte(spec), &details); err != nil {
			return false, err
		}
		keys := []string{}
		for k := range details {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			value := string(details[k])
			var plain string
			if err := json.Unmarshal(details[k], &plain); err == nil && value != "null" {
				value = plain
			}
			name += " · " + k + "：" + value
		}
		if _, err := tx.Exec("INSERT INTO order_lines(order_id,item_id,code,title,qty,price,amount) VALUES(?,?,?,?,?,?,?)", id, l.ItemID, code, name, l.Qty, l.Price, *l.Amount); err != nil {
			return false, err
		}
		n, err := sum(quantities[l.ItemID], l.Qty)
		if err != nil {
			return false, err
		}
		quantities[l.ItemID] = n
	}
	short := false
	// Stable order also makes shortage errors deterministic.
	ids := []int64{}
	for id := range quantities {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, itemID := range ids {
		qty := quantities[itemID]
		if in.AllowShort {
			level, err := inventory.Stock(tx, itemID)
			if err != nil {
				return false, err
			}
			if !level.Unlimited && level.Available < qty {
				qty = max(0, level.Available)
				short = true
			}
		}
		if qty > 0 {
			if err := inventory.Reserve(tx, itemID, qty, "order", id); err != nil {
				return false, err
			}
		}
	}
	return short, nil
}
func (a *Module) create(w http.ResponseWriter, r *http.Request) (any, error) {
	var in orderIn
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	goods, paid, err := validate(&in)
	if err != nil {
		return nil, err
	}
	return a.change(r, "create", func(tx *sql.Tx) (int64, any, error) {
		if err := shopAllowed(tx, core.CurrentUser(r), in.ShopID); err != nil {
			return 0, nil, err
		}
		no, err := number(tx)
		if err != nil {
			return 0, nil, err
		}
		now := core.Now()
		res, err := tx.Exec(`INSERT INTO orders(no,shop_id,platform_no,buyer_name,buyer_phone,address,buyer_note,seller_note,currency,goods_amount,shipping_fee,discount,paid_amount,paid_at,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, no, in.ShopID, in.PlatformNo, in.BuyerName, in.BuyerPhone, in.Address, in.BuyerNote, in.SellerNote, in.Currency, goods, in.ShippingFee, in.Discount, paid, in.PaidAt, core.CurrentUser(r).ID, now, now)
		if err != nil {
			return 0, nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, nil, err
		}
		short, err := writeLines(tx, id, in)
		if err != nil {
			return 0, nil, err
		}
		_, err = tx.Exec("UPDATE orders SET short=? WHERE id=?", short, id)
		return id, core.Object{"id": id, "no": no, "short": short}, err
	})
}
func (a *Module) patch(w http.ResponseWriter, r *http.Request) (any, error) {
	var patch map[string]json.RawMessage
	if err := decode(w, r, &patch); err != nil {
		return nil, err
	}
	id := core.ID(r, "id")
	return a.change(r, "update", func(tx *sql.Tx) (int64, any, error) {
		o, err := accessible(tx, core.CurrentUser(r), id)
		if err != nil {
			return 0, nil, err
		}
		allowed := map[string]bool{"platform_no": true, "buyer_name": true, "buyer_phone": true, "address": true, "buyer_note": true, "seller_note": true, "currency": true, "shipping_fee": true, "discount": true, "paid_amount": true, "paid_at": true, "allow_short": true, "lines": true}
		for k := range patch {
			if !allowed[k] {
				return 0, nil, bad(400, "不支持修改字段："+k)
			}
			if o["status"] != "paid" && k != "seller_note" {
				return 0, nil, bad(409, "只有待发货订单能修改明细和金额")
			}
		}
		if o["status"] != "paid" {
			var in struct {
				SellerNote string `json:"seller_note"`
			}
			in.SellerNote = o["seller_note"].(string)
			if err = merge(patch, &in); err != nil {
				return 0, nil, err
			}
			if runeLen(in.SellerNote) > 1000 {
				return 0, nil, bad(400, "备注过长")
			}
			_, err = tx.Exec("UPDATE orders SET seller_note=?,updated_at=? WHERE id=?", in.SellerNote, core.Now(), id)
			return id, core.Object{"id": id}, err
		}
		var in orderIn
		b, _ := json.Marshal(o)
		if err = json.Unmarshal(b, &in); err != nil {
			return 0, nil, err
		}
		lines, err := objects(tx, "SELECT item_id,qty,price,amount FROM order_lines WHERE order_id=? ORDER BY id", id)
		if err != nil {
			return 0, nil, err
		}
		b, _ = json.Marshal(lines)
		if err = json.Unmarshal(b, &in.Lines); err != nil {
			return 0, nil, err
		}
		if _, ok := patch["lines"]; ok {
			in.Lines = nil // Replacement lines must not inherit omitted amounts from old slice elements.
		}
		if err = merge(patch, &in); err != nil {
			return 0, nil, err
		}
		financial := false
		for _, k := range []string{"lines", "shipping_fee", "discount"} {
			if _, ok := patch[k]; ok {
				financial = true
			}
		}
		if _, ok := patch["paid_amount"]; !ok && financial {
			in.PaidAmount = nil
		}
		goods, paid, err := validate(&in)
		if err != nil {
			return 0, nil, err
		}
		// Notes and monetary edits must not drop reservations from forced orders.
		if _, ok := patch["lines"]; ok {
			_, err = writeLines(tx, id, in)
			if err != nil {
				return 0, nil, err
			}
		}
		short, err := orderShort(tx, id)
		if err != nil {
			return 0, nil, err
		}
		_, err = tx.Exec(`UPDATE orders SET platform_no=?,buyer_name=?,buyer_phone=?,address=?,buyer_note=?,seller_note=?,currency=?,goods_amount=?,shipping_fee=?,discount=?,paid_amount=?,paid_at=?,short=?,updated_at=? WHERE id=?`, in.PlatformNo, in.BuyerName, in.BuyerPhone, in.Address, in.BuyerNote, in.SellerNote, in.Currency, goods, in.ShippingFee, in.Discount, paid, in.PaidAt, short, core.Now(), id)
		return id, core.Object{"id": id, "short": short}, err
	})
}

func orderShort(q inventory.Querier, id int64) (short bool, err error) {
	err = q.QueryRow("SELECT "+shortExpr+" FROM orders o WHERE o.id=?", id).Scan(&short)
	return
}
