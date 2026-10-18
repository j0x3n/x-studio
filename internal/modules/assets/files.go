package assets

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math/bits"
	"net/http"
	"os"

	"sort"
	"strconv"
	"strings"

	"xstudio/internal/imaging"
)

func (a *Module) load(p Asset) (*image.NRGBA, imaging.Exif, error) {
	if !validAssetFile(p.File) {
		return nil, imaging.Exif{}, fmt.Errorf("素材文件路径不合法")
	}
	root, err := os.OpenRoot(a.Dir(p.ProductID))
	if err != nil {
		return nil, imaging.Exif{}, fmt.Errorf("打开素材目录失败")
	}
	defer root.Close()
	f, err := root.Open(p.File)
	if err != nil {
		return nil, imaging.Exif{}, fmt.Errorf("读取素材文件失败")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (128<<20)+1))
	if err != nil || len(b) > 128<<20 {
		return nil, imaging.Exif{}, fmt.Errorf("素材文件读取失败或过大")
	}
	if _, _, err = imageConfig(b); err != nil {
		return nil, imaging.Exif{}, err
	}
	im, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, imaging.Exif{}, fmt.Errorf("素材图片已损坏")
	}
	return imaging.Geometry(im, 1, 0, nil), imaging.ReadExif(b), nil
}
func (a *Module) download(w http.ResponseWriter, r *http.Request) {
	var in struct {
		selection
	}
	if !decode(w, r, &in) {
		return
	}
	if err := validIDs(in.AssetIDs); err != nil {
		fail(w, 400, err.Error())
		return
	}
	items, err := a.selected(currentUser(r), in.AssetIDs)
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	f, err := os.CreateTemp(a.Cfg.DataDir, ".assets-*.zip")
	if err != nil {
		fail(w, 500, "创建下载包失败")
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()
	z := zip.NewWriter(f)
	names := map[string]bool{}
	for _, p := range items {
		im, ex, e := a.load(p)
		if e != nil {
			z.Close()
			fail(w, 400, e.Error())
			return
		}
		var product string
		if e = a.DB.QueryRow("SELECT name FROM products WHERE id=?", p.ProductID).Scan(&product); e != nil {
			z.Close()
			fail(w, 500, "读取商品名失败")
			return
		}
		code := p.Code
		if code == "" {
			code = fmt.Sprint(p.ID)
		}
		base := safeName(product) + "_" + safeName(code) + "_" + p.EffectiveSource()
		name := base + ".jpg"
		if names[name] {
			name = base + "_" + fmt.Sprint(p.ID) + ".jpg"
		}
		names[name] = true
		entry, e := z.Create(name)
		if e == nil {
			var buf bytes.Buffer
			e = jpeg.Encode(&buf, imaging.Geometry(im, ex.Orientation, 0, nil), &jpeg.Options{Quality: 92})
			if e == nil {
				_, e = entry.Write(buf.Bytes())
			}
		}
		if e != nil {
			z.Close()
			fail(w, 500, "打包素材失败")
			return
		}
	}
	if err = z.Close(); err != nil {
		fail(w, 500, "完成下载包失败")
		return
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		fail(w, 500, "读取下载包失败")
		return
	}
	st, err := f.Stat()
	if err != nil {
		fail(w, 500, "读取下载包失败")
		return
	}
	a.Audit(r, "assets.download", "assets", 0, Object{"asset_ids": in.AssetIDs})
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="assets.zip"`)
	http.ServeContent(w, r, "assets.zip", st.ModTime(), f)
}
func (a *Module) similar(w http.ResponseWriter, r *http.Request) {
	p, err := a.asset(idParam(r, "pid"))
	u := currentUser(r)
	if err != nil || !a.CanProduct(u, p.ProductID) {
		fail(w, 404, "素材不存在")
		return
	}
	if err = a.ensureHash(&p); err != nil {
		fail(w, 400, err.Error())
		return
	}
	hash, err := strconv.ParseUint(p.PHash, 16, 64)
	if err != nil {
		fail(w, 400, "素材哈希无效")
		return
	}
	scope, args := productScopeSQL(u)
	args = append(args, p.ID)
	all, err := a.readAssets("SELECT "+strings.ReplaceAll("a."+assetColumns, ",", ",a.")+" FROM assets a JOIN products p ON p.id=a.product_id WHERE "+scope+" AND a.phash<>'' AND a.phash<>'-' AND a.id<>? ORDER BY a.id", args...)
	if err != nil {
		fail(w, 500, "读取相似素材失败")
		return
	}
	type hit struct {
		Asset
		Distance int `json:"distance"`
	}
	items := []hit{}
	for _, v := range all {
		h, e := strconv.ParseUint(v.PHash, 16, 64)
		if e != nil {
			continue
		}
		distance := bits.OnesCount64(h ^ hash)
		if distance <= 10 {
			items = append(items, hit{v, distance})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Distance == items[j].Distance {
			return items[i].ID < items[j].ID
		}
		return items[i].Distance < items[j].Distance
	})
	if len(items) > 50 {
		items = items[:50]
	}
	chains := map[int64][]Asset{}
	for i := range items {
		p := &items[i].Asset
		if _, ok := chains[p.ProductID]; !ok {
			chain, err := a.readAssets("SELECT "+assetColumns+" FROM assets WHERE product_id=? ORDER BY created_at,id", p.ProductID)
			if err != nil {
				fail(w, 500, "读取版本链失败")
				return
			}
			chains[p.ProductID] = chain
		}
		*p = a.decorate(*p, u, chains[p.ProductID])
	}
	writeJSON(w, 200, Object{"items": items, "total": len(items)})
}
