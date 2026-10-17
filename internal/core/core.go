package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Migration struct {
	Version int
	Up      func(*sql.Tx) error
}
type JobHandler func(context.Context, Job) error
type Module interface {
	Name() string
	Migrations() []Migration
	Routes(*Router)
	Jobs() map[string]JobHandler
}

type Core struct {
	PrepareListing func(Product) (Product, error)
	AssetCoverage  func([]Photo) Object
	Cfg            Config
	DB             *sql.DB
	Slots          []byte
	Mu             sync.Mutex
	ThumbMu        sync.Mutex
	Client         *http.Client
	web            fs.FS
	mux            *http.ServeMux
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	wake           chan struct{}
	baseMu         sync.RWMutex
	baseURL        string
	limitMu        sync.Mutex
	failures       map[string][]time.Time
	dummyHash      []byte
	aiMu           sync.Mutex
	inflight       map[string]int
	jobHandlers    map[string]JobHandler
	closers        []func()
	moves          [][2]string
}

func New(c Config, web fs.FS, slots []byte) (*Core, error) {
	if len(c.SessionSecret) < 32 {
		return nil, errors.New("SESSION_SECRET 至少需要 32 个字符")
	}
	if c.Workers == 0 {
		c.Workers = 2
	}
	if c.Workers < 1 || c.Workers > 32 {
		return nil, errors.New("WORKERS 必须在 1–32 之间")
	}
	if c.ImageModel == "" {
		c.ImageModel = "gpt-image-2.5-burst"
	}
	if c.ImageTimeout <= 0 {
		c.ImageTimeout = 240 * time.Second
	}
	if c.UpscaleMinSide <= 0 {
		c.UpscaleMinSide = 900
	}
	if e := os.MkdirAll(c.DataDir, 0700); e != nil {
		return nil, fmt.Errorf("无法创建数据目录：%w", e)
	}
	db, e := sql.Open("sqlite", filepath.Join(c.DataDir, "app.db"))
	if e != nil {
		return nil, fmt.Errorf("打开数据库失败：%w", e)
	}
	db.SetMaxOpenConns(1)
	if _, e = db.Exec("PRAGMA busy_timeout=5000"); e != nil {
		db.Close()
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &Core{Cfg: c, DB: db, Slots: slots, web: web, mux: http.NewServeMux(), ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), failures: map[string][]time.Time{}, inflight: map[string]int{}, Client: &http.Client{Timeout: 120 * time.Second}, jobHandlers: map[string]JobHandler{}}
	a.dummyHash, _ = bcrypt.GenerateFromPassword([]byte("invalid-password-placeholder"), bcrypt.DefaultCost)
	return a, nil
}

// Start is called once after the composition root has supplied all modules.
func (a *Core) Start(modules ...Module) error {
	if e := a.Migrate(modules); e != nil {
		return fmt.Errorf("数据库迁移失败：%w", e)
	}
	var n int
	if e := a.DB.QueryRow("SELECT count(*) FROM users").Scan(&n); e != nil {
		return e
	}
	if n == 0 {
		if a.Cfg.AdminUser == "" || len(a.Cfg.AdminPassword) < 8 || len(a.Cfg.AdminPassword) > 72 {
			return errors.New("首次启动需设置 ADMIN_USER 和 8–72 字节的 ADMIN_PASSWORD")
		}
		h, e := bcrypt.GenerateFromPassword([]byte(a.Cfg.AdminPassword), bcrypt.DefaultCost)
		if e != nil {
			return e
		}
		if _, e = a.DB.Exec("INSERT INTO users(username,password_hash,display_name,role_id,created_at) SELECT ?,?,?,id,? FROM roles WHERE key='admin'", a.Cfg.AdminUser, h, a.Cfg.AdminUser, Now()); e != nil {
			return e
		}
	}
	if _, e := a.DB.Exec("UPDATE listings SET pipeline_status='failed',pipeline_error='服务重启中断' WHERE id IN (SELECT target_id FROM jobs WHERE target_type='listing' AND status='running'); UPDATE jobs SET status='failed',message='服务重启中断',finished_at='" + Now() + "' WHERE status='running'"); e != nil {
		return e
	}
	r := &Router{Core: a}
	for _, m := range modules {
		m.Routes(r)
		for k, h := range m.Jobs() {
			if _, ok := a.jobHandlers[k]; ok {
				return fmt.Errorf("重复任务类型：%s", k)
			}
			a.jobHandlers[k] = h
		}
	}
	if r.err != nil {
		return r.err
	}
	a.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { Fail(w, 404, "接口不存在或请求方法不正确") })
	a.mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) { Fail(w, 404, "文件不存在") })
	a.mux.HandleFunc("/render/", func(w http.ResponseWriter, r *http.Request) { Fail(w, 404, "渲染文件不存在") })
	a.mux.HandleFunc("/", a.static)
	for i := 0; i < a.Cfg.Workers; i++ {
		a.wg.Add(1)
		go a.worker()
	}
	return nil
}

func (a *Core) OnClose(f func()) { a.closers = append(a.closers, f) }
func (a *Core) Close() error {
	a.cancel()
	a.wg.Wait()
	for _, f := range a.closers {
		f()
	}
	return a.DB.Close()
}
func (a *Core) Context() context.Context { return a.ctx }
func (a *Core) SetBaseURL(s string)      { a.baseMu.Lock(); a.baseURL = s; a.baseMu.Unlock(); a.Wake() }
func (a *Core) BaseURL() string          { a.baseMu.RLock(); defer a.baseMu.RUnlock(); return a.baseURL }
func (a *Core) Dir(id int64) string      { return filepath.Join(a.Cfg.DataDir, "products", fmt.Sprint(id)) }
func (a *Core) ListingDir(id int64) string {
	return filepath.Join(a.Cfg.DataDir, "listings", fmt.Sprint(id))
}
func (a *Core) Handler() http.Handler { return http.HandlerFunc(a.serve) }
