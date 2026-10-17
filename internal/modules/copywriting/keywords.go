package copywriting

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
)

type Keyword struct {
	ID        int64  `json:"id"`
	ShopID    *int64 `json:"shop_id"`
	Word      string `json:"word"`
	Grp       string `json:"grp"`
	Weight    int    `json:"weight"`
	Note      string `json:"note"`
	CreatedBy int64  `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

func (k Keyword) toResponse() core.Object {
	return core.Object{
		"keyword":    k,
		"id":         k.ID,
		"shop_id":    k.ShopID,
		"word":       k.Word,
		"grp":        k.Grp,
		"weight":     k.Weight,
		"note":       k.Note,
		"created_by": k.CreatedBy,
		"created_at": k.CreatedAt,
	}
}

const keywordColumns = "id,shop_id,word,grp,weight,note,created_by,created_at"

func scanKeyword(s core.Scanner) (k Keyword, e error) {
	var sid sql.NullInt64
	e = s.Scan(&k.ID, &sid, &k.Word, &k.Grp, &k.Weight, &k.Note, &k.CreatedBy, &k.CreatedAt)
	if sid.Valid && sid.Int64 > 0 {
		v := sid.Int64
		k.ShopID = &v
	}
	return
}

func (a *Module) listKeywords(w http.ResponseWriter, r *http.Request) {
	u := core.CurrentUser(r)
	q := r.URL.Query()
	query := "SELECT " + keywordColumns + " FROM keywords WHERE 1=1"
	args := []any{}

	if s := strings.TrimSpace(q.Get("shop_id")); s != "" {
		if s == "0" || s == "null" {
			query += " AND shop_id IS NULL"
		} else {
			sid, e := strconv.ParseInt(s, 10, 64)
			if e != nil || sid < 1 || !a.CanShop(u, sid) {
				core.Fail(w, 404, "店铺不存在")
				return
			}
			query += " AND (shop_id IS NULL OR shop_id=?)"
			args = append(args, sid)
		}
	} else if u.Role.Key != "admin" {
		if u.Role.Scope == "shares" {
			core.PageItems(w, r, []Keyword{})
			return
		}
		query += " AND (shop_id IS NULL OR shop_id IN (SELECT shop_id FROM user_shops WHERE user_id=?))"
		args = append(args, u.ID)
	}
	if grp := strings.TrimSpace(q.Get("grp")); grp != "" {
		query += " AND grp=?"
		args = append(args, grp)
	}
	rows, e := a.DB.Query(query+" ORDER BY weight DESC, id ASC", args...)
	if e != nil {
		core.Fail(w, 500, "读取关键词失败")
		return
	}
	defer rows.Close()
	items := []Keyword{}
	for rows.Next() {
		kw, e := scanKeyword(rows)
		if e != nil {
			core.Fail(w, 500, "读取关键词失败")
			return
		}
		items = append(items, kw)
	}
	core.PageItems(w, r, items)
}

func (a *Module) createKeyword(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	var in struct {
		ShopID *int64 `json:"shop_id"`
		Word   string `json:"word"`
		Grp    string `json:"grp"`
		Weight int    `json:"weight"`
		Note   string `json:"note"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	in.Word = strings.TrimSpace(in.Word)
	in.Grp = strings.TrimSpace(in.Grp)
	if in.Word == "" || utf8.RuneCountInString(in.Word) > 80 {
		core.Fail(w, 400, "关键词不能为空且不超过 80 字")
		return
	}
	u := core.CurrentUser(r)
	var shopArg any
	if in.ShopID != nil && *in.ShopID > 0 {
		if !a.CanShop(u, *in.ShopID) {
			core.Fail(w, 404, "店铺不存在")
			return
		}
		shopArg = *in.ShopID
	} else {
		in.ShopID = nil
	}
	ts := core.Now()
	res, e := a.DB.Exec(
		"INSERT INTO keywords(shop_id,word,grp,weight,note,created_by,created_at) VALUES(?,?,?,?,?,?,?)",
		shopArg, in.Word, in.Grp, in.Weight, in.Note, u.ID, ts,
	)
	if e != nil {
		core.Fail(w, 409, "同一店铺下该关键词已存在")
		return
	}
	id, _ := res.LastInsertId()
	kw := Keyword{
		ID:        id,
		ShopID:    in.ShopID,
		Word:      in.Word,
		Grp:       in.Grp,
		Weight:    in.Weight,
		Note:      in.Note,
		CreatedBy: u.ID,
		CreatedAt: ts,
	}
	a.Audit(r, "copy.keyword.create", "keyword", id, kw)
	core.WriteJSON(w, 201, kw.toResponse())
}

func (a *Module) patchKeyword(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	kw, e := scanKeyword(a.DB.QueryRow("SELECT "+keywordColumns+" FROM keywords WHERE id=?", id))
	if e != nil {
		core.Fail(w, 404, "关键词不存在")
		return
	}
	u := core.CurrentUser(r)
	if kw.ShopID != nil && !a.CanShop(u, *kw.ShopID) {
		core.Fail(w, 404, "关键词不存在")
		return
	}
	var in map[string]json.RawMessage
	if !core.Decode(w, r, &in) {
		return
	}
	for k, v := range in {
		var err error
		switch k {
		case "word":
			err = json.Unmarshal(v, &kw.Word)
			kw.Word = strings.TrimSpace(kw.Word)
			if kw.Word == "" || utf8.RuneCountInString(kw.Word) > 80 {
				core.Fail(w, 400, "关键词不能为空且不超过 80 字")
				return
			}
		case "grp":
			err = json.Unmarshal(v, &kw.Grp)
			kw.Grp = strings.TrimSpace(kw.Grp)
		case "weight":
			err = json.Unmarshal(v, &kw.Weight)
		case "note":
			err = json.Unmarshal(v, &kw.Note)
		case "shop_id":
			var sid *int64
			err = json.Unmarshal(v, &sid)
			if err == nil && sid != nil && *sid > 0 {
				if !a.CanShop(u, *sid) {
					core.Fail(w, 404, "店铺不存在")
					return
				}
				kw.ShopID = sid
			} else if err == nil {
				kw.ShopID = nil
			}
		default:
			core.Fail(w, 400, "不支持的关键词字段："+k)
			return
		}
		if err != nil {
			core.Fail(w, 400, "关键词字段类型不正确："+k)
			return
		}
	}
	var shopArg any
	if kw.ShopID != nil && *kw.ShopID > 0 {
		shopArg = *kw.ShopID
	}
	if _, e := a.DB.Exec("UPDATE keywords SET shop_id=?,word=?,grp=?,weight=?,note=? WHERE id=?", shopArg, kw.Word, kw.Grp, kw.Weight, kw.Note, kw.ID); e != nil {
		core.Fail(w, 409, "同一店铺下该关键词已存在")
		return
	}
	a.Audit(r, "copy.keyword.update", "keyword", kw.ID, kw)
	core.WriteJSON(w, 200, kw.toResponse())
}

func (a *Module) deleteKeyword(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	kw, e := scanKeyword(a.DB.QueryRow("SELECT "+keywordColumns+" FROM keywords WHERE id=?", id))
	if e != nil {
		core.Fail(w, 404, "关键词不存在")
		return
	}
	u := core.CurrentUser(r)
	if kw.ShopID != nil && !a.CanShop(u, *kw.ShopID) {
		core.Fail(w, 404, "关键词不存在")
		return
	}
	if _, e := a.DB.Exec("DELETE FROM keywords WHERE id=?", id); e != nil {
		core.Fail(w, 500, "删除关键词失败")
		return
	}
	a.Audit(r, "copy.keyword.delete", "keyword", id, core.Object{"word": kw.Word, "shop_id": kw.ShopID})
	core.WriteJSON(w, 200, core.Object{})
}

func (a *Module) importKeywords(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	var in struct {
		ShopID *int64          `json:"shop_id"`
		Lines  json.RawMessage `json:"lines"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	u := core.CurrentUser(r)
	var shopArg any
	var auditShopID int64
	if in.ShopID != nil && *in.ShopID > 0 {
		if !a.CanShop(u, *in.ShopID) {
			core.Fail(w, 404, "店铺不存在")
			return
		}
		shopArg = *in.ShopID
		auditShopID = *in.ShopID
	} else {
		in.ShopID = nil
	}
	var rawLines []string
	var singleStr string
	if json.Unmarshal(in.Lines, &singleStr) == nil {
		rawLines = strings.Split(singleStr, "\n")
	} else if json.Unmarshal(in.Lines, &rawLines) != nil {
		core.Fail(w, 400, "lines 必须是字符串或字符串数组")
		return
	}

	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "批量导入关键词失败")
		return
	}
	defer tx.Rollback()

	ts := core.Now()
	imported := []Keyword{}
	for _, line := range rawLines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if line == "" || core.HasPrefix(line, "#", "//") {
			continue
		}
		cols := strings.Split(strings.ReplaceAll(line, `\t`, "\t"), "\t")
		word := strings.TrimSpace(cols[0])
		if word == "" || utf8.RuneCountInString(word) > 80 {
			continue
		}
		grp := ""
		if len(cols) >= 2 {
			grp = strings.TrimSpace(cols[1])
		}
		weight := 0
		if len(cols) >= 3 {
			if wVal, err := strconv.Atoi(strings.TrimSpace(cols[2])); err == nil {
				weight = wVal
			} else if fVal, err := strconv.ParseFloat(strings.TrimSpace(cols[2]), 64); err == nil {
				weight = int(fVal)
			}
		}
		note := ""
		if len(cols) >= 4 {
			note = strings.TrimSpace(cols[3])
		}
		var existingID int64
		var err error
		if shopArg == nil {
			err = tx.QueryRow("SELECT id FROM keywords WHERE shop_id IS NULL AND word=?", word).Scan(&existingID)
		} else {
			err = tx.QueryRow("SELECT id FROM keywords WHERE shop_id=? AND word=?", shopArg, word).Scan(&existingID)
		}
		if err == nil && existingID > 0 {
			if _, e := tx.Exec("UPDATE keywords SET grp=?,weight=?,note=? WHERE id=?", grp, weight, note, existingID); e != nil {
				core.Fail(w, 500, "更新关键词失败")
				return
			}
			imported = append(imported, Keyword{ID: existingID, ShopID: in.ShopID, Word: word, Grp: grp, Weight: weight, Note: note, CreatedBy: u.ID, CreatedAt: ts})
		} else {
			res, e := tx.Exec("INSERT INTO keywords(shop_id,word,grp,weight,note,created_by,created_at) VALUES(?,?,?,?,?,?,?)", shopArg, word, grp, weight, note, u.ID, ts)
			if e != nil {
				core.Fail(w, 500, "导入关键词失败")
				return
			}
			kid, _ := res.LastInsertId()
			imported = append(imported, Keyword{ID: kid, ShopID: in.ShopID, Word: word, Grp: grp, Weight: weight, Note: note, CreatedBy: u.ID, CreatedAt: ts})
		}
	}
	if e := core.AuditTx(tx, r, "copy.keyword.import", "shop", auditShopID, core.Object{"count": len(imported)}); e != nil || tx.Commit() != nil {
		core.Fail(w, 500, "保存导入关键词失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"imported": len(imported), "items": imported, "total": len(imported)})
}

func (a *Module) getVoice(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !a.CanShop(core.CurrentUser(r), id) {
		core.Fail(w, 404, "店铺不存在")
		return
	}
	var text, exRaw, updatedAt string
	var updatedBy sql.NullInt64
	e := a.DB.QueryRow("SELECT text,examples,updated_by,updated_at FROM voice_profiles WHERE shop_id=?", id).Scan(&text, &exRaw, &updatedBy, &updatedAt)
	examples := []string{}
	if e == nil {
		_ = json.Unmarshal([]byte(exRaw), &examples)
	}
	if examples == nil {
		examples = []string{}
	}
	var uid any
	if updatedBy.Valid {
		uid = updatedBy.Int64
	}
	voice := core.Object{
		"shop_id":    id,
		"text":       text,
		"examples":   examples,
		"updated_by": uid,
		"updated_at": updatedAt,
	}
	core.WriteJSON(w, 200, core.Object{
		"voice":      voice,
		"shop_id":    id,
		"text":       text,
		"examples":   examples,
		"updated_by": uid,
		"updated_at": updatedAt,
	})
}

func (a *Module) putVoice(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	u := core.CurrentUser(r)
	if !a.CanShop(u, id) {
		core.Fail(w, 404, "店铺不存在")
		return
	}
	var in struct {
		Text     string   `json:"text"`
		Examples []string `json:"examples"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if in.Examples == nil {
		in.Examples = []string{}
	}
	ts := core.Now()
	_, e := a.DB.Exec(
		"INSERT INTO voice_profiles(shop_id,text,examples,updated_by,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(shop_id) DO UPDATE SET text=excluded.text,examples=excluded.examples,updated_by=excluded.updated_by,updated_at=excluded.updated_at",
		id, in.Text, core.JS(in.Examples), u.ID, ts,
	)
	if e != nil {
		core.Fail(w, 500, "保存品牌口吻失败")
		return
	}
	voice := core.Object{
		"shop_id":    id,
		"text":       in.Text,
		"examples":   in.Examples,
		"updated_by": u.ID,
		"updated_at": ts,
	}
	a.Audit(r, "copy.voice.update", "shop", id, voice)
	core.WriteJSON(w, 200, core.Object{
		"voice":      voice,
		"shop_id":    id,
		"text":       in.Text,
		"examples":   in.Examples,
		"updated_by": u.ID,
		"updated_at": ts,
	})
}
