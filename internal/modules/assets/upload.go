package assets

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

const maxPhotoBytes int64 = 25 << 20
const maxPixels int64 = 40_000_000

func safeName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, name)
	r := []rune(name)
	if len(r) > 100 {
		r = r[:100]
	}
	name = strings.Trim(string(r), ". ")
	if name == "" {
		return "照片"
	}
	return name
}
func imageConfig(b []byte) (image.Config, string, error) {
	c, f, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || (f != "jpeg" && f != "png" && f != "webp") {
		return c, "", fmt.Errorf("只接受真实的 JPG、PNG、WebP 图片")
	}
	if c.Width < 1 || c.Height < 1 || int64(c.Width)*int64(c.Height) > maxPixels {
		return c, "", fmt.Errorf("照片像素过大，最多 4000 万像素")
	}
	return c, f, nil
}
func (a *Module) upload(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := idParam(r, "id")
	if _, ok := a.authorizedProduct(w, r, id, true); !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
	mr, e := r.MultipartReader()
	if e != nil {
		fail(w, 400, "请使用 multipart 的 files 字段上传")
		return
	}
	if e = os.MkdirAll(filepath.Join(a.Dir(id), "原图"), 0700); e != nil {
		fail(w, 500, "无法创建原图目录")
		return
	}
	type pending struct {
		p   Photo
		tmp string
	}
	files := []pending{}
	defer func() {
		for _, v := range files {
			_ = os.Remove(v.tmp)
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
			_ = part.Close()
			continue
		}
		if len(files) >= 32 {
			fail(w, 413, "一次最多上传 32 张照片")
			return
		}
		b, e := io.ReadAll(io.LimitReader(part, maxPhotoBytes+1))
		_ = part.Close()
		if e != nil {
			fail(w, 400, "读取上传照片失败")
			return
		}
		if int64(len(b)) > maxPhotoBytes {
			fail(w, 413, "单张照片不能超过 25 MB")
			return
		}
		c, f, e := imageConfig(b)
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
		// Decode as well as sniffing the header: reject truncated or corrupt images.
		if _, _, e = image.Decode(bytes.NewReader(b)); e != nil {
			fail(w, 400, "图片损坏，无法解码")
			return
		}
		tmp, e := os.CreateTemp(filepath.Join(a.Dir(id), "原图"), ".upload-*")
		if e != nil {
			fail(w, 500, "保存上传照片失败")
			return
		}
		e1 := func() error { defer tmp.Close(); _, e := tmp.Write(b); return e }()
		files = append(files, pending{p: Photo{ProductID: id, OrigName: safeName(part.FileName()), Width: c.Width, Height: c.Height, Metrics: Object{}, RetouchLog: Object{}, Source: "original"}, tmp: tmp.Name()})
		if e1 != nil {
			fail(w, 500, "保存上传照片失败")
			return
		}
		ext := f
		if f == "jpeg" {
			ext = "jpg"
		}
		files[len(files)-1].p.File = ext
	}
	if len(files) == 0 {
		fail(w, 400, "没有收到 files 照片")
		return
	}
	tx, e := a.DB.Begin()
	if e != nil {
		fail(w, 500, "保存照片失败")
		return
	}
	defer tx.Rollback()
	renamed := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, s := range renamed {
				_ = os.Remove(s)
			}
		}
	}()
	photos := []Photo{}
	for _, v := range files {
		p := v.p
		p.CreatedBy = currentUser(r).ID
		p.CreatedAt = now()
		p.Kind = "photo"
		p.Tags = []string{}
		p.Op = Object{}
		res, e := tx.Exec("INSERT INTO assets(product_id,file,orig_name,width,height,created_by,created_at) VALUES(?,'',?,?,?,?,?)", id, p.OrigName, p.Width, p.Height, currentUser(r).ID, now())
		if e != nil {
			fail(w, 500, "保存照片失败")
			return
		}
		p.ID, _ = res.LastInsertId()
		base := strings.TrimSuffix(p.OrigName, filepath.Ext(p.OrigName))
		p.File = fmt.Sprintf("原图/%06d_%s.%s", p.ID, base, p.File)
		dst := filepath.Join(a.Dir(id), filepath.FromSlash(p.File))
		if e = os.Rename(v.tmp, dst); e != nil {
			fail(w, 500, "保存照片文件失败")
			return
		}
		renamed = append(renamed, dst)
		if _, e = tx.Exec("UPDATE assets SET file=? WHERE id=?", p.File, p.ID); e != nil {
			fail(w, 500, "保存照片失败")
			return
		}
		p.URL = fmt.Sprintf("/files/products/%d/%s", id, p.File)
		photos = append(photos, p)
	}
	if _, e = tx.Exec("UPDATE products SET updated_at=? WHERE id=?", now(), id); e != nil {
		fail(w, 500, "更新款式失败")
		return
	}
	if e = tx.Commit(); e != nil {
		fail(w, 500, "保存照片失败")
		return
	}
	committed = true
	writeJSON(w, 201, Object{"items": photos, "total": len(photos)})
}
