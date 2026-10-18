package assistant

import (
	"encoding/json"
	"fmt"
	"xstudio/internal/core"
)

type capability struct{ name, perm, description string }

var toolCatalog = []capability{
	{"today", "", "今天的统计与待办；参数 {}"},
	{"search_products", core.ProductView, "查找商品；参数 {q?:款名或编号,status?:状态,shop_id?:店铺编号,limit?:1至20，默认10}"},
	{"get_product", core.ProductView, "商品信息、版本和素材来源计数；参数 {id:商品编号}"},
	{"list_assets", core.ProductView, "商品素材；参数 {product_id:商品编号,source?:来源,tag?:标签,limit?:1至50，默认50}"},
	{"recent_jobs", "", "最近任务；参数 {status?:failed或running或done,limit?:1至20，默认10}"},
	{"review_queue", core.RenderReview, "待审核上架版本，最多20条；参数 {shop_id?:店铺编号}"},
	{"list_presets", core.AssetUpload, "可用图片处理预设；参数 {}"},
	{"list_templates", core.ProductView, "可用出图模板；参数 {shop_id?:店铺编号}"},
	{"my_usage", "", "本月文本和图片用量、额度；参数 {}"},
}

func catalogText(u core.User, catalog []capability) string {
	out := ""
	for _, c := range catalog {
		if c.perm == "" || u.Has(c.perm) {
			out += c.name + "：" + c.description + "\n"
		}
	}
	return out
}

func bounded(n *int, def, max int) (int, error) {
	if n == nil {
		return def, nil
	}
	if *n < 1 || *n > max {
		return 0, fmt.Errorf("数量必须在 1–%d 之间", max)
	}
	return *n, nil
}

func (a *Module) tool(u core.User, name string, raw json.RawMessage) (any, error) {
	known := false
	for _, c := range toolCatalog {
		if c.name == name {
			known = true
			if c.perm != "" && !u.Has(c.perm) {
				return nil, fmt.Errorf("没有此工具的权限")
			}
			break
		}
	}
	if !known {
		return nil, fmt.Errorf("未知工具")
	}
	switch name {
	case "today", "list_presets", "my_usage":
		if len(raw) == 0 {
			raw = json.RawMessage(`{}`)
		}
		if err := parseArgs(raw, &struct{}{}); err != nil {
			return nil, err
		}
		switch name {
		case "today":
			return a.today(u, false)
		case "my_usage":
			return a.usage(u)
		default:
			items, err := core.QueryObjects(a.DB, "SELECT id,name,shared,params FROM asset_presets WHERE owner_id=? OR (shared=1 AND ?<>'shares') ORDER BY id", u.ID, u.Role.Scope)
			visible := []object{}
			for _, v := range items {
				v["shared"] = number(v["shared"]) != 0
				params, _ := v["params"].(map[string]any)
				if ops, ok := params["ops"].(map[string]any); ok {
					if color, ok := ops["color"].(map[string]any); ok {
						if id := number(color["match_to"]); id > 0 {
							if _, ok := a.assetProduct(u, id); !ok {
								continue
							}
						}
					}
				}
				v["ops"] = params["ops"]
				delete(v, "params")
				visible = append(visible, v)
			}
			return visible, err
		}
	case "search_products":
		var in struct {
			Q      string `json:"q"`
			Status string `json:"status"`
			ShopID *int64 `json:"shop_id"`
			Limit  *int   `json:"limit"`
		}
		if err := parseArgs(raw, &in); err != nil {
			return nil, err
		}
		limit, err := bounded(in.Limit, 10, 20)
		if err != nil {
			return nil, err
		}
		scope, args := productScope(u)
		q := "SELECT p.id,p.name,p.status,(SELECT count(*) FROM assets a WHERE a.product_id=p.id) asset_count FROM products p WHERE " + scope
		if in.Q != "" {
			q += " AND (p.name LIKE ? OR CAST(p.id AS TEXT) LIKE ? OR json_extract(p.info,'$.code') LIKE ? OR json_extract(p.info,'$.sku') LIKE ?)"
			for i := 0; i < 4; i++ {
				args = append(args, "%"+in.Q+"%")
			}
		}
		if in.Status != "" {
			if !productStatus(in.Status) {
				return nil, fmt.Errorf("商品状态不正确")
			}
			q += " AND p.status=?"
			args = append(args, in.Status)
		}
		if in.ShopID != nil {
			if *in.ShopID < 1 || !a.CanShop(u, *in.ShopID) {
				return nil, fmt.Errorf("店铺不存在或无权限")
			}
			q += " AND EXISTS(SELECT 1 FROM listings sl WHERE sl.product_id=p.id AND sl.shop_id=?)"
			args = append(args, *in.ShopID)
		}
		args = append(args, limit)
		items, err := core.QueryObjects(a.DB, q+" ORDER BY p.updated_at DESC,p.id DESC LIMIT ?", args...)
		if err != nil {
			return nil, err
		}
		for _, v := range items {
			listings, e := a.productListings(u, number(v["id"]), false)
			if e != nil {
				return nil, e
			}
			v["listings"] = listings
		}
		return items, nil
	case "get_product":
		var in struct {
			ID int64 `json:"id"`
		}
		if err := parseArgs(raw, &in); err != nil {
			return nil, err
		}
		if in.ID < 1 || !a.CanProduct(u, in.ID) {
			return nil, fmt.Errorf("商品不存在或无权限")
		}
		cols := "id,name,status,info,created_at,updated_at"
		if u.Has(core.ProductCost) {
			cols += ",cost,supplier"
		}
		items, err := core.QueryObjects(a.DB, "SELECT "+cols+" FROM products WHERE id=?", in.ID)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("商品不存在或无权限")
		}
		out := items[0]
		out["listings"], err = a.productListings(u, in.ID, true)
		if err != nil {
			return nil, err
		}
		counts := object{"original": int64(0), "ai_enhanced": int64(0), "ai_edited": int64(0), "ai_generated": int64(0)}
		rows, err := core.QueryObjects(a.DB, "SELECT coalesce(nullif(source_override,''),source) source,count(*) n FROM assets WHERE product_id=? GROUP BY coalesce(nullif(source_override,''),source)", in.ID)
		if err != nil {
			return nil, err
		}
		for _, v := range rows {
			if _, ok := counts[v["source"].(string)]; ok {
				counts[v["source"].(string)] = v["n"]
			}
		}
		out["assets"] = counts
		return out, nil
	case "list_assets":
		var in struct {
			ProductID int64  `json:"product_id"`
			Source    string `json:"source"`
			Tag       string `json:"tag"`
			Limit     *int   `json:"limit"`
		}
		if err := parseArgs(raw, &in); err != nil {
			return nil, err
		}
		limit, err := bounded(in.Limit, 50, 50)
		if err != nil {
			return nil, err
		}
		if in.ProductID < 1 || !a.CanProduct(u, in.ProductID) {
			return nil, fmt.Errorf("商品不存在或无权限")
		}
		q := "SELECT id,code,title,coalesce(nullif(source_override,''),source) source,tags,width,height,parent_id,created_at FROM assets WHERE product_id=?"
		args := []any{in.ProductID}
		if in.Source != "" {
			switch in.Source {
			case "original", "ai_enhanced", "ai_edited", "ai_generated":
			default:
				return nil, fmt.Errorf("素材来源不正确")
			}
			q += " AND coalesce(nullif(source_override,''),source)=?"
			args = append(args, in.Source)
		}
		if in.Tag != "" {
			q += " AND EXISTS(SELECT 1 FROM json_each(assets.tags) WHERE value=?)"
			args = append(args, in.Tag)
		}
		args = append(args, limit)
		items, err := core.QueryObjects(a.DB, q+" ORDER BY created_at DESC,id DESC LIMIT ?", args...)
		for _, v := range items {
			if id := number(v["parent_id"]); id > 0 {
				if _, ok := a.assetProduct(u, id); !ok {
					v["parent_id"] = nil
				}
			}
		}
		return items, err
	case "recent_jobs":
		var in struct {
			Status string `json:"status"`
			Limit  *int   `json:"limit"`
		}
		if err := parseArgs(raw, &in); err != nil {
			return nil, err
		}
		limit, err := bounded(in.Limit, 10, 20)
		if err != nil {
			return nil, err
		}
		if in.Status != "" && in.Status != "failed" && in.Status != "running" && in.Status != "done" {
			return nil, fmt.Errorf("任务状态不正确")
		}
		ps, pa := productScope(u)
		ls, la := listingScope(u)
		q := `SELECT j.id,j.kind,j.target_type,j.target_id,j.user_id,j.status,j.message,j.created_at,j.finished_at FROM jobs j WHERE (j.user_id=? OR (j.target_type='product' AND EXISTS(SELECT 1 FROM products p WHERE p.id=j.target_id AND ` + ps + `)) OR (j.target_type='listing' AND EXISTS(SELECT 1 FROM listings l WHERE l.id=j.target_id AND ` + ls + `)) OR (j.target_type='asset' AND EXISTS(SELECT 1 FROM assets a JOIN products p ON p.id=a.product_id WHERE a.id=j.target_id AND ` + ps + `)) OR (j.target_type='user' AND j.target_id=?))`
		args := append([]any{u.ID}, pa...)
		args = append(args, la...)
		args = append(args, pa...)
		args = append(args, u.ID)
		if in.Status != "" {
			q += " AND j.status=?"
			args = append(args, in.Status)
		}
		args = append(args, limit)
		items, err := core.QueryObjects(a.DB, q+" ORDER BY j.created_at DESC,j.id DESC LIMIT ?", args...)
		for _, v := range items {
			_, label, visible := a.reference(u, v["target_type"].(string), number(v["target_id"]))
			v["target_name"] = ""
			if visible {
				v["target_name"] = label
			}
			delete(v, "user_id")
		}
		return items, err
	case "review_queue", "list_templates":
		var in struct {
			ShopID *int64 `json:"shop_id"`
		}
		if err := parseArgs(raw, &in); err != nil {
			return nil, err
		}
		var shop int64
		if in.ShopID != nil {
			shop = *in.ShopID
			if shop < 1 || !a.CanShop(u, shop) {
				return nil, fmt.Errorf("店铺不存在或无权限")
			}
		}
		if name == "list_templates" {
			return a.templates(shop)
		}
		items, err := a.reviewRows(u, shop, 20)
		if err != nil {
			return nil, err
		}
		for _, v := range items {
			id := number(v["id"])
			_, file, e := a.detailTemplate(id, number(v["shop_id"]))
			if e != nil {
				return nil, e
			}
			v["outputs"] = a.outputs(id)
			v["preview_url"] = fmt.Sprintf("/render/详情页模板/%s?p=%d", file, id)
		}
		return items, nil
	}
	return nil, fmt.Errorf("未知工具")
}

func productStatus(s string) bool {
	switch s {
	case "sampling", "pending", "listed", "archived":
		return true
	}
	return false
}

func (a *Module) productListings(u core.User, pid int64, full bool) ([]object, error) {
	scope, args := listingScope(u)
	args = append(args, pid)
	cols := "l.id,l.shop_id,s.name shop_name,l.pipeline_status,l.pipeline_step,l.pipeline_error,coalesce(lr.review_status,'none') review_status"
	if full {
		cols += ",coalesce(lr.detail_template,'') detail_template,coalesce(lr.main_template,'default') main_template"
	}
	items, err := core.QueryObjects(a.DB, "SELECT "+cols+listingJoin+" WHERE "+scope+" AND l.product_id=? ORDER BY l.id", args...)
	if err != nil {
		return nil, err
	}
	if full {
		for _, v := range items {
			id := number(v["id"])
			key, _, e := a.detailTemplate(id, number(v["shop_id"]))
			if e != nil {
				return nil, e
			}
			v["effective_detail_template"] = key
			v["latest_job"] = nil
			if j := a.LatestJob(id); j != nil {
				v["latest_job"] = object{"status": j.Status, "message": j.Message, "finished_at": j.FinishedAt}
			}
		}
	}
	return items, nil
}

func (a *Module) usage(u core.User) (object, error) {
	start, end, err := core.MonthRange("")
	if err != nil {
		return nil, err
	}
	out := object{}
	for _, v := range []struct{ kind, col string }{{"text", "month_text"}, {"image", "month_images"}} {
		var used int64
		if err = a.DB.QueryRow("SELECT coalesce(sum(units),0) FROM ai_usage WHERE user_id=? AND kind=? AND julianday(at)>=julianday(?) AND julianday(at)<julianday(?)", u.ID, v.kind, start, end).Scan(&used); err != nil {
			return nil, err
		}
		rows, e := core.QueryObjects(a.DB, "SELECT "+v.col+" quota FROM quotas WHERE user_id=?", u.ID)
		if e != nil {
			return nil, e
		}
		var quota any
		if len(rows) > 0 {
			quota = rows[0]["quota"]
		}
		out[v.kind] = object{"used": used, "quota": quota}
	}
	return out, nil
}
