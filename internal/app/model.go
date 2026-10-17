package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type Object = map[string]any
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
	Disabled  bool   `json:"disabled"`
	Hash      []byte `json:"-"`
	Version   int64  `json:"-"`
}
type Product struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	OwnerID   int64  `json:"owner_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Info      Object `json:"info"`
	Status    string `json:"status"`
	Step      string `json:"step"`
	Error     string `json:"error"`
	Content   Object `json:"content"`
	Main      Object `json:"main"`
	Listing   Object `json:"listing"`
}
type Photo struct {
	ID           int64     `json:"id"`
	ProductID    int64     `json:"product_id"`
	File         string    `json:"file"`
	OrigName     string    `json:"orig_name"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	Metrics      Object    `json:"metrics"`
	Code         string    `json:"code"`
	Crop         []float64 `json:"crop"`
	Rotate       int       `json:"rotate"`
	WhiteBG      bool      `json:"white_bg"`
	AINote       string    `json:"ai_note"`
	RetouchLog   Object    `json:"retouch_log"`
	Manual       bool      `json:"manual"`
	EnhancedFile string    `json:"enhanced_file"`
	AIEnhanced   bool      `json:"ai_enhanced"`
	AIGenerated  bool      `json:"ai_generated"`
	Source       string    `json:"source"`
	URL          string    `json:"url"`
}
type Job struct {
	ID         int64  `json:"id"`
	ProductID  int64  `json:"product_id"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Progress   int    `json:"progress"`
	Message    string `json:"message"`
	CreatedAt  string `json:"created_at"`
	FinishedAt string `json:"finished_at"`
	Steps      string `json:"-"`
}

const schema = `
PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY,username TEXT NOT NULL UNIQUE,password_hash BLOB NOT NULL,role TEXT NOT NULL CHECK(role IN ('admin','staff')),created_at TEXT NOT NULL,disabled INTEGER NOT NULL DEFAULT 0,version INTEGER NOT NULL DEFAULT 1);
CREATE TABLE IF NOT EXISTS products(id INTEGER PRIMARY KEY,name TEXT NOT NULL,owner_id INTEGER NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,updated_at TEXT NOT NULL,info TEXT NOT NULL DEFAULT '{}',status TEXT NOT NULL DEFAULT 'draft',step TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '',content TEXT NOT NULL DEFAULT '{}',main TEXT NOT NULL DEFAULT '{}',listing TEXT NOT NULL DEFAULT '{}');
CREATE TABLE IF NOT EXISTS photos(id INTEGER PRIMARY KEY,product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,file TEXT NOT NULL,orig_name TEXT NOT NULL,width INTEGER NOT NULL,height INTEGER NOT NULL,metrics TEXT NOT NULL DEFAULT '{}',code TEXT NOT NULL DEFAULT '',crop TEXT NOT NULL DEFAULT 'null',rotate INTEGER NOT NULL DEFAULT 0,white_bg INTEGER NOT NULL DEFAULT 0,ai_note TEXT NOT NULL DEFAULT '',retouch_log TEXT NOT NULL DEFAULT '{}',manual INTEGER NOT NULL DEFAULT 0);
CREATE UNIQUE INDEX IF NOT EXISTS photo_code ON photos(product_id,code) WHERE code<>'';
CREATE TABLE IF NOT EXISTS jobs(id INTEGER PRIMARY KEY,product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,kind TEXT NOT NULL,status TEXT NOT NULL,progress INTEGER NOT NULL DEFAULT 0,message TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,finished_at TEXT NOT NULL DEFAULT '',steps TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS active_job ON jobs(product_id) WHERE status IN ('running','queued');
CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY,user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,version INTEGER NOT NULL,expires INTEGER NOT NULL);
`

func js(v any) string { b, _ := json.Marshal(v); return string(b) }
func object(s string) Object {
	var v Object
	_ = json.Unmarshal([]byte(s), &v)
	if v == nil {
		v = Object{}
	}
	return v
}

type scanner interface{ Scan(...any) error }

func scanUser(s scanner) (u User, e error) {
	e = s.Scan(&u.ID, &u.Username, &u.Hash, &u.Role, &u.CreatedAt, &u.Disabled, &u.Version)
	return
}
func scanProduct(s scanner) (p Product, e error) {
	var a, b, c, d string
	e = s.Scan(&p.ID, &p.Name, &p.OwnerID, &p.CreatedAt, &p.UpdatedAt, &a, &p.Status, &p.Step, &p.Error, &b, &c, &d)
	p.Info = object(a)
	p.Content = object(b)
	p.Main = object(c)
	p.Listing = object(d)
	return
}
func scanPhoto(s scanner) (p Photo, e error) {
	var a, b, c string
	e = s.Scan(&p.ID, &p.ProductID, &p.File, &p.OrigName, &p.Width, &p.Height, &a, &p.Code, &b, &p.Rotate, &p.WhiteBG, &p.AINote, &c, &p.Manual, &p.EnhancedFile, &p.AIEnhanced, &p.AIGenerated, &p.Source)
	p.Metrics = object(a)
	_ = json.Unmarshal([]byte(b), &p.Crop)
	p.RetouchLog = object(c)
	p.URL = fmt.Sprintf("/files/products/%d/%s", p.ProductID, p.File)
	return
}
func scanJob(s scanner) (j Job, e error) {
	e = s.Scan(&j.ID, &j.ProductID, &j.Kind, &j.Status, &j.Progress, &j.Message, &j.CreatedAt, &j.FinishedAt, &j.Steps)
	return
}
func (a *App) product(id int64) (Product, error) {
	return scanProduct(a.db.QueryRow("SELECT * FROM products WHERE id=?", id))
}

const photoColumns = "id,product_id,file,orig_name,width,height,metrics,code,crop,rotate,white_bg,ai_note,retouch_log,manual,enhanced_file,ai_enhanced,ai_generated,source"

func (a *App) photos(id int64) ([]Photo, error) {
	rows, e := a.db.Query("SELECT "+photoColumns+" FROM photos WHERE product_id=? ORDER BY id", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Photo{}
	for rows.Next() {
		p, e := scanPhoto(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (a *App) latestJob(id int64) *Job {
	j, e := scanJob(a.db.QueryRow("SELECT * FROM jobs WHERE product_id=? ORDER BY id DESC LIMIT 1", id))
	if e != nil {
		return nil
	}
	return &j
}
func (a *App) active(id int64) bool {
	var n int
	_ = a.db.QueryRow("SELECT count(*) FROM jobs WHERE product_id=? AND status IN ('queued','running')", id).Scan(&n)
	return n > 0
}
func notFound(e error) bool { return errors.Is(e, sql.ErrNoRows) }
