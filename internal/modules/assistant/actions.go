package assistant

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"xstudio/internal/core"
)

type Action struct {
	Type     string `json:"type"`
	Label    string `json:"label"`
	Summary  string `json:"summary"`
	Params   object `json:"params"`
	Preview  object `json:"preview,omitempty"`
	CostHint string `json:"cost_hint,omitempty"`
}
type Ref struct {
	Type  string `json:"type"`
	ID    int64  `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
}

var actionCatalog = []capability{
	{"open", "", "打开前端页面；参数 {path: /、/products、/assets、/process、/copy、/templates、/review、/admin、/settings 及其子路由；/tasks 仅本身，可带查询参数，需要task.view}"},
	{"create_task", core.TaskView, "建议新建任务，确认后调用POST /api/tasks；参数 {title:1至100字,body?:最多1000字,assignee_id?:默认自己或0暂不派人,priority?:low或normal或high或urgent,due_at?:带时区RFC3339,target_type?:product或listing或asset,target_id?:对象编号（与类型同时给）,checklist?:最多20项{text:1至100字,done:false},watcher_ids?:最多10个不重复且不含负责人的可见用户编号}；派给别人或0需要task.manage"},
	{"run_listing", core.RenderRun, "从头一键出图；参数 {listing_id}"},
	{"render_listing", core.RenderRun, "按当前模板出图；参数 {listing_id}"},
	{"render_batch", core.RenderRun, "批量出图；参数 {listing_ids:最多50个}"},
	{"process_assets", core.AssetUpload, "图片处理；参数 {asset_ids:同一商品最多20个,ops:处理参数,preset_id?:预设编号}；基础操作需要asset.upload，upscale需要asset.upscale，ai需要asset.process（含ai时优先按ai判断）"},
	{"generate_copy", core.CopyEdit, "生成文案；参数 {listing_id,instructions?:要求}"},
	{"review", core.RenderReview, "审核出图；参数 {listing_id,status:approved或rejected,note:驳回时必填}"},
}

func actionText(u core.User) string {
	catalog := append([]capability{}, actionCatalog...)
	for i := range catalog {
		if catalog[i].name == "process_assets" {
			desc := "图片处理；参数 {asset_ids:同一商品最多20个,ops,preset_id?}；ops 可用：geometry:{rotate:0或90或180或270,crop?:[x,y,w,h]归一化坐标}、aspect:free或1:1或3:4或4:3或16:9、color:{auto_wb,match_to?:参考素材编号}、cutout:{background:white或transparent}、resize:{long_side:1至10000}、format:jpg或png"
			if u.Has(core.AssetUpscale) {
				desc += "、upscale:{target_long_side:2048或3000或4096}"
			}
			if u.Has(core.AssetProcess) {
				desc += "、ai:{quick?:[dust或glare或bright或wb或bg_white或bg_linen或bg_velvet或shadow],extra?:额外需求}（含ai时权限按ai优先判断）"
			}
			catalog[i].description = desc
		}
	}
	return catalogText(u, catalog)
}

func safePath(path string) bool {
	// Decode before checking so encoded slashes, protocols and traversal cannot
	// turn an allowed frontend route into an API or external URL.
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return false
	}
	if strings.Contains(decoded, "%") {
		return false
	}
	if !strings.HasPrefix(decoded, "/") || strings.Contains(decoded, "//") || strings.ContainsAny(decoded, "\\:") || strings.IndexFunc(decoded, unicode.IsControl) >= 0 {
		return false
	}
	u, err := url.Parse(decoded)
	if err != nil || u.IsAbs() || u.Host != "" {
		return false
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	if u.Path == "/" || u.Path == "/tasks" {
		return true
	}
	for _, base := range []string{"/products", "/assets", "/process", "/copy", "/templates", "/review", "/admin", "/settings"} {
		if u.Path == base || strings.HasPrefix(u.Path, base+"/") {
			return true
		}
	}
	return false
}

func (a *Module) validateAction(u core.User, raw json.RawMessage) (Action, bool) {
	var card Action
	if json.Unmarshal(raw, &card) != nil || card.Params == nil {
		return card, false
	}
	known := false
	for _, c := range actionCatalog {
		if c.name == card.Type {
			known = true
			if c.perm != "" && !u.Has(c.perm) {
				return card, false
			}
			break
		}
	}
	if !known {
		return card, false
	}
	params := json.RawMessage(core.JS(card.Params))
	card.CostHint = ""
	card.Preview = nil
	card.Label = clip(card.Label, 12)
	card.Summary = clip(card.Summary, 60)
	switch card.Type {
	case "create_task":
		// Preserve integer IDs exactly while validating and forwarding task params.
		var exact struct {
			Params object `json:"params"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if decoder.Decode(&exact) != nil {
			return card, false
		}
		card.Params = exact.Params
		return a.createTaskAction(u, card)
	case "open":
		var in struct {
			Path string `json:"path"`
		}
		if parseArgs(params, &in) != nil || !safePath(in.Path) || !a.visiblePath(u, in.Path) {
			return card, false
		}
	case "render_batch":
		var in struct {
			IDs []int64 `json:"listing_ids"`
		}
		if parseArgs(params, &in) != nil || !validIDs(in.IDs, 50) {
			return card, false
		}
		for _, id := range in.IDs {
			if !a.CanListing(u, id) {
				return card, false
			}
		}
	case "process_assets":
		return a.processAction(u, card)
	default:
		var in struct {
			ListingID    int64   `json:"listing_id"`
			Instructions *string `json:"instructions"`
			Status       *string `json:"status"`
			Note         *string `json:"note"`
		}
		if parseArgs(params, &in) != nil || in.ListingID < 1 || !a.CanListing(u, in.ListingID) {
			return card, false
		}
		// Only forward parameters accepted by the specific existing endpoint.
		allowed := map[string]bool{"listing_id": true}
		switch card.Type {
		case "generate_copy":
			allowed["instructions"] = true
		case "review":
			allowed["status"] = true
			allowed["note"] = true
			if in.Status == nil || (*in.Status != "approved" && *in.Status != "rejected") || (*in.Status == "rejected" && (in.Note == nil || strings.TrimSpace(*in.Note) == "")) {
				return card, false
			}
		}
		for key := range card.Params {
			if !allowed[key] {
				return card, false
			}
		}
	}
	return card, true
}

// open is also an object-bearing action when its route or filters identify an
// object. Apply the same visibility checks used by the other action cards.
func (a *Module) visiblePath(u core.User, path string) bool {
	decoded, _ := url.PathUnescape(path) // safePath has already checked encoding.
	parsed, _ := url.Parse(decoded)
	if parsed.Path == "/tasks" && !u.Has(core.TaskView) {
		return false
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return false
	}
	visible := func(kind, raw string) bool {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 1 {
			return false
		}
		_, _, ok := a.reference(u, kind, id)
		return ok
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) > 1 {
		for base, kind := range map[string]string{"products": "product", "assets": "asset"} {
			if parts[0] == base && !visible(kind, parts[1]) {
				return false
			}
		}
	}
	for key, kind := range map[string]string{"product": "product", "product_id": "product", "listing": "listing", "listing_id": "listing", "asset": "asset", "asset_id": "asset"} {
		for _, raw := range query[key] {
			if !visible(kind, raw) {
				return false
			}
		}
	}
	for _, key := range []string{"assets", "ids"} {
		for _, raw := range query[key] {
			for _, part := range strings.Split(raw, ",") {
				id, err := strconv.ParseInt(part, 10, 64)
				if err != nil || id < 1 {
					return false
				}
				if _, ok := a.assetProduct(u, id); !ok {
					return false
				}
			}
		}
	}
	for _, raw := range query["shop_id"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 1 || !a.CanShop(u, id) {
			return false
		}
	}
	return true
}

func validIDs(ids []int64, max int) bool {
	if len(ids) == 0 || len(ids) > max {
		return false
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id < 1 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// Ref paths and labels come from the visible object, never from model output.
func (a *Module) reference(u core.User, kind string, id int64) (path, label string, ok bool) {
	if id < 1 {
		return
	}
	switch kind {
	case "product":
		if !a.CanProduct(u, id) {
			return
		}
		ok = a.DB.QueryRow("SELECT name FROM products WHERE id=?", id).Scan(&label) == nil
		path = fmt.Sprintf("/products/%d", id)
	case "listing":
		if !a.CanListing(u, id) {
			return
		}
		var pid int64
		ok = a.DB.QueryRow("SELECT p.id,p.name || ' · ' || s.name FROM listings l JOIN products p ON p.id=l.product_id JOIN shops s ON s.id=l.shop_id WHERE l.id=?", id).Scan(&pid, &label) == nil
		path = fmt.Sprintf("/products/%d?listing=%d", pid, id)
	case "asset":
		if _, visible := a.assetProduct(u, id); !visible {
			return
		}
		ok = a.DB.QueryRow("SELECT coalesce(nullif(title,''),orig_name) FROM assets WHERE id=?", id).Scan(&label) == nil
		path = fmt.Sprintf("/assets?asset=%d", id)
	case "job":
		var target string
		var targetID, userID int64
		if a.DB.QueryRow("SELECT target_type,target_id,user_id,kind FROM jobs WHERE id=?", id).Scan(&target, &targetID, &userID, &label) != nil {
			return
		}
		if target != "job" {
			path, _, ok = a.reference(u, target, targetID)
		}
		ok = ok || userID == u.ID
		if path == "" {
			path = "/"
		}
	case "user":
		if id == u.ID {
			path = "/settings"
			label = u.DisplayName
			ok = true
		}
	}
	return
}
