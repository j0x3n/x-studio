package core

import "net/http"

type Permission struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

const (
	ProductView      = "product.view"
	ProductCost      = "product.cost"
	ProductEdit      = "product.edit"
	ProductDeleteOwn = "product.delete_own"
	ProductDeleteAny = "product.delete_any"
	AssetUpload      = "asset.upload"
	AssetUpscale     = "asset.upscale"
	AssetProcess     = "asset.process"
	AssetDeleteOwn   = "asset.delete_own"
	AssetDeleteAny   = "asset.delete_any"
	CopyView         = "copy.view"
	CopyEdit         = "copy.edit"
	TemplateManage   = "template.manage"
	RenderRun        = "render.run"
	ExportRun        = "export.run"
	ExportNoAILabel  = "export.no_ai_label"
	AdminUsers       = "admin.users"
	AdminShops       = "admin.shops"
	AdminSettings    = "admin.settings"
	AdminAudit       = "admin.audit"
	ShareManage      = "share.manage"
)

var Permissions = []Permission{
	{ProductView, "看商品", "商品"}, {ProductCost, "看成本价和货源", "商品"}, {ProductEdit, "新建、修改商品和上架版本", "商品"}, {ProductDeleteOwn, "删除自己创建的商品", "商品"}, {ProductDeleteAny, "删除任何商品", "商品"},
	{AssetUpload, "上传照片", "素材"}, {AssetUpscale, "AI 高清化", "素材"}, {AssetProcess, "图片处理", "素材"}, {AssetDeleteOwn, "删除自己的素材", "素材"}, {AssetDeleteAny, "删除任何素材", "素材"},
	{CopyView, "看文案", "文案"}, {CopyEdit, "编辑文案", "文案"}, {TemplateManage, "管理模板", "模板"}, {RenderRun, "跑出图任务", "出图"}, {ExportRun, "导出、下载成品", "出图"}, {ExportNoAILabel, "导出时取消 AI 标识", "出图"},
	{AdminUsers, "管理账号、角色、分店和额度", "管理"}, {AdminShops, "管理店铺", "管理"}, {AdminSettings, "修改系统设置", "管理"}, {AdminAudit, "看操作日志", "管理"}, {ShareManage, "分享商品给外部协作者", "管理"},
}

func KnownPermission(p string) bool {
	for _, v := range Permissions {
		if v.Key == p {
			return true
		}
	}
	return false
}
func (u User) Has(p string) bool {
	if u.Role.Key == "admin" {
		return true
	}
	for _, v := range u.Role.Perms {
		if v == p {
			return true
		}
	}
	return false
}
func (a *Core) CanShop(u User, id int64) bool {
	if u.Role.Key == "admin" {
		return true
	}
	if u.Role.Scope != "shops" {
		return false
	}
	var n int
	_ = a.DB.QueryRow("SELECT count(*) FROM user_shops WHERE user_id=? AND shop_id=?", u.ID, id).Scan(&n)
	return n > 0
}
func (a *Core) CanProduct(u User, id int64) bool {
	var n int
	if u.Role.Key == "admin" {
		_ = a.DB.QueryRow("SELECT count(*) FROM products WHERE id=?", id).Scan(&n)
	} else if u.Role.Scope == "shares" {
		_ = a.DB.QueryRow("SELECT count(*) FROM shares WHERE user_id=? AND product_id=?", u.ID, id).Scan(&n)
	} else {
		_ = a.DB.QueryRow("SELECT count(*) FROM products p WHERE p.id=? AND (p.created_by=? OR EXISTS(SELECT 1 FROM listings l JOIN user_shops us ON us.shop_id=l.shop_id WHERE l.product_id=p.id AND us.user_id=?))", id, u.ID, u.ID).Scan(&n)
	}
	return n > 0
}
func (a *Core) CanListing(u User, id int64) bool {
	var shop int64
	if a.DB.QueryRow("SELECT shop_id FROM listings WHERE id=?", id).Scan(&shop) != nil {
		return false
	}
	return a.CanShop(u, shop)
}
func (a *Core) RequireProduct(w http.ResponseWriter, r *http.Request, id int64, write bool) bool {
	if !a.CanProduct(CurrentUser(r), id) {
		Fail(w, 404, "商品不存在")
		return false
	}
	if write && a.ActiveProduct(id) {
		Fail(w, 409, "此商品正在排队或处理中，请等待任务完成")
		return false
	}
	return true
}
func (a *Core) RequireListing(w http.ResponseWriter, r *http.Request, id int64, write bool) bool {
	if !a.CanListing(CurrentUser(r), id) {
		Fail(w, 404, "上架版本不存在")
		return false
	}
	if write && a.Active("listing", id) {
		Fail(w, 409, "此上架版本正在排队或处理中")
		return false
	}
	return true
}
func (a *Core) CanUpload(u User, id int64) bool {
	if !u.Has(AssetUpload) || !a.CanProduct(u, id) {
		return false
	}
	if u.Role.Scope != "shares" {
		return true
	}
	var yes bool
	_ = a.DB.QueryRow("SELECT can_upload FROM shares WHERE product_id=? AND user_id=?", id, u.ID).Scan(&yes)
	return yes
}
