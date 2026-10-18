package assistant

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
	"xstudio/internal/core"
)

func TestToolsProductScopeCostAssetsAndJobs(t *testing.T) {
	f := newFixture(t)
	shop := f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES('隐藏店','other','en',?)", core.Now())
	p1, l1 := f.product(t, "珠蝶", 1)
	p2, l2 := f.product(t, "隐藏款", shop)
	a1 := f.asset(t, p1, "original")
	f.asset(t, p1, "ai_enhanced")
	a3 := f.asset(t, p2, "original")
	uid := f.user(t, "reader", "shops", []string{core.ProductView}, 1)
	guest := f.user(t, "shared", "shares", []string{core.ProductView})
	f.exec(t, "INSERT INTO shares(product_id,user_id,created_by,created_at) VALUES(?,?,1,?)", p1, guest, core.Now())
	// Even accidental shop assignments must not expand a shares role's scope.
	f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(?,?)", guest, shop)
	f.exec(t, "UPDATE listings SET pipeline_error='分拣失败' WHERE id=?", l1)
	for _, v := range []struct {
		kind     string
		id, user int64
	}{{"listing", l1, 1}, {"product", p1, 1}, {"asset", a1, 1}, {"listing", l2, 1}, {"asset", a3, 1}, {"product", p2, uid}} {
		f.exec(t, "INSERT INTO jobs(kind,target_type,target_id,user_id,status,message,created_at,finished_at,steps) VALUES('test',?,?,?,'failed','失败了',?,?,'')", v.kind, v.id, v.user, core.Now(), core.Now())
	}
	for _, user := range []int64{uid, guest} {
		items := f.tool(t, user, "search_products", object{"q": "珠", "limit": 20}).([]object)
		if len(items) != 1 || number(items[0]["id"]) != p1 || number(items[0]["asset_count"]) != 2 {
			t.Fatal(items)
		}
		product := f.tool(t, user, "get_product", object{"id": p1}).(object)
		if product["cost"] != nil || product["supplier"] != nil || strings.Contains(core.JS(product), "秘密货源") {
			t.Fatal(product)
		}
		if product["assets"].(object)["original"] != int64(1) || product["assets"].(object)["ai_enhanced"] != int64(1) {
			t.Fatal(product)
		}
		wantListings := 1
		if user == guest {
			wantListings = 0
		}
		if len(product["listings"].([]object)) != wantListings {
			t.Fatal(product)
		}
		assets := f.tool(t, user, "list_assets", object{"product_id": p1, "source": "original", "tag": "耳钉", "limit": 1}).([]object)
		if len(assets) != 1 || number(assets[0]["id"]) != a1 {
			t.Fatal(assets)
		}
		u, _ := f.c.User(user)
		for _, name := range []string{"get_product", "list_assets"} {
			args := object{"id": p2}
			if name == "list_assets" {
				args = object{"product_id": p2}
			}
			if _, err := f.m.tool(u, name, json.RawMessage(core.JS(args))); err == nil {
				t.Fatalf("%s leaked hidden product", name)
			}
		}
	}
	admin := f.tool(t, 1, "get_product", object{"id": p1}).(object)
	if admin["cost"] == nil || admin["supplier"] == nil || admin["listings"].([]object)[0]["latest_job"].(object)["message"] != "失败了" {
		t.Fatal(admin)
	}
	jobs := f.tool(t, uid, "recent_jobs", object{"status": "failed"}).([]object)
	if len(jobs) != 4 {
		t.Fatal(jobs)
	}
	// Own jobs are included even when the target is no longer visible, but the
	// hidden target's name must not be fetched into the result.
	if jobs[0]["target_name"] != "" {
		t.Fatal(jobs[0])
	}
	sharedJobs := f.tool(t, guest, "recent_jobs", object{}).([]object)
	if len(sharedJobs) != 2 {
		t.Fatal(sharedJobs)
	}
	if matches := f.tool(t, 1, "search_products", object{"q": "P001"}).([]object); len(matches) != 2 {
		t.Fatal(matches)
	}
}

func TestReviewQueueMatchesStudioAndTemplates(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.c.Cfg.TemplateDir, "详情页模板")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"模板.html", "白.html"} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("<html></html>"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := object{"默认": "navy", "模板": []object{{"key": "navy", "name": "蓝色", "file": "模板.html"}, {"key": "white", "name": "白色", "file": "白.html"}, {"key": "evil", "name": "不允许", "file": "../outside.html"}}}
	if err := os.WriteFile(filepath.Join(dir, "模板清单.json"), []byte(core.JS(manifest)), 0600); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "INSERT INTO template_prefs(shop_id,default_detail,disabled,updated_at) VALUES(1,'white','[\"navy\"]',?)", core.Now())
	for i := 0; i < 23; i++ {
		_, lid := f.product(t, fmt.Sprintf("款%d", i), 1)
		f.exec(t, "UPDATE listing_render SET review_status='pending',updated_at=? WHERE listing_id=?", fmt.Sprintf("2026-10-%02dT00:00:00Z", i+1), lid)
	}
	queue := f.req(t, "GET", "/api/review/queue?page_size=20", nil, 200)
	fromTool := f.tool(t, 1, "review_queue", object{"shop_id": 1}).([]object)
	var normalized any
	_ = json.Unmarshal([]byte(core.JS(fromTool)), &normalized)
	if len(fromTool) != 20 || core.JS(normalized) != core.JS(queue["items"]) {
		t.Fatal("review tool differs from studio", core.JS(normalized), core.JS(queue["items"]))
	}
	home := f.req(t, "GET", "/api/home", nil, 200)
	if home["counts"].(map[string]any)["review_pending"] != queue["total"] {
		t.Fatal(home, queue)
	}
	tpls := f.tool(t, 1, "list_templates", object{"shop_id": 1}).(object)
	detail := tpls["detail"].([]object)
	if len(detail) != 2 || detail[0]["disabled"] != true || detail[1]["is_default"] != true {
		t.Fatal(tpls)
	}
	f.user(t, "viewer", "shops", []string{core.ProductView}, 1)
	f.login(t, "viewer")
	if _, err := f.m.tool(mustUser(t, f, 2), "review_queue", json.RawMessage(`{}`)); err == nil {
		t.Fatal("review permission bypass")
	}
}

func mustUser(t *testing.T, f *fixture, id int64) core.User {
	t.Helper()
	u, err := f.c.User(id)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestPresetsUsageToolPermissionsAndParameters(t *testing.T) {
	f := newFixture(t)
	uid := f.user(t, "uploader", "shops", []string{core.ProductView, core.AssetUpload}, 1)
	guest := f.user(t, "guest", "shares", []string{core.AssetUpload})
	for _, v := range []struct {
		owner  int64
		shared int
	}{{1, 0}, {1, 1}, {uid, 0}, {guest, 0}} {
		f.exec(t, "INSERT INTO asset_presets(name,owner_id,shared,params,created_at,updated_at) VALUES('预设',?,?,?, ?,?)", v.owner, v.shared, `{"ops":{"resize":{"long_side":800}}}`, core.Now(), core.Now())
	}
	if items := f.tool(t, uid, "list_presets", object{}).([]object); len(items) != 2 || items[0]["ops"] == nil || items[0]["shared"] != true {
		t.Fatal(items)
	}
	if items := f.tool(t, guest, "list_presets", object{}).([]object); len(items) != 1 || number(items[0]["id"]) != 4 {
		t.Fatal(items)
	}
	f.exec(t, "UPDATE quotas SET month_text=10,month_images=NULL WHERE user_id=?", uid)
	for _, kind := range []string{"text", "text", "image"} {
		f.exec(t, "INSERT INTO ai_usage(at,user_id,feature,model,kind,units,est_cost) VALUES(?,?,'test','fake',?,1,0)", core.Now(), uid, kind)
	}
	f.exec(t, "INSERT INTO ai_usage(at,user_id,feature,model,kind,units,est_cost) VALUES('2001-01-01T00:00:00Z',?,'test','fake','text',100,0)", uid)
	usage := f.tool(t, uid, "my_usage", object{}).(object)
	if usage["text"].(object)["used"] != int64(2) || usage["text"].(object)["quota"] != int64(10) || usage["image"].(object)["quota"] != nil || usage["image"].(object)["used"] != int64(1) {
		t.Fatal(usage)
	}
	u := mustUser(t, f, guest)
	for _, name := range []string{"search_products", "get_product", "list_assets", "review_queue", "list_templates"} {
		if _, err := f.m.tool(u, name, json.RawMessage(`{}`)); err == nil {
			t.Fatal("missing tool permission", name)
		}
	}
	u = mustUser(t, f, uid)
	for _, test := range []struct{ name, args string }{{"unknown", "{}"}, {"today", `{"unexpected":1}`}, {"get_product", `{"id":"1"}`}, {"get_product", `{}`}, {"list_assets", `{"product_id":1,"limit":51}`}, {"search_products", `{"limit":21}`}, {"search_products", `{"limit":0}`}, {"search_products", `{"status":"anything"}`}, {"search_products", `{"shop_id":999}`}, {"recent_jobs", `{"status":"queued"}`}, {"recent_jobs", `{"limit":21}`}, {"list_templates", `{"shop_id":999}`}} {
		if _, err := f.m.tool(u, test.name, json.RawMessage(test.args)); err == nil {
			t.Fatal("invalid tool args accepted", test)
		}
	}
}

func TestChatToolErrorsAndResultTruncation(t *testing.T) {
	f := newFixture(t, object{"tool_calls": []object{{"name": "unknown", "args": object{}}, {"name": "review_queue", "args": object{}}, {"name": "get_product", "args": object{"id": "oops"}}, {"name": "get_product", "args": object{"id": 1}}}}, object{"reply": "已查到"})
	f.product(t, "珠蝶", 1)
	f.exec(t, "UPDATE products SET info=?", core.JS(object{"description": strings.Repeat("大", 9000)}))
	f.user(t, "viewer", "shops", []string{core.ProductView}, 1)
	f.login(t, "viewer")
	f.req(t, "POST", "/api/assistant/chat", chatBody("查询"), 200)
	h := aiHistory(t, f.ai.snapshot()[1])
	results := h[len(h)-4:]
	for i, want := range []string{"未知工具", "没有此工具的权限", "参数字段或类型不正确"} {
		if !strings.Contains(results[i]["content"].(string), want) {
			t.Fatal(results)
		}
	}
	if utf8.RuneCountInString(results[3]["content"].(string)) != 6000 {
		t.Fatal("tool result not truncated")
	}
}

func TestChatAtMostFourToolsPerRound(t *testing.T) {
	f := newFixture(t, object{"tool_calls": []object{{"name": "today", "args": object{}}, {"name": "today", "args": object{}}, {"name": "today", "args": object{}}, {"name": "today", "args": object{}}, {"name": "today", "args": object{}}}}, object{"reply": "好"})
	f.req(t, "POST", "/api/assistant/chat", chatBody("好"), 200)
	h := aiHistory(t, f.ai.snapshot()[1])
	if len(h) != 2 || !strings.Contains(h[1]["content"].(string), "1–4") {
		t.Fatal(h)
	}
}

func TestSQLScopesMatchCore(t *testing.T) {
	f := newFixture(t)
	shop := f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES('另一店','other','en',?)", core.Now())
	p1, _ := f.product(t, "分店商品", 1)
	p2, _ := f.product(t, "本人商品", shop)
	p3, _ := f.product(t, "未分享商品", shop)
	operator := f.user(t, "operator2", "shops", []string{core.ProductView}, 1)
	guest := f.user(t, "guest2", "shares", []string{core.ProductView})
	f.exec(t, "UPDATE products SET created_by=? WHERE id IN (?,?)", operator, p2, p3)
	f.exec(t, "INSERT INTO shares(product_id,user_id,created_by,created_at) VALUES(?,?,1,?)", p2, guest, core.Now())
	f.exec(t, "INSERT INTO user_shops(user_id,shop_id) VALUES(?,1)", guest)
	f.exec(t, "UPDATE products SET created_by=? WHERE id=?", guest, p3)
	for _, uid := range []int64{1, operator, guest} {
		u := mustUser(t, f, uid)
		ps, pa := productScope(u)
		rows, err := core.QueryObjects(f.c.DB, "SELECT p.id FROM products p WHERE "+ps, pa...)
		if err != nil {
			t.Fatal(err)
		}
		visible := map[int64]bool{}
		for _, v := range rows {
			visible[number(v["id"])] = true
		}
		for _, pid := range []int64{p1, p2, p3} {
			if visible[pid] != f.c.CanProduct(u, pid) {
				t.Fatal("product SQL scope differs", uid, pid)
			}
		}
		ls, la := listingScope(u)
		rows, err = core.QueryObjects(f.c.DB, "SELECT l.id FROM listings l WHERE "+ls, la...)
		if err != nil {
			t.Fatal(err)
		}
		visible = map[int64]bool{}
		for _, v := range rows {
			visible[number(v["id"])] = true
		}
		for lid := int64(1); lid <= 3; lid++ {
			if visible[lid] != f.c.CanListing(u, lid) {
				t.Fatal("listing SQL scope differs", uid, lid)
			}
		}
	}
}

func TestToolNestedAssetReferencesStayVisible(t *testing.T) {
	f := newFixture(t)
	shop := f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES('隐藏店','other','en',?)", core.Now())
	p1, _ := f.product(t, "珠蝶", 1)
	p2, _ := f.product(t, "隐藏款", shop)
	a1 := f.asset(t, p1, "original")
	a2 := f.asset(t, p2, "original")
	uid := f.user(t, "viewer", "shops", []string{core.ProductView, core.AssetUpload}, 1)
	f.exec(t, "UPDATE assets SET parent_id=? WHERE id=?", a2, a1)
	f.exec(t, "INSERT INTO asset_presets(name,owner_id,shared,params,created_at,updated_at) VALUES('校色',1,1,?, ?,?)", core.JS(object{"ops": object{"color": object{"match_to": a2}}}), core.Now(), core.Now())
	items := f.tool(t, uid, "list_assets", object{"product_id": p1}).([]object)
	if len(items) != 1 || items[0]["parent_id"] != nil {
		t.Fatal(items)
	}
	presets := f.tool(t, uid, "list_presets", object{}).([]object)
	if len(presets) != 0 {
		t.Fatal("preset leaked an invisible reference", presets)
	}
}
