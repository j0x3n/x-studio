package assets

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"xstudio/internal/core"
)

func (a *Module) workbenchSave(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	u := currentUser(r)
	item, err := a.workbenchItem(u.ID, idParam(r, "id"))
	if err != nil || !a.modelWorkbenchVisible(u, item) {
		fail(w, 404, "处理台图片不存在")
		return
	}
	if item.Kind != "result" {
		fail(w, 400, "只有处理结果可以保存")
		return
	}
	if item.Op["type"] == "model_candidate" {
		fail(w, 400, "请确认保存到模特库")
		return
	}
	var raw Object
	if !decode(w, r, &raw) {
		return
	}
	var in struct {
		ProductID  *int64 `json:"product_id"`
		SetCurrent bool   `json:"set_current"`
	}
	if err = strictDecode(raw, &in); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if item.Op["type"] == "tryon" {
		in.SetCurrent = false
	}
	if item.SavedAssetID != nil {
		p, e := a.asset(*item.SavedAssetID)
		if e != nil || !a.CanProduct(u, p.ProductID) {
			fail(w, 404, "素材不存在")
			return
		}
		if !a.CanUpload(u, p.ProductID) {
			fail(w, 403, "此商品不允许生成新素材")
			return
		}
		a.workbenchSavedResponse(w, r, p)
		return
	}
	var source Asset
	sourceOK := false
	if item.AssetID != nil {
		source, err = a.asset(*item.AssetID)
		sourceOK = err == nil && a.CanProduct(u, source.ProductID)
	}
	var pid int64
	if in.ProductID != nil {
		pid = *in.ProductID
	} else if sourceOK {
		pid = source.ProductID
	} else if item.AssetID != nil {
		fail(w, 404, "素材不存在")
		return
	} else {
		fail(w, 400, "请选择保存到哪一款")
		return
	}
	if !a.CanProduct(u, pid) {
		fail(w, 404, "商品不存在")
		return
	}
	if !a.CanUpload(u, pid) {
		fail(w, 403, "此商品不允许生成新素材")
		return
	}
	if sharedPipelineBusy(a.DB, pid) {
		fail(w, 409, "商品正在检查、分拣、增强或修图，不能保存素材")
		return
	}
	var parent *int64
	tags := []string{}
	title, note, code := "", "", ""
	if sourceOK {
		tags, title, note = source.Tags, source.Title, source.Note
		if source.ProductID == pid {
			parent = &source.ID
			if in.SetCurrent {
				code = source.Code
			}
		}
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(item.File)), ".")
	if format != "jpg" && format != "png" {
		fail(w, 400, "处理结果格式不正确")
		return
	}
	root, err := a.workbenchRoot(u.ID)
	if err != nil {
		fail(w, 500, "打开处理台目录失败")
		return
	}
	defer root.Close()
	data, err := workbenchRead(root, item.File)
	if err != nil {
		fail(w, 400, "读取处理结果失败")
		return
	}
	base, err := os.OpenRoot(a.Cfg.DataDir)
	if err != nil {
		fail(w, 500, "打开素材目录失败")
		return
	}
	defer base.Close()
	dir := fmt.Sprintf("products/%d", pid)
	if err = base.MkdirAll(dir+"/原图", 0700); err != nil {
		fail(w, 500, "创建素材目录失败")
		return
	}
	dest, err := base.OpenRoot(dir)
	if err != nil {
		fail(w, 500, "打开素材目录失败")
		return
	}
	defer dest.Close()
	tx, err := a.DB.Begin()
	if err != nil {
		fail(w, 500, "保存素材失败")
		return
	}
	defer tx.Rollback()
	t := now()
	if code != "" {
		if _, err = tx.Exec("UPDATE assets SET code='',updated_at=? WHERE id=?", t, source.ID); err != nil {
			fail(w, 500, "转移拍摄编号失败")
			return
		}
	}
	res, err := tx.Exec("INSERT INTO assets(product_id,parent_id,file,orig_name,width,height,source,code,manual,tags,created_by,created_at,updated_at,title,note,op,phash,ai_enhanced,ai_generated) VALUES(?,?,'',?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", pid, parent, item.OrigName, item.Width, item.Height, item.Source, code, code != "", js(tags), u.ID, t, t, title, note, js(item.Op), item.PHash, item.Source != "original", item.Source == "ai_generated")
	if err != nil {
		fail(w, 500, "保存素材记录失败")
		return
	}
	aid, _ := res.LastInsertId()
	file := fmt.Sprintf("原图/proc_%d.%s", aid, format)
	if _, err = tx.Exec("UPDATE assets SET file=? WHERE id=?", file, aid); err != nil {
		fail(w, 500, "保存素材路径失败")
		return
	}
	if _, err = tx.Exec("UPDATE workbench_items SET saved_asset_id=? WHERE id=? AND user_id=?", aid, item.ID, u.ID); err != nil {
		fail(w, 500, "记录保存结果失败")
		return
	}
	detail := Object{"asset_id": aid, "product_id": pid, "set_current": code != ""}
	if err = core.AuditTx(tx, r, "assets.workbench.save", "workbench_item", item.ID, detail); err != nil {
		fail(w, 500, "记录保存日志失败")
		return
	}
	if code != "" {
		if err = core.AuditTx(tx, r, "assets.set_current", "asset", aid, Object{"from_asset_id": source.ID, "to_asset_id": aid, "code": code}); err != nil {
			fail(w, 500, "记录编号转移失败")
			return
		}
	}
	f, err := dest.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fail(w, 500, "创建素材文件失败")
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = dest.Remove(file)
		}
	}()
	_, err = f.Write(data)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		fail(w, 500, "保存素材文件失败")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "提交素材失败")
		return
	}
	committed = true
	if code != "" {
		a.removePhotoOutputs(pid, code)
	}
	p, err := a.asset(aid)
	if err != nil {
		fail(w, 500, "读取保存的素材失败")
		return
	}
	a.workbenchSavedResponse(w, r, p)
}
func (a *Module) workbenchSavedResponse(w http.ResponseWriter, r *http.Request, p Asset) {
	chain, err := a.readAssets("SELECT "+assetColumns+" FROM assets WHERE product_id=? ORDER BY created_at,id", p.ProductID)
	if err != nil {
		fail(w, 500, "读取素材版本失败")
		return
	}
	writeJSON(w, 200, Object{"asset": a.decorate(p, currentUser(r), chain)})
}
