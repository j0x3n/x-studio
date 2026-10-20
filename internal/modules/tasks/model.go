package tasks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

type checkItem struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

type taskRec struct {
	ID            int64
	Title         string
	Body          string
	Status        string
	Priority      string
	AssigneeID    int64
	CreatedBy     int64
	DueAt         string
	TargetType    string
	TargetID      int64
	Checklist     string
	RuleID        int64
	Reopen        int64
	CreatedAt     string
	UpdatedAt     string
	DoneAt        string
	TargetDeleted bool
}

type rule struct {
	ID         int64
	Name       string
	Enabled    bool
	Event      string
	TitleTpl   string
	BodyTpl    string
	AssignMode string
	AssignUser sql.NullInt64
	AssignRole sql.NullInt64
	DueHours   sql.NullInt64
	Priority   string
	CreatedAt  string
	UpdatedAt  string
}

type note struct {
	TaskID     int64
	UserID     int64
	Type       string
	Title      string
	Body       string
	Link       string
	TargetType string
	TargetID   int64
	OnceKey    string
}

type outgoing struct {
	userID int64
	frame  []byte
}

var statuses = map[string]bool{"todo": true, "doing": true, "waiting": true, "done": true, "canceled": true}
var priorities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}
var targetTypes = map[string]bool{"product": true, "listing": true, "asset": true, "inv_item": true, "purchase_order": true}
var ruleEvents = map[string]bool{"product.created": true, "assets.first_upload": true, "render.failed": true, "review.rejected": true, "inventory.low_stock": true, "purchase.received": true}
var assignModes = map[string]bool{"fixed": true, "role_round_robin": true, "role_least_loaded": true, "product_owner": true, "job_user": true}

var noticeTypes = []string{"task_assigned", "task_mentioned", "task_comment", "task_due", "task_overdue", "task_done", "job_failed", "job_done", "review_result", "quota_80", "task_unassigned"}

func knownType(s string) bool {
	for _, v := range noticeTypes {
		if v == s {
			return true
		}
	}
	return false
}

func scanTask(s core.Scanner) (taskRec, error) {
	var t taskRec
	var assignee, targetID, ruleID sql.NullInt64
	var due, done, targetType sql.NullString
	err := s.Scan(&t.ID, &t.Title, &t.Body, &t.Status, &t.Priority, &assignee, &t.CreatedBy, &due, &targetType, &targetID, &t.Checklist, &ruleID, &t.Reopen, &t.CreatedAt, &t.UpdatedAt, &done, &t.TargetDeleted)
	if err != nil {
		return t, err
	}
	if assignee.Valid {
		t.AssigneeID = assignee.Int64
	}
	if targetType.Valid {
		t.TargetType = targetType.String
	}
	if targetID.Valid {
		t.TargetID = targetID.Int64
	}
	if ruleID.Valid {
		t.RuleID = ruleID.Int64
	}
	if due.Valid {
		t.DueAt = due.String
	}
	if done.Valid {
		t.DoneAt = done.String
	}
	return t, nil
}

const taskColumns = "id,title,body,status,priority,assignee_id,created_by,due_at,target_type,target_id,checklist,rule_id,reopen_count,created_at,updated_at,done_at,target_deleted"

func (a *Module) loadTask(id int64) (taskRec, error) {
	return scanTask(a.DB.QueryRow("SELECT "+taskColumns+" FROM tasks WHERE id=?", id))
}

func parseChecks(raw string) []checkItem {
	var items []checkItem
	if json.Unmarshal([]byte(raw), &items) != nil || items == nil {
		return []checkItem{}
	}
	return items
}

func validateChecks(items []checkItem) error {
	if len(items) > 50 {
		return bad(400, "检查清单最多 50 项")
	}
	for i := range items {
		items[i].Text = strings.TrimSpace(items[i].Text)
		if items[i].Text == "" || runeLen(items[i].Text) > 200 {
			return bad(400, "检查项需要 1–200 字")
		}
	}
	return nil
}

func doneCount(items []checkItem) int {
	n := 0
	for _, it := range items {
		if it.Done {
			n++
		}
	}
	return n
}

func (a *Module) isOverdue(status, due string) bool {
	if due == "" || status == "done" || status == "canceled" {
		return false
	}
	t, ok := parseTS(due)
	if !ok {
		return false
	}
	return t.Before(a.now())
}

func (a *Module) userName(id int64) string {
	if id == 0 {
		return "系统"
	}
	var name string
	err := a.DB.QueryRow("SELECT CASE WHEN display_name!='' THEN display_name ELSE username END FROM users WHERE id=?", id).Scan(&name)
	if err != nil || name == "" {
		return "已删除"
	}
	return name
}

func (a *Module) userBrief(id int64) core.Object {
	return core.Object{"id": id, "name": a.userName(id)}
}

func productWhere(u core.User, alias string) (string, []any) {
	if u.Role.Key == "admin" {
		return "1=1", nil
	}
	if u.Role.Scope == "shares" {
		return fmt.Sprintf("EXISTS(SELECT 1 FROM shares sh WHERE sh.product_id=%s.id AND sh.user_id=?)", alias), []any{u.ID}
	}
	return fmt.Sprintf("(%s.created_by=? OR EXISTS(SELECT 1 FROM listings sl JOIN user_shops us ON us.shop_id=sl.shop_id WHERE sl.product_id=%s.id AND us.user_id=?))", alias, alias), []any{u.ID, u.ID}
}

func listingWhere(u core.User, alias string) (string, []any) {
	if u.Role.Key == "admin" {
		return "1=1", nil
	}
	if u.Role.Scope != "shops" {
		return "0=1", nil
	}
	return fmt.Sprintf("EXISTS(SELECT 1 FROM user_shops us WHERE us.shop_id=%s.shop_id AND us.user_id=?)", alias), []any{u.ID}
}

func (a *Module) visibleWhere(u core.User) (string, []any) {
	return a.visibleWhereQ(a.DB, u)
}

func (a *Module) legacyVisibleWhere(u core.User) (string, []any) {
	if u.Role.Key == "admin" {
		return "1=1", nil
	}
	part := "(t.assignee_id=? OR t.created_by=? OR EXISTS(SELECT 1 FROM task_watchers w WHERE w.task_id=t.id AND w.user_id=?))"
	args := []any{u.ID, u.ID, u.ID}
	if !u.Has(core.TaskManage) {
		return part, args
	}
	pw, pa := productWhere(u, "p")
	lw, la := listingWhere(u, "l")
	aw, aa := productWhere(u, "p")
	clause := part + " OR t.target_type IS NULL OR ((" + liveTarget + ") AND ((t.target_type='product' AND EXISTS(SELECT 1 FROM products p WHERE p.id=t.target_id AND " + pw + ")) OR (t.target_type='listing' AND EXISTS(SELECT 1 FROM listings l WHERE l.id=t.target_id AND " + lw + ")) OR (t.target_type='asset' AND EXISTS(SELECT 1 FROM assets ast JOIN products p ON p.id=ast.product_id WHERE ast.id=t.target_id AND " + aw + "))))"
	args = append(args, pa...)
	args = append(args, la...)
	args = append(args, aa...)
	return "(" + clause + ")", args
}

func (a *Module) targetExists(typ string, id int64) bool {
	q := ""
	switch typ {
	case "product":
		q = "SELECT count(*) FROM products WHERE id=?"
	case "listing":
		q = "SELECT count(*) FROM listings WHERE id=?"
	case "asset":
		q = "SELECT count(*) FROM assets WHERE id=?"
	case "inv_item":
		q = "SELECT count(*) FROM inv_items WHERE id=?"
	case "purchase_order":
		q = "SELECT count(*) FROM purchase_orders WHERE id=?"
	default:
		return false
	}
	var n int
	if a.DB.QueryRow(q, id).Scan(&n) != nil {
		return false
	}
	return n > 0
}

func (a *Module) targetVisible(u core.User, typ string, id int64) bool {
	if commerceTarget(typ) {
		perm := core.InventoryView
		if typ == "purchase_order" {
			perm = core.PurchaseManage
		}
		return u.Role.Scope != "shares" && u.Has(perm) && a.targetExists(typ, id)
	}
	switch typ {
	case "product":
		return a.CanProduct(u, id)
	case "listing":
		return a.CanListing(u, id)
	case "asset":
		var pid int64
		if a.DB.QueryRow("SELECT product_id FROM assets WHERE id=?", id).Scan(&pid) != nil {
			return false
		}
		return a.CanProduct(u, pid)
	default:
		return false
	}
}

func (a *Module) isWatcher(taskID, userID int64) bool {
	var n int
	_ = a.DB.QueryRow("SELECT count(*) FROM task_watchers WHERE task_id=? AND user_id=?", taskID, userID).Scan(&n)
	return n > 0
}

func (a *Module) canSeeTask(u core.User, t taskRec) bool {
	if commerceTarget(t.TargetType) && (!a.taskTargetAlive(t) || !a.targetVisible(u, t.TargetType, t.TargetID)) {
		return false
	}
	if u.Role.Key == "admin" {
		return true
	}
	if t.AssigneeID == u.ID || t.CreatedBy == u.ID || a.isWatcher(t.ID, u.ID) {
		return true
	}
	if !u.Has(core.TaskManage) {
		return false
	}
	if t.TargetType == "" || t.TargetID == 0 {
		return true
	}
	if !a.taskTargetAlive(t) {
		return false
	}
	return a.targetVisible(u, t.TargetType, t.TargetID)
}

func (a *Module) targetNames(typ string, id int64) (product, shop string, productID int64, exists bool) {
	if typ == "inv_item" {
		err := a.DB.QueryRow("SELECT code || ' ' || name FROM inv_items WHERE id=?", id).Scan(&product)
		return product, "", 0, err == nil
	}
	if typ == "purchase_order" {
		err := a.DB.QueryRow("SELECT no FROM purchase_orders WHERE id=?", id).Scan(&product)
		return product, "", 0, err == nil
	}
	switch typ {
	case "product":
		err := a.DB.QueryRow("SELECT name FROM products WHERE id=?", id).Scan(&product)
		return product, "", id, err == nil
	case "listing":
		err := a.DB.QueryRow("SELECT p.name, s.name, p.id FROM listings l JOIN products p ON p.id=l.product_id JOIN shops s ON s.id=l.shop_id WHERE l.id=?", id).Scan(&product, &shop, &productID)
		return product, shop, productID, err == nil
	case "asset":
		err := a.DB.QueryRow("SELECT p.name, p.id FROM assets ast JOIN products p ON p.id=ast.product_id WHERE ast.id=?", id).Scan(&product, &productID)
		return product, "", productID, err == nil
	default:
		return "", "", 0, false
	}
}

func (a *Module) targetJSON(u core.User, t taskRec) any {
	typ, id := t.TargetType, t.TargetID
	if typ == "" || id == 0 {
		return nil
	}
	if !a.taskTargetAlive(t) {
		return core.Object{"type": typ, "id": id, "label": "已删除", "path": ""}
	}
	if !a.targetVisible(u, typ, id) {
		return core.Object{"type": typ, "id": id, "label": "无权查看", "path": ""}
	}
	product, shop, pid, exists := a.targetNames(typ, id)
	if !exists {
		return core.Object{"type": typ, "id": id, "label": "已删除", "path": ""}
	}
	label := product
	if typ == "inv_item" {
		return core.Object{"type": typ, "id": id, "label": label, "path": "/inventory/items/" + strconv.FormatInt(id, 10)}
	}
	if typ == "purchase_order" {
		return core.Object{"type": typ, "id": id, "label": label, "path": "/purchase-orders/" + strconv.FormatInt(id, 10)}
	}
	path := ""
	if pid > 0 {
		path = "/products/" + strconv.FormatInt(pid, 10)
	}
	if typ == "listing" {
		label = product + " · " + shop
	}
	return core.Object{"type": typ, "id": id, "label": label, "path": path}
}

func (a *Module) watchersOf(ids []int64) map[int64][]core.Object {
	out := map[int64][]core.Object{}
	if len(ids) == 0 {
		return out
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
		out[id] = []core.Object{}
	}
	rows, err := a.DB.Query("SELECT w.task_id, u.id, CASE WHEN u.display_name!='' THEN u.display_name ELSE u.username END FROM task_watchers w JOIN users u ON u.id=w.user_id WHERE w.task_id IN ("+strings.Join(ph, ",")+") ORDER BY u.id", args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var tid, uid int64
		var name string
		if rows.Scan(&tid, &uid, &name) != nil {
			continue
		}
		out[tid] = append(out[tid], core.Object{"id": uid, "name": name})
	}
	return out
}

func (a *Module) taskJSON(u core.User, t taskRec, watchers []core.Object, count int) core.Object {
	if watchers == nil {
		watchers = []core.Object{}
	}
	checks := parseChecks(t.Checklist)
	var assignee any
	if t.AssigneeID > 0 {
		assignee = a.userBrief(t.AssigneeID)
	}
	var ruleID any
	if t.RuleID > 0 {
		ruleID = t.RuleID
	}
	return core.Object{
		"id": t.ID, "title": t.Title, "body": t.Body, "status": t.Status, "priority": t.Priority,
		"assignee": assignee, "created_by": a.userBrief(t.CreatedBy),
		"due_at": t.DueAt, "overdue": a.isOverdue(t.Status, t.DueAt),
		"target":    a.targetJSON(u, t),
		"checklist": checks, "checklist_done": doneCount(checks),
		"watchers": watchers, "comment_count": count,
		"rule_id": ruleID, "reopen_count": t.Reopen,
		"created_at": t.CreatedAt, "updated_at": t.UpdatedAt, "done_at": t.DoneAt,
	}
}

func (a *Module) commentCount(id int64) int {
	var n int
	_ = a.DB.QueryRow("SELECT count(*) FROM task_comments WHERE task_id=?", id).Scan(&n)
	return n
}

func nullableID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func insertEvent(q dbx, taskID, userID int64, kind string, detail any, ts string) error {
	_, err := q.Exec("INSERT INTO task_events(task_id,user_id,kind,detail,created_at) VALUES(?,?,?,?,?)", taskID, userID, kind, core.JS(detail), ts)
	return err
}

type newTask struct {
	Title, Body, Status, Priority string
	Assignee, CreatedBy           int64
	Due, TargetType               string
	TargetID, RuleID              int64
	Checklist                     []checkItem
	DoneAt                        string
}

func insertTask(q dbx, n newTask, ts string) (int64, error) {
	if n.Status == "" {
		n.Status = "todo"
	}
	if n.Priority == "" {
		n.Priority = "normal"
	}
	if n.Checklist == nil {
		n.Checklist = []checkItem{}
	}
	var targetType, targetID any
	if n.TargetType != "" && n.TargetID > 0 {
		targetType, targetID = n.TargetType, n.TargetID
	}
	res, err := q.Exec(`INSERT INTO tasks(title,body,status,priority,assignee_id,created_by,due_at,target_type,target_id,checklist,rule_id,reopen_count,created_at,updated_at,done_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,0,?,?,?)`,
		n.Title, n.Body, n.Status, n.Priority, nullableID(n.Assignee), n.CreatedBy, nullStr(n.Due), targetType, targetID, core.JS(n.Checklist), nullableID(n.RuleID), ts, ts, nullStr(n.DoneAt))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err == nil {
		err = bindTaskTarget(q, id)
	}
	return id, err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (a *Module) requireTarget(u core.User, typ string, id int64) error {
	if typ == "" && id == 0 {
		return nil
	}
	if !targetTypes[typ] || id <= 0 {
		return bad(400, "对象类型或编号不正确")
	}
	if !a.targetExists(typ, id) || !a.targetVisible(u, typ, id) {
		return bad(404, "对象不存在")
	}
	return nil
}

func targetMissing(typ string) string {
	switch typ {
	case "listing":
		return "上架版本不存在"
	case "asset":
		return "素材不存在"
	default:
		return "商品不存在"
	}
}

func targetNoun(typ string) string {
	switch typ {
	case "inv_item":
		return "物料"
	case "purchase_order":
		return "采购单"
	case "listing":
		return "上架版本"
	case "asset":
		return "素材"
	default:
		return "商品"
	}
}

func (a *Module) checkMember(id int64, who, typ string, targetID int64) error {
	if id <= 0 {
		return bad(400, who+"不存在")
	}
	u, err := a.User(id)
	if err != nil {
		return bad(400, who+"不存在")
	}
	if u.Disabled {
		return bad(400, who+"已停用")
	}
	if !u.Has(core.TaskView) {
		return bad(400, who+"没有任务权限")
	}
	if typ != "" && targetID > 0 && !a.targetVisible(u, typ, targetID) {
		return bad(400, fmt.Sprintf("%s看不到这个%s", a.userName(id), targetNoun(typ)))
	}
	return nil
}

func dedupe(ids []int64) []int64 {
	seen := map[int64]bool{}
	out := []int64{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
