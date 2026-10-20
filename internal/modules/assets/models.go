package assets

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
)

func modelMigration(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE shop_models(
 id INTEGER PRIMARY KEY,shop_id INTEGER NOT NULL REFERENCES shops(id),name TEXT NOT NULL,description TEXT NOT NULL DEFAULT '',archived INTEGER NOT NULL DEFAULT 0,
 cover_reference_id INTEGER REFERENCES model_references(id),created_by INTEGER NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
 CREATE INDEX shop_models_shop ON shop_models(shop_id,archived);
 CREATE TABLE model_references(id INTEGER PRIMARY KEY,model_id INTEGER NOT NULL REFERENCES shop_models(id),body_part TEXT NOT NULL,source TEXT NOT NULL,file TEXT NOT NULL,orig_name TEXT NOT NULL,width INTEGER NOT NULL,height INTEGER NOT NULL,op TEXT NOT NULL DEFAULT '{}',created_by INTEGER NOT NULL REFERENCES users(id),created_at TEXT NOT NULL);
 CREATE INDEX model_references_model ON model_references(model_id);
 CREATE TRIGGER model_identity_immutable BEFORE UPDATE OF id,shop_id ON shop_models WHEN NEW.id<>OLD.id OR NEW.shop_id<>OLD.shop_id BEGIN SELECT RAISE(ABORT,'model identity is immutable'); END;`)
	return err
}

type ShopModel struct {
	ID               int64            `json:"id"`
	ShopID           int64            `json:"shop_id"`
	Name             string           `json:"name"`
	Description      string           `json:"description"`
	Archived         bool             `json:"archived"`
	CoverReferenceID *int64           `json:"cover_reference_id"`
	CoverURL         string           `json:"cover_url"`
	References       []ModelReference `json:"references"`
	CreatedBy        int64            `json:"created_by"`
	CreatedAt        string           `json:"created_at"`
	UpdatedAt        string           `json:"updated_at"`
}
type ModelReference struct {
	ID        int64  `json:"id"`
	ModelID   int64  `json:"model_id"`
	BodyPart  string `json:"body_part"`
	Source    string `json:"source"`
	File      string `json:"-"`
	OrigName  string `json:"orig_name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Op        Object `json:"op"`
	CreatedBy int64  `json:"-"`
	CreatedAt string `json:"created_at"`
	URL       string `json:"url"`
	ThumbURL  string `json:"thumb_url"`
}

const modelColumns = "id,shop_id,name,description,archived,cover_reference_id,created_by,created_at,updated_at"
const referenceColumns = "id,model_id,body_part,source,file,orig_name,width,height,op,created_by,created_at"

func scanModel(s core.Scanner) (m ShopModel, err error) {
	err = s.Scan(&m.ID, &m.ShopID, &m.Name, &m.Description, &m.Archived, &m.CoverReferenceID, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt)
	m.References = []ModelReference{}
	if m.CoverReferenceID != nil {
		m.CoverURL = referenceURL(m.ID, *m.CoverReferenceID)
	}
	return
}
func referenceURL(mid, rid int64) string {
	return fmt.Sprintf("/api/models/%d/references/%d/file", mid, rid)
}
func scanReference(s core.Scanner) (v ModelReference, err error) {
	var op string
	err = s.Scan(&v.ID, &v.ModelID, &v.BodyPart, &v.Source, &v.File, &v.OrigName, &v.Width, &v.Height, &op, &v.CreatedBy, &v.CreatedAt)
	v.Op = core.JSONObj(op)
	v.URL = referenceURL(v.ModelID, v.ID)
	v.ThumbURL = v.URL + "?w=240"
	return
}
func (a *Module) shopAllowed(u core.User, sid int64, active bool) bool {
	if u.Disabled || u.Role.Scope == "shares" || !u.Has(core.ModelView) || !a.CanShop(u, sid) {
		return false
	}
	var archived bool
	return a.DB.QueryRow("SELECT archived FROM shops WHERE id=?", sid).Scan(&archived) == nil && (!active || !archived)
}
func (a *Module) getModel(u core.User, id int64, active bool) (ShopModel, error) {
	m, err := scanModel(a.DB.QueryRow("SELECT "+modelColumns+" FROM shop_models WHERE id=?", id))
	if err != nil || !a.shopAllowed(u, m.ShopID, active) {
		return m, fmt.Errorf("模特不存在")
	}
	if active && m.Archived {
		return m, fmt.Errorf("模特已归档")
	}
	return m, nil
}
func (a *Module) modelWithReferences(m ShopModel) (ShopModel, error) {
	rows, err := a.DB.Query("SELECT "+referenceColumns+" FROM model_references WHERE model_id=? AND deleted_at='' ORDER BY id", m.ID)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		v, e := scanReference(rows)
		if e != nil {
			return m, e
		}
		m.References = append(m.References, v)
	}
	return m, rows.Err()
}
func (a *Module) reference(mid, rid int64) (ModelReference, error) {
	return scanReference(a.DB.QueryRow("SELECT "+referenceColumns+" FROM model_references WHERE model_id=? AND id=? AND deleted_at=''", mid, rid))
}
func (a *Module) modelRoutes(r *core.Router) {
	r.Handle("GET /api/models", core.ModelView, a.modelsList)
	r.Handle("POST /api/models", core.ModelManage, a.modelCreate)
	r.Handle("GET /api/models/{id}", core.ModelView, a.modelGet)
	r.Handle("PATCH /api/models/{id}", core.ModelManage, a.modelPatch)
	r.Handle("POST /api/models/{id}/references", core.ModelManage, a.modelUpload)
	r.Handle("DELETE /api/models/{id}/references/{ref}", core.ModelManage, a.modelDeleteReference)
	r.Handle("GET /api/models/{id}/references/{ref}/file", core.ModelView, a.modelFile)
	r.Handle("POST /api/models/{id}/generate", core.ModelManage, a.modelGenerate)
	r.Handle("POST /api/models/{id}/references/confirm", core.ModelManage, a.modelConfirm)
	r.Handle("POST /api/tryon/analyze", core.AssetUpload, a.tryonAnalyze)
	r.Handle("POST /api/tryon/generate", core.AssetUpload, a.tryonGenerate)
	r.Handle("GET /api/tryon/state", core.AssetUpload, a.tryonState)
}
func bounded(s string, n int) bool { return utf8.ValidString(s) && utf8.RuneCountInString(s) <= n }
func modelText(name, desc string) bool {
	return strings.TrimSpace(name) != "" && bounded(name, 80) && bounded(desc, 4000)
}
func (a *Module) modelResponse(w http.ResponseWriter, status int, m ShopModel, ref *ModelReference) {
	m, err := a.modelWithReferences(m)
	if err != nil {
		fail(w, 500, "读取模特参考失败")
		return
	}
	out := Object{"model": m}
	if ref != nil {
		out["reference"] = ref
	}
	writeJSON(w, status, out)
}
func (a *Module) modelsList(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.Role.Scope == "shares" {
		fail(w, 403, "外部协作者不能访问店铺模特")
		return
	}
	archived := r.URL.Query().Get("archived")
	if archived == "" {
		archived = "0"
	}
	if archived != "0" && archived != "1" {
		fail(w, 400, "archived 只能是 0 或 1")
		return
	}
	sid := r.URL.Query().Get("shop_id")
	q := r.URL.Query().Get("q")
	if !bounded(q, 4000) {
		fail(w, 400, "搜索文本过长")
		return
	}
	rows, err := a.DB.Query("SELECT "+modelColumns+" FROM shop_models WHERE archived=? AND (?='' OR shop_id=?) AND (name LIKE ? OR description LIKE ?) ORDER BY id DESC", archived, sid, sid, "%"+q+"%", "%"+q+"%")
	if err != nil {
		fail(w, 500, "读取模特失败")
		return
	}
	all := []ShopModel{}
	for rows.Next() {
		m, e := scanModel(rows)
		if e != nil {
			err = e
			break
		}
		all = append(all, m)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		fail(w, 500, "读取模特失败")
		return
	}
	items := []ShopModel{}
	for _, m := range all {
		if a.shopAllowed(u, m.ShopID, false) {
			items = append(items, m)
		}
	}
	total := len(items)
	offset, size := core.Page(r)
	offset = min(offset, total)
	items = items[offset:min(offset+size, total)]
	for i, m := range items {
		items[i], err = a.modelWithReferences(m)
		if err != nil {
			fail(w, 500, "读取模特失败")
			return
		}
	}
	writeJSON(w, 200, Object{"items": items, "total": total})
}
func (a *Module) modelGet(w http.ResponseWriter, r *http.Request) {
	m, e := a.getModel(currentUser(r), idParam(r, "id"), false)
	if e != nil {
		fail(w, 404, e.Error())
		return
	}
	a.modelResponse(w, 200, m, nil)
}
func (a *Module) modelCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ShopID      int64  `json:"shop_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !modelDecode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !modelText(in.Name, in.Description) {
		fail(w, 400, "名称必填且不超过80字，描述不超过4000字")
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	u := currentUser(r)
	if !a.shopAllowed(u, in.ShopID, true) {
		fail(w, 404, "店铺不存在或已归档")
		return
	}
	t := now()
	res, e := a.DB.Exec("INSERT INTO shop_models(shop_id,name,description,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?)", in.ShopID, in.Name, in.Description, u.ID, t, t)
	if e != nil {
		fail(w, 500, "创建模特失败")
		return
	}
	id, _ := res.LastInsertId()
	a.Audit(r, "assets.model.create", "model", id, nil)
	m, _ := a.getModel(u, id, false)
	a.modelResponse(w, 201, m, nil)
}
func modelDecode(w http.ResponseWriter, r *http.Request, out any) bool {
	var raw Object
	if !decode(w, r, &raw) {
		return false
	}
	if e := strictDecode(raw, out); e != nil {
		fail(w, 400, "请求字段或类型不正确")
		return false
	}
	return true
}
func (a *Module) modelPatch(w http.ResponseWriter, r *http.Request) {
	var raw Object
	if !decode(w, r, &raw) {
		return
	}
	var in struct {
		Name             *string `json:"name"`
		Description      *string `json:"description"`
		Archived         *bool   `json:"archived"`
		CoverReferenceID *int64  `json:"cover_reference_id"`
	}
	if strictDecode(raw, &in) != nil {
		fail(w, 400, "请求字段或类型不正确，店铺不可修改")
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	m, e := a.getModel(currentUser(r), idParam(r, "id"), false)
	if e != nil {
		fail(w, 404, e.Error())
		return
	}
	if in.Name != nil {
		m.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		m.Description = *in.Description
	}
	if !modelText(m.Name, m.Description) {
		fail(w, 400, "名称或描述长度不正确")
		return
	}
	if in.Archived != nil {
		m.Archived = *in.Archived
	}
	if _, ok := raw["cover_reference_id"]; ok {
		m.CoverReferenceID = in.CoverReferenceID
		if in.CoverReferenceID != nil {
			if _, e = a.reference(m.ID, *in.CoverReferenceID); e != nil {
				fail(w, 400, "封面必须是该模特的参考图")
				return
			}
		}
	}
	m.UpdatedAt = now()
	_, e = a.DB.Exec("UPDATE shop_models SET name=?,description=?,archived=?,cover_reference_id=?,updated_at=? WHERE id=?", m.Name, m.Description, m.Archived, m.CoverReferenceID, m.UpdatedAt, m.ID)
	if e != nil {
		fail(w, 500, "修改模特失败")
		return
	}
	m.CoverURL = ""
	if m.CoverReferenceID != nil {
		m.CoverURL = referenceURL(m.ID, *m.CoverReferenceID)
	}
	a.Audit(r, "assets.model.update", "model", m.ID, raw)
	a.modelResponse(w, 200, m, nil)
}
