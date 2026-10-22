package assets

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"xstudio/internal/core"
	"xstudio/internal/imaging"
)

type viewJobRequest struct {
	AssetIDs []int64 `json:"asset_ids"`
	Replace  string  `json:"replace,omitempty"`
}

func (a *Module) viewsClassify(w http.ResponseWriter, r *http.Request) { a.enqueueViewJob(w, r, false) }
func (a *Module) viewsEnhance(w http.ResponseWriter, r *http.Request)  { a.enqueueViewJob(w, r, true) }

func (a *Module) enqueueViewJob(w http.ResponseWriter, r *http.Request, enhance bool) {
	var raw Object
	if !decode(w, r, &raw) {
		return
	}
	var in viewJobRequest
	if strictDecode(raw, &in) != nil {
		fail(w, 400, "底图任务参数不正确")
		return
	}
	if enhance && in.Replace != "confirm" && in.Replace != "auto" {
		fail(w, 400, "replace 必须是 confirm 或 auto")
		return
	}
	if !enhance && in.Replace != "" {
		fail(w, 400, "分拣不接受 replace 参数")
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := idParam(r, "id")
	p, ok := a.authorizedProduct(w, r, id, true)
	if !ok {
		return
	}
	if !enhance && len(p.Category.Views) == 0 {
		fail(w, 400, "请先设置品类底图槽位")
		return
	}
	if !enhance && in.AssetIDs == nil {
		items, err := a.readAssets("SELECT "+assetColumns+" FROM assets WHERE product_id=? AND view_code='' AND archived=0 AND kind<>'output' AND media='image' ORDER BY id", id)
		if err != nil {
			fail(w, 500, "读取未分配照片失败")
			return
		}
		for _, item := range items {
			in.AssetIDs = append(in.AssetIDs, item.ID)
		}
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
	for _, item := range items {
		if item.ProductID != id || item.Archived || item.Kind == "output" || item.Media != "image" {
			fail(w, 400, "只能处理本商品未丢弃的照片")
			return
		}
	}
	kind := "assets.views_classify"
	if enhance {
		kind = "assets.views_enhance"
	}
	j, err := a.enqueue(kind, id, currentUser(r).ID, in)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	a.Audit(r, kind, "product", id, Object{"job_id": j.ID, "asset_ids": in.AssetIDs, "replace": in.Replace})
	writeJSON(w, 202, Object{"job_id": j.ID, "job": j})
}

func imagePart(im image.Image) (Object, error) {
	var data bytes.Buffer
	if err := jpeg.Encode(&data, imaging.Resize(im, 1024), &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return Object{"type": "image_url", "image_url": Object{"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data.Bytes())}}, nil
}

func (a *Module) freshViewAsset(ctx context.Context, j core.Job, id int64, permission string) (Asset, error) {
	if ctx.Err() != nil {
		return Asset{}, ctx.Err()
	}
	u, err := a.User(j.UserID)
	if err != nil || u.Disabled || !u.Has(permission) || !a.CanUpload(u, j.TargetID) {
		return Asset{}, fmt.Errorf("底图处理权限已变化")
	}
	p, err := a.asset(id)
	if err != nil || p.ProductID != j.TargetID || p.Archived || p.Kind == "output" || p.Media != "image" {
		return p, fmt.Errorf("照片已移动、丢弃或删除")
	}
	return p, nil
}
