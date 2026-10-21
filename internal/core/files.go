package core

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
)

func SniffVideo(b []byte) (ext, contentType string, ok bool) {
	if len(b) >= 4 && bytes.Equal(b[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}) {
		return "webm", "video/webm", true
	}
	if len(b) >= 8 && bytes.Equal(b[4:8], []byte("ftyp")) {
		ext, contentType = "mp4", "video/mp4"
		if len(b) >= 12 {
			brand := string(b[8:12])
			if brand == "qt  " || brand == "mov " {
				ext, contentType = "mov", "video/quicktime"
			}
		}
		return ext, contentType, true
	}
	return "", "", false
}

func ServeRoot(w http.ResponseWriter, r *http.Request, root, name string) {
	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:") {
		Fail(w, 400, "文件路径不合法")
		return
	}
	rt, e := os.OpenRoot(root)
	if e != nil {
		Fail(w, 404, "文件不存在")
		return
	}
	defer rt.Close()
	f, e := rt.Open(name)
	if e != nil {
		Fail(w, 404, "文件不存在")
		return
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		Fail(w, 404, "文件不存在")
		return
	}
	head := make([]byte, 32)
	n, _ := f.Read(head)
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		Fail(w, 500, "读取文件失败")
		return
	}
	if _, ct, ok := SniffVideo(head[:n]); ok {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}
