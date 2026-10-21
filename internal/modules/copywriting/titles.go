package copywriting

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"xstudio/internal/core"
	"xstudio/internal/modules/market"
)

type titleKeyword struct {
	Word         string   `json:"word"`
	Score        *float64 `json:"score"`
	SearchVolume *int64   `json:"search_volume"`
	CVRBps       *int64   `json:"cvr_bps"`
}

type titleSearchTerm struct {
	Term   string `json:"term"`
	UV     *int64 `json:"uv"`
	Orders *int64 `json:"orders"`
}

type titleHistoryItem struct {
	ID            int64  `json:"id"`
	OldTitle      string `json:"old_title"`
	NewTitle      string `json:"new_title"`
	Reason        string `json:"reason"`
	CreatedBy     int64  `json:"created_by"`
	CreatedByName string `json:"created_by_name"`
	CreatedAt     string `json:"created_at"`
}

func (a *Module) suggestTitles(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, true) {
		return
	}
	var request struct {
		Note string `json:"note"`
	}
	if !decodeOptional(w, r, &request) {
		return
	}
	if utf8.RuneCountInString(request.Note) > 200 {
		core.Fail(w, 400, "补充要求不能超过 200 字")
		return
	}
	p, err := a.PipelineProduct(id)
	if err != nil {
		core.Fail(w, 500, "读取商品资料失败")
		return
	}
	input, keywords, competitors, err := a.titleInputs(p)
	if err != nil {
		core.Fail(w, 500, "读取标题优化资料失败")
		return
	}
	input["note"] = request.Note
	prompt, err := a.prompt("标题-优化.md")
	if err != nil {
		core.Fail(w, 500, err.Error())
		return
	}
	ctx := core.WithFeature(core.WithAIActor(r.Context(), core.AIActor{UserID: core.CurrentUser(r).ID, Feature: "title_suggest"}), "title_suggest")
	var result struct {
		Titles []struct {
			Title string `json:"title"`
			Focus string `json:"focus"`
			Note  string `json:"note"`
		} `json:"titles"`
	}
	valid := false
	for attempt := 0; attempt < 2; attempt++ {
		var raw json.RawMessage
		if err := a.AIJSON(ctx, a.Cfg.TextModel, core.FillPrompt(prompt, p.Category), core.JS(input), &raw); err != nil {
			core.Fail(w, 502, a.SafeAIError(err.Error()))
			return
		}
		result.Titles = nil
		if json.Unmarshal(raw, &result) != nil || len(result.Titles) != 3 {
			continue
		}
		valid = true
		for index := range result.Titles {
			result.Titles[index].Title = strings.TrimSpace(result.Titles[index].Title)
			if result.Titles[index].Title == "" {
				valid = false
			}
		}
		if valid {
			break
		}
	}
	if !valid {
		core.Fail(w, 502, "AI 返回的标题格式不对")
		return
	}
	rules := a.titleRules(p)
	titles := []core.Object{}
	for _, item := range result.Titles {
		covered := []core.Object{}
		for _, keyword := range keywords {
			if strings.Contains(item.Title, keyword.Word) {
				covered = append(covered, core.Object{"word": keyword.Word, "score": keyword.Score})
			}
		}
		overlap, matched, longest := titleOverlap(item.Title, competitors)
		compliance := titleCompliance(item.Title, rules)
		if longest > 6 {
			compliance = append(compliance, core.Object{"level": "warn", "word": matched, "msg": "和竞品标题连续相同超过 6 个字"})
		}
		titles = append(titles, core.Object{
			"title": item.Title, "chars": titleLength(item.Title), "over_limit": titleLength(item.Title) > 60,
			"keywords": covered, "overlap_bps": overlap, "overlap_with": matched,
			"compliance": compliance, "focus": item.Focus, "note": item.Note,
		})
	}
	allowed, next, err := market.TitleChangeAllowed(a.Core, id)
	if err != nil {
		core.Fail(w, 500, "读取标题修改间隔失败")
		return
	}
	counts := core.Object{"keywords": len(keywords), "competitors": len(competitors), "search_terms": len(input["search_terms"].([]titleSearchTerm)), "history": len(input["history"].([]core.Object))}
	a.Audit(r, "title.suggest", "listing", id, core.Object{"inputs": counts})
	current, _ := p.Listing["title"].(string)
	core.WriteJSON(w, 200, core.Object{
		"current": core.Object{"title": current, "chars": titleLength(current)}, "titles": titles,
		"change_allowed": allowed, "next_change_at": titleNextTime(allowed, next), "inputs": counts,
	})
}

func (a *Module) titleInputs(p core.Product) (core.Object, []titleKeyword, []string, error) {
	marketKeywords, err := market.TopKeywords(a.Core, p.ShopID, p.Category.Key, 30)
	if err != nil {
		return nil, nil, nil, err
	}
	keywords := []titleKeyword{}
	seen := map[string]bool{}
	for _, keyword := range marketKeywords {
		if keyword.Word != "" && !seen[keyword.Word] {
			seen[keyword.Word] = true
			keywords = append(keywords, titleKeyword{keyword.Word, keyword.Score, keyword.SearchVolume, keyword.CVRBps})
		}
	}
	_, _, ownKeywords, _ := a.shopContext(p.ShopID)
	for _, keyword := range ownKeywords {
		word, _ := keyword["word"].(string)
		word = strings.TrimSpace(word)
		if word != "" && !seen[word] {
			seen[word] = true
			keywords = append(keywords, titleKeyword{Word: word})
		}
	}
	sort.SliceStable(keywords, func(left, right int) bool {
		if keywords[left].Score == nil {
			return false
		}
		return keywords[right].Score == nil || *keywords[left].Score > *keywords[right].Score
	})
	today := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	start, end := today.AddDate(0, 0, -29).Format("2006-01-02"), today.Format("2006-01-02")
	competitors := []string{}
	competitorInput := []core.Object{}
	if len(keywords) > 0 {
		args := []any{p.ShopID, start, end}
		for _, keyword := range keywords {
			args = append(args, keyword.Word)
		}
		query := `SELECT title FROM competitor_items WHERE shop_id=? AND date>=? AND date<=? AND keyword IN (` + strings.TrimSuffix(strings.Repeat("?,", len(keywords)), ",") + `) AND title<>'' GROUP BY title ORDER BY MIN(rank) IS NULL, MIN(rank), title LIMIT 20`
		rows, err := a.DB.Query(query, args...)
		if err != nil {
			return nil, nil, nil, err
		}
		for rows.Next() {
			var title string
			if err := rows.Scan(&title); err != nil {
				rows.Close()
				return nil, nil, nil, err
			}
			competitors = append(competitors, title)
			competitorInput = append(competitorInput, core.Object{"title": title})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, nil, err
		}
	}
	terms, err := a.titleSearchTerms(p.ListingID, start, end)
	if err != nil {
		return nil, nil, nil, err
	}
	history, err := a.readTitleHistory(p.ListingID, 5)
	if err != nil {
		return nil, nil, nil, err
	}
	historyInput := []core.Object{}
	for _, item := range history {
		changed, err := time.Parse(time.RFC3339Nano, item.CreatedAt)
		if err != nil {
			return nil, nil, nil, err
		}
		day := changed.In(today.Location())
		before, err := a.titleDailySummary(p.ListingID, day.AddDate(0, 0, -7), day)
		if err != nil {
			return nil, nil, nil, err
		}
		after, err := a.titleDailySummary(p.ListingID, day.AddDate(0, 0, 1), day.AddDate(0, 0, 8))
		if err != nil {
			return nil, nil, nil, err
		}
		historyInput = append(historyInput, core.Object{"old_title": item.OldTitle, "new_title": item.NewTitle, "created_at": item.CreatedAt, "before": before, "after": after})
	}
	listing := core.Object{"title": p.Listing["title"], "name": p.Name, "sku": []any{}, "price": nil}
	for _, source := range []core.Object{p.Info, p.Listing} {
		for _, key := range []string{"sku", "price"} {
			if value, exists := source[key]; exists && value != nil {
				listing[key] = value
			}
		}
	}
	if listing["title"] == nil {
		listing["title"] = ""
	}
	return core.Object{"listing": listing, "info": p.Info, "keywords": keywords, "competitors": competitorInput, "search_terms": terms, "history": historyInput}, keywords, competitors, nil
}

func (a *Module) titleSearchTerms(id int64, start, end string) ([]titleSearchTerm, error) {
	rows, err := a.DB.Query("SELECT search_terms FROM listing_daily WHERE listing_id=? AND date>=? AND date<=? ORDER BY date DESC", id, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	terms := []titleSearchTerm{}
	positions := map[string]int{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var items []json.RawMessage
		if json.Unmarshal([]byte(raw), &items) != nil {
			continue
		}
		for _, item := range items {
			var term titleSearchTerm
			if json.Unmarshal(item, &term.Term) != nil {
				var object struct {
					titleSearchTerm
					Word string `json:"word"`
				}
				if json.Unmarshal(item, &object) != nil {
					continue
				}
				term = object.titleSearchTerm
				if term.Term == "" {
					term.Term = object.Word
				}
			}
			term.Term = strings.TrimSpace(term.Term)
			if term.Term == "" {
				continue
			}
			if position, exists := positions[term.Term]; exists {
				terms[position].UV = titleSum(terms[position].UV, term.UV)
				terms[position].Orders = titleSum(terms[position].Orders, term.Orders)
			} else if len(terms) < 30 {
				positions[term.Term] = len(terms)
				terms = append(terms, term)
			}
		}
	}
	return terms, rows.Err()
}

func titleSum(left, right *int64) *int64 {
	if right == nil {
		return left
	}
	value := *right
	if left != nil {
		value += *left
	}
	return &value
}

func (a *Module) titleDailySummary(id int64, start, end time.Time) (core.Object, error) {
	var count int
	var impressions, clicks, orders sql.NullInt64
	err := a.DB.QueryRow(`SELECT COUNT(*),SUM(impressions),SUM(clicks),SUM(orders) FROM listing_daily WHERE listing_id=? AND date>=? AND date<?`, id, start.Format("2006-01-02"), end.Format("2006-01-02")).Scan(&count, &impressions, &clicks, &orders)
	if err != nil || count == 0 {
		return nil, err
	}
	summary := core.Object{}
	for key, value := range map[string]sql.NullInt64{"impressions": impressions, "clicks": clicks, "orders": orders} {
		summary[key] = nil
		if value.Valid {
			summary[key] = value.Int64
		}
	}
	return summary, nil
}

func (a *Module) titleRules(p core.Product) []ruleItem {
	rules := a.loadBannedRules(p.ShopID)
	for _, word := range p.Category.Banned {
		if word = strings.TrimSpace(word); word != "" {
			rules = append(rules, ruleItem{level: "error", word: word, reason: p.Category.Name + "违禁词"})
		}
	}
	for _, word := range []string{"最", "第一", "顶级", "极品", "唯一", "100%", "绝对", "万能", "国家级"} {
		rules = append(rules, ruleItem{level: "error", word: word, reason: "标题含极限词"})
	}
	return rules
}

func titleCompliance(title string, rules []ruleItem) []core.Object {
	issues := []core.Object{}
	positions := map[string]int{}
	for _, rule := range rules {
		if rule.word == "" || !strings.Contains(title, rule.word) {
			continue
		}
		issue := core.Object{"level": rule.level, "word": rule.word, "msg": rule.reason}
		if position, exists := positions[rule.word]; exists {
			if issues[position]["level"] != "error" && rule.level == "error" {
				issues[position] = issue
			}
		} else {
			positions[rule.word] = len(issues)
			issues = append(issues, issue)
		}
	}
	if titleLength(title) > 60 {
		issues = append(issues, core.Object{"level": "error", "word": "", "msg": "标题长度不能超过 60（30 个汉字）"})
	}
	return issues
}

func titleOverlap(title string, competitors []string) (int, string, int) {
	compact := func(text string) []rune {
		return []rune(strings.Map(func(char rune) rune {
			if unicode.IsSpace(char) {
				return -1
			}
			return char
		}, text))
	}
	text := compact(title)
	longest, matched := 0, ""
	if len(text) == 0 {
		return 0, "", 0
	}
	if len(competitors) > 0 {
		matched = competitors[0]
	}
	for _, competitor := range competitors {
		other := compact(competitor)
		previous := make([]int, len(other)+1)
		for _, char := range text {
			for index := len(other) - 1; index >= 0; index-- {
				if char == other[index] {
					previous[index+1] = previous[index] + 1
					if previous[index+1] > longest {
						longest, matched = previous[index+1], competitor
					}
				} else {
					previous[index+1] = 0
				}
			}
		}
	}
	return longest * 10000 / len(text), matched, longest
}

func titleNextTime(allowed bool, next time.Time) string {
	if allowed {
		return ""
	}
	return next.Format(time.RFC3339)
}

func (a *Module) applyTitle(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, true) {
		return
	}
	var request struct {
		Title  string `json:"title"`
		Reason string `json:"reason"`
	}
	if !core.Decode(w, r, &request) {
		return
	}
	if utf8.RuneCountInString(request.Reason) > 200 {
		core.Fail(w, 400, "修改原因不能超过 200 字")
		return
	}
	p, err := a.PipelineProduct(id)
	if err != nil {
		core.Fail(w, 500, "读取商品资料失败")
		return
	}
	if a.ActiveProduct(p.ID) {
		core.Fail(w, 409, "此上架版本正在排队或处理中")
		return
	}
	request.Title = strings.TrimSpace(request.Title)
	compliance := titleCompliance(request.Title, a.titleRules(p))
	message := ""
	if request.Title == "" {
		message = "标题不能为空"
	}
	for _, issue := range compliance {
		if issue["level"] == "error" && message == "" {
			message, _ = issue["msg"].(string)
		}
	}
	if message != "" {
		core.WriteJSON(w, 400, core.Object{"error": message, "compliance": compliance})
		return
	}
	oldTitle, _ := p.Listing["title"].(string)
	if request.Title == oldTitle {
		core.Fail(w, 400, "和现在的标题一样")
		return
	}
	allowed, next, err := market.TitleChangeAllowed(a.Core, id)
	if err != nil {
		core.Fail(w, 500, "读取标题修改间隔失败")
		return
	}
	if !allowed {
		var last string
		if err := a.DB.QueryRow("SELECT created_at FROM listing_title_history WHERE listing_id=? ORDER BY created_at DESC,id DESC LIMIT 1", id).Scan(&last); err != nil {
			core.Fail(w, 500, "读取标题修改历史失败")
			return
		}
		changed, err := time.Parse(time.RFC3339Nano, last)
		if err != nil {
			core.Fail(w, 500, "读取标题修改历史失败")
			return
		}
		days := int(next.Sub(changed) / (24 * time.Hour))
		core.WriteJSON(w, 409, core.Object{"error": fmt.Sprintf("距上次改标题不满 %d 天", days), "next_change_at": titleNextTime(false, next)})
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		core.Fail(w, 500, "应用标题失败")
		return
	}
	defer tx.Rollback()
	ts := core.Now()
	uid := core.CurrentUser(r).ID
	note := "标题建议"
	if request.Reason != "" {
		note += "：" + request.Reason
	}
	if chars := []rune(note); len(chars) > 200 {
		note = string(chars[:200])
	}
	p.Listing["title"] = request.Title
	_, err = tx.Exec("UPDATE listings SET meta=?,updated_at=? WHERE id=?", core.JS(p.Listing), ts, id)
	var copyID int64
	if err == nil {
		var result sql.Result
		result, err = tx.Exec("INSERT INTO copy_versions(listing_id,title,content,main,meta,source,note,created_by,created_at,applied_at) SELECT id,?,content,main,meta,'manual',?,?,?,? FROM listings WHERE id=?", request.Title, note, uid, ts, ts, id)
		if err == nil {
			copyID, err = result.LastInsertId()
		}
	}
	if err == nil {
		err = market.RecordTitleChange(tx, id, oldTitle, request.Title, request.Reason, uid)
	}
	if err == nil {
		err = core.AuditTx(tx, r, "title.apply", "listing", id, core.Object{"old_title": oldTitle, "new_title": request.Title, "reason": request.Reason})
	}
	if err != nil || tx.Commit() != nil {
		core.Fail(w, 500, "应用标题失败")
		return
	}
	allowed, next, err = market.TitleChangeAllowed(a.Core, id)
	if err != nil {
		core.Fail(w, 500, "读取标题修改间隔失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"title": request.Title, "old_title": oldTitle, "next_change_at": titleNextTime(allowed, next), "copy_id": copyID})
}

func (a *Module) readTitleHistory(id int64, limit int) ([]titleHistoryItem, error) {
	rows, err := a.DB.Query(`SELECT h.id,h.old_title,h.new_title,h.reason,h.created_by,COALESCE(NULLIF(u.display_name,''),u.username,''),h.created_at FROM listing_title_history h LEFT JOIN users u ON u.id=h.created_by WHERE h.listing_id=? ORDER BY h.created_at DESC,h.id DESC LIMIT ?`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []titleHistoryItem{}
	for rows.Next() {
		var item titleHistoryItem
		if err := rows.Scan(&item.ID, &item.OldTitle, &item.NewTitle, &item.Reason, &item.CreatedBy, &item.CreatedByName, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *Module) titleHistory(w http.ResponseWriter, r *http.Request) {
	id := core.ID(r, "id")
	if !a.RequireListing(w, r, id, false) {
		return
	}
	items, err := a.readTitleHistory(id, 50)
	if err != nil {
		core.Fail(w, 500, "读取标题修改历史失败")
		return
	}
	allowed, next, err := market.TitleChangeAllowed(a.Core, id)
	if err != nil {
		core.Fail(w, 500, "读取标题修改间隔失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"items": items, "change_allowed": allowed, "next_change_at": titleNextTime(allowed, next)})
}
