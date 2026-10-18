package assistant

import (
	"fmt"
	"strings"
	"testing"
	"xstudio/internal/core"
)

func TestChatPromptShopScopeAfterRoleChange(t *testing.T) {
	f := newFixture(t)
	f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES('未分配的秘密店','other','en',?)", core.Now())
	uid := f.user(t, "shopreader", "shops", []string{core.ProductView}, 1)
	// Admin sees every shop even without user_shops assignments.
	f.req(t, "POST", "/api/assistant/chat", chatBody("今天做什么"), 200)
	prompt := aiPrompt(f.ai.snapshot()[0])
	if !strings.Contains(prompt, "淘宝店") || !strings.Contains(prompt, "未分配的秘密店") {
		t.Fatal(prompt)
	}
	f.login(t, "shopreader")
	f.req(t, "POST", "/api/assistant/chat", chatBody("今天做什么"), 200)
	prompt = aiPrompt(f.ai.snapshot()[1])
	if !strings.Contains(prompt, "淘宝店") || strings.Contains(prompt, "未分配的秘密店") {
		t.Fatal(prompt)
	}
	// Changing the scope deliberately leaves the old assignment in place.
	u := mustUser(t, f, uid)
	f.exec(t, "UPDATE roles SET scope='shares' WHERE id=?", u.Role.ID)
	var assignments int
	if err := f.c.DB.QueryRow("SELECT count(*) FROM user_shops WHERE user_id=?", uid).Scan(&assignments); err != nil || assignments != 1 {
		t.Fatal(assignments, err)
	}
	f.req(t, "POST", "/api/assistant/chat", chatBody("今天做什么"), 200)
	prompt = aiPrompt(f.ai.snapshot()[2])
	if strings.Contains(prompt, "淘宝店") || strings.Contains(prompt, "未分配的秘密店") {
		t.Fatal("shares prompt leaked shop names", prompt)
	}
}

func setReply(f *fixture, reply object) {
	f.ai.mu.Lock()
	defer f.ai.mu.Unlock()
	f.ai.script = []any{reply}
}

func TestChatOpenAssetListsAndShopFilters(t *testing.T) {
	f := newFixture(t)
	shop := f.exec(t, "INSERT INTO shops(name,platform,language,created_at) VALUES('未分配店','other','en',?)", core.Now())
	p1, _ := f.product(t, "珠蝶", 1)
	p2, _ := f.product(t, "不可见款", shop)
	a1 := f.asset(t, p1, "original")
	a2 := f.asset(t, p1, "original")
	hidden := f.asset(t, p2, "original")
	f.user(t, "viewer", "shops", []string{core.ProductView}, 1)
	f.login(t, "viewer")
	for _, tc := range []struct {
		name, path string
		valid      bool
	}{
		{"hidden assets", fmt.Sprintf("/process?assets=%d", hidden), false},
		{"mixed ids", fmt.Sprintf("/process?ids=%d,%d", a1, hidden), false},
		{"hidden shop", fmt.Sprintf("/assets?shop_id=%d", shop), false},
		{"visible assets", fmt.Sprintf("/process?assets=%d,%d", a1, a2), true},
		{"visible ids", fmt.Sprintf("/process?ids=%d,%d", a1, a2), true},
		{"visible shop", "/assets?shop_id=1", true},
		{"bad assets", "/process?assets=oops", false},
		{"empty ids", "/process?ids=", false},
		{"empty element", fmt.Sprintf("/process?ids=%d,", a1), false},
		{"negative id", "/process?ids=-1", false},
		{"fractional id", "/process?assets=1.5", false},
		{"malformed query", "/process?ids=1;2", false},
		{"bad shop", "/assets?shop_id=oops", false},
		{"empty shop", "/assets?shop_id=", false},
		{"zero shop", "/assets?shop_id=0", false},
		{"repeated ids", fmt.Sprintf("/process?ids=%d&ids=%d", a1, hidden), false},
		{"repeated shops", fmt.Sprintf("/assets?shop_id=1&shop_id=%d", shop), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setReply(f, object{"reply": "打开页面", "actions": []object{card("open", object{"path": tc.path})}})
			o := f.req(t, "POST", "/api/assistant/chat", chatBody("打开"), 200)
			wantDropped, wantActions := float64(1), 0
			if tc.valid {
				wantDropped, wantActions = 0, 1
			}
			if o["dropped"] != wantDropped || len(o["actions"].([]any)) != wantActions {
				t.Fatal(o)
			}
		})
	}
}

func TestChatContextRuneLimits(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct{ name, path, title string }{
		{"long path", strings.Repeat("路", 301), "商品"},
		{"long title", "/products", strings.Repeat("款", 101)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := chatBody("查询")
			body["context"] = object{"path": tc.path, "title": tc.title}
			o := f.req(t, "POST", "/api/assistant/chat", body, 400)
			if !strings.Contains(o["error"].(string), "最多") {
				t.Fatal(o)
			}
		})
	}
	if len(f.ai.snapshot()) != 0 {
		t.Fatal("invalid context reached AI")
	}
	body := chatBody("查询")
	body["context"] = object{"path": strings.Repeat("路", 300), "title": strings.Repeat("款", 100)}
	f.req(t, "POST", "/api/assistant/chat", body, 200)
}

func TestHomeBoundedRowsKeepFullCountsAndSummaryOrder(t *testing.T) {
	f := newFixture(t)
	for i := 1; i <= 30; i++ {
		pid, lid := f.product(t, fmt.Sprintf("款%02d", i), 1)
		at := fmt.Sprintf("2026-10-%02dT00:00:00Z", i)
		f.exec(t, "UPDATE products SET updated_at=? WHERE id=?", at, pid)
		f.exec(t, "UPDATE listings SET pipeline_status='failed',updated_at=? WHERE id=?", at, lid)
		f.exec(t, "UPDATE listing_render SET review_status='pending',updated_at=? WHERE listing_id=?", at, lid)
		f.exec(t, "INSERT INTO copy_versions(listing_id,source,created_by,created_at) VALUES(?,'ai_generate',1,?)", lid, fmt.Sprintf("2026-11-%02dT00:00:00Z", i))
	}
	for _, limit := range []int{2, 10} {
		total, rows, err := f.m.homeRows("p.name product_name,p.updated_at", " FROM products p", "p.updated_at DESC,p.id DESC", limit)
		if err != nil || total != 30 || len(rows) != limit || rows[0]["product_name"] != "款30" {
			t.Fatal(total, rows, err)
		}
	}
	o := f.req(t, "GET", "/api/home", nil, 200)
	counts := o["counts"].(map[string]any)
	for _, kind := range []string{"in_progress", "failed", "review_pending", "copy_unapplied", "no_photo"} {
		if counts[kind] != float64(30) {
			t.Fatal(kind, counts)
		}
	}
	todos := o["todos"].([]any)
	if len(todos) != 10 {
		t.Fatal(todos)
	}
	for i, v := range todos {
		td := v.(map[string]any)
		if td["kind"] != "render_failed" || td["listing_id"] != float64(30-i) {
			t.Fatal(todos)
		}
	}
	// Without failures, all three aggregated cards retain their full counts,
	// first two names, newest timestamp ordering and first listing link.
	f.exec(t, "UPDATE listings SET pipeline_status='draft'")
	o = f.req(t, "GET", "/api/home", nil, 200)
	todos = o["todos"].([]any)
	if len(todos) != 3 {
		t.Fatal(todos)
	}
	for i, kind := range []string{"review_pending", "copy_unapplied", "no_photo"} {
		td := todos[i].(map[string]any)
		if td["kind"] != kind || td["count"] != float64(30) || td["detail"] != "款30、款29 等" {
			t.Fatal(todos)
		}
	}
	copyAction := todos[1].(map[string]any)["action"].(map[string]any)
	if copyAction["params"].(map[string]any)["path"] != "/copy?listing=30" {
		t.Fatal(copyAction)
	}
}

func TestTodoNamesOnlyAppendEtcBeyondTwo(t *testing.T) {
	for _, tc := range []struct {
		total int
		want  string
	}{{0, ""}, {1, "珠蝶"}, {2, "珠蝶、珍珠"}, {3, "珠蝶、珍珠 等"}} {
		t.Run(fmt.Sprint(tc.total), func(t *testing.T) {
			items := []object{}
			for _, name := range []string{"珠蝶", "珍珠"}[:min(tc.total, 2)] {
				items = append(items, object{"product_name": name})
			}
			if got := names(items, tc.total); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestProcessCostHintCountsEachImageCall(t *testing.T) {
	f := newFixture(t)
	pid, _ := f.product(t, "珠蝶", 1)
	ids := []int64{f.asset(t, pid, "original"), f.asset(t, pid, "original")}
	for _, tc := range []struct {
		name string
		ops  object
		hint string
	}{
		{"local", object{"resize": object{"long_side": 1000}}, ""},
		{"ai", object{"ai": object{"extra": "去灰尘"}}, "约用 2 张图额度"},
		{"upscale", object{"upscale": object{"target_long_side": 2048}}, "约用 2 张图额度"},
		{"both", object{"ai": object{"extra": "去灰尘"}, "upscale": object{"target_long_side": 2048}}, "约用 4 张图额度"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setReply(f, object{"reply": "确认后开始", "actions": []object{card("process_assets", object{"asset_ids": ids, "ops": tc.ops})}})
			o := f.req(t, "POST", "/api/assistant/chat", chatBody("处理图片"), 200)
			if o["dropped"] != float64(0) || len(o["actions"].([]any)) != 1 {
				t.Fatal(o)
			}
			got, _ := o["actions"].([]any)[0].(map[string]any)["cost_hint"].(string)
			if got != tc.hint {
				t.Fatalf("got %q want %q", got, tc.hint)
			}
		})
	}
}
