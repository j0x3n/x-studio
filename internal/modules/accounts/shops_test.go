package accounts

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
	"xstudio/internal/core"
	"xstudio/internal/modules/products"
)

func TestShopChannelMigration(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`CREATE TABLE shops(id INTEGER PRIMARY KEY,name TEXT NOT NULL,platform TEXT NOT NULL,language TEXT NOT NULL,settings TEXT NOT NULL DEFAULT '{}',archived INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL)`); e != nil {
		t.Fatal(e)
	}
	now := core.Now()
	for _, row := range []struct{ name, platform, language string }{
		{"虾皮台湾", "shopee_tw", "zh-TW"},
		{"淘宝", "taobao", "zh-CN"},
		{"其他", "x", "en"},
		{"天猫", "tmall", "zh-CN"},
	} {
		if _, e = db.Exec("INSERT INTO shops(name,platform,language,created_at) VALUES(?,?,?,?)", row.name, row.platform, row.language, now); e != nil {
			t.Fatal(e)
		}
	}
	run := func() {
		t.Helper()
		tx, e := db.Begin()
		if e != nil {
			t.Fatal(e)
		}
		if e = shopChannelMigration(tx); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
	}
	run()
	want := map[string][3]string{
		"虾皮台湾": {"shopee", "tw", "TWD"},
		"淘宝":   {"taobao", "", "CNY"},
		"其他":   {"x", "", ""},
		"天猫":   {"tmall", "", ""},
	}
	assertShops := func() {
		t.Helper()
		rows, e := db.Query("SELECT name,platform,site,currency FROM shops ORDER BY id")
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		got := map[string][3]string{}
		for rows.Next() {
			var name, platform, site, currency string
			if e = rows.Scan(&name, &platform, &site, &currency); e != nil {
				t.Fatal(e)
			}
			got[name] = [3]string{platform, site, currency}
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		for name, row := range want {
			if got[name] != row {
				t.Fatal(name, got[name], row)
			}
		}
	}
	assertShops()
	if _, e = db.Exec("UPDATE shops SET currency='USD' WHERE name='淘宝'"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO shops(name,platform,site,currency,language,created_at) VALUES('新加坡虾皮','shopee','sg','','zh-CN',?),('空币种台湾','shopee','tw','','zh-TW',?),('又一家台湾','shopee_tw','','','zh-TW',?)", now, now, now); e != nil {
		t.Fatal(e)
	}
	want["淘宝"] = [3]string{"taobao", "", "USD"}
	want["新加坡虾皮"] = [3]string{"shopee", "sg", ""}
	want["空币种台湾"] = [3]string{"shopee", "tw", "TWD"}
	want["又一家台湾"] = [3]string{"shopee", "tw", "TWD"}
	run()
	assertShops()
}

type shopFixture struct {
	db     *sql.DB
	server *httptest.Server
	client *http.Client
}

func newShopFixture(t *testing.T) *shopFixture {
	t.Helper()
	root := t.TempDir()
	c, e := core.New(core.Config{DataDir: filepath.Join(root, "data"), PromptDir: filepath.Join(root, "prompts"), TemplateDir: filepath.Join(root, "templates"), AdminUser: "admin", AdminPassword: "password123", SessionSecret: strings.Repeat("s", 32), Workers: 1}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Start(New(c), products.New(c)); e != nil {
		c.Close()
		t.Fatal(e)
	}
	s := httptest.NewServer(c.Handler())
	c.SetBaseURL(s.URL)
	jar, _ := cookiejar.New(nil)
	f := &shopFixture{c.DB, s, &http.Client{Jar: jar}}
	t.Cleanup(func() { s.Close(); c.Close() })
	f.call(t, "POST", "/api/login", map[string]any{"username": "admin", "password": "password123"}, 200)
	return f
}
func (f *shopFixture) call(t *testing.T, method, path string, body any, status int) map[string]any {
	t.Helper()
	var input io.Reader
	if body != nil {
		input = strings.NewReader(core.JS(body))
	}
	req, e := http.NewRequest(method, f.server.URL+path, input)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", "application/json")
	res, e := f.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != status {
		t.Fatalf("%s %s：%d，期望 %d：%s", method, path, res.StatusCode, status, b)
	}
	var out map[string]any
	if len(b) > 0 && json.Unmarshal(b, &out) != nil {
		t.Fatalf("无效 JSON：%s", b)
	}
	return out
}

func TestShopSiteCurrencyAndShopeeTW(t *testing.T) {
	f := newShopFixture(t)
	var platform, site, currency string
	if e := f.db.QueryRow("SELECT platform,site,currency FROM shops WHERE name='淘宝店'").Scan(&platform, &site, &currency); e != nil || platform != "taobao" || site != "" || currency != "CNY" {
		t.Fatal(platform, site, currency, e)
	}
	seed := shopByName(t, f.call(t, "GET", "/api/shops", nil, 200)["items"].([]any), "淘宝店")
	if seed["site"] != "" || seed["currency"] != "CNY" || seed["platform"] != "taobao" {
		t.Fatal(seed)
	}
	me := shopByName(t, f.call(t, "GET", "/api/me", nil, 200)["shops"].([]any), "淘宝店")
	if me["site"] != "" || me["currency"] != "CNY" {
		t.Fatal(me)
	}
	for _, p := range []string{"taobao", "tmall", "pdd", "douyin", "xiaohongshu", "website", "live", "x", "other"} {
		got := f.call(t, "POST", "/api/shops", map[string]any{"name": p + "店", "platform": p, "language": "zh-CN"}, 201)["shop"].(map[string]any)
		if got["platform"] != p || got["site"] != "" || got["currency"] != "" {
			t.Fatal(got)
		}
	}
	for _, p := range []string{"shopee", "lazada", "tiktok"} {
		if f.call(t, "POST", "/api/shops", map[string]any{"name": p + "缺站点", "platform": p, "language": "zh-CN"}, 400)["error"] != "虾皮、Lazada、TikTok Shop 必须填写站点" {
			t.Fatal(p)
		}
		got := f.call(t, "POST", "/api/shops", map[string]any{"name": p + "店", "platform": p, "site": "tw", "language": "zh-TW"}, 201)["shop"].(map[string]any)
		if got["platform"] != p || got["site"] != "tw" {
			t.Fatal(got)
		}
	}
	for _, site := range []string{"my", "sg", "th", "ph", "vn", "id", "br"} {
		got := f.call(t, "POST", "/api/shops", map[string]any{"name": "虾皮" + site, "platform": "shopee", "site": site, "language": "en"}, 201)["shop"].(map[string]any)
		if got["site"] != site || got["platform"] != "shopee" {
			t.Fatal(got)
		}
	}
	for _, cur := range []string{"CNY", "TWD", "MYR", "SGD", "THB", "PHP", "VND", "IDR", "BRL", "USD"} {
		got := f.call(t, "POST", "/api/shops", map[string]any{"name": "币种" + cur, "platform": "website", "currency": cur, "language": "zh-CN"}, 201)["shop"].(map[string]any)
		if got["currency"] != cur || got["site"] != "" {
			t.Fatal(got)
		}
	}
	bad := []struct {
		body map[string]any
		msg  string
	}{
		{map[string]any{"name": "", "platform": "taobao", "language": "zh-CN"}, "店铺名称必填，且不超过 120 个字"},
		{map[string]any{"name": strings.Repeat("名", 121), "platform": "taobao", "language": "zh-CN"}, "店铺名称必填，且不超过 120 个字"},
		{map[string]any{"name": "坏平台", "platform": "amazon", "language": "zh-CN"}, "平台不正确，可选淘宝、天猫、拼多多、抖音小店、小红书、虾皮、Lazada、TikTok Shop、独立站、直播间、X 或其他"},
		{map[string]any{"name": "坏站点", "platform": "shopee", "site": "jp", "language": "zh-CN"}, "站点只能是 tw、my、sg、th、ph、vn、id、br"},
		{map[string]any{"name": "不该有站点", "platform": "taobao", "site": "tw", "language": "zh-CN"}, "这个平台不用填写站点"},
		{map[string]any{"name": "坏币种", "platform": "taobao", "currency": "eur", "language": "zh-CN"}, "币种留空，或填写 CNY、TWD、MYR、SGD、THB、PHP、VND、IDR、BRL、USD"},
		{map[string]any{"name": "坏语言", "platform": "taobao", "language": "ja"}, "语言只能是 zh-CN、zh-TW 或 en"},
		{map[string]any{"name": "旧虾皮填错站点", "platform": "shopee_tw", "site": "my", "language": "zh-TW"}, "虾皮台湾店的站点只能是 tw"},
	}
	var before int
	if e := f.db.QueryRow("SELECT count(*) FROM shops").Scan(&before); e != nil {
		t.Fatal(e)
	}
	for _, tc := range bad {
		if f.call(t, "POST", "/api/shops", tc.body, 400)["error"] != tc.msg {
			t.Fatal(tc.body, tc.msg)
		}
	}
	var after int
	if e := f.db.QueryRow("SELECT count(*) FROM shops").Scan(&after); e != nil || after != before {
		t.Fatal(before, after, e)
	}
	legacy := f.call(t, "POST", "/api/shops", map[string]any{"name": "旧虾皮", "platform": "shopee_tw", "language": "zh-TW", "currency": "USD"}, 201)["shop"].(map[string]any)
	if legacy["platform"] != "shopee" || legacy["site"] != "tw" || legacy["currency"] != "USD" {
		t.Fatal(legacy)
	}
	var storedPlatform, storedSite, storedCurrency string
	if e := f.db.QueryRow("SELECT platform,site,currency FROM shops WHERE id=?", int64(legacy["id"].(float64))).Scan(&storedPlatform, &storedSite, &storedCurrency); e != nil || storedPlatform != "shopee" || storedSite != "tw" || storedCurrency != "USD" {
		t.Fatal(storedPlatform, storedSite, storedCurrency, e)
	}
	plain := f.call(t, "POST", "/api/shops", map[string]any{"name": "旧虾皮没填站点", "platform": "shopee_tw", "language": "zh-TW"}, 201)["shop"].(map[string]any)
	if plain["platform"] != "shopee" || plain["site"] != "tw" || plain["currency"] != "" {
		t.Fatal(plain)
	}
	listed := shopByName(t, f.call(t, "GET", "/api/shops", nil, 200)["items"].([]any), "旧虾皮")
	if listed["platform"] != "shopee" || listed["site"] != "tw" || listed["currency"] != "USD" {
		t.Fatal(listed)
	}
	seen := shopByName(t, f.call(t, "GET", "/api/me", nil, 200)["shops"].([]any), "旧虾皮没填站点")
	if seen["site"] != "tw" || seen["currency"] != "" || seen["platform"] != "shopee" {
		t.Fatal(seen)
	}
	id := int64(f.call(t, "POST", "/api/shops", map[string]any{"name": "待改", "platform": "taobao", "currency": "CNY", "language": "zh-CN"}, 201)["shop"].(map[string]any)["id"].(float64))
	path := "/api/shops/" + strconv.FormatInt(id, 10)
	patched := f.call(t, "PATCH", path, map[string]any{"platform": "shopee_tw"}, 200)["shop"].(map[string]any)
	if patched["platform"] != "shopee" || patched["site"] != "tw" || patched["currency"] != "CNY" {
		t.Fatal(patched)
	}
	if f.call(t, "PATCH", path, map[string]any{"site": "jp"}, 400)["error"] != "站点只能是 tw、my、sg、th、ph、vn、id、br" {
		t.Fatal("坏站点被接受")
	}
	if f.call(t, "PATCH", path, map[string]any{"platform": "website"}, 400)["error"] != "这个平台不用填写站点" {
		t.Fatal("换平台时旧站点还在")
	}
	if f.call(t, "PATCH", path, map[string]any{"id": 9}, 400)["error"] != "不支持的店铺字段" {
		t.Fatal("未知字段被接受")
	}
	moved := f.call(t, "PATCH", path, map[string]any{"platform": "website", "site": "", "currency": "USD"}, 200)["shop"].(map[string]any)
	if moved["platform"] != "website" || moved["site"] != "" || moved["currency"] != "USD" {
		t.Fatal(moved)
	}
	if e := f.db.QueryRow("SELECT platform,site,currency FROM shops WHERE id=?", id).Scan(&storedPlatform, &storedSite, &storedCurrency); e != nil || storedPlatform != "website" || storedSite != "" || storedCurrency != "USD" {
		t.Fatal(storedPlatform, storedSite, storedCurrency, e)
	}
}
func shopByName(t *testing.T, items []any, name string) map[string]any {
	t.Helper()
	for _, v := range items {
		s := v.(map[string]any)
		if s["name"] == name {
			return s
		}
	}
	t.Fatal("找不到店铺", name)
	return nil
}
