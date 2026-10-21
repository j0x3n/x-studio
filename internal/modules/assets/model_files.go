package assets

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/imaging"
)

func (a *Module) modelRoot(mid int64) (*os.Root, error) {
	root, e := os.OpenRoot(a.Cfg.DataDir)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	dir := fmt.Sprintf("models/%d", mid)
	if e = root.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	return root.OpenRoot(dir)
}
func insertReference(tx *sql.Tx, v ModelReference) (ModelReference, error) {
	res, e := tx.Exec("INSERT INTO model_references(model_id,body_part,source,file,orig_name,width,height,op,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", v.ModelID, v.BodyPart, v.Source, v.File, v.OrigName, v.Width, v.Height, js(v.Op), v.CreatedBy, v.CreatedAt)
	if e != nil {
		return v, e
	}
	v.ID, e = res.LastInsertId()
	v.URL = referenceURL(v.ModelID, v.ID)
	v.ThumbURL = v.URL + "?w=240"
	return v, e
}
func (a *Module) modelUpload(w http.ResponseWriter, r *http.Request) {
	// Parse and validate the entire multipart before publishing a single immutable reference.
	r.Body = http.MaxBytesReader(w, r.Body, maxPhotoBytes+(1<<20))
	mr, e := r.MultipartReader()
	if e != nil {
		fail(w, 400, "请使用 multipart 的 files 字段上传")
		return
	}
	var data []byte
	name, part := "", ""
	count := 0
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fail(w, 413, "上传数据过大或格式不正确")
			return
		}
		switch p.FormName() {
		case "files":
			count++
			if count > 1 || p.FileName() == "" {
				p.Close()
				fail(w, 400, "一次只能上传一张参考图")
				return
			}
			name = safeName(p.FileName())
			data, err = io.ReadAll(io.LimitReader(p, maxPhotoBytes+1))
		case "body_part":
			var b []byte
			b, err = io.ReadAll(io.LimitReader(p, 100))
			part = string(b)
		default:
			p.Close()
			fail(w, 400, "上传字段不正确")
			return
		}
		p.Close()
		if err != nil {
			fail(w, 400, "读取参考图失败")
			return
		}
	}
	if count != 1 || !validBodyPart(part) {
		fail(w, 400, "请选择一张图片和有效的身体部位")
		return
	}
	if int64(len(data)) > maxPhotoBytes {
		fail(w, 413, "单张照片不能超过25 MB")
		return
	}
	if _, _, e = imageConfig(data); e != nil {
		fail(w, 400, e.Error())
		return
	}
	im, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		fail(w, 400, "图片损坏，无法解码")
		return
	}
	normalized := imaging.Geometry(im, imaging.ReadExif(data).Orientation, 0, nil)
	data, e = encodeWorkbench(normalized, "png")
	if e != nil {
		fail(w, 400, "处理参考图失败")
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	u, e := a.User(currentUser(r).ID)
	if e != nil || !u.Has(core.ModelManage) {
		fail(w, 403, "没有维护模特权限")
		return
	}
	m, e := a.getModel(u, idParam(r, "id"), true)
	if e != nil {
		fail(w, 404, e.Error())
		return
	}
	v := ModelReference{ModelID: m.ID, BodyPart: part, Source: "original", OrigName: name, Width: normalized.Bounds().Dx(), Height: normalized.Bounds().Dy(), Op: Object{}, CreatedBy: u.ID, CreatedAt: now()}
	v, m, e = a.persistReference(r, m, v, data, nil, false)
	if e != nil {
		fail(w, 500, "保存模特参考失败")
		return
	}
	a.modelResponse(w, 201, m, &v)
}

// Called with Core.Mu held; file creation and candidate confirmation are one publication.
func (a *Module) persistReference(r *http.Request, m ShopModel, v ModelReference, data []byte, item *WorkbenchItem, setCover bool) (ModelReference, ShopModel, error) {
	root, e := a.modelRoot(m.ID)
	if e != nil {
		return v, m, e
	}
	defer root.Close()
	v.File = rand.Text() + ".png"
	committed := false
	defer func() {
		if !committed {
			_ = root.Remove(v.File)
		}
	}()
	tx, e := a.DB.Begin()
	if e != nil {
		return v, m, e
	}
	defer tx.Rollback()
	v, e = insertReference(tx, v)
	if e != nil {
		return v, m, e
	}
	if item != nil {
		item.Op["confirmed_reference_id"] = v.ID
		if _, e = tx.Exec("UPDATE workbench_items SET op=? WHERE id=? AND user_id=?", js(item.Op), item.ID, item.UserID); e != nil {
			return v, m, e
		}
	}
	if setCover {
		m.CoverReferenceID = &v.ID
		m.CoverURL = v.URL
	}
	m.UpdatedAt = now()
	if _, e = tx.Exec("UPDATE shop_models SET cover_reference_id=?,updated_at=? WHERE id=?", m.CoverReferenceID, m.UpdatedAt, m.ID); e != nil {
		return v, m, e
	}
	action := "assets.model.reference.upload"
	if item != nil {
		action = "assets.model.reference.confirm"
	}
	if e = core.AuditTx(tx, r, action, "model", m.ID, Object{"reference_id": v.ID}); e != nil {
		return v, m, e
	}
	f, e := root.OpenFile(v.File, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return v, m, e
	}
	_, e = f.Write(data)
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return v, m, e
	}
	if e = tx.Commit(); e != nil {
		return v, m, e
	}
	committed = true
	return v, m, nil
}
func (a *Module) modelConfirm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ItemID   int64 `json:"item_id"`
		SetCover bool  `json:"set_cover"`
	}
	if !modelDecode(w, r, &in) {
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	u, e := a.User(currentUser(r).ID)
	if e != nil || !u.Has(core.ModelManage) || !u.Has(core.AssetUpload) {
		fail(w, 403, "没有维护模特或工作台权限")
		return
	}
	m, e := a.getModel(u, idParam(r, "id"), true)
	if e != nil {
		fail(w, 404, e.Error())
		return
	}
	item, e := a.workbenchItem(u.ID, in.ItemID)
	if e != nil || item.Kind != "result" || item.Op["type"] != "model_candidate" || opID(item.Op, "model_id") != m.ID || opID(item.Op, "shop_id") != m.ShopID || !a.modelWorkbenchVisible(u, item) {
		fail(w, 404, "模特候选不存在")
		return
	}
	if rid := opID(item.Op, "confirmed_reference_id"); rid > 0 {
		v, err := a.reference(m.ID, rid)
		if err != nil {
			fail(w, 404, "已确认参考不存在")
			return
		}
		a.Audit(r, "assets.model.reference.confirm", "model", m.ID, Object{"reference_id": rid, "idempotent": true})
		a.modelResponse(w, 201, m, &v)
		return
	}
	part, _ := item.Op["body_part"].(string)
	if !validBodyPart(part) {
		fail(w, 400, "候选身体部位不正确")
		return
	}
	root, e := a.workbenchRoot(u.ID)
	if e != nil {
		fail(w, 500, "读取候选失败")
		return
	}
	defer root.Close()
	data, e := workbenchRead(root, item.File)
	if e != nil {
		fail(w, 404, "候选文件不存在")
		return
	}
	cfg, format, e := imageConfig(data)
	if e != nil || format != "png" {
		fail(w, 400, "候选图片无效")
		return
	}
	v := ModelReference{ModelID: m.ID, BodyPart: part, Source: "ai_generated", OrigName: item.OrigName, Width: cfg.Width, Height: cfg.Height, Op: core.JSONObj(js(item.Op)), CreatedBy: u.ID, CreatedAt: now()}
	v, m, e = a.persistReference(r, m, v, data, &item, in.SetCover)
	if e != nil {
		fail(w, 500, "确认模特参考失败")
		return
	}
	a.modelResponse(w, 201, m, &v)
}
func (a *Module) modelDeleteReference(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	u, err := a.User(currentUser(r).ID)
	if err != nil || !u.Has(core.ModelManage) {
		fail(w, 403, "没有维护模特权限")
		return
	}
	m, err := a.getModel(u, idParam(r, "id"), false)
	if err != nil {
		fail(w, 404, "模特不存在")
		return
	}
	v, err := a.reference(m.ID, idParam(r, "ref"))
	if err != nil {
		fail(w, 404, "参考图不存在")
		return
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "删除参考图失败")
		return
	}
	defer tx.Rollback()
	var used bool
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM jobs WHERE status IN ('queued','running') AND kind IN ('assets.model_generate','assets.tryon') AND json_extract(params,'$.model_id')=? AND (json_extract(params,'$.reference_id')=? OR json_extract(params,'$.base_reference_id')=?))`, m.ID, v.ID, v.ID).Scan(&used); err != nil {
		fail(w, 500, "读取生成任务失败")
		return
	}
	if used {
		fail(w, 409, "参考图正在用于生成，请等任务完成后删除")
		return
	}
	err = func() error {
		m.UpdatedAt = now()
		if _, err := tx.Exec("UPDATE model_references SET deleted_at=? WHERE id=?", m.UpdatedAt, v.ID); err != nil {
			return err
		}
		if m.CoverReferenceID != nil && *m.CoverReferenceID == v.ID {
			var cover sql.NullInt64
			if err := tx.QueryRow("SELECT MIN(id) FROM model_references WHERE model_id=? AND deleted_at=''", m.ID).Scan(&cover); err != nil {
				return err
			}
			m.CoverReferenceID, m.CoverURL = nil, ""
			if cover.Valid {
				m.CoverReferenceID = &cover.Int64
				m.CoverURL = referenceURL(m.ID, cover.Int64)
			}
		}
		if _, err := tx.Exec("UPDATE shop_models SET cover_reference_id=?,updated_at=? WHERE id=?", m.CoverReferenceID, m.UpdatedAt, m.ID); err != nil {
			return err
		}
		// A surviving candidate can be confirmed again as a new reference with a new ID.
		if _, err := tx.Exec(`UPDATE workbench_items SET op=json_remove(op,'$.confirmed_reference_id') WHERE json_extract(op,'$.type')='model_candidate' AND json_extract(op,'$.model_id')=? AND json_extract(op,'$.confirmed_reference_id')=?`, m.ID, v.ID); err != nil {
			return err
		}
		if err := core.AuditTx(tx, r, "assets.model.reference.delete", "model", m.ID, Object{"reference_id": v.ID}); err != nil {
			return err
		}
		return tx.Commit()
	}()
	if err != nil {
		fail(w, 500, "删除参考图失败")
		return
	}
	// Keep a tombstone so historical reference IDs can never point to another image.
	root, err := a.modelRoot(m.ID)
	if err == nil {
		err = root.Remove(v.File)
		root.Close()
	}
	if err != nil && !os.IsNotExist(err) {
		log.Printf("清理已删除模特参考图 %d: %v", v.ID, err)
	}
	a.modelResponse(w, 200, m, nil)
}

func (a *Module) removeModelFiles(mid int64, files []string) {
	root, err := a.modelRoot(mid)
	if err != nil {
		log.Printf("清理模特 %d 的参考图失败：%v", mid, err)
		return
	}
	for _, name := range files {
		if name == "" {
			continue
		}
		if err = root.Remove(name); err != nil && !os.IsNotExist(err) {
			log.Printf("清理模特 %d 的参考图 %s 失败：%v", mid, name, err)
		}
	}
	root.Close()
	parent, err := os.OpenRoot(a.Cfg.DataDir)
	if err != nil {
		log.Printf("清理模特 %d 的目录失败：%v", mid, err)
		return
	}
	defer parent.Close()
	if err = parent.Remove(fmt.Sprintf("models/%d", mid)); err != nil && !os.IsNotExist(err) {
		log.Printf("清理模特 %d 的目录失败：%v", mid, err)
	}
}
func (a *Module) modelFile(w http.ResponseWriter, r *http.Request) {
	m, e := a.getModel(currentUser(r), idParam(r, "id"), false)
	if e != nil {
		fail(w, 404, "模特不存在")
		return
	}
	v, e := a.reference(m.ID, idParam(r, "ref"))
	if e != nil {
		fail(w, 404, "参考图不存在")
		return
	}
	root, e := a.modelRoot(m.ID)
	if e != nil {
		fail(w, 404, "参考图不存在")
		return
	}
	defer root.Close()
	b, e := workbenchRead(root, v.File)
	if e != nil {
		fail(w, 404, "参考图不存在")
		return
	}
	if s := r.URL.Query().Get("w"); s != "" {
		n, _ := strconv.Atoi(s)
		if n != 240 && n != 480 && n != 960 {
			fail(w, 400, "缩略图宽度只能是240、480或960")
			return
		}
		im, _, err := image.Decode(bytes.NewReader(b))
		if err != nil {
			fail(w, 400, "参考图损坏")
			return
		}
		b, e = encodeWorkbench(imaging.Resize(im, n), "jpg")
		if e != nil {
			fail(w, 500, "生成缩略图失败")
			return
		}
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", "Cookie")
	w.Header().Set("Content-Type", http.DetectContentType(b))
	http.ServeContent(w, r, strings.TrimSpace(v.OrigName), parseWorkbenchTime(v.CreatedAt), bytes.NewReader(b))
}
