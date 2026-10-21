package props

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
)

const propImageSize = "1024x1024"

type generateParams struct {
	Count int    `json:"count"`
	Extra string `json:"extra"`
}
type viewParams struct {
	Views []string `json:"views"`
}

func (module *Module) generate(writer http.ResponseWriter, request *http.Request) {
	prop, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	if prop.Archived {
		core.Fail(writer, 409, "道具已归档")
		return
	}
	var in generateParams
	if !core.Decode(writer, request, &in) {
		return
	}
	in.Extra = strings.TrimSpace(in.Extra)
	if in.Count < 1 || in.Count > 4 {
		core.Fail(writer, 400, "一次生成 1–4 张")
		return
	}
	if utf8.RuneCountInString(in.Extra) > 400 {
		core.Fail(writer, 400, "补充说明不能超过 400 字")
		return
	}
	module.enqueue(writer, request, "props.generate", prop.ID, in)
}
func (module *Module) views(writer http.ResponseWriter, request *http.Request) {
	prop, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	if prop.Archived {
		core.Fail(writer, 409, "道具已归档")
		return
	}
	var in viewParams
	if !core.Decode(writer, request, &in) {
		return
	}
	if len(in.Views) < 1 || len(in.Views) > 5 {
		core.Fail(writer, 400, "请选择 1–5 个视角")
		return
	}
	seen := map[string]bool{}
	for _, view := range in.Views {
		if angleNames[view] == "" || seen[view] {
			core.Fail(writer, 400, "视角请选 left、right、top、back、other，且不要重复")
			return
		}
		seen[view] = true
	}
	var locked int
	if err := module.DB.QueryRowContext(request.Context(), "SELECT count(*) FROM prop_refs WHERE prop_id=? AND view='main' AND status='locked'", prop.ID).Scan(&locked); err != nil {
		fail(writer, err)
		return
	}
	if locked == 0 {
		core.Fail(writer, 409, "先锁定一张正面参考图")
		return
	}
	module.enqueue(writer, request, "props.views", prop.ID, in)
}
func (module *Module) enqueue(writer http.ResponseWriter, request *http.Request, kind string, propID int64, params any) {
	if module.Active("prop", propID) {
		core.Fail(writer, 409, "这个道具已经有进行中的任务")
		return
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(request.Context(), "INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,steps,created_at) VALUES(?,'prop',?,?,?,'queued',?,?)", kind, propID, core.CurrentUser(request).ID, core.JS(params), kind, core.Now())
	if busy(err) {
		core.Fail(writer, 409, "这个道具已经有进行中的任务")
		return
	}
	var jobID int64
	if err == nil {
		jobID, err = result.LastInsertId()
	}
	if err == nil {
		err = core.AuditTx(tx, request, kind, "prop", propID, params)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(writer, err)
		return
	}
	module.Wake()
	job, err := core.ScanJob(module.DB.QueryRowContext(request.Context(), "SELECT "+core.JobColumns+" FROM jobs WHERE id=?", jobID))
	if err != nil {
		fail(writer, err)
		return
	}
	core.WriteJSON(writer, 202, core.Object{"job_id": jobID, "job": job})
}
func (module *Module) runGenerate(ctx context.Context, job core.Job) error {
	var params generateParams
	if err := json.Unmarshal([]byte(core.JS(job.Params)), &params); err != nil || params.Count < 1 || params.Count > 4 {
		return fmt.Errorf("任务参数不正确")
	}
	prop, err := get(ctx, module.DB, job.TargetID)
	if err != nil {
		return err
	}
	if prop.Archived {
		return fmt.Errorf("道具已归档")
	}
	prompt, err := module.fillPrompt("道具-生成.md", map[string]string{
		"kind": kindName(prop.Kind), "description": prop.Description, "size": sizePhrase(prop.SizeCM), "extra": params.Extra,
	})
	if err != nil {
		return err
	}
	callCtx := core.WithFeature(ctx, "prop_generate")
	files := []Ref{}
	committed := false
	defer func() {
		if !committed {
			for _, ref := range files {
				_ = module.removeFile(ref.File)
			}
		}
	}()
	for index := 0; index < params.Count; index++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		info := &core.ImageInfo{}
		data, genErr := module.GenerateImage(core.WithImageInfo(callCtx, info), prompt, propImageSize)
		if genErr != nil {
			return genErr
		}
		width, height, sizeErr := pngSize(data, propImageSize)
		if sizeErr != nil {
			return sizeErr
		}
		ref, writeErr := module.writeReference(prop.ID, data)
		if writeErr != nil {
			return writeErr
		}
		ref.Width, ref.Height = width, height
		ref.View = "main"
		ref.Prompt = prompt
		ref.Model = info.Model
		files = append(files, ref)
	}
	return module.saveGenerated(ctx, job, prop.ID, "ai", files, &committed)
}
func (module *Module) runViews(ctx context.Context, job core.Job) error {
	var params viewParams
	if err := json.Unmarshal([]byte(core.JS(job.Params)), &params); err != nil || len(params.Views) == 0 {
		return fmt.Errorf("任务参数不正确")
	}
	prop, err := get(ctx, module.DB, job.TargetID)
	if err != nil {
		return err
	}
	if prop.Archived {
		return fmt.Errorf("道具已归档")
	}
	var file string
	err = module.DB.QueryRowContext(ctx, "SELECT file FROM prop_refs WHERE prop_id=? AND view='main' AND status='locked' ORDER BY id DESC LIMIT 1", prop.ID).Scan(&file)
	if err != nil {
		return fmt.Errorf("先锁定一张正面参考图")
	}
	data, err := module.readDataFile(file)
	if err != nil {
		return fmt.Errorf("正面参考图无法读取")
	}
	main, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("正面参考图无法读取")
	}
	callCtx := core.WithFeature(ctx, "prop_views")
	files := []Ref{}
	committed := false
	defer func() {
		if !committed {
			for _, ref := range files {
				_ = module.removeFile(ref.File)
			}
		}
	}()
	for _, view := range params.Views {
		if angleNames[view] == "" {
			return fmt.Errorf("视角不正确")
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		prompt, fillErr := module.fillPrompt("道具-视角.md", map[string]string{
			"view": angleNames[view], "description": prop.Description, "size": sizePhrase(prop.SizeCM),
		})
		if fillErr != nil {
			return fillErr
		}
		info := &core.ImageInfo{}
		generated, editErr := module.EditImageSize(core.WithImageInfo(callCtx, info), prompt, []image.Image{main}, propImageSize)
		if editErr != nil {
			return editErr
		}
		width, height, sizeErr := pngSize(generated, propImageSize)
		if sizeErr != nil {
			return sizeErr
		}
		ref, writeErr := module.writeReference(prop.ID, generated)
		if writeErr != nil {
			return writeErr
		}
		ref.Width, ref.Height = width, height
		ref.View = view
		ref.Prompt = prompt
		ref.Model = info.Model
		files = append(files, ref)
	}
	return module.saveGenerated(ctx, job, prop.ID, "ai", files, &committed)
}
func (module *Module) saveGenerated(ctx context.Context, job core.Job, propID int64, source string, files []Ref, committed *bool) error {
	tx, err := module.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ids := []int64{}
	for _, ref := range files {
		result, execErr := tx.ExecContext(ctx, "INSERT INTO prop_refs(prop_id,view,source,status,file,width,height,prompt,model,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", propID, ref.View, source, "candidate", ref.File, ref.Width, ref.Height, ref.Prompt, ref.Model, job.UserID, core.Now())
		if execErr != nil {
			return execErr
		}
		id, idErr := result.LastInsertId()
		if idErr != nil {
			return idErr
		}
		ids = append(ids, id)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_log(at,user_id,action,target_type,target_id,detail,ip) VALUES(?,?,?,?,?,?,?)", core.Now(), job.UserID, "prop.ref_add", "prop", propID, core.JS(core.Object{"ref_ids": ids, "job_id": job.ID, "source": source}), ""); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE jobs SET result=?,message=? WHERE id=?", core.JS(core.Object{"ref_ids": ids}), "已生成", job.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	*committed = true
	return nil
}
func (module *Module) fillPrompt(name string, vars map[string]string) (string, error) {
	root, err := os.OpenRoot(module.Cfg.PromptDir)
	if err != nil {
		return "", fmt.Errorf("缺少提示词文件：%s", name)
	}
	defer root.Close()
	data, err := root.ReadFile(name)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return "", fmt.Errorf("缺少或空提示词文件：%s", name)
	}
	text := string(data)
	if index := strings.Index(text, "## 规则（后端照做，不给模型看）"); index >= 0 {
		text = text[:index]
	}
	pairs := make([]string, 0, len(vars)*2)
	for key, value := range vars {
		pairs = append(pairs, "{{"+key+"}}", value)
	}
	return strings.TrimSpace(strings.NewReplacer(pairs...).Replace(text)), nil
}
func pngSize(data []byte, size string) (int, int, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" {
		return 0, 0, fmt.Errorf("生成图片不是有效的 PNG")
	}
	if fmt.Sprintf("%dx%d", cfg.Width, cfg.Height) != size {
		return 0, 0, fmt.Errorf("生成图片尺寸不对")
	}
	return cfg.Width, cfg.Height, nil
}
