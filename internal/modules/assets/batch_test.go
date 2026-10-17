package assets_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"xstudio/internal/core"
)

func TestBatchDownloadSimilarScopeAndMove(t *testing.T) {
	f := newFixture(t, nil)
	p := f.product(t, "珍珠", 1)
	dest := f.product(t, "目标", 1)
	hidden := f.product(t, "不可见")
	b := imageBytes(t, 32, 48)
	aid := f.upload(t, p, b)
	hiddenID := f.upload(t, hidden, b)
	f.user(t, "op", 2, 1)
	f.login(t, "op")
	for _, action := range []string{"tag_add", "tag_remove", "archive", "unarchive", "delete"} {
		o := f.req(t, "POST", "/api/assets/batch", obj{"action": action, "asset_ids": []int64{hiddenID}, "tags": []string{"白色"}}, 200)
		if o["items"].([]any)[0].(obj)["error"] == nil {
			t.Fatal("批量操作越权", o)
		}
	}
	f.req(t, "POST", "/api/assets/download", obj{"asset_ids": []int64{aid, hiddenID}}, 404)
	f.req(t, "POST", fmt.Sprintf("/api/assets/%d/similar", hiddenID), obj{}, 404)
	o := f.req(t, "POST", fmt.Sprintf("/api/assets/%d/similar", aid), obj{}, 200)
	if o["total"] != float64(0) {
		t.Fatal("相似图泄漏", o)
	}
	f.req(t, "POST", "/api/assets/batch", obj{"action": "tag_add", "asset_ids": []int64{aid}, "tags": []string{"白色", "白色"}}, 200)
	req, _ := http.NewRequest("POST", f.s.URL+"/api/assets/download", strings.NewReader(fmt.Sprintf(`{"asset_ids":[%d]}`, aid)))
	req.Header.Set("X-Requested-With", "fetch")
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode, string(data))
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(z.File) != 1 || !strings.HasSuffix(z.File[0].Name, "_original.jpg") {
		t.Fatal("下载包不正确", err)
	}
	f.req(t, "POST", "/api/assets/batch", obj{"action": "move", "asset_ids": []int64{aid}, "product_id": hidden}, 404)
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), obj{"code": "B1"}, 200)
	o = f.req(t, "POST", "/api/assets/batch", obj{"action": "move", "asset_ids": []int64{aid}, "product_id": dest}, 200)
	if o["items"].([]any)[0].(obj)["error"] == nil {
		t.Fatal("占用编号仍移动")
	}
	f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", aid), obj{"code": ""}, 200)
	o = f.req(t, "POST", "/api/assets/batch", obj{"action": "move", "asset_ids": []int64{aid}, "product_id": dest}, 200)
	if o["items"].([]any)[0].(obj)["error"] != nil {
		t.Fatal(o)
	}
	var moved int64
	f.c.DB.QueryRow("SELECT product_id FROM assets WHERE id=?", aid).Scan(&moved)
	if moved != dest {
		t.Fatal("移动未保存")
	}
	f.login(t, "admin")
	second := f.upload(t, dest, b)
	f.login(t, "op")
	o = f.req(t, "POST", fmt.Sprintf("/api/assets/%d/similar", aid), obj{}, 200)
	if o["total"] != float64(1) || id(o["items"].([]any)[0]) != second {
		t.Fatal("相似搜索结果不正确", o)
	}
	o = f.req(t, "POST", "/api/assets/batch", obj{"action": "delete", "asset_ids": []int64{second}}, 200)
	if o["items"].([]any)[0].(obj)["error"] == nil {
		t.Fatal("删除了别人的素材")
	}
	f.assertAudit(t, "assets.batch.tag_add", "assets.batch.move", "assets.download")
}
func TestTagMergesTruncatesAndCountsUsage(t *testing.T) {
	var ids []int64
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request obj
		json.NewDecoder(r.Body).Decode(&request)
		parts := request["messages"].([]any)[1].(obj)["content"].([]any)
		count := 0
		for _, v := range parts {
			if v.(obj)["type"] == "image_url" {
				count++
			}
		}
		if count > 8 {
			t.Error("超过 8 张")
		}
		reply := []obj{}
		for _, id := range ids {
			reply = append(reply, obj{"asset_id": id, "tags": []string{"手动", "白色", "白色", "一二三四五六七八九十十一十二", "甲", "乙", "丙", "丁", "戊", "己", "庚"}})
		}
		json.NewEncoder(w).Encode(obj{"choices": []obj{{"message": obj{"content": fmt.Sprint(string(mustJSON(reply)))}}}})
	}))
	defer ai.Close()
	f := newFixture(t, func(c *core.Config) { c.AIKey = "fake"; c.AIBaseURL = ai.URL; c.VisionModel = "vision" })
	p := f.product(t, "标签商品", 1)
	for i := 0; i < 9; i++ {
		ids = append(ids, f.upload(t, p, imageBytes(t, 32, 40)))
	}
	for _, id := range ids {
		f.req(t, "PATCH", fmt.Sprintf("/api/assets/%d", id), obj{"tags": []string{"手动"}}, 200)
	}
	if err := os.MkdirAll(f.c.Cfg.PromptDir, 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(f.c.Cfg.PromptDir, "打标签.md"), []byte("假提示词"), 0600)
	j := f.req(t, "POST", "/api/assets/tag", obj{"asset_ids": ids}, 202)["job"]
	done := f.wait(t, id(j))
	if done.Status != "done" || len(done.Result["items"].([]any)) != 9 {
		t.Fatal(done)
	}
	var tags string
	f.c.DB.QueryRow("SELECT tags FROM assets WHERE id=?", ids[0]).Scan(&tags)
	var values []string
	json.Unmarshal([]byte(tags), &values)
	if len(values) != 8 || values[0] != "手动" {
		t.Fatal(values)
	}
	for _, v := range values {
		if len([]rune(v)) > 10 {
			t.Fatal("AI 标签未截断")
		}
	}
	var usage int
	f.c.DB.QueryRow("SELECT sum(units) FROM ai_usage WHERE feature='tag'").Scan(&usage)
	if usage != 2 {
		t.Fatal("未按两次看图请求计量", usage)
	}
	f.assertAudit(t, "assets.tag")
	os.Remove(filepath.Join(f.c.Cfg.PromptDir, "打标签.md"))
	done = f.wait(t, id(f.req(t, "POST", "/api/assets/tag", obj{"asset_ids": ids}, 202)["job"]))
	if done.Status != "failed" || !strings.Contains(done.Message, "缺少提示词") {
		t.Fatal(done)
	}
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func TestSimilarUsesScopedHashesAndCapsSortedResults(t *testing.T) {
	f := newFixture(t, nil)
	pid, hidden := f.product(t, "可见", 1), f.product(t, "隐藏")
	f.user(t, "operator", 2, 1)
	guest := f.user(t, "guest", 5)
	f.req(t, "POST", fmt.Sprintf("/api/products/%d/shares", pid), obj{"user_id": guest, "can_upload": false}, 201)
	aid := f.upload(t, pid, imageBytes(t, 8, 8))
	var originalHash string
	if err := f.c.DB.QueryRow("SELECT phash FROM assets WHERE id=?", aid).Scan(&originalHash); err != nil {
		t.Fatal(err)
	}
	base, err := strconv.ParseUint(originalHash, 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	f.c.Mu.Lock()
	defer f.c.Mu.Unlock()
	type expected struct {
		id       int64
		distance int
	}
	want := []expected{}
	insert := func(product int64, hash string, archived bool) int64 {
		res, err := f.c.DB.Exec("INSERT INTO assets(product_id,file,orig_name,width,height,created_by,created_at,phash,archived) VALUES(?,'原图/无需读取.png','a',8,8,1,?,?,?)", product, core.Now(), hash, archived)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	for i := 0; i < 60; i++ {
		distance := i % 11
		hash := fmt.Sprintf("%016x", base^((uint64(1)<<distance)-1))
		id := insert(pid, hash, i == 0)
		want = append(want, expected{id, distance})
	}
	hiddenID := insert(hidden, originalHash, false)
	insert(pid, fmt.Sprintf("%016x", base^((uint64(1)<<11)-1)), false)
	empty := insert(pid, "", false)
	insert(pid, "-", false)
	sort.Slice(want, func(i, j int) bool {
		if want[i].distance == want[j].distance {
			return want[i].id < want[j].id
		}
		return want[i].distance < want[j].distance
	})
	if _, err := f.c.DB.Exec("UPDATE assets SET phash='' WHERE id=?", aid); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"operator", "guest"} {
		f.login(t, user)
		f.req(t, "POST", fmt.Sprintf("/api/assets/%d/similar", hiddenID), obj{}, 404)
		o := f.req(t, "POST", fmt.Sprintf("/api/assets/%d/similar", aid), obj{}, 200)
		items := o["items"].([]any)
		if o["total"] != float64(50) || len(items) != 50 {
			t.Fatal("相似图数量不正确", o)
		}
		for i, item := range items {
			v := item.(obj)
			if id(v) != want[i].id || v["distance"] != float64(want[i].distance) || v["product_id"] != float64(pid) {
				t.Fatal("相似图距离、顺序或范围错误", i, v)
			}
		}
		if !items[0].(obj)["archived"].(bool) {
			t.Fatal("归档素材被排除")
		}
	}
	var targetHash, candidateHash string
	f.c.DB.QueryRow("SELECT phash FROM assets WHERE id=?", aid).Scan(&targetHash)
	f.c.DB.QueryRow("SELECT phash FROM assets WHERE id=?", empty).Scan(&candidateHash)
	if targetHash != originalHash || candidateHash != "" {
		t.Fatal("相似搜索未限制为只补算目标图", targetHash, candidateHash)
	}
}
