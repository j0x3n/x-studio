package copywriting_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xstudio/internal/core"
	"xstudio/internal/modules/market"
)

func newTitleTestEnv(t *testing.T, handler http.HandlerFunc) *testEnv {
	t.Helper()
	baseURL := ""
	if handler != nil {
		ai := httptest.NewServer(handler)
		t.Cleanup(ai.Close)
		baseURL = ai.URL
	}
	env := newTestEnv(t, func(cfg *core.Config) { cfg.AIBaseURL = baseURL })
	tx, err := env.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, migration := range market.New(env.c).Migrations() {
		if err := migration.Up(tx); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.c.Cfg.PromptDir, "标题-优化.md"), []byte("为{{category_name}}写三个标题。{{category_rules}} 禁用{{banned_words}}"), 0600); err != nil {
		t.Fatal(err)
	}
	env.login(t, "admin", "AdminPassword123!")
	return env
}

func titleExec(t *testing.T, env *testEnv, query string, args ...any) {
	t.Helper()
	if _, err := env.c.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func titleListing(t *testing.T, env *testEnv) int64 {
	t.Helper()
	product := env.req(t, "POST", "/api/products", Object{"name": "月光耳环", "shop_ids": []int64{1}, "info": Object{"price": 88, "highlight": "通勤"}}, 201)["product"].(Object)
	id := int64(product["listings"].([]any)[0].(Object)["id"].(float64))
	titleExec(t, env, "UPDATE listings SET meta=?,updated_at=? WHERE id=?", core.JS(Object{"title": "旧标题", "sku": []any{Object{"name": "银针款", "price": 99}}, "attrs": []any{[]any{"颜色", "白色"}}, "custom": Object{"keep": true}}), "2000-01-01T00:00:00Z", id)
	return id
}

func titleReply(w http.ResponseWriter, titles ...string) {
	items := []Object{}
	for _, title := range titles {
		items = append(items, Object{"title": title, "keywords": []string{"AI编的词"}, "focus": "覆盖", "note": "测试建议", "chars": 999})
	}
	core.WriteJSON(w, 200, Object{"choices": []Object{{"message": Object{"content": core.JS(Object{"titles": items})}}}})
}

func TestTitleSuggestInputsAndChecks(t *testing.T) {
	requests := make(chan Object, 2)
	env := newTitleTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		var body Object
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests <- body
		titleReply(w, " 真淡水珍珠耳环925 送礼 ", "最疗愈复古", strings.Repeat("长", 31))
	})
	id := titleListing(t, env)
	shop2 := int64(env.req(t, "POST", "/api/shops", Object{"name": "二号店", "platform": "taobao", "language": "zh-CN"}, 201)["shop"].(Object)["id"].(float64))
	today := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	date := func(days int) string { return today.AddDate(0, 0, days).Format("2006-01-02") }
	for _, keyword := range []struct {
		shop, volume, cvr int64
		category, word    string
	}{
		{1, 10000, 1000, "pearl", "珍珠"}, {1, 1000, 5000, "pearl", "耳环"},
		{1, 100, 10000, "pearl", "银针"}, {shop2, 100000, 10000, "pearl", "别店词"},
		{1, 100000, 10000, "crystal", "别品类"},
	} {
		titleExec(t, env, "INSERT INTO market_keywords(shop_id,category,word,date,search_volume,cvr_bps,updated_at) VALUES(?,?,?,?,?,?,?)", keyword.shop, keyword.category, keyword.word, date(0), keyword.volume, keyword.cvr, core.Now())
	}
	for _, word := range []string{"珍珠", "送礼"} {
		env.req(t, "POST", "/api/keywords", Object{"shop_id": 1, "word": word, "grp": "品类词", "weight": 10}, 201)
	}
	env.req(t, "POST", "/api/keywords", Object{"shop_id": shop2, "word": "别店自有词", "grp": "品类词"}, 201)
	env.req(t, "POST", "/api/keywords", Object{"shop_id": 1, "word": "包过检", "grp": "禁用词"}, 201)
	if err := os.WriteFile(filepath.Join(env.c.Cfg.PromptDir, "违禁词.txt"), []byte("error\t疗愈\t功效承诺\nwarn\t复古\t需核实风格\n"), 0600); err != nil {
		t.Fatal(err)
	}
	competitor := "真淡水珍珠耳环925 经典款"
	for index, item := range []struct {
		shop        int64
		day         int
		word, title string
		rank        int
	}{
		{1, 0, "珍珠", competitor, 2}, {1, -29, "送礼", "月光送礼耳饰", 1},
		{1, -1, "耳环", competitor, 3}, {shop2, 0, "珍珠", "别店竞品", 1},
		{1, -30, "珍珠", "过期竞品", 1}, {1, 1, "珍珠", "未来竞品", 1},
		{1, 0, "无关词", "无关竞品", 1},
	} {
		titleExec(t, env, "INSERT INTO competitor_items(shop_id,platform_item_id,keyword,title,rank,date) VALUES(?,?,?,?,?,?)", item.shop, fmt.Sprint(index), item.word, item.title, item.rank, date(item.day))
	}
	for _, item := range []struct {
		day   int
		terms string
	}{
		{0, `["珍珠",{"term":"送礼","uv":3,"orders":1},{"word":"耳环","uv":4,"orders":2},null,42]`},
		{-1, `[{"term":"珍珠","uv":10,"orders":2},{"word":"送礼","uv":2,"orders":1}]`},
		{-29, `["边界词"]`}, {-30, `["过期词"]`}, {1, `["未来词"]`},
	} {
		titleExec(t, env, "INSERT INTO listing_daily(listing_id,date,search_terms,impressions,clicks,orders,source) VALUES(?,?,?,100,10,1,'test')", id, date(item.day), item.terms)
	}
	changed := today.AddDate(0, 0, -10)
	titleExec(t, env, "INSERT INTO listing_title_history(listing_id,old_title,new_title,created_by,created_at) VALUES(?,?,?,?,?)", id, "更早标题", "旧标题", 1, changed.Format(time.RFC3339Nano))
	for _, day := range []int{-18, -17, -11, -10, -9, -3, -2} {
		titleExec(t, env, "INSERT INTO listing_daily(listing_id,date,impressions,clicks,orders,source) VALUES(?,?,100,10,1,'test')", id, date(day))
	}
	result := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/titles/suggest", id), Object{"note": "保留银针规格"}, 200)
	titles := result["titles"].([]any)
	first := titles[0].(Object)
	if len(titles) != 3 || first["title"] != "真淡水珍珠耳环925 送礼" || first["chars"] != float64(22) || first["over_limit"] != false || first["overlap_bps"] != float64(8333) || first["overlap_with"] != competitor {
		t.Fatalf("computed title: %v", result)
	}
	covered := first["keywords"].([]any)
	if len(covered) != 3 || covered[0].(Object)["word"] != "珍珠" || covered[0].(Object)["score"] != float64(1000) || covered[1].(Object)["word"] != "耳环" || covered[2].(Object)["word"] != "送礼" || covered[2].(Object)["score"] != nil {
		t.Fatalf("covered keywords: %v", covered)
	}
	if issues := first["compliance"].([]any); len(issues) != 1 || issues[0].(Object)["level"] != "warn" {
		t.Fatalf("overlap warning: %v", issues)
	}
	issues := titles[1].(Object)["compliance"].([]any)
	levels := map[string]any{}
	for _, issue := range issues {
		levels[issue.(Object)["word"].(string)] = issue.(Object)["level"]
	}
	if len(issues) != 3 || levels["疗愈"] != "error" || levels["复古"] != "warn" || levels["最"] != "error" {
		t.Fatalf("compliance: %v", issues)
	}
	third := titles[2].(Object)
	if third["chars"] != float64(62) || third["over_limit"] != true || third["compliance"].([]any)[0].(Object)["level"] != "error" {
		t.Fatalf("long title: %v", third)
	}
	if core.JS(result["inputs"]) != core.JS(Object{"keywords": 4, "competitors": 2, "search_terms": 4, "history": 1}) || result["change_allowed"] != true || result["next_change_at"] != "" {
		t.Fatalf("input counts: %v", result)
	}
	body := <-requests
	messages := body["messages"].([]any)
	if system := messages[0].(Object)["content"].(string); !strings.Contains(system, "珍珠") || strings.Contains(system, "{{") {
		t.Fatal(system)
	}
	var input Object
	if err := json.Unmarshal([]byte(messages[1].(Object)["content"].(string)), &input); err != nil {
		t.Fatal(err)
	}
	listing := input["listing"].(Object)
	if input["note"] != "保留银针规格" || listing["name"] != "月光耳环" || listing["title"] != "旧标题" || listing["price"] != float64(88) || listing["sku"].([]any)[0].(Object)["name"] != "银针款" || input["info"].(Object)["highlight"] != "通勤" {
		t.Fatalf("listing input: %v", input)
	}
	if competitors := input["competitors"].([]any); len(competitors) != 2 || competitors[0].(Object)["title"] != "月光送礼耳饰" || competitors[1].(Object)["title"] != competitor {
		t.Fatalf("competitors: %v", competitors)
	}
	terms := map[string]Object{}
	for _, item := range input["search_terms"].([]any) {
		term := item.(Object)
		terms[term["term"].(string)] = term
	}
	if len(terms) != 4 || terms["珍珠"]["orders"] != float64(2) || terms["送礼"]["uv"] != float64(5) || terms["送礼"]["orders"] != float64(2) || terms["耳环"]["orders"] != float64(2) {
		t.Fatalf("search terms: %v", terms)
	}
	history := input["history"].([]any)[0].(Object)
	for _, period := range []string{"before", "after"} {
		if core.JS(history[period]) != core.JS(Object{"impressions": 200, "clicks": 20, "orders": 2}) {
			t.Fatalf("history %s: %v", period, history)
		}
	}
	var meta, updated, audit string
	if err := env.c.DB.QueryRow("SELECT meta,updated_at FROM listings WHERE id=?", id).Scan(&meta, &updated); err != nil {
		t.Fatal(err)
	}
	if core.JSONObj(meta)["title"] != "旧标题" || updated != "2000-01-01T00:00:00Z" {
		t.Fatalf("suggest modified listing: %s %s", meta, updated)
	}
	if err := env.c.DB.QueryRow("SELECT detail FROM audit_log WHERE action='title.suggest' AND target_id=?", id).Scan(&audit); err != nil || core.JS(core.JSONObj(audit)["inputs"]) != core.JS(result["inputs"]) {
		t.Fatalf("audit: %s %v", audit, err)
	}
	var feature string
	if err := env.c.DB.QueryRow("SELECT feature FROM ai_usage ORDER BY id DESC LIMIT 1").Scan(&feature); err != nil || feature != "title_suggest" {
		t.Fatalf("AI attribution: %s %v", feature, err)
	}
	t.Logf("suggest response: %s", core.JS(result))
}

func TestTitleSuggestRetryAndEmptyInputs(t *testing.T) {
	for _, test := range []struct {
		name          string
		first, second []string
		status, calls int
	}{
		{"valid", []string{"耳饰", "通勤", "送礼"}, nil, 200, 1},
		{"retry_count", []string{"耳饰", "通勤"}, []string{"耳饰", "通勤", "送礼"}, 200, 2},
		{"retry_empty", []string{"耳饰", "  ", "送礼"}, []string{"耳饰", "通勤", "送礼"}, 200, 2},
		{"invalid_twice", []string{"耳饰", "通勤"}, []string{"耳饰", "通勤"}, 502, 2},
		{"empty_twice", []string{"耳饰", "", "送礼"}, []string{"耳饰", "", "送礼"}, 502, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			env := newTitleTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					titleReply(w, test.first...)
				} else {
					titleReply(w, test.second...)
				}
			})
			id := titleListing(t, env)
			result := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/titles/suggest", id), nil, test.status)
			if int(calls.Load()) != test.calls {
				t.Fatalf("AI calls: %d", calls.Load())
			}
			if test.status == 502 {
				if result["error"] != "AI 返回的标题格式不对" {
					t.Fatal(result)
				}
				return
			}
			if core.JS(result["inputs"]) != core.JS(Object{"keywords": 0, "competitors": 0, "search_terms": 0, "history": 0}) {
				t.Fatal(result)
			}
			for _, item := range result["titles"].([]any) {
				title := item.(Object)
				if title["overlap_bps"] != float64(0) || title["overlap_with"] != "" || len(title["keywords"].([]any)) != 0 || len(title["compliance"].([]any)) != 0 {
					t.Fatal(title)
				}
			}
			if test.name == "valid" {
				t.Logf("empty-input suggest response: %s", core.JS(result))
			}
		})
	}
}

func TestTitleApplyValidationHistoryAndCooldown(t *testing.T) {
	env := newTitleTestEnv(t, nil)
	id := titleListing(t, env)
	content := Object{"story": "原来的商品文案", "selling": []any{"保留卖点"}}
	main := Object{"headline": "原来的主图文案"}
	titleExec(t, env, "UPDATE listings SET content=?,main=? WHERE id=?", core.JS(content), core.JS(main), id)
	titleExec(t, env, "INSERT INTO copy_versions(listing_id,title,content,main,meta,source,note,created_by,created_at,applied_at) SELECT id,'旧标题',content,main,meta,'manual','原版本',1,updated_at,updated_at FROM listings WHERE id=?", id)
	previous := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", id), nil, 200)["current"]
	titleExec(t, env, "UPDATE categories SET banned=? WHERE id=1", core.JS([]string{"天然"}))
	path := fmt.Sprintf("/api/listings/%d/titles/apply", id)
	for _, title := range []string{" ", strings.Repeat("长", 31), "最美耳环", "第一耳环", "顶级耳环", "极品耳环", "唯一耳环", "100%耳环", "绝对耳环", "万能耳环", "国家级耳环", "疗愈耳环", "天然耳环"} {
		result := env.req(t, "POST", path, Object{"title": title}, 400)
		issues, ok := result["compliance"].([]any)
		if !ok || (strings.TrimSpace(title) != "" && (len(issues) == 0 || issues[0].(Object)["level"] != "error")) {
			t.Fatalf("validation %q: %v", title, result)
		}
	}
	if result := env.req(t, "POST", path, Object{"title": " 旧标题 "}, 400); result["error"] != "和现在的标题一样" {
		t.Fatal(result)
	}
	env.req(t, "POST", path, Object{"title": "新标题", "reason": strings.Repeat("长", 201)}, 400)
	env.req(t, "POST", fmt.Sprintf("/api/listings/%d/titles/suggest", id), Object{"note": strings.Repeat("长", 201)}, 400)
	if err := os.WriteFile(filepath.Join(env.c.Cfg.PromptDir, "违禁词.txt"), []byte("warn\t复古\t核实风格\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result := env.req(t, "POST", path, Object{"title": " 复古珍珠耳环 ", "reason": strings.Repeat("好", 200)}, 200)
	if result["title"] != "复古珍珠耳环" || result["old_title"] != "旧标题" {
		t.Fatal(result)
	}
	next, err := time.Parse(time.RFC3339, result["next_change_at"].(string))
	if err != nil || time.Until(next) < 7*24*time.Hour-time.Minute || time.Until(next) > 7*24*time.Hour {
		t.Fatalf("next change: %v %v", result, err)
	}
	var meta, updated, audit string
	if err := env.c.DB.QueryRow("SELECT meta,updated_at FROM listings WHERE id=?", id).Scan(&meta, &updated); err != nil {
		t.Fatal(err)
	}
	listing := core.JSONObj(meta)
	if listing["title"] != "复古珍珠耳环" || listing["custom"].(Object)["keep"] != true || len(listing["sku"].([]any)) != 1 || len(listing["attrs"].([]any)) != 1 || updated == "2000-01-01T00:00:00Z" {
		t.Fatalf("listing meta: %s %s", meta, updated)
	}
	copyID, ok := result["copy_id"].(float64)
	if !ok || copyID <= 0 || result["copy_id"] == previous {
		t.Fatalf("new copy ID: %v (previous %v)", result, previous)
	}
	copies := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", id), nil, 200)
	if copies["current"] != copyID || copies["total"] != float64(2) || copies["items"].([]any)[0].(Object)["id"] != copyID {
		t.Fatalf("current copy: %v", copies)
	}
	version := env.req(t, "GET", fmt.Sprintf("/api/copies/%d", int64(copyID)), nil, 200)["copy"].(Object)
	if version["title"] != "复古珍珠耳环" || version["source"] != "manual" || version["created_by"] != float64(1) || version["created_at"] != updated || version["applied_at"] != updated || version["note"] != string([]rune("标题建议：" + strings.Repeat("好", 200))[:200]) {
		t.Fatalf("saved copy: %v", version)
	}
	if core.JS(version["meta"]) != core.JS(listing) || core.JS(version["content"]) != core.JS(content) || core.JS(version["main"]) != core.JS(main) {
		t.Fatalf("copy must match current listing and preserve content/main: %v", version)
	}
	if err := env.c.DB.QueryRow("SELECT detail FROM audit_log WHERE action='title.apply' AND target_id=?", id).Scan(&audit); err != nil || core.JSONObj(audit)["old_title"] != "旧标题" || core.JSONObj(audit)["new_title"] != "复古珍珠耳环" || core.JSONObj(audit)["reason"] != strings.Repeat("好", 200) {
		t.Fatalf("apply audit: %s %v", audit, err)
	}
	history := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/titles/history", id), nil, 200)
	items := history["items"].([]any)
	if len(items) != 1 || items[0].(Object)["old_title"] != "旧标题" || items[0].(Object)["new_title"] != "复古珍珠耳环" || items[0].(Object)["created_by"] != float64(1) || items[0].(Object)["created_by_name"] != "admin" || items[0].(Object)["reason"] != strings.Repeat("好", 200) || history["change_allowed"] != false || history["next_change_at"] != result["next_change_at"] {
		t.Fatal(history)
	}
	blocked := env.req(t, "POST", path, Object{"title": "送礼耳环"}, 409)
	if blocked["error"] != "距上次改标题不满 7 天" || blocked["next_change_at"] != result["next_change_at"] {
		t.Fatal(blocked)
	}
	titleExec(t, env, "UPDATE listing_title_history SET created_at=? WHERE listing_id=?", time.Now().Add(-8*24*time.Hour).Format(time.RFC3339Nano), id)
	history = env.req(t, "GET", fmt.Sprintf("/api/listings/%d/titles/history", id), nil, 200)
	if history["change_allowed"] != true || history["next_change_at"] != "" {
		t.Fatal(history)
	}
	second := env.req(t, "POST", path, Object{"title": "送礼耳环"}, 200)
	version = env.req(t, "GET", fmt.Sprintf("/api/copies/%d", int64(second["copy_id"].(float64))), nil, 200)["copy"].(Object)
	copies = env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", id), nil, 200)
	if version["note"] != "标题建议" || version["meta"].(Object)["title"] != "送礼耳环" || core.JS(version["content"]) != core.JS(content) || copies["current"] != second["copy_id"] {
		t.Fatalf("copy without reason: %v, copies: %v", version, copies)
	}
	t.Logf("apply response: %s", core.JS(result))
	t.Logf("history response: %s", core.JS(Object{"items": items, "change_allowed": false, "next_change_at": result["next_change_at"]}))
}

func TestTitleApplyAtomicAndActiveProduct(t *testing.T) {
	env := newTitleTestEnv(t, nil)
	id := titleListing(t, env)
	path := fmt.Sprintf("/api/listings/%d/titles/apply", id)
	titleExec(t, env, `CREATE TRIGGER reject_title_audit BEFORE INSERT ON audit_log WHEN NEW.action='title.apply' BEGIN SELECT RAISE(ABORT,'test audit failure'); END`)
	env.req(t, "POST", path, Object{"title": "新标题"}, 500)
	var title string
	var count int
	if err := env.c.DB.QueryRow("SELECT json_extract(meta,'$.title') FROM listings WHERE id=?", id).Scan(&title); err != nil || title != "旧标题" {
		t.Fatalf("rollback title: %s %v", title, err)
	}
	if err := env.c.DB.QueryRow("SELECT COUNT(*) FROM listing_title_history WHERE listing_id=?", id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback history: %d %v", count, err)
	}
	if err := env.c.DB.QueryRow("SELECT COUNT(*) FROM copy_versions WHERE listing_id=?", id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback copy versions: %d %v", count, err)
	}
	titleExec(t, env, "DROP TRIGGER reject_title_audit")
	titleExec(t, env, "INSERT INTO jobs(kind,target_type,target_id,user_id,status,created_at,steps) VALUES('copy.generate','listing',?,1,'running',?,'copy.generate')", id, core.Now())
	env.req(t, "POST", path, Object{"title": "新标题"}, 409)
	titleExec(t, env, "DELETE FROM jobs WHERE target_id=? AND target_type='listing'", id)
	shop2 := int64(env.req(t, "POST", "/api/shops", Object{"name": "二号店", "platform": "taobao", "language": "zh-CN"}, 201)["shop"].(Object)["id"].(float64))
	titleExec(t, env, "INSERT INTO listings(product_id,shop_id,created_by,created_at,updated_at) SELECT product_id,?,1,?,? FROM listings WHERE id=?", shop2, core.Now(), core.Now(), id)
	titleExec(t, env, "INSERT INTO jobs(kind,target_type,target_id,user_id,status,created_at,steps) SELECT 'copy.generate','listing',id,1,'running',?,'copy.generate' FROM listings WHERE shop_id=?", core.Now(), shop2)
	env.req(t, "POST", path, Object{"title": "新标题"}, 409)
	titleExec(t, env, "DELETE FROM jobs WHERE status='running'")
	env.req(t, "POST", path, Object{"title": "新标题"}, 200)
}

func TestTitlePermissionsAndShopScope(t *testing.T) {
	env := newTitleTestEnv(t, nil)
	id := titleListing(t, env)
	shop2 := int64(env.req(t, "POST", "/api/shops", Object{"name": "二号店", "platform": "taobao", "language": "zh-CN"}, 201)["shop"].(Object)["id"].(float64))
	product := env.req(t, "POST", "/api/products", Object{"name": "别店耳环", "shop_ids": []int64{shop2}}, 201)["product"].(Object)
	other := int64(product["listings"].([]any)[0].(Object)["id"].(float64))
	for _, role := range []string{"designer", "photographer", "operator"} {
		env.req(t, "POST", "/api/users", Object{"username": "title_" + role, "password": "Password123!", "role_id": env.roleIDByKey(t, role), "shop_ids": []int64{1}}, 201)
	}
	for _, role := range []string{"designer", "photographer"} {
		env.login(t, "title_"+role, "Password123!")
		for _, action := range []string{"suggest", "apply"} {
			env.req(t, "POST", fmt.Sprintf("/api/listings/%d/titles/%s", id, action), Object{"title": "新标题"}, 403)
		}
		status := 200
		if role == "photographer" {
			status = 403
		}
		env.req(t, "GET", fmt.Sprintf("/api/listings/%d/titles/history", id), nil, status)
	}
	env.login(t, "title_operator", "Password123!")
	for _, action := range []string{"suggest", "apply"} {
		env.req(t, "POST", fmt.Sprintf("/api/listings/%d/titles/%s", other, action), Object{"title": "新标题"}, 404)
		env.req(t, "POST", fmt.Sprintf("/api/listings/%d/titles/%s", 999999, action), Object{"title": "新标题"}, 404)
	}
	env.req(t, "GET", fmt.Sprintf("/api/listings/%d/titles/history", other), nil, 404)
	result := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/titles/apply", id), Object{"title": "本店新标题", "reason": "优化搜索词"}, 200)
	t.Logf("short apply response: %s", core.JS(result))
	result = env.req(t, "GET", fmt.Sprintf("/api/listings/%d/titles/history", id), nil, 200)
	t.Logf("short history response: %s", core.JS(result))
	copies := env.req(t, "GET", fmt.Sprintf("/api/listings/%d/copies", id), nil, 200)
	version := env.req(t, "GET", fmt.Sprintf("/api/copies/%d", int64(copies["current"].(float64))), nil, 200)["copy"].(Object)
	if version["note"] != "标题建议：优化搜索词" {
		t.Fatalf("copy reason: %v", version)
	}
}

func TestTitleInputLimitsAndHistoryOrder(t *testing.T) {
	requests := make(chan Object, 2)
	env := newTitleTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		var body Object
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests <- body
		titleReply(w, "a bcdefg", "aa", "xy")
	})
	id := titleListing(t, env)
	today := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
	env.req(t, "POST", "/api/keywords", Object{"word": "公共词", "grp": "品类词"}, 201)
	for index := 0; index < 25; index++ {
		title := fmt.Sprintf("竞品%d", index)
		if index == 0 {
			title = "abcdefg"
		} else if index == 1 {
			title = "abac"
		}
		titleExec(t, env, "INSERT INTO competitor_items(shop_id,platform_item_id,keyword,title,rank,date) VALUES(1,?,'公共词',?,?,?)", fmt.Sprint(index), title, index, today)
	}
	terms := []any{}
	for index := 0; index < 40; index++ {
		terms = append(terms, fmt.Sprintf("词%d", index))
	}
	titleExec(t, env, "INSERT INTO listing_daily(listing_id,date,search_terms,source) VALUES(?,?,?,'test')", id, today, core.JS(terms))
	latest := time.Now().Add(-10 * 24 * time.Hour)
	for index := 0; index < 55; index++ {
		titleExec(t, env, "INSERT INTO listing_title_history(listing_id,old_title,new_title,created_by,created_at) VALUES(?,?,?,?,?)", id, fmt.Sprintf("旧%d", index), fmt.Sprintf("新%d", index), 1, latest.Add(-time.Duration(index)*time.Hour).Format(time.RFC3339Nano))
	}
	result := env.req(t, "POST", fmt.Sprintf("/api/listings/%d/titles/suggest", id), nil, 200)
	if core.JS(result["inputs"]) != core.JS(Object{"keywords": 1, "competitors": 20, "search_terms": 30, "history": 5}) {
		t.Fatal(result)
	}
	titles := result["titles"].([]any)
	if first := titles[0].(Object); first["chars"] != float64(8) || first["overlap_bps"] != float64(10000) || len(first["compliance"].([]any)) != 1 {
		t.Fatal(first)
	}
	if second := titles[1].(Object); second["overlap_bps"] != float64(5000) {
		t.Fatalf("must use a contiguous substring: %v", second)
	}
	messages := (<-requests)["messages"].([]any)
	var input Object
	if err := json.Unmarshal([]byte(messages[1].(Object)["content"].(string)), &input); err != nil {
		t.Fatal(err)
	}
	history := input["history"].([]any)
	if len(history) != 5 || history[0].(Object)["new_title"] != "新0" || history[4].(Object)["new_title"] != "新4" || history[0].(Object)["before"] != nil || history[0].(Object)["after"] != nil {
		t.Fatal(history)
	}
	result = env.req(t, "GET", fmt.Sprintf("/api/listings/%d/titles/history", id), nil, 200)
	items := result["items"].([]any)
	if len(items) != 50 || items[0].(Object)["new_title"] != "新0" || items[49].(Object)["new_title"] != "新49" || result["change_allowed"] != true || result["next_change_at"] != "" {
		t.Fatal(result)
	}
}
