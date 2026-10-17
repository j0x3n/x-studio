package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, s string) { writeJSON(w, status, Object{"error": s}) }
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	if e := d.Decode(v); e != nil {
		fail(w, 400, "JSON 格式或字段类型不正确，或请求过大")
		return false
	}
	if e := d.Decode(new(any)); e != io.EOF {
		fail(w, 400, "只能提交一个 JSON 值")
		return false
	}
	return true
}
func idParam(r *http.Request, key string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(key), 10, 64)
	if n < 1 {
		return 0
	}
	return n
}
func (a *App) authorizedProduct(w http.ResponseWriter, r *http.Request, id int64, write bool) (Product, bool) {
	p, e := a.product(id)
	if e != nil {
		if notFound(e) {
			fail(w, 404, "款式不存在")
		} else {
			fail(w, 500, "读取款式失败")
		}
		return p, false
	}
	u := currentUser(r)
	if write && u.Role != "admin" && u.ID != p.OwnerID {
		fail(w, 403, "只能修改自己创建的款式")
		return p, false
	}
	if write && a.active(id) {
		fail(w, 409, "此款正在排队或处理中，请等待任务完成")
		return p, false
	}
	return p, true
}
func (a *App) createProduct(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Info Object `json:"info"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 120 {
		fail(w, 400, "款名不能为空且不能超过 120 字")
		return
	}
	if in.Info == nil {
		in.Info = Object{}
	}
	if e := validateInfo(in.Info); e != nil {
		fail(w, 400, e.Error())
		return
	}
	t := now()
	res, e := a.db.Exec("INSERT INTO products(name,owner_id,created_at,updated_at,info) VALUES(?,?,?,?,?)", in.Name, currentUser(r).ID, t, t, js(in.Info))
	if e != nil {
		fail(w, 500, "创建款式失败")
		return
	}
	id, _ := res.LastInsertId()
	p, _ := a.product(id)
	writeJSON(w, 201, Object{"product": p})
}
func (a *App) listProducts(w http.ResponseWriter, r *http.Request) {
	query := `SELECT p.*,u.username,(SELECT count(*) FROM photos WHERE product_id=p.id),(SELECT file FROM photos WHERE product_id=p.id ORDER BY id LIMIT 1) FROM products p JOIN users u ON p.owner_id=u.id WHERE 1=1`
	args := []any{}
	q := r.URL.Query()
	if s := q.Get("q"); s != "" {
		query += " AND instr(p.name,?)>0"
		args = append(args, s)
	}
	if s := q.Get("status"); s != "" {
		query += " AND p.status=?"
		args = append(args, s)
	}
	if q.Get("mine") == "1" {
		query += " AND p.owner_id=?"
		args = append(args, currentUser(r).ID)
	}
	query += " ORDER BY p.updated_at DESC,p.id DESC"
	rows, e := a.db.Query(query, args...)
	if e != nil {
		fail(w, 500, "读取款式列表失败")
		return
	}
	defer rows.Close()
	items := []Object{}
	for rows.Next() {
		var p Product
		var info, content, main, listing, owner string
		var count int
		var cover *string
		if e = rows.Scan(&p.ID, &p.Name, &p.OwnerID, &p.CreatedAt, &p.UpdatedAt, &info, &p.Status, &p.Step, &p.Error, &content, &main, &listing, &owner, &count, &cover); e != nil {
			fail(w, 500, "读取款式列表失败")
			return
		}
		url := ""
		if cover != nil {
			url = fmt.Sprintf("/files/products/%d/%s?w=240", p.ID, *cover)
		}
		items = append(items, Object{"id": p.ID, "name": p.Name, "status": p.Status, "step": p.Step, "owner": owner, "owner_id": p.OwnerID, "updated_at": p.UpdatedAt, "cover_url": url, "photo_count": count})
	}
	if rows.Err() != nil {
		fail(w, 500, "读取款式列表失败")
		return
	}
	writeJSON(w, 200, Object{"items": items, "total": len(items)})
}
func (a *App) getProduct(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	p, ok := a.authorizedProduct(w, r, id, false)
	if !ok {
		return
	}
	photos, e := a.photos(id)
	if e != nil {
		fail(w, 500, "读取照片失败")
		return
	}
	writeJSON(w, 200, Object{"product": p, "photos": photos, "job": a.latestJob(id), "coverage": a.coverage(photos), "outputs": a.outputs(id)})
}
func (a *App) patchProduct(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := idParam(r, "id")
	p, ok := a.authorizedProduct(w, r, id, true)
	if !ok {
		return
	}
	var in map[string]json.RawMessage
	if !decode(w, r, &in) {
		return
	}
	if len(in) == 0 {
		fail(w, 400, "没有要更新的字段")
		return
	}
	for k, v := range in {
		switch k {
		case "name":
			if json.Unmarshal(v, &p.Name) != nil || strings.TrimSpace(p.Name) == "" || utf8.RuneCountInString(p.Name) > 120 {
				fail(w, 400, "款名不正确")
				return
			}
		case "info", "content", "main", "listing":
			var o Object
			if json.Unmarshal(v, &o) != nil || o == nil {
				fail(w, 400, k+" 必须是 JSON 对象")
				return
			}
			switch k {
			case "info":
				p.Info = o
			case "content":
				p.Content = o
			case "main":
				p.Main = o
			case "listing":
				p.Listing = o
			}
		default:
			fail(w, 400, "不支持修改字段："+k)
			return
		}
	}
	p.Main = cleanMain(p.Main, filepath.Join(a.dir(id), "照片"))
	if e := validateInfo(p.Info); e != nil {
		fail(w, 400, e.Error())
		return
	}
	if e := a.annotateAI(id, p.Content, p.Main); e != nil {
		fail(w, 500, "读取照片标记失败")
		return
	}
	if e := validateCopy(p.Content, p.Main, p.Listing); e != nil {
		fail(w, 400, e.Error())
		return
	}
	p.Content = cleanObject(p.Content, filepath.Join(a.dir(id), "照片"))
	if _, changedContent := in["content"]; changedContent || in["main"] != nil || in["listing"] != nil {
		if words, e := a.prompt("违禁词.txt"); e == nil {
			p.Listing["compliance"] = appendCompliance(p.Content, p.Main, p.Listing, words)
		}
	}
	_, e := a.db.Exec("UPDATE products SET name=?,info=?,content=?,main=?,listing=?,status='draft',error='',updated_at=? WHERE id=?", p.Name, js(p.Info), js(p.Content), js(p.Main), js(p.Listing), now(), id)
	if e != nil {
		fail(w, 500, "保存款式失败")
		return
	}
	p, _ = a.product(id)
	writeJSON(w, 200, Object{"product": p})
}
func (a *App) deleteProduct(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := idParam(r, "id")
	if _, ok := a.authorizedProduct(w, r, id, true); !ok {
		return
	}
	if _, e := a.db.Exec("DELETE FROM products WHERE id=?", id); e != nil {
		fail(w, 500, "删除款式失败")
		return
	}
	if e := os.RemoveAll(a.dir(id)); e != nil {
		fail(w, 500, "款式记录已删除，但文件清理失败")
		return
	}
	writeJSON(w, 200, Object{})
}

var codePattern = regexp.MustCompile(`^[A-Z][1-9][0-9]?(?:_[\p{L}\p{N}_-]+)?$`)

func validatePhoto(p Photo) error {
	if p.AIGenerated && p.Code != "" && !allowedFillCode(p.Code) {
		return errors.New("AI 生成照片只能使用 A1、E1、E2、E3、D4 编号")
	}
	if p.Code != "" && (!codePattern.MatchString(p.Code) || utf8.RuneCountInString(p.Code) > 60) {
		return errors.New("拍摄编号格式不正确")
	}
	if strings.HasSuffix(p.Code, "_白底") || strings.HasSuffix(p.Code, "_透明") {
		return errors.New("白底、透明是输出文件保留后缀，请换一个拍摄编号")
	}
	if p.Rotate != 0 && p.Rotate != 90 && p.Rotate != 180 && p.Rotate != 270 {
		return errors.New("旋转角度只能是 0、90、180、270")
	}
	if p.Crop != nil {
		if len(p.Crop) != 4 {
			return errors.New("裁剪必须是 [x,y,w,h]")
		}
		c := p.Crop
		if c[0] < 0 || c[1] < 0 || c[2] <= 0 || c[3] <= 0 || c[0]+c[2] > 1.00000001 || c[1]+c[3] > 1.00000001 {
			return errors.New("裁剪范围必须在图片比例 0–1 之间")
		}
	}
	return nil
}
func (a *App) photoForWrite(w http.ResponseWriter, r *http.Request) (Photo, bool) {
	p, e := scanPhoto(a.db.QueryRow("SELECT "+photoColumns+" FROM photos WHERE id=?", idParam(r, "pid")))
	if e != nil {
		fail(w, 404, "照片不存在")
		return p, false
	}
	_, ok := a.authorizedProduct(w, r, p.ProductID, true)
	return p, ok
}
func (a *App) patchPhoto(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.photoForWrite(w, r)
	if !ok {
		return
	}
	var in map[string]json.RawMessage
	if !decode(w, r, &in) {
		return
	}
	old := p.Code
	oldCrop, oldRotate := js(p.Crop), p.Rotate
	for k, v := range in {
		var e error
		switch k {
		case "code":
			if string(v) == "null" {
				p.Code = ""
			} else {
				e = json.Unmarshal(v, &p.Code)
			}
			p.Manual = true
		case "crop":
			e = json.Unmarshal(v, &p.Crop)
		case "rotate":
			e = json.Unmarshal(v, &p.Rotate)
		case "white_bg":
			e = json.Unmarshal(v, &p.WhiteBG)
		default:
			e = errors.New("不支持的字段")
		}
		if e != nil {
			fail(w, 400, "照片字段类型不正确："+k)
			return
		}
	}
	if e := validatePhoto(p); e != nil {
		fail(w, 400, e.Error())
		return
	}
	if oldCrop != js(p.Crop) || oldRotate != p.Rotate {
		p.EnhancedFile = ""
		p.AIEnhanced = false
		delete(p.RetouchLog, "upscale_error")
	}
	tx, e := a.db.Begin()
	if e != nil {
		fail(w, 500, "保存照片失败")
		return
	}
	defer tx.Rollback()
	if !p.AIGenerated && p.Code != "" {
		if _, e = tx.Exec("UPDATE photos SET code='' WHERE product_id=? AND code=? AND ai_generated=1", p.ProductID, p.Code); e != nil {
			fail(w, 500, "更新 AI 照片编号失败")
			return
		}
	}
	if _, e := tx.Exec("UPDATE photos SET code=?,crop=?,rotate=?,white_bg=?,manual=?,enhanced_file=?,ai_enhanced=?,retouch_log=? WHERE id=?", p.Code, js(p.Crop), p.Rotate, p.WhiteBG, p.Manual, p.EnhancedFile, p.AIEnhanced, js(p.RetouchLog), p.ID); e != nil {
		fail(w, 409, "该编号已被另一张照片使用")
		return
	}
	if e = tx.Commit(); e != nil {
		fail(w, 500, "保存照片失败")
		return
	}
	a.removePhotoOutputs(p.ProductID, old)
	a.removePhotoOutputs(p.ProductID, p.Code)
	_, _ = a.db.Exec("UPDATE products SET status='draft',updated_at=? WHERE id=?", now(), p.ProductID)
	writeJSON(w, 200, Object{"photo": p})
}
func (a *App) removePhotoOutputs(id int64, code string) {
	if code == "" || !codePattern.MatchString(code) {
		return
	}
	for _, ext := range []string{".jpg", "_白底.jpg", "_透明.png"} {
		_ = os.Remove(filepath.Join(a.dir(id), "照片", code+ext))
	}
}
func (a *App) deletePhoto(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.photoForWrite(w, r)
	if !ok {
		return
	}
	if _, e := a.db.Exec("DELETE FROM photos WHERE id=?", p.ID); e != nil {
		fail(w, 500, "删除照片失败")
		return
	}
	_ = os.Remove(filepath.Join(a.dir(p.ProductID), filepath.FromSlash(p.File)))
	if p.EnhancedFile != "" {
		_ = os.Remove(filepath.Join(a.dir(p.ProductID), filepath.FromSlash(p.EnhancedFile)))
	}
	a.removePhotoOutputs(p.ProductID, p.Code)
	_, _ = a.db.Exec("UPDATE products SET status='draft',updated_at=? WHERE id=?", now(), p.ProductID)
	writeJSON(w, 200, Object{})
}
func (a *App) getJob(w http.ResponseWriter, r *http.Request) {
	j, e := scanJob(a.db.QueryRow("SELECT * FROM jobs WHERE id=?", idParam(r, "jid")))
	if e != nil {
		fail(w, 404, "任务不存在")
		return
	}
	writeJSON(w, 200, Object{"job": j})
}
func admin(w http.ResponseWriter, r *http.Request) bool {
	if currentUser(r).Role != "admin" {
		fail(w, 403, "只有管理员能管理账号")
		return false
	}
	return true
}
func (a *App) listUsers(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	rows, e := a.db.Query("SELECT * FROM users ORDER BY id")
	if e != nil {
		fail(w, 500, "读取用户失败")
		return
	}
	defer rows.Close()
	items := []User{}
	for rows.Next() {
		u, e := scanUser(rows)
		if e != nil {
			fail(w, 500, "读取用户失败")
			return
		}
		items = append(items, u)
	}
	writeJSON(w, 200, Object{"users": items})
}
func (a *App) createUser(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Role == "" {
		in.Role = "staff"
	}
	if strings.TrimSpace(in.Username) == "" || len(in.Username) > 120 || len(in.Password) < 8 || len(in.Password) > 72 || (in.Role != "staff" && in.Role != "admin") {
		fail(w, 400, "用户名、角色或密码不符合要求（密码 8–72 字节）")
		return
	}
	h, e := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if e != nil {
		fail(w, 400, "密码不符合要求")
		return
	}
	res, e := a.db.Exec("INSERT INTO users(username,password_hash,role,created_at) VALUES(?,?,?,?)", in.Username, h, in.Role, now())
	if e != nil {
		fail(w, 409, "用户名已存在")
		return
	}
	id, _ := res.LastInsertId()
	u, _ := scanUser(a.db.QueryRow("SELECT * FROM users WHERE id=?", id))
	writeJSON(w, 201, Object{"user": u})
}
func (a *App) patchUser(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	id := idParam(r, "uid")
	u, e := scanUser(a.db.QueryRow("SELECT * FROM users WHERE id=?", id))
	if e != nil {
		fail(w, 404, "用户不存在")
		return
	}
	var in map[string]json.RawMessage
	if !decode(w, r, &in) {
		return
	}
	for k, v := range in {
		switch k {
		case "password":
			var s string
			if json.Unmarshal(v, &s) != nil || len(s) < 8 || len(s) > 72 {
				fail(w, 400, "密码必须是 8–72 字节")
				return
			}
			u.Hash, e = bcrypt.GenerateFromPassword([]byte(s), bcrypt.DefaultCost)
			if e != nil {
				fail(w, 400, "密码不正确")
				return
			}
		case "role":
			if json.Unmarshal(v, &u.Role) != nil || (u.Role != "admin" && u.Role != "staff") {
				fail(w, 400, "角色不正确")
				return
			}
		case "disabled":
			if json.Unmarshal(v, &u.Disabled) != nil {
				fail(w, 400, "disabled 必须是布尔值")
				return
			}
		default:
			fail(w, 400, "不支持的用户字段")
			return
		}
	}
	if u.Role != "admin" || u.Disabled {
		var n int
		_ = a.db.QueryRow("SELECT count(*) FROM users WHERE role='admin' AND disabled=0 AND id<>?", id).Scan(&n)
		if n == 0 {
			fail(w, 409, "必须保留至少一位启用的管理员")
			return
		}
	}
	if _, e = a.db.Exec("UPDATE users SET password_hash=?,role=?,disabled=?,version=version+1 WHERE id=?", u.Hash, u.Role, u.Disabled, id); e != nil {
		fail(w, 500, "保存用户失败")
		return
	}
	_, _ = a.db.Exec("DELETE FROM sessions WHERE user_id=?", id)
	writeJSON(w, 200, Object{"user": u})
}
