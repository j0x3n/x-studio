package copywriting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"xstudio/internal/core"
)

func (a *Module) generateCopy(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, true) {
		return
	}
	var in struct {
		Instructions string `json:"instructions"`
	}
	if !decodeOptional(w, r, &in) {
		return
	}
	p, e := a.PipelineProduct(id)
	if e != nil {
		core.Fail(w, 404, "上架版本不存在")
		return
	}
	if a.ActiveProduct(p.ID) {
		core.Fail(w, 409, "此上架版本正在排队或处理中")
		return
	}
	params := core.Object{"instructions": strings.TrimSpace(in.Instructions)}
	res, e := a.DB.Exec(
		"INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,created_at,steps) VALUES('copy.generate','listing',?,?,?,'queued',?,'copy.generate')",
		id, core.CurrentUser(r).ID, core.JS(params), core.Now(),
	)
	if e != nil {
		core.Fail(w, 409, "此上架版本已有正在排队或运行的任务")
		return
	}
	jid, _ := res.LastInsertId()
	a.Audit(r, "copy.generate", "listing", id, core.Object{"job_id": jid, "instructions": in.Instructions})
	j, _ := core.ScanJob(a.DB.QueryRow("SELECT "+core.JobColumns+" FROM jobs WHERE id=?", jid))
	a.Wake()
	core.WriteJSON(w, 202, core.Object{"job": j})
}

func (a *Module) rewriteCopy(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	cid := core.ID(r, "id")
	orig, e := scanCopyVersion(a.DB.QueryRow("SELECT "+copyColumns+" FROM copy_versions WHERE id=?", cid))
	if e != nil {
		core.Fail(w, 404, "文案版本不存在")
		return
	}
	if !a.RequireListing(w, r, orig.ListingID, true) {
		return
	}
	var in struct {
		Scope        string `json:"scope"`
		Instructions string `json:"instructions"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if in.Scope != "title" && in.Scope != "selling" && in.Scope != "all" {
		core.Fail(w, 400, "改写范围 scope 只能是 title、selling 或 all")
		return
	}
	p, e := a.PipelineProduct(orig.ListingID)
	if e != nil {
		core.Fail(w, 404, "上架版本不存在")
		return
	}
	if a.ActiveProduct(p.ID) {
		core.Fail(w, 409, "此上架版本正在排队或处理中")
		return
	}
	params := core.Object{
		"copy_id":      orig.ID,
		"scope":        in.Scope,
		"instructions": strings.TrimSpace(in.Instructions),
	}
	res, e := a.DB.Exec(
		"INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,created_at,steps) VALUES('copy.rewrite','listing',?,?,?,'queued',?,'copy.rewrite')",
		orig.ListingID, core.CurrentUser(r).ID, core.JS(params), core.Now(),
	)
	if e != nil {
		core.Fail(w, 409, "此上架版本已有正在排队或运行的任务")
		return
	}
	jid, _ := res.LastInsertId()
	a.Audit(r, "copy.rewrite", "listing", orig.ListingID, core.Object{"copy_id": orig.ID, "job_id": jid, "scope": in.Scope, "instructions": in.Instructions})
	j, _ := core.ScanJob(a.DB.QueryRow("SELECT "+core.JobColumns+" FROM jobs WHERE id=?", jid))
	a.Wake()
	core.WriteJSON(w, 202, core.Object{"job": j})
}

func (a *Module) shopContext(shopID int64) (voiceText string, voiceExamples []string, topKeywords []core.Object, bannedWords []string) {
	var exRaw string
	_ = a.DB.QueryRow("SELECT text,examples FROM voice_profiles WHERE shop_id=?", shopID).Scan(&voiceText, &exRaw)
	_ = json.Unmarshal([]byte(exRaw), &voiceExamples)
	if voiceExamples == nil {
		voiceExamples = []string{}
	}
	rows, e := a.DB.Query("SELECT word,grp,weight,note FROM keywords WHERE shop_id IS NULL OR shop_id=? ORDER BY weight DESC, id ASC", shopID)
	if e != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var word, grp, note string
		var weight int
		if rows.Scan(&word, &grp, &weight, &note) != nil {
			continue
		}
		if grp == "禁用词" {
			bannedWords = append(bannedWords, word)
		} else if len(topKeywords) < 50 {
			topKeywords = append(topKeywords, core.Object{"word": word, "grp": grp, "weight": weight, "note": note})
		}
	}
	return
}

func (a *Module) runGenerate(ctx context.Context, j core.Job) error {
	basePrompt, e := a.prompt("文案.md")
	if e != nil {
		return e
	}
	extraPrompt, e := a.prompt("文案-附加.md")
	if e != nil {
		return e
	}
	p, e := a.PipelineProduct(j.TargetID)
	if e != nil {
		return e
	}
	instructions, _ := j.Params["instructions"].(string)
	voiceText, voiceExamples, topKeywords, bannedWords := a.shopContext(p.ShopID)
	voiceFull := voiceText
	if len(voiceExamples) > 0 {
		voiceFull = strings.TrimSpace(voiceFull + "\n示例：" + strings.Join(voiceExamples, "；"))
	}
	kwStrings := []string{}
	for _, kw := range topKeywords {
		w, _ := kw["word"].(string)
		g, _ := kw["grp"].(string)
		if g != "" {
			kwStrings = append(kwStrings, fmt.Sprintf("%s(%s)", w, g))
		} else {
			kwStrings = append(kwStrings, w)
		}
	}
	replacer := strings.NewReplacer(
		"{{voice}}", voiceFull,
		"{{keywords}}", strings.Join(kwStrings, "、"),
		"{{banned}}", strings.Join(bannedWords, "、"),
		"{{instructions}}", instructions,
	)
	renderedExtra := replacer.Replace(extraPrompt)

	samples := core.Object{}
	for _, name := range []string{"内容.js", "主图.js"} {
		if b, err := os.ReadFile(filepath.Join(a.Cfg.TemplateDir, "详情页模板", "商品", "示例款", name)); err == nil {
			samples[name] = string(b)
		}
	}
	photos, e := a.Photos(p.ID)
	if e != nil {
		return e
	}
	notes := []core.Object{}
	for _, ph := range photos {
		if ph.Code != "" {
			notes = append(notes, core.Object{"code": ph.Code, "file": ph.Code + ".jpg", "note": ph.AINote, "ai_generated": ph.AIGenerated, "ai_enhanced": ph.AIEnhanced})
		}
	}
	entries, _ := os.ReadDir(filepath.Join(a.Dir(p.ID), "照片"))
	files := []string{}
	for _, f := range entries {
		if !f.IsDir() {
			files = append(files, f.Name())
		}
	}
	var cov core.Object
	if a.AssetCoverage != nil {
		cov = a.AssetCoverage(photos)
	}
	input := core.Object{
		"platform":        p.Platform,
		"language":        p.Language,
		"name":            p.Name,
		"info":            p.Info,
		"photos":          notes,
		"files":           files,
		"coverage":        cov,
		"slots":           json.RawMessage(a.Slots),
		"samples":         samples,
		"voice":           core.Object{"text": voiceText, "examples": voiceExamples},
		"keywords":        topKeywords,
		"banned_keywords": bannedWords,
		"instructions":    instructions,
		"要求":              "返回 {content,main,listing} 三个 JSON 对象，按样例字段类型。不得虚构材质、承诺或照片。",
	}
	var result struct {
		Content core.Object `json:"content"`
		Main    core.Object `json:"main"`
		Listing core.Object `json:"listing"`
		Meta    core.Object `json:"meta"`
	}
	fullPrompt := basePrompt + "\n" + renderedExtra
	if e = a.AIJSON(core.WithFeature(ctx, "copy"), a.Cfg.TextModel, fullPrompt, core.JS(input), &result); e != nil {
		return e
	}
	meta := result.Listing
	if meta == nil {
		meta = result.Meta
	}
	if result.Content == nil || result.Main == nil || meta == nil {
		return fmt.Errorf("AI 文案必须包含 content、main、listing 三个对象")
	}
	result.Main = a.cleanMainForProduct(p.ID, result.Main)
	if e = a.annotateAI(p.ID, result.Content, result.Main); e != nil {
		return e
	}
	if e = validateCopy(result.Content, result.Main, meta); e != nil {
		return e
	}
	normalizeCopy(result.Content, result.Main, meta)
	result.Content = a.cleanObjectForProduct(p.ID, result.Content)
	bannedText, _ := a.prompt("违禁词.txt")
	if len(bannedWords) > 0 {
		bannedText += "\n" + strings.Join(bannedWords, "\n")
	}
	meta["compliance"] = appendCompliance(result.Content, result.Main, meta, bannedText)

	title, _ := meta["title"].(string)
	if title == "" {
		title = p.Name
	}
	res, e := a.DB.Exec(
		"INSERT INTO copy_versions(listing_id,title,content,main,meta,source,note,created_by,created_at,applied_at) VALUES(?,?,?,?,?,'ai_generate',?,?,?,'')",
		p.ListingID, title, core.JS(result.Content), core.JS(result.Main), core.JS(meta), instructions, j.UserID, core.Now(),
	)
	if e != nil {
		return e
	}
	vid, _ := res.LastInsertId()
	_, e = a.DB.Exec("UPDATE jobs SET message='文案生成完成',result=? WHERE id=?", core.JS(core.Object{"copy_id": vid}), j.ID)
	return e
}

func (a *Module) runRewrite(ctx context.Context, j core.Job) error {
	prompt, e := a.prompt("文案-改写.md")
	if e != nil {
		return e
	}
	cidFloat, _ := j.Params["copy_id"].(float64)
	cid := int64(cidFloat)
	scope, _ := j.Params["scope"].(string)
	instructions, _ := j.Params["instructions"].(string)

	orig, e := scanCopyVersion(a.DB.QueryRow("SELECT "+copyColumns+" FROM copy_versions WHERE id=?", cid))
	if e != nil {
		return fmt.Errorf("原文案版本不存在")
	}
	p, e := a.PipelineProduct(orig.ListingID)
	if e != nil {
		return e
	}

	voiceText, voiceExamples, topKeywords, bannedWords := a.shopContext(p.ShopID)
	voiceFull := voiceText
	if len(voiceExamples) > 0 {
		voiceFull = strings.TrimSpace(voiceFull + "\n示例：" + strings.Join(voiceExamples, "；"))
	}
	if topKeywords == nil {
		topKeywords = []core.Object{}
	}
	if bannedWords == nil {
		bannedWords = []string{}
	}

	input := core.Object{
		"copy": core.Object{
			"content": orig.Content,
			"main":    orig.Main,
			"meta":    orig.Meta,
		},
		"scope":        scope,
		"instructions": instructions,
		"voice":        voiceFull,
		"keywords":     topKeywords,
		"banned":       bannedWords,
	}
	var rawAI core.Object
	if e = a.AIJSON(core.WithFeature(ctx, "copy_rewrite"), a.Cfg.TextModel, prompt, core.JS(input), &rawAI); e != nil {
		return e
	}
	if rawAI == nil {
		return fmt.Errorf("AI 改写返回为空")
	}

	newContent, newMain, newMeta, ignored := mergeRewriteCopy(orig.Content, orig.Main, orig.Meta, rawAI, scope)

	if e = validateCopy(newContent, newMain, newMeta); e != nil {
		return e
	}
	normalizeCopy(newContent, newMain, newMeta)
	bannedText, _ := a.prompt("违禁词.txt")
	if len(bannedWords) > 0 {
		bannedText += "\n" + strings.Join(bannedWords, "\n")
	}
	newMeta["compliance"] = appendCompliance(newContent, newMain, newMeta, bannedText)

	title, _ := newMeta["title"].(string)
	if title == "" {
		title = orig.Title
	}
	res, e := a.DB.Exec(
		"INSERT INTO copy_versions(listing_id,title,content,main,meta,source,note,created_by,created_at,applied_at) VALUES(?,?,?,?,?,'ai_rewrite',?,?,?,'')",
		orig.ListingID, title, core.JS(newContent), core.JS(newMain), core.JS(newMeta), instructions, j.UserID, core.Now(),
	)
	if e != nil {
		return e
	}
	vid, _ := res.LastInsertId()
	msg := "文案改写完成"
	if ignored > 0 {
		msg = fmt.Sprintf("文案改写完成；已忽略 %d 处超出改写范围的改动", ignored)
	}
	_, e = a.DB.Exec("UPDATE jobs SET message=?,result=? WHERE id=?", msg, core.JS(core.Object{"copy_id": vid}), j.ID)
	return e
}

func sameTextOrSliceType(origVal, aiVal any) bool {
	switch origVal.(type) {
	case string:
		_, ok := aiVal.(string)
		return ok
	case []any:
		arr, ok := aiVal.([]any)
		if !ok {
			return false
		}
		for _, elem := range arr {
			if _, ok := elem.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func cloneValue(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func isAllowedRewriteScalarOrSlice(scope, path string) bool {
	switch path {
	case "meta.title", "meta.title_alts":
		return scope == "title" || scope == "all"
	case "content.oneLiner", "content.tags", "content.real.note", "content.detailsLead", "content.wear.lead", "content.sizeLead", "content.relatedLead":
		return scope == "selling" || scope == "all"
	case "content.name", "content.nameEn":
		return scope == "all"
	default:
		return false
	}
}

func hasComplianceWarn(list []any, text string) bool {
	for _, item := range list {
		if m, ok := item.(core.Object); ok {
			if m["level"] == "warn" && m["text"] == text {
				return true
			}
		}
	}
	return false
}

func hasComplianceExact(list []any, target core.Object) bool {
	want := core.JS(target)
	for _, item := range list {
		if core.JS(item) == want {
			return true
		}
	}
	return false
}

func mergeRewriteCopy(origContent, origMain, origMeta, rawAI core.Object, scope string) (newContent, newMain, newMeta core.Object, ignored int) {
	newContent = cloneObject(origContent)
	newMain = cloneObject(origMain)
	newMeta = cloneObject(origMeta)

	for k := range rawAI {
		if k != "content" && k != "main" && k != "meta" && k != "listing" {
			ignored++
		}
	}

	var aiContent, aiMain, aiMeta core.Object
	if v, exists := rawAI["content"]; exists && v != nil {
		if o, ok := v.(core.Object); ok {
			aiContent = o
		} else {
			ignored++
		}
	}
	if v, exists := rawAI["main"]; exists && v != nil {
		if o, ok := v.(core.Object); ok {
			aiMain = o
		} else {
			ignored++
		}
	}
	if v, exists := rawAI["meta"]; exists && v != nil {
		if o, ok := v.(core.Object); ok {
			aiMeta = o
		} else {
			ignored++
		}
	} else if v, exists := rawAI["listing"]; exists && v != nil {
		if o, ok := v.(core.Object); ok {
			aiMeta = o
		} else {
			ignored++
		}
	}

	allowSelling := scope == "selling" || scope == "all"

	for k, aiVal := range aiContent {
		switch k {
		case "real", "wear":
			origSub, origOk := origContent[k].(core.Object)
			aiSub, aiOk := aiVal.(core.Object)
			if !origOk || !aiOk {
				if core.JS(aiVal) != core.JS(origContent[k]) {
					ignored++
				}
				continue
			}
			newSub, _ := newContent[k].(core.Object)
			for sk, sAIVal := range aiSub {
				sOrigVal, sExists := origSub[sk]
				if !sExists {
					ignored++
					continue
				}
				subpath := "content." + k + "." + sk
				if isAllowedRewriteScalarOrSlice(scope, subpath) && sameTextOrSliceType(sOrigVal, sAIVal) {
					newSub[sk] = cloneValue(sAIVal)
				} else if core.JS(sAIVal) != core.JS(sOrigVal) {
					ignored++
				}
			}
		case "details":
			origArr, origOk := origContent["details"].([]any)
			aiArr, aiOk := aiVal.([]any)
			if !origOk || !aiOk {
				if core.JS(aiVal) != core.JS(origContent["details"]) {
					ignored++
				}
				continue
			}
			newArr, _ := newContent["details"].([]any)
			for i, item := range aiArr {
				if i >= len(origArr) {
					ignored++
					continue
				}
				origElem, oOk := origArr[i].(core.Object)
				aiElem, aOk := item.(core.Object)
				if !oOk || !aOk {
					if core.JS(item) != core.JS(origArr[i]) {
						ignored++
					}
					continue
				}
				newElem, _ := newArr[i].(core.Object)
				for ek, eAIVal := range aiElem {
					eOrigVal, eExists := origElem[ek]
					if !eExists {
						ignored++
						continue
					}
					if allowSelling && (ek == "title" || ek == "text") && sameTextOrSliceType(eOrigVal, eAIVal) {
						newElem[ek] = cloneValue(eAIVal)
					} else if core.JS(eAIVal) != core.JS(eOrigVal) {
						ignored++
					}
				}
			}
		default:
			origVal, exists := origContent[k]
			if !exists {
				ignored++
				continue
			}
			path := "content." + k
			if isAllowedRewriteScalarOrSlice(scope, path) && sameTextOrSliceType(origVal, aiVal) {
				newContent[k] = cloneValue(aiVal)
			} else if core.JS(aiVal) != core.JS(origVal) {
				ignored++
			}
		}
	}

	for k, aiVal := range aiMain {
		if k == "imgs" {
			origArr, origOk := origMain["imgs"].([]any)
			aiArr, aiOk := aiVal.([]any)
			if !origOk || !aiOk {
				if core.JS(aiVal) != core.JS(origMain["imgs"]) {
					ignored++
				}
				continue
			}
			newArr, _ := newMain["imgs"].([]any)
			for i, item := range aiArr {
				if i >= len(origArr) {
					ignored++
					continue
				}
				origElem, oOk := origArr[i].(core.Object)
				aiElem, aOk := item.(core.Object)
				if !oOk || !aOk {
					if core.JS(item) != core.JS(origArr[i]) {
						ignored++
					}
					continue
				}
				newElem, _ := newArr[i].(core.Object)
				for ek, eAIVal := range aiElem {
					eOrigVal, eExists := origElem[ek]
					if !eExists {
						ignored++
						continue
					}
					if allowSelling && (ek == "text" || ek == "label") && sameTextOrSliceType(eOrigVal, eAIVal) {
						newElem[ek] = cloneValue(eAIVal)
					} else if core.JS(eAIVal) != core.JS(eOrigVal) {
						ignored++
					}
				}
			}
			continue
		}
		origVal, exists := origMain[k]
		if !exists || core.JS(aiVal) != core.JS(origVal) {
			ignored++
		}
	}

	for k, aiVal := range aiMeta {
		if k == "compliance" {
			aiComp, aiOk := aiVal.([]any)
			if !aiOk {
				if core.JS(aiVal) != core.JS(origMeta["compliance"]) {
					ignored++
				}
				continue
			}
			origComp, _ := origMeta["compliance"].([]any)
			mergedComp, _ := newMeta["compliance"].([]any)
			for _, item := range aiComp {
				m, ok := item.(core.Object)
				if !ok {
					ignored++
					continue
				}
				level, _ := m["level"].(string)
				text, textOk := m["text"].(string)
				if level == "warn" && textOk && strings.TrimSpace(text) != "" {
					if !hasComplianceWarn(mergedComp, text) {
						mergedComp = append(mergedComp, core.Object{"level": "warn", "text": text})
					}
				} else if !hasComplianceExact(origComp, m) {
					ignored++
				}
			}
			if len(mergedComp) > 0 || origMeta["compliance"] != nil {
				newMeta["compliance"] = mergedComp
			}
			continue
		}
		origVal, exists := origMeta[k]
		if !exists {
			ignored++
			continue
		}
		path := "meta." + k
		if isAllowedRewriteScalarOrSlice(scope, path) && sameTextOrSliceType(origVal, aiVal) {
			newMeta[k] = cloneValue(aiVal)
		} else if core.JS(aiVal) != core.JS(origVal) {
			ignored++
		}
	}
	return
}

type ruleItem struct {
	level   string
	word    string
	reason  string
	suggest string
}

func (a *Module) loadBannedRules(shopID int64) []ruleItem {
	rules := []ruleItem{}
	seen := map[string]bool{}
	if b, e := os.ReadFile(filepath.Join(a.Cfg.PromptDir, "违禁词.txt")); e == nil {
		for _, line := range strings.Split(string(b), "\n") {
			word := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
			if word == "" || core.HasPrefix(word, "#", "//") {
				continue
			}
			level, desc := "error", "违禁词"
			cols := strings.SplitN(strings.ReplaceAll(word, `\t`, "\t"), "\t", 3)
			if len(cols) >= 2 && (cols[0] == "error" || cols[0] == "warn") {
				level = cols[0]
				word = strings.TrimSpace(cols[1])
				if len(cols) == 3 && strings.TrimSpace(cols[2]) != "" {
					desc = strings.TrimSpace(cols[2])
				}
			}
			if word != "" && !seen[word] {
				seen[word] = true
				rules = append(rules, ruleItem{level: level, word: word, reason: desc})
			}
		}
	}
	rows, e := a.DB.Query("SELECT word,note FROM keywords WHERE (shop_id IS NULL OR shop_id=?) AND grp='禁用词' ORDER BY id", shopID)
	if e == nil {
		defer rows.Close()
		for rows.Next() {
			var word, note string
			if rows.Scan(&word, &note) == nil {
				word = strings.TrimSpace(word)
				if word != "" && !seen[word] {
					seen[word] = true
					reason := strings.TrimSpace(note)
					if reason == "" {
						reason = "店铺禁用词"
					}
					rules = append(rules, ruleItem{level: "error", word: word, reason: reason})
				}
			}
		}
	}
	return rules
}

func (a *Module) checkCopy(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	v, e := scanCopyVersion(a.DB.QueryRow("SELECT "+copyColumns+" FROM copy_versions WHERE id=?", id))
	if e != nil {
		core.Fail(w, 404, "文案版本不存在")
		return
	}
	if !a.RequireListing(w, r, v.ListingID, false) {
		return
	}
	var shopID int64
	_ = a.DB.QueryRow("SELECT shop_id FROM listings WHERE id=?", v.ListingID).Scan(&shopID)

	rules := a.loadBannedRules(shopID)
	issues := []core.Object{}
	seenHit := map[string]bool{}

	var walk func(val any, path string)
	walk = func(val any, path string) {
		switch x := val.(type) {
		case string:
			for _, rule := range rules {
				if strings.Contains(x, rule.word) {
					key := path + "::" + rule.word
					if !seenHit[key] {
						seenHit[key] = true
						issues = append(issues, core.Object{
							"level":   rule.level,
							"field":   path,
							"text":    rule.word,
							"reason":  rule.reason,
							"suggest": rule.suggest,
						})
					}
				}
			}
		case map[string]any:
			for k, child := range x {
				if path == "meta" && k == "compliance" {
					continue
				}
				walk(child, path+"."+k)
			}
		case []any:
			for i, child := range x {
				walk(child, fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}

	walk(v.Content, "content")
	walk(v.Main, "main")
	walk(v.Meta, "meta")

	if title, ok := v.Meta["title"].(string); ok && titleLength(title) > 60 {
		issues = append(issues, core.Object{
			"level":   "error",
			"field":   "meta.title",
			"text":    title,
			"reason":  "标题超过 60 字符（汉字算 2），请缩短后再上架",
			"suggest": "精简标题至 60 字符以内",
		})
	}

	if r.URL.Query().Get("ai") != "0" {
		aiIssues, err := a.aiCheckCopy(r, v)
		if err != nil {
			issues = append(issues, core.Object{
				"level":   "warn",
				"field":   "",
				"text":    "AI 审查暂不可用",
				"reason":  "AI 审查暂不可用",
				"suggest": "",
			})
		} else {
			issues = append(issues, aiIssues...)
		}
	}

	core.WriteJSON(w, 200, core.Object{"issues": issues})
}

func (a *Module) aiCheckCopy(r *http.Request, v CopyVersion) ([]core.Object, error) {
	prompt, e := a.prompt("合规审查.md")
	if e != nil {
		return nil, e
	}
	ctx := core.WithFeature(core.WithAIActor(r.Context(), core.AIActor{UserID: core.CurrentUser(r).ID, Feature: "copy_check"}), "copy_check")
	input := core.Object{
		"title":   v.Title,
		"content": v.Content,
		"main":    v.Main,
		"meta":    v.Meta,
	}
	var raw json.RawMessage
	if e := a.AIJSON(ctx, a.Cfg.TextModel, prompt, core.JS(input), &raw); e != nil {
		return nil, e
	}
	var list []core.Object
	if len(raw) > 0 && raw[0] == '{' {
		var wrap struct {
			Issues []core.Object `json:"issues"`
		}
		if e := json.Unmarshal(raw, &wrap); e != nil {
			return nil, e
		}
		list = wrap.Issues
	} else if e := json.Unmarshal(raw, &list); e != nil {
		return nil, e
	}
	out := []core.Object{}
	for _, item := range list {
		level, _ := item["level"].(string)
		if level != "error" && level != "warn" {
			level = "warn"
		}
		field, _ := item["field"].(string)
		text, _ := item["text"].(string)
		reason, _ := item["reason"].(string)
		suggest, _ := item["suggest"].(string)
		out = append(out, core.Object{
			"level":   level,
			"field":   field,
			"text":    text,
			"reason":  reason,
			"suggest": suggest,
		})
	}
	return out, nil
}
