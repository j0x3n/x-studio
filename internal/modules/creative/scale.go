package creative

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"xstudio/internal/core"
)

var (
	numberRE = regexp.MustCompile(`\d+(?:\.\d+)?`)
	lengthRE = regexp.MustCompile(`(\d+(?:\.\d+)?)(?:\s*(?:cm|厘米)?\s*[/／、或]\s*\d+(?:\.\d+)?)*\s*(?:cm|厘米)`)
)

// productFacts 把商品资料按品类字段的中文名和单位写出来（如「珠径：3.3–3.8 mm」），跳过 auto_* 开关。
func productFacts(product core.Product) string {
	done := map[string]bool{}
	facts := []string{}
	add := func(key, label, unit string) {
		done[key] = true
		value := factValue(product.Info[key])
		if value == "" || strings.HasPrefix(key, "auto_") {
			return
		}
		if unit != "" && !strings.HasSuffix(value, unit) {
			value += " " + unit
		}
		facts = append(facts, label+"："+value)
	}
	if product.Category != nil {
		for _, attr := range product.Category.Attrs {
			add(attr.Key, attr.Label, attr.Unit)
		}
	}
	keys := []string{}
	for key := range product.Info {
		if !done[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		add(key, key, "")
	}
	return strings.Join(facts, "；")
}
func factValue(value any) string {
	if list, ok := value.([]any); ok {
		words := []string{}
		for _, v := range list {
			words = append(words, stringValue(v))
		}
		return strings.Join(words, "、")
	}
	if b, ok := value.(bool); ok && !b {
		return ""
	}
	return strings.TrimSpace(stringValue(value))
}

// beadScale 按珠径和链长算出真实比例：满串大约多少颗、佩戴时颈宽/腕宽里大约几颗。
// 返回给生图的英文说明和给复核的中文说明；资料不够时都为空。
func beadScale(product core.Product) (string, string) {
	raw := stringValue(product.Info["size"])
	if raw == "" || strings.ContainsAny(raw, "×xX*") {
		return "", ""
	}
	numbers := numberRE.FindAllString(raw, 2)
	if len(numbers) == 0 {
		return "", ""
	}
	mm := 0.0
	for _, n := range numbers {
		v, _ := strconv.ParseFloat(n, 64)
		mm += v
	}
	mm /= float64(len(numbers))
	if mm < 1 || mm > 30 {
		return "", ""
	}
	label := strings.TrimSpace(raw)
	en := []string{fmt.Sprintf("Real-world scale, keep it exact: each bead or pearl is about %.1f mm in diameter (%s mm).", mm, label)}
	zh := []string{fmt.Sprintf("珠径约 %.1f mm（资料 %s mm）", mm, label)}
	wear := wearType(stringValue(product.Info["style"]))
	length := strandLength(product)
	if (wear == "necklace" || wear == "bracelet") && length > 0 {
		count := int(math.Round(length * 10 / mm))
		en = append(en, fmt.Sprintf("A %g cm strand fully beaded with them holds about %d beads packed closely side by side.", length, count))
		zh = append(zh, fmt.Sprintf("%g cm 满串约 %d 颗", length, count))
	}
	switch {
	case wear == "necklace" && length > 0:
		half := int(math.Round(length * 5 / mm))
		en = append(en, fmt.Sprintf("Worn and seen from the front, the visible half of the strand (about %g cm, from one side of the neck around the collarbone to the other) holds about %d beads; in a flat lay the whole strand is a long, dense line of tiny beads.", length/2, half))
		zh = append(zh, fmt.Sprintf("佩戴正面能看到约半串（约 %g cm，沿链子从颈侧绕到另一侧）约 %d 颗", length/2, half))
	case wear == "bracelet" && length > 0:
		half := int(math.Round(length * 5 / mm))
		en = append(en, fmt.Sprintf("Worn, the visible top half of the bracelet (about %g cm) holds about %d beads.", length/2, half))
		zh = append(zh, fmt.Sprintf("佩戴时手腕正面能看到约半圈（约 %g cm）约 %d 颗", length/2, half))
	case wear == "earring":
		en = append(en, fmt.Sprintf("Worn, the pearl is about %.0f%% of the earlobe height (earlobe about 17 mm).", mm/17*100))
		zh = append(zh, fmt.Sprintf("约为耳垂高度（约 17 mm）的 %.0f%%", mm/17*100))
	}
	if mm <= 5 && (wear == "necklace" || wear == "bracelet") {
		en = append(en, "These are small seed-size beads: draw many small beads at true scale, never fewer and larger beads. If the reference shows only part of the strand beaded, keep the same proportion as the reference.")
	}
	return strings.Join(en, " "), strings.Join(zh, "；")
}

// strandLength 从上架参数的链长/手围行，或资料亮点里找第一个厘米数。
func strandLength(product core.Product) float64 {
	texts := []string{}
	if rows, ok := product.Content["params"].([]any); ok {
		for _, row := range rows {
			if cells, ok := row.([]any); ok && len(cells) >= 2 {
				name := stringValue(cells[0])
				if strings.Contains(name, "长") || strings.Contains(name, "围") {
					texts = append(texts, stringValue(cells[1]))
				}
			}
		}
	}
	texts = append(texts, stringValue(product.Info["highlights"]))
	for _, text := range texts {
		if m := lengthRE.FindStringSubmatch(text); m != nil {
			v, _ := strconv.ParseFloat(m[1], 64)
			if v >= 5 && v <= 120 {
				return v
			}
		}
	}
	return 0
}
