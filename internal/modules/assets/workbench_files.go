package assets

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

func workbenchFilename(v WorkbenchItem) string {
	source := map[string]string{"original": "实拍", "ai_enhanced": "AI增强", "ai_edited": "AI改图", "ai_generated": "AI生成"}[v.Source]
	if source == "" {
		source = v.Source
	}
	return strings.TrimSuffix(safeName(v.OrigName), filepath.Ext(v.OrigName)) + "_" + source + filepath.Ext(v.File)
}
func (a *Module) workbenchFile(w http.ResponseWriter, r *http.Request) {
	v, err := a.workbenchItem(currentUser(r).ID, idParam(r, "id"))
	if err != nil || !a.modelWorkbenchVisible(currentUser(r), v) {
		fail(w, 404, "处理台图片不存在")
		return
	}
	download := r.URL.Query().Get("download") == "1"
	if download {
		if !currentUser(r).Has(core.ExportRun) {
			fail(w, 403, "没有下载权限")
			return
		}
	}
	root, err := a.workbenchRoot(v.UserID)
	if err != nil {
		fail(w, 500, "打开处理台目录失败")
		return
	}
	defer root.Close()
	var b []byte
	name := v.OrigName
	if !download && r.URL.Query().Get("w") != "" {
		n, _ := strconv.Atoi(r.URL.Query().Get("w"))
		if n != 240 && n != 480 && n != 960 {
			fail(w, 400, "缩略图宽度只能是 240、480 或 960")
			return
		}
		b, err = a.workbenchThumbnail(root, v, n)
		name = "thumb.jpg"
	} else {
		b, err = workbenchRead(root, v.File)
	}
	if err != nil {
		status := 400
		if os.IsNotExist(err) {
			status = 404
		}
		fail(w, status, "读取处理台图片失败")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if modelWorkbenchItem(v) {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	w.Header().Add("Vary", "Cookie")
	if download {
		name = workbenchFilename(v)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		w.Header().Set("Cache-Control", "private, no-store")
		a.Audit(r, "assets.workbench.download", "workbench_item", v.ID, nil)
	}
	w.Header().Set("Content-Type", http.DetectContentType(b))
	http.ServeContent(w, r, name, parseWorkbenchTime(v.CreatedAt), bytes.NewReader(b))
}
func (a *Module) workbenchDownload(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.IDs) < 1 || len(in.IDs) > 50 {
		fail(w, 400, "一次下载需要 1–50 张图片")
		return
	}
	items := []WorkbenchItem{}
	seen := map[int64]bool{}
	for _, id := range in.IDs {
		if id <= 0 || seen[id] {
			fail(w, 400, "图片编号不正确或重复")
			return
		}
		seen[id] = true
		v, err := a.workbenchItem(currentUser(r).ID, id)
		if err != nil || !a.modelWorkbenchVisible(currentUser(r), v) {
			fail(w, 404, "处理台图片不存在")
			return
		}
		items = append(items, v)
	}
	if !currentUser(r).Has(core.ExportRun) {
		fail(w, 403, "没有下载权限")
		return
	}
	root, err := a.workbenchRoot(currentUser(r).ID)
	if err != nil {
		fail(w, 500, "打开处理台目录失败")
		return
	}
	defer root.Close()
	zipName := "download_" + rand.Text() + ".zip"
	release := a.holdWorkbenchFile(currentUser(r).ID, zipName)
	defer release()
	f, err := root.OpenFile(zipName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fail(w, 500, "创建下载包失败")
		return
	}
	defer root.Remove(zipName)
	defer f.Close()
	z := zip.NewWriter(f)
	names := map[string]bool{}
	for _, v := range items {
		b, e := workbenchRead(root, v.File)
		if e != nil {
			z.Close()
			status := 400
			if os.IsNotExist(e) {
				status = 404
			}
			fail(w, status, "读取下载图片失败")
			return
		}
		name := workbenchFilename(v)
		if names[name] {
			name = strings.TrimSuffix(name, filepath.Ext(name)) + fmt.Sprintf("_%d", v.ID) + filepath.Ext(name)
		}
		names[name] = true
		entry, e := z.Create(name)
		if e == nil {
			_, e = entry.Write(b)
		}
		if e != nil {
			z.Close()
			fail(w, 500, "打包图片失败")
			return
		}
	}
	if err = z.Close(); err != nil {
		fail(w, 500, "完成下载包失败")
		return
	}
	a.Audit(r, "assets.workbench.download", "workbench", 0, Object{"ids": in.IDs})
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="workbench.zip"`)
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		fail(w, 500, "读取下载包失败")
		return
	}
	http.ServeContent(w, r, "workbench.zip", parseWorkbenchTime(""), f)
}
