package scenes

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
	if err = root.MkdirAll("scenes", 0700); err != nil {
		return nil, err
	}
	return root.OpenRoot("scenes")
}
func imageInfo(data []byte) (string, int, int, error) {
	if len(data) > maxReferenceBytes {
		return "", 0, 0, invalid(413, "参考图不能超过 20 MB")
	}
	info, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || !contains([]string{"jpeg", "png", "webp"}, format) {
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
func (module *Module) writeReference(sceneID int64, data []byte) (Reference, error) {
	ext, width, height, err := imageInfo(data)
	if err != nil {
		return Reference{}, err
	}
	root, err := module.fileRoot()
	if err != nil {
		return Reference{}, err
	}
	defer root.Close()
	dir := strconv.FormatInt(sceneID, 10)
	if err = root.MkdirAll(dir, 0700); err != nil {
		return Reference{}, err
	}
	token := make([]byte, 16)
	if _, err = rand.Read(token); err != nil {
		return Reference{}, err
	}
	name := fmt.Sprintf("%s/%x.%s", dir, token, ext)
	if err = root.WriteFile(name, data, 0600); err != nil {
		_ = root.Remove(name)
		return Reference{}, err
	}
	path, err := filepath.Abs(filepath.Join(module.Cfg.DataDir, "scenes", filepath.FromSlash(name)))
	return Reference{File: path, Width: width, Height: height}, err
}
func (module *Module) relativeFile(file string) (string, error) {
	base, err := filepath.Abs(filepath.Join(module.Cfg.DataDir, "scenes"))
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
	scene, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxReferenceBytes+(1<<20))
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		core.Fail(writer, 413, "参考图不能超过 20 MB，或上传格式不正确")
		return
	}
	defer request.MultipartForm.RemoveAll()
	use := request.FormValue("use")
	if !contains([]string{"still", "wear"}, use) {
		core.Fail(writer, 400, "参考图用途请选择静物或上身")
		return
	}
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
	ref, err := module.writeReference(scene.ID, data)
	if err != nil {
		fail(writer, err)
		return
	}
	ref.Use = use
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
	var count int
	if err = tx.QueryRowContext(request.Context(), "SELECT count(*) FROM scene_refs WHERE scene_id=? AND use=?", scene.ID, use).Scan(&count); err != nil {
		fail(writer, err)
		return
	}
	if count >= 4 {
		core.Fail(writer, 409, "每种用途最多放 4 张参考图")
		return
	}
	result, err := tx.ExecContext(request.Context(), "INSERT INTO scene_refs(scene_id,use,file,width,height,created_at) VALUES(?,?,?,?,?,?)", scene.ID, use, ref.File, ref.Width, ref.Height, core.Now())
	if err == nil {
		ref.ID, err = result.LastInsertId()
	}
	if err == nil {
		ref.URL = refURL(scene.ID, ref.ID)
		err = core.AuditTx(tx, request, "scene.ref_add", "scene", scene.ID, ref)
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
func (module *Module) reference(request *http.Request, sceneID int64) (Reference, error) {
	var ref Reference
	err := module.DB.QueryRowContext(request.Context(), "SELECT id,use,file,width,height FROM scene_refs WHERE scene_id=? AND id=?", sceneID, core.ID(request, "ref")).Scan(&ref.ID, &ref.Use, &ref.File, &ref.Width, &ref.Height)
	if err != nil {
		return ref, invalid(404, "参考图不存在")
	}
	ref.URL = refURL(sceneID, ref.ID)
	return ref, nil
}
func (module *Module) refDelete(writer http.ResponseWriter, request *http.Request) {
	scene, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	ref, err := module.reference(request, scene.ID)
	if err != nil {
		fail(writer, err)
		return
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(request.Context(), "DELETE FROM scene_refs WHERE id=? AND scene_id=?", ref.ID, scene.ID)
	if err == nil {
		err = core.AuditTx(tx, request, "scene.ref_delete", "scene", scene.ID, ref)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(writer, err)
		return
	}
	if err = module.removeFile(ref.File); err != nil {
		fail(writer, err)
		return
	}
	core.WriteJSON(writer, 200, core.Object{"ok": true})
}
func (module *Module) refFile(writer http.ResponseWriter, request *http.Request) {
	scene, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	ref, err := module.reference(request, scene.ID)
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
