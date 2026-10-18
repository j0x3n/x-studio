package kb

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"
	"xstudio/internal/core"
)

type Module struct {
	*core.Core
	mu              sync.Mutex
	bgWG            sync.WaitGroup
	CleanupInterval time.Duration
}

func New(c *core.Core) *Module                   { return &Module{Core: c, CleanupInterval: time.Hour} }
func (*Module) Name() string                     { return "kb" }
func (*Module) Jobs() map[string]core.JobHandler { return nil }

func (a *Module) Routes(r *core.Router) {
	view := func(pattern string, h http.HandlerFunc) { r.Handle(pattern, core.KBView, a.guard(h, false)) }
	edit := func(pattern string, h http.HandlerFunc) { r.Handle(pattern, core.KBEdit, a.guard(h, false)) }
	view("GET /api/kb/folders", a.listFolders)
	edit("POST /api/kb/folders", a.saveFolder)
	edit("PATCH /api/kb/folders/{id}", a.saveFolder)
	edit("DELETE /api/kb/folders/{id}", a.deleteFolder)
	view("GET /api/kb/articles", a.listArticles)
	edit("POST /api/kb/articles", a.createArticle)
	view("GET /api/kb/articles/{id}", a.getArticle)
	edit("PATCH /api/kb/articles/{id}", a.patchArticle)
	edit("DELETE /api/kb/articles/{id}", a.deleteArticle)
	view("GET /api/kb/articles/{id}/versions/{version}", a.getVersion)
	edit("POST /api/kb/articles/{id}/revert", a.revertArticle)
	r.Handle("GET /api/kb/trash", core.KBView, a.guard(a.listArticles, true))
	r.Handle("POST /api/kb/articles/{id}/restore", core.KBEdit, a.guard(a.restoreArticle, true))
	edit("POST /api/kb/files", a.upload)
	view("GET /api/kb/tags", a.listTags)
	view("GET /api/kb/search", a.search)
	edit("POST /api/kb/organize", a.organize)
	r.Raw("GET /files/kb/{path...}", a.guard(a.serveFile, false))
	a.bgWG.Add(1)
	go a.cleanupLoop()
	a.OnClose(func() { a.bgWG.Wait() })
}

func (a *Module) guard(h http.HandlerFunc, admin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := core.CurrentUser(r)
		if u.ID == 0 {
			core.Fail(w, 401, "请先登录")
			return
		}
		if u.Role.Scope == "shares" || !u.Has(core.KBView) || (admin && u.Role.Key != "admin") {
			core.Fail(w, 403, "没有此操作的权限")
			return
		}
		h(w, r)
	}
}

func (a *Module) cleanupLoop() {
	defer a.bgWG.Done()
	every := a.CleanupInterval
	if every <= 0 {
		every = time.Hour
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-a.Context().Done():
			return
		case <-ticker.C:
			if err := a.cleanupFiles(time.Now()); err != nil {
				log.Printf("知识库附件清理失败：%v", err)
			}
		}
	}
}

type dbx interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
	Exec(string, ...any) (sql.Result, error)
}
type statusError struct {
	code    int
	message string
}

func (e *statusError) Error() string     { return e.message }
func bad(code int, message string) error { return &statusError{code, message} }
func replyError(w http.ResponseWriter, err error) {
	var he *statusError
	if errors.As(err, &he) {
		core.Fail(w, he.code, he.message)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		core.Fail(w, 404, "对象不存在")
		return
	}
	log.Printf("知识库操作失败：%v", err)
	core.Fail(w, 500, "知识库操作失败")
}
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
