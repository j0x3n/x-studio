package assets

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"xstudio/internal/core"
)

type Preset struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	OwnerID   int64  `json:"owner_id"`
	Shared    bool   `json:"shared"`
	Params    Object `json:"params"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func scanPreset(s core.Scanner) (p Preset, err error) {
	var raw string
	err = s.Scan(&p.ID, &p.Name, &p.OwnerID, &p.Shared, &raw, &p.CreatedAt, &p.UpdatedAt)
	p.Params = core.JSONObj(raw)
	return
}
func validatePresetParams(raw Object) (Object, error) {
	if raw == nil {
		return nil, fmt.Errorf("预设参数必须是 JSON 对象")
	}
	for k := range raw {
		if k != "ops" && k != "set_current" && k != "preset_id" {
			return nil, fmt.Errorf("预设不能包含素材编号或未知参数")
		}
	}
	var v ProcessRequest
	if err := strictDecode(raw, &v); err != nil {
		return nil, err
	}
	if v.PresetID != nil {
		return nil, fmt.Errorf("预设不能引用另一个预设")
	}
	if err := validateOps(&v.Ops); err != nil {
		return nil, err
	}
	return Object{"ops": v.Ops, "set_current": v.SetCurrent}, nil
}
func (a *Module) presets(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := a.DB.Query("SELECT id,name,owner_id,shared,params,created_at,updated_at FROM asset_presets WHERE owner_id=? OR (shared=1 AND ?<>'shares') ORDER BY updated_at DESC,id DESC", u.ID, u.Role.Scope)
	if err != nil {
		fail(w, 500, "读取预设失败")
		return
	}
	items := []Preset{}
	for rows.Next() {
		p, e := scanPreset(rows)
		if e != nil {
			rows.Close()
			fail(w, 500, "读取预设失败")
			return
		}
		items = append(items, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		fail(w, 500, "读取预设失败")
		return
	}
	core.PageItems(w, r, items)
}
func (a *Module) createPreset(w http.ResponseWriter, r *http.Request) {
	var raw Object
	if !decode(w, r, &raw) {
		return
	}
	var in struct {
		Name   string `json:"name"`
		Shared bool   `json:"shared"`
		Params Object `json:"params"`
	}
	if strictDecode(raw, &in) != nil {
		fail(w, 400, "预设字段不正确")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 120 {
		fail(w, 400, "预设名称需为 1–120 字")
		return
	}
	if in.Shared && !currentUser(r).Has(core.AssetProcess) {
		fail(w, 403, "共享预设需要图片处理权限")
		return
	}
	params, err := validatePresetParams(in.Params)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	t := now()
	res, err := a.DB.Exec("INSERT INTO asset_presets(name,owner_id,shared,params,created_at,updated_at) VALUES(?,?,?,?,?,?)", in.Name, currentUser(r).ID, in.Shared, js(params), t, t)
	if err != nil {
		fail(w, 500, "创建预设失败")
		return
	}
	id, _ := res.LastInsertId()
	p := Preset{id, in.Name, currentUser(r).ID, in.Shared, params, t, t}
	a.Audit(r, "assets.preset.create", "preset", id, Object{"shared": in.Shared, "name": in.Name})
	writeJSON(w, 201, Object{"preset": p})
}
func (a *Module) presetForWrite(w http.ResponseWriter, r *http.Request) (Preset, bool) {
	p, err := scanPreset(a.DB.QueryRow("SELECT id,name,owner_id,shared,params,created_at,updated_at FROM asset_presets WHERE id=?", idParam(r, "id")))
	u := currentUser(r)
	if err != nil || p.OwnerID != u.ID {
		fail(w, 404, "预设不存在")
		return p, false
	}
	return p, true
}
func (a *Module) patchPreset(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	p, ok := a.presetForWrite(w, r)
	if !ok {
		return
	}
	var in map[string]json.RawMessage
	if !decode(w, r, &in) {
		return
	}
	if len(in) == 0 {
		fail(w, 400, "没有需要修改的字段")
		return
	}
	for k, v := range in {
		switch k {
		case "name":
			if json.Unmarshal(v, &p.Name) != nil || strings.TrimSpace(p.Name) == "" || len([]rune(p.Name)) > 120 {
				fail(w, 400, "预设名称需为 1–120 字")
				return
			}
			p.Name = strings.TrimSpace(p.Name)
		case "shared":
			if json.Unmarshal(v, &p.Shared) != nil {
				fail(w, 400, "共享状态必须是布尔值")
				return
			}
			if p.Shared && !currentUser(r).Has(core.AssetProcess) {
				fail(w, 403, "共享预设需要图片处理权限")
				return
			}
		case "params":
			var params Object
			if json.Unmarshal(v, &params) != nil {
				fail(w, 400, "预设参数必须是 JSON 对象")
				return
			}
			var err error
			p.Params, err = validatePresetParams(params)
			if err != nil {
				fail(w, 400, err.Error())
				return
			}
		default:
			fail(w, 400, "不支持的预设字段")
			return
		}
	}
	p.UpdatedAt = now()
	if _, err := a.DB.Exec("UPDATE asset_presets SET name=?,shared=?,params=?,updated_at=? WHERE id=?", p.Name, p.Shared, js(p.Params), p.UpdatedAt, p.ID); err != nil {
		fail(w, 500, "保存预设失败")
		return
	}
	a.Audit(r, "assets.preset.update", "preset", p.ID, Object{"fields": rawFields(in)})
	writeJSON(w, 200, Object{"preset": p})
}
func (a *Module) deletePreset(w http.ResponseWriter, r *http.Request) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	p, ok := a.presetForWrite(w, r)
	if !ok {
		return
	}
	if _, err := a.DB.Exec("DELETE FROM asset_presets WHERE id=?", p.ID); err != nil {
		fail(w, 500, "删除预设失败")
		return
	}
	a.Audit(r, "assets.preset.delete", "preset", p.ID, Object{"name": p.Name})
	writeJSON(w, 200, Object{})
}
