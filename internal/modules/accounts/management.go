package accounts

import (
	"encoding/json"
	"net/http"
	"time"
	"xstudio/internal/core"
)

func (a *Module) shops(w http.ResponseWriter, r *http.Request) {
	items, e := a.visibleShops(core.CurrentUser(r))
	if e != nil {
		core.Fail(w, 500, "读取店铺失败")
		return
	}
	core.PageItems(w, r, items)
}

type shopInput struct {
	Name     string      `json:"name"`
	Platform string      `json:"platform"`
	Language string      `json:"language"`
	Settings core.Object `json:"settings"`
	Archived bool        `json:"archived"`
}

func validShop(s shopInput) bool {
	return len([]rune(s.Name)) > 0 && len([]rune(s.Name)) <= 120 && (s.Platform == "taobao" || s.Platform == "shopee_tw" || s.Platform == "x" || s.Platform == "other") && (s.Language == "zh-CN" || s.Language == "zh-TW" || s.Language == "en")
}
func (a *Module) createShop(w http.ResponseWriter, r *http.Request) {
	var in shopInput
	if !core.Decode(w, r, &in) {
		return
	}
	if !validShop(in) {
		core.Fail(w, 400, "店铺名称、平台或语言不正确")
		return
	}
	if in.Settings == nil {
		in.Settings = core.Object{}
	}
	res, e := a.DB.Exec("INSERT INTO shops(name,platform,language,settings,archived,created_at) VALUES(?,?,?,?,?,?)", in.Name, in.Platform, in.Language, core.JS(in.Settings), in.Archived, core.Now())
	if e != nil {
		core.Fail(w, 500, "创建店铺失败")
		return
	}
	id, _ := res.LastInsertId()
	a.Audit(r, "shop.create", "shop", id, in)
	core.WriteJSON(w, 201, core.Object{"shop": core.Object{"id": id, "name": in.Name, "platform": in.Platform, "language": in.Language, "settings": in.Settings, "archived": in.Archived}})
}
func (a *Module) patchShop(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	items, e := core.QueryObjects(a.DB, "SELECT * FROM shops WHERE id=?", id)
	if e != nil || len(items) == 0 {
		core.Fail(w, 404, "店铺不存在")
		return
	}
	b, _ := json.Marshal(items[0])
	var in shopInput
	_ = json.Unmarshal(b, &in)
	var patch map[string]json.RawMessage
	if !core.Decode(w, r, &patch) {
		return
	}
	for k := range patch {
		if k != "name" && k != "platform" && k != "language" && k != "settings" && k != "archived" {
			core.Fail(w, 400, "不支持的店铺字段")
			return
		}
	}
	b, _ = json.Marshal(patch)
	if json.Unmarshal(b, &in) != nil || !validShop(in) {
		core.Fail(w, 400, "店铺字段不正确")
		return
	}
	if _, e = a.DB.Exec("UPDATE shops SET name=?,platform=?,language=?,settings=?,archived=? WHERE id=?", in.Name, in.Platform, in.Language, core.JS(in.Settings), in.Archived, id); e != nil {
		core.Fail(w, 500, "保存店铺失败")
		return
	}
	a.Audit(r, "shop.update", "shop", id, in)
	core.WriteJSON(w, 200, core.Object{"shop": in})
}
func (a *Module) shares(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !a.RequireProduct(w, r, id, false) {
		return
	}
	items, e := core.QueryObjects(a.DB, "SELECT s.*,u.username,u.display_name FROM shares s JOIN users u ON u.id=s.user_id WHERE product_id=? ORDER BY s.id", id)
	if e != nil {
		core.Fail(w, 500, "读取分享失败")
		return
	}
	core.PageItems(w, r, items)
}
func (a *Module) createShare(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	if !a.RequireProduct(w, r, id, false) {
		return
	}
	var in struct {
		UserID    int64 `json:"user_id"`
		CanUpload bool  `json:"can_upload"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	u, e := a.User(in.UserID)
	if e != nil || u.Disabled || u.Role.Scope != "shares" {
		core.Fail(w, 400, "只能分享给启用的外部协作者")
		return
	}
	_, e = a.DB.Exec("INSERT INTO shares(product_id,user_id,can_upload,created_by,created_at) VALUES(?,?,?,?,?) ON CONFLICT(product_id,user_id) DO UPDATE SET can_upload=excluded.can_upload", id, in.UserID, in.CanUpload, core.CurrentUser(r).ID, core.Now())
	if e != nil {
		core.Fail(w, 500, "保存分享失败")
		return
	}
	items, _ := core.QueryObjects(a.DB, "SELECT * FROM shares WHERE product_id=? AND user_id=?", id, in.UserID)
	a.Audit(r, "share.create", "product", id, in)
	core.WriteJSON(w, 201, core.Object{"share": items[0]})
}
func (a *Module) deleteShare(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	var pid int64
	if a.DB.QueryRow("SELECT product_id FROM shares WHERE id=?", id).Scan(&pid) != nil || !a.CanProduct(core.CurrentUser(r), pid) {
		core.Fail(w, 404, "分享不存在")
		return
	}
	if _, e := a.DB.Exec("DELETE FROM shares WHERE id=?", id); e != nil {
		core.Fail(w, 500, "取消分享失败")
		return
	}
	a.Audit(r, "share.delete", "product", pid, core.Object{"share_id": id})
	core.WriteJSON(w, 200, core.Object{})
}
func (a *Module) quota(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if _, e := a.User(id); e != nil {
		core.Fail(w, 404, "账号不存在")
		return
	}
	var in json.RawMessage
	if !core.Decode(w, r, &in) {
		return
	}
	if e := core.ValidateSetting("quota_default", in); e != nil {
		core.Fail(w, 400, e.Error())
		return
	}
	o := core.JSONObj(string(in))
	if _, e := a.DB.Exec("INSERT INTO quotas VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET month_images=excluded.month_images,month_text=excluded.month_text", id, o["month_images"], o["month_text"]); e != nil {
		core.Fail(w, 500, "保存额度失败")
		return
	}
	a.Audit(r, "quota.update", "user", id, o)
	core.WriteJSON(w, 200, core.Object{"quota": o})
}
func (a *Module) usage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	start, end, e := core.MonthRange(q.Get("month"))
	if e != nil {
		core.Fail(w, 400, e.Error())
		return
	}
	group := q.Get("group")
	if group == "" {
		group = "user"
	}
	if group != "user" && group != "feature" {
		core.Fail(w, 400, "分组只能是 user 或 feature")
		return
	}
	col := "user_id"
	if group == "feature" {
		col = "feature"
	}
	query := "SELECT " + col + ",kind,sum(units) AS units,sum(est_cost) AS est_cost FROM ai_usage WHERE julianday(at)>=julianday(?) AND julianday(at)<julianday(?)"
	args := []any{start, end}
	if r.URL.Path == "/api/usage/me" {
		query += " AND user_id=?"
		args = append(args, core.CurrentUser(r).ID)
	}
	query += " GROUP BY " + col + ",kind ORDER BY " + col + ",kind"
	items, e := core.QueryObjects(a.DB, query, args...)
	if e != nil {
		core.Fail(w, 500, "读取用量失败")
		return
	}
	core.PageItems(w, r, items)
}
func (a *Module) audit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := "SELECT * FROM audit_log WHERE 1=1"
	args := []any{}
	for _, key := range []string{"user_id", "action", "target_type", "target_id"} {
		if v := q.Get(key); v != "" {
			query += " AND " + key + "=?"
			args = append(args, v)
		}
	}
	for _, s := range []struct{ k, op string }{{"from", ">="}, {"to", "<="}} {
		if v := q.Get(s.k); v != "" {
			t, e := time.Parse(time.RFC3339, v)
			if e != nil {
				core.Fail(w, 400, "日志时间必须是 RFC 3339 格式")
				return
			}
			query += " AND julianday(at)" + s.op + "julianday(?)"
			args = append(args, t.UTC().Format(time.RFC3339Nano))
		}
	}
	items, e := core.QueryObjects(a.DB, query+" ORDER BY id DESC", args...)
	if e != nil {
		core.Fail(w, 500, "读取日志失败")
		return
	}
	core.PageItems(w, r, items)
}
func (a *Module) settings(w http.ResponseWriter, r *http.Request) {
	items, e := core.QueryObjects(a.DB, "SELECT key,value FROM settings ORDER BY key")
	if e != nil {
		core.Fail(w, 500, "读取设置失败")
		return
	}
	out := core.Object{}
	for _, v := range items {
		key := v["key"].(string)
		if key == "ai_prices" && core.CurrentUser(r).Role.Key != "admin" {
			continue
		}
		value := core.JSONObj(v["value"].(string))
		if key == "ai_models" {
			for feature, model := range map[string]string{"classify": a.Cfg.VisionModel, "tag": a.Cfg.VisionModel, "copy": a.Cfg.TextModel, "copy_rewrite": a.Cfg.TextModel, "copy_check": a.Cfg.TextModel, "enhance": a.Cfg.ImageModel, "fill": a.Cfg.ImageModel, "upscale": a.Cfg.ImageModel, "process": a.Cfg.ImageModel, "model_create": a.Cfg.ImageModel, "tryon": a.Cfg.ImageModel, "tryon_analyze": a.Cfg.VisionModel, "assistant": a.Cfg.TextModel} {
				if s, _ := value[feature].(string); s == "" {
					value[feature] = a.Model(feature, model)
				}
			}
		}
		out[key] = value
	}
	core.WriteJSON(w, 200, out)
}
func (a *Module) patchSettings(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	var in map[string]json.RawMessage
	if !core.Decode(w, r, &in) {
		return
	}
	for k, v := range in {
		if e := core.ValidateSetting(k, v); e != nil {
			core.Fail(w, 400, e.Error())
			return
		}
	}
	tx, e := a.DB.Begin()
	if e != nil {
		core.Fail(w, 500, "保存设置失败")
		return
	}
	defer tx.Rollback()
	for k, v := range in {
		if _, e = tx.Exec("UPDATE settings SET value=?,updated_by=?,updated_at=? WHERE key=?", string(v), core.CurrentUser(r).ID, core.Now(), k); e != nil {
			core.Fail(w, 500, "保存设置失败")
			return
		}
	}
	if core.AuditTx(tx, r, "settings.update", "settings", 0, core.Object{"fields": keys(in)}) != nil || tx.Commit() != nil {
		core.Fail(w, 500, "保存设置失败")
		return
	}
	a.settings(w, r)
}
