package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type App struct {
	cfg       Config
	db        *sql.DB
	web       fs.FS
	slots     []byte
	mux       *http.ServeMux
	mu        sync.Mutex // Serializes short mutations and job reservation.
	thumbMu   sync.Mutex // Only cache misses serialize image decoding/generation.
	limitMu   sync.Mutex
	failures  map[string][]time.Time
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	wake      chan struct{}
	baseMu    sync.RWMutex
	baseURL   string
	browser   *Browser
	client    *http.Client
	dummyHash []byte
}

func New(c Config, web fs.FS, slots []byte) (*App, error) {
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
		return nil, e
	}
	db.SetMaxOpenConns(1)
	ok := false
	defer func() {
		if !ok {
			_ = db.Close()
		}
	}()
	if _, e = db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;" + schema); e != nil {
		return nil, e
	}
	if e = migratePhotos(db); e != nil {
		return nil, e
	}
	var n int
	if e = db.QueryRow("SELECT count(*) FROM users").Scan(&n); e != nil {
		return nil, e
	}
	if n == 0 {
		if c.AdminUser == "" || len(c.AdminPassword) < 8 || len(c.AdminPassword) > 72 {
			return nil, errors.New("首次启动需设置 ADMIN_USER 和 8–72 字节的 ADMIN_PASSWORD")
		}
		h, e := bcrypt.GenerateFromPassword([]byte(c.AdminPassword), bcrypt.DefaultCost)
		if e != nil {
			return nil, e
		}
		if _, e = db.Exec("INSERT INTO users(username,password_hash,role,created_at) VALUES(?,?,'admin',?)", c.AdminUser, h, now()); e != nil {
			return nil, e
		}
		log.Print("首次启动：管理员已创建")
	}
	// Queued jobs survive restart; their exact step list is persisted.
	if _, e = db.Exec("UPDATE products SET status='failed',error='服务重启中断' WHERE id IN (SELECT product_id FROM jobs WHERE status='running'); UPDATE jobs SET status='failed',message='服务重启中断',finished_at='" + now() + "' WHERE status='running'"); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{cfg: c, db: db, web: web, slots: slots, mux: http.NewServeMux(), ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), failures: map[string][]time.Time{}, client: &http.Client{Timeout: 120 * time.Second}}
	a.dummyHash, _ = bcrypt.GenerateFromPassword([]byte("invalid-password-placeholder"), bcrypt.DefaultCost)
	a.browser = NewBrowser(c.ChromePath)
	a.routes()
	for i := 0; i < c.Workers; i++ {
		a.wg.Add(1)
		go a.worker()
	}
	ok = true
	return a, nil
}
func (a *App) Close() error { a.cancel(); a.wg.Wait(); a.browser.Close(); return a.db.Close() }
func (a *App) SetBaseURL(s string) {
	a.baseMu.Lock()
	a.baseURL = s
	a.baseMu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
}
func (a *App) getBaseURL() string    { a.baseMu.RLock(); defer a.baseMu.RUnlock(); return a.baseURL }
func (a *App) dir(id int64) string   { return filepath.Join(a.cfg.DataDir, "products", fmt.Sprint(id)) }
func (a *App) Handler() http.Handler { return http.HandlerFunc(a.serve) }
func (a *App) routes() {
	m := a.mux
	m.HandleFunc("POST /api/login", a.login)
	m.HandleFunc("POST /api/logout", a.logout)
	m.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, Object{"user": currentUser(r)}) })
	m.HandleFunc("GET /api/products", a.listProducts)
	m.HandleFunc("POST /api/products", a.createProduct)
	m.HandleFunc("GET /api/products/{id}", a.getProduct)
	m.HandleFunc("PATCH /api/products/{id}", a.patchProduct)
	m.HandleFunc("DELETE /api/products/{id}", a.deleteProduct)
	m.HandleFunc("POST /api/products/{id}/photos", a.upload)
	m.HandleFunc("PATCH /api/photos/{pid}", a.patchPhoto)
	m.HandleFunc("DELETE /api/photos/{pid}", a.deletePhoto)
	m.HandleFunc("POST /api/products/{id}/run", a.run)
	m.HandleFunc("GET /api/jobs/{jid}", a.getJob)
	m.HandleFunc("GET /api/products/{id}/download", a.download)
	m.HandleFunc("GET /api/users", a.listUsers)
	m.HandleFunc("POST /api/users", a.createUser)
	m.HandleFunc("PATCH /api/users/{uid}", a.patchUser)
	m.HandleFunc("GET /api/slots", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(a.slots)
	})
	m.HandleFunc("GET /api/codes", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, Object{"codes": codes}) })
	m.HandleFunc("GET /files/products/{id}/{path...}", a.files)
	m.HandleFunc("GET /render/{path...}", a.renderRoute)
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "接口不存在或请求方法不正确") })
	m.HandleFunc("/", a.static)
}

type userKey struct{}

func currentUser(r *http.Request) User { u, _ := r.Context().Value(userKey{}).(User); return u }
func (a *App) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Cache-Control", "no-store")
	defer func() {
		if recover() != nil {
			log.Print("请求处理异常")
			fail(w, 500, "服务器处理异常")
		}
	}()
	if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("X-Requested-With") != "fetch" {
		fail(w, 403, "缺少 X-Requested-With: fetch 请求头")
		return
	}
	public := r.URL.Path == "/login.html" || r.URL.Path == "/help.html" || hasPrefix(r.URL.Path, "/css/", "/js/") || (r.URL.Path == "/api/login" && r.Method == "POST")
	if !public {
		if hasPrefix(r.URL.Path, "/render/") && a.renderAuth(w, r) {
			a.mux.ServeHTTP(w, r)
			return
		}
		u, e := a.authenticate(r)
		if e != nil {
			if hasPrefix(r.URL.Path, "/api/", "/files/", "/render/") {
				fail(w, 401, "请先登录")
			} else {
				http.Redirect(w, r, "/login.html", http.StatusSeeOther)
			}
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), userKey{}, u))
	}
	a.mux.ServeHTTP(w, r)
}
