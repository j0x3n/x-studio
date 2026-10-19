package assets

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"time"
	"xstudio/internal/core"
	"xstudio/internal/imaging"
)

type WorkbenchItem struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"-"`
	Kind         string `json:"kind"`
	File         string `json:"-"`
	OrigName     string `json:"orig_name"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	PHash        string `json:"phash"`
	Source       string `json:"source"`
	AssetID      *int64 `json:"asset_id"`
	InputID      *int64 `json:"input_id"`
	Op           Object `json:"op"`
	SavedAssetID *int64 `json:"saved_asset_id"`
	CreatedAt    string `json:"created_at"`
	URL          string `json:"url"`
	ThumbURL     string `json:"thumb_url"`
	Asset        Object `json:"asset,omitempty"`
}

const workbenchColumns = "id,user_id,kind,file,orig_name,width,height,phash,source,asset_id,input_id,op,saved_asset_id,created_at"

func scanWorkbench(s core.Scanner) (v WorkbenchItem, err error) {
	var op string
	err = s.Scan(&v.ID, &v.UserID, &v.Kind, &v.File, &v.OrigName, &v.Width, &v.Height, &v.PHash, &v.Source, &v.AssetID, &v.InputID, &op, &v.SavedAssetID, &v.CreatedAt)
	v.Op = core.JSONObj(op)
	v.URL = fmt.Sprintf("/api/workbench/items/%d/file", v.ID)
	v.ThumbURL = v.URL + "?w=240"
	return
}
func (a *Module) workbenchItem(uid, id int64) (WorkbenchItem, error) {
	return scanWorkbench(a.DB.QueryRow("SELECT "+workbenchColumns+" FROM workbench_items WHERE id=? AND user_id=?", id, uid))
}
func (a *Module) decorateWorkbench(v WorkbenchItem, u core.User) WorkbenchItem {
	if v.AssetID != nil {
		p, err := a.asset(*v.AssetID)
		if err == nil && a.CanProduct(u, p.ProductID) {
			var name string
			_ = a.DB.QueryRow("SELECT name FROM products WHERE id=?", p.ProductID).Scan(&name)
			v.Asset = Object{"id": p.ID, "product_id": p.ProductID, "product_name": name, "code": p.Code}
		}
	}
	return v
}
func (a *Module) workbenchRoot(uid int64) (*os.Root, error) {
	base, err := os.OpenRoot(a.Cfg.DataDir)
	if err != nil {
		return nil, err
	}
	defer base.Close()
	dir := fmt.Sprintf("workbench/%d", uid)
	if err = base.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return base.OpenRoot(dir)
}
func workbenchRead(root *os.Root, file string) ([]byte, error) {
	if !validAssetFile(file) {
		return nil, fmt.Errorf("素材文件路径不合法")
	}
	f, err := root.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (128<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 128<<20 {
		return nil, fmt.Errorf("读取图片失败或图片过大")
	}
	return b, nil
}
func workbenchLoad(root *os.Root, file string) (*image.NRGBA, imaging.Exif, error) {
	b, err := workbenchRead(root, file)
	if err != nil {
		return nil, imaging.Exif{}, err
	}
	if _, _, err = imageConfig(b); err != nil {
		return nil, imaging.Exif{}, err
	}
	im, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, imaging.Exif{}, fmt.Errorf("图片损坏，无法解码")
	}
	return imaging.Geometry(im, 1, 0, nil), imaging.ReadExif(b), nil
}
func encodeWorkbench(im image.Image, format string) ([]byte, error) {
	var b bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&b, im)
	} else {
		err = jpeg.Encode(&b, im, &jpeg.Options{Quality: 92})
	}
	return b.Bytes(), err
}
func insertWorkbench(tx *sql.Tx, v WorkbenchItem) (WorkbenchItem, error) {
	res, err := tx.Exec("INSERT INTO workbench_items(user_id,kind,file,orig_name,width,height,phash,source,asset_id,input_id,op,saved_asset_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", v.UserID, v.Kind, v.File, v.OrigName, v.Width, v.Height, v.PHash, v.Source, v.AssetID, v.InputID, js(v.Op), v.SavedAssetID, v.CreatedAt)
	if err != nil {
		return v, err
	}
	v.ID, err = res.LastInsertId()
	v.URL = fmt.Sprintf("/api/workbench/items/%d/file", v.ID)
	v.ThumbURL = v.URL + "?w=240"
	return v, err
}
func (a *Module) workbenchRoutes(r *core.Router) {
	r.Handle("POST /api/workbench/inputs", core.AssetUpload, a.workbenchUpload)
	r.Handle("GET /api/workbench/items", core.AssetUpload, a.workbenchList)
	r.Handle("GET /api/workbench/items/{id}", core.AssetUpload, a.workbenchGet)
	r.Handle("DELETE /api/workbench/items/{id}", core.AssetUpload, a.workbenchDelete)
	r.Handle("GET /api/workbench/items/{id}/file", core.AssetUpload, a.workbenchFile)
	r.Handle("POST /api/workbench/process", core.AssetUpload, a.workbenchProcess)
	r.Handle("POST /api/workbench/items/{id}/save", core.AssetUpload, a.workbenchSave)
	r.Handle("POST /api/workbench/download", core.AssetUpload, a.workbenchDownload)
}
func (a *Module) workbenchList(w http.ResponseWriter, r *http.Request) {
	operation := r.URL.Query().Get("operation")
	if operation != "" && operation != "tryon" && operation != "model_candidate" && operation != "process" {
		fail(w, 400, "operation 只能是 tryon、model_candidate 或 process")
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != "input" && kind != "result" {
		fail(w, 400, "kind 只能是 input 或 result")
		return
	}
	items := []WorkbenchItem{}
	cutoff := time.Now().Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	// Filter operation in SQL before each limit. Refill after permission filtering,
	// keeping memory bounded even when many revoked results precede visible ones.
	for offset := 0; len(items) < 200; offset += 200 {
		rows, err := a.DB.Query("SELECT "+workbenchColumns+" FROM workbench_items WHERE user_id=? AND julianday(created_at)>=julianday(?) AND (?='' OR kind=?) AND (?='' OR coalesce(json_extract(op,'$.type'),'process')=?) ORDER BY created_at DESC,id DESC LIMIT 200 OFFSET ?", currentUser(r).ID, cutoff, kind, kind, operation, operation, offset)
		if err != nil {
			fail(w, 500, "读取处理台失败")
			return
		}
		batch := []WorkbenchItem{}
		for rows.Next() {
			v, e := scanWorkbench(rows)
			if e != nil {
				err = e
				break
			}
			batch = append(batch, v)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			fail(w, 500, "读取处理台失败")
			return
		}
		for _, v := range batch {
			if a.modelWorkbenchVisible(currentUser(r), v) {
				items = append(items, a.decorateWorkbench(v, currentUser(r)))
				if len(items) == 200 {
					break
				}
			}
		}
		if len(batch) < 200 {
			break
		}
	}
	writeJSON(w, 200, Object{"items": items})
}
func (a *Module) workbenchGet(w http.ResponseWriter, r *http.Request) {
	v, err := a.workbenchItem(currentUser(r).ID, idParam(r, "id"))
	if err != nil || !a.modelWorkbenchVisible(currentUser(r), v) {
		fail(w, 404, "处理台图片不存在")
		return
	}
	writeJSON(w, 200, Object{"item": a.decorateWorkbench(v, currentUser(r))})
}
func (a *Module) workbenchDelete(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	v, err := a.workbenchItem(currentUser(r).ID, idParam(r, "id"))
	if err != nil {
		fail(w, 404, "处理台图片不存在")
		return
	}
	root, err := a.workbenchRoot(v.UserID)
	if err != nil {
		fail(w, 500, "打开处理台目录失败")
		return
	}
	defer root.Close()
	if !validAssetFile(v.File) {
		fail(w, 400, "素材文件路径不合法")
		return
	}
	err = func() error {
		a.workbenchFileMu.Lock()
		defer a.workbenchFileMu.Unlock()
		if err := root.Remove(v.File); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := removeWorkbenchThumbs(root, v.File); err != nil {
			return err
		}
		_, err := a.DB.Exec("DELETE FROM workbench_items WHERE id=? AND user_id=?", v.ID, v.UserID)
		return err
	}()
	if err != nil {
		fail(w, 500, "删除处理台图片失败")
		return
	}
	a.Audit(r, "assets.workbench.delete", "workbench_item", v.ID, nil)
	writeJSON(w, 200, Object{"ok": true})
}
func (a *Module) workbenchUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		fail(w, 400, "请使用 multipart 的 files 字段上传")
		return
	}
	root, err := a.workbenchRoot(currentUser(r).ID)
	if err != nil {
		fail(w, 500, "打开处理台目录失败")
		return
	}
	defer root.Close()
	if err = root.MkdirAll("inputs", 0700); err != nil {
		fail(w, 500, "创建处理台目录失败")
		return
	}
	items := []WorkbenchItem{}
	temps := []string{}
	releases := []func(){}
	committed := false
	defer func() {
		for _, file := range temps {
			_ = root.Remove(file)
		}
		for _, release := range releases {
			release()
		}
		if !committed {
			for _, v := range items {
				_ = root.Remove(v.File)
			}
		}
	}()
	for {
		part, e := mr.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			fail(w, 413, "上传数据过大或格式不正确")
			return
		}
		if part.FormName() != "files" || part.FileName() == "" {
			part.Close()
			continue
		}
		if len(items) >= 20 {
			part.Close()
			fail(w, 413, "一次最多上传 20 张照片")
			return
		}
		b, e := io.ReadAll(io.LimitReader(part, maxPhotoBytes+1))
		part.Close()
		if e != nil {
			fail(w, 400, "读取上传照片失败")
			return
		}
		if int64(len(b)) > maxPhotoBytes {
			fail(w, 413, "单张照片不能超过 25 MB")
			return
		}
		c, format, e := imageConfig(b)
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
		im, _, e := image.Decode(bytes.NewReader(b))
		if e != nil {
			fail(w, 400, "图片损坏，无法解码")
			return
		}
		if format == "jpeg" {
			format = "jpg"
		}
		ex := imaging.ReadExif(b)
		if ex.Orientation >= 5 && ex.Orientation <= 8 {
			c.Width, c.Height = c.Height, c.Width
		}
		file := fmt.Sprintf("inputs/%s.%s", rand.Text(), format)
		v := WorkbenchItem{UserID: currentUser(r).ID, Kind: "input", File: file, OrigName: safeName(part.FileName()), Width: c.Width, Height: c.Height, PHash: fmt.Sprintf("%016x", imaging.Hash(imaging.Geometry(im, ex.Orientation, 0, nil))), Source: "original", Op: Object{}, CreatedAt: now()}
		items = append(items, v)
		tmp := file + "." + rand.Text() + ".tmp"
		temps = append(temps, tmp)
		releases = append(releases, a.holdWorkbenchFile(v.UserID, tmp))
		if e = root.WriteFile(tmp, b, 0600); e != nil {
			fail(w, 500, "保存上传照片失败")
			return
		}
	}
	if len(items) == 0 {
		fail(w, 400, "没有收到 files 照片")
		return
	}
	// Only database publication and short renames share Core.Mu with cleanup.
	// Slow request reads, image validation and temporary writes are complete.
	err = func() error {
		a.Mu.Lock()
		defer a.Mu.Unlock()
		tx, err := a.DB.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for i, v := range items {
			items[i], err = insertWorkbench(tx, v)
			if err != nil {
				return err
			}
			if err = root.Rename(temps[i], v.File); err != nil {
				return err
			}
		}
		return tx.Commit()
	}()
	if err != nil {
		fail(w, 500, "保存处理台失败")
		return
	}
	committed = true
	a.Audit(r, "assets.workbench.upload", "user", currentUser(r).ID, Object{"count": len(items)})
	writeJSON(w, 201, Object{"items": items})
}
