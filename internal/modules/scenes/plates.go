package scenes

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"xstudio/internal/core"
)

type Plate struct {
	ID             int64  `json:"id"`
	Status         string `json:"status"`
	URL            string `json:"url"`
	SketchURL      string `json:"sketch_url,omitempty"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	Size           string `json:"size"`
	LayoutSnapshot Layout `json:"layout_snapshot"`
	Prompt         string `json:"prompt,omitempty"`
	Model          string `json:"model,omitempty"`
	CreatedAt      string `json:"created_at"`
	File           string `json:"-"`
	SketchFile     string `json:"-"`
	rawSnapshot    string `json:"-"`
}

type plateParams struct {
	Extra string `json:"extra"`
}

func (module *Module) attachPlates(scene *Scene, user core.User) error {
	for index, element := range scene.Layout.Elements {
		if element.Type != "prop" || element.PropID <= 0 {
			continue
		}
		card, err := module.loadProp(context.Background(), element.PropID)
		if err != nil {
			return err
		}
		if !module.propVisible(user, card) {
			continue
		}
		scene.Layout.Elements[index].PropName = card.Name
		if card.CoverID != nil {
			scene.Layout.Elements[index].CoverURL = fmt.Sprintf("/api/props/%d/refs/%d/file", card.ID, *card.CoverID)
		}
	}
	rows, err := module.DB.Query(`SELECT id,status,file,width,height,size,layout_snapshot,prompt,model,sketch_file,created_at FROM scene_plates WHERE scene_id=? AND status='candidate' ORDER BY id`, scene.ID)
	if err != nil {
		return err
	}
	plates := []Plate{}
	for rows.Next() {
		plate, scanErr := scanPlate(scene.ID, rows)
		if scanErr != nil {
			err = scanErr
			break
		}
		plates = append(plates, plate)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	scene.Plates = plates
	if scene.PlateID != nil {
		plate, plateErr := module.plateByID(context.Background(), scene.ID, *scene.PlateID)
		if plateErr != nil {
			var problem *sceneError
			if errors.As(plateErr, &problem) && problem.status == 404 {
				scene.Plate = nil
			} else {
				return plateErr
			}
		} else {
			scene.Plate = &plate
			scene.PlateOutdated = layoutsDiffer(scene.Layout, plate.rawSnapshot)
		}
	}
	if scene.Plate != nil && scene.Plate.URL != "" {
		scene.ThumbURL = scene.Plate.URL
	} else if len(scene.Refs) > 0 {
		scene.ThumbURL = scene.Refs[0].URL
	}
	return nil
}
func scanPlate(sceneID int64, scanner core.Scanner) (Plate, error) {
	var plate Plate
	var snapshot string
	err := scanner.Scan(&plate.ID, &plate.Status, &plate.File, &plate.Width, &plate.Height, &plate.Size, &snapshot, &plate.Prompt, &plate.Model, &plate.SketchFile, &plate.CreatedAt)
	if err != nil {
		return plate, err
	}
	plate.rawSnapshot = snapshot
	if strings.TrimSpace(snapshot) == "" {
		snapshot = `{"size":"1024x1536","elements":[]}`
	}
	if err = json.Unmarshal([]byte(snapshot), &plate.LayoutSnapshot); err != nil {
		return plate, err
	}
	if plate.LayoutSnapshot.Elements == nil {
		plate.LayoutSnapshot.Elements = []LayoutElement{}
	}
	plate.URL = fmt.Sprintf("/api/scenes/%d/plates/%d/file", sceneID, plate.ID)
	if plate.SketchFile != "" {
		plate.SketchURL = fmt.Sprintf("/api/scenes/%d/plates/%d/sketch", sceneID, plate.ID)
	}
	return plate, nil
}
func (module *Module) plateByID(ctx context.Context, sceneID, plateID int64) (Plate, error) {
	if plateID <= 0 {
		return Plate{}, invalid(404, "定场图不存在")
	}
	plate, err := scanPlate(sceneID, module.DB.QueryRowContext(ctx, `SELECT id,status,file,width,height,size,layout_snapshot,prompt,model,sketch_file,created_at FROM scene_plates WHERE id=? AND scene_id=?`, plateID, sceneID))
	if errors.Is(err, sql.ErrNoRows) {
		return Plate{}, invalid(404, "定场图不存在")
	}
	return plate, err
}
func plateSnapshot(ctx context.Context, db *sql.DB, scene Scene) (*SnapshotPlate, error) {
	if scene.PlateID == nil {
		return nil, nil
	}
	var file, size, snapshot string
	var width, height int
	err := db.QueryRowContext(ctx, "SELECT file,width,height,size,layout_snapshot FROM scene_plates WHERE id=? AND scene_id=?", *scene.PlateID, scene.ID).Scan(&file, &width, &height, &size, &snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	out := &SnapshotPlate{Path: abs, Size: size, Width: width, Height: height}
	var layout Layout
	if json.Unmarshal([]byte(snapshot), &layout) != nil {
		return out, nil
	}
	for _, element := range layout.Elements {
		if element.Type == "product" {
			copied := element.Box
			out.Box = &copied
			break
		}
	}
	return out, nil
}
func (module *Module) plateCreate(writer http.ResponseWriter, request *http.Request) {
	scene, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	if scene.Archived {
		core.Fail(writer, 409, "场景已归档")
		return
	}
	var in plateParams
	if !core.Decode(writer, request, &in) {
		return
	}
	in.Extra = strings.TrimSpace(in.Extra)
	if utf8.RuneCountInString(in.Extra) > 400 {
		core.Fail(writer, 400, "补充说明不能超过 400 字")
		return
	}
	if module.Active("scene", scene.ID) {
		core.Fail(writer, 409, "这个场景已经有进行中的定场图任务")
		return
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(request.Context(), "INSERT INTO jobs(kind,target_type,target_id,user_id,params,status,steps,created_at) VALUES('scenes.plate','scene',?,?,?,'queued','scenes.plate',?)", scene.ID, core.CurrentUser(request).ID, core.JS(in), core.Now())
	if constraint(err) {
		core.Fail(writer, 409, "这个场景已经有进行中的定场图任务")
		return
	}
	var jobID int64
	if err == nil {
		jobID, err = result.LastInsertId()
	}
	if err == nil {
		err = core.AuditTx(tx, request, "scenes.plate", "scene", scene.ID, in)
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
func constraint(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique") || strings.Contains(text, "constraint")
}
func (module *Module) plateLock(writer http.ResponseWriter, request *http.Request) {
	module.setPlateStatus(writer, request, true)
}
func (module *Module) plateDiscard(writer http.ResponseWriter, request *http.Request) {
	module.setPlateStatus(writer, request, false)
}
func (module *Module) setPlateStatus(writer http.ResponseWriter, request *http.Request, lock bool) {
	scene, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	plate, err := module.plateByID(request.Context(), scene.ID, core.ID(request, "plate"))
	if err != nil {
		fail(writer, err)
		return
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	if lock {
		_, err = tx.ExecContext(request.Context(), "UPDATE scene_plates SET status='candidate' WHERE scene_id=? AND status='locked' AND id<>?", scene.ID, plate.ID)
		if err == nil {
			_, err = tx.ExecContext(request.Context(), "UPDATE scene_plates SET status='locked' WHERE id=? AND scene_id=?", plate.ID, scene.ID)
		}
		if err == nil {
			_, err = tx.ExecContext(request.Context(), "UPDATE scenes SET plate_id=?,updated_at=? WHERE id=?", plate.ID, core.Now(), scene.ID)
		}
		if err == nil {
			err = core.AuditTx(tx, request, "scene.plate_lock", "scene", scene.ID, core.Object{"plate_id": plate.ID})
		}
	} else {
		if plate.Status == "discarded" {
			core.Fail(writer, 409, "这张定场图已经丢弃")
			return
		}
		_, err = tx.ExecContext(request.Context(), "UPDATE scene_plates SET status='discarded' WHERE id=? AND scene_id=?", plate.ID, scene.ID)
		if err == nil {
			_, err = tx.ExecContext(request.Context(), "UPDATE scenes SET plate_id=NULL,updated_at=? WHERE id=? AND plate_id=?", core.Now(), scene.ID, plate.ID)
		}
		if err == nil {
			err = core.AuditTx(tx, request, "scene.plate_discard", "scene", scene.ID, core.Object{"plate_id": plate.ID})
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(writer, err)
		return
	}
	module.read(writer, request)
}
func (module *Module) plateFile(writer http.ResponseWriter, request *http.Request) {
	module.servePlate(writer, request, false)
}
func (module *Module) plateSketch(writer http.ResponseWriter, request *http.Request) {
	module.servePlate(writer, request, true)
}
func (module *Module) servePlate(writer http.ResponseWriter, request *http.Request, sketch bool) {
	scene, ok := module.scoped(writer, request)
	if !ok {
		return
	}
	plate, err := module.plateByID(request.Context(), scene.ID, core.ID(request, "plate"))
	if err != nil {
		fail(writer, err)
		return
	}
	path := plate.File
	if sketch {
		path = plate.SketchFile
	}
	if path == "" {
		core.Fail(writer, 404, "定场图文件不存在")
		return
	}
	name, err := module.relativeFile(path)
	if err != nil {
		fail(writer, err)
		return
	}
	root, err := module.fileRoot()
	if err != nil {
		fail(writer, err)
		return
	}
	defer root.Close()
	file, err := root.Open(name)
	if err != nil {
		core.Fail(writer, 404, "定场图文件不存在")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fail(writer, err)
		return
	}
	http.ServeContent(writer, request, filepath.Base(name), info.ModTime(), file)
}
func (module *Module) runPlate(ctx context.Context, job core.Job) error {
	var params plateParams
	if err := json.Unmarshal([]byte(core.JS(job.Params)), &params); err != nil {
		return fmt.Errorf("任务参数不正确")
	}
	scene, err := get(ctx, module.DB, job.TargetID)
	if err != nil {
		return err
	}
	if scene.Archived {
		return fmt.Errorf("场景已归档")
	}
	user, err := module.User(job.UserID)
	if err != nil {
		return fmt.Errorf("任务用户不存在")
	}
	sketch, err := drawSketch(scene.Layout)
	if err != nil {
		return err
	}
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, sketch); err != nil {
		return err
	}
	sketchRef, err := module.writeReference(scene.ID, encoded.Bytes())
	if err != nil {
		return err
	}
	files := []string{sketchRef.File}
	committed := false
	defer func() {
		if !committed {
			for _, file := range files {
				_ = module.removeFile(file)
			}
		}
	}()
	images, references, regions, err := module.plateInputs(ctx, user, scene, sketch)
	if err != nil {
		return err
	}
	prompt, err := module.fillPrompt("场景-定场图.md", map[string]string{
		"regions": regions, "references": references, "still": scene.Still, "light": scene.Light, "camera": cameraLabel(scene.Camera), "extra": params.Extra,
	})
	if err != nil {
		return err
	}
	info := &core.ImageInfo{}
	data, err := module.EditImageSize(core.WithImageInfo(core.WithFeature(ctx, "scene_plate"), info), prompt, images, scene.Layout.Size)
	if err != nil {
		return err
	}
	width, height, err := pngSize(data, scene.Layout.Size)
	if err != nil {
		return err
	}
	plateRef, err := module.writeReference(scene.ID, data)
	if err != nil {
		return err
	}
	files = append(files, plateRef.File)
	return module.savePlate(ctx, job, scene, plateRef.File, sketchRef.File, width, height, prompt, info.Model, &committed)
}
func (module *Module) plateInputs(ctx context.Context, user core.User, scene Scene, sketch image.Image) ([]image.Image, string, string, error) {
	type extra struct {
		img  image.Image
		what string
	}
	// 图像编辑最多 3 张参考图：布局示意图 + 道具合集（每个道具外框用它在布局图里的颜色）+ 1 张场景参考
	extras := []extra{}
	sheet := []sheetItem{}
	lines := []string{}
	for _, item := range layoutRegions(scene.Layout) {
		var card propCard
		known := false
		if item.Element.Type == "prop" {
			loaded, err := module.loadProp(ctx, item.Element.PropID)
			if err != nil {
				return nil, "", "", err
			}
			if module.propVisible(user, loaded) {
				card = loaded
				known = true
				if loaded.File != "" {
					data, readErr := module.readDataFile(loaded.File)
					if readErr != nil {
						return nil, "", "", fmt.Errorf("道具「%s」的锁定参考图无法读取", loaded.Name)
					}
					img, _, decErr := image.Decode(bytes.NewReader(data))
					if decErr != nil {
						return nil, "", "", fmt.Errorf("道具「%s」的锁定参考图无法读取", loaded.Name)
					}
					sheet = append(sheet, sheetItem{img: img, ink: item.Color})
				}
			}
		}
		lines = append(lines, regionLine(item, card, known))
	}
	if len(sheet) > 0 {
		extras = append(extras, extra{img: propSheet(sheet, scene.Layout.Size), what: "道具参考合集：每个道具的外框颜色对应布局示意图里同色的区域，照它的材质、颜色、形状摆进去，外框本身不要画"})
	}
	rows, err := module.DB.QueryContext(ctx, "SELECT file FROM scene_refs WHERE scene_id=? AND use='still' ORDER BY id LIMIT 2", scene.ID)
	if err != nil {
		return nil, "", "", err
	}
	stillFiles := []string{}
	for rows.Next() {
		var file string
		if err = rows.Scan(&file); err != nil {
			break
		}
		stillFiles = append(stillFiles, file)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, "", "", err
	}
	for _, file := range stillFiles {
		data, readErr := module.readDataFile(file)
		if readErr != nil {
			return nil, "", "", fmt.Errorf("场景静物参考图无法读取")
		}
		img, _, decErr := image.Decode(bytes.NewReader(data))
		if decErr != nil {
			return nil, "", "", fmt.Errorf("场景静物参考图无法读取")
		}
		extras = append(extras, extra{img: img, what: "场景静物参考，只参考气氛，不要照搬里面的物体"})
	}
	dropped := false
	if len(extras) > 2 {
		extras = extras[:2]
		dropped = true
	}
	images := []image.Image{sketch}
	notes := []string{"第1张是布局示意图，按色块位置摆放，成片里不要留下色块。"}
	for index, item := range extras {
		images = append(images, item.img)
		notes = append(notes, fmt.Sprintf("第%d张是%s。", index+2, item.what))
	}
	if dropped {
		notes = append(notes, "其余参考图放不下，只按文字说明摆放。")
	}
	return images, strings.Join(notes, "\n"), strings.Join(lines, "\n"), nil
}
func (module *Module) savePlate(ctx context.Context, job core.Job, scene Scene, file, sketch string, width, height int, prompt, model string, committed *bool) error {
	tx, err := module.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	snapshot := canonicalLayout(scene.Layout)
	result, err := tx.ExecContext(ctx, `INSERT INTO scene_plates(scene_id,status,file,width,height,size,layout_snapshot,prompt,model,sketch_file,created_by,created_at) VALUES(?,'candidate',?,?,?,?,?,?,?,?,?,?)`, scene.ID, file, width, height, scene.Layout.Size, snapshot, prompt, model, sketch, job.UserID, core.Now())
	var plateID int64
	if err == nil {
		plateID, err = result.LastInsertId()
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO audit_log(at,user_id,action,target_type,target_id,detail,ip) VALUES(?,?,?,?,?,?,?)", core.Now(), job.UserID, "scene.plate_add", "scene", scene.ID, core.JS(core.Object{"plate_id": plateID, "job_id": job.ID}), "")
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE jobs SET result=?,message=? WHERE id=?", core.JS(core.Object{"plate_id": plateID}), "已生成", job.ID)
	}
	if err != nil {
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
func (module *Module) readDataFile(path string) ([]byte, error) {
	base, err := filepath.Abs(module.Cfg.DataDir)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("参考图路径不正确")
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxReferenceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReferenceBytes {
		return nil, fmt.Errorf("参考图不能超过 20 MB")
	}
	return data, nil
}
