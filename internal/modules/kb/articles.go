package kb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
)

type Named struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type Link struct {
	Type  string `json:"type"`
	ID    int64  `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
}
type Article struct {
	ID        int64    `json:"id"`
	FolderID  int64    `json:"folder_id"`
	Folder    Named    `json:"folder"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Summary   string   `json:"summary"`
	Tags      []string `json:"tags"`
	Pinned    bool     `json:"pinned"`
	Version   int      `json:"version"`
	CreatedBy int64    `json:"created_by"`
	UpdatedBy Named    `json:"updated_by"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	DeletedAt *string  `json:"deleted_at"`
	Links     []Link   `json:"links"`
}

const articleColumns = `a.id,a.folder_id,f.name,a.title,a.body,a.summary,a.tags,a.pinned,a.version,a.created_by,a.updated_by,COALESCE(NULLIF(u.display_name,''),u.username,''),a.created_at,a.updated_at,a.deleted_at`
const articleFrom = ` FROM kb_articles a JOIN kb_folders f ON f.id=a.folder_id LEFT JOIN users u ON u.id=a.updated_by `

type scanner interface{ Scan(...any) error }

func scanArticle(row scanner) (Article, error) {
	a := Article{Tags: []string{}, Links: []Link{}}
	var tags string
	err := row.Scan(&a.ID, &a.FolderID, &a.Folder.Name, &a.Title, &a.Body, &a.Summary, &tags, &a.Pinned, &a.Version, &a.CreatedBy, &a.UpdatedBy.ID, &a.UpdatedBy.Name, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt)
	a.Folder.ID = a.FolderID
	if err == nil {
		err = json.Unmarshal([]byte(tags), &a.Tags)
	}
	return a, err
}
func readArticle(q dbx, id int64) (Article, error) {
	return scanArticle(q.QueryRow("SELECT "+articleColumns+articleFrom+"WHERE a.id=? AND a.deleted_at IS NULL", id))
}
func decodeMap(raw any, out any) error {
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

type articleInput struct {
	FolderID    *int64    `json:"folder_id"`
	Title       *string   `json:"title"`
	Body        *string   `json:"body"`
	Summary     *string   `json:"summary"`
	Tags        *[]string `json:"tags"`
	Pinned      *bool     `json:"pinned"`
	Links       *[]Link   `json:"links"`
	Note        string    `json:"note"`
	BaseVersion *int      `json:"base_version"`
}

func (in articleInput) apply(a *Article) {
	if in.FolderID != nil {
		a.FolderID = *in.FolderID
	}
	if in.Title != nil {
		a.Title = strings.TrimSpace(*in.Title)
	}
	if in.Body != nil {
		a.Body = *in.Body
	}
	if in.Summary != nil {
		a.Summary = *in.Summary
	}
	if in.Tags != nil {
		a.Tags = *in.Tags
	}
	if in.Pinned != nil {
		a.Pinned = *in.Pinned
	}
	if in.Links != nil {
		a.Links = *in.Links
	}
}
func validateArticle(q dbx, a Article, note string) error {
	if strings.TrimSpace(a.Title) == "" || utf8.RuneCountInString(a.Title) > 100 {
		return bad(400, "标题须为 1–100 字")
	}
	if utf8.RuneCountInString(a.Body) > 100000 || utf8.RuneCountInString(a.Summary) > 200 || utf8.RuneCountInString(note) > 100 {
		return bad(400, "正文、摘要或版本备注过长")
	}
	if len(a.Tags) > 10 {
		return bad(400, "最多 10 个标签")
	}
	seen := map[string]bool{}
	for _, tag := range a.Tags {
		if strings.TrimSpace(tag) == "" || utf8.RuneCountInString(tag) > 20 || seen[tag] {
			return bad(400, "标签须为 1–20 字且不重复")
		}
		seen[tag] = true
	}
	var n int
	if err := q.QueryRow("SELECT count(*) FROM kb_folders WHERE id=?", a.FolderID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return bad(400, "目录不存在")
	}
	return nil
}

// This is CanProduct's scope check using the transaction's connection. Calling
// Core.CanProduct here would wait forever with SetMaxOpenConns(1).
func canProduct(q dbx, u core.User, id int64) (bool, error) {
	var n int
	var err error
	if u.Role.Key == "admin" {
		err = q.QueryRow("SELECT count(*) FROM products WHERE id=?", id).Scan(&n)
	} else if u.Role.Scope == "shares" {
		err = q.QueryRow("SELECT count(*) FROM shares WHERE user_id=? AND product_id=?", u.ID, id).Scan(&n)
	} else {
		err = q.QueryRow(`SELECT count(*) FROM products p WHERE p.id=? AND (p.created_by=? OR EXISTS(SELECT 1 FROM listings l JOIN user_shops us ON us.shop_id=l.shop_id WHERE l.product_id=p.id AND us.user_id=?))`, id, u.ID, u.ID).Scan(&n)
	}
	return n > 0, err
}
func validateLinks(q dbx, u core.User, links []Link) error {
	seen := map[string]bool{}
	for _, l := range links {
		key := fmt.Sprintf("%s/%d", l.Type, l.ID)
		if seen[key] {
			return bad(400, "关联对象不能重复")
		}
		seen[key] = true
		var n int
		var err error
		switch l.Type {
		case "category":
			err = q.QueryRow("SELECT count(*) FROM categories WHERE id=?", l.ID).Scan(&n)
		case "product":
			var yes bool
			yes, err = canProduct(q, u, l.ID)
			if yes {
				n = 1
			}
		case "item":
			err = q.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='inv_items'").Scan(&n)
			if err != nil {
				return err
			}
			if n == 0 {
				return bad(400, "库存模块还没上线")
			}
			err = q.QueryRow("SELECT count(*) FROM inv_items WHERE id=?", l.ID).Scan(&n)
		default:
			return bad(400, "关联类型不正确")
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return bad(400, "关联对象不存在或无权查看")
		}
	}
	return nil
}
func replaceLinks(tx *sql.Tx, id int64, links []Link) error {
	if _, err := tx.Exec("DELETE FROM kb_links WHERE article_id=?", id); err != nil {
		return err
	}
	for _, l := range links {
		if _, err := tx.Exec("INSERT INTO kb_links(article_id,target_type,target_id,created_at) VALUES(?,?,?,?)", id, l.Type, l.ID, core.Now()); err != nil {
			return err
		}
	}
	return nil
}
func insertVersion(tx *sql.Tx, a Article, note string) error {
	_, err := tx.Exec("INSERT INTO kb_versions(article_id,version,title,body,folder_id,tags,note,user_id,created_at) VALUES(?,?,?,?,?,?,?,?,?)", a.ID, a.Version, a.Title, a.Body, a.FolderID, core.JS(a.Tags), note, a.UpdatedBy.ID, a.UpdatedAt)
	return err
}
func (a *Module) createArticle(w http.ResponseWriter, r *http.Request) {
	var in articleInput
	if !core.Decode(w, r, &in) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	tx, err := a.DB.Begin()
	if err != nil {
		replyError(w, err)
		return
	}
	defer tx.Rollback()
	v := Article{Tags: []string{}, Links: []Link{}, Version: 1, CreatedBy: core.CurrentUser(r).ID, UpdatedBy: Named{ID: core.CurrentUser(r).ID}, CreatedAt: core.Now(), UpdatedAt: core.Now()}
	in.apply(&v)
	if err = validateArticle(tx, v, in.Note); err == nil {
		err = validateLinks(tx, core.CurrentUser(r), v.Links)
	}
	if err != nil {
		replyError(w, err)
		return
	}
	res, err := tx.Exec(`INSERT INTO kb_articles(folder_id,title,body,summary,tags,pinned,created_by,updated_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, v.FolderID, v.Title, v.Body, v.Summary, core.JS(v.Tags), v.Pinned, v.CreatedBy, v.UpdatedBy.ID, v.CreatedAt, v.UpdatedAt)
	if err == nil {
		v.ID, err = res.LastInsertId()
	}
	if err == nil {
		err = insertVersion(tx, v, in.Note)
	}
	if err == nil {
		err = replaceLinks(tx, v.ID, v.Links)
	}
	if err == nil {
		err = attachFiles(tx, v.ID, v.Body)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		replyError(w, err)
		return
	}
	a.Audit(r, "kb.article.create", "kb_article", v.ID, core.Object{"version": v.Version})
	a.writeArticle(w, r, v.ID, 201)
}
func conflict(w http.ResponseWriter, in articleInput, v Article) bool {
	if in.BaseVersion == nil {
		core.Fail(w, 400, "必须提供 base_version")
		return true
	}
	if *in.BaseVersion != v.Version {
		core.WriteJSON(w, 409, core.Object{"error": fmt.Sprintf("这篇文章刚被%s改过", v.UpdatedBy.Name), "version": v.Version, "current_version": v.Version})
		return true
	}
	return false
}
func (a *Module) patchArticle(w http.ResponseWriter, r *http.Request) {
	var in articleInput
	if !core.Decode(w, r, &in) {
		return
	}
	a.saveArticle(w, r, in, false, 0)
}
func (a *Module) revertArticle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version     int  `json:"version"`
		BaseVersion *int `json:"base_version"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if in.Version < 1 {
		core.Fail(w, 400, "版本号不正确")
		return
	}
	a.saveArticle(w, r, articleInput{BaseVersion: in.BaseVersion, Note: fmt.Sprintf("回退到第 %d 版", in.Version)}, true, in.Version)
}
func (a *Module) saveArticle(w http.ResponseWriter, r *http.Request, in articleInput, revert bool, version int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	tx, err := a.DB.Begin()
	if err != nil {
		replyError(w, err)
		return
	}
	defer tx.Rollback()
	old, err := readArticle(tx, core.ID(r, "id"))
	if err != nil {
		replyError(w, err)
		return
	}
	if conflict(w, in, old) {
		return
	}
	v := old
	if revert {
		var tags string
		err = tx.QueryRow("SELECT title,body,folder_id,tags FROM kb_versions WHERE article_id=? AND version=?", v.ID, version).Scan(&v.Title, &v.Body, &v.FolderID, &tags)
		if err == nil {
			err = json.Unmarshal([]byte(tags), &v.Tags)
		}
		if err != nil {
			replyError(w, err)
			return
		}
	} else {
		in.apply(&v)
	}
	if err = validateArticle(tx, v, in.Note); err == nil && in.Links != nil {
		err = validateLinks(tx, core.CurrentUser(r), v.Links)
	}
	if err != nil {
		replyError(w, err)
		return
	}
	changed := revert || old.Title != v.Title || old.Body != v.Body || old.FolderID != v.FolderID || !reflect.DeepEqual(old.Tags, v.Tags)
	if changed {
		v.Version++
	}
	v.UpdatedBy.ID = core.CurrentUser(r).ID
	v.UpdatedAt = core.Now()
	_, err = tx.Exec("UPDATE kb_articles SET folder_id=?,title=?,body=?,summary=?,tags=?,pinned=?,version=?,updated_by=?,updated_at=? WHERE id=?", v.FolderID, v.Title, v.Body, v.Summary, core.JS(v.Tags), v.Pinned, v.Version, v.UpdatedBy.ID, v.UpdatedAt, v.ID)
	if err == nil && changed {
		err = insertVersion(tx, v, in.Note)
	}
	if err == nil && in.Links != nil {
		err = replaceLinks(tx, v.ID, v.Links)
	}
	if err == nil {
		err = attachFiles(tx, v.ID, v.Body)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		replyError(w, err)
		return
	}
	action := "kb.article.update"
	if revert {
		action = "kb.article.revert"
	}
	a.Audit(r, action, "kb_article", v.ID, core.Object{"version": v.Version, "note": in.Note})
	a.writeArticle(w, r, v.ID, 200)
}

func (a *Module) links(id int64, u core.User) ([]Link, error) {
	rows, err := a.DB.Query("SELECT target_type,target_id FROM kb_links WHERE article_id=? ORDER BY target_type,target_id", id)
	if err != nil {
		return nil, err
	}
	links := []Link{}
	for rows.Next() {
		var l Link
		if err = rows.Scan(&l.Type, &l.ID); err != nil {
			break
		}
		links = append(links, l)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range links {
		l := &links[i]
		var e error
		switch l.Type {
		case "category":
			e = a.DB.QueryRow("SELECT name FROM categories WHERE id=?", l.ID).Scan(&l.Label)
			l.Path = "/settings/categories"
		case "product":
			if !a.CanProduct(u, l.ID) {
				l.Label = "无权查看"
				continue
			}
			e = a.DB.QueryRow("SELECT name FROM products WHERE id=?", l.ID).Scan(&l.Label)
			l.Path = fmt.Sprintf("/products/%d", l.ID)
		case "item":
			var n int
			e = a.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='inv_items'").Scan(&n)
			if e == nil && n > 0 {
				e = a.DB.QueryRow("SELECT trim(code || ' ' || name) FROM inv_items WHERE id=?", l.ID).Scan(&l.Label)
			} else if e == nil {
				e = sql.ErrNoRows
			}
			l.Path = fmt.Sprintf("/inventory/items/%d", l.ID)
		}
		if e == sql.ErrNoRows {
			l.Label = "已删除"
			l.Path = ""
		} else if e != nil {
			return nil, e
		}
	}
	return links, nil
}
func (a *Module) getArticle(w http.ResponseWriter, r *http.Request) {
	a.writeArticle(w, r, core.ID(r, "id"), 200)
}
func (a *Module) writeArticle(w http.ResponseWriter, r *http.Request, id int64, status int) {
	v, err := readArticle(a.DB, id)
	if err != nil {
		replyError(w, err)
		return
	}
	v.Links, err = a.links(id, core.CurrentUser(r))
	if err != nil {
		replyError(w, err)
		return
	}
	versions, err := a.versions(id)
	if err != nil {
		replyError(w, err)
		return
	}
	files, err := a.articleFiles(id)
	if err != nil {
		replyError(w, err)
		return
	}
	var out core.Object
	_ = decodeMap(v, &out)
	out["versions"] = versions
	out["files"] = files
	core.WriteJSON(w, status, out)
}
func (a *Module) versions(id int64) ([]core.Object, error) {
	rows, err := a.DB.Query(`SELECT v.version,v.note,v.user_id,COALESCE(NULLIF(u.display_name,''),u.username,''),v.created_at FROM kb_versions v LEFT JOIN users u ON u.id=v.user_id WHERE v.article_id=? ORDER BY v.version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Object{}
	for rows.Next() {
		var version int
		var uid int64
		var note, name, at string
		if err = rows.Scan(&version, &note, &uid, &name, &at); err != nil {
			return nil, err
		}
		out = append(out, core.Object{"version": version, "note": note, "user": Named{uid, name}, "created_at": at})
	}
	return out, rows.Err()
}
func (a *Module) getVersion(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if _, err := readArticle(a.DB, id); err != nil {
		replyError(w, err)
		return
	}
	var title, body, tags, note, at string
	var folder, uid int64
	err := a.DB.QueryRow("SELECT title,body,folder_id,tags,note,user_id,created_at FROM kb_versions WHERE article_id=? AND version=?", id, core.ID(r, "version")).Scan(&title, &body, &folder, &tags, &note, &uid, &at)
	if err != nil {
		replyError(w, err)
		return
	}
	var ts []string
	if err = json.Unmarshal([]byte(tags), &ts); err != nil {
		replyError(w, err)
		return
	}
	core.WriteJSON(w, 200, core.Object{"article_id": id, "version": core.ID(r, "version"), "title": title, "body": body, "folder_id": folder, "tags": ts, "note": note, "user_id": uid, "created_at": at})
}
func (a *Module) deleteArticle(w http.ResponseWriter, r *http.Request)  { a.setDeleted(w, r, true) }
func (a *Module) restoreArticle(w http.ResponseWriter, r *http.Request) { a.setDeleted(w, r, false) }
func (a *Module) setDeleted(w http.ResponseWriter, r *http.Request, deleted bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := core.ID(r, "id")
	var value any
	condition := "IS NOT NULL"
	action := "kb.article.restore"
	if deleted {
		value = core.Now()
		condition = "IS NULL"
		action = "kb.article.delete"
	}
	res, err := a.DB.Exec("UPDATE kb_articles SET deleted_at=?,updated_by=?,updated_at=? WHERE id=? AND deleted_at "+condition, value, core.CurrentUser(r).ID, core.Now(), id)
	if err != nil {
		replyError(w, err)
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		replyError(w, err)
		return
	}
	if n == 0 {
		core.Fail(w, 404, "文章不存在")
		return
	}
	a.Audit(r, action, "kb_article", id, nil)
	core.WriteJSON(w, 200, core.Object{"ok": true})
}
