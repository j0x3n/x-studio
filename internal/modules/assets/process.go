package assets

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"xstudio/internal/core"
	"xstudio/internal/imaging"
)

func sharedPipelineBusy(db interface{ QueryRow(string, ...any) *sql.Row }, pid int64) bool {
	var n int
	err := db.QueryRow(`SELECT count(*) FROM jobs j JOIN listings l ON j.target_type='listing' AND j.target_id=l.id WHERE l.product_id=? AND j.status IN ('queued','running') AND (instr(','||j.steps||',',',check,')>0 OR instr(','||j.steps||',',',classify,')>0 OR instr(','||j.steps||',',',enhance,')>0 OR instr(','||j.steps||',',',retouch,')>0)`, pid).Scan(&n)
	return err != nil || n > 0
}
func (a *Module) process(w http.ResponseWriter, r *http.Request) {
	var raw Object
	if !decode(w, r, &raw) {
		return
	}
	in, err := a.processRequest(raw, currentUser(r))
	if err != nil {
		status := 400
		if err.Error() == "预设不存在" {
			status = 404
		}
		fail(w, status, err.Error())
		return
	}
	perm := processPermission(in.Ops)
	if !currentUser(r).Has(perm) {
		fail(w, 403, "没有此图片处理的权限")
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	items, err := a.selected(currentUser(r), in.AssetIDs)
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	pid := items[0].ProductID
	for _, p := range items {
		if p.ProductID != pid {
			fail(w, 400, "一次处理的图片必须属于同一商品")
			return
		}
		if currentUser(r).Role.Scope == "shares" && !a.CanUpload(currentUser(r), pid) {
			fail(w, 403, "此商品不允许生成新素材")
			return
		}
	}
	if in.Ops.Color != nil && in.Ops.Color.MatchTo > 0 {
		ref, e := a.asset(in.Ops.Color.MatchTo)
		if e != nil || !a.CanProduct(currentUser(r), ref.ProductID) {
			fail(w, 404, "校色参考图不存在")
			return
		}
	}
	if in.SetCurrent && sharedPipelineBusy(a.DB, pid) {
		fail(w, 409, "商品正在检查、分拣、增强或修图，不能转移编号")
		return
	}
	j, err := a.enqueue("assets.process", pid, currentUser(r).ID, in)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	a.Audit(r, "assets.process", "product", pid, Object{"asset_ids": in.AssetIDs, "job_id": j.ID, "set_current": in.SetCurrent, "ops": operationNames(in.Ops)})
	writeJSON(w, 202, Object{"job": j})
}
func operationNames(o Ops) []string {
	m := core.JSONObj(js(o))
	names := []string{}
	for k := range m {
		names = append(names, k)
	}
	return names
}

type processPrompts struct {
	upscale, ai string
	edited      bool
	names       []string
}

func (a *Module) preparePrompts(o Ops) (processPrompts, error) {
	var out processPrompts
	var err error
	if o.Upscale != nil {
		out.upscale, err = a.prompt("处理-高清化.md")
		if err != nil {
			return out, err
		}
		out.names = append(out.names, "处理-高清化.md")
	}
	if o.AI != nil {
		quick := []string{}
		if len(o.AI.Quick) > 0 {
			table, e := a.prompt("处理-快捷需求.md")
			if e != nil {
				return out, e
			}
			type row struct{ kind, text string }
			known := map[string]row{}
			for _, line := range strings.Split(strings.ReplaceAll(table, `\t`, "\t"), "\n") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				cols := strings.SplitN(line, "\t", 3)
				if len(cols) != 3 || cols[0] == "key" {
					continue
				}
				if cols[1] != "轻度" && cols[1] != "改图" {
					return out, fmt.Errorf("快捷需求表分类不正确")
				}
				known[cols[0]] = row{cols[1], strings.TrimSpace(cols[2])}
			}
			for _, key := range o.AI.Quick {
				v, ok := known[key]
				if !ok || v.text == "" {
					return out, fmt.Errorf("缺少提示词：快捷需求 %s", key)
				}
				quick = append(quick, v.text)
				if v.kind == "改图" || key == "bg_white" || key == "bg_linen" || key == "bg_velvet" || key == "shadow" {
					out.edited = true
				}
			}
			out.names = append(out.names, "处理-快捷需求.md")
		}
		skeleton, e := a.prompt("处理-额外需求.md")
		if e != nil {
			return out, e
		}
		if !strings.Contains(skeleton, "{{quick}}") || !strings.Contains(skeleton, "{{extra}}") {
			return out, fmt.Errorf("处理-额外需求.md 缺少 quick 或 extra 占位符")
		}
		out.ai = strings.NewReplacer("{{quick}}", strings.Join(quick, "\n"), "{{extra}}", o.AI.Extra).Replace(skeleton)
		out.names = append(out.names, "处理-额外需求.md")
		if o.AI.Extra != "" {
			out.edited = true
		}
	}
	return out, nil
}
func composeSource(input, current string) string {
	rank := map[string]int{"original": 0, "ai_enhanced": 1, "ai_edited": 2, "ai_generated": 3}
	if rank[input] > rank[current] {
		return input
	}
	return current
}

type stagedProcess struct {
	input         Asset
	width, height int
	phash         string
	file, source  string
	op            Object
}

func (a *Module) executeProcess(ctx context.Context, j core.Job) error {
	unlock := a.lockAI(j.UserID)
	defer unlock()
	var in ProcessRequest
	if err := strictDecode(j.Params, &in); err != nil {
		return err
	}
	if err := validateOps(&in.Ops); err != nil {
		return err
	}
	u, items, err := a.jobAssets(ctx, j, in.AssetIDs, processPermission(in.Ops))
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
	staged := make([]*stagedProcess, len(items))
	results := make([]Object, len(items))
	defer func() {
		for _, v := range staged {
			if v != nil {
				_ = os.Remove(v.file)
			}
		}
	}()
	for i, p := range items {
		results[i] = Object{"asset_id": p.ID}
		if ctx.Err() != nil {
			return fmt.Errorf("处理任务已取消")
		}
		v, e := a.processOne(ctx, p, in, prompts, ref)
		if e != nil {
			if strings.Contains(e.Error(), "额度") {
				return e
			}
			results[i]["error"] = a.SafeAIError(e.Error())
			continue
		}
		staged[i] = v
		_, _ = a.DB.Exec("UPDATE jobs SET progress=?,message=? WHERE id=?", (i+1)*80/len(items), fmt.Sprintf("已处理 %d/%d 张", i+1, len(items)), j.ID)
	}
	for i, v := range staged {
		if v == nil {
			continue
		}
		newID, e := a.publishProcess(j, in, *v)
		if e != nil {
			results[i]["error"] = e.Error()
		} else {
			results[i]["new_asset_id"] = newID
		}
	}
	return a.saveResults(j, results)
}
func (a *Module) processOne(ctx context.Context, p Asset, in ProcessRequest, prompts processPrompts, ref *image.NRGBA) (*stagedProcess, error) {
	started := time.Now()
	raw, ex, err := a.load(p)
	if err != nil {
		return nil, err
	}
	original := imaging.Geometry(raw, ex.Orientation, 0, nil)
	im := original
	o := in.Ops
	op := Object{"ops": o, "preset_id": in.PresetID, "set_current": in.SetCurrent, "prompts": prompts.names, "models": Object{}}
	warnings := []string{}
	models := op["models"].(Object)
	source := p.EffectiveSource()
	if o.Geometry != nil {
		im = imaging.Geometry(raw, ex.Orientation, o.Geometry.Rotate, o.Geometry.Crop)
	}
	if ratio, ok := map[string]float64{"1:1": 1, "3:4": .75, "4:3": 4.0 / 3, "16:9": 16.0 / 9}[o.Aspect]; ok {
		im = imaging.Aspect(im, ratio)
	}
	if o.Color != nil {
		var params Object
		im, params = imaging.Color(im, p.Code, o.Color.AutoWB, ref)
		op["color"] = params
	}
	if o.Cutout != nil {
		mask, mode := imaging.Mask(ctx, im, a.Cfg.RembgURL, a.Client)
		op["cutout"] = mode
		im = imaging.ReplaceBackground(im, mask, o.Cutout.Background == "transparent")
		if mode == "fallback" {
			warnings = append(warnings, "抠图服务不可用，已使用边缘连通算法")
		}
	}
	edit := func(feature, prompt string) error {
		models[feature] = a.Model(feature, a.Cfg.ImageModel)
		data, e := a.EditImage(core.WithFeature(ctx, feature), prompt, []image.Image{im})
		if e != nil {
			return e
		}
		if _, _, e = imageConfig(data); e != nil {
			return fmt.Errorf("AI 图片格式或像素数量不符合要求")
		}
		decoded, _, e := image.Decode(bytes.NewReader(data))
		if e != nil {
			return fmt.Errorf("AI 图片无法解码")
		}
		im = imaging.Geometry(decoded, 1, 0, nil)
		return nil
	}
	if o.Upscale != nil {
		if err = edit("upscale", prompts.upscale); err != nil {
			return nil, err
		}
		im, err = imaging.Resample(im, o.Upscale.TargetLongSide)
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, "高清化采用 AI 重绘后重采样，并非真正的无损放大")
		source = composeSource(source, "ai_enhanced")
	}
	if o.AI != nil {
		if err = edit("process", prompts.ai); err != nil {
			return nil, err
		}
		kind := "ai_enhanced"
		if prompts.edited {
			kind = "ai_edited"
		}
		source = composeSource(source, kind)
	}
	if o.Upscale != nil || o.AI != nil {
		diff := imaging.Diff(original, im)
		op["diff"] = diff
		if diff["phash_distance"].(int) > 16 {
			warnings = append(warnings, "商品外观可能被改动，请对照原图")
		}
	}
	if o.Resize != nil {
		im, err = imaging.Resample(im, o.Resize.LongSide)
		if err != nil {
			return nil, err
		}
	}
	if o.Format == "jpg" {
		im = imaging.Flatten(im)
		if o.Cutout != nil && o.Cutout.Background == "transparent" {
			warnings = append(warnings, "JPG 不支持透明，已合成白底")
		}
	}
	if err = os.MkdirAll(filepath.Join(a.Dir(p.ProductID), "原图"), 0700); err != nil {
		return nil, fmt.Errorf("创建处理图片目录失败")
	}
	f, err := os.CreateTemp(filepath.Join(a.Dir(p.ProductID), "原图"), ".process-*.")
	if err != nil {
		return nil, fmt.Errorf("创建处理图片临时文件失败")
	}
	name := f.Name()
	f.Close()
	if o.Format == "png" {
		err = imaging.SavePNG(name, im)
	} else {
		err = imaging.SaveJPEG(name, im)
	}
	if err != nil {
		os.Remove(name)
		return nil, fmt.Errorf("保存处理图片失败")
	}
	op["warnings"] = warnings
	op["elapsed_ms"] = time.Since(started).Milliseconds()
	return &stagedProcess{input: p, width: im.Rect.Dx(), height: im.Rect.Dy(), phash: fmt.Sprintf("%016x", imaging.Hash(im)), file: name, source: source, op: op}, nil
}
func (a *Module) publishProcess(j core.Job, in ProcessRequest, v stagedProcess) (int64, error) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	u, err := a.User(j.UserID)
	if err != nil || u.Disabled || !u.Has(processPermission(in.Ops)) {
		return 0, fmt.Errorf("处理权限已变化")
	}
	fresh, err := a.asset(v.input.ID)
	if err != nil || fresh.ProductID != j.TargetID || !a.CanProduct(u, fresh.ProductID) || (u.Role.Scope == "shares" && !a.CanUpload(u, fresh.ProductID)) {
		return 0, fmt.Errorf("原素材已移动、删除或不再可见")
	}
	code := ""
	if in.SetCurrent && v.input.Code != "" {
		if sharedPipelineBusy(a.DB, fresh.ProductID) {
			v.op["warnings"] = append(v.op["warnings"].([]string), "一键出图正在使用共用素材，已跳过设为当前用图")
		} else if fresh.Code != v.input.Code {
			v.op["warnings"] = append(v.op["warnings"].([]string), "原素材编号已变化，已跳过设为当前用图")
		} else {
			code = fresh.Code
		}
	}
	tx, err := a.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("保存新版本失败")
	}
	defer tx.Rollback()
	if code != "" {
		if _, err = tx.Exec("UPDATE assets SET code='',updated_at=? WHERE id=? AND code=?", now(), fresh.ID, code); err != nil {
			return 0, fmt.Errorf("转移拍摄编号失败")
		}
	}
	t := now()
	res, err := tx.Exec("INSERT INTO assets(product_id,parent_id,file,orig_name,width,height,source,code,manual,tags,created_by,created_at,updated_at,title,note,op,phash,ai_enhanced,ai_generated) VALUES(?,?,'',?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", fresh.ProductID, fresh.ID, fresh.OrigName, v.width, v.height, v.source, code, code != "", js(fresh.Tags), j.UserID, t, t, fresh.Title, fresh.Note, js(v.op), v.phash, v.source != "original", v.source == "ai_generated")
	if err != nil {
		return 0, fmt.Errorf("保存新素材记录失败：%w", err)
	}
	id, _ := res.LastInsertId()
	file := fmt.Sprintf("原图/proc_%d.%s", id, in.Ops.Format)
	if _, err = tx.Exec("UPDATE assets SET file=? WHERE id=?", file, id); err != nil {
		return 0, fmt.Errorf("保存新素材路径失败")
	}
	dest := filepath.Join(a.Dir(fresh.ProductID), filepath.FromSlash(file))
	if _, err = os.Stat(dest); err == nil {
		return 0, fmt.Errorf("新素材目标文件已存在")
	}
	if err = os.Rename(v.file, dest); err != nil {
		return 0, fmt.Errorf("写入处理文件失败")
	}
	if err = tx.Commit(); err != nil {
		os.Remove(dest)
		return 0, fmt.Errorf("提交新版本失败")
	}
	if code != "" {
		a.removePhotoOutputs(fresh.ProductID, code)
		a.AuditUser(j.UserID, "", "assets.set_current", "asset", id, Object{"from_asset_id": fresh.ID, "to_asset_id": id, "code": code, "job_id": j.ID})
	}
	return id, nil
}
