package core

import "net/http"

type Permission struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

const (
	ModelView        = "model.view"
	ModelManage      = "model.manage"
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
	RenderReview     = "render.review"
	AdminUsers       = "admin.users"
	AdminShops       = "admin.shops"
	AdminSettings    = "admin.settings"
	AdminAudit       = "admin.audit"
	ShareManage      = "share.manage"
	TaskView         = "task.view"
	TaskManage       = "task.manage"
	KBView           = "kb.view"
	KBEdit           = "kb.edit"
	InventoryView    = "inventory.view"
	InventoryEdit    = "inventory.edit"
	PurchaseManage   = "purchase.manage"
	OrderView        = "order.view"
	OrderManage      = "order.manage"
	FinanceView      = "finance.view"
	FinanceEdit      = "finance.edit"
	CreativeRun      = "creative.run"
	CreativeReview   = "creative.review"
	CreativeTemplate = "creative.template"
	MarketView       = "market.view"
	MarketImport     = "market.import"
	OpsAdvice        = "ops.advice"
)

// MarketIngest 只用于个人采集令牌的范围，不是角色权限点，不出现在 Permissions 里。
const MarketIngest = "market.ingest"

var Permissions = []Permission{
	{ModelView, "看店铺模特", "模特"}, {ModelManage, "维护店铺模特", "模特"},
	{ProductView, "看商品", "商品"}, {ProductCost, "看成本价和货源", "商品"}, {ProductEdit, "新建、修改商品和上架版本", "商品"}, {ProductDeleteOwn, "删除自己创建的商品", "商品"}, {ProductDeleteAny, "删除任何商品", "商品"},
	{AssetUpload, "上传照片", "素材"}, {AssetUpscale, "AI 高清化", "素材"}, {AssetProcess, "图片处理", "素材"}, {AssetDeleteOwn, "删除自己的素材", "素材"}, {AssetDeleteAny, "删除任何素材", "素材"},
	{CopyView, "看文案", "文案"}, {CopyEdit, "编辑文案", "文案"}, {TemplateManage, "管理模板", "模板"}, {RenderRun, "跑出图任务", "出图"}, {ExportRun, "导出、下载成品", "出图"}, {RenderReview, "审核出图结果", "出图"},
	{AdminUsers, "管理账号、角色、分店和额度", "管理"}, {AdminShops, "管理店铺", "管理"}, {AdminSettings, "修改系统设置", "管理"}, {AdminAudit, "看操作日志", "管理"}, {ShareManage, "分享商品给外部协作者", "管理"},
	{TaskView, "看任务、管自己的任务", "任务"}, {TaskManage, "给别人派活、改别人的任务、看负载", "任务"},
	{KBView, "看知识库", "知识库"}, {KBEdit, "编辑知识库", "知识库"},
	{InventoryView, "看库存和物料", "库存"}, {InventoryEdit, "出入库、调拨、盘点、管物料和仓库", "库存"},
	{PurchaseManage, "供应商、采购单、到货质检", "采购"},
	{OrderView, "看订单", "订单"}, {OrderManage, "录单、发货、取消、售后", "订单"},
	{FinanceView, "看财务数据", "财务"}, {FinanceEdit, "改财务数据", "财务"},
	{CreativeRun, "AI 出图：生成、重画、改字、导出", "AI 出图"}, {CreativeReview, "复核 AI 出图", "AI 出图"}, {CreativeTemplate, "管理生图模板和店铺出图设置", "AI 出图"},
	{MarketView, "看市场和店铺数据", "数据"}, {MarketImport, "导入数据、管理采集令牌", "数据"}, {OpsAdvice, "看运营建议、据此建任务", "数据"},
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
