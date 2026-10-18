package kb

import (
	"database/sql"
	"net/http"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
)

type Folder struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	ParentID     *int64    `json:"parent_id"`
	Sort         int       `json:"sort"`
	ArticleCount int       `json:"article_count"`
	Children     []*Folder `json:"children"`
}

func (a *Module) listFolders(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(`SELECT f.id,f.name,f.parent_id,f.sort,(SELECT count(*) FROM kb_articles a WHERE a.folder_id=f.id AND a.deleted_at IS NULL) FROM kb_folders f ORDER BY f.sort,f.id`)
	if err != nil {
		replyError(w, err)
		return
	}
	all := []*Folder{}
	for rows.Next() {
		f := &Folder{Children: []*Folder{}}
		if err = rows.Scan(&f.ID, &f.Name, &f.ParentID, &f.Sort, &f.ArticleCount); err != nil {
			break
		}
		all = append(all, f)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		replyError(w, err)
		return
	}
	byID := map[int64]*Folder{}
	for _, f := range all {
		byID[f.ID] = f
	}
	tree := []*Folder{}
	for _, f := range all {
		if f.ParentID == nil {
			tree = append(tree, f)
		} else if p := byID[*f.ParentID]; p != nil {
			p.Children = append(p.Children, f)
		}
	}
	core.WriteJSON(w, 200, tree)
}

func (a *Module) saveFolder(w http.ResponseWriter, r *http.Request) {
	// The field map distinguishes an omitted parent from an explicit null.
	var body struct {
		Name     *string `json:"name"`
		ParentID *int64  `json:"parent_id"`
		Sort     *int    `json:"sort"`
	}
	var raw map[string]any
	if !core.Decode(w, r, &raw) {
		return
	}
	// Re-decode with typed fields to reject incorrect field types.
	if err := decodeMap(raw, &body); err != nil {
		core.Fail(w, 400, "目录字段类型不正确")
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
	f := Folder{Children: []*Folder{}}
	create := r.Method == "POST"
	if !create {
		f.ID = core.ID(r, "id")
		if err = tx.QueryRow("SELECT name,parent_id,sort FROM kb_folders WHERE id=?", f.ID).Scan(&f.Name, &f.ParentID, &f.Sort); err != nil {
			replyError(w, err)
			return
		}
	}
	if body.Name != nil {
		f.Name = strings.TrimSpace(*body.Name)
	}
	if _, ok := raw["parent_id"]; ok {
		f.ParentID = body.ParentID
	}
	if body.Sort != nil {
		f.Sort = *body.Sort
	}
	if utf8.RuneCountInString(f.Name) < 1 || utf8.RuneCountInString(f.Name) > 30 {
		core.Fail(w, 400, "目录名称须为 1–30 字")
		return
	}
	if f.ParentID != nil {
		var parent sql.NullInt64
		if *f.ParentID == f.ID || tx.QueryRow("SELECT parent_id FROM kb_folders WHERE id=?", *f.ParentID).Scan(&parent) != nil || parent.Valid {
			core.Fail(w, 400, "目录最多两层，父目录须为现有根目录")
			return
		}
		var n int
		if err = tx.QueryRow("SELECT count(*) FROM kb_folders WHERE parent_id=?", f.ID).Scan(&n); err != nil {
			replyError(w, err)
			return
		}
		if n > 0 {
			core.Fail(w, 400, "含子目录的目录不能移到第二层")
			return
		}
	}
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM kb_folders WHERE COALESCE(parent_id,0)=COALESCE(?,0) AND name=? AND id<>?", f.ParentID, f.Name, f.ID).Scan(&n); err != nil {
		replyError(w, err)
		return
	}
	if n > 0 {
		core.Fail(w, 409, "同一父目录下不能重名")
		return
	}
	if create {
		var res sql.Result
		res, err = tx.Exec("INSERT INTO kb_folders(name,parent_id,sort,created_at,updated_at) VALUES(?,?,?,?,?)", f.Name, f.ParentID, f.Sort, core.Now(), core.Now())
		if err == nil {
			f.ID, err = res.LastInsertId()
		}
	} else {
		_, err = tx.Exec("UPDATE kb_folders SET name=?,parent_id=?,sort=?,updated_at=? WHERE id=?", f.Name, f.ParentID, f.Sort, core.Now(), f.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		replyError(w, err)
		return
	}
	a.Audit(r, "kb.folder.save", "kb_folder", f.ID, core.Object{"name": f.Name})
	status := 200
	if create {
		status = 201
	}
	core.WriteJSON(w, status, f)
}

func (a *Module) deleteFolder(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	tx, err := a.DB.Begin()
	if err != nil {
		replyError(w, err)
		return
	}
	defer tx.Rollback()
	id := core.ID(r, "id")
	var n int
	if err = tx.QueryRow("SELECT id FROM kb_folders WHERE id=?", id).Scan(&n); err != nil {
		replyError(w, err)
		return
	}
	// Include trash and historical versions so restore/revert always has a valid folder.
	err = tx.QueryRow(`SELECT (SELECT count(*) FROM kb_folders WHERE parent_id=?)+(SELECT count(*) FROM kb_articles WHERE folder_id=?)+(SELECT count(*) FROM kb_versions WHERE folder_id=?)`, id, id, id).Scan(&n)
	if err != nil {
		replyError(w, err)
		return
	}
	if n > 0 {
		core.Fail(w, 409, "目录下有文章、历史版本或子目录，不能删除")
		return
	}
	_, err = tx.Exec("DELETE FROM kb_folders WHERE id=?", id)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		replyError(w, err)
		return
	}
	a.Audit(r, "kb.folder.delete", "kb_folder", id, nil)
	core.WriteJSON(w, 200, core.Object{"ok": true})
}
