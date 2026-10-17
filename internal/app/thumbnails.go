package app

import (
	"bytes"
	"crypto/rand"
	"image"
	"image/jpeg"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

func validFilePath(name string) bool {
	return fs.ValidPath(name) && !strings.ContainsAny(name, "\\:")
}

// serveThumbnail returns false only when w is absent, preserving raw downloads.
// Both source and cache accesses stay under the same os.Root, including writes.
func (a *App) serveThumbnail(w http.ResponseWriter, r *http.Request, id int64, name string) bool {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		fail(w, 400, "查询参数不合法")
		return true
	}
	values, present := query["w"]
	if !present {
		return false
	}
	if len(values) != 1 || (values[0] != "240" && values[0] != "480" && values[0] != "960") {
		fail(w, 400, "w 只能是 240、480 或 960")
		return true
	}
	if !validFilePath(name) {
		fail(w, 400, "文件路径不合法")
		return true
	}
	size, _ := strconv.Atoi(values[0])
	root, err := os.OpenRoot(a.dir(id))
	if err != nil {
		fail(w, 404, "文件不存在")
		return true
	}
	defer root.Close()
	source, err := root.Open(name)
	if err != nil {
		fail(w, 404, "文件不存在")
		return true
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		fail(w, 404, "文件不存在")
		return true
	}
	cacheName := path.Join(".thumbs", values[0], name)
	serveCached := func() bool {
		cache, err := root.Open(cacheName)
		if err != nil {
			return false
		}
		defer cache.Close()
		stat, err := cache.Stat()
		if err != nil || !stat.Mode().IsRegular() || stat.ModTime().Before(info.ModTime()) {
			return false
		}
		w.Header().Set("Content-Type", "image/jpeg")
		http.ServeContent(w, r, path.Base(name)+".jpg", stat.ModTime(), cache)
		return true
	}
	if serveCached() {
		return true
	}
	a.thumbMu.Lock()
	defer a.thumbMu.Unlock()
	if serveCached() {
		return true
	}
	const maxThumbnailSource = 64 << 20
	b, err := io.ReadAll(io.LimitReader(source, maxThumbnailSource+1))
	if err != nil {
		fail(w, 500, "读取照片失败")
		return true
	}
	if len(b) > maxThumbnailSource {
		fail(w, 400, "图片过大，无法生成缩略图")
		return true
	}
	if _, _, err = imageConfig(b); err != nil {
		fail(w, 400, "文件不是支持的图片")
		return true
	}
	im, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		fail(w, 400, "图片损坏，无法生成缩略图")
		return true
	}
	// Match the orientation a browser displays for an original JPEG.
	thumb := resize(oriented(im, ReadExif(b).Orientation), size)
	if err = root.MkdirAll(path.Dir(cacheName), 0700); err != nil {
		fail(w, 500, "创建缩略图缓存失败")
		return true
	}
	tempName := path.Join(path.Dir(cacheName), ".tmp-"+rand.Text())
	cache, err := root.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fail(w, 500, "创建缩略图缓存失败")
		return true
	}
	defer root.Remove(tempName)
	err = jpeg.Encode(cache, thumb, &jpeg.Options{Quality: 82})
	closeErr := cache.Close()
	if err != nil || closeErr != nil {
		fail(w, 500, "保存缩略图失败")
		return true
	}
	if err = root.Rename(tempName, cacheName); err != nil {
		fail(w, 500, "保存缩略图缓存失败")
		return true
	}
	// Preserve the source version time so future-dated files also hit the cache.
	if err = root.Chtimes(cacheName, time.Now(), info.ModTime()); err != nil {
		fail(w, 500, "更新缩略图缓存时间失败")
		return true
	}
	if !serveCached() {
		fail(w, 500, "读取缩略图缓存失败")
	}
	return true
}
