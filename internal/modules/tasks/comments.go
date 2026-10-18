package tasks

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

func (a *Module) commentsOf(taskID int64) ([]core.Object, error) {
	rows, err := a.DB.Query("SELECT id,user_id,body,mentions,created_at FROM task_comments WHERE task_id=? ORDER BY id", taskID)
	if err != nil {
		return nil, err
	}
	type row struct {
		id, uid       int64
		body, raw, at string
	}
	var list []row
	for rows.Next() {
		var v row
		if err = rows.Scan(&v.id, &v.uid, &v.body, &v.raw, &v.at); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []core.Object{}
	for _, v := range list {
		var ids []int64
		_ = json.Unmarshal([]byte(v.raw), &ids)
		if ids == nil {
			ids = []int64{}
		}
		out = append(out, core.Object{"id": v.id, "user": a.userBrief(v.uid), "body": v.body, "mentions": ids, "created_at": v.at})
	}
	return out, nil
}

type mentionHit struct {
	id    int64
	token string
	user  bool
}

func (a *Module) parseMentions(body string) []mentionHit {
	type cand struct {
		id   int64
		name string
	}
	rows, err := a.DB.Query("SELECT id, username, display_name FROM users")
	if err != nil {
		return nil
	}
	var cands []cand
	for rows.Next() {
		var id int64
		var user, display string
		if rows.Scan(&id, &user, &display) != nil {
			continue
		}
		if user != "" {
			cands = append(cands, cand{id, user})
		}
		if display != "" && display != user {
			cands = append(cands, cand{id, display})
		}
	}
	rows.Close()
	sort.Slice(cands, func(i, j int) bool { return len([]rune(cands[i].name)) > len([]rune(cands[j].name)) })
	var hits []mentionHit
	rest := body
	for {
		i := strings.Index(rest, "@")
		if i < 0 {
			break
		}
		rest = rest[i+1:]
		matched := false
		for _, c := range cands {
			if c.name != "" && strings.HasPrefix(rest, c.name) {
				hits = append(hits, mentionHit{id: c.id, token: c.name, user: true})
				rest = rest[len(c.name):]
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		token := rest
		if j := strings.IndexAny(rest, " \t\r\n@,，。！？、；：,.!?;:"); j >= 0 {
			token = rest[:j]
		}
		token = strings.TrimSpace(token)
		if token != "" {
			hits = append(hits, mentionHit{token: token})
			rest = rest[len(token):]
		} else if rest != "" {
			rest = rest[1:]
		} else {
			break
		}
	}
	return hits
}

func (a *Module) comment(w http.ResponseWriter, r *http.Request) {
	a.Core.Mu.Lock()
	defer a.Core.Mu.Unlock()
	u := core.CurrentUser(r)
	t, err := a.loadTask(core.ID(r, "id"))
	if err != nil || !a.canSeeTask(u, t) {
		core.Fail(w, 404, "任务不存在")
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	in.Body = strings.TrimSpace(in.Body)
	if in.Body == "" || runeLen(in.Body) > 2000 {
		core.Fail(w, 400, "评论需要 1–2000 字")
		return
	}
	hits := a.parseMentions(in.Body)
	mentioned := []int64{}
	ignored := []string{}
	seenUser := map[int64]bool{}
	seenIgnore := map[string]bool{}
	for _, h := range hits {
		if !h.user {
			if h.token != "" && !seenIgnore[h.token] {
				ignored = append(ignored, h.token)
				seenIgnore[h.token] = true
			}
			continue
		}
		if seenUser[h.id] || h.id == u.ID {
			continue
		}
		seenUser[h.id] = true
		canReceive, e := a.canReceiveTask(a.DB, h.id, t.ID)
		if e != nil {
			writeErr(w, e, "检查提及权限失败：", "保存评论失败")
			return
		}
		if !canReceive {
			name := h.token
			if !seenIgnore[name] {
				ignored = append(ignored, name)
				seenIgnore[name] = true
			}
			continue
		}
		mentioned = append(mentioned, h.id)
	}
	tx, err := a.DB.Begin()
	if err != nil {
		core.Fail(w, 500, "保存评论失败")
		return
	}
	defer tx.Rollback()
	ts := a.ts()
	res, err := tx.Exec("INSERT INTO task_comments(task_id,user_id,body,mentions,created_at) VALUES(?,?,?,?,?)", t.ID, u.ID, in.Body, core.JS(mentioned), ts)
	if err != nil {
		writeErr(w, err, "保存评论失败：", "保存评论失败")
		return
	}
	cid, _ := res.LastInsertId()
	if err = tx.Commit(); err != nil {
		core.Fail(w, 500, "保存评论失败")
		return
	}
	a.Audit(r, "task.comment", "task", t.ID, core.Object{"comment_id": cid, "mentions": mentioned})
	link := "/tasks/" + strconv.FormatInt(t.ID, 10)
	notified := map[int64]bool{u.ID: true}
	for _, id := range mentioned {
		a.deliver(note{TaskID: t.ID, UserID: id, Type: "task_mentioned", Title: clip("评论里提到你："+t.Title, 100), Body: clip(in.Body, 200), Link: link, TargetType: t.TargetType, TargetID: t.TargetID})
		notified[id] = true
	}
	recipients := []int64{}
	if t.AssigneeID > 0 {
		recipients = append(recipients, t.AssigneeID)
	}
	if t.CreatedBy > 0 {
		recipients = append(recipients, t.CreatedBy)
	}
	recipients = append(recipients, a.watcherIDs(t.ID)...)
	seen := map[int64]bool{}
	for _, id := range recipients {
		if id <= 0 || notified[id] || seen[id] {
			continue
		}
		seen[id] = true
		a.deliver(note{TaskID: t.ID, UserID: id, Type: "task_comment", Title: clip("任务有新评论："+t.Title, 100), Body: clip(in.Body, 200), Link: link, TargetType: t.TargetType, TargetID: t.TargetID})
	}
	core.WriteJSON(w, 201, core.Object{
		"comment":          core.Object{"id": cid, "user": a.userBrief(u.ID), "body": in.Body, "mentions": mentioned, "created_at": ts},
		"ignored_mentions": ignored,
	})
}
