package assets

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"xstudio/internal/core"
)

type selection struct {
	AssetIDs []int64 `json:"asset_ids"`
}

func validIDs(ids []int64) error {
	if len(ids) < 1 || len(ids) > 200 {
		return fmt.Errorf("每次请选择 1–200 张素材")
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id < 1 || seen[id] {
			return fmt.Errorf("素材编号不正确或重复")
		}
		seen[id] = true
	}
	return nil
}
func (a *Module) selected(u core.User, ids []int64) ([]Asset, error) {
	if err := validIDs(ids); err != nil {
		return nil, err
	}
	out := []Asset{}
	for _, id := range ids {
		p, err := a.asset(id)
		if err != nil || !a.CanProduct(u, p.ProductID) {
			return nil, fmt.Errorf("素材不存在")
		}
		out = append(out, p)
	}
	return out, nil
}
func canDelete(u core.User, p Asset) bool {
	return u.Has(core.AssetDeleteAny) || (u.Has(core.AssetDeleteOwn) && p.CreatedBy == u.ID)
}
func (a *Module) batch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		selection
		Action    string   `json:"action"`
		Tags      []string `json:"tags"`
		ProductID int64    `json:"product_id"`
		FolderID  *int64   `json:"folder_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := validIDs(in.AssetIDs); err != nil {
		fail(w, 400, err.Error())
		return
	}
	u := currentUser(r)
	perm := core.AssetUpload
	switch in.Action {
	case "tag_add", "tag_remove", "archive", "unarchive":
	case "folder":
		if !folderWriteAllowed(w, r) {
			return
		}
	case "move":
		perm = core.ProductEdit
	case "delete":
		perm = ""
	default:
		fail(w, 400, "不支持的批量动作")
		return
	}
	if (perm != "" && !u.Has(perm)) || (in.Action == "delete" && !u.Has(core.AssetDeleteOwn) && !u.Has(core.AssetDeleteAny)) {
		fail(w, 403, "没有此批量操作的权限")
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	if in.Action == "folder" && !a.requireFolder(w, r, in.FolderID) {
		return
	}
	if in.Action == "move" && !a.CanProduct(u, in.ProductID) {
		fail(w, 404, "目标商品不存在")
		return
	}
	items := []Object{}
	moved := []int64{}
	for _, id := range in.AssetIDs {
		result := Object{"asset_id": id}
		p, err := a.asset(id)
		if err != nil || !a.CanProduct(u, p.ProductID) {
			result["error"] = "素材不存在"
			items = append(items, result)
			continue
		}
		if a.ActiveProduct(p.ProductID) || a.Active("product", p.ProductID) {
			err = fmt.Errorf("商品正在处理中，请等待任务完成")
		} else if in.Action != "delete" && in.Action != "move" && !a.CanUpload(u, p.ProductID) {
			err = fmt.Errorf("此商品不允许修改素材")
		} else {
			switch in.Action {
			case "folder":
				_, err = a.DB.Exec("UPDATE assets SET folder_id=?,updated_at=? WHERE id=?", in.FolderID, now(), id)
			case "tag_add", "tag_remove":
				tags := normalizeTags(in.Tags)
				if in.Action == "tag_add" {
					p.Tags = normalizeTags(append(p.Tags, tags...))
				} else {
					drop := map[string]bool{}
					for _, v := range tags {
						drop[v] = true
					}
					keep := []string{}
					for _, v := range p.Tags {
						if !drop[v] {
							keep = append(keep, v)
						}
					}
					p.Tags = keep
				}
				_, err = a.DB.Exec("UPDATE assets SET tags=?,updated_at=? WHERE id=?", js(p.Tags), now(), id)
			case "archive", "unarchive":
				_, err = a.DB.Exec("UPDATE assets SET archived=?,updated_at=? WHERE id=?", in.Action == "archive", now(), id)
			case "delete":
				if !canDelete(u, p) {
					err = fmt.Errorf("只能删除自己上传或处理的素材")
				} else {
					_, err = a.DB.Exec("DELETE FROM assets WHERE id=?", id)
					if err == nil {
						a.cleanupFiles(p)
						a.removePhotoOutputs(p.ProductID, p.Code)
					}
				}
			case "move":
				if p.Code != "" {
					err = fmt.Errorf("素材仍占用拍摄编号，不能移动")
				} else if a.ActiveProduct(in.ProductID) || a.Active("product", in.ProductID) {
					err = fmt.Errorf("目标商品正在处理中")
				} else {
					err = a.move(p, in.ProductID)
					if err == nil {
						moved = append(moved, id)
					}
				}
			}
		}
		if err != nil {
			result["error"] = a.SafeAIError(err.Error())
		} else {
			result["ok"] = true
			detail := Object{"product_id": p.ProductID, "to_product_id": in.ProductID}
			if in.Action == "folder" {
				detail["folder_id"] = in.FolderID
			}
			a.Audit(r, "assets.batch."+in.Action, "asset", id, detail)
		}
		items = append(items, result)
	}
	for _, id := range moved {
		_, _ = a.DB.Exec("UPDATE assets SET parent_id=NULL WHERE (id=? OR parent_id=?) AND EXISTS(SELECT 1 FROM assets parent WHERE parent.id=assets.parent_id AND parent.product_id<>assets.product_id)", id, id)
	}
	writeJSON(w, 200, Object{"items": items, "total": len(items)})
}
func (a *Module) cleanupFiles(p Asset) {
	for _, file := range []string{p.File, p.EnhancedFile} {
		if file == "" || !validAssetFile(file) {
			continue
		}
		var n int
		if a.DB.QueryRow("SELECT count(*) FROM assets WHERE product_id=? AND (file=? OR enhanced_file=?)", p.ProductID, file, file).Scan(&n) == nil && n == 0 {
			_ = os.Remove(filepath.Join(a.Dir(p.ProductID), filepath.FromSlash(file)))
		}
	}
}
func (a *Module) copyToProduct(p Asset, file string, dst int64) (string, error) {
	if !validAssetFile(file) {
		return "", fmt.Errorf("素材文件路径不合法")
	}
	root, err := os.OpenRoot(a.Dir(p.ProductID))
	if err != nil {
		return "", fmt.Errorf("打开素材目录失败")
	}
	defer root.Close()
	src, err := root.Open(file)
	if err != nil {
		return "", fmt.Errorf("读取素材文件失败")
	}
	defer src.Close()
	if err = os.MkdirAll(filepath.Join(a.Dir(dst), "原图"), 0700); err != nil {
		return "", fmt.Errorf("创建目标素材目录失败")
	}
	out, err := os.CreateTemp(filepath.Join(a.Dir(dst), "原图"), fmt.Sprintf("move_%d-*%s", p.ID, filepath.Ext(file)))
	if err != nil {
		return "", fmt.Errorf("创建目标素材文件失败")
	}
	name := out.Name()
	_, err = io.Copy(out, src)
	ce := out.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		os.Remove(name)
		return "", fmt.Errorf("复制素材文件失败")
	}
	return "原图/" + filepath.Base(name), nil
}
func (a *Module) move(p Asset, dst int64) error {
	if dst == p.ProductID {
		return nil
	}
	file, err := a.copyToProduct(p, p.File, dst)
	if err != nil {
		return err
	}
	enhanced := ""
	if p.EnhancedFile != "" {
		enhanced, err = a.copyToProduct(p, p.EnhancedFile, dst)
		if err != nil {
			os.Remove(filepath.Join(a.Dir(dst), filepath.FromSlash(file)))
			return err
		}
	}
	_, err = a.DB.Exec("UPDATE assets SET product_id=?,file=?,enhanced_file=?,updated_at=? WHERE id=?", dst, file, enhanced, now(), p.ID)
	if err != nil {
		os.Remove(filepath.Join(a.Dir(dst), filepath.FromSlash(file)))
		if enhanced != "" {
			os.Remove(filepath.Join(a.Dir(dst), filepath.FromSlash(enhanced)))
		}
		return fmt.Errorf("保存素材移动失败")
	}
	a.cleanupFiles(p)
	return nil
}
