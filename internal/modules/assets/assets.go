package assets

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"xstudio/internal/core"
	"xstudio/internal/media"
)

func (a *Module) photoForWrite(w http.ResponseWriter, r *http.Request) (Photo, bool) {
	p, e := scanPhoto(a.DB.QueryRow("SELECT "+photoColumns+" FROM assets WHERE id=?", idParam(r, "pid")))
	if e != nil {
		fail(w, 404, "照片不存在")
		return p, false
	}
	ok := a.RequireProduct(w, r, p.ProductID, true)
	return p, ok
}
func (a *Module) patchPhoto(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	p, ok := a.photoForWrite(w, r)
	if !ok {
		return
	}
	var in map[string]json.RawMessage
	if !decode(w, r, &in) {
		return
	}
	if !a.CanUpload(currentUser(r), p.ProductID) {
		fail(w, 403, "此商品不允许修改素材")
		return
	}
	old := p.Code
	oldWhiteBG := p.WhiteBG
	oldCrop, oldRotate := js(p.Crop), p.Rotate
	for k, v := range in {
		var e error
		switch k {
		case "code":
			if string(v) == "null" {
				p.Code = ""
			} else {
				e = json.Unmarshal(v, &p.Code)
			}
			p.Manual = true
		case "crop":
			e = json.Unmarshal(v, &p.Crop)
		case "rotate":
			e = json.Unmarshal(v, &p.Rotate)
		case "tags":
			e = json.Unmarshal(v, &p.Tags)
		case "source_override":
			if currentUser(r).Role.Key != "admin" {
				fail(w, 403, "只有管理员能改判素材来源")
				return
			}
			e = json.Unmarshal(v, &p.SourceOverride)
			if e == nil && p.SourceOverride != "" && p.SourceOverride != "original" && p.SourceOverride != "ai_enhanced" && p.SourceOverride != "ai_edited" && p.SourceOverride != "ai_generated" {
				e = errors.New("来源不正确")
			}
		case "white_bg":
			e = json.Unmarshal(v, &p.WhiteBG)
		default:
			e = errors.New("不支持的字段")
		}
		if e != nil {
			fail(w, 400, "照片字段类型不正确："+k)
			return
		}
	}
	if e := validatePhoto(p); e != nil {
		fail(w, 400, e.Error())
		return
	}
	if oldCrop != js(p.Crop) || oldRotate != p.Rotate {
		p.EnhancedFile = ""
		p.AIEnhanced = false
		delete(p.RetouchLog, "upscale_error")
	}
	tx, e := a.DB.Begin()
	if e != nil {
		fail(w, 500, "保存照片失败")
		return
	}
	defer tx.Rollback()
	if !p.AIGenerated && p.Code != "" {
		if _, e = tx.Exec("UPDATE assets SET code='' WHERE product_id=? AND code=? AND ai_generated=1", p.ProductID, p.Code); e != nil {
			fail(w, 500, "更新 AI 照片编号失败")
			return
		}
	}
	if _, e := tx.Exec("UPDATE assets SET code=?,crop=?,rotate=?,white_bg=?,manual=?,enhanced_file=?,ai_enhanced=?,retouch_log=?,tags=?,source_override=? WHERE id=?", p.Code, js(p.Crop), p.Rotate, p.WhiteBG, p.Manual, p.EnhancedFile, p.AIEnhanced, js(p.RetouchLog), js(p.Tags), p.SourceOverride, p.ID); e != nil {
		fail(w, 409, "该编号已被另一张照片使用")
		return
	}
	if in["source_override"] != nil {
		if e = core.AuditTx(tx, r, "asset.source_override", "asset", p.ID, Object{"source_override": p.SourceOverride}); e != nil {
			fail(w, 500, "记录来源改判失败")
			return
		}
	}
	if e = tx.Commit(); e != nil {
		fail(w, 500, "保存照片失败")
		return
	}
	if old != p.Code || oldCrop != js(p.Crop) || oldRotate != p.Rotate || oldWhiteBG != p.WhiteBG {
		a.removePhotoOutputs(p.ProductID, old)
		a.removePhotoOutputs(p.ProductID, p.Code)
	}
	_, _ = a.DB.Exec("UPDATE products SET updated_at=? WHERE id=?", now(), p.ProductID)
	writeJSON(w, 200, Object{"asset": p})
}
func (a *Module) removePhotoOutputs(id int64, code string) {
	if code == "" || !media.CodePattern.MatchString(code) {
		return
	}
	for _, ext := range []string{".jpg", "_白底.jpg", "_透明.png"} {
		_ = os.Remove(filepath.Join(a.Dir(id), "照片", code+ext))
	}
}
func (a *Module) deletePhoto(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	p, ok := a.photoForWrite(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	if !u.Has(core.AssetDeleteAny) && !(u.Has(core.AssetDeleteOwn) && p.CreatedBy == u.ID) {
		fail(w, 403, "只能删除自己上传或处理的素材")
		return
	}
	if _, e := a.DB.Exec("DELETE FROM assets WHERE id=?", p.ID); e != nil {
		fail(w, 500, "删除照片失败")
		return
	}
	a.Audit(r, "asset.delete", "asset", p.ID, Object{"product_id": p.ProductID})
	for _, file := range []string{p.File, p.EnhancedFile} {
		if file == "" {
			continue
		}
		var n int
		if e := a.DB.QueryRow("SELECT count(*) FROM assets WHERE product_id=? AND (file=? OR enhanced_file=?)", p.ProductID, file, file).Scan(&n); e == nil && n == 0 {
			_ = os.Remove(filepath.Join(a.Dir(p.ProductID), filepath.FromSlash(file)))
		}
	}
	a.removePhotoOutputs(p.ProductID, p.Code)
	_, _ = a.DB.Exec("UPDATE products SET updated_at=? WHERE id=?", now(), p.ProductID)
	writeJSON(w, 200, Object{})
}
