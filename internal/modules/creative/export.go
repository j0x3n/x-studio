package creative

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"image"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"xstudio/internal/core"
)

const exportTimeLayout = "2006-01-02T15:04:05.000000000Z"

func slotLabel(slot string) string {
	if strings.HasPrefix(slot, "d") {
		return "第 " + strings.TrimPrefix(slot, "d") + " 屏"
	}
	if slot == "main1" {
		return "首图"
	}
	if strings.HasPrefix(slot, "main") {
		return "主图 " + strings.TrimPrefix(slot, "main")
	}
	return map[string]string{"sku": "规格图", "white": "白底图"}[slot]
}

func exportMissing(set Set, items []Item, settings core.Object) []string {
	missing := []string{}
	if len(items) == 0 {
		missing = append(missing, "还没有出图")
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.Slot] = true
		label := fmt.Sprintf("%s（第 %d 张%s）", slotLabel(item.Slot), item.Seq, item.Variant)
		if item.OutputAssetID == 0 {
			missing = append(missing, label+" 尚未生成")
			continue
		}
		if item.Human == "discard" || (item.Human != "pass" && item.Route != "auto_pass") {
			missing = append(missing, label+" 还没有通过复核")
		}
		if !item.Hard.Pixel.Pass || !item.Hard.Format.Pass || len(item.Hard.Banned) > 0 || item.Hard.Error != "" {
			missing = append(missing, label+" 硬检查未通过")
		}
		checks := map[string]TextCheck{}
		for _, check := range item.Hard.Text {
			checks[check.Key] = check
		}
		for _, text := range item.Texts {
			if text.Critical || critical(text.Text) {
				check, ok := checks[text.Key]
				if !ok || !check.Pass || check.Expect != text.Text {
					missing = append(missing, label+" 关键文字未核对通过："+text.Key)
				}
			}
		}
	}
	for slot := range set.TemplateKeys {
		if !seen[slot] {
			missing = append(missing, slotLabel(slot)+" 缺少当前图片")
		}
	}
	if settings["final_glance"] == true && set.GlancedAt == "" {
		missing = append(missing, "尚未整套过目")
	}
	return missing
}
func safeName(name string) string {
	name = strings.Map(func(char rune) rune {
		if char < 32 || strings.ContainsRune(`/\:*?"<>|`, char) {
			return '_'
		}
		return char
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "未命名"
	}
	runes := []rune(name)
	if len(runes) > 80 {
		name = string(runes[:80])
	}
	return name
}
func exportPath(item Item, size ExportSize, product core.Product, width, height int) string {
	suffix := ""
	if item.Seq > 1 {
		suffix = "-" + strconv.Itoa(item.Seq)
	}
	switch {
	case strings.HasPrefix(item.Slot, "main"):
		index, _ := strconv.Atoi(strings.TrimPrefix(item.Slot, "main"))
		name := fmt.Sprintf("%02d", index)
		if item.Variant == "B" {
			name += "B"
		}
		return fmt.Sprintf("主图/%s_%dx%d/%s%s.jpg", safeName(size.Name), width, height, name, suffix)
	case item.Slot == "sku":
		name := fmt.Sprintf("规格%d", item.Seq)
		if sku, ok := product.Listing["sku"].([]any); ok && item.Seq <= len(sku) {
			if spec, ok := sku[item.Seq-1].(map[string]any); ok && stringValue(spec["name"]) != "" {
				name = stringValue(spec["name"])
			}
		}
		return "SKU/" + safeName(name) + ".jpg"
	case item.Slot == "white":
		return "白底/800x800.jpg"
	default:
		index, _ := strconv.Atoi(strings.TrimPrefix(item.Slot, "d"))
		return fmt.Sprintf("详情页/%d/%02d%s.jpg", width, index, suffix)
	}
}
func (a *Module) exportImage(item Item, size ExportSize) (*image.NRGBA, int, int, error) {
	output, err := a.outputImage(item)
	if err != nil {
		return nil, 0, 0, err
	}
	genWidth, genHeight := imageSize(item.TemplateSnapshot.GenSize)
	if output.Rect.Dx() != genWidth || output.Rect.Dy() != genHeight {
		return nil, 0, 0, problem(409, "成品尺寸已变化")
	}
	width, height := size.W, size.H
	if height == 0 {
		height = max(1, int(float64(output.Rect.Dy())*float64(width)/float64(output.Rect.Dx())+0.5))
	}
	if int64(width)*int64(height) > 40_000_000 {
		return nil, 0, 0, problem(400, "导出尺寸过大")
	}
	if item.ProductMode == "pixel_lock" {
		root := a.ListingDir(item.ListingID)
		base := strings.TrimSuffix(item.CanvasFile, ".png")
		original, err := loadImage(root, base+"-source.png")
		if err != nil {
			return nil, 0, 0, err
		}
		mask, err := loadImage(root, base+"-source-mask.png")
		if err != nil {
			return nil, 0, 0, err
		}
		result, check := exportLocked(output, original, alphaImage(mask), item.TemplateSnapshot.ProductBox, width, height, item.TemplateSnapshot.LockShape)
		if !check.Pass {
			return nil, 0, 0, problem(409, "导出像素锁定检查未通过")
		}
		return result, width, height, nil
	}
	return resizeCrop(output, width, height), width, height, nil
}
func (a *Module) whiteImage(product core.Product) (*image.NRGBA, error) {
	for _, code := range []string{"A1", "A2", "A1_AI", "A2_AI"} {
		for _, dir := range []string{"照片", "增强"} {
			im, err := loadImage(a.Dir(product.ID), dir+"/"+code+"_白底.jpg")
			if err == nil {
				return resizeCrop(im, 800, 800), nil
			}
		}
	}
	return nil, problem(409, "缺少一键出图的 "+photoCodeNames(product.Category, []string{"A1", "A2"})+" 白底图，请先完成抠图")
}
func (a *Module) makeExport(ctx context.Context, set Set, items []Item) (map[string][]byte, string, error) {
	product, err := a.PipelineProduct(set.ListingID)
	if err != nil {
		return nil, "", err
	}
	files := map[string][]byte{}
	shop, err := a.shopSettings(ctx, product.ShopID)
	if err != nil {
		return nil, "", err
	}
	var record bytes.Buffer
	record.Write([]byte{0xef, 0xbb, 0xbf})
	writer := csv.NewWriter(&record)
	if err = writer.Write([]string{"位置", "张数", "版本", "模板 key", "模板 version", "route", "人工判断", "分数 A", "分数 B"}); err != nil {
		return nil, "", err
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		for _, size := range exportSizes(item, shop) {
			im, width, height, err := a.exportImage(item, size)
			if err != nil {
				return nil, "", err
			}
			data, err := jpegBytes(im)
			if err != nil {
				return nil, "", err
			}
			path := exportPath(item, size, product, width, height)
			if _, exists := files[path]; exists {
				return nil, "", problem(409, "导出文件名重复："+path)
			}
			files[path] = data
		}
		if err = writer.Write([]string{item.Slot, strconv.Itoa(item.Seq), item.Variant, item.TemplateKey, strconv.Itoa(item.TemplateVersion), item.Route, item.Human, strconv.Itoa(item.ReviewA.Score), strconv.Itoa(item.ReviewB.Score)}); err != nil {
			return nil, "", err
		}
	}
	if contains(set.Parts, "main") {
		white, err := a.whiteImage(product)
		if err != nil {
			return nil, "", err
		}
		data, err := jpegBytes(white)
		if err != nil {
			return nil, "", err
		}
		files["白底/800x800.jpg"] = data
	}
	writer.Flush()
	if err = writer.Error(); err != nil {
		return nil, "", err
	}
	files["复核记录.csv"] = record.Bytes()
	return files, fmt.Sprintf("AI出图-%s-v%d.zip", safeName(product.Name), set.Version), nil
}
func (a *Module) exportPost(w http.ResponseWriter, r *http.Request) {
	set, ok := a.setScope(w, r)
	if !ok {
		return
	}
	uid := core.CurrentUser(r).ID
	if !core.CurrentUser(r).Has(core.ExportRun) {
		core.Fail(w, 403, "没有导出权限")
		return
	}
	if a.setBusy(set.ID) {
		core.Fail(w, 409, "套装正在处理中")
		return
	}
	items, err := a.currentItems(r.Context(), set.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if missing := exportMissing(set, items, a.settings()); len(missing) > 0 {
		core.WriteJSON(w, 409, core.Object{"error": strings.Join(missing, "；"), "missing": missing})
		return
	}
	files, zipName, err := a.makeExport(r.Context(), set, items)
	if err != nil {
		fail(w, err)
		return
	}
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		entry, err := zw.Create(path)
		if err != nil {
			fail(w, err)
			return
		}
		if _, err = entry.Write(files[path]); err != nil {
			fail(w, err)
			return
		}
	}
	if err = zw.Close(); err != nil {
		fail(w, err)
		return
	}
	if _, err = a.authorize(r.Context(), uid, set.ListingID, core.CreativeRun); err != nil {
		fail(w, err)
		return
	}
	rootPath := a.ListingDir(set.ListingID)
	if err = os.MkdirAll(filepath.Join(rootPath, "AI出图"), 0700); err != nil {
		fail(w, err)
		return
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		fail(w, err)
		return
	}
	defer root.Close()
	stage := fmt.Sprintf("AI出图/.export-%d-%s", set.ID, strings.ReplaceAll(core.Now(), ":", ""))
	if err = root.MkdirAll(stage, 0700); err != nil {
		fail(w, err)
		return
	}
	defer root.RemoveAll(stage)
	for path, data := range files {
		if err = root.MkdirAll(stage+"/"+filepath.ToSlash(filepath.Dir(path)), 0700); err != nil {
			fail(w, err)
			return
		}
		if err = root.WriteFile(stage+"/"+path, data, 0600); err != nil {
			fail(w, err)
			return
		}
	}
	if err = root.WriteFile(stage+"/"+zipName, zipped.Bytes(), 0600); err != nil {
		fail(w, err)
		return
	}
	current, err := a.set(r.Context(), set.ID)
	if err != nil {
		fail(w, err)
		return
	}
	currentItems, err := a.currentItems(r.Context(), set.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if core.JS(currentItems) != core.JS(items) || current.GlancedAt != set.GlancedAt || a.setBusy(set.ID) {
		core.Fail(w, 409, "复核状态已变化，请重新导出")
		return
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	if err = authorizeTx(r.Context(), tx, uid, set.ListingID, core.CreativeRun); err != nil {
		fail(w, err)
		return
	}
	if err = authorizeTx(r.Context(), tx, uid, set.ListingID, core.ExportRun); err != nil {
		fail(w, err)
		return
	}
	rows, err := objects(r.Context(), tx, "SELECT * FROM creative_items WHERE set_id=? AND superseded=0 ORDER BY slot,seq,variant", set.ID)
	if err != nil {
		fail(w, err)
		return
	}
	latestItems, err := typed[[]Item](rows)
	if err != nil {
		fail(w, err)
		return
	}
	for index := range latestItems {
		latestItems[index].Scene = sceneSummary(latestItems[index].SceneSnapshot)
	}
	var latestGlance string
	if err = tx.QueryRowContext(r.Context(), "SELECT glanced_at FROM creative_sets WHERE id=?", set.ID).Scan(&latestGlance); err != nil {
		fail(w, err)
		return
	}
	var active int
	if err = tx.QueryRowContext(r.Context(), `SELECT count(*) FROM jobs WHERE status IN ('queued','running') AND ((target_type='creative_set' AND target_id=?) OR (target_type='creative_item' AND target_id IN(SELECT id FROM creative_items WHERE set_id=?)))`, set.ID, set.ID).Scan(&active); err != nil {
		fail(w, err)
		return
	}
	if active > 0 || core.JS(latestItems) != core.JS(items) || latestGlance != set.GlancedAt {
		core.Fail(w, 409, "复核状态已变化，请重新导出")
		return
	}
	folder := fmt.Sprintf("AI出图/set-%d", set.Version)
	if err = root.RemoveAll(folder); err != nil {
		fail(w, err)
		return
	}
	if err = root.Rename(stage, folder); err != nil {
		fail(w, err)
		return
	}
	file := folder + "/" + zipName
	if _, err = tx.ExecContext(r.Context(), "UPDATE creative_sets SET status='exported',exported_at=?,export_file=?,updated_at=? WHERE id=?", time.Now().UTC().Format(exportTimeLayout), file, core.Now(), set.ID); err != nil {
		fail(w, err)
		return
	}
	if err = auditTx(r.Context(), tx, uid, "creative.export", "creative_set", set.ID, core.Object{"file": file}); err != nil {
		fail(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	core.WriteJSON(w, 200, core.Object{"file": fmt.Sprintf("/api/creative/sets/%d/export", set.ID)})
}
func (a *Module) exportGet(w http.ResponseWriter, r *http.Request) {
	set, ok := a.setScope(w, r)
	if !ok {
		return
	}
	if !core.CurrentUser(r).Has(core.ExportRun) {
		core.Fail(w, 403, "没有导出权限")
		return
	}
	obj, err := a.one(r.Context(), "SELECT export_file,status FROM creative_sets WHERE id=?", set.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if obj["status"] != "exported" || stringValue(obj["export_file"]) == "" {
		core.Fail(w, 409, "套装尚未导出或内容已变化")
		return
	}
	items, err := a.currentItems(r.Context(), set.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if missing := exportMissing(set, items, a.settings()); len(missing) > 0 {
		core.WriteJSON(w, 409, core.Object{"error": strings.Join(missing, "；"), "missing": missing})
		return
	}
	name := stringValue(obj["export_file"])
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(name)}))
	core.ServeRoot(w, r, a.ListingDir(set.ListingID), name)
}
