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
	"strconv"
	"strings"
	"unicode"

	"xstudio/internal/core"
	"xstudio/internal/imaging"
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
	code := ""
	codeSent := false
	var folderID *int64
	if values, ok := r.URL.Query()["folder_id"]; ok {
		if !folderWriteAllowed(w, r) {
			return
		}
		value, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || value < 1 {
			fail(w, 400, "文件夹编号不正确")
			return
		}
		folderID = &value
	}
	id := idParam(r, "id")
	if _, ok := a.authorizedProduct(w, r, id, true); !ok {
		return
	}
	bodyLimit := maxVideoBytes
	if bodyLimit < maxPhotoBytes {
		bodyLimit = maxPhotoBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*bodyLimit+1<<20)
	if !a.requireFolder(w, r, folderID) {
		return
	}
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
		p      Asset
		tmp    string
		poster string
	}
	files := []pending{}
	defer func() {
		for _, v := range files {
			_ = os.Remove(v.tmp)
			if v.poster != "" {
				_ = os.Remove(v.poster)
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
		if part.FormName() == "code" && part.FileName() == "" {
			value, err := io.ReadAll(io.LimitReader(part, 128))
			_ = part.Close()
			if err != nil || len(value) > 60 || codeSent {
				fail(w, 400, "底图槽位编号不正确或重复")
				return
			}
			code, codeSent = strings.TrimSpace(string(value)), true
			continue
		}
		if part.FormName() != "files" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		if len(files) >= 32 {
			_ = part.Close()
			fail(w, 413, "一次最多上传 32 张照片")
			return
		}
		name := safeName(part.FileName())
		head, e := readPrefix(part, 32)
		if e != nil {
			_ = part.Close()
			fail(w, 400, "读取上传文件失败")
			return
		}
		if ext, _, ok := core.SniffVideo(head); ok {
			saved, status, msg := a.saveIncomingVideo(id, head, part, ext)
			_ = part.Close()
			if status != 0 {
				fail(w, status, msg)
				return
			}
			p := Asset{Photo: Photo{ProductID: id, File: saved.file, OrigName: name, Width: saved.width, Height: saved.height, Metrics: saved.metrics, RetouchLog: Object{}, Source: "original", Media: "video", DurationMS: saved.dur, PosterFile: saved.posterFile}}
			files = append(files, pending{p: p, tmp: saved.tmp, poster: saved.posterTmp})
			continue
		}
		rest, e := io.ReadAll(io.LimitReader(part, maxPhotoBytes+1))
		_ = part.Close()
		if e != nil {
			fail(w, 400, "读取上传照片失败")
			return
		}
		b := append(append([]byte{}, head...), rest...)
		if int64(len(b)) > maxPhotoBytes {
			fail(w, 413, "单张照片不能超过 25 MB")
			return
		}
		c, f, e := imageConfig(b)
		if e != nil {
			if strings.Contains(e.Error(), "像素") {
				fail(w, 400, e.Error())
			} else {
				fail(w, 400, "只接受真实的 JPG、PNG、WebP 图片，或 MP4、MOV、WebM 视频")
			}
			return
		}
		decoded, _, decodeErr := image.Decode(bytes.NewReader(b))
		if decodeErr != nil {
			fail(w, 400, "图片损坏，无法解码")
			return
		}
		tmp, e := os.CreateTemp(filepath.Join(a.Dir(id), "原图"), ".upload-*")
		if e != nil {
			fail(w, 500, "保存上传照片失败")
			return
		}
		e1 := func() error { defer tmp.Close(); _, e := tmp.Write(b); return e }()
		ext := f
		if f == "jpeg" {
			ext = "jpg"
		}
		files = append(files, pending{p: Asset{Photo: Photo{ProductID: id, File: ext, OrigName: name, Width: c.Width, Height: c.Height, Metrics: Object{}, RetouchLog: Object{}, Source: "original", Media: "image"}, PHash: fmt.Sprintf("%016x", imaging.Hash(imaging.Geometry(decoded, imaging.ReadExif(b).Orientation, 0, nil)))}, tmp: tmp.Name()})
		metrics, _ := imaging.Metrics(imaging.Geometry(decoded, imaging.ReadExif(b).Orientation, 0, nil), imaging.ReadExif(b))
		files[len(files)-1].p.Metrics = metrics
		if e1 != nil {
			fail(w, 500, "保存上传照片失败")
			return
		}
	}
	if len(files) == 0 {
		fail(w, 400, "没有收到 files 照片")
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	product, ok := a.authorizedProduct(w, r, id, true)
	if !ok {
		return
	}
	if code != "" {
		if !viewCodeAllowed(product, code) {
			fail(w, 400, "编号必须属于本商品底图槽位")
			return
		}
		for _, file := range files {
			if file.p.Media != "image" {
				fail(w, 400, "视频不能设置底图槽位")
				return
			}
		}
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
	photos := []Asset{}
	medias := []string{}
	for _, v := range files {
		p := v.p
		p.FolderID = folderID
		p.CreatedBy = currentUser(r).ID
		p.CreatedAt = now()
		p.UpdatedAt = p.CreatedAt
		p.Kind = "photo"
		p.Tags = []string{}
		p.Op = Object{}
		if p.Media == "" {
			p.Media = "image"
		}
		res, e := tx.Exec("INSERT INTO assets(product_id,file,orig_name,width,height,metrics,created_by,created_at,folder_id,media,duration_ms,poster_file) VALUES(?,'',?,?,?,?,?,?,?,?,?,?)", id, p.OrigName, p.Width, p.Height, js(p.Metrics), currentUser(r).ID, p.CreatedAt, folderID, p.Media, p.DurationMS, p.PosterFile)
		if e != nil {
			fail(w, 500, "保存照片失败")
			return
		}
		p.ID, _ = res.LastInsertId()
		if code != "" {
			if err := setCurrentTx(tx, p, code); err != nil {
				fail(w, 500, "设置底图槽位失败")
				return
			}
			p.Code, p.ViewCode, p.Manual = code, code, true
		}
		p.Suggest, p.EnhanceCheck = Object{}, Object{}
		p.BlurLevel, _ = p.Metrics["blur_level"].(string)
		if p.Media != "video" {
			base := strings.TrimSuffix(p.OrigName, filepath.Ext(p.OrigName))
			p.File = fmt.Sprintf("原图/%06d_%s.%s", p.ID, base, p.File)
		}
		dst := filepath.Join(a.Dir(id), filepath.FromSlash(p.File))
		if e = os.Rename(v.tmp, dst); e != nil {
			fail(w, 500, "保存照片文件失败")
			return
		}
		renamed = append(renamed, dst)
		if v.poster != "" {
			posterDst := filepath.Join(a.Dir(id), filepath.FromSlash(p.PosterFile))
			if e = os.Rename(v.poster, posterDst); e != nil {
				fail(w, 500, "保存视频封面失败")
				return
			}
			renamed = append(renamed, posterDst)
			p.PosterURL = fmt.Sprintf("/api/assets/%d/poster", p.ID)
		}
		if _, e = tx.Exec("UPDATE assets SET file=?,phash=?,updated_at=? WHERE id=?", p.File, p.PHash, p.UpdatedAt, p.ID); e != nil {
			fail(w, 500, "保存照片失败")
			return
		}
		p.URL = fmt.Sprintf("/files/products/%d/%s", id, p.File)
		photos = append(photos, p)
		medias = append(medias, p.Media)
	}
	if e = tx.Commit(); e != nil {
		fail(w, 500, "保存照片失败")
		return
	}
	committed = true
	if code != "" {
		a.removePhotoOutputs(id, code)
	}
	ids := []int64{}
	for index := range photos {
		fresh, err := a.asset(photos[index].ID)
		if err != nil {
			fail(w, 500, "读取上传结果失败")
			return
		}
		photos[index] = fresh
	}
	for i := range photos {
		ids = append(ids, photos[i].ID)
		photos[i] = a.decorate(photos[i], currentUser(r), photos)
	}
	a.Audit(r, "assets.upload", "product", id, Object{"asset_ids": ids, "media": medias})
	writeJSON(w, 201, Object{"items": photos, "total": len(photos)})
}
