package assets

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"strings"
	"xstudio/internal/core"
	"xstudio/internal/imaging"
)

type modelGenerateRequest struct {
	BodyPart        string `json:"body_part"`
	Prompt          string `json:"prompt"`
	AspectRatio     string `json:"aspect_ratio"`
	BaseReferenceID *int64 `json:"base_reference_id,omitempty"`
}
type tryonRequest struct {
	Inputs      []workbenchInput `json:"inputs"`
	ModelID     int64            `json:"model_id"`
	ReferenceID int64            `json:"reference_id"`
	BodyPart    string           `json:"body_part"`
	Framing     string           `json:"framing"`
	ProductType string           `json:"product_type"`
	Prompt      string           `json:"prompt"`
	AspectRatio string           `json:"aspect_ratio"`
}
type analyzeRequest struct {
	Inputs []workbenchInput `json:"inputs"`
	Hint   string           `json:"hint"`
}
type modelJobRequest struct {
	Inputs          []workbenchInput `json:"inputs,omitempty"`
	ModelID         int64            `json:"model_id,omitempty"`
	ShopID          int64            `json:"shop_id,omitempty"`
	ReferenceID     int64            `json:"reference_id,omitempty"`
	BaseReferenceID *int64           `json:"base_reference_id,omitempty"`
	BodyPart        string           `json:"body_part,omitempty"`
	Framing         string           `json:"framing,omitempty"`
	ProductType     string           `json:"product_type,omitempty"`
	Prompt          string           `json:"prompt,omitempty"`
	AspectRatio     string           `json:"aspect_ratio,omitempty"`
	Hint            string           `json:"hint,omitempty"`
}

func validBodyPart(s string) bool {
	switch s {
	case "full_body", "upper_body", "hand", "wrist", "ear", "neck", "foot", "custom":
		return true
	}
	return false
}
func validFraming(s string) bool { return s == "full_body" || s == "half_body" || s == "closeup" }
func aspectSize(s string) string {
	return map[string]string{"1:1": "1024x1024", "2:3": "1024x1536", "3:2": "1536x1024"}[s]
}
func modelJobPermission(u core.User, kind string) bool {
	if u.Disabled || u.Role.Scope == "shares" || !u.Has(core.AssetUpload) || !u.Has(core.AssetProcess) {
		return false
	}
	if kind == "assets.tryon_analyze" {
		return true
	}
	return u.Has(core.ModelView) && (kind != "assets.model_generate" || u.Has(core.ModelManage))
}
func (a *Module) tryonSources(u core.User, inputs []workbenchInput) ([]workbenchSource, error) {
	if len(inputs) < 1 || len(inputs) > 2 {
		return nil, fmt.Errorf("请选择1–2张商品图")
	}
	seen := map[int64]bool{}
	assetMode := inputs[0].AssetID != nil
	for _, v := range inputs {
		if (v.AssetID == nil) == (v.InputID == nil) || (v.AssetID != nil) != assetMode {
			return nil, fmt.Errorf("输入只允许同商品素材或本人上传，不可混用")
		}
		id := v.InputID
		if assetMode {
			id = v.AssetID
		}
		if *id <= 0 || seen[*id] {
			return nil, fmt.Errorf("图片编号不正确或重复")
		}
		seen[*id] = true
	}
	sources, e := a.workbenchSources(u, workbenchRequest{Inputs: inputs})
	if e != nil {
		return nil, e
	}
	var pid int64
	for _, s := range sources {
		if assetMode {
			if pid != 0 && pid != s.asset.ProductID {
				return nil, fmt.Errorf("素材必须属于同一商品")
			}
			pid = s.asset.ProductID
		}
	}
	return sources, nil
}

// The same check is used at enqueue, execution and publication, including identity binding.
func (a *Module) validateModelJob(u core.User, kind string, in modelJobRequest) (ShopModel, []workbenchSource, error) {
	var m ShopModel
	if !modelJobPermission(u, kind) {
		return m, nil, fmt.Errorf("没有模特或试穿处理权限")
	}
	if !bounded(in.Prompt, 4000) || !bounded(in.Hint, 4000) || !bounded(in.ProductType, 4000) {
		return m, nil, fmt.Errorf("文本不能超过4000字")
	}
	var sources []workbenchSource
	var e error
	if kind != "assets.model_generate" {
		sources, e = a.tryonSources(u, in.Inputs)
		if e != nil {
			return m, nil, e
		}
	}
	if kind == "assets.tryon_analyze" {
		return m, sources, nil
	}
	if !validBodyPart(in.BodyPart) || aspectSize(in.AspectRatio) == "" {
		return m, nil, fmt.Errorf("身体部位或画幅不正确")
	}
	m, e = a.getModel(u, in.ModelID, true)
	if e != nil {
		return m, nil, e
	}
	if m.ShopID != in.ShopID {
		return m, nil, fmt.Errorf("模特店铺不匹配")
	}
	if kind == "assets.model_generate" {
		if in.BaseReferenceID != nil {
			if _, e = a.reference(m.ID, *in.BaseReferenceID); e != nil {
				return m, nil, fmt.Errorf("基准参考图不属于该模特")
			}
		}
		if in.BaseReferenceID == nil && in.BodyPart != "full_body" {
			var count int
			if e = a.DB.QueryRow("SELECT count(*) FROM model_references WHERE model_id=? AND deleted_at=''", m.ID).Scan(&count); e != nil {
				return m, nil, fmt.Errorf("读取基准参考失败")
			}
			if count > 0 {
				return m, nil, fmt.Errorf("生成局部参考请选择已有基准参考图")
			}
		}
	} else {
		if !validFraming(in.Framing) || (strings.TrimSpace(in.ProductType) == "" && strings.TrimSpace(in.Prompt) == "") {
			return m, nil, fmt.Errorf("请选择取景，并填写商品类型或额外要求")
		}
		if _, e = a.reference(m.ID, in.ReferenceID); e != nil {
			return m, nil, fmt.Errorf("参考图不属于该模特")
		}
	}
	return m, sources, nil
}
func (a *Module) enqueueModel(w http.ResponseWriter, r *http.Request, kind string, in modelJobRequest) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	u, e := a.User(currentUser(r).ID)
	if e != nil {
		fail(w, 403, "账号不可用")
		return
	}
	if !modelJobPermission(u, kind) {
		fail(w, 403, "没有模特或试穿处理权限")
		return
	}
	if in.ModelID > 0 {
		m, err := a.getModel(u, in.ModelID, true)
		if err != nil {
			fail(w, 404, err.Error())
			return
		}
		in.ShopID = m.ShopID
	}
	if _, _, e = a.validateModelJob(u, kind, in); e != nil {
		fail(w, 400, e.Error())
		return
	}
	res, e := a.DB.Exec("INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,steps,created_at,result) VALUES(?,'user',?,?,?,'queued',?,?,'{}')", kind, u.ID, u.ID, js(in), kind, now())
	if e != nil {
		fail(w, 409, "你已有排队或运行中的处理任务")
		return
	}
	jid, _ := res.LastInsertId()
	j, e := core.ScanJob(a.DB.QueryRow("SELECT "+core.JobColumns+" FROM jobs WHERE id=?", jid))
	if e != nil {
		fail(w, 500, "读取任务失败")
		return
	}
	a.Audit(r, kind, "user", u.ID, Object{"job_id": jid, "model_id": in.ModelID})
	a.Wake()
	writeJSON(w, 202, Object{"job": j})
}
func (a *Module) modelGenerate(w http.ResponseWriter, r *http.Request) {
	var in modelGenerateRequest
	if !modelDecode(w, r, &in) {
		return
	}
	a.enqueueModel(w, r, "assets.model_generate", modelJobRequest{ModelID: idParam(r, "id"), BodyPart: in.BodyPart, Prompt: in.Prompt, AspectRatio: in.AspectRatio, BaseReferenceID: in.BaseReferenceID})
}
func (a *Module) tryonAnalyze(w http.ResponseWriter, r *http.Request) {
	var in analyzeRequest
	if !modelDecode(w, r, &in) {
		return
	}
	a.enqueueModel(w, r, "assets.tryon_analyze", modelJobRequest{Inputs: in.Inputs, Hint: in.Hint})
}
func (a *Module) tryonGenerate(w http.ResponseWriter, r *http.Request) {
	var in tryonRequest
	if !modelDecode(w, r, &in) {
		return
	}
	a.enqueueModel(w, r, "assets.tryon", modelJobRequest{Inputs: in.Inputs, ModelID: in.ModelID, ReferenceID: in.ReferenceID, BodyPart: in.BodyPart, Framing: in.Framing, ProductType: in.ProductType, Prompt: in.Prompt, AspectRatio: in.AspectRatio})
}
func (a *Module) tryonState(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !modelJobPermission(u, "assets.tryon_analyze") {
		fail(w, 403, "没有试穿处理权限")
		return
	}
	j, e := core.ScanJob(a.DB.QueryRow("SELECT "+core.JobColumns+" FROM jobs WHERE target_type='user' AND target_id=? AND user_id=? AND status IN ('queued','running') ORDER BY id LIMIT 1", u.ID, u.ID))
	if e == sql.ErrNoRows {
		writeJSON(w, 200, Object{"job": nil})
		return
	}
	if e != nil {
		fail(w, 500, "读取任务失败")
		return
	}
	writeJSON(w, 200, Object{"job": j})
}
func replaceModelPrompt(s string, vars map[string]string) string {
	// Replace only template tokens once: user text cannot introduce more substitutions.
	pairs := []string{}
	for k, v := range vars {
		pairs = append(pairs, "{{"+k+"}}", v)
	}
	return strings.NewReplacer(pairs...).Replace(s)
}
func (a *Module) sourceImage(s workbenchSource, uid int64) (image.Image, error) {
	if s.input.AssetID != nil {
		im, ex, e := a.load(s.asset)
		if e != nil {
			return nil, fmt.Errorf("读取商品图片失败")
		}
		return imaging.Geometry(im, ex.Orientation, 0, nil), nil
	}
	root, e := a.workbenchRoot(uid)
	if e != nil {
		return nil, fmt.Errorf("读取上传图片失败")
	}
	defer root.Close()
	im, ex, e := workbenchLoad(root, s.item.File)
	if e != nil {
		return nil, fmt.Errorf("读取上传图片失败")
	}
	return imaging.Geometry(im, ex.Orientation, 0, nil), nil
}
func (a *Module) referenceImage(mid, rid int64) (image.Image, error) {
	v, e := a.reference(mid, rid)
	if e != nil {
		return nil, fmt.Errorf("参考图不存在")
	}
	root, e := a.modelRoot(mid)
	if e != nil {
		return nil, fmt.Errorf("读取模特参考失败")
	}
	defer root.Close()
	im, _, e := workbenchLoad(root, v.File)
	if e != nil {
		return nil, fmt.Errorf("读取模特参考失败")
	}
	return im, nil
}
func (a *Module) freshModelJob(j core.Job, in modelJobRequest) (core.User, ShopModel, []workbenchSource, error) {
	u, e := a.User(j.UserID)
	if e != nil {
		return u, ShopModel{}, nil, fmt.Errorf("任务发起人不存在")
	}
	m, s, e := a.validateModelJob(u, j.Kind, in)
	return u, m, s, e
}
func (a *Module) executeModelJob(ctx context.Context, j core.Job) error {
	unlock := a.lockAI(j.UserID)
	defer unlock()
	if j.TargetType != "user" || j.TargetID != j.UserID {
		return fmt.Errorf("任务归属不正确")
	}
	var in modelJobRequest
	if strictDecode(j.Params, &in) != nil {
		return fmt.Errorf("任务参数不正确")
	}
	u, m, sources, e := a.freshModelJob(j, in)
	if e != nil {
		return e
	}
	if j.Kind == "assets.tryon_analyze" {
		return a.executeTryonAnalyze(ctx, j, in, sources)
	}
	if e = a.checkQuota(u.ID, 1, 0); e != nil {
		return e
	}
	refs := []image.Image{}
	template := "模特-创建.md"
	feature := "model_create"
	opType := "model_candidate"
	vars := map[string]string{"description": m.Description, "body_part": in.BodyPart, "extra": in.Prompt}
	op := Object{"type": opType, "model_id": m.ID, "shop_id": m.ShopID, "model_name": m.Name, "body_part": in.BodyPart, "prompt": in.Prompt, "aspect_ratio": in.AspectRatio, "base_reference_id": in.BaseReferenceID}
	if j.Kind == "assets.tryon" {
		template = "试穿-生成.md"
		feature = "tryon"
		op["type"] = "tryon"
		op["reference_id"] = in.ReferenceID
		op["framing"] = in.Framing
		op["product_type"] = in.ProductType
		op["inputs"] = in.Inputs
		roles := []string{"第1张：主商品图"}
		for i, s := range sources {
			im, err := a.sourceImage(s, u.ID)
			if err != nil {
				return err
			}
			refs = append(refs, im)
			if i == 1 {
				roles = append(roles, "第2张：辅商品图（同一商品）")
			}
		}
		roles = append(roles, fmt.Sprintf("第%d张：模特参考图，保持模特身份和身体特征", len(refs)+1))
		im, err := a.referenceImage(m.ID, in.ReferenceID)
		if err != nil {
			return err
		}
		refs = append(refs, im)
		info := "用户上传无商品资料，不得猜测精确尺寸。"
		if sources[0].input.AssetID != nil {
			info = a.tryonProductInfo(sources[0].asset.ProductID)
		}
		vars = map[string]string{"model_description": m.Description, "body_part": in.BodyPart, "framing": in.Framing, "product_type": in.ProductType, "extra": in.Prompt, "reference_roles": strings.Join(roles, "\n"), "product_info": info}
	} else if in.BaseReferenceID != nil {
		template = "模特-局部参考.md"
		im, err := a.referenceImage(m.ID, *in.BaseReferenceID)
		if err != nil {
			return err
		}
		refs = append(refs, im)
	}
	prompt, e := a.prompt(template)
	if e != nil {
		return e
	}
	prompt = replaceModelPrompt(prompt, vars)
	op["template"] = template
	op["final_prompt"] = prompt
	op["model"] = a.Model(feature, a.Cfg.ImageModel)
	if _, _, _, e = a.freshModelJob(j, in); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return fmt.Errorf("任务已取消")
	}
	ctx = core.WithFeature(core.WithAIActor(ctx, core.AIActor{UserID: j.UserID, JobID: j.ID}), feature)
	var data []byte
	if len(refs) == 0 {
		data, e = a.GenerateImage(ctx, prompt, aspectSize(in.AspectRatio))
	} else {
		data, e = a.EditImageSize(ctx, prompt, refs, aspectSize(in.AspectRatio))
	}
	if e != nil {
		return e
	}
	return a.publishModelResult(ctx, j, in, op, data)
}
func (a *Module) tryonProductInfo(pid int64) string {
	var raw string
	if a.DB.QueryRow("SELECT info FROM products WHERE id=?", pid).Scan(&raw) != nil {
		return "缺少商品资料，不得猜测精确尺寸。"
	}
	info := core.JSONObj(raw)
	safe := Object{}
	// Only scalar, explicitly approved descriptive fields; never copy arbitrary nested objects.
	for _, key := range []string{"style", "size", "material", "color", "shape", "length", "width", "height", "diameter", "pearl_size"} {
		switch v := info[key].(type) {
		case string:
			if bounded(v, 500) {
				safe[key] = v
			}
		case float64, bool:
			safe[key] = v
		}
	}
	return js(safe)
}
func (a *Module) publishModelResult(ctx context.Context, j core.Job, in modelJobRequest, op Object, data []byte) error {
	cfg, _, e := imageConfig(data)
	if e != nil {
		return fmt.Errorf("生成图片无效")
	}
	root, e := a.workbenchRoot(j.UserID)
	if e != nil {
		return fmt.Errorf("打开结果目录失败")
	}
	defer root.Close()
	if e = root.MkdirAll("results", 0700); e != nil {
		return fmt.Errorf("创建结果目录失败")
	}
	file := "results/" + rand.Text() + ".png"
	release := a.holdWorkbenchFile(j.UserID, file)
	defer release()
	committed := false
	defer func() {
		if !committed {
			_ = root.Remove(file)
		}
	}()
	if e = root.WriteFile(file, data, 0600); e != nil {
		return fmt.Errorf("保存生成图片失败")
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	if ctx.Err() != nil {
		return fmt.Errorf("任务已取消")
	}
	if _, _, _, e = a.freshModelJob(j, in); e != nil {
		return e
	}
	tx, e := a.DB.Begin()
	if e != nil {
		return fmt.Errorf("保存生成结果失败")
	}
	defer tx.Rollback()
	item := WorkbenchItem{UserID: j.UserID, Kind: "result", File: file, OrigName: "AI生成.png", Width: cfg.Width, Height: cfg.Height, Source: "ai_generated", Op: op, CreatedAt: now()}
	if j.Kind == "assets.tryon" {
		item.AssetID = in.Inputs[0].AssetID
		item.InputID = in.Inputs[0].InputID
	}
	item, e = insertWorkbench(tx, item)
	if e != nil {
		return fmt.Errorf("保存生成结果失败")
	}
	result := Object{"items": []Object{{"result_id": item.ID}}}
	if j.Kind == "assets.model_generate" {
		result["model_id"] = in.ModelID
	}
	if _, e = tx.Exec("UPDATE jobs SET result=?,progress=100,message='处理完成' WHERE id=?", js(result), j.ID); e != nil {
		return fmt.Errorf("保存任务结果失败")
	}
	if e = tx.Commit(); e != nil {
		return fmt.Errorf("发布生成结果失败")
	}
	committed = true
	return nil
}

type tryonAnalysis struct {
	ProductType     string `json:"product_type"`
	BodyPart        string `json:"body_part"`
	Framing         string `json:"framing"`
	Description     string `json:"description"`
	SuggestedPrompt string `json:"suggested_prompt"`
}

func (a *Module) executeTryonAnalyze(ctx context.Context, j core.Job, in modelJobRequest, sources []workbenchSource) error {
	prompt, e := a.prompt("试穿-识别.md")
	if e != nil {
		return e
	}
	prompt = replaceModelPrompt(prompt, map[string]string{"hint": in.Hint})
	if e = a.checkQuota(j.UserID, 0, 1); e != nil {
		return e
	}
	content := []Object{{"type": "text", "text": "按图片顺序识别同一商品，返回商品类型、身体部位、取景和建议。"}}
	for _, s := range sources {
		im, err := a.sourceImage(s, j.UserID)
		if err != nil {
			return err
		}
		var b bytes.Buffer
		if err = jpeg.Encode(&b, imaging.Resize(im, 768), &jpeg.Options{Quality: 85}); err != nil {
			return fmt.Errorf("准备识别图片失败")
		}
		content = append(content, Object{"type": "image_url", "image_url": Object{"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b.Bytes())}})
	}
	if _, _, _, e = a.freshModelJob(j, in); e != nil {
		return e
	}
	ctx = core.WithFeature(core.WithAIActor(ctx, core.AIActor{UserID: j.UserID, JobID: j.ID}), "tryon_analyze")
	var out tryonAnalysis
	if e = a.AIJSON(ctx, a.Cfg.VisionModel, prompt, content, &out); e != nil {
		return fmt.Errorf("商品识别失败，请重试或手动填写")
	}
	if !validBodyPart(out.BodyPart) || !validFraming(out.Framing) || strings.TrimSpace(out.ProductType) == "" || !bounded(out.ProductType, 4000) || !bounded(out.Description, 4000) || !bounded(out.SuggestedPrompt, 4000) {
		return fmt.Errorf("识别结果字段不正确，请手动填写或重试")
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	if ctx.Err() != nil {
		return fmt.Errorf("任务已取消")
	}
	if _, _, _, e = a.freshModelJob(j, in); e != nil {
		return e
	}
	_, e = a.DB.Exec("UPDATE jobs SET result=?,progress=100,message='识别完成' WHERE id=?", js(out), j.ID)
	if e != nil {
		return fmt.Errorf("保存识别结果失败")
	}
	return nil
}
