package assets

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"xstudio/internal/core"
)

func (a *Module) library(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	u := currentUser(r)
	scope, args := productScopeSQL(u)
	query := " FROM assets a JOIN products p ON p.id=a.product_id WHERE " + scope
	archived := q.Get("archived")
	if archived == "" {
		archived = "0"
	}
	if archived != "0" && archived != "1" {
		fail(w, 400, "archived 只能是 0 或 1")
		return
	}
	query += " AND a.archived=?"
	args = append(args, archived)
	switch q.Get("stale") {
	case "", "0":
		query += " AND a.stale=0"
	case "1":
		query += " AND a.stale=1"
	case "all":
	default:
		fail(w, 400, "stale 只能是 0、1 或 all")
		return
	}
	switch q.Get("media") {
	case "":
	case "image", "video":
		query += " AND a.media=?"
		args = append(args, q.Get("media"))
	default:
		fail(w, 400, "media 只能是 image 或 video")
		return
	}
	if values, ok := q["folder_id"]; ok {
		folderID, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || folderID < 0 {
			fail(w, 400, "文件夹编号不正确")
			return
		}
		if folderID == 0 {
			query += " AND a.folder_id IS NULL"
		} else {
			query += " AND a.folder_id=?"
			args = append(args, folderID)
		}
	}
	for _, key := range []string{"product_id", "created_by"} {
		if v := q.Get(key); v != "" {
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil || id < 1 {
				fail(w, 400, "筛选编号不正确")
				return
			}
			query += " AND a." + key + "=?"
			args = append(args, id)
		}
	}
	if v := q.Get("shop_id"); v != "" {
		sid, err := strconv.ParseInt(v, 10, 64)
		if err != nil || sid < 1 {
			fail(w, 400, "店铺编号不正确")
			return
		}
		if u.Role.Scope == "shares" || !a.CanShop(u, sid) {
			core.PageItems(w, r, []Asset{})
			return
		}
		query += " AND EXISTS(SELECT 1 FROM listings l WHERE l.product_id=a.product_id AND l.shop_id=?)"
		args = append(args, sid)
	}
	if v := q.Get("code"); v != "" {
		query += " AND substr(a.code,1,length(?))=?"
		args = append(args, v, v)
	}
	if v := q.Get("kind"); v != "" {
		if v != "photo" && v != "output" {
			fail(w, 400, "素材类型不正确")
			return
		}
		query += " AND a.kind=?"
		args = append(args, v)
	}
	if v := q.Get("source"); v != "" {
		sources := normalizeTags(strings.Split(v, ","))
		marks := []string{}
		for _, source := range sources {
			if !validSource(source) {
				fail(w, 400, "素材来源不正确")
				return
			}
			marks = append(marks, "?")
			args = append(args, source)
		}
		if len(marks) == 0 {
			fail(w, 400, "素材来源不能为空")
			return
		}
		query += " AND coalesce(nullif(a.source_override,''),a.source) IN (" + strings.Join(marks, ",") + ")"
	}
	if v := q.Get("tag"); v != "" {
		for _, tag := range normalizeTags(strings.Split(v, ",")) {
			query += " AND EXISTS(SELECT 1 FROM json_each(a.tags) WHERE value=?)"
			args = append(args, tag)
		}
	}
	if v := q.Get("q"); v != "" {
		query += " AND (instr(a.title,?)>0 OR instr(a.orig_name,?)>0 OR instr(a.note,?)>0 OR EXISTS(SELECT 1 FROM json_each(a.tags) WHERE instr(value,?)>0) OR instr(p.name,?)>0)"
		for i := 0; i < 5; i++ {
			args = append(args, v)
		}
	}
	for _, key := range []string{"from", "to"} {
		if v := q.Get(key); v != "" {
			stamp, day, err := dateFilter(v)
			if err != nil {
				fail(w, 400, err.Error())
				return
			}
			op := ">="
			if key == "to" {
				op = "<="
				if day {
					stamp = stamp.AddDate(0, 0, 1)
					op = "<"
				}
			}
			query += " AND julianday(a.created_at)" + op + "julianday(?)"
			args = append(args, stamp.UTC().Format(time.RFC3339Nano))
		}
	}
	if q.Get("root") == "1" {
		query += " AND a.parent_id IS NULL"
	}
	sort := q.Get("sort")
	if sort != "" && sort != "new" && sort != "old" {
		fail(w, 400, "排序只能是 new 或 old")
		return
	}
	order := " DESC"
	if sort == "old" {
		order = " ASC"
	}
	var total int
	if err := a.DB.QueryRowContext(r.Context(), "SELECT count(*)"+query, args...).Scan(&total); err != nil {
		fail(w, 500, "读取图片库失败")
		return
	}
	offset, size := core.Page(r)
	query += " ORDER BY a.created_at" + order + ",a.id" + order + " LIMIT ? OFFSET ?"
	args = append(args, size, offset)
	visible, err := a.readAssets("SELECT "+strings.ReplaceAll("a."+assetColumns, ",", ",a.")+query, args...)
	if err != nil {
		fail(w, 500, "读取图片库失败")
		return
	}
	chains := map[int64][]Asset{}
	for i := range visible {
		p := &visible[i]
		if _, ok := chains[p.ProductID]; !ok {
			chain, err := a.readAssets("SELECT "+assetColumns+" FROM assets WHERE product_id=? ORDER BY created_at,id", p.ProductID)
			if err != nil {
				fail(w, 500, "读取版本链失败")
				return
			}
			chains[p.ProductID] = chain
		}
		*p = a.decorate(*p, u, chains[p.ProductID])
	}
	writeJSON(w, 200, Object{"items": visible, "total": total})
}
func validSource(s string) bool {
	return s == "original" || s == "ai_enhanced" || s == "ai_edited" || s == "ai_generated"
}
func dateFilter(s string) (time.Time, bool, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, false, nil
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Time{}, false, fmt.Errorf("无法读取上海时区")
	}
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return t, false, fmt.Errorf("日期必须是 YYYY-MM-DD 或 RFC 3339")
	}
	return t, true, nil
}

func (a *Module) detail(w http.ResponseWriter, r *http.Request) {
	p, err := a.asset(idParam(r, "pid"))
	if err != nil || !a.CanProduct(currentUser(r), p.ProductID) {
		fail(w, 404, "素材不存在")
		return
	}
	chain, err := a.readAssets("SELECT "+assetColumns+" FROM assets WHERE product_id=? ORDER BY created_at,id", p.ProductID)
	if err != nil {
		fail(w, 500, "读取版本链失败")
		return
	}
	byID := map[int64]Asset{}
	for _, v := range chain {
		byID[v.ID] = v
	}
	root := rootOf(p, byID)
	versions := []Asset{}
	for _, v := range chain {
		if rootOf(v, byID) == root {
			versions = append(versions, a.decorate(v, currentUser(r), chain))
		}
	}
	// The root is shown first even if imported timestamps are out of order.
	for i, v := range versions {
		if v.ID == root {
			versions = append([]Asset{v}, append(versions[:i], versions[i+1:]...)...)
			break
		}
	}
	used, err := a.usedBy(p, currentUser(r))
	if err != nil {
		fail(w, 500, "读取素材引用失败")
		return
	}
	writeJSON(w, 200, Object{"asset": a.decorate(p, currentUser(r), chain), "versions": versions, "used_by": used})
}
func (a *Module) usedBy(p Asset, u core.User) ([]Object, error) {
	used := []Object{}
	if p.Code == "" {
		return used, nil
	}
	used = append(used, Object{"type": "code", "product_id": p.ProductID, "code": p.Code})
	rows, err := core.QueryObjects(a.DB, "SELECT id,shop_id,content,main FROM listings WHERE product_id=? ORDER BY id", p.ProductID)
	if err != nil {
		return nil, err
	}
	for _, l := range rows {
		if !a.CanListing(u, l["id"].(int64)) {
			continue
		}
		var refs func(any) bool
		refs = func(v any) bool {
			switch x := v.(type) {
			case map[string]any:
				if f, ok := x["f"].(string); ok && (f == p.Code+".jpg" || f == p.Code+"_白底.jpg" || f == p.Code+"_透明.png") {
					return true
				}
				for _, v := range x {
					if refs(v) {
						return true
					}
				}
			case []any:
				for _, v := range x {
					if refs(v) {
						return true
					}
				}
			}
			return false
		}
		if refs(l["content"]) || refs(l["main"]) {
			used = append(used, Object{"type": "listing", "id": l["id"], "shop_id": l["shop_id"]})
		}
	}
	return used, nil
}
func rawFields(in map[string]json.RawMessage) []string {
	out := []string{}
	for k := range in {
		out = append(out, k)
	}
	return out
}
