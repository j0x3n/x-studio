package assembly

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"xstudio/internal/core"
	"xstudio/internal/modules/inventory"
)

type CreateOrderRequest struct {
	RecipeID    int64  `json:"recipe_id"`
	Runs        int64  `json:"runs"`
	WarehouseID int64  `json:"warehouse_id"`
	Note        string `json:"note"`
}

type OrderRow struct {
	ID           int64
	No           string
	RecipeID     int64
	Runs         int64
	WarehouseID  int64
	Status       string
	OutputItemID int64
	OutputQty    int64
	Currency     string
	LaborCost    int64
	MaterialCost int64
	TotalCost    int64
	Note         string
}

type OrderLineRow struct {
	ID          int64
	ItemID      int64
	UnitQty     int64
	LossBps     int64
	RequiredQty int64
	ActualCost  int64
	Currency    string
	Code        string
	Name        string
	Unit        string
}

func generateOrderNo(tx *sql.Tx) (string, error) {
	prefix := "ASM" + time.Now().In(time.FixedZone("CST", 8*3600)).Format("20060102")
	var last string
	if err := tx.QueryRow("SELECT coalesce(max(no),'') FROM assembly_orders WHERE no LIKE ?", prefix+"%").Scan(&last); err != nil {
		return "", err
	}
	seq := 0
	if last != "" {
		if _, err := fmt.Sscanf(strings.TrimPrefix(last, prefix), "%d", &seq); err != nil {
			return "", err
		}
	}
	if seq >= 9999 {
		return "", bad(409, "今日组装单号已用完")
	}
	return fmt.Sprintf("%s%04d", prefix, seq+1), nil
}

func (a *Module) listOrders(w http.ResponseWriter, r *http.Request) (any, error) {
	u := core.CurrentUser(r)
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	warehouseID, _ := strconv.ParseInt(r.URL.Query().Get("warehouse_id"), 10, 64)
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	where := []string{"1=1"}
	args := []any{}

	if status != "" {
		where = append(where, "o.status=?")
		args = append(args, status)
	}
	if warehouseID > 0 {
		where = append(where, "o.warehouse_id=?")
		args = append(args, warehouseID)
	}
	if q != "" {
		where = append(where, "(o.no LIKE ? OR r.name LIKE ? OR i.code LIKE ? OR i.name LIKE ?)")
		pat := "%" + q + "%"
		args = append(args, pat, pat, pat, pat)
	}

	whereClause := strings.Join(where, " AND ")

	var total int
	countQuery := `SELECT count(*) 
		FROM assembly_orders o
		LEFT JOIN assembly_recipes r ON r.id = o.recipe_id
		JOIN inv_warehouses w ON w.id = o.warehouse_id
		JOIN inv_items i ON i.id = o.output_item_id
		WHERE ` + whereClause
	if err := a.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	offset, size := core.Page(r)
	query := `SELECT 
		o.id, o.no, o.recipe_id, o.runs, o.warehouse_id, o.status,
		o.output_item_id, o.output_qty, o.currency,
		o.labor_cost, o.material_cost, o.total_cost,
		o.output_batch_id, o.note, o.created_by, o.created_at, o.done_at, o.canceled_at,
		r.name AS recipe_name,
		w.name AS warehouse_name,
		i.code AS output_item_code, i.name AS output_item_name, i.unit AS output_item_unit
	FROM assembly_orders o
	LEFT JOIN assembly_recipes r ON r.id = o.recipe_id
	JOIN inv_warehouses w ON w.id = o.warehouse_id
	JOIN inv_items i ON i.id = o.output_item_id
	WHERE ` + whereClause + ` ORDER BY o.id DESC LIMIT ? OFFSET ?`

	queryArgs := append(args, size, offset)
	rows, err := objects(a.DB, query, queryArgs...)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		redactOrder(u, row)
	}

	return core.Object{"items": rows, "total": total}, nil
}

func (a *Module) getOrder(w http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	if id == 0 {
		return nil, bad(400, "无效的组装单ID")
	}
	u := core.CurrentUser(r)
	ord, err := a.fetchOrderDetail(r.Context(), a.DB, id, u)
	if err != nil {
		return nil, err
	}
	return core.Object{"order": ord}, nil
}

func (a *Module) fetchOrderDetail(ctx context.Context, q inventory.Querier, id int64, u core.User) (core.Object, error) {
	query := `SELECT 
		o.id, o.no, o.recipe_id, o.runs, o.warehouse_id, o.status,
		o.output_item_id, o.output_qty, o.currency,
		o.labor_cost, o.material_cost, o.total_cost,
		o.output_batch_id, o.recipe_snapshot, o.note, o.created_by, o.created_at, o.done_at, o.canceled_at,
		r.name AS recipe_name,
		w.name AS warehouse_name,
		i.code AS output_item_code, i.name AS output_item_name, i.unit AS output_item_unit
	FROM assembly_orders o
	LEFT JOIN assembly_recipes r ON r.id = o.recipe_id
	JOIN inv_warehouses w ON w.id = o.warehouse_id
	JOIN inv_items i ON i.id = o.output_item_id
	WHERE o.id=?`

	ord, err := one(q, query, id)
	if err != nil {
		return nil, err
	}

	// Parse recipe_snapshot JSON
	if rawSnap, ok := ord["recipe_snapshot"].(string); ok && rawSnap != "" {
		var snap map[string]any
		if e := json.Unmarshal([]byte(rawSnap), &snap); e == nil {
			ord["recipe_snapshot"] = snap
		}
	}

	linesQuery := `SELECT 
		ol.id, ol.order_id, ol.item_id, ol.unit_qty, ol.loss_bps, ol.required_qty, ol.actual_cost, ol.currency,
		i.code AS item_code, i.name AS item_name, i.unit AS item_unit
	FROM assembly_order_lines ol
	JOIN inv_items i ON i.id = ol.item_id
	WHERE ol.order_id=? ORDER BY ol.id ASC`

	lines, err := objects(q, linesQuery, id)
	if err != nil {
		return nil, err
	}
	ord["lines"] = lines

	redactOrder(u, ord)
	return ord, nil
}

func (a *Module) createOrder(w http.ResponseWriter, r *http.Request) (any, error) {
	u := core.CurrentUser(r)
	var in CreateOrderRequest
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}

	if in.RecipeID <= 0 {
		return nil, bad(400, "配方参数不正确")
	}
	if in.Runs <= 0 {
		return nil, bad(400, "组装批数必须大于0")
	}
	if in.WarehouseID <= 0 {
		return nil, bad(400, "仓库参数不正确")
	}
	if utf8.RuneCountInString(in.Note) > 4000 {
		return nil, bad(400, "备注最多 4000 字")
	}

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 1. Fetch recipe
	var recName, recCur, recNote, recCreated, recUpdated string
	var recOutputItemID, recOutputQty, recLaborCost, recCreatedBy int64
	var recArticleID sql.NullInt64
	var recArchived int

	err = tx.QueryRow(`SELECT name, output_item_id, output_qty, currency, labor_cost, article_id, note, archived, created_by, created_at, updated_at
		FROM assembly_recipes WHERE id=?`, in.RecipeID).Scan(
		&recName, &recOutputItemID, &recOutputQty, &recCur, &recLaborCost, &recArticleID, &recNote, &recArchived, &recCreatedBy, &recCreated, &recUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, bad(404, "配方不存在")
	}
	if err != nil {
		return nil, err
	}
	if recArchived == 1 {
		return nil, bad(409, "配方已归档，无法用于创建组装单")
	}

	// 2. Fetch recipe lines
	type RecLine struct {
		ItemID  int64
		Qty     int64
		LossBps int64
		Code    string
		Name    string
		Unit    string
	}
	lineRows, err := tx.Query(`SELECT l.item_id, l.qty, l.loss_bps, i.code, i.name, i.unit
		FROM assembly_recipe_lines l
		JOIN inv_items i ON i.id = l.item_id
		WHERE l.recipe_id=? ORDER BY l.id ASC`, in.RecipeID)
	if err != nil {
		return nil, err
	}
	var recipeLines []RecLine
	for lineRows.Next() {
		var rl RecLine
		if err = lineRows.Scan(&rl.ItemID, &rl.Qty, &rl.LossBps, &rl.Code, &rl.Name, &rl.Unit); err != nil {
			lineRows.Close()
			return nil, err
		}
		recipeLines = append(recipeLines, rl)
	}
	lineRows.Close()
	if len(recipeLines) == 0 {
		return nil, bad(400, "配方缺少原料明细")
	}

	// 3. Verify warehouse
	var whType string
	var whArchived int
	err = tx.QueryRow("SELECT type, archived FROM inv_warehouses WHERE id=?", in.WarehouseID).Scan(&whType, &whArchived)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, bad(404, "仓库不存在")
	}
	if err != nil {
		return nil, err
	}
	if whArchived == 1 {
		return nil, bad(409, "仓库已归档")
	}
	if whType != "own" && whType != "stall" {
		return nil, bad(400, "组装加工仅支持自有仓或档口仓")
	}

	// 4. Verify output item
	var itemArchived int
	var outputCode, outputName, outputUnit string
	err = tx.QueryRow("SELECT archived, code, name, unit FROM inv_items WHERE id=?", recOutputItemID).Scan(&itemArchived, &outputCode, &outputName, &outputUnit)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, bad(404, "产物物料不存在")
	}
	if err != nil {
		return nil, err
	}
	if itemArchived == 1 {
		return nil, bad(409, "产物物料已归档")
	}

	// 5. Overflow checks and calculations
	totalOutputQty, err := calcTotalOutputQty(recOutputQty, in.Runs)
	if err != nil {
		return nil, err
	}
	totalLaborCost, err := calcTotalLabor(recLaborCost, in.Runs)
	if err != nil {
		return nil, err
	}

	type CalcLine struct {
		ItemID      int64
		UnitQty     int64
		LossBps     int64
		RequiredQty int64
		Code        string
		Name        string
		Unit        string
	}
	calcLines := make([]CalcLine, 0, len(recipeLines))
	for _, rl := range recipeLines {
		reqQty, err := calcRequiredQty(rl.Qty, in.Runs, rl.LossBps)
		if err != nil {
			return nil, err
		}
		calcLines = append(calcLines, CalcLine{
			ItemID:      rl.ItemID,
			UnitQty:     rl.Qty,
			LossBps:     rl.LossBps,
			RequiredQty: reqQty,
			Code:        rl.Code,
			Name:        rl.Name,
			Unit:        rl.Unit,
		})
	}

	// 6. Generate order number
	orderNo, err := generateOrderNo(tx)
	if err != nil {
		return nil, err
	}

	// 7. Build recipe snapshot
	snapshotLines := make([]map[string]any, 0, len(recipeLines))
	for _, cl := range calcLines {
		snapshotLines = append(snapshotLines, map[string]any{
			"item_id":  cl.ItemID,
			"code":     cl.Code,
			"name":     cl.Name,
			"unit":     cl.Unit,
			"qty":      cl.UnitQty,
			"loss_bps": cl.LossBps,
		})
	}

	var snapArticleID any
	if recArticleID.Valid {
		snapArticleID = recArticleID.Int64
	}

	snapshot := map[string]any{
		"recipe_id":        in.RecipeID,
		"name":             recName,
		"output_item_id":   recOutputItemID,
		"output_item_code": outputCode,
		"output_item_name": outputName,
		"output_item_unit": outputUnit,
		"output_qty":       recOutputQty,
		"currency":         recCur,
		"labor_cost":       recLaborCost,
		"article_id":       snapArticleID,
		"note":             recNote,
		"lines":            snapshotLines,
	}
	snapJSON, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}

	// 8. Insert assembly order
	now := core.Now()
	res, err := tx.Exec(`INSERT INTO assembly_orders(
		no, recipe_id, runs, warehouse_id, status, output_item_id, output_qty, currency, labor_cost, material_cost, total_cost, recipe_snapshot, note, created_by, created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		orderNo, in.RecipeID, in.Runs, in.WarehouseID, "draft", recOutputItemID, totalOutputQty, recCur, totalLaborCost, 0, 0, string(snapJSON), in.Note, u.ID, now)
	if err != nil {
		return nil, err
	}

	orderID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	// 9. Insert order lines
	for _, cl := range calcLines {
		_, err = tx.Exec(`INSERT INTO assembly_order_lines(
			order_id, item_id, unit_qty, loss_bps, required_qty, actual_cost, currency, created_at
		) VALUES(?,?,?,?,?,?,?,?)`,
			orderID, cl.ItemID, cl.UnitQty, cl.LossBps, cl.RequiredQty, 0, recCur, now)
		if err != nil {
			return nil, err
		}
	}

	// 10. Audit log
	if err = core.AuditTx(tx, r, "assembly.order_create", "assembly_order", orderID, core.Object{
		"no":         orderNo,
		"recipe_id":  in.RecipeID,
		"runs":       in.Runs,
		"output_qty": totalOutputQty,
	}); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	ord, err := a.fetchOrderDetail(r.Context(), a.DB, orderID, u)
	if err != nil {
		return nil, err
	}
	return core.Object{"order": ord}, nil
}

func (a *Module) cancelOrder(w http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	if id == 0 {
		return nil, bad(400, "无效的组装单ID")
	}
	u := core.CurrentUser(r)

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var status, orderNo string
	err = tx.QueryRow("SELECT status, no FROM assembly_orders WHERE id=?", id).Scan(&status, &orderNo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, bad(404, "组装单不存在")
	}
	if err != nil {
		return nil, err
	}
	if status != "draft" {
		return nil, bad(409, "只有草稿状态的组装单可以取消")
	}

	now := core.Now()
	_, err = tx.Exec("UPDATE assembly_orders SET status='canceled', canceled_at=? WHERE id=?", now, id)
	if err != nil {
		return nil, err
	}

	if err = core.AuditTx(tx, r, "assembly.order_cancel", "assembly_order", id, core.Object{
		"no": orderNo,
	}); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	ord, err := a.fetchOrderDetail(r.Context(), a.DB, id, u)
	if err != nil {
		return nil, err
	}
	return core.Object{"order": ord}, nil
}

func (a *Module) completeOrder(w http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	if id == 0 {
		return nil, bad(400, "无效的组装单ID")
	}
	u := core.CurrentUser(r)

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 1. Fetch order
	var order OrderRow
	err = tx.QueryRow(`SELECT id, no, recipe_id, runs, warehouse_id, status, output_item_id, output_qty, currency, labor_cost, material_cost, total_cost, note
		FROM assembly_orders WHERE id=?`, id).Scan(
		&order.ID, &order.No, &order.RecipeID, &order.Runs, &order.WarehouseID, &order.Status,
		&order.OutputItemID, &order.OutputQty, &order.Currency, &order.LaborCost, &order.MaterialCost, &order.TotalCost, &order.Note)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, bad(404, "组装单不存在")
	}
	if err != nil {
		return nil, err
	}

	// Idempotency: if already done, return existing detail without re-processing
	if order.Status == "done" {
		tx.Rollback()
		ord, err := a.fetchOrderDetail(r.Context(), a.DB, id, u)
		if err != nil {
			return nil, err
		}
		return core.Object{"order": ord}, nil
	}

	if order.Status != "draft" {
		return nil, bad(409, "只有草稿状态的组装单可以完成")
	}

	// 2. Verify warehouse
	var whType string
	var whArchived int
	err = tx.QueryRow("SELECT type, archived FROM inv_warehouses WHERE id=?", order.WarehouseID).Scan(&whType, &whArchived)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, bad(404, "仓库不存在")
	}
	if err != nil {
		return nil, err
	}
	if whArchived == 1 {
		return nil, bad(409, "仓库已归档")
	}
	if whType != "own" && whType != "stall" {
		return nil, bad(400, "组装加工仅支持自有仓或档口仓")
	}

	// 3. Verify output item
	var itemArchived int
	err = tx.QueryRow("SELECT archived FROM inv_items WHERE id=?", order.OutputItemID).Scan(&itemArchived)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, bad(404, "产物物料不存在")
	}
	if err != nil {
		return nil, err
	}
	if itemArchived == 1 {
		return nil, bad(409, "产物物料已归档")
	}

	// 4. Fetch order lines
	linesRows, err := tx.Query(`SELECT ol.id, ol.item_id, ol.unit_qty, ol.loss_bps, ol.required_qty, ol.actual_cost, ol.currency, i.code, i.name, i.unit
		FROM assembly_order_lines ol
		JOIN inv_items i ON i.id = ol.item_id
		WHERE ol.order_id=? ORDER BY ol.id ASC`, id)
	if err != nil {
		return nil, err
	}
	var lines []OrderLineRow
	for linesRows.Next() {
		var l OrderLineRow
		if err = linesRows.Scan(&l.ID, &l.ItemID, &l.UnitQty, &l.LossBps, &l.RequiredQty, &l.ActualCost, &l.Currency, &l.Code, &l.Name, &l.Unit); err != nil {
			linesRows.Close()
			return nil, err
		}
		lines = append(lines, l)
	}
	linesRows.Close()
	if len(lines) == 0 {
		return nil, bad(400, "组装单缺少物料明细")
	}

	// 5. Pre-check currency consistency of warehouse batches for all materials
	for _, l := range lines {
		if l.ItemID == order.OutputItemID {
			return nil, bad(400, "原料物料不能与产物相同")
		}
		if err := a.validateItem(tx, l.ItemID, "原料物料"); err != nil {
			return nil, err
		}
		level, err := inventory.Stock(tx, l.ItemID)
		if err != nil {
			return nil, err
		}
		// Unlimited supplier stock cannot make physically reserved materials
		// available to assembly, which consumes only our own warehouses.
		if level.OnHand < level.Reserved || level.OnHand-level.Reserved < l.RequiredQty {
			return nil, bad(409, "物料 "+l.Code+" 自有可用库存不足（已扣除订单占用）")
		}
		curRows, err := tx.Query("SELECT DISTINCT currency FROM inv_batches WHERE item_id=? AND warehouse_id=? AND qty_left>0", l.ItemID, order.WarehouseID)
		if err != nil {
			return nil, err
		}
		currencies := []string{}
		for curRows.Next() {
			var c string
			if err = curRows.Scan(&c); err != nil {
				curRows.Close()
				return nil, err
			}
			currencies = append(currencies, c)
		}
		curRows.Close()

		if len(currencies) > 1 {
			return nil, bad(400, fmt.Sprintf("物料 %s 存在多种币种批次，拒绝混合扣料", l.Code))
		}
		if len(currencies) == 1 && currencies[0] != order.Currency {
			return nil, bad(400, fmt.Sprintf("物料 %s 库存批次币种 (%s) 与配方币种 (%s) 不一致", l.Code, currencies[0], order.Currency))
		}
	}

	// 6. Deduct raw materials using inventory.StockOut
	var totalMaterialCost int64 = 0
	lineActualCosts := make(map[int64]int64)

	for _, l := range lines {
		out := inventory.Out{
			ItemID:      l.ItemID,
			WarehouseID: order.WarehouseID,
			Qty:         l.RequiredQty,
			BatchID:     0, // FIFO
			Kind:        "assembly_out",
			RefType:     "assembly_order",
			RefID:       order.ID,
			Note:        fmt.Sprintf("组装单 %s 消耗原料", order.No),
			UserID:      u.ID,
		}

		taken, err := inventory.StockOut(tx, out)
		if err != nil {
			return nil, err
		}

		var lineCost int64 = 0
		for _, t := range taken {
			var bCur string
			if err = tx.QueryRow("SELECT currency FROM inv_batches WHERE id=?", t.BatchID).Scan(&bCur); err != nil {
				return nil, err
			}
			if bCur != order.Currency {
				return nil, bad(400, fmt.Sprintf("原料批次币种 (%s) 与组装单币种 (%s) 不一致", bCur, order.Currency))
			}
			lineCost, err = sum(lineCost, t.Cost)
			if err != nil {
				return nil, err
			}
		}

		lineActualCosts[l.ID] = lineCost
		totalMaterialCost, err = sum(totalMaterialCost, lineCost)
		if err != nil {
			return nil, err
		}
	}

	// 7. Calculate total cost = material cost + labor cost
	totalCost, err := sum(totalMaterialCost, order.LaborCost)
	if err != nil {
		return nil, err
	}

	// 8. Produce finished goods batch using inventory.StockIn
	in := inventory.In{
		ItemID:      order.OutputItemID,
		WarehouseID: order.WarehouseID,
		Qty:         order.OutputQty,
		Cost:        totalCost,
		Currency:    order.Currency,
		Grade:       "",
		SupplierID:  0,
		ReceivedAt:  "",
		Kind:        "assembly_in",
		RefType:     "assembly_order",
		RefID:       order.ID,
		Note:        fmt.Sprintf("组装单 %s 产出入库", order.No),
		UserID:      u.ID,
	}

	batchID, err := inventory.StockIn(tx, in)
	if err != nil {
		return nil, err
	}

	// 9. Update order and line records
	now := core.Now()
	_, err = tx.Exec(`UPDATE assembly_orders
		SET status='done', material_cost=?, total_cost=?, output_batch_id=?, done_at=?
		WHERE id=?`,
		totalMaterialCost, totalCost, batchID, now, id)
	if err != nil {
		return nil, err
	}

	for lineID, cost := range lineActualCosts {
		_, err = tx.Exec("UPDATE assembly_order_lines SET actual_cost=? WHERE id=?", cost, lineID)
		if err != nil {
			return nil, err
		}
	}

	// 10. Audit log
	if err = core.AuditTx(tx, r, "assembly.order_complete", "assembly_order", id, core.Object{
		"no":              order.No,
		"runs":            order.Runs,
		"output_qty":      order.OutputQty,
		"output_batch_id": batchID,
	}); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	ord, err := a.fetchOrderDetail(r.Context(), a.DB, id, u)
	if err != nil {
		return nil, err
	}
	return core.Object{"order": ord}, nil
}
