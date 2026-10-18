package assets

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"net/http"
	"sort"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/imaging"
)

func decodeObject(o any, out any) error { return json.Unmarshal([]byte(js(o)), out) }
func (a *Module) tag(w http.ResponseWriter, r *http.Request) {
	var in selection
	if !decode(w, r, &in) {
		return
	}
	if err := validIDs(in.AssetIDs); err != nil {
		fail(w, 400, err.Error())
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	items, err := a.selected(currentUser(r), in.AssetIDs)
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	pid := items[0].ProductID
	for _, p := range items {
		if p.ProductID != pid {
			fail(w, 400, "一次打标签的素材必须属于同一商品")
			return
		}
		if !a.CanUpload(currentUser(r), pid) {
			fail(w, 403, "此商品不允许打标签")
			return
		}
	}
	if a.ActiveProduct(pid) {
		fail(w, 409, "商品正在进行一键出图")
		return
	}
	j, err := a.enqueue("assets.tag", pid, currentUser(r).ID, in)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	a.Audit(r, "assets.tag", "product", pid, Object{"asset_ids": in.AssetIDs, "job_id": j.ID})
	writeJSON(w, 202, Object{"job": j})
}
func (a *Module) tagVocabulary(pid int64, u core.User) ([]string, error) {
	rows, err := core.QueryObjects(a.DB, "SELECT product_id,tags FROM assets WHERE product_id=? OR product_id IN (SELECT b.product_id FROM listings a JOIN listings b ON a.shop_id=b.shop_id WHERE a.product_id=?)", pid, pid)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, r := range rows {
		if !a.CanProduct(u, r["product_id"].(int64)) {
			continue
		}
		if tags, ok := r["tags"].([]any); ok {
			seen := map[string]bool{}
			for _, v := range tags {
				if s, ok := v.(string); ok && !seen[s] {
					counts[s]++
					seen[s] = true
				}
			}
		}
	}
	tags := []string{}
	for s := range counts {
		tags = append(tags, s)
	}
	sort.Slice(tags, func(i, j int) bool {
		if counts[tags[i]] == counts[tags[j]] {
			return tags[i] < tags[j]
		}
		return counts[tags[i]] > counts[tags[j]]
	})
	return tags[:min(200, len(tags))], nil
}
func aiTags(tags []string) []string {
	out := []string{}
	for _, s := range tags {
		r := []rune(strings.TrimSpace(s))
		if len(r) > 10 {
			r = r[:10]
		}
		out = append(out, string(r))
	}
	out = normalizeTags(out)
	return out[:min(8, len(out))]
}
func (a *Module) executeTag(ctx context.Context, j core.Job) error {
	unlock := a.lockAI(j.UserID)
	defer unlock()
	ids, err := jobSelection(j)
	if err != nil {
		return err
	}
	u, items, err := a.jobAssets(ctx, j, ids, core.AssetUpload)
	if err != nil {
		return err
	}
	prompt, err := a.prompt("打标签.md")
	if err != nil {
		return err
	}
	c, err := a.ProductCategory(j.TargetID)
	if err != nil {
		return err
	}
	prompt = core.FillPrompt(prompt, c)
	if a.Cfg.AIKey == "" {
		return fmt.Errorf("未配置 AI_API_KEY，无法开始整批打标签")
	}
	if err = a.checkQuota(j.UserID, 0, (len(items)+7)/8); err != nil {
		return err
	}
	vocab, err := a.tagVocabulary(j.TargetID, u)
	if err != nil {
		return fmt.Errorf("读取已有标签失败")
	}
	results := make([]Object, len(items))
	proposed := map[int64][]string{}
	for offset := 0; offset < len(items); offset += 8 {
		if ctx.Err() != nil {
			return fmt.Errorf("打标签任务已取消")
		}
		parts := []Object{{"type": "text", "text": "已有标签：" + js(vocab) + "；返回 [{asset_id,tags}]，每张最多 8 个标签，每个最多 10 字。"}}
		requested := map[int64]int{}
		for i := offset; i < min(offset+8, len(items)); i++ {
			p := items[i]
			results[i] = Object{"asset_id": p.ID}
			im, ex, e := a.load(p)
			if e != nil {
				results[i]["error"] = e.Error()
				continue
			}
			var buf bytes.Buffer
			_ = jpeg.Encode(&buf, imaging.Resize(imaging.Geometry(im, ex.Orientation, 0, nil), 768), &jpeg.Options{Quality: 85})
			parts = append(parts, Object{"type": "text", "text": fmt.Sprintf("asset_id=%d", p.ID)}, Object{"type": "image_url", "image_url": Object{"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())}})
			requested[p.ID] = i
		}
		if len(requested) == 0 {
			continue
		}
		var raw json.RawMessage
		e := a.AIJSON(core.WithFeature(ctx, "tag"), a.Cfg.VisionModel, prompt, parts, &raw)
		if e != nil {
			if strings.Contains(e.Error(), "额度") {
				return e
			}
			for _, i := range requested {
				results[i]["error"] = a.SafeAIError(e.Error())
			}
			continue
		}
		var reply []struct {
			AssetID int64    `json:"asset_id"`
			Tags    []string `json:"tags"`
		}
		if e = json.Unmarshal(raw, &reply); e != nil {
			var wrap struct {
				Items []struct {
					AssetID int64    `json:"asset_id"`
					Tags    []string `json:"tags"`
				} `json:"items"`
			}
			e = json.Unmarshal(raw, &wrap)
			reply = wrap.Items
		}
		if e != nil || reply == nil {
			for _, i := range requested {
				results[i]["error"] = "AI 标签回复格式不正确"
			}
			continue
		}
		seen := map[int64]bool{}
		for _, v := range reply {
			if _, ok := requested[v.AssetID]; ok && !seen[v.AssetID] {
				proposed[v.AssetID] = aiTags(v.Tags)
				seen[v.AssetID] = true
			}
		}
		for id, i := range requested {
			if !seen[id] {
				results[i]["error"] = "AI 回复缺少该素材的标签"
			}
		}
	}
	for i, p := range items {
		tags, ok := proposed[p.ID]
		if !ok {
			continue
		}
		a.Mu.Lock()
		fresh, e := a.asset(p.ID)
		actor, userErr := a.User(j.UserID)
		if e == nil && userErr == nil && !actor.Disabled && fresh.ProductID == j.TargetID && a.CanUpload(actor, fresh.ProductID) {
			tags = normalizeTags(append(fresh.Tags, tags...))
			_, e = a.DB.Exec("UPDATE assets SET tags=?,updated_at=? WHERE id=?", js(tags), now(), p.ID)
		} else {
			e = fmt.Errorf("素材已移动或权限已变化")
		}
		a.Mu.Unlock()
		if e != nil {
			results[i]["error"] = e.Error()
		} else {
			results[i]["tags"] = tags
		}
	}
	return a.saveResults(j, results)
}
