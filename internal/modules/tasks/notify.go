package tasks

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"xstudio/internal/core"
)

type hub struct {
	mu   sync.Mutex
	next int
	subs map[int64]map[int]chan []byte
}

func newHub() *hub { return &hub{subs: map[int64]map[int]chan []byte{}} }

func (h *hub) subscribe(uid int64) (<-chan []byte, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	id := h.next
	ch := make(chan []byte, 16)
	if h.subs[uid] == nil {
		h.subs[uid] = map[int]chan []byte{}
	}
	h.subs[uid][id] = ch
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if m := h.subs[uid]; m != nil {
			delete(m, id)
			if len(m) == 0 {
				delete(h.subs, uid)
			}
		}
	}
}

func (h *hub) publish(uid int64, frame []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs[uid] {
		select {
		case ch <- frame:
		default:
		}
	}
}

func (h *hub) count(uid int64) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[uid])
}

func sseFrame(event string, v any) []byte {
	b, _ := json.Marshal(v)
	return []byte("event: " + event + "\ndata: " + string(b) + "\n\n")
}

type typePref struct {
	Inbox   *bool `json:"inbox"`
	Desktop *bool `json:"desktop"`
}
type quietPref struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type prefDoc struct {
	Types map[string]typePref `json:"types"`
	Quiet *quietPref          `json:"quiet"`
}
type storedPref struct {
	Types map[string]struct {
		Inbox   bool `json:"inbox"`
		Desktop bool `json:"desktop"`
	} `json:"types"`
	Quiet *quietPref `json:"quiet"`
}

func (a *Module) getPrefs(w http.ResponseWriter, r *http.Request) {
	doc, err := a.loadPref(core.CurrentUser(r).ID)
	if err != nil {
		writeErr(w, err, "读取通知偏好失败：", "读取通知偏好失败")
		return
	}
	core.WriteJSON(w, 200, doc)
}

func (a *Module) loadPref(uid int64) (storedPref, error) {
	doc := storedPref{Types: map[string]struct {
		Inbox   bool `json:"inbox"`
		Desktop bool `json:"desktop"`
	}{}}
	var raw string
	err := a.DB.QueryRow("SELECT prefs FROM notification_prefs WHERE user_id=?", uid).Scan(&raw)
	if err == nil {
		_ = json.Unmarshal([]byte(raw), &doc)
		if doc.Types == nil {
			doc.Types = map[string]struct {
				Inbox   bool `json:"inbox"`
				Desktop bool `json:"desktop"`
			}{}
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return doc, err
	}
	return doc, nil
}

func (a *Module) putPrefs(w http.ResponseWriter, r *http.Request) {
	var in prefDoc
	if !core.Decode(w, r, &in) {
		return
	}
	stored := storedPref{Types: map[string]struct {
		Inbox   bool `json:"inbox"`
		Desktop bool `json:"desktop"`
	}{}}
	for k, v := range in.Types {
		if !knownType(k) {
			core.Fail(w, 400, "不支持的通知类型")
			return
		}
		inbox, desktop := true, true
		if v.Inbox != nil {
			inbox = *v.Inbox
		}
		if v.Desktop != nil {
			desktop = *v.Desktop
		}
		stored.Types[k] = struct {
			Inbox   bool `json:"inbox"`
			Desktop bool `json:"desktop"`
		}{inbox, desktop}
	}
	if in.Quiet != nil {
		if !clockOK(in.Quiet.From) || !clockOK(in.Quiet.To) {
			core.Fail(w, 400, "免打扰时间要用 24 小时制，比如 22:00")
			return
		}
		if in.Quiet.From == in.Quiet.To {
			core.Fail(w, 400, "免打扰的开始和结束不能相同")
			return
		}
		stored.Quiet = &quietPref{From: in.Quiet.From, To: in.Quiet.To}
	}
	uid := core.CurrentUser(r).ID
	ts := a.ts()
	raw := core.JS(stored)
	_, err := a.DB.Exec(`INSERT INTO notification_prefs(user_id,prefs,updated_at) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET prefs=excluded.prefs, updated_at=excluded.updated_at`, uid, raw, ts)
	if err != nil {
		writeErr(w, err, "保存通知偏好失败：", "保存通知偏好失败")
		return
	}
	core.WriteJSON(w, 200, stored)
}

func clockOK(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	h, err1 := strconv.Atoi(s[:2])
	m, err2 := strconv.Atoi(s[3:])
	return err1 == nil && err2 == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59
}

func inQuiet(now time.Time, from, to string) bool {
	if from == "" || to == "" || from == to {
		return false
	}
	local := now.In(shanghai())
	mins := local.Hour()*60 + local.Minute()
	f := hm(from)
	t := hm(to)
	if f < 0 || t < 0 {
		return false
	}
	if f < t {
		return mins >= f && mins < t
	}
	return mins >= f || mins < t
}

func hm(s string) int {
	if !clockOK(s) {
		return -1
	}
	h, _ := strconv.Atoi(s[:2])
	m, _ := strconv.Atoi(s[3:])
	return h*60 + m
}

type chanPref struct {
	inbox, desktop bool
	quietFrom      string
	quietTo        string
}

func readChan(q dbx, uid int64, typ string) (chanPref, error) {
	p := chanPref{inbox: true, desktop: true}
	var raw string
	err := q.QueryRow("SELECT prefs FROM notification_prefs WHERE user_id=?", uid).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	var doc storedPref
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return p, nil
	}
	if item, ok := doc.Types[typ]; ok {
		p.inbox = item.Inbox
		p.desktop = item.Desktop
	}
	if doc.Quiet != nil {
		p.quietFrom = doc.Quiet.From
		p.quietTo = doc.Quiet.To
	}
	return p, nil
}

func noteObject(id int64, n note, created, read string, desktop *bool) core.Object {
	var tt, tid any
	if n.TargetType != "" && n.TargetID > 0 {
		tt = n.TargetType
		tid = n.TargetID
	}
	o := core.Object{
		"id": id, "type": n.Type, "title": n.Title, "body": n.Body, "link": n.Link,
		"target_type": tt, "target_id": tid, "read_at": read, "created_at": created,
	}
	if desktop != nil {
		o["desktop"] = *desktop
	}
	return o
}

// Use the caller's transaction: Core has a single database connection, and the
// task may have just been inserted or reassigned in that transaction.
func (a *Module) canReceiveTask(q dbx, uid, taskID int64) (bool, error) {
	if uid <= 0 || taskID <= 0 {
		return false, nil
	}
	u, err := core.ScanUser(q.QueryRow(core.UserQuery+" WHERE u.id=?", uid))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if u.Disabled || !u.Has(core.TaskView) {
		return false, nil
	}
	vis, args := a.visibleWhereQ(q, u)
	var n int
	err = q.QueryRow("SELECT count(*) FROM tasks t WHERE t.id=? AND ("+vis+")", append([]any{taskID}, args...)...).Scan(&n)
	return n > 0, err
}

func (a *Module) insertNote(q dbx, n note, ts string) (*outgoing, error) {
	if n.UserID <= 0 {
		return nil, nil
	}
	if strings.HasPrefix(n.Type, "task_") {
		ok, err := a.canReceiveTask(q, n.UserID, n.TaskID)
		if err != nil || !ok {
			return nil, err
		}
	}
	if n.OnceKey != "" {
		res, err := q.Exec("INSERT OR IGNORE INTO task_once(key, created_at) VALUES(?,?)", n.OnceKey, ts)
		if err != nil {
			return nil, err
		}
		aff, _ := res.RowsAffected()
		if aff == 0 {
			return nil, nil
		}
	}
	pref, err := readChan(q, n.UserID, n.Type)
	if err != nil {
		return nil, err
	}
	if !pref.inbox {
		return nil, nil
	}
	var tt, tid any
	if n.TargetType != "" && n.TargetID > 0 {
		tt, tid = n.TargetType, n.TargetID
	}
	res, err := q.Exec("INSERT INTO notifications(user_id,type,title,body,link,target_type,target_id,created_at,task_id) VALUES(?,?,?,?,?,?,?,?,?)", n.UserID, n.Type, n.Title, n.Body, n.Link, tt, tid, ts, nullableID(n.TaskID))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if commerceTarget(n.TargetType) {
		if _, err = q.Exec("UPDATE notifications SET target_created_at=coalesce((SELECT target_created_at FROM tasks WHERE id=?),'') WHERE id=?", n.TaskID, id); err != nil {
			return nil, err
		}
	}
	desktop := pref.desktop && !inQuiet(a.now(), pref.quietFrom, pref.quietTo)
	return &outgoing{userID: n.UserID, frame: sseFrame("notification", noteObject(id, n, ts, "", &desktop))}, nil
}

func (a *Module) publish(list []outgoing) {
	for _, o := range list {
		if a.frameAllowed(o.userID, o.frame) {
			a.hub.publish(o.userID, o.frame)
			a.enqueuePush(o)
		}
	}
}

func (a *Module) deliver(n note) {
	tx, err := a.DB.Begin()
	if err != nil {
		log.Printf("写入通知失败：%v", err)
		return
	}
	defer tx.Rollback()
	o, err := a.insertNote(tx, n, a.ts())
	if err != nil {
		log.Printf("写入通知失败：%v", err)
		return
	}
	if err = tx.Commit(); err != nil {
		log.Printf("写入通知失败：%v", err)
		return
	}
	if o != nil {
		if a.frameAllowed(o.userID, o.frame) {
			a.hub.publish(o.userID, o.frame)
			a.enqueuePush(*o)
		}
	}
}

func frameMeta(frame []byte) (int64, bool) {
	parts := strings.SplitN(string(frame), "\ndata: ", 2)
	if len(parts) != 2 {
		return 0, false
	}
	var n struct {
		ID      int64 `json:"id"`
		Desktop *bool `json:"desktop"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(parts[1])), &n) != nil || n.ID <= 0 {
		return 0, false
	}
	if n.Desktop != nil && !*n.Desktop {
		return n.ID, false
	}
	return n.ID, true
}

func (a *Module) noteVisible(uid, id int64) bool {
	if uid <= 0 || id <= 0 {
		return false
	}
	u, err := a.User(uid)
	if err != nil || u.Disabled {
		return false
	}
	var typ string
	var target sql.NullString
	var targetID, taskID sql.NullInt64
	err = a.DB.QueryRow("SELECT type, target_type, target_id, task_id FROM notifications WHERE id=? AND user_id=?", id, uid).Scan(&typ, &target, &targetID, &taskID)
	if err != nil {
		return false
	}
	vis, args := a.noticeScope(u)
	var n int
	if a.DB.QueryRow("SELECT count(*) FROM notifications WHERE id=? AND user_id=? AND ("+vis+")", append([]any{id, uid}, args...)...).Scan(&n) != nil || n != 1 {
		return false
	}
	tt := ""
	var tid int64
	if target.Valid {
		tt = target.String
	}
	if targetID.Valid {
		tid = targetID.Int64
	}
	if tt != "" || tid != 0 {
		if !a.targetStillAllowed(u, tt, tid) {
			return false
		}
	}
	if strings.HasPrefix(typ, "task_") {
		if !taskID.Valid || taskID.Int64 <= 0 {
			return false
		}
		ok, err := a.canReceiveTask(a.DB, uid, taskID.Int64)
		if err != nil || !ok {
			return false
		}
	}
	return true
}

func (a *Module) targetStillAllowed(u core.User, typ string, id int64) bool {
	if u.Disabled || id <= 0 {
		return false
	}
	switch typ {
	case "user":
		return id == u.ID
	case "product", "listing", "asset", "inv_item", "purchase_order":
		if (typ == "product" || typ == "listing" || typ == "asset") && !u.Has(core.ProductView) {
			return false
		}
		if !a.targetExists(typ, id) {
			return false
		}
		return a.targetVisible(u, typ, id)
	default:
		return false
	}
}

func (a *Module) pushAllowed(uid, id int64) bool {
	if !a.noteVisible(uid, id) {
		return false
	}
	var typ string
	var read sql.NullString
	if a.DB.QueryRow("SELECT type, read_at FROM notifications WHERE id=? AND user_id=?", id, uid).Scan(&typ, &read) != nil {
		return false
	}
	if read.Valid && strings.TrimSpace(read.String) != "" {
		return false
	}
	pref, err := readChan(a.DB, uid, typ)
	if err != nil || !pref.inbox || !pref.desktop || inQuiet(a.now(), pref.quietFrom, pref.quietTo) {
		return false
	}
	return true
}

func (a *Module) listNotes(w http.ResponseWriter, r *http.Request) {
	u := core.CurrentUser(r)
	q := r.URL.Query()
	vis, visArgs := a.noticeScope(u)
	where := "user_id=? AND (" + vis + ")"
	args := append([]any{u.ID}, visArgs...)
	if q.Get("unread") == "1" {
		where += " AND read_at IS NULL"
	}
	if typ := q.Get("type"); typ != "" {
		if !knownType(typ) {
			core.Fail(w, 400, "不支持的通知类型")
			return
		}
		where += " AND type=?"
		args = append(args, typ)
	}
	var total int
	if err := a.DB.QueryRow("SELECT count(*) FROM notifications WHERE "+where, args...).Scan(&total); err != nil {
		writeErr(w, err, "读取通知失败：", "读取通知失败")
		return
	}
	offset, size := core.Page(r)
	rows, err := a.DB.Query("SELECT id,type,title,body,link,target_type,target_id,read_at,created_at FROM notifications WHERE "+where+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?", append(append([]any{}, args...), size, offset)...)
	if err != nil {
		writeErr(w, err, "读取通知失败：", "读取通知失败")
		return
	}
	defer rows.Close()
	items := []core.Object{}
	for rows.Next() {
		var id int64
		var tid sql.NullInt64
		var typ, title, body, link, created string
		var target, read sql.NullString
		if err = rows.Scan(&id, &typ, &title, &body, &link, &target, &tid, &read, &created); err != nil {
			writeErr(w, err, "读取通知失败：", "读取通知失败")
			return
		}
		n := note{Type: typ, Title: title, Body: body, Link: link}
		if target.Valid {
			n.TargetType = target.String
		}
		if tid.Valid {
			n.TargetID = tid.Int64
		}
		readAt := ""
		if read.Valid {
			readAt = read.String
		}
		items = append(items, noteObject(id, n, created, readAt, nil))
	}
	core.WriteJSON(w, 200, core.Object{"items": items, "total": total})
}

func (a *Module) unreadCount(w http.ResponseWriter, r *http.Request) {
	var n int
	var err error
	n, err = a.unreadFor(core.CurrentUser(r))
	if err != nil {
		writeErr(w, err, "读取未读数失败：", "读取未读数失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"count": n})
}

func (a *Module) markRead(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if !in.All && len(in.IDs) == 0 {
		core.Fail(w, 400, "要指定通知编号或全部已读")
		return
	}
	uid := core.CurrentUser(r).ID
	vis, visArgs := a.noticeScope(core.CurrentUser(r))
	ts := a.ts()
	var updated int64
	if in.All {
		res, err := a.DB.Exec("UPDATE notifications SET read_at=? WHERE user_id=? AND read_at IS NULL AND ("+vis+")", append([]any{ts, uid}, visArgs...)...)
		if err != nil {
			writeErr(w, err, "标记已读失败：", "标记已读失败")
			return
		}
		updated, _ = res.RowsAffected()
	} else {
		for _, id := range in.IDs {
			res, err := a.DB.Exec("UPDATE notifications SET read_at=? WHERE id=? AND user_id=? AND read_at IS NULL AND ("+vis+")", append([]any{ts, id, uid}, visArgs...)...)
			if err != nil {
				writeErr(w, err, "标记已读失败：", "标记已读失败")
				return
			}
			n, _ := res.RowsAffected()
			updated += n
		}
	}
	core.WriteJSON(w, 200, core.Object{"updated": updated})
}

func (a *Module) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		core.Fail(w, 500, "服务器不支持流式输出")
		return
	}
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		log.Printf("关闭写超时失败：%v", err)
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	u := core.CurrentUser(r)
	var n int
	var err error
	n, err = a.unreadFor(u)
	if err != nil {
		core.Fail(w, 500, "读取未读数失败")
		return
	}
	if _, err := fmt.Fprintf(w, "event: unread\ndata: {\"count\":%d}\n\n", n); err != nil {
		return
	}
	flusher.Flush()
	ch, cancel := a.hub.subscribe(u.ID)
	defer cancel()
	ping := a.PingInterval
	if ping <= 0 {
		ping = 25 * time.Second
	}
	ticker := time.NewTicker(ping)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-a.Context().Done():
			return
		case frame := <-ch:
			if !a.frameAllowed(u.ID, frame) {
				continue
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
