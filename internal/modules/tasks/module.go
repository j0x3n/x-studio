package tasks

import (
	"database/sql"
	"log"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"
	"xstudio/internal/core"
)

type Module struct {
	*core.Core
	ScanInterval   time.Duration
	RemindInterval time.Duration
	PingInterval   time.Duration
	Now            func() time.Time
	mu             sync.Mutex
	bgWG           sync.WaitGroup
	hub            *hub
	pushMu         sync.Mutex
	pushSetupMu    sync.Mutex
	pushCfg        *pushConfig
	pushFromEnv    bool
	pushCh         chan pushJob
	pushClient     *http.Client
}

func New(c *core.Core) *Module {
	return &Module{Core: c, ScanInterval: 3 * time.Second, RemindInterval: time.Minute, PingInterval: 25 * time.Second, hub: newHub()}
}
func (*Module) Name() string                     { return "tasks" }
func (*Module) Jobs() map[string]core.JobHandler { return nil }

func (a *Module) Routes(r *core.Router) {
	r.Handle("GET /api/tasks", core.TaskView, a.listTasks)
	r.Handle("POST /api/tasks", core.TaskView, a.createTask)
	r.Handle("GET /api/tasks/assignees", core.TaskView, a.assignees)
	r.Handle("GET /api/tasks/load", core.TaskManage, a.load)
	r.Handle("GET /api/tasks/stats", core.TaskManage, a.stats)
	r.Handle("GET /api/tasks/{id}", core.TaskView, a.getTask)
	r.Handle("PATCH /api/tasks/{id}", core.TaskView, a.patchTask)
	r.Handle("POST /api/tasks/{id}/comments", core.TaskView, a.comment)

	r.Handle("GET /api/task-rules", core.AdminSettings, a.listRules)
	r.Handle("POST /api/task-rules", core.AdminSettings, a.createRule)
	r.Handle("PATCH /api/task-rules/{id}", core.AdminSettings, a.patchRule)
	r.Handle("DELETE /api/task-rules/{id}", core.AdminSettings, a.deleteRule)

	r.Handle("GET /api/notifications", core.LoggedIn, a.listNotes)
	r.Handle("GET /api/notifications/unread-count", core.LoggedIn, a.unreadCount)
	r.Handle("POST /api/notifications/read", core.LoggedIn, a.markRead)
	r.Handle("GET /api/notifications/stream", core.LoggedIn, a.stream)
	r.Handle("GET /api/notification-prefs", core.LoggedIn, a.getPrefs)
	r.Handle("PUT /api/notification-prefs", core.LoggedIn, a.putPrefs)

	r.Handle("GET /api/push/key", core.LoggedIn, a.pushKey)
	r.Handle("POST /api/push/subscriptions", core.LoggedIn, a.pushSubscribe)
	r.Handle("DELETE /api/push/subscriptions", core.LoggedIn, a.pushUnsubscribe)
	r.Handle("POST /api/push/vapid", core.AdminSettings, a.pushGenerate)

	a.initPush()
	a.bgWG.Add(1)
	go a.loop()
	a.bgWG.Add(1)
	go a.pushLoop()
	a.OnClose(func() { a.bgWG.Wait() })
}

func (a *Module) loop() {
	defer a.bgWG.Done()
	scanEvery := a.ScanInterval
	if scanEvery <= 0 {
		scanEvery = 3 * time.Second
	}
	remindEvery := a.RemindInterval
	if remindEvery <= 0 {
		remindEvery = time.Minute
	}
	scanTick := time.NewTicker(scanEvery)
	remindTick := time.NewTicker(remindEvery)
	defer scanTick.Stop()
	defer remindTick.Stop()
	for {
		select {
		case <-a.Context().Done():
			return
		case <-scanTick.C:
			a.scan()
		case <-remindTick.C:
			a.remind()
		}
	}
}

func (a *Module) scan() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.scanAudit(); err != nil {
		log.Printf("扫描操作日志生成任务失败：%v", err)
	}
	if err := a.scanJobs(); err != nil {
		log.Printf("扫描后台任务生成通知失败：%v", err)
	}
}

func (a *Module) remind() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.remindDue(); err != nil {
		log.Printf("检查任务到期失败：%v", err)
	}
	if err := a.remindQuota(); err != nil {
		log.Printf("检查额度提醒失败：%v", err)
	}
}

func (a *Module) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}
func (a *Module) ts() string { return a.now().UTC().Format(time.RFC3339Nano) }

func shanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }
func bad(code int, msg string) error { return &statusError{code, msg} }

func writeErr(w http.ResponseWriter, err error, logMsg, public string) {
	if err == nil {
		return
	}
	if he, ok := err.(*statusError); ok {
		core.Fail(w, he.code, he.msg)
		return
	}
	log.Print(logMsg, err)
	core.Fail(w, 500, public)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
func runeLen(s string) int { return utf8.RuneCountInString(s) }

func parseTS(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func normDue(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	t, ok := parseTS(s)
	if !ok {
		return "", bad(400, "截止时间格式不正确")
	}
	return t.UTC().Format(time.RFC3339Nano), nil
}

func sqlTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05") }

const dueExpr = "julianday(substr(replace(replace(due_at,'T',' '),'Z',''),1,19))"
const doneExpr = "julianday(substr(replace(replace(done_at,'T',' '),'Z',''),1,19))"

type dbx interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

func setCursor(q dbx, name, value, ts string) error {
	_, err := q.Exec(`INSERT INTO task_cursors(name,value,updated_at) VALUES(?,?,?) ON CONFLICT(name) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, name, value, ts)
	return err
}
