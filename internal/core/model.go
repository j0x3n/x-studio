package core

import (
	"encoding/json"
	"fmt"
)

type Role struct {
	ID        int64    `json:"id"`
	Key       string   `json:"key"`
	Name      string   `json:"name"`
	Builtin   bool     `json:"builtin"`
	Scope     string   `json:"scope"`
	Perms     []string `json:"perms"`
	CreatedAt string   `json:"created_at"`
}
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	RoleID      int64  `json:"role_id"`
	CreatedAt   string `json:"created_at"`
	Disabled    bool   `json:"disabled"`
	Hash        []byte `json:"-"`
	Version     int64  `json:"-"`
	Role        Role   `json:"-"`
}

// Product is the pipeline's aggregate: shared product data plus one listing.
type Product struct {
	CategoryID int64     `json:"category_id"`
	Category   *Category `json:"category"`
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	OwnerID    int64     `json:"created_by"`
	CreatedAt  string    `json:"created_at"`
	UpdatedAt  string    `json:"updated_at"`
	Info       Object    `json:"info"`
	Status     string    `json:"status"`
	Step       string    `json:"pipeline_step"`
	Error      string    `json:"pipeline_error"`
	Content    Object    `json:"content"`
	Main       Object    `json:"main"`
	Listing    Object    `json:"meta"`
	ListingID  int64     `json:"listing_id"`
	ShopID     int64     `json:"shop_id"`
	Platform   string    `json:"platform"`
	Language   string    `json:"language"`
}
type Photo struct {
	ID             int64     `json:"id"`
	ProductID      int64     `json:"product_id"`
	File           string    `json:"file"`
	OrigName       string    `json:"orig_name"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	Metrics        Object    `json:"metrics"`
	Code           string    `json:"code"`
	Crop           []float64 `json:"crop"`
	Rotate         int       `json:"rotate"`
	WhiteBG        bool      `json:"white_bg"`
	AINote         string    `json:"ai_note"`
	RetouchLog     Object    `json:"retouch_log"`
	Manual         bool      `json:"manual"`
	EnhancedFile   string    `json:"enhanced_file"`
	AIEnhanced     bool      `json:"-"`
	AIGenerated    bool      `json:"-"`
	Source         string    `json:"source"`
	URL            string    `json:"url"`
	ParentID       *int64    `json:"parent_id"`
	Kind           string    `json:"kind"`
	Tags           []string  `json:"tags"`
	Op             Object    `json:"op"`
	CreatedBy      int64     `json:"created_by"`
	CreatedAt      string    `json:"created_at"`
	SourceOverride string    `json:"source_override"`
}

func (p Photo) EffectiveSource() string {
	if p.SourceOverride != "" {
		return p.SourceOverride
	}
	return p.Source
}

type Job struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
	UserID     int64  `json:"user_id"`
	Params     Object `json:"params"`
	Status     string `json:"status"`
	Progress   int    `json:"progress"`
	Message    string `json:"message"`
	Steps      string `json:"steps"`
	Result     Object `json:"result"`
	CreatedAt  string `json:"created_at"`
	FinishedAt string `json:"finished_at"`
}
type Scanner interface{ Scan(...any) error }

const UserColumns = "u.id,u.username,u.password_hash,u.display_name,u.role_id,u.created_at,u.disabled,u.version,r.id,r.key,r.name,r.builtin,r.scope,r.perms,r.created_at"
const UserQuery = "SELECT " + UserColumns + " FROM users u JOIN roles r ON r.id=u.role_id"

func ScanUser(s Scanner) (u User, e error) {
	var perms string
	e = s.Scan(&u.ID, &u.Username, &u.Hash, &u.DisplayName, &u.RoleID, &u.CreatedAt, &u.Disabled, &u.Version, &u.Role.ID, &u.Role.Key, &u.Role.Name, &u.Role.Builtin, &u.Role.Scope, &perms, &u.Role.CreatedAt)
	_ = json.Unmarshal([]byte(perms), &u.Role.Perms)
	return
}
func (a *Core) User(id int64) (User, error) {
	return ScanUser(a.DB.QueryRow(UserQuery+" WHERE u.id=?", id))
}

const PhotoColumns = "id,product_id,file,orig_name,width,height,metrics,code,crop,rotate,white_bg,ai_note,retouch_log,manual,enhanced_file,ai_enhanced,ai_generated,source,parent_id,kind,tags,op,created_by,created_at,source_override"

func ScanPhoto(s Scanner) (p Photo, e error) {
	var metrics, crop, log, tags, op string
	e = s.Scan(&p.ID, &p.ProductID, &p.File, &p.OrigName, &p.Width, &p.Height, &metrics, &p.Code, &crop, &p.Rotate, &p.WhiteBG, &p.AINote, &log, &p.Manual, &p.EnhancedFile, &p.AIEnhanced, &p.AIGenerated, &p.Source, &p.ParentID, &p.Kind, &tags, &op, &p.CreatedBy, &p.CreatedAt, &p.SourceOverride)
	p.Metrics = JSONObj(metrics)
	p.RetouchLog = JSONObj(log)
	p.Op = JSONObj(op)
	_ = json.Unmarshal([]byte(crop), &p.Crop)
	p.Tags = []string{}
	_ = json.Unmarshal([]byte(tags), &p.Tags)
	p.URL = fmt.Sprintf("/files/products/%d/%s", p.ProductID, p.File)
	return
}
func (a *Core) Photos(id int64) ([]Photo, error) {
	return a.readPhotos(id, false)
}
func (a *Core) AllPhotos(id int64) ([]Photo, error) { return a.readPhotos(id, true) }
func (a *Core) readPhotos(id int64, all bool) ([]Photo, error) {
	query := "SELECT " + PhotoColumns + " FROM assets WHERE product_id=?"
	if !all {
		query += " AND kind='photo' AND NOT(source='ai_enhanced' AND parent_id IS NOT NULL AND code='')"
	}
	rows, e := a.DB.Query(query+" ORDER BY id", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Photo{}
	for rows.Next() {
		p, e := ScanPhoto(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (a *Core) Product(id int64) (p Product, e error) {
	var info string
	e = a.DB.QueryRow("SELECT id,name,created_by,created_at,updated_at,info,status FROM products WHERE id=?", id).Scan(&p.ID, &p.Name, &p.OwnerID, &p.CreatedAt, &p.UpdatedAt, &info, &p.Status)
	if e != nil {
		return
	}
	p.Category, e = a.ProductCategory(id)
	if e != nil {
		return
	}
	p.CategoryID = p.Category.ID
	p.Info = JSONObj(info)
	p.Content = Object{}
	p.Main = Object{}
	p.Listing = Object{}
	return
}
func (a *Core) PipelineProduct(id int64) (p Product, e error) {
	var pid int64
	var content, main, meta string
	var status, step, msg, platform, language string
	var shop int64
	e = a.DB.QueryRow("SELECT l.product_id,l.content,l.main,l.meta,l.pipeline_status,l.pipeline_step,l.pipeline_error,l.shop_id,s.platform,s.language FROM listings l JOIN shops s ON s.id=l.shop_id WHERE l.id=?", id).Scan(&pid, &content, &main, &meta, &status, &step, &msg, &shop, &platform, &language)
	if e != nil {
		return
	}
	p, e = a.Product(pid)
	p.ListingID = id
	p.ShopID = shop
	p.Content = JSONObj(content)
	p.Main = JSONObj(main)
	p.Listing = JSONObj(meta)
	p.Status = status
	p.Step = step
	p.Error = msg
	p.Platform = platform
	p.Language = language
	return
}

const JobColumns = "id,kind,target_type,target_id,user_id,params,status,progress,message,steps,result,created_at,finished_at"

func ScanJob(s Scanner) (j Job, e error) {
	var params, result string
	e = s.Scan(&j.ID, &j.Kind, &j.TargetType, &j.TargetID, &j.UserID, &params, &j.Status, &j.Progress, &j.Message, &j.Steps, &result, &j.CreatedAt, &j.FinishedAt)
	j.Params = JSONObj(params)
	j.Result = JSONObj(result)
	return
}
func (a *Core) Active(kind string, id int64) bool {
	var n int
	_ = a.DB.QueryRow("SELECT count(*) FROM jobs WHERE target_type=? AND target_id=? AND status IN ('queued','running')", kind, id).Scan(&n)
	return n > 0
}
func (a *Core) ActiveProduct(id int64) bool {
	var n int
	_ = a.DB.QueryRow("SELECT count(*) FROM jobs j JOIN listings l ON j.target_type='listing' AND j.target_id=l.id WHERE l.product_id=? AND j.status IN ('queued','running')", id).Scan(&n)
	return n > 0
}
func (a *Core) LatestJob(id int64) *Job {
	j, e := ScanJob(a.DB.QueryRow("SELECT "+JobColumns+" FROM jobs WHERE target_type='listing' AND target_id=? ORDER BY id DESC LIMIT 1", id))
	if e != nil {
		return nil
	}
	return &j
}
