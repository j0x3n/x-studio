package assets

import (
	"context"
	"crypto/rand"
	"fmt"
	"image"
	"net/http"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/imaging"
)

type workbenchInput struct {
	AssetID *int64 `json:"asset_id,omitempty"`
	InputID *int64 `json:"input_id,omitempty"`
}
type workbenchRequest struct {
	Inputs   []workbenchInput `json:"inputs"`
	Ops      Ops              `json:"ops"`
	PresetID *int64           `json:"preset_id,omitempty"`
}
type workbenchSource struct {
	input workbenchInput
	asset Asset
	item  WorkbenchItem
}

func validateWorkbench(in *workbenchRequest) error {
	if len(in.Inputs) < 1 || len(in.Inputs) > 20 {
		return fmt.Errorf("一次处理需要 1–20 张图片")
	}
	seen := map[string]bool{}
	for _, v := range in.Inputs {
		if (v.AssetID == nil) == (v.InputID == nil) {
			return fmt.Errorf("每个输入必须且只能传 asset_id 或 input_id")
		}
		key := ""
		if v.AssetID != nil {
			if *v.AssetID <= 0 {
				return fmt.Errorf("素材编号不正确")
			}
			key = fmt.Sprintf("a%d", *v.AssetID)
		} else {
			if *v.InputID <= 0 {
				return fmt.Errorf("输入编号不正确")
			}
			key = fmt.Sprintf("i%d", *v.InputID)
		}
		if seen[key] {
			return fmt.Errorf("输入图片不能重复")
		}
		seen[key] = true
	}
	if err := validateOps(&in.Ops); err != nil {
		return err
	}
	if in.Ops.Color != nil && in.Ops.Color.MatchTo > 0 {
		found := false
		for _, v := range in.Inputs {
			if v.AssetID != nil && *v.AssetID == in.Ops.Color.MatchTo {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("校色参考图必须是本次输入里的一张素材")
		}
	}
	return nil
}
func (a *Module) workbenchSources(u core.User, in workbenchRequest) ([]workbenchSource, error) {
	items := make([]workbenchSource, 0, len(in.Inputs))
	for _, v := range in.Inputs {
		s := workbenchSource{input: v}
		if v.AssetID != nil {
			p, err := a.asset(*v.AssetID)
			if err != nil || !a.CanProduct(u, p.ProductID) {
				return nil, fmt.Errorf("素材不存在")
			}
			s.asset = p
		} else {
			p, err := a.workbenchItem(u.ID, *v.InputID)
			if err != nil || p.Kind != "input" {
				return nil, fmt.Errorf("处理台图片不存在")
			}
			s.item = p
		}
		items = append(items, s)
	}
	return items, nil
}
func (a *Module) workbenchProcess(w http.ResponseWriter, r *http.Request) {
	var raw Object
	if !decode(w, r, &raw) {
		return
	}
	// set_current belongs to save; presets created for the older endpoint may
	// contain its default, but clients cannot send it here.
	if _, ok := raw["set_current"]; ok {
		fail(w, 400, "set_current 请在保存时传入")
		return
	}
	merged, err := a.resolveProcessPreset(raw, currentUser(r))
	if err != nil {
		status := 400
		if err.Error() == "预设不存在" {
			status = 404
		}
		fail(w, status, err.Error())
		return
	}
	delete(merged, "set_current")
	var in workbenchRequest
	if err = strictDecode(merged, &in); err == nil {
		err = validateWorkbench(&in)
	}
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !currentUser(r).Has(processPermission(in.Ops)) {
		fail(w, 403, "没有此图片处理的权限")
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	if _, err = a.workbenchSources(currentUser(r), in); err != nil {
		fail(w, 404, err.Error())
		return
	}
	uid := currentUser(r).ID
	res, err := a.DB.Exec("INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,steps,created_at,result) VALUES('assets.workbench','user',?,?,?,'queued','assets.workbench',?,?)", uid, uid, js(in), now(), js(Object{"items": []Object{}}))
	if err != nil {
		fail(w, 409, "你已有排队或运行中的处理任务")
		return
	}
	jid, _ := res.LastInsertId()
	j, err := core.ScanJob(a.DB.QueryRow("SELECT "+core.JobColumns+" FROM jobs WHERE id=?", jid))
	if err != nil {
		fail(w, 500, "读取任务失败")
		return
	}
	a.Audit(r, "assets.workbench.process", "user", uid, Object{"inputs": in.Inputs, "ops": operationNames(in.Ops), "job_id": jid})
	a.Wake()
	writeJSON(w, 202, Object{"job": j})
}
func (a *Module) executeWorkbench(ctx context.Context, j core.Job) error {
	unlock := a.lockAI(j.UserID)
	defer unlock()
	var in workbenchRequest
	if err := strictDecode(j.Params, &in); err != nil {
		return err
	}
	if err := validateWorkbench(&in); err != nil {
		return err
	}
	if j.TargetType != "user" || j.TargetID != j.UserID {
		return fmt.Errorf("任务归属不正确")
	}
	u, err := a.User(j.UserID)
	if err != nil || u.Disabled || !u.Has(core.AssetUpload) || !u.Has(processPermission(in.Ops)) {
		return fmt.Errorf("任务发起人已停用或没有处理权限")
	}
	items, err := a.workbenchSources(u, in)
	if err != nil {
		return err
	}
	prompts, err := a.preparePrompts(in.Ops)
	if err != nil {
		return err
	}
	calls := 0
	if in.Ops.Upscale != nil {
		calls++
	}
	if in.Ops.AI != nil {
		calls++
	}
	if calls > 0 && a.Cfg.AIKey == "" {
		return fmt.Errorf("未配置 AI_API_KEY，无法开始整批处理")
	}
	if err = a.checkQuota(j.UserID, calls*len(items), 0); err != nil {
		return err
	}
	root, err := a.workbenchRoot(u.ID)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.MkdirAll("results", 0700); err != nil {
		return err
	}
	var ref *image.NRGBA
	if in.Ops.Color != nil && in.Ops.Color.MatchTo > 0 {
		p, e := a.asset(in.Ops.Color.MatchTo)
		if e != nil || !a.CanProduct(u, p.ProductID) {
			return fmt.Errorf("校色参考图不存在")
		}
		im, ex, e := a.load(p)
		if e != nil {
			return e
		}
		ref = imaging.Geometry(im, ex.Orientation, 0, nil)
	}
	results := make([]Object, 0, len(items))
	staged := make([]*stagedProcess, len(items))
	names := make([]string, len(items))
	defer func() {
		for _, v := range staged {
			if v != nil {
				_ = root.Remove(v.file)
				a.workbenchFiles.Delete(fmt.Sprintf("%d/%s", u.ID, v.file))
			}
		}
	}()
	for i, s := range items {
		if ctx.Err() != nil {
			return fmt.Errorf("处理任务已取消")
		}
		row := Object{}
		if s.input.AssetID != nil {
			row["asset_id"] = *s.input.AssetID
		} else {
			row["input_id"] = *s.input.InputID
		}
		results = append(results, row)
		var im *image.NRGBA
		var ex imaging.Exif
		source, name, code := "original", s.item.OrigName, ""
		// Recheck source visibility immediately before reading each image.
		fresh, e := a.User(j.UserID)
		if e != nil || fresh.Disabled || !fresh.Has(core.AssetUpload) || !fresh.Has(processPermission(in.Ops)) {
			return fmt.Errorf("处理权限已变化")
		}
		checked, e := a.workbenchSources(fresh, workbenchRequest{Inputs: []workbenchInput{s.input}})
		if e != nil {
			row["error"] = e.Error()
			continue
		}
		s = checked[0]
		if s.input.AssetID != nil {
			im, ex, err = a.load(s.asset)
			source = s.asset.EffectiveSource()
			name = s.asset.OrigName
			code = s.asset.Code
		} else {
			im, ex, err = workbenchLoad(root, s.item.File)
		}
		if err != nil {
			row["error"] = err.Error()
			continue
		}
		v, e := a.processImage(ctx, im, ex, source, code, ProcessRequest{Ops: in.Ops, PresetID: in.PresetID}, prompts, ref, func(im image.Image, format string) (string, error) {
			file := "results/" + rand.Text() + "." + format
			b, e := encodeWorkbench(im, format)
			if e != nil {
				return "", e
			}
			a.workbenchFiles.Store(fmt.Sprintf("%d/%s", u.ID, file), true)
			if e = root.WriteFile(file, b, 0600); e != nil {
				_ = root.Remove(file)
				a.workbenchFiles.Delete(fmt.Sprintf("%d/%s", u.ID, file))
				return "", e
			}
			return file, nil
		})
		if e != nil {
			if strings.Contains(e.Error(), "额度") {
				return e
			}
			row["error"] = a.SafeAIError(e.Error())
			continue
		}
		staged[i], names[i], items[i] = v, name, s
		_, _ = a.DB.Exec("UPDATE jobs SET progress=?,message=? WHERE id=?", (i+1)*80/len(items), fmt.Sprintf("已处理 %d/%d 张", i+1, len(items)), j.ID)
	}
	// Like executeProcess, publish only after the full batch has passed processing.
	// A cancellation or unexpected quota failure discards all staged outputs.
	for i, v := range staged {
		if v == nil {
			continue
		}
		s, row, name := items[i], results[i], names[i]
		e := func() error {
			a.Mu.Lock()
			defer a.Mu.Unlock()
			fresh, e := a.User(j.UserID)
			if e != nil || fresh.Disabled || !fresh.Has(core.AssetUpload) || !fresh.Has(processPermission(in.Ops)) {
				return fmt.Errorf("处理权限已变化")
			}
			if _, e = a.workbenchSources(fresh, workbenchRequest{Inputs: []workbenchInput{s.input}}); e != nil {
				return e
			}
			tx, e := a.DB.Begin()
			if e != nil {
				return e
			}
			defer tx.Rollback()
			item, e := insertWorkbench(tx, WorkbenchItem{UserID: u.ID, Kind: "result", File: v.file, OrigName: name, Width: v.width, Height: v.height, PHash: v.phash, Source: v.source, AssetID: s.input.AssetID, InputID: s.input.InputID, Op: v.op, CreatedAt: now()})
			if e != nil {
				return e
			}
			if e = tx.Commit(); e != nil {
				return e
			}
			row["result_id"] = item.ID
			a.workbenchFiles.Delete(fmt.Sprintf("%d/%s", u.ID, v.file))
			return nil
		}()
		if e != nil {
			row["error"] = e.Error()
		} else {
			staged[i] = nil
		}
	}
	return a.saveResults(j, results)
}
