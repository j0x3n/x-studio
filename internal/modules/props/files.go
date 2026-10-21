package props

import (
	"bytes"
	"crypto/rand"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"xstudio/internal/core"
)

const maxReferenceBytes = 20 << 20

func (module *Module) fileRoot() (*os.Root, error) {
	root, err := os.OpenRoot(module.Cfg.DataDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err = root.MkdirAll("props", 0700); err != nil {
		return nil, err
	}
	return root.OpenRoot("props")
}
func imageInfo(data []byte) (string, int, int, error) {
	if len(data) > maxReferenceBytes {
		return "", 0, 0, invalid(413, "参考图不能超过 20 MB")
	}
	info, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") {
		return "", 0, 0, invalid(400, "参考图只支持 jpg、png、webp 图片")
	}
	if info.Width <= 0 || info.Height <= 0 || int64(info.Width)*int64(info.Height) > 100_000_000 {
		return "", 0, 0, invalid(400, "参考图尺寸过大或不正确")
	}
	if format == "jpeg" {
		format = "jpg"
	}
	return format, info.Width, info.Height, nil
}
func (module *Module) writeReference(propID int64, data []byte) (Ref, error) {
	ext, width, height, err := imageInfo(data)
	if err != nil {
		return Ref{}, err
	}
	root, err := module.fileRoot()
	if err != nil {
		return Ref{}, err
	}
	defer root.Close()
	dir := strconv.FormatInt(propID, 10)
	if err = root.MkdirAll(dir, 0700); err != nil {
		return Ref{}, err
	}
	token := make([]byte, 16)
	if _, err = rand.Read(token); err != nil {
		return Ref{}, err
	}
	name := fmt.Sprintf("%s/%x.%s", dir, token, ext)
	if err = root.WriteFile(name, data, 0600); err != nil {
		_ = root.Remove(name)
		return Ref{}, err
	}
	path, err := filepath.Abs(filepath.Join(module.Cfg.DataDir, "props", filepath.FromSlash(name)))
	return Ref{File: path, Width: width, Height: height}, err
}
func (module *Module) relativeFile(file string) (string, error) {
	base, err := filepath.Abs(filepath.Join(module.Cfg.DataDir, "props"))
	if err != nil {
		return "", err
	}
	return filepath.Rel(base, file)
}
func (module *Module) removeFile(file string) error {
	name, err := module.relativeFile(file)
	if err != nil {
		return err
	}
	root, err := module.fileRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	err = root.Remove(name)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func (module *Module) upload(writer http.ResponseWriter, request *http.Request) {
	prop, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	if prop.Archived {
		core.Fail(writer, 409, "道具已归档")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxReferenceBytes+(1<<20))
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		core.Fail(writer, 413, "参考图不能超过 20 MB，或上传格式不正确")
		return
	}
	defer request.MultipartForm.RemoveAll()
	view := request.FormValue("view")
	if view == "" {
		view = "main"
	}
	if !propViews[view] {
		core.Fail(writer, 400, "参考图角度不正确")
		return
	}
	lock := request.FormValue("lock") == "true" || request.FormValue("lock") == "1"
	if len(request.MultipartForm.File["file"]) != 1 {
		core.Fail(writer, 400, "一次只能上传一张参考图")
		return
	}
	file, _, err := request.FormFile("file")
	if err != nil {
		core.Fail(writer, 400, "请选择参考图")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxReferenceBytes+1))
	if err != nil {
		fail(writer, err)
		return
	}
	ref, err := module.writeReference(prop.ID, data)
	if err != nil {
		fail(writer, err)
		return
	}
	ref.View = view
	ref.Source = "upload"
	ref.Status = "candidate"
	committed := false
	defer func() {
		if !committed {
			_ = module.removeFile(ref.File)
		}
	}()
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(request.Context(), "INSERT INTO prop_refs(prop_id,view,source,status,file,width,height,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?)", prop.ID, view, "upload", "candidate", ref.File, ref.Width, ref.Height, core.CurrentUser(request).ID, core.Now())
	if err == nil {
		ref.ID, err = result.LastInsertId()
	}
	if err == nil {
		ref.URL = refURL(prop.ID, ref.ID)
		err = core.AuditTx(tx, request, "prop.ref_add", "prop", prop.ID, core.Object{"ref_id": ref.ID, "view": view, "lock": lock})
	}
	if err == nil && lock {
		err = module.promote(tx, request, prop.ID, ref)
		ref.Status = "locked"
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(writer, err)
		return
	}
	committed = true
	core.WriteJSON(writer, 201, core.Object{"ref": ref})
}
func (module *Module) reference(request *http.Request, propID int64) (Ref, error) {
	var ref Ref
	err := module.DB.QueryRowContext(request.Context(), "SELECT id,view,source,status,file,width,height,prompt,model,created_at FROM prop_refs WHERE prop_id=? AND id=?", propID, core.ID(request, "ref")).Scan(&ref.ID, &ref.View, &ref.Source, &ref.Status, &ref.File, &ref.Width, &ref.Height, &ref.Prompt, &ref.Model, &ref.CreatedAt)
	if err != nil {
		return ref, invalid(404, "参考图不存在")
	}
	ref.URL = refURL(propID, ref.ID)
	return ref, nil
}
func (module *Module) refFile(writer http.ResponseWriter, request *http.Request) {
	prop, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	ref, err := module.reference(request, prop.ID)
	if err != nil {
		fail(writer, err)
		return
	}
	name, err := module.relativeFile(ref.File)
	if err != nil {
		fail(writer, err)
		return
	}
	root, err := module.fileRoot()
	if err != nil {
		fail(writer, err)
		return
	}
	defer root.Close()
	file, err := root.Open(name)
	if err != nil {
		core.Fail(writer, 404, "参考图文件不存在")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fail(writer, err)
		return
	}
	http.ServeContent(writer, request, filepath.Base(name), info.ModTime(), file)
}
func (module *Module) readDataFile(path string) ([]byte, error) {
	base, err := filepath.Abs(module.Cfg.DataDir)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, invalid(400, "参考图路径不正确")
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxReferenceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReferenceBytes {
		return nil, invalid(413, "参考图不能超过 20 MB")
	}
	return data, nil
}
