package core

import (
	"io/fs"
	"net/http"
	"os"
	"strings"
)

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
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}
