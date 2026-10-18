package kb

import (
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

type Hit struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Folder  Named  `json:"folder"`
	Snippet string `json:"snippet"`
	Path    string `json:"path"`
}

func likeWord(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s) + "%"
}

// queryTerms is shared by the list endpoint and Search, including literal LIKE
// escaping, AND matching and the exact relevance weights from the contract.
func queryTerms(q string) (where, order string, args []any) {
	words := strings.Fields(q)
	conditions := []string{"a.deleted_at IS NULL"}
	score := []string{"a.pinned*5"}
	filterArgs := []any{}
	scoreArgs := []any{}
	for _, word := range words {
		conditions = append(conditions, `(a.title LIKE ? ESCAPE '\' OR a.body LIKE ? ESCAPE '\')`)
		filterArgs = append(filterArgs, likeWord(word), likeWord(word))
		score = append(score, `CASE WHEN a.title LIKE ? ESCAPE '\' THEN 10 ELSE 0 END`, `CASE WHEN a.body LIKE ? ESCAPE '\' THEN 1 ELSE 0 END`)
		scoreArgs = append(scoreArgs, likeWord(word), likeWord(word))
	}
	order = "a.pinned DESC,a.updated_at DESC,a.id DESC"
	if len(words) > 0 {
		order = "(" + strings.Join(score, "+") + ") DESC,a.updated_at DESC,a.id DESC"
	}
	return strings.Join(conditions, " AND "), order, append(filterArgs, scoreArgs...)
}

// Search returns live articles using the same matching and ranking as the API.
func Search(db *sql.DB, q string, limit int) ([]Hit, error) {
	if limit < 1 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	where, order, args := queryTerms(q)
	args = append(args, limit)
	rows, err := db.Query("SELECT a.id,a.title,a.folder_id,f.name,a.body FROM kb_articles a JOIN kb_folders f ON f.id=a.folder_id WHERE "+where+" ORDER BY "+order+" LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Hit{}
	for rows.Next() {
		var h Hit
		var body string
		if err = rows.Scan(&h.ID, &h.Title, &h.Folder.ID, &h.Folder.Name, &body); err != nil {
			return nil, err
		}
		h.Path = fmt.Sprintf("/kb/%d", h.ID)
		h.Snippet = snippet(body, q)
		out = append(out, h)
	}
	return out, rows.Err()
}

var markdownLink = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
var htmlTag = regexp.MustCompile(`<[^>]*>`)
var markdownMarks = regexp.MustCompile("(?m)^\\s*(?:#{1,6}\\s+|>\\s*|[-+*]\\s+|[0-9]+[.)]\\s+)")

func plain(s string) string {
	s = markdownLink.ReplaceAllString(s, "$1")
	s = htmlTag.ReplaceAllString(s, "")
	s = markdownMarks.ReplaceAllString(s, "")
	s = strings.NewReplacer("```", "", "`", "", "**", "", "__", "", "*", "", "_", "", "~~", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
func snippet(body, q string) string {
	s := plain(body)
	lower := strings.ToLower(s)
	first := -1
	matchLen := 0
	for _, word := range strings.Fields(q) {
		i := strings.Index(lower, strings.ToLower(word))
		if i >= 0 && (first < 0 || i < first) {
			first = i
			matchLen = len([]rune(word))
		}
	}
	if first < 0 {
		return clip(s, 120)
	}
	start := len([]rune(lower[:first]))
	r := []rune(s)
	return string(r[max(0, start-60):min(len(r), start+matchLen+60)])
}
func (a *Module) search(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	hits, err := Search(a.DB, r.URL.Query().Get("q"), limit)
	if err != nil {
		replyError(w, err)
		return
	}
	core.WriteJSON(w, 200, hits)
}
func (a *Module) listArticles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where, order, args := queryTerms(q.Get("q"))
	// Extra filters belong before ORDER BY parameters.
	nFilter := len(strings.Fields(q.Get("q"))) * 2
	filters := append([]any{}, args[:nFilter]...)
	scores := args[nFilter:]
	if r.URL.Path == "/api/kb/trash" {
		where = strings.Replace(where, "a.deleted_at IS NULL", "a.deleted_at IS NOT NULL", 1)
	}
	for _, f := range []struct{ key, sql string }{{"folder_id", "a.folder_id=?"}, {"tag", "EXISTS(SELECT 1 FROM json_each(a.tags) WHERE value=?)"}} {
		if value := q.Get(f.key); value != "" {
			where += " AND " + f.sql
			filters = append(filters, value)
		}
	}
	if q.Get("target_type") != "" || q.Get("target_id") != "" {
		typ := q.Get("target_type")
		id, err := strconv.ParseInt(q.Get("target_id"), 10, 64)
		if (typ != "category" && typ != "product" && typ != "item") || err != nil || id < 1 {
			core.Fail(w, 400, "关联查询须提供有效的 target_type 和 target_id")
			return
		}
		where += " AND EXISTS(SELECT 1 FROM kb_links l WHERE l.article_id=a.id AND l.target_type=? AND l.target_id=?)"
		filters = append(filters, typ, id)
	}
	var total int
	if err := a.DB.QueryRow("SELECT count(*) FROM kb_articles a WHERE "+where, filters...).Scan(&total); err != nil {
		replyError(w, err)
		return
	}
	offset, size := core.Page(r)
	params := append(filters, scores...)
	params = append(params, size, offset)
	rows, err := a.DB.Query("SELECT "+articleColumns+articleFrom+"WHERE "+where+" ORDER BY "+order+" LIMIT ? OFFSET ?", params...)
	if err != nil {
		replyError(w, err)
		return
	}
	articles := []Article{}
	for rows.Next() {
		v, e := scanArticle(rows)
		if e != nil {
			err = e
			break
		}
		articles = append(articles, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		replyError(w, err)
		return
	}
	out := []core.Object{}
	for _, v := range articles {
		v.Links, err = a.links(v.ID, core.CurrentUser(r))
		if err != nil {
			replyError(w, err)
			return
		}
		if v.Summary == "" {
			v.Summary = clip(plain(v.Body), 120)
		}
		var item core.Object
		_ = decodeMap(v, &item)
		delete(item, "body")
		out = append(out, item)
	}
	core.WriteJSON(w, 200, core.Object{"items": out, "total": total})
}
func (a *Module) listTags(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(`SELECT j.value,count(*) FROM kb_articles a,json_each(a.tags) j WHERE a.deleted_at IS NULL GROUP BY j.value ORDER BY count(*) DESC,j.value`)
	if err != nil {
		replyError(w, err)
		return
	}
	defer rows.Close()
	out := []core.Object{}
	for rows.Next() {
		var tag string
		var n int
		if err = rows.Scan(&tag, &n); err != nil {
			replyError(w, err)
			return
		}
		out = append(out, core.Object{"tag": tag, "count": n})
	}
	if err = rows.Err(); err != nil {
		replyError(w, err)
		return
	}
	core.WriteJSON(w, 200, out)
}
