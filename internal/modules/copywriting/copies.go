package copywriting

import (
	"net/http"
	"strings"
	"xstudio/internal/core"
)

type CopyVersion struct {
	ID        int64       `json:"id"`
	ListingID int64       `json:"listing_id"`
	Title     string      `json:"title"`
	Content   core.Object `json:"content"`
	Main      core.Object `json:"main"`
	Meta      core.Object `json:"meta"`
	Source    string      `json:"source"`
	Note      string      `json:"note"`
	CreatedBy int64       `json:"created_by"`
	CreatedAt string      `json:"created_at"`
	AppliedAt string      `json:"applied_at"`
}

func (v CopyVersion) toResponse() core.Object {
	return core.Object{
		"copy":       v,
		"id":         v.ID,
		"listing_id": v.ListingID,
		"title":      v.Title,
		"content":    v.Content,
		"main":       v.Main,
		"meta":       v.Meta,
		"source":     v.Source,
		"note":       v.Note,
		"created_by": v.CreatedBy,
		"created_at": v.CreatedAt,
		"applied_at": v.AppliedAt,
	}
}

const copyColumns = "id,listing_id,title,content,main,meta,source,note,created_by,created_at,applied_at"

func scanCopyVersion(s core.Scanner) (v CopyVersion, e error) {
	var content, main, meta string
	e = s.Scan(&v.ID, &v.ListingID, &v.Title, &content, &main, &meta, &v.Source, &v.Note, &v.CreatedBy, &v.CreatedAt, &v.AppliedAt)
	v.Content = core.JSONObj(content)
	v.Main = core.JSONObj(main)
	v.Meta = core.JSONObj(meta)
	return
}

func canonicalTriple(content, main, meta core.Object) string {
	return core.JS(core.Object{"c": content, "m": main, "l": meta})
}

func (a *Module) syncPipelineVersions() {
	if a.Context().Err() != nil {
		return
	}
	a.syncMu.Lock()
	defer a.syncMu.Unlock()

	rows, e := a.DB.Query(`
		SELECT l.id, l.content, l.main, l.meta, l.updated_at, l.created_by, j.id, j.user_id, j.finished_at
		FROM listings l
		JOIN jobs j ON j.id = (
			SELECT id FROM jobs
			WHERE target_type='listing' AND target_id=l.id AND kind='pipeline' AND status='done' AND instr(steps,'copy')>0
			ORDER BY id DESC LIMIT 1
		)
	`)
	if e != nil {
		return
	}
	type candidate struct {
		listingID, createdBy, jobID, jobUser int64
		content, main, meta                  string
		updatedAt, finishedAt                string
	}
	var list []candidate
	for rows.Next() {
		var c candidate
		if rows.Scan(&c.listingID, &c.content, &c.main, &c.meta, &c.updatedAt, &c.createdBy, &c.jobID, &c.jobUser, &c.finishedAt) == nil {
			list = append(list, c)
		}
	}
	rows.Close()

	for _, c := range list {
		if a.Active("listing", c.listingID) {
			continue
		}
		cObj, mObj, lObj := core.JSONObj(c.content), core.JSONObj(c.main), core.JSONObj(c.meta)
		if len(cObj) == 0 && len(mObj) == 0 && len(lObj) == 0 {
			continue
		}
		latest, err := scanCopyVersion(a.DB.QueryRow("SELECT "+copyColumns+" FROM copy_versions WHERE listing_id=? ORDER BY id DESC LIMIT 1", c.listingID))
		if err == nil {
			vTime := latest.CreatedAt
			if latest.AppliedAt > vTime {
				vTime = latest.AppliedAt
			}
			same := canonicalTriple(latest.Content, latest.Main, latest.Meta) == canonicalTriple(cObj, mObj, lObj)
			if (vTime >= c.updatedAt && vTime >= c.finishedAt) || (vTime >= c.finishedAt && same) || (latest.Source == "pipeline" && same) {
				continue
			}
		}
		title, _ := lObj["title"].(string)
		uid := c.jobUser
		if uid <= 0 {
			uid = c.createdBy
		}
		ts := core.Now()
		if ts < c.updatedAt {
			ts = c.updatedAt
		}
		if ts < c.finishedAt {
			ts = c.finishedAt
		}
		_, _ = a.DB.Exec(
			"INSERT INTO copy_versions(listing_id,title,content,main,meta,source,note,created_by,created_at,applied_at) VALUES(?,?,?,?,?,'pipeline','一键出图自动保存',?,?,?)",
			c.listingID, title, core.JS(cObj), core.JS(mObj), core.JS(lObj), uid, ts, ts,
		)
	}
}

func (a *Module) listCopies(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, false) {
		return
	}
	a.syncPipelineVersions()

	var lContent, lMain, lMeta string
	if e := a.DB.QueryRow("SELECT content,main,meta FROM listings WHERE id=?", id).Scan(&lContent, &lMain, &lMeta); e != nil {
		core.Fail(w, 404, "上架版本不存在")
		return
	}
	curTriple := canonicalTriple(core.JSONObj(lContent), core.JSONObj(lMain), core.JSONObj(lMeta))

	rows, e := a.DB.Query("SELECT "+copyColumns+" FROM copy_versions WHERE listing_id=? ORDER BY id DESC", id)
	if e != nil {
		core.Fail(w, 500, "读取文案版本失败")
		return
	}
	defer rows.Close()

	var current *int64
	items := []core.Object{}
	for rows.Next() {
		v, e := scanCopyVersion(rows)
		if e != nil {
			core.Fail(w, 500, "读取文案版本失败")
			return
		}
		if current == nil && canonicalTriple(v.Content, v.Main, v.Meta) == curTriple {
			cid := v.ID
			current = &cid
		}
		items = append(items, core.Object{
			"id":         v.ID,
			"listing_id": v.ListingID,
			"title":      v.Title,
			"source":     v.Source,
			"note":       v.Note,
			"created_by": v.CreatedBy,
			"created_at": v.CreatedAt,
			"applied_at": v.AppliedAt,
		})
	}
	total := len(items)
	offset, size := core.Page(r)
	if offset > total {
		offset = total
	}
	end := min(offset+size, total)
	core.WriteJSON(w, 200, core.Object{
		"items":   items[offset:end],
		"total":   total,
		"current": current,
	})
}

func (a *Module) getCopy(w http.ResponseWriter, r *http.Request) {
	a.syncPipelineVersions()
	id := core.ID(r, "id")
	v, e := scanCopyVersion(a.DB.QueryRow("SELECT "+copyColumns+" FROM copy_versions WHERE id=?", id))
	if e != nil {
		core.Fail(w, 404, "文案版本不存在")
		return
	}
	if !a.RequireListing(w, r, v.ListingID, false) {
		return
	}
	core.WriteJSON(w, 200, v.toResponse())
}

func (a *Module) createCopy(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, false) {
		return
	}
	var in struct {
		Title   string      `json:"title"`
		Content core.Object `json:"content"`
		Main    core.Object `json:"main"`
		Meta    core.Object `json:"meta"`
		Listing core.Object `json:"listing"`
		Note    string      `json:"note"`
		Source  string      `json:"source"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if in.Meta == nil && in.Listing != nil {
		in.Meta = in.Listing
	}
	if in.Content == nil || in.Main == nil || in.Meta == nil {
		core.Fail(w, 400, "必须提供 content、main、meta 三个 JSON 对象")
		return
	}
	p, e := a.PipelineProduct(id)
	if e != nil {
		core.Fail(w, 404, "上架版本不存在")
		return
	}
	if e := a.checkReferencedPhotosExist(p.ID, in.Content, in.Main); e != nil {
		core.Fail(w, 400, e.Error())
		return
	}
	in.Main = a.cleanMainForProduct(p.ID, in.Main)
	if e := a.annotateAI(p.ID, in.Content, in.Main); e != nil {
		core.Fail(w, 500, "标注 AI 素材失败")
		return
	}
	if e := validateCopy(in.Content, in.Main, in.Meta); e != nil {
		core.Fail(w, 400, e.Error())
		return
	}
	normalizeCopy(in.Content, in.Main, in.Meta)
	in.Content = a.cleanObjectForProduct(p.ID, in.Content)
	if words, e := a.prompt("违禁词.txt"); e == nil || len(p.Category.Banned) > 0 {
		in.Meta["compliance"] = appendCompliance(in.Content, in.Main, in.Meta, core.CategoryBannedLines(words, p.Category))
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title, _ = in.Meta["title"].(string)
	}
	source := "manual"
	if in.Source == "restore" {
		source = "restore"
	}
	ts := core.Now()
	uid := core.CurrentUser(r).ID
	res, e := a.DB.Exec(
		"INSERT INTO copy_versions(listing_id,title,content,main,meta,source,note,created_by,created_at,applied_at) VALUES(?,?,?,?,?,?,?,?,?,'')",
		id, title, core.JS(in.Content), core.JS(in.Main), core.JS(in.Meta), source, in.Note, uid, ts,
	)
	if e != nil {
		core.Fail(w, 500, "保存文案版本失败")
		return
	}
	vid, _ := res.LastInsertId()
	a.Audit(r, "copy.save", "listing", id, core.Object{"copy_id": vid, "title": title, "source": source})
	v, _ := scanCopyVersion(a.DB.QueryRow("SELECT "+copyColumns+" FROM copy_versions WHERE id=?", vid))
	core.WriteJSON(w, 201, v.toResponse())
}

func (a *Module) applyCopy(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	v, e := scanCopyVersion(a.DB.QueryRow("SELECT "+copyColumns+" FROM copy_versions WHERE id=?", id))
	if e != nil {
		core.Fail(w, 404, "文案版本不存在")
		return
	}
	if !a.RequireListing(w, r, v.ListingID, true) {
		return
	}
	p, e := a.PipelineProduct(v.ListingID)
	if e != nil {
		core.Fail(w, 404, "上架版本不存在")
		return
	}
	if a.ActiveProduct(p.ID) {
		core.Fail(w, 409, "此上架版本正在排队或处理中")
		return
	}
	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "应用文案版本失败")
		return
	}
	defer tx.Rollback()
	ts := core.Now()
	if _, e = tx.Exec("UPDATE copy_versions SET applied_at=? WHERE id=?", ts, v.ID); e == nil {
		_, e = tx.Exec("UPDATE listings SET content=?,main=?,meta=?,updated_at=? WHERE id=?", core.JS(v.Content), core.JS(v.Main), core.JS(v.Meta), ts, v.ListingID)
	}
	if e == nil {
		e = core.AuditTx(tx, r, "copy.apply", "listing", v.ListingID, core.Object{"copy_id": v.ID})
	}
	if e != nil || tx.Commit() != nil {
		core.Fail(w, 500, "应用文案版本失败")
		return
	}
	v.AppliedAt = ts
	core.WriteJSON(w, 200, v.toResponse())
}
