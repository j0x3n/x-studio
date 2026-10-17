package pipeline

import (
	"encoding/json"
	"fmt"
)

type slot struct {
	Position   any      `json:"位,omitempty"`
	Screen     any      `json:"屏,omitempty"`
	Name       string   `json:"名"`
	Candidates []string `json:"候选"`
	Required   bool     `json:"必需"`
	Minimum    int      `json:"最少,omitempty"`
	Note       string   `json:"备注,omitempty"`
}

func (a *Module) coverage(photos []Photo) Object {
	var input map[string]json.RawMessage
	_ = json.Unmarshal(a.Slots, &input)
	out := Object{}
	need := []Object{}
	for key, cn := range map[string]string{"main": "主图", "detail": "详情页"} {
		var slots []slot
		_ = json.Unmarshal(input[cn], &slots)
		items := []Object{}
		for _, s := range slots {
			hits := []string{}
			ai := false
			for _, ph := range slotHits(s, photos) {
				hits = append(hits, ph.Code)
				if ph.AIGenerated || ph.AIEnhanced {
					ai = true
				}
			}
			min := s.Minimum
			if min == 0 {
				min = 1
			}
			ok := len(hits) >= min
			pos := s.Position
			if pos == nil {
				pos = s.Screen
			}
			hit := ""
			if len(hits) > 0 {
				hit = hits[0]
			}
			entry := Object{"position": pos, "name": s.Name, "candidates": s.Candidates, "required": s.Required, "matched": hits, "hit": hit, "covered": ok, "note": s.Note, "ai": ai}
			items = append(items, entry)
			if s.Required && !ok && !slotAllowsFill(s) {
				need = append(need, Object{"type": key, "position": pos, "name": s.Name, "candidates": s.Candidates})
			}
		}
		out[key] = items
	}
	out["need_reshoot"] = need
	return out
}

var codes = func() []Object {
	groups := []struct {
		g     string
		names []string
	}{{"A 白底", []string{"一对平放正面", "一对立起 45°", "每个颜色或规格"}}, {"B 微距", []string{"珍珠表面生长纹", "珍珠光泽和反光", "配件做工", "925 印记", "耳针或耳夹结构", "珍珠微瑕"}}, {"C 尺寸", []string{"钢尺参照", "一元硬币参照", "指尖或手心", "同一只耳朵各尺寸上耳"}}, {"D 上耳", []string{"侧脸正对耳朵", "微侧 45°", "正脸半脸", "生活感上耳", "耳饰配颈链"}}, {"E 场景", []string{"深蓝丝绒", "米白亚麻布", "大理石纹场景", "打开的包装盒", "手托托盘批量实拍（不校色）"}}, {"F 包装", []string{"礼盒和品牌卡", "打开的礼盒", "打包好的快递"}}}
	out := []Object{}
	for _, g := range groups {
		for i, n := range g.names {
			out = append(out, Object{"code": fmt.Sprintf("%c%d", g.g[0], i+1), "group": g.g, "name": n})
		}
	}
	return out
}()
