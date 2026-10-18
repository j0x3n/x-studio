package kb

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"xstudio/internal/core"
)

const maxImageBytes int64 = 20 << 20
const maxVideoBytes int64 = 200 << 20

type File struct {
	ID        int64  `json:"id"`
	ArticleID *int64 `json:"article_id"`
	Name      string `json:"name"`
	File      string `json:"-"`
	URL       string `json:"url"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	CreatedBy int64  `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

const fileColumns = "id,article_id,name,file,mime,size,width,height,created_by,created_at"

func scanFile(s scanner) (File, error) {
	v := File{}
	err := s.Scan(&v.ID, &v.ArticleID, &v.Name, &v.File, &v.Mime, &v.Size, &v.Width, &v.Height, &v.CreatedBy, &v.CreatedAt)
	v.URL = "/files/kb/" + v.File
	return v, err
}
func (a *Module) articleFiles(id int64) ([]File, error) {
	rows, err := a.DB.Query("SELECT "+fileColumns+" FROM kb_files WHERE article_id=? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		v, e := scanFile(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func fileType(head []byte, name string) (mime, ext string, limit int64, err error) {
	mime = http.DetectContentType(head)
	limit = maxImageBytes
	switch mime {
	case "image/jpeg":
		ext = "jpg"
	case "image/png":
		ext = "png"
	case "image/gif":
		ext = "gif"
	case "image/webp":
		ext = "webp"
	case "video/mp4":
		ext = "mp4"
		limit = maxVideoBytes
	case "video/webm":
		if !strings.Contains(string(head), "webm") {
			return "", "", 0, bad(400, "只接受 WebM 视频")
		}
		ext = "webm"
		limit = maxVideoBytes
	default:
		return "", "", 0, bad(400, "只接受 JPG、PNG、WebP、GIF、MP4、WebM")
	}
	provided := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if provided != ext && !(ext == "jpg" && provided == "jpeg") {
		return "", "", 0, bad(400, "文件扩展名与实际类型不符")
	}
	return
}
func (a *Module) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxVideoBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		core.Fail(w, 413, "上传数据过大或 multipart 格式不正确")
		return
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File["file"]) != 1 {
		core.Fail(w, 400, "请上传一个 file 附件")
		return
	}
	for key, files := range r.MultipartForm.File {
		if key != "file" && len(files) > 0 {
			core.Fail(w, 400, "只接受 file 附件字段")
			return
		}
	}
	header := r.MultipartForm.File["file"][0]
	f, err := header.Open()
	if err != nil {
		replyError(w, err)
		return
	}
	defer f.Close()
	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		core.Fail(w, 400, "读取附件失败")
		return
	}
	head = head[:n]
	name := path.Base(strings.ReplaceAll(header.Filename, `\`, "/"))
	mime, ext, limit, err := fileType(head, name)
	if err != nil {
		replyError(w, err)
		return
	}
	if header.Size > limit {
		core.Fail(w, 413, "图片最多 20 MB，视频最多 200 MB")
		return
	}
	v := File{Name: clip(name, 255), Mime: mime, CreatedBy: core.CurrentUser(r).ID, CreatedAt: core.Now()}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		replyError(w, err)
		return
	}
	if strings.HasPrefix(mime, "image/") {
		cfg, format, e := image.DecodeConfig(f)
		if e != nil || cfg.Width < 1 || cfg.Height < 1 || (format != ext && !(format == "jpeg" && ext == "jpg")) {
			core.Fail(w, 400, "图片损坏或类型不正确")
			return
		}
		v.Width = cfg.Width
		v.Height = cfg.Height
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		replyError(w, err)
		return
	}
	if raw := r.FormValue("article_id"); raw != "" {
		id, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || id < 1 {
			core.Fail(w, 400, "article_id 不正确")
			return
		}
		v.ArticleID = &id
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if v.ArticleID != nil {
		if _, err = readArticle(a.DB, *v.ArticleID); err != nil {
			replyError(w, err)
			return
		}
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		replyError(w, err)
		return
	}
	v.File = time.Now().Format("2006-01") + "/" + hex.EncodeToString(token[:]) + "." + ext
	v.URL = "/files/kb/" + v.File
	root := filepath.Join(a.Cfg.DataDir, "kb")
	dst := filepath.Join(root, filepath.FromSlash(v.File))
	if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		replyError(w, err)
		return
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		replyError(w, err)
		return
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(dst)
		}
	}()
	v.Size, err = io.Copy(out, io.LimitReader(f, limit+1))
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		replyError(w, err)
		return
	}
	if v.Size > limit {
		core.Fail(w, 413, "附件超出大小限制")
		return
	}
	res, err := a.DB.Exec("INSERT INTO kb_files(article_id,name,file,mime,size,width,height,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?)", v.ArticleID, v.Name, v.File, v.Mime, v.Size, v.Width, v.Height, v.CreatedBy, v.CreatedAt)
	if err == nil {
		v.ID, err = res.LastInsertId()
	}
	if err != nil {
		replyError(w, err)
		return
	}
	keep = true
	a.Audit(r, "kb.file.upload", "kb_file", v.ID, core.Object{"name": v.Name, "size": v.Size})
	core.WriteJSON(w, 201, v)
}
func (a *Module) serveFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("path")
	if !fs.ValidPath(name) || strings.ContainsAny(name, `\:`) {
		core.Fail(w, 400, "文件路径不合法")
		return
	}
	var mime string
	if err := a.DB.QueryRow("SELECT mime FROM kb_files WHERE file=?", name).Scan(&mime); err == sql.ErrNoRows {
		core.Fail(w, 404, "文件不存在")
		return
	} else if err != nil {
		replyError(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	core.ServeRoot(w, r, filepath.Join(a.Cfg.DataDir, "kb"), name)
}

var fileReference = regexp.MustCompile(`/files/kb/[0-9]{4}-[0-9]{2}/[a-f0-9]{32}\.(?:jpg|png|webp|gif|mp4|webm)\b`)

func references(body string) map[string]bool {
	out := map[string]bool{}
	for _, url := range fileReference.FindAllString(body, -1) {
		out[strings.TrimPrefix(url, "/files/kb/")] = true
	}
	return out
}
func attachFiles(tx *sql.Tx, id int64, body string) error {
	for file := range references(body) {
		if _, err := tx.Exec("UPDATE kb_files SET article_id=? WHERE file=? AND article_id IS NULL", id, file); err != nil {
			return err
		}
	}
	return nil
}

// Keep references from trash and historical versions too: both can become the
// current article again. The mutex serializes cleanup against upload/save/revert.
func (a *Module) cleanupFiles(now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rows, err := a.DB.QueryContext(a.Context(), "SELECT body FROM kb_articles UNION ALL SELECT body FROM kb_versions")
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for rows.Next() {
		var body string
		if err = rows.Scan(&body); err != nil {
			break
		}
		for file := range references(body) {
			used[file] = true
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = a.DB.QueryContext(a.Context(), "SELECT "+fileColumns+" FROM kb_files WHERE julianday(created_at)<julianday(?)", now.Add(-24*time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	files := []File{}
	for rows.Next() {
		v, e := scanFile(rows)
		if e != nil {
			err = e
			break
		}
		if !used[v.File] {
			files = append(files, v)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	root, err := os.OpenRoot(filepath.Join(a.Cfg.DataDir, "kb"))
	if err != nil {
		return err
	}
	defer root.Close()
	for _, v := range files {
		if a.Context().Err() != nil {
			return a.Context().Err()
		}
		if !fs.ValidPath(v.File) || strings.ContainsAny(v.File, `\:`) {
			return fmt.Errorf("附件路径不合法：%d", v.ID)
		}
		if err = root.Remove(v.File); err != nil && !os.IsNotExist(err) {
			return err
		}
		if _, err = a.DB.ExecContext(a.Context(), "DELETE FROM kb_files WHERE id=?", v.ID); err != nil {
			return err
		}
		a.AuditUser(0, "", "kb.file.cleanup", "kb_file", v.ID, nil)
	}
	return nil
}
