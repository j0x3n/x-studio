package market

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/products"
)

func TestParseMoneySales(t *testing.T) {
	cases := []struct {
		in    string
		scale int64
		want  int64
	}{
		{"0.29", 100, 29},
		{"19.99", 100, 1999},
		{"12.5", 100, 1250},
		{"1.5", 100, 150},
		{"1.23", 100, 123},
		{"1.235", 100, 124},
		{"1.225", 100, 123},
		{"3.2", 100, 320},
		{"100", 100, 10000},
		{"1.2", 10000, 12000},
		{"1.25", 10000, 12500},
		{"1000", 1, 1000},
	}
	for _, c := range cases {
		got, ok := scaleDecimal(c.in, c.scale)
		if !ok || got != c.want {
			t.Fatalf("scale %s * %d = %d ok=%v want %d", c.in, c.scale, got, ok, c.want)
		}
	}
	if _, ok := scaleDecimal("-1", 100); ok {
		t.Fatal("negative accepted")
	}
	if !withinPercent("3.2") || !withinPercent("100") || withinPercent("100.1") || withinPercent("-1") {
		t.Fatal("percent range")
	}
	if p := parseSales("月销 1000+"); p == nil || *p != 1000 {
		t.Fatalf("1000+ -> %v", p)
	}
	if p := parseSales("1.2万+"); p == nil || *p != 12000 {
		t.Fatalf("1.2万+ -> %v", p)
	}
	if p := parseSales("已售1.25万件"); p == nil || *p != 12500 {
		t.Fatalf("1.25万 -> %v", p)
	}
	if parseSales("暂无") != nil || parseSales("") != nil {
		t.Fatal("unparsed sales should be nil")
	}
}

func TestMaterialOrder(t *testing.T) {
	cases := []struct {
		title  string
		params map[string]any
		want   string
	}{
		{"施家贝珠项链", nil, "施家"},
		{"贝珠耳钉", nil, "贝珠"},
		{"贝壳珠手链", nil, "贝珠"},
		{"高仿淡水", nil, "仿"},
		{"天然淡水珍珠", nil, "淡水"},
		{"天然海水", nil, "天然"},
		{"海水珍珠", nil, "未写"},
		{"珍珠", map[string]any{"材质": "施家"}, "施家"},
		{"珍珠", map[string]any{"卖点": []any{"贝壳珠"}}, "贝珠"},
	}
	for _, c := range cases {
		if got := materialFrom(c.title, c.params); got != c.want {
			t.Fatalf("%s %+v -> %s want %s", c.title, c.params, got, c.want)
		}
	}
	f := boot(t)
	shop := f.shop()
	raw := fmt.Sprintf(`{"shop_id":%d,"kind":"item","items":[
		{"item_id":"m1","title":"施家贝珠项链"},
		{"item_id":"m2","title":"贝珠耳钉"},
		{"item_id":"m3","title":"贝壳珠手链"},
		{"item_id":"m4","title":"高仿淡水"},
		{"item_id":"m5","title":"天然淡水珍珠"},
		{"item_id":"m6","title":"天然海水"},
		{"item_id":"m7","title":"海水珍珠"},
		{"item_id":"m8","title":"珍珠","params":{"材质":"施家"}}
	]}`, shop)
	f.call("POST", "/api/market/ingest", []byte(raw), "", 200)
	want := map[string]string{"m1": "施家", "m2": "贝珠", "m3": "贝珠", "m4": "仿", "m5": "淡水", "m6": "天然", "m7": "未写", "m8": "施家"}
	for id, material := range want {
		var got string
		if err := f.c.DB.QueryRow("SELECT material FROM competitor_items WHERE platform_item_id=?", id).Scan(&got); err != nil {
			t.Fatal(id, err)
		}
		if got != material {
			t.Fatalf("stored %s -> %s want %s", id, got, material)
		}
	}
}

func TestTokens(t *testing.T) {
	f := boot(t)
	shop := f.shop()
	shop2 := f.exec("INSERT INTO shops(name, platform, language, created_at) VALUES('二店','taobao','zh-CN',?)", core.Now())
	op := f.user("op1", "operator")
	f.link(op, shop)
	other := f.user("op2", "operator")
	f.link(other, shop)
	f.login("op1")
	created := f.call("POST", "/api/market/tokens", []byte(`{"name":"采集"}`), "", 201)
	token, _ := created["token"].(string)
	id := int64(created["id"].(float64))
	if !strings.HasPrefix(token, "xst_") || len(token) < 20 {
		t.Fatalf("token %q", token)
	}
	var name, prefix, hash, scopes string
	if err := f.c.DB.QueryRow("SELECT name, prefix, token_hash, scopes FROM user_api_tokens WHERE id=?", id).Scan(&name, &prefix, &hash, &scopes); err != nil {
		t.Fatal(err)
	}
	if hash != hashToken(token) || prefix != token[:8] || strings.Contains(hash, token) || strings.Contains(scopes, token) || name == token {
		t.Fatalf("plaintext stored name=%s prefix=%s hash=%s scopes=%s", name, prefix, hash, scopes)
	}
	listed := f.call("GET", "/api/market/tokens", nil, "", 200)
	rows := listed["items"].([]any)
	if len(rows) != 1 {
		t.Fatalf("list %v", listed)
	}
	row := rows[0].(map[string]any)
	if _, ok := row["token"]; ok || row["prefix"] != prefix {
		t.Fatalf("list leaked or prefix wrong: %v", row)
	}
	body := []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"keyword","items":[{"word":"令牌词","search_volume":3}]}`, shop))
	f.call("POST", "/api/market/ingest", body, token, 200)
	listed = f.call("GET", "/api/market/tokens", nil, "", 200)
	used := false
	for _, it := range listed["items"].([]any) {
		item := it.(map[string]any)
		if int64(item["id"].(float64)) == id && item["last_used_at"].(string) != "" {
			used = true
		}
	}
	if !used {
		t.Fatal("last_used_at not updated", listed)
	}
	f.login("op2")
	f.call("DELETE", fmt.Sprintf("/api/market/tokens/%d", id), nil, "", 403)
	f.call("POST", "/api/market/ingest", body, token, 200)
	f.call("GET", fmt.Sprintf("/api/market/keywords?shop_id=%d", shop), nil, token, 401)
	f.call("GET", "/api/market/tokens", nil, token, 401)
	f.call("POST", "/api/market/ingest", []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"keyword","items":[{"word":"跨店","search_volume":1}]}`, shop2)), token, 403)
	f.call("POST", "/api/market/ingest", []byte(`{"shop_id":999999,"kind":"keyword","items":[{"word":"无店","search_volume":1}]}`), token, 404)
	f.login("op1")
	f.call("GET", fmt.Sprintf("/api/market/keywords?shop_id=%d", shop2), nil, "", 403)
	f.call("GET", "/api/market/keywords?shop_id=999999", nil, "", 404)
	f.exec("INSERT INTO roles(key, name, builtin, scope, perms, created_at) VALUES('outsider','外部',0,'shares',?,?)", core.JS([]string{core.MarketView, core.MarketImport}), core.Now())
	f.user("out", "outsider")
	f.login("out")
	f.call("GET", fmt.Sprintf("/api/market/keywords?shop_id=%d", shop), nil, "", 403)
	f.login("op1")
	f.call("DELETE", fmt.Sprintf("/api/market/tokens/%d", id), nil, "", 200)
	f.call("POST", "/api/market/ingest", body, token, 401)
	again := f.call("POST", "/api/market/tokens", []byte(`{"name":"第二次"}`), "", 201)
	token2 := again["token"].(string)
	f.exec("UPDATE users SET role_id=(SELECT id FROM roles WHERE key='designer') WHERE username='op1'")
	f.call("POST", "/api/market/ingest", []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"keyword","items":[{"word":"无权限","search_volume":1}]}`, shop)), token2, 403)
	f.exec("UPDATE users SET disabled=1 WHERE username='op1'")
	f.call("POST", "/api/market/ingest", []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"keyword","items":[{"word":"停用","search_volume":1}]}`, shop)), token2, 401)
	f.login("admin")
	var ids []int64
	for i := 0; i < 10; i++ {
		o := f.call("POST", "/api/market/tokens", []byte(fmt.Sprintf(`{"name":"n%d"}`, i)), "", 201)
		ids = append(ids, int64(o["id"].(float64)))
	}
	f.call("POST", "/api/market/tokens", []byte(`{"name":"超额"}`), "", 400)
	f.call("DELETE", fmt.Sprintf("/api/market/tokens/%d", ids[0]), nil, "", 200)
	f.call("POST", "/api/market/tokens", []byte(`{"name":"补一个"}`), "", 201)
	if f.n("SELECT count(*) FROM jobs") != 0 {
		t.Fatal("diagnose job was created")
	}
}

func TestIngestDuplicateOverwriteUnitsLimitAndRate(t *testing.T) {
	f := boot(t)
	shop := f.shop()
	before := f.n("SELECT count(*) FROM market_snapshots WHERE kind='keyword'")
	first := []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"keyword","category":"pearl","items":[{"word":"珍珠项链","search_volume":10,"ctr":1.235,"cvr":3.2}]}`, shop))
	got := f.call("POST", "/api/market/ingest", first, "", 200)
	if got["duplicate"] == true || got["inserted"].(float64) != 1 {
		t.Fatalf("first %v", got)
	}
	var vol int64
	var clicks, ctr, cvr sql.NullInt64
	if err := f.c.DB.QueryRow("SELECT search_volume, clicks, ctr_bps, cvr_bps FROM market_keywords WHERE word='珍珠项链'").Scan(&vol, &clicks, &ctr, &cvr); err != nil {
		t.Fatal(err)
	}
	if vol != 10 || clicks.Valid || !ctr.Valid || ctr.Int64 != 124 || !cvr.Valid || cvr.Int64 != 320 {
		t.Fatalf("units vol=%d clicks=%v ctr=%v cvr=%v", vol, clicks, ctr, cvr)
	}
	second := []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"keyword","category":"pearl","items":[{"word":"珍珠项链","search_volume":25,"clicks":4}]}`, shop))
	got = f.call("POST", "/api/market/ingest", second, "", 200)
	if got["duplicate"] == true || got["updated"].(float64) != 1 {
		t.Fatalf("overwrite %v", got)
	}
	if err := f.c.DB.QueryRow("SELECT search_volume, clicks, ctr_bps, cvr_bps FROM market_keywords WHERE word='珍珠项链'").Scan(&vol, &clicks, &ctr, &cvr); err != nil {
		t.Fatal(err)
	}
	if vol != 25 || !clicks.Valid || clicks.Int64 != 4 || ctr.Valid || cvr.Valid {
		t.Fatalf("overwritten vol=%d clicks=%v ctr=%v cvr=%v", vol, clicks, ctr, cvr)
	}
	if f.n("SELECT count(*) FROM market_keywords WHERE word='珍珠项链'") != 1 {
		t.Fatal("row accumulated")
	}
	got = f.call("POST", "/api/market/ingest", second, "", 200)
	if got["duplicate"] != true {
		t.Fatalf("duplicate %v", got)
	}
	if f.n("SELECT count(*) FROM market_snapshots WHERE kind='keyword'") != before+2 {
		t.Fatal("duplicate wrote a snapshot")
	}
	if err := f.c.DB.QueryRow("SELECT search_volume FROM market_keywords WHERE word='珍珠项链'").Scan(&vol); err != nil || vol != 25 {
		t.Fatal(vol, err)
	}
	search := []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"search","items":[
		{"keyword":"珍珠","rank":1,"item_id":"10001","title":"淡水珍珠","price":0.29,"sales_text":"月销 1000+","shop_name":"甲"},
		{"keyword":"珍珠","rank":2,"item_id":"10002","title":"海水珍珠","price":19.99,"sales_text":"1.2万+","shop_name":"乙"}
	]}`, shop))
	f.call("POST", "/api/market/ingest", search, "", 200)
	var price, sales sql.NullInt64
	if err := f.c.DB.QueryRow("SELECT price_cents, sales FROM competitor_items WHERE platform_item_id='10001'").Scan(&price, &sales); err != nil {
		t.Fatal(err)
	}
	if !price.Valid || price.Int64 != 29 || !sales.Valid || sales.Int64 != 1000 {
		t.Fatalf("10001 price=%v sales=%v", price, sales)
	}
	if err := f.c.DB.QueryRow("SELECT price_cents, sales FROM competitor_items WHERE platform_item_id='10002'").Scan(&price, &sales); err != nil {
		t.Fatal(err)
	}
	if !price.Valid || price.Int64 != 1999 || !sales.Valid || sales.Int64 != 12000 {
		t.Fatalf("10002 price=%v sales=%v", price, sales)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `{"shop_id":%d,"kind":"keyword","items":[`, shop)
	for i := 0; i < 2001; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"word":"bulk%d","search_volume":1}`, i)
	}
	b.WriteString(`]}`)
	tooMany := f.call("POST", "/api/market/ingest", []byte(b.String()), "", 400)
	if !strings.Contains(tooMany["error"].(string), "2000") {
		t.Fatal(tooMany)
	}
	created := f.call("POST", "/api/market/tokens", []byte(`{"name":"限流"}`), "", 201)
	token := created["token"].(string)
	for i := 0; i < 60; i++ {
		payload := []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"keyword","items":[{"word":"rate-%d","search_volume":1}]}`, shop, i))
		f.call("POST", "/api/market/ingest", payload, token, 200)
	}
	f.call("POST", "/api/market/ingest", []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"keyword","items":[{"word":"rate-over","search_volume":1}]}`, shop)), token, 429)
	if f.n("SELECT count(*) FROM jobs") != 0 {
		t.Fatal("diagnose job was created")
	}
}

func TestImportPreviewConfirmExpiryAndUnmatched(t *testing.T) {
	f := boot(t)
	shop := f.shop()
	f.listing(shop, "https://item.taobao.com/item.htm?id=998877")
	day := chinaToday()
	raw := []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"my_listing","rows":[
		{"date":"%s","item_id":"998877","impressions":8,"gmv":12.5,"search_terms":[{"term":"珍珠","uv":1,"clicks":2,"orders":0}]},
		{"date":"%s","item_id":"111","impressions":1}
	]}`, shop, day, day))
	dailyBefore := f.n("SELECT count(*) FROM listing_daily")
	snapBefore := f.n("SELECT count(*) FROM market_snapshots")
	preview := f.call("POST", "/api/market/import/preview", raw, "", 200)
	if preview["total"].(float64) != 2 || preview["ok"].(float64) != 1 {
		t.Fatalf("preview %v", preview)
	}
	errs := preview["errors"].([]any)
	if len(errs) != 1 || int(errs[0].(map[string]any)["index"].(float64)) != 1 || !strings.Contains(errs[0].(map[string]any)["reason"].(string), "上架版本") {
		t.Fatalf("errors %v", errs)
	}
	sample := preview["sample"].([]any)
	if len(sample) != 1 || sample[0].(map[string]any)["gmv_cents"].(float64) != 1250 {
		t.Fatalf("sample %v", sample)
	}
	if f.n("SELECT count(*) FROM listing_daily") != dailyBefore || f.n("SELECT count(*) FROM market_snapshots") != snapBefore {
		t.Fatal("preview wrote rows")
	}
	confirmed := f.call("POST", "/api/market/import/confirm", []byte(fmt.Sprintf(`{"preview_id":"%s"}`, preview["preview_id"])), "", 200)
	if confirmed["duplicate"] == true || confirmed["inserted"].(float64) != 1 {
		t.Fatalf("confirm %v", confirmed)
	}
	var gmv, impr sql.NullInt64
	var terms string
	if err := f.c.DB.QueryRow("SELECT gmv_cents, impressions, search_terms FROM listing_daily").Scan(&gmv, &impr, &terms); err != nil {
		t.Fatal(err)
	}
	if !gmv.Valid || gmv.Int64 != 1250 || !impr.Valid || impr.Int64 != 8 || !strings.Contains(terms, "珍珠") {
		t.Fatalf("stored gmv=%v impr=%v terms=%s", gmv, impr, terms)
	}
	if f.n("SELECT count(*) FROM listing_daily") != dailyBefore+1 {
		t.Fatal("confirm count")
	}
	f.call("POST", "/api/market/import/confirm", []byte(fmt.Sprintf(`{"preview_id":"%s"}`, preview["preview_id"])), "", 409)
	again := f.call("POST", "/api/market/import/preview", raw, "", 200)
	dup := f.call("POST", "/api/market/import/confirm", []byte(fmt.Sprintf(`{"preview_id":"%s"}`, again["preview_id"])), "", 200)
	if dup["duplicate"] != true || f.n("SELECT count(*) FROM listing_daily") != dailyBefore+1 {
		t.Fatalf("second confirm %v", dup)
	}
	fresh := f.call("POST", "/api/market/import/preview", []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"my_listing","rows":[{"date":"%s","item_id":"998877","impressions":1}]}`, shop, day)), "", 200)
	f.m.previewTTL = time.Millisecond
	time.Sleep(30 * time.Millisecond)
	f.call("POST", "/api/market/import/confirm", []byte(fmt.Sprintf(`{"preview_id":"%s"}`, fresh["preview_id"])), "", 409)
	f.m.previewTTL = 0
	ad := []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"ad","rows":[
		{"date":"%s","campaign":"计划","keyword":"珍珠","item_id":"998877","cost":1.5,"clicks":3},
		{"date":"%s","campaign":"计划","keyword":"别的","item_id":"404","cost":1}
	]}`, shop, day, day))
	adPreview := f.call("POST", "/api/market/import/preview", ad, "", 200)
	if adPreview["ok"].(float64) != 1 || len(adPreview["errors"].([]any)) != 1 {
		t.Fatalf("ad preview %v", adPreview)
	}
	if f.n("SELECT count(*) FROM ad_daily") != 0 {
		t.Fatal("ad preview wrote")
	}
	f.call("POST", "/api/market/import/confirm", []byte(fmt.Sprintf(`{"preview_id":"%s"}`, adPreview["preview_id"])), "", 200)
	var cost sql.NullInt64
	if err := f.c.DB.QueryRow("SELECT cost_cents FROM ad_daily").Scan(&cost); err != nil || !cost.Valid || cost.Int64 != 150 {
		t.Fatalf("ad cost %v %v", cost, err)
	}
}

func TestTopKeywordsAndTitleChange(t *testing.T) {
	f := boot(t)
	shop := f.shop()
	day := chinaToday()
	old := addDays(day, -40)
	ins := func(category, word, date string, vol, cvr, comp any) {
		t.Helper()
		if _, err := f.c.DB.Exec("INSERT INTO market_keywords(shop_id, category, word, date, search_volume, cvr_bps, competition, updated_at) VALUES(?,?,?,?,?,?,?,?)", shop, category, word, date, vol, cvr, comp, core.Now()); err != nil {
			t.Fatal(err)
		}
	}
	ins("pearl", "high", day, 50, 5000, 3)
	ins("pearl", "mid", day, 100, 1000, nil)
	ins("pearl", "none", day, 99999, nil, nil)
	ins("pearl", "old", old, 999, 10000, nil)
	ins("other", "skip", day, 10, 10000, nil)
	got, err := TopKeywords(f.c, shop, "pearl", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Word != "high" || got[1].Word != "mid" || got[2].Word != "none" {
		t.Fatalf("order %+v", got)
	}
	if got[0].Score == nil || *got[0].Score != 25 || got[1].Score == nil || *got[1].Score != 10 || got[2].Score != nil {
		t.Fatalf("scores %+v %+v %+v", got[0].Score, got[1].Score, got[2].Score)
	}
	if got[0].Competition == nil || *got[0].Competition != 3 || got[0].SearchVolume == nil || *got[0].SearchVolume != 50 {
		t.Fatalf("high %+v", got[0])
	}
	top, err := TopKeywords(f.c, shop, "pearl", 2)
	if err != nil || len(top) != 2 || top[0].Word != "high" || top[1].Word != "mid" {
		t.Fatalf("n=2 %+v %v", top, err)
	}
	empty, err := TopKeywords(f.c, shop, "pearl", 0)
	if err != nil || len(empty) != 0 {
		t.Fatalf("n=0 %+v %v", empty, err)
	}
	lid := f.listing(shop, "https://item.taobao.com/item.htm?id=555")
	ok, next, err := TitleChangeAllowed(f.c, lid)
	if err != nil || !ok || !next.IsZero() {
		t.Fatalf("first change ok=%v next=%v err=%v", ok, next, err)
	}
	if _, _, err = TitleChangeAllowed(f.c, 999999); err == nil {
		t.Fatal("missing listing")
	}
	adminID := f.uid("admin")
	tx, err := f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = RecordTitleChange(tx, lid, "旧标题", "新标题", "优化", adminID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	ok, next, err = TitleChangeAllowed(f.c, lid)
	if err != nil || ok {
		t.Fatalf("fresh change ok=%v err=%v", ok, err)
	}
	if next.Before(time.Now().Add(6*24*time.Hour)) || next.After(time.Now().Add(8*24*time.Hour)) {
		t.Fatal(next)
	}
	past := time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err = f.c.DB.Exec("UPDATE listing_title_history SET created_at=? WHERE listing_id=?", past, lid); err != nil {
		t.Fatal(err)
	}
	ok, _, err = TitleChangeAllowed(f.c, lid)
	if err != nil || !ok {
		t.Fatalf("8 days ok=%v err=%v", ok, err)
	}
	past = time.Now().Add(-6 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err = f.c.DB.Exec("UPDATE listing_title_history SET created_at=? WHERE listing_id=?", past, lid); err != nil {
		t.Fatal(err)
	}
	ok, _, err = TitleChangeAllowed(f.c, lid)
	if err != nil || ok {
		t.Fatalf("6 days ok=%v err=%v", ok, err)
	}
}

func TestMarketQueries(t *testing.T) {
	f := boot(t)
	shop := f.shop()
	day := chinaToday()
	ago := addDays(day, -3)
	older := addDays(day, -10)
	ins := func(word, date string, vol, cvr any) {
		t.Helper()
		if _, err := f.c.DB.Exec("INSERT INTO market_keywords(shop_id, category, word, date, search_volume, cvr_bps, updated_at) VALUES(?,?,?,?,?,?,?)", shop, "pearl", word, date, vol, cvr, core.Now()); err != nil {
			t.Fatal(err)
		}
	}
	ins("alpha", day, 10, 1000)
	ins("beta", day, 100, nil)
	ins("gamma", day, 20, 5000)
	ins("gamma", ago, 5, 1000)
	words := func(sort string) []map[string]any {
		t.Helper()
		o := f.call("GET", fmt.Sprintf("/api/market/keywords?shop_id=%d&category=pearl&sort=%s", shop, sort), nil, "", 200)
		raw := o["items"].([]any)
		out := make([]map[string]any, len(raw))
		for i, it := range raw {
			out[i] = it.(map[string]any)
		}
		return out
	}
	byScore := words("score")
	if len(byScore) != 3 || byScore[0]["word"] != "gamma" || byScore[1]["word"] != "alpha" || byScore[2]["word"] != "beta" || byScore[2]["score"] != nil {
		t.Fatalf("score order %v", byScore)
	}
	if byScore[0]["score"].(float64) != 10 {
		t.Fatal(byScore[0]["score"])
	}
	trend := byScore[0]["trend"].([]any)
	if len(trend) != 2 || trend[0].(map[string]any)["date"].(string) != ago || trend[1].(map[string]any)["date"].(string) != day {
		t.Fatalf("trend %v", trend)
	}
	byVol := words("volume")
	if byVol[0]["word"] != "beta" || byVol[1]["word"] != "gamma" || byVol[2]["word"] != "alpha" {
		t.Fatalf("volume %v", byVol)
	}
	byCVR := words("cvr")
	if byCVR[0]["word"] != "gamma" || byCVR[1]["word"] != "alpha" || byCVR[2]["word"] != "beta" || byCVR[2]["cvr_bps"] != nil {
		t.Fatalf("cvr %v", byCVR)
	}
	if _, err := f.c.DB.Exec("INSERT INTO competitor_items(shop_id, platform_item_id, keyword, rank, title, material, params, date) VALUES(?,?,?,?,?,?,?,?),(?,?,?,?,?,?,?,?),(?,?,?,?,?,?,?,?)",
		shop, "9", "珍珠", 2, "今天", "淡水", "{}", day,
		shop, "9", "珍珠", 8, "前天", "仿", "{}", ago,
		shop, "8", "珍珠", 1, "太早", "淡水", "{}", older); err != nil {
		t.Fatal(err)
	}
	comps := f.call("GET", fmt.Sprintf("/api/market/competitors?shop_id=%d&keyword=珍珠&days=7", shop), nil, "", 200)
	citems := comps["items"].([]any)
	if len(citems) != 1 || citems[0].(map[string]any)["rank"].(float64) != 2 {
		t.Fatalf("competitors %v", comps)
	}
	filtered := f.call("GET", fmt.Sprintf("/api/market/competitors?shop_id=%d&material=%s&days=7", shop, "仿"), nil, "", 200)
	if len(filtered["items"].([]any)) != 0 {
		t.Fatalf("stale material %v", filtered)
	}
	freshMat := f.call("GET", fmt.Sprintf("/api/market/competitors?shop_id=%d&material=%s&days=7", shop, "淡水"), nil, "", 200)
	if len(freshMat["items"].([]any)) != 1 {
		t.Fatalf("material %v", freshMat)
	}
	f.call("PUT", fmt.Sprintf("/api/market/shops/%d/settings", shop), []byte(`{"daily_budget_cents":-1}`), "", 400)
	f.call("PUT", fmt.Sprintf("/api/market/shops/%d/settings", shop), []byte(`{"daily_budget_cents":12345}`), "", 200)
	settings := f.call("GET", fmt.Sprintf("/api/market/shops/%d/settings", shop), nil, "", 200)
	if settings["daily_budget_cents"].(float64) != 12345 {
		t.Fatal(settings)
	}
	ingested := f.call("POST", "/api/market/ingest", []byte(fmt.Sprintf(`{"shop_id":%d,"kind":"keyword","items":[{"word":"快照词","search_volume":2}]}`, shop)), "", 200)
	snaps := f.call("GET", fmt.Sprintf("/api/market/snapshots?shop_id=%d&kind=keyword", shop), nil, "", 200)
	sitems := snaps["items"].([]any)
	if len(sitems) == 0 {
		t.Fatal("no snapshots")
	}
	if _, ok := sitems[0].(map[string]any)["raw"]; ok {
		t.Fatal("list returned raw")
	}
	detail := f.call("GET", fmt.Sprintf("/api/market/snapshots/%d", int64(ingested["snapshot_id"].(float64))), nil, "", 200)
	if detail["raw"] == nil {
		t.Fatal("detail missing raw")
	}
	lid := f.listing(shop, "https://item.taobao.com/item.htm?id=777")
	if _, err := f.c.DB.Exec("INSERT INTO listing_daily(listing_id, date, impressions, gmv_cents, search_terms, source) VALUES(?,?,?,?,?,?)", lid, day, 5, 500, `[{"term":"珍珠","uv":1,"clicks":2,"orders":3}]`, "csv"); err != nil {
		t.Fatal(err)
	}
	daily := f.call("GET", fmt.Sprintf("/api/market/listings/%d/daily?from=%s&to=%s", lid, day, day), nil, "", 200)
	ditems := daily["items"].([]any)
	if len(ditems) != 1 || ditems[0].(map[string]any)["impressions"].(float64) != 5 {
		t.Fatalf("daily %v", daily)
	}
	terms := ditems[0].(map[string]any)["search_terms"].([]any)
	if terms[0].(map[string]any)["term"] != "珍珠" {
		t.Fatal(terms)
	}
	if _, err := f.c.DB.Exec("INSERT INTO ad_daily(shop_id, listing_id, date, campaign, keyword, cost_cents) VALUES(?,?,?,?,?,?)", shop, lid, day, "计划", "词", 80); err != nil {
		t.Fatal(err)
	}
	ads := f.call("GET", fmt.Sprintf("/api/market/ads?shop_id=%d&from=%s&to=%s&listing_id=%d", shop, day, day, lid), nil, "", 200)
	if ads["items"].([]any)[0].(map[string]any)["cost_cents"].(float64) != 80 {
		t.Fatal(ads)
	}
}

type fixture struct {
	t      *testing.T
	c      *core.Core
	m      *Module
	cookie *http.Cookie
}

func boot(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	c, err := core.New(core.Config{DataDir: filepath.Join(dir, "data"), PromptDir: filepath.Join(dir, "prompts"), TemplateDir: filepath.Join(dir, "templates"), SessionSecret: strings.Repeat("s", 32), AdminUser: "admin", AdminPassword: "password123", Workers: 1}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := New(c)
	if err = c.Start(accounts.New(c), products.New(c), m); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	f := &fixture{t: t, c: c, m: m}
	f.login("admin")
	return f
}

func (f *fixture) call(method, path string, body []byte, token string, status int) map[string]any {
	f.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if f.cookie != nil {
		req.AddCookie(f.cookie)
	}
	w := httptest.NewRecorder()
	f.c.Handler().ServeHTTP(w, req)
	if w.Code != status {
		f.t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	if w.Body.Len() == 0 {
		return nil
	}
	var o map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		f.t.Fatalf("json %s: %v", w.Body.String(), err)
	}
	return o
}

func (f *fixture) login(name string) {
	f.t.Helper()
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(core.JS(map[string]any{"username": name, "password": "password123"})))
	req.Header.Set("X-Requested-With", "fetch")
	w := httptest.NewRecorder()
	f.c.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		f.t.Fatalf("login %s: %d %s", name, w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		f.t.Fatal("no cookie")
	}
	f.cookie = cookies[0]
}

func (f *fixture) exec(q string, args ...any) int64 {
	f.t.Helper()
	res, err := f.c.DB.Exec(q, args...)
	if err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (f *fixture) n(q string, args ...any) int64 {
	f.t.Helper()
	var n int64
	if err := f.c.DB.QueryRow(q, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (f *fixture) user(name, role string) int64 {
	f.t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.exec("INSERT INTO users(username, password_hash, display_name, role_id, created_at) SELECT ?,?,?,id,? FROM roles WHERE key=?", name, hash, name, core.Now(), role)
}

func (f *fixture) link(user, shop int64) {
	f.t.Helper()
	f.exec("INSERT INTO user_shops(user_id, shop_id) VALUES(?,?)", user, shop)
}

func (f *fixture) shop() int64 {
	f.t.Helper()
	return f.n("SELECT id FROM shops ORDER BY id LIMIT 1")
}

func (f *fixture) uid(name string) int64 {
	f.t.Helper()
	return f.n("SELECT id FROM users WHERE username=?", name)
}

func (f *fixture) listing(shop int64, rawURL string) int64 {
	f.t.Helper()
	now := core.Now()
	pid := f.exec("INSERT INTO products(name, created_by, created_at, updated_at) VALUES('商品',?,?,?)", f.uid("admin"), now, now)
	return f.exec("INSERT INTO listings(product_id, shop_id, url, created_by, created_at, updated_at) VALUES(?,?,?,?,?,?)", pid, shop, rawURL, f.uid("admin"), now, now)
}
