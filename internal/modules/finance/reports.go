package finance

import (
	"database/sql"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"
	"xstudio/internal/core"
)

const reportBasis = "销售收入和实际出库成本统一按发货月份确认，时间用中国时区。待发货订单的收款按付款月份单独列示，属于预收，不计入销售收入或利润，避免跨月发货导致收入和成本错期。退款按售后登记时间计入，退货回仓把对应成本加回。代发零成本且缺少历史确认的，按未知成本处理，不用 0 去算利润。平台费用和运费险只统计已导入且匹配订单的对账行，按结算日期计入。费用按发生日期记在店铺；公司公共费用仅在全部店铺的公司汇总计入，不分摊至某一款。金额以分记录，换算人民币使用所选月份汇率；缺汇率保留未换算。"

type moneyPart struct {
	Currency string `json:"currency"`
	Amount   int64  `json:"amount"`
}

type moneyView struct {
	Currency             string      `json:"currency"`
	Revenue              int64       `json:"revenue"`
	UnshippedRevenue     int64       `json:"unshipped_revenue"`
	Refund               int64       `json:"refund"`
	UnallocatedRefund    int64       `json:"unallocated_refund"`
	PlatformFee          int64       `json:"platform_fee"`
	ShippingInsurance    int64       `json:"shipping_insurance"`
	Expense              int64       `json:"expense"`
	Cost                 *int64      `json:"cost"`
	CostKnown            bool        `json:"cost_known"`
	CostHidden           bool        `json:"cost_hidden"`
	CostCurrency         *string     `json:"cost_currency"`
	CostParts            []moneyPart `json:"cost_parts"`
	KnownCostParts       []moneyPart `json:"known_cost_parts"`
	UnknownCostCount     int         `json:"unknown_cost_count"`
	RateMicros           *int64      `json:"rate_micros"`
	CostRateMicros       *int64      `json:"cost_rate_micros"`
	Converted            bool        `json:"converted"`
	CNYRevenue           *int64      `json:"cny_revenue"`
	CNYUnshippedRevenue  *int64      `json:"cny_unshipped_revenue"`
	CNYRefund            *int64      `json:"cny_refund"`
	CNYUnallocatedRefund *int64      `json:"cny_unallocated_refund"`
	CNYPlatformFee       *int64      `json:"cny_platform_fee"`
	CNYShippingInsurance *int64      `json:"cny_shipping_insurance"`
	CNYExpense           *int64      `json:"cny_expense"`
	CNYCost              *int64      `json:"cny_cost"`
	CNYProfit            *int64      `json:"cny_profit"`
	Profit               *int64      `json:"profit"`
}

type rollup struct {
	Currencies           []moneyView `json:"currencies"`
	CNYReady             bool        `json:"cny_ready"`
	CNYRevenue           *int64      `json:"cny_revenue"`
	CNYUnshippedRevenue  *int64      `json:"cny_unshipped_revenue"`
	CNYRefund            *int64      `json:"cny_refund"`
	CNYUnallocatedRefund *int64      `json:"cny_unallocated_refund"`
	CNYPlatformFee       *int64      `json:"cny_platform_fee"`
	CNYShippingInsurance *int64      `json:"cny_shipping_insurance"`
	CNYExpense           *int64      `json:"cny_expense"`
	CNYCost              *int64      `json:"cny_cost"`
	CNYProfit            *int64      `json:"cny_profit"`
}

type shopReport struct {
	ShopID   int64  `json:"shop_id"`
	ShopName string `json:"shop_name"`
	rollup
}

type itemReport struct {
	ShopID      int64  `json:"shop_id"`
	ShopName    string `json:"shop_name"`
	ItemID      int64  `json:"item_id"`
	ProductID   *int64 `json:"product_id"`
	Code        string `json:"code"`
	Title       string `json:"title"`
	ProductName string `json:"product_name"`
	moneyView
}

type companyExpense struct {
	Currency         string `json:"currency"`
	Amount           int64  `json:"amount"`
	Converted        bool   `json:"converted"`
	CNY              *int64 `json:"cny"`
	IncludedInTotals bool   `json:"included_in_totals"`
}

type missingRate struct {
	Currency string `json:"currency"`
	Reason   string `json:"reason"`
}

type reportNote struct {
	Kind    string `json:"kind"`
	ShopID  int64  `json:"shop_id"`
	OrderID int64  `json:"order_id,omitempty"`
	OrderNo string `json:"order_no,omitempty"`
	LineID  int64  `json:"line_id,omitempty"`
	ItemID  int64  `json:"item_id,omitempty"`
	Message string `json:"message"`
}

type reportOut struct {
	Month                  string           `json:"month"`
	ShopID                 *int64           `json:"shop_id"`
	Timezone               string           `json:"timezone"`
	Basis                  string           `json:"basis"`
	CostVisible            bool             `json:"cost_visible"`
	IncludesCompanyExpense bool             `json:"includes_company_expense"`
	ItemExpenseIncluded    bool             `json:"item_expense_included"`
	MissingRates           []missingRate    `json:"missing_rates"`
	Totals                 rollup           `json:"totals"`
	CompanyExpenses        []companyExpense `json:"company_expenses"`
	Shops                  []shopReport     `json:"shops"`
	Items                  []itemReport     `json:"items"`
	Notes                  []reportNote     `json:"notes"`
	NotesTruncated         bool             `json:"notes_truncated"`
}

type bag struct {
	revenue, unshipped, refund, unalloc, fee, insurance, expense int64
}

type group struct {
	bags     map[string]*bag
	cost     map[string]int64
	partial  map[string]int64
	unknown  map[int64]struct{}
	shopName string
}

type itemKey struct {
	shop, item int64
	currency   string
}

type itemMeta struct {
	shopID, itemID int64
	shopName, code string
	title, product string
	currency       string
	productID      *int64
}

type itemHold struct {
	group group
	meta  itemMeta
}

type builder struct {
	showCost bool
	start    time.Time
	end      time.Time
	rates    map[string]int64
	grand    *group
	shops    map[int64]*group
	items    map[itemKey]*itemHold
	company  map[string]int64
	notes    []reportNote
	cut      bool
	seenShip map[int64]struct{}
	seenCost map[int64]struct{}
	missing  map[string]string
	feeSkip  int
}

func newGroup() *group {
	return &group{bags: map[string]*bag{}, cost: map[string]int64{}, partial: map[string]int64{}, unknown: map[int64]struct{}{}}
}

func (b *builder) shopGroup(id int64, name string) *group {
	g := b.shops[id]
	if g == nil {
		g = newGroup()
		b.shops[id] = g
	}
	if name != "" {
		g.shopName = name
	}
	return g
}

func (b *builder) itemGroup(meta itemMeta) *group {
	key := itemKey{meta.shopID, meta.itemID, meta.currency}
	hold := b.items[key]
	if hold == nil {
		hold = &itemHold{group: *newGroup(), meta: meta}
		b.items[key] = hold
	}
	if hold.meta.code == "" && meta.code != "" {
		hold.meta.code = meta.code
	}
	if hold.meta.title == "" && meta.title != "" {
		hold.meta.title = meta.title
	}
	if hold.meta.product == "" && meta.product != "" {
		hold.meta.product = meta.product
	}
	if hold.meta.productID == nil && meta.productID != nil {
		hold.meta.productID = meta.productID
	}
	if hold.meta.shopName == "" && meta.shopName != "" {
		hold.meta.shopName = meta.shopName
	}
	return &hold.group
}

func (g *group) bag(cur string) *bag {
	item := g.bags[cur]
	if item == nil {
		item = &bag{}
		g.bags[cur] = item
	}
	return item
}

func (g *group) touched() bool {
	return len(g.bags) > 0 || len(g.cost) > 0 || len(g.partial) > 0 || len(g.unknown) > 0
}

func addMap(m map[string]int64, cur string, n int64) error {
	v, err := addSigned(m[cur], n)
	if err != nil {
		return err
	}
	m[cur] = v
	return nil
}

func (b *builder) note(n reportNote) {
	if len(b.notes) >= 100 {
		b.cut = true
		return
	}
	b.notes = append(b.notes, n)
}

func (b *builder) addRevenue(shop int64, shopName, cur string, meta *itemMeta, amount int64, unshipped bool) error {
	sg := b.shopGroup(shop, shopName)
	for _, g := range []*group{b.grand, sg} {
		bag := g.bag(cur)
		var err error
		if unshipped {
			if bag.unshipped, err = addSigned(bag.unshipped, amount); err != nil {
				return err
			}
		} else if bag.revenue, err = addSigned(bag.revenue, amount); err != nil {
			return err
		}
	}
	if meta == nil {
		return nil
	}
	bag := b.itemGroup(*meta).bag(cur)
	var err error
	if unshipped {
		bag.unshipped, err = addSigned(bag.unshipped, amount)
	} else {
		bag.revenue, err = addSigned(bag.revenue, amount)
	}
	return err
}

func (b *builder) addRefund(shop int64, shopName, cur string, meta *itemMeta, amount int64, unalloc bool) error {
	if meta != nil {
		bag := b.itemGroup(*meta).bag(cur)
		var err error
		bag.refund, err = addSigned(bag.refund, amount)
		return err
	}
	for _, g := range []*group{b.grand, b.shopGroup(shop, shopName)} {
		bag := g.bag(cur)
		var err error
		if bag.refund, err = addSigned(bag.refund, amount); err != nil {
			return err
		}
		if unalloc {
			if bag.unalloc, err = addSigned(bag.unalloc, amount); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *builder) addFee(shop int64, shopName, cur string, meta *itemMeta, fee, insurance int64) error {
	var targets []*group
	if meta != nil {
		targets = []*group{b.itemGroup(*meta)}
	} else {
		targets = []*group{b.grand, b.shopGroup(shop, shopName)}
	}
	for _, g := range targets {
		bag := g.bag(cur)
		var err error
		if bag.fee, err = addSigned(bag.fee, fee); err != nil {
			return err
		}
		if bag.insurance, err = addSigned(bag.insurance, insurance); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) addExpense(shop int64, shopName, cur string, amount int64, company bool) error {
	if company {
		if err := addMap(b.company, cur, amount); err != nil {
			return err
		}
		bag := b.grand.bag(cur)
		var err error
		bag.expense, err = addSigned(bag.expense, amount)
		return err
	}
	bag := b.shopGroup(shop, shopName).bag(cur)
	var err error
	if bag.expense, err = addSigned(bag.expense, amount); err != nil {
		return err
	}
	bag = b.grand.bag(cur)
	bag.expense, err = addSigned(bag.expense, amount)
	return err
}

func (b *builder) addCost(meta itemMeta, cur string, amount int64, partial bool) error {
	groups := []*group{b.grand, b.shopGroup(meta.shopID, meta.shopName), b.itemGroup(meta)}
	for _, g := range groups {
		dst := g.cost
		if partial {
			dst = g.partial
		}
		if err := addMap(dst, cur, amount); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) markUnknown(meta itemMeta, lineID, orderID int64, orderNo, reason string) {
	for _, g := range []*group{b.grand, b.shopGroup(meta.shopID, meta.shopName), b.itemGroup(meta)} {
		g.unknown[lineID] = struct{}{}
	}
	if _, ok := b.seenCost[lineID]; ok {
		return
	}
	b.seenCost[lineID] = struct{}{}
	b.note(reportNote{Kind: "unknown_cost", ShopID: meta.shopID, OrderID: orderID, OrderNo: orderNo, LineID: lineID, ItemID: meta.itemID, Message: reason})
}

func (a *Module) report(_ http.ResponseWriter, r *http.Request) (any, error) {
	u := core.CurrentUser(r)
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if month == "" {
		month = time.Now().In(china).Format("2006-01")
	}
	start, end, err := monthRange(month)
	if err != nil || start.IsZero() {
		return nil, bad(400, "月份应为 YYYY-MM")
	}
	shopID, _, err := queryID(r, "shop_id")
	if err != nil {
		return nil, err
	}
	where, args, err := a.shopScopeSQL(u, shopID, "o.shop_id")
	if err != nil {
		return nil, err
	}
	rates, err := a.ratesFor(month)
	if err != nil {
		return nil, err
	}
	b := &builder{
		showCost: u.Has(core.ProductCost), start: start, end: end, rates: rates,
		grand: newGroup(), shops: map[int64]*group{}, items: map[itemKey]*itemHold{},
		company: map[string]int64{}, notes: []reportNote{}, seenShip: map[int64]struct{}{},
		seenCost: map[int64]struct{}{}, missing: map[string]string{},
	}
	if err = a.loadRevenue(b, where, args); err != nil {
		return nil, err
	}
	if b.showCost {
		if err = a.loadShippedCost(b, where, args); err != nil {
			return nil, err
		}
		if err = a.loadRestock(b, where, args); err != nil {
			return nil, err
		}
	}
	if err = a.loadRefunds(b, where, args); err != nil {
		return nil, err
	}
	if err = a.loadFees(b, u, shopID); err != nil {
		return nil, err
	}
	if err = a.loadReportExpenses(b, u, shopID); err != nil {
		return nil, err
	}
	if b.feeSkip > 0 {
		b.note(reportNote{Kind: "fee_currency", Message: "有对账行的币种和订单币种不一致，这些平台费用和运费险没有计入"})
	}
	return b.finish(month, shopID, companyAccess(u) && shopID == 0), nil
}

type lineBit struct {
	id, itemID, qty, amount int64
	code, title, product    string
	productID               *int64
}

type orderBit struct {
	id, shopID, paid int64
	no, shopName     string
	status, currency string
	when             string
	lines            []lineBit
	seen             map[int64]struct{}
}

func (a *Module) loadRevenue(b *builder, where string, args []any) error {
	likes := monthLikes(b.start)
	recognizedAt := "CASE WHEN o.status='shipped' THEN o.shipped_at ELSE o.paid_at END"
	qargs := append([]any{likes[0], likes[1], likes[2]}, args...)
	rows, err := a.DB.Query(`SELECT o.id,o.no,o.shop_id,coalesce(s.name,''),o.status,o.currency,o.paid_amount,`+recognizedAt+`,
l.id,l.item_id,l.code,l.title,l.qty,l.amount,i.product_id,coalesce(p.name,'')
FROM orders o JOIN shops s ON s.id=o.shop_id
LEFT JOIN order_lines l ON l.order_id=o.id
LEFT JOIN inv_items i ON i.id=l.item_id
LEFT JOIN products p ON p.id=i.product_id
WHERE o.status IN ('paid','shipped') AND `+likeSQL(recognizedAt)+` AND `+where, qargs...)
	if err != nil {
		return err
	}
	defer rows.Close()
	orders := map[int64]*orderBit{}
	for rows.Next() {
		var o orderBit
		var lineID, itemID, qty, amount, productID sql.NullInt64
		var code, title, product sql.NullString
		if err = rows.Scan(&o.id, &o.no, &o.shopID, &o.shopName, &o.status, &o.currency, &o.paid, &o.when, &lineID, &itemID, &code, &title, &qty, &amount, &productID, &product); err != nil {
			return err
		}
		got := orders[o.id]
		if got == nil {
			o.seen = map[int64]struct{}{}
			orders[o.id] = &o
			got = orders[o.id]
		}
		if !lineID.Valid || lineID.Int64 == 0 {
			continue
		}
		if _, ok := got.seen[lineID.Int64]; ok {
			continue
		}
		got.seen[lineID.Int64] = struct{}{}
		got.lines = append(got.lines, lineBit{id: lineID.Int64, itemID: itemID.Int64, qty: qty.Int64, amount: amount.Int64, code: code.String, title: title.String, product: product.String, productID: shopPtr(productID)})
	}
	if err = rows.Err(); err != nil {
		return err
	}
	ids := mapKeys(orders)
	for _, id := range ids {
		o := orders[id]
		if !inSpan(o.when, b.start, b.end) {
			continue
		}
		unshipped := o.status == "paid"
		if unshipped {
			if _, ok := b.seenShip[o.id]; !ok {
				b.seenShip[o.id] = struct{}{}
				b.note(reportNote{Kind: "unshipped", ShopID: o.shopID, OrderID: o.id, OrderNo: o.no, Message: "待发货收款单独列示，不计入销售收入与利润"})
			}
		}
		if len(o.lines) == 0 {
			if err = b.addRevenue(o.shopID, o.shopName, o.currency, nil, o.paid, unshipped); err != nil {
				return err
			}
			continue
		}
		weights := make([]int64, len(o.lines))
		byQty := true
		for _, ln := range o.lines {
			if ln.amount > 0 {
				byQty = false
				break
			}
		}
		for i, ln := range o.lines {
			if byQty {
				weights[i] = ln.qty
			} else {
				weights[i] = ln.amount
			}
		}
		parts, err := splitAmounts(o.paid, weights)
		if err != nil {
			return err
		}
		for i, ln := range o.lines {
			meta := itemMeta{shopID: o.shopID, shopName: o.shopName, itemID: ln.itemID, code: ln.code, title: ln.title, product: ln.product, currency: o.currency, productID: ln.productID}
			if err = b.addRevenue(o.shopID, o.shopName, o.currency, &meta, parts[i], unshipped); err != nil {
				return err
			}
		}
	}
	return nil
}

type pickBit struct {
	id       int64
	batch    sql.NullInt64
	cost     int64
	currency string
	unit     sql.NullInt64
	hit      bool
}

type costLine struct {
	meta    itemMeta
	orderID int64
	orderNo string
	qty     int64
	picks   []pickBit
	seen    map[int64]struct{}
}

func classifyPicks(picks []pickBit) (bool, map[string]int64, string, error) {
	if len(picks) == 0 {
		return false, map[string]int64{}, "已发货但没有出库成本记录", nil
	}
	costs := map[string]int64{}
	unknown := false
	for _, p := range picks {
		if p.currency == "" || !validCurrency(p.currency) {
			return false, costs, "出库成本币种不一致", nil
		}
		dropUnknown := !p.batch.Valid && p.cost == 0
		if dropUnknown {
			unknown = true
			continue
		}
		var err error
		costs[p.currency], err = addSigned(costs[p.currency], p.cost)
		if err != nil {
			return false, nil, "", err
		}
	}
	if unknown {
		return false, costs, "代发零成本缺少历史确认，请核对", nil
	}
	if len(costs) != 1 {
		return false, costs, "出库成本币种不一致", nil
	}
	return true, costs, "", nil
}

func (a *Module) loadShippedCost(b *builder, where string, args []any) error {
	likes := monthLikes(b.start)
	qargs := append([]any{likes[0], likes[1], likes[2]}, args...)
	rows, err := a.DB.Query(`SELECT o.id,o.no,o.shop_id,coalesce(s.name,''),o.currency,o.shipped_at,
l.id,l.item_id,l.code,l.title,l.qty,i.product_id,coalesce(p.name,''),
pk.id,pk.batch_id,pk.cost,pk.currency,d.unit_cost,CASE WHEN d.item_id IS NULL THEN 0 ELSE 1 END
FROM orders o JOIN shops s ON s.id=o.shop_id
JOIN order_lines l ON l.order_id=o.id
LEFT JOIN inv_items i ON i.id=l.item_id
LEFT JOIN products p ON p.id=i.product_id
LEFT JOIN order_picks pk ON pk.line_id=l.id
LEFT JOIN inv_dropship d ON pk.id IS NOT NULL AND pk.batch_id IS NULL AND d.warehouse_id=pk.warehouse_id AND d.item_id=l.item_id
WHERE o.status='shipped' AND `+likeSQL("o.shipped_at")+` AND `+where, qargs...)
	if err != nil {
		return err
	}
	defer rows.Close()
	lines := map[int64]*costLine{}
	orderAt := map[int64]string{}
	for rows.Next() {
		var orderID, shopID, lineID, itemID, qty int64
		var no, shopName, salesCur, shippedAt, code, title, product string
		var productID, pickID, batch, unit sql.NullInt64
		var cost sql.NullInt64
		var pickCur sql.NullString
		var hit int
		if err = rows.Scan(&orderID, &no, &shopID, &shopName, &salesCur, &shippedAt, &lineID, &itemID, &code, &title, &qty, &productID, &product, &pickID, &batch, &cost, &pickCur, &unit, &hit); err != nil {
			return err
		}
		orderAt[orderID] = shippedAt
		ln := lines[lineID]
		if ln == nil {
			ln = &costLine{meta: itemMeta{shopID: shopID, shopName: shopName, itemID: itemID, code: code, title: title, product: product, currency: salesCur, productID: shopPtr(productID)}, orderID: orderID, orderNo: no, qty: qty, seen: map[int64]struct{}{}}
			lines[lineID] = ln
		}
		if !pickID.Valid {
			continue
		}
		if _, ok := ln.seen[pickID.Int64]; ok {
			continue
		}
		ln.seen[pickID.Int64] = struct{}{}
		ln.picks = append(ln.picks, pickBit{id: pickID.Int64, batch: batch, cost: cost.Int64, currency: pickCur.String, unit: unit, hit: hit == 1})
	}
	if err = rows.Err(); err != nil {
		return err
	}
	ids := make([]int64, 0, len(lines))
	for id := range lines {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		ln := lines[id]
		if !inSpan(orderAt[ln.orderID], b.start, b.end) {
			continue
		}
		known, costs, reason, err := classifyPicks(ln.picks)
		if err != nil {
			return err
		}
		if !known {
			b.markUnknown(ln.meta, id, ln.orderID, ln.orderNo, reason)
			for cur, amount := range costs {
				if err = b.addCost(ln.meta, cur, amount, true); err != nil {
					return err
				}
			}
			continue
		}
		for cur, amount := range costs {
			if err = b.addCost(ln.meta, cur, amount, false); err != nil {
				return err
			}
		}
	}
	return nil
}

type retBit struct {
	id, qty int64
	action  string
	at      string
}

func (a *Module) loadRestock(b *builder, where string, args []any) error {
	likes := monthLikes(b.start)
	qargs := append([]any{likes[0], likes[1], likes[2]}, args...)
	rows, err := a.DB.Query(`SELECT rl.line_id,r.created_at
FROM order_return_lines rl
JOIN order_returns r ON r.id=rl.return_id
JOIN order_lines l ON l.id=rl.line_id
JOIN orders o ON o.id=l.order_id
WHERE rl.action='restock' AND `+likeSQL("r.created_at")+` AND `+where, qargs...)
	if err != nil {
		return err
	}
	want := map[int64]struct{}{}
	err = func() error {
		defer rows.Close()
		for rows.Next() {
			var lineID int64
			var at string
			if err = rows.Scan(&lineID, &at); err != nil {
				return err
			}
			if inSpan(at, b.start, b.end) {
				want[lineID] = struct{}{}
			}
		}
		return rows.Err()
	}()
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, chunk := range chunks(ids, 200) {
		if err = a.restockChunk(b, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (a *Module) restockChunk(b *builder, ids []int64) error {
	in := placeholders(len(ids))
	args := idList(ids)
	meta := map[int64]*costLine{}
	rows, err := a.DB.Query(`SELECT l.id,l.qty,l.item_id,l.code,l.title,o.id,o.no,o.shop_id,coalesce(s.name,''),o.currency,i.product_id,coalesce(p.name,'')
FROM order_lines l JOIN orders o ON o.id=l.order_id JOIN shops s ON s.id=o.shop_id
LEFT JOIN inv_items i ON i.id=l.item_id LEFT JOIN products p ON p.id=i.product_id
WHERE l.id IN (`+in+`)`, args...)
	if err != nil {
		return err
	}
	err = func() error {
		defer rows.Close()
		for rows.Next() {
			var lineID, qty, itemID, orderID, shopID int64
			var code, title, no, shopName, cur, product string
			var productID sql.NullInt64
			if err = rows.Scan(&lineID, &qty, &itemID, &code, &title, &orderID, &no, &shopID, &shopName, &cur, &productID, &product); err != nil {
				return err
			}
			meta[lineID] = &costLine{meta: itemMeta{shopID: shopID, shopName: shopName, itemID: itemID, code: code, title: title, product: product, currency: cur, productID: shopPtr(productID)}, orderID: orderID, orderNo: no, qty: qty, seen: map[int64]struct{}{}}
		}
		return rows.Err()
	}()
	if err != nil {
		return err
	}
	rows, err = a.DB.Query(`SELECT pk.line_id,pk.id,pk.batch_id,pk.cost,pk.currency,d.unit_cost,CASE WHEN d.item_id IS NULL THEN 0 ELSE 1 END
FROM order_picks pk JOIN order_lines l ON l.id=pk.line_id
LEFT JOIN inv_dropship d ON pk.batch_id IS NULL AND d.warehouse_id=pk.warehouse_id AND d.item_id=l.item_id
WHERE pk.line_id IN (`+in+`) ORDER BY pk.line_id,pk.id`, args...)
	if err != nil {
		return err
	}
	err = func() error {
		defer rows.Close()
		for rows.Next() {
			var lineID, pickID int64
			var batch, unit, cost sql.NullInt64
			var cur sql.NullString
			var hit int
			if err = rows.Scan(&lineID, &pickID, &batch, &cost, &cur, &unit, &hit); err != nil {
				return err
			}
			ln := meta[lineID]
			if ln == nil {
				continue
			}
			if _, ok := ln.seen[pickID]; ok {
				continue
			}
			ln.seen[pickID] = struct{}{}
			ln.picks = append(ln.picks, pickBit{id: pickID, batch: batch, cost: cost.Int64, currency: cur.String, unit: unit, hit: hit == 1})
		}
		return rows.Err()
	}()
	if err != nil {
		return err
	}
	rets := map[int64][]retBit{}
	rows, err = a.DB.Query(`SELECT rl.line_id,r.id,rl.qty,rl.action,r.created_at
FROM order_return_lines rl JOIN order_returns r ON r.id=rl.return_id
WHERE rl.line_id IN (`+in+`) ORDER BY rl.line_id,r.id`, args...)
	if err != nil {
		return err
	}
	err = func() error {
		defer rows.Close()
		for rows.Next() {
			var lineID, rid, qty int64
			var action, at string
			if err = rows.Scan(&lineID, &rid, &qty, &action, &at); err != nil {
				return err
			}
			rets[lineID] = append(rets[lineID], retBit{id: rid, qty: qty, action: action, at: at})
		}
		return rows.Err()
	}()
	if err != nil {
		return err
	}
	for _, id := range ids {
		ln := meta[id]
		if ln == nil {
			continue
		}
		known, costs, reason, err := classifyPicks(ln.picks)
		if err != nil {
			return err
		}
		inMonth := false
		for _, ret := range rets[id] {
			if ret.action == "restock" && inSpan(ret.at, b.start, b.end) {
				inMonth = true
				break
			}
		}
		if !inMonth {
			continue
		}
		if !known || ln.qty <= 0 {
			if reason == "" {
				reason = "代发没有填写成本"
			}
			b.markUnknown(ln.meta, id, ln.orderID, ln.orderNo, reason)
			continue
		}
		var cur string
		var total int64
		for c, amount := range costs {
			cur, total = c, amount
		}
		var cumulative, prev int64
		for _, ret := range rets[id] {
			cumulative, err = addNonNeg(cumulative, ret.qty)
			if err != nil {
				return err
			}
			target, err := ratio(total, cumulative, ln.qty)
			if err != nil {
				return err
			}
			part, err := addSigned(target, -prev)
			if err != nil {
				return err
			}
			prev = target
			if ret.action == "restock" && inSpan(ret.at, b.start, b.end) {
				negPart, err := neg(part)
				if err != nil {
					return err
				}
				if err = b.addCost(ln.meta, cur, negPart, false); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (a *Module) loadRefunds(b *builder, where string, args []any) error {
	likes := monthLikes(b.start)
	qargs := append([]any{likes[0], likes[1], likes[2]}, args...)
	rows, err := a.DB.Query(`SELECT r.id,r.refund_amount,r.created_at,o.id,o.no,o.shop_id,coalesce(s.name,''),o.currency,
rl.line_id,rl.qty,l.item_id,l.code,l.title,l.qty,l.amount,i.product_id,coalesce(p.name,'')
FROM order_returns r JOIN orders o ON o.id=r.order_id JOIN shops s ON s.id=o.shop_id
LEFT JOIN order_return_lines rl ON rl.return_id=r.id
LEFT JOIN order_lines l ON l.id=rl.line_id
LEFT JOIN inv_items i ON i.id=l.item_id
LEFT JOIN products p ON p.id=i.product_id
WHERE `+likeSQL("r.created_at")+` AND `+where+` ORDER BY r.id`, qargs...)
	if err != nil {
		return err
	}
	defer rows.Close()
	type retLine struct {
		line lineBit
		qty  int64
		ok   bool
	}
	type retOrder struct {
		amount                int64
		at, no, shopName, cur string
		shopID, orderID       int64
		lines                 []retLine
	}
	got := map[int64]*retOrder{}
	var orderIDs []int64
	for rows.Next() {
		var rid, amount, orderID, shopID int64
		var at, no, shopName, cur string
		var lineID, retQty, itemID, lineQty, lineAmount, productID sql.NullInt64
		var code, title, product sql.NullString
		if err = rows.Scan(&rid, &amount, &at, &orderID, &no, &shopID, &shopName, &cur, &lineID, &retQty, &itemID, &code, &title, &lineQty, &lineAmount, &productID, &product); err != nil {
			return err
		}
		item := got[rid]
		if item == nil {
			item = &retOrder{amount: amount, at: at, no: no, shopName: shopName, cur: cur, shopID: shopID, orderID: orderID}
			got[rid] = item
			orderIDs = append(orderIDs, rid)
		}
		if !lineID.Valid {
			continue
		}
		ok := itemID.Valid && lineQty.Valid && lineQty.Int64 > 0 && retQty.Valid && retQty.Int64 > 0
		item.lines = append(item.lines, retLine{ok: ok, qty: retQty.Int64, line: lineBit{id: lineID.Int64, itemID: itemID.Int64, qty: lineQty.Int64, amount: lineAmount.Int64, code: code.String, title: title.String, product: product.String, productID: shopPtr(productID)}})
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, rid := range orderIDs {
		item := got[rid]
		if !inSpan(item.at, b.start, b.end) {
			continue
		}
		if len(item.lines) == 0 {
			if err = b.addRefund(item.shopID, item.shopName, item.cur, nil, item.amount, true); err != nil {
				return err
			}
			continue
		}
		weights := make([]int64, len(item.lines))
		usable := true
		for i, ln := range item.lines {
			if !ln.ok {
				usable = false
				break
			}
			weights[i], err = ratio(ln.line.amount, ln.qty, ln.line.qty)
			if err != nil {
				return err
			}
		}
		if !usable {
			if err = b.addRefund(item.shopID, item.shopName, item.cur, nil, item.amount, true); err != nil {
				return err
			}
			continue
		}
		allZero := true
		for _, w := range weights {
			if w > 0 {
				allZero = false
				break
			}
		}
		if allZero {
			for i, ln := range item.lines {
				weights[i] = ln.qty
			}
		}
		parts, err := splitAmounts(item.amount, weights)
		if err != nil {
			return err
		}
		if err = b.addRefund(item.shopID, item.shopName, item.cur, nil, item.amount, false); err != nil {
			return err
		}
		for i, ln := range item.lines {
			meta := itemMeta{shopID: item.shopID, shopName: item.shopName, itemID: ln.line.itemID, code: ln.line.code, title: ln.line.title, product: ln.line.product, currency: item.cur, productID: ln.line.productID}
			if err = b.addRefund(item.shopID, item.shopName, item.cur, &meta, parts[i], false); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Module) loadFees(b *builder, u core.User, shopID int64) error {
	where, args, err := a.shopScopeSQL(u, shopID, "rr.shop_id")
	if err != nil {
		return err
	}
	startDate := b.start.Format("2006-01-02")
	endDate := b.end.Format("2006-01-02")
	rows, err := a.DB.Query(`SELECT rr.id,rr.fee,rr.shipping_insurance,rr.currency,o.id,o.no,o.shop_id,coalesce(s.name,''),o.currency,
l.id,l.item_id,l.code,l.title,l.qty,l.amount,i.product_id,coalesce(p.name,'')
FROM fin_reconcile_rows rr
JOIN orders o ON o.id=rr.order_id AND o.shop_id=rr.shop_id
JOIN shops s ON s.id=o.shop_id
LEFT JOIN order_lines l ON l.order_id=o.id
LEFT JOIN inv_items i ON i.id=l.item_id
LEFT JOIN products p ON p.id=i.product_id
WHERE rr.match_status='matched' AND rr.settled_at>=? AND rr.settled_at<? AND `+where+` ORDER BY rr.id`, append([]any{startDate, endDate}, args...)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	type feeLine struct {
		line lineBit
	}
	type feeRow struct {
		fee, insurance    int64
		cur, orderCur, no string
		shopName          string
		shopID, orderID   int64
		lines             []lineBit
		seen              map[int64]struct{}
	}
	got := map[int64]*feeRow{}
	var ids []int64
	for rows.Next() {
		var rid, fee, insurance, orderID, shopID int64
		var cur, no, shopName, orderCur string
		var lineID, itemID, qty, amount, productID sql.NullInt64
		var code, title, product sql.NullString
		if err = rows.Scan(&rid, &fee, &insurance, &cur, &orderID, &no, &shopID, &shopName, &orderCur, &lineID, &itemID, &code, &title, &qty, &amount, &productID, &product); err != nil {
			return err
		}
		item := got[rid]
		if item == nil {
			item = &feeRow{fee: fee, insurance: insurance, cur: cur, orderCur: orderCur, no: no, shopName: shopName, shopID: shopID, orderID: orderID, seen: map[int64]struct{}{}}
			got[rid] = item
			ids = append(ids, rid)
		}
		if !lineID.Valid || lineID.Int64 == 0 {
			continue
		}
		if _, ok := item.seen[lineID.Int64]; ok {
			continue
		}
		item.seen[lineID.Int64] = struct{}{}
		item.lines = append(item.lines, lineBit{id: lineID.Int64, itemID: itemID.Int64, qty: qty.Int64, amount: amount.Int64, code: code.String, title: title.String, product: product.String, productID: shopPtr(productID)})
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		item := got[id]
		if item.cur != item.orderCur {
			b.feeSkip++
			continue
		}
		if item.fee == 0 && item.insurance == 0 {
			continue
		}
		if err = b.addFee(item.shopID, item.shopName, item.cur, nil, item.fee, item.insurance); err != nil {
			return err
		}
		if len(item.lines) == 0 {
			continue
		}
		weights := make([]int64, len(item.lines))
		byQty := true
		for _, ln := range item.lines {
			if ln.amount > 0 {
				byQty = false
				break
			}
		}
		for i, ln := range item.lines {
			if byQty {
				weights[i] = ln.qty
			} else {
				weights[i] = ln.amount
			}
		}
		fees, err := splitAmounts(item.fee, weights)
		if err != nil {
			return err
		}
		ins, err := splitAmounts(item.insurance, weights)
		if err != nil {
			return err
		}
		for i, ln := range item.lines {
			meta := itemMeta{shopID: item.shopID, shopName: item.shopName, itemID: ln.itemID, code: ln.code, title: ln.title, product: ln.product, currency: item.cur, productID: ln.productID}
			if err = b.addFee(item.shopID, item.shopName, item.cur, &meta, fees[i], ins[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Module) loadReportExpenses(b *builder, u core.User, shopID int64) error {
	where, args, err := a.expenseScope(u, shopID, shopID > 0)
	if err != nil {
		return err
	}
	rows, err := a.DB.Query(`SELECT e.shop_id,coalesce(s.name,''),e.amount,e.currency,e.occurred_at
FROM fin_expenses e LEFT JOIN shops s ON s.id=e.shop_id
WHERE `+where+` AND e.occurred_at>=? AND e.occurred_at<?`, append(append([]any{}, args...), stamp(b.start), stamp(b.end))...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var shop sql.NullInt64
		var name, cur, at string
		var amount int64
		if err = rows.Scan(&shop, &name, &amount, &cur, &at); err != nil {
			return err
		}
		if !inSpan(at, b.start, b.end) {
			continue
		}
		if !shop.Valid || shop.Int64 == 0 {
			if shopID == 0 && companyAccess(u) {
				if err = b.addExpense(0, "", cur, amount, true); err != nil {
					return err
				}
			}
			continue
		}
		if err = b.addExpense(shop.Int64, name, cur, amount, false); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (b *builder) finish(month string, shopID int64, company bool) reportOut {
	out := reportOut{
		Month: month, Timezone: "Asia/Shanghai", Basis: reportBasis, CostVisible: b.showCost,
		IncludesCompanyExpense: company, ItemExpenseIncluded: false,
		MissingRates: []missingRate{}, CompanyExpenses: []companyExpense{}, Shops: []shopReport{}, Items: []itemReport{}, Notes: b.notes,
		NotesTruncated: b.cut,
	}
	if shopID > 0 {
		out.ShopID = i64ptr(shopID)
	}
	if out.Notes == nil {
		out.Notes = []reportNote{}
	}
	out.Totals = b.present(b.grand, "", false)
	shopIDs := make([]int64, 0, len(b.shops))
	for id, g := range b.shops {
		if g.touched() {
			shopIDs = append(shopIDs, id)
		}
	}
	sort.Slice(shopIDs, func(i, j int) bool { return shopIDs[i] < shopIDs[j] })
	for _, id := range shopIDs {
		g := b.shops[id]
		out.Shops = append(out.Shops, shopReport{ShopID: id, ShopName: g.shopName, rollup: b.present(g, "", false)})
	}
	type itemSort struct {
		key itemKey
		rev int64
	}
	order := make([]itemSort, 0, len(b.items))
	for key, hold := range b.items {
		if !hold.group.touched() {
			continue
		}
		rev := int64(0)
		if bag := hold.group.bags[key.currency]; bag != nil {
			rev = bag.revenue
		}
		order = append(order, itemSort{key, rev})
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].rev != order[j].rev {
			return order[i].rev > order[j].rev
		}
		if order[i].key.shop != order[j].key.shop {
			return order[i].key.shop < order[j].key.shop
		}
		if order[i].key.item != order[j].key.item {
			return order[i].key.item < order[j].key.item
		}
		return order[i].key.currency < order[j].key.currency
	})
	for _, row := range order {
		hold := b.items[row.key]
		view := b.present(&hold.group, hold.meta.currency, true)
		if len(view.Currencies) == 0 {
			continue
		}
		out.Items = append(out.Items, itemReport{
			ShopID: hold.meta.shopID, ShopName: hold.meta.shopName, ItemID: hold.meta.itemID,
			ProductID: hold.meta.productID, Code: hold.meta.code, Title: hold.meta.title, ProductName: hold.meta.product,
			moneyView: view.Currencies[0],
		})
	}
	curs := make([]string, 0, len(b.company))
	for cur := range b.company {
		curs = append(curs, cur)
	}
	sort.Slice(curs, func(i, j int) bool {
		if curs[i] == "CNY" {
			return true
		}
		if curs[j] == "CNY" {
			return false
		}
		return curs[i] < curs[j]
	})
	for _, cur := range curs {
		amount := b.company[cur]
		row := companyExpense{Currency: cur, Amount: amount, IncludedInTotals: true}
		if rate, ok := b.rates[cur]; ok && rate > 0 {
			if v, err := toCNY(amount, rate); err == nil {
				row.Converted = true
				row.CNY = i64ptr(v)
			}
		}
		out.CompanyExpenses = append(out.CompanyExpenses, row)
	}
	for cur, reason := range b.missing {
		out.MissingRates = append(out.MissingRates, missingRate{Currency: cur, Reason: reason})
	}
	sort.Slice(out.MissingRates, func(i, j int) bool { return out.MissingRates[i].Currency < out.MissingRates[j].Currency })
	return out
}

func (b *builder) present(g *group, primary string, single bool) rollup {
	curs := map[string]struct{}{}
	for cur := range g.bags {
		curs[cur] = struct{}{}
	}
	costKnown := b.showCost && len(g.unknown) == 0
	if costKnown {
		for cur := range g.cost {
			curs[cur] = struct{}{}
		}
	}
	if primary != "" {
		curs[primary] = struct{}{}
	}
	list := sortCurrencies(curs)
	if len(list) == 0 {
		list = []string{"CNY"}
	}
	costCur, costAmt, costSingle := "", int64(0), len(g.cost) == 1
	if costSingle {
		for cur, amount := range g.cost {
			costCur, costAmt = cur, amount
		}
	}
	revCurs := 0
	for cur, bag := range g.bags {
		if bag.revenue != 0 || bag.unshipped != 0 || bag.refund != 0 || bag.fee != 0 || bag.insurance != 0 || bag.expense != 0 {
			revCurs++
			_ = cur
		}
	}
	knownParts := []moneyPart{}
	if b.showCost && !costKnown {
		merged := map[string]int64{}
		for cur, amount := range g.cost {
			merged[cur] = amount
		}
		for cur, amount := range g.partial {
			merged[cur], _ = addSigned(merged[cur], amount)
		}
		knownParts = partsOf(merged)
	}
	views := make([]moneyView, 0, len(list))
	ready := true
	var sumRev, sumUn, sumRef, sumUnalloc, sumFee, sumIns, sumExp int64
	var sumCost int64
	var costConverted bool
	for _, cur := range list {
		stored := g.bags[cur]
		if stored == nil {
			stored = &bag{}
		}
		bag := stored
		view := moneyView{
			Currency: cur, Revenue: bag.revenue, UnshippedRevenue: bag.unshipped, Refund: bag.refund,
			UnallocatedRefund: bag.unalloc, PlatformFee: bag.fee, ShippingInsurance: bag.insurance,
			CostParts: []moneyPart{}, KnownCostParts: []moneyPart{},
		}
		if !single {
			view.Expense = bag.expense
		}
		view.UnknownCostCount = len(g.unknown)
		if !b.showCost {
			view.CostHidden = true
		} else if !costKnown {
			view.KnownCostParts = knownParts
		} else {
			view.CostKnown = true
			if len(g.cost) == 0 {
				view.Cost = i64ptr(0)
				view.CostCurrency = strptr(cur)
			} else if costSingle {
				view.CostCurrency = strptr(costCur)
				view.CostParts = []moneyPart{{Currency: costCur, Amount: costAmt}}
				if cur == costCur {
					view.Cost = i64ptr(costAmt)
				}
			}
		}
		same := b.showCost && costKnown && (len(g.cost) == 0 || (costSingle && cur == costCur && (revCurs <= 1 || len(g.cost) == 0)))
		if len(g.cost) == 0 && b.showCost && costKnown {
			same = true
		}
		if b.showCost && costKnown && costSingle && cur == costCur && revCurs > 1 && primary == "" {
			same = false
		}
		if single && b.showCost && costKnown && (len(g.cost) == 0 || (costSingle && costCur == primary && cur == primary)) {
			same = true
		}
		if same && view.Cost != nil {
			if p, err := profitOf(view, *view.Cost, !single); err == nil {
				view.Profit = i64ptr(p)
			} else {
				ready = false
				b.miss(cur, "换算结果超出范围")
			}
		}
		rate, ok := b.rates[cur]
		needCost := b.showCost && costKnown && costSingle && costCur != "" && costCur != cur
		costRate, costOK := int64(0), true
		if needCost {
			costRate, costOK = b.rates[costCur]
		}
		if !ok || rate <= 0 || (needCost && (!costOK || costRate <= 0)) {
			ready = false
			view.Converted = false
			if !ok || rate <= 0 {
				b.miss(cur, "未设置本月汇率")
			}
			if needCost && (!costOK || costRate <= 0) {
				b.miss(costCur, "未设置本月汇率")
			}
			views = append(views, view)
			continue
		}
		view.RateMicros = i64ptr(rate)
		if costCur != "" && b.showCost && costKnown {
			if cr, ok := b.rates[costCur]; ok && cr > 0 {
				view.CostRateMicros = i64ptr(cr)
			}
		}
		cny, overflow := convertBag(bag, rate, !single)
		if overflow {
			ready = false
			b.miss(cur, "换算结果超出范围")
			views = append(views, view)
			continue
		}
		view.Converted = true
		view.CNYRevenue = i64ptr(cny.revenue)
		view.CNYUnshippedRevenue = i64ptr(cny.unshipped)
		view.CNYRefund = i64ptr(cny.refund)
		view.CNYUnallocatedRefund = i64ptr(cny.unalloc)
		view.CNYPlatformFee = i64ptr(cny.fee)
		view.CNYShippingInsurance = i64ptr(cny.insurance)
		if !single {
			view.CNYExpense = i64ptr(cny.expense)
		} else {
			view.CNYExpense = i64ptr(0)
		}
		var err error
		sumRev, err = addSigned(sumRev, cny.revenue)
		if err == nil {
			sumUn, err = addSigned(sumUn, cny.unshipped)
		}
		if err == nil {
			sumRef, err = addSigned(sumRef, cny.refund)
		}
		if err == nil {
			sumUnalloc, err = addSigned(sumUnalloc, cny.unalloc)
		}
		if err == nil {
			sumFee, err = addSigned(sumFee, cny.fee)
		}
		if err == nil {
			sumIns, err = addSigned(sumIns, cny.insurance)
		}
		if err == nil && !single {
			sumExp, err = addSigned(sumExp, cny.expense)
		}
		if err != nil {
			ready = false
			b.miss(cur, "换算结果超出范围")
			view.Converted = false
			view.CNYRevenue, view.CNYUnshippedRevenue, view.CNYRefund = nil, nil, nil
			view.CNYUnallocatedRefund, view.CNYPlatformFee, view.CNYShippingInsurance, view.CNYExpense = nil, nil, nil, nil
			views = append(views, view)
			continue
		}
		if view.Profit != nil {
			if p, err := toCNY(*view.Profit, rate); err == nil {
				view.CNYProfit = i64ptr(p)
			} else {
				ready = false
				view.Converted = false
				b.miss(cur, "换算结果超出范围")
				blankCNY(&view)
			}
		}
		if view.Cost != nil && cur == costCur {
			if v, err := toCNY(*view.Cost, rate); err == nil {
				view.CNYCost = i64ptr(v)
				if cur == costCur {
					sumCost = v
					costConverted = true
				}
			} else {
				ready = false
				view.Converted = false
				b.miss(cur, "换算结果超出范围")
				blankCNY(&view)
			}
		}
		views = append(views, view)
	}
	if single {
		views = combineItem(b, g, views, primary, costKnown, costSingle, costCur, costAmt)
		ready = viewsReady(views)
	}
	roll := rollup{Currencies: views, CNYReady: ready && len(views) > 0}
	if !roll.CNYReady {
		return roll
	}
	roll.CNYRevenue = i64ptr(sumRev)
	roll.CNYUnshippedRevenue = i64ptr(sumUn)
	roll.CNYRefund = i64ptr(sumRef)
	roll.CNYUnallocatedRefund = i64ptr(sumUnalloc)
	roll.CNYPlatformFee = i64ptr(sumFee)
	roll.CNYShippingInsurance = i64ptr(sumIns)
	if single {
		roll.CNYExpense = i64ptr(0)
	} else {
		roll.CNYExpense = i64ptr(sumExp)
	}
	if !b.showCost || !costKnown {
		return roll
	}
	if len(g.cost) == 0 {
		roll.CNYCost = i64ptr(0)
	} else if costSingle && costConverted {
		roll.CNYCost = i64ptr(sumCost)
	} else if costSingle && costCur != "" {
		if rate, ok := b.rates[costCur]; ok && rate > 0 {
			if v, err := toCNY(costAmt, rate); err == nil {
				roll.CNYCost = i64ptr(v)
				sumCost = v
			} else {
				roll.CNYReady = false
				b.miss(costCur, "换算结果超出范围")
				blankRoll(&roll)
				return roll
			}
		} else {
			roll.CNYReady = false
			b.miss(costCur, "未设置本月汇率")
			blankRoll(&roll)
			return roll
		}
	} else {
		roll.CNYReady = false
		blankRoll(&roll)
		return roll
	}
	p, err := profitCNY(sumRev, sumUn, sumRef, sumFee, sumIns, sumExp, sumCost, !single)
	if err != nil {
		roll.CNYReady = false
		b.miss("CNY", "换算结果超出范围")
		blankRoll(&roll)
		return roll
	}
	roll.CNYProfit = i64ptr(p)
	if single && len(roll.Currencies) > 0 {
		roll.Currencies[0].CNYCost = roll.CNYCost
		roll.Currencies[0].CNYProfit = roll.CNYProfit
		roll.Currencies[0].Converted = true
	}
	return roll
}

func combineItem(b *builder, g *group, views []moneyView, primary string, costKnown, costSingle bool, costCur string, costAmt int64) []moneyView {
	var view moneyView
	found := false
	for _, item := range views {
		if item.Currency == primary {
			view = item
			found = true
			break
		}
	}
	if !found {
		view = moneyView{Currency: primary, CostParts: []moneyPart{}, KnownCostParts: []moneyPart{}}
	}
	if !b.showCost {
		view.CostHidden = true
		view.Cost, view.Profit, view.CNYCost, view.CNYProfit = nil, nil, nil, nil
		return []moneyView{view}
	}
	if !costKnown {
		view.CostKnown = false
		view.Cost, view.Profit, view.CNYCost, view.CNYProfit = nil, nil, nil, nil
		return []moneyView{view}
	}
	view.CostKnown = true
	needed := []string{primary}
	if costSingle && costCur != "" && costCur != primary {
		needed = append(needed, costCur)
		view.Cost = nil
		view.CostCurrency = strptr(costCur)
		view.CostParts = []moneyPart{{Currency: costCur, Amount: costAmt}}
		view.Profit = nil
	}
	for _, cur := range needed {
		rate, ok := b.rates[cur]
		if !ok || rate <= 0 {
			b.miss(cur, "未设置本月汇率")
			blankCNY(&view)
			view.Converted = false
			return []moneyView{view}
		}
	}
	return []moneyView{view}
}

func viewsReady(views []moneyView) bool {
	if len(views) == 0 {
		return false
	}
	for _, view := range views {
		if !view.Converted {
			return false
		}
	}
	return true
}

func convertBag(src *bag, rate int64, expense bool) (bag, bool) {
	var out bag
	var err error
	if out.revenue, err = toCNY(src.revenue, rate); err != nil {
		return bag{}, true
	}
	if out.unshipped, err = toCNY(src.unshipped, rate); err != nil {
		return bag{}, true
	}
	if out.refund, err = toCNY(src.refund, rate); err != nil {
		return bag{}, true
	}
	if out.unalloc, err = toCNY(src.unalloc, rate); err != nil {
		return bag{}, true
	}
	if out.fee, err = toCNY(src.fee, rate); err != nil {
		return bag{}, true
	}
	if out.insurance, err = toCNY(src.insurance, rate); err != nil {
		return bag{}, true
	}
	if expense {
		if out.expense, err = toCNY(src.expense, rate); err != nil {
			return bag{}, true
		}
	}
	return out, false
}

func profitOf(view moneyView, cost int64, expense bool) (int64, error) {
	exp := int64(0)
	if expense {
		exp = view.Expense
	}
	return profitCNY(view.Revenue, view.UnshippedRevenue, view.Refund, view.PlatformFee, view.ShippingInsurance, exp, cost, expense || true)
}

func profitCNY(rev, unship, refund, fee, ins, exp, cost int64, _ bool) (int64, error) {
	n, err := addSigned(rev, 0)
	if err != nil {
		return 0, err
	}
	if n, err = addSigned(n, -refund); err != nil {
		return 0, err
	}
	if n, err = addSigned(n, -fee); err != nil {
		return 0, err
	}
	if n, err = addSigned(n, -ins); err != nil {
		return 0, err
	}
	if n, err = addSigned(n, -exp); err != nil {
		return 0, err
	}
	return addSigned(n, -cost)
}

func blankCNY(view *moneyView) {
	view.CNYRevenue, view.CNYUnshippedRevenue, view.CNYRefund = nil, nil, nil
	view.CNYUnallocatedRefund, view.CNYPlatformFee, view.CNYShippingInsurance = nil, nil, nil
	view.CNYExpense, view.CNYCost, view.CNYProfit = nil, nil, nil
	view.RateMicros, view.CostRateMicros = nil, nil
}

func blankRoll(roll *rollup) {
	roll.CNYReady = false
	roll.CNYRevenue, roll.CNYUnshippedRevenue, roll.CNYRefund = nil, nil, nil
	roll.CNYUnallocatedRefund, roll.CNYPlatformFee, roll.CNYShippingInsurance = nil, nil, nil
	roll.CNYExpense, roll.CNYCost, roll.CNYProfit = nil, nil, nil
}

func (b *builder) miss(cur, reason string) {
	if cur == "" {
		return
	}
	if _, ok := b.missing[cur]; ok && reason != "换算结果超出范围" {
		return
	}
	b.missing[cur] = reason
}

func partsOf(m map[string]int64) []moneyPart {
	if len(m) == 0 {
		return []moneyPart{}
	}
	out := make([]moneyPart, 0, len(m))
	for _, cur := range sortCurrencies(m) {
		out = append(out, moneyPart{Currency: cur, Amount: m[cur]})
	}
	return out
}

func sortCurrencies[M ~map[string]V, V any](m M) []string {
	out := make([]string, 0, len(m))
	for cur := range m {
		out = append(out, cur)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i] == out[j] {
			return false
		}
		if out[i] == "CNY" || out[j] == "CNY" {
			return out[i] == "CNY"
		}
		return out[i] < out[j]
	})
	return out
}

func mapKeys[M ~map[K]V, K int64, V any](m M) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func splitAmounts(total int64, weights []int64) ([]int64, error) {
	if total < 0 {
		return nil, errOverflow
	}
	n := len(weights)
	out := make([]int64, n)
	if n == 0 {
		if total == 0 {
			return out, nil
		}
		return nil, errOverflow
	}
	var sumW int64
	for _, w := range weights {
		if w < 0 {
			return nil, errOverflow
		}
		var err error
		sumW, err = addNonNeg(sumW, w)
		if err != nil {
			return nil, err
		}
	}
	if sumW == 0 {
		if total == 0 {
			return out, nil
		}
		return nil, errOverflow
	}
	type frac struct {
		i, rem int64
	}
	fracs := make([]frac, n)
	var used int64
	for i, w := range weights {
		prod := new(big.Int).Mul(big.NewInt(total), big.NewInt(w))
		q, m := new(big.Int).QuoRem(prod, big.NewInt(sumW), new(big.Int))
		if !q.IsInt64() || !m.IsInt64() {
			return nil, errOverflow
		}
		out[i] = q.Int64()
		var err error
		used, err = addNonNeg(used, out[i])
		if err != nil {
			return nil, errOverflow
		}
		fracs[i] = frac{int64(i), m.Int64()}
	}
	left, err := addSigned(total, -used)
	if err != nil || left < 0 || left > int64(n) {
		return nil, errOverflow
	}
	sort.SliceStable(fracs, func(a, c int) bool {
		if fracs[a].rem != fracs[c].rem {
			return fracs[a].rem > fracs[c].rem
		}
		return fracs[a].i < fracs[c].i
	})
	for i := int64(0); i < left; i++ {
		out[fracs[i].i]++
	}
	return out, nil
}

func i64ptr(v int64) *int64   { return &v }
func strptr(v string) *string { return &v }
