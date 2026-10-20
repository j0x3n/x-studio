package assets_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"xstudio/internal/core"
	"xstudio/internal/modules/assets"
)

func folder(t *testing.T, f *fixture, name string, parent any) int64 {
	t.Helper()
	out := f.req(t, "POST", "/api/asset-folders", obj{"name": name, "parent_id": parent}, 201)
	return int64(out["id"].(float64))
}

func folderPath(folderID int64) string {
	return fmt.Sprintf("/api/asset-folders/%d", folderID)
}

func folderUpload(t *testing.T, f *fixture, productID int64, query string, status int) obj {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "文件夹上传.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(imageBytes(t, 24, 32)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/products/%d/assets%s", f.s.URL, productID, query), &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out obj
	if err = json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("上传状态 %d，期望 %d：%v", res.StatusCode, status, out)
	}
	return out
}

func assertAssetFolder(t *testing.T, f *fixture, assetID int64, expected any) {
	t.Helper()
	var actual sql.NullInt64
	if err := f.c.DB.QueryRow("SELECT folder_id FROM assets WHERE id=?", assetID).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if expected == nil {
		if actual.Valid {
			t.Fatal("素材没有移出文件夹", actual)
		}
	} else if !actual.Valid || actual.Int64 != expected.(int64) {
		t.Fatal("素材文件夹不正确", actual, expected)
	}
}

func TestAssetFolderTreeValidationRenameAndMove(t *testing.T) {
	f := newFixture(t, nil)
	root := folder(t, f, "  素材  ", nil)
	f.req(t, "POST", "/api/asset-folders", obj{"name": "素材"}, 409)
	for _, name := range []string{"", "  ", "a/b", `a\b`, strings.Repeat("图", 31)} {
		f.req(t, "POST", "/api/asset-folders", obj{"name": name}, 400)
	}
	folder(t, f, strings.Repeat("图", 30), nil)
	f.req(t, "POST", "/api/asset-folders", obj{"name": "失效", "parent_id": 999999}, 404)
	for _, parent := range []any{0, -1, "1", 1.5} {
		f.req(t, "POST", "/api/asset-folders", obj{"name": "失效", "parent_id": parent}, 400)
	}
	child := folder(t, f, "子目录", root)
	grandchild := folder(t, f, "三级", child)
	fourth := folder(t, f, "四级", grandchild)
	fifth := folder(t, f, "五级", fourth)
	f.req(t, "POST", "/api/asset-folders", obj{"name": "六级", "parent_id": fifth}, 400)
	f.req(t, "POST", "/api/asset-folders", obj{"name": "子目录", "parent_id": root}, 409)
	other := folder(t, f, "另一根", nil)
	otherChild := folder(t, f, "子目录", other)
	f.req(t, "PATCH", folderPath(root), obj{"parent_id": root}, 400)
	f.req(t, "PATCH", folderPath(root), obj{"parent_id": fifth}, 400)
	f.req(t, "PATCH", folderPath(child), obj{"parent_id": otherChild}, 400)
	f.req(t, "PATCH", folderPath(root), obj{"parent_id": other}, 400)
	f.req(t, "PATCH", folderPath(child), obj{"parent_id": 999999}, 404)
	f.req(t, "PATCH", folderPath(child), obj{"parent_id": other}, 409)
	f.req(t, "PATCH", folderPath(otherChild), obj{"name": "另一个子目录"}, 200)
	f.req(t, "PATCH", folderPath(child), obj{"parent_id": other}, 200)
	f.req(t, "PATCH", folderPath(child), obj{"name": "  改名  "}, 200)
	var name string
	var parent int64
	if err := f.c.DB.QueryRow("SELECT name,parent_id FROM asset_folders WHERE id=?", child).Scan(&name, &parent); err != nil || name != "改名" || parent != other {
		t.Fatal("改名或省略父级的语义不正确", name, parent, err)
	}
	f.req(t, "PATCH", folderPath(child), obj{"parent_id": nil}, 200)
	f.req(t, "PATCH", folderPath(root), obj{"name": "改名"}, 409)
	f.req(t, "PATCH", folderPath(root), obj{"name": nil}, 400)
	f.req(t, "PATCH", "/api/asset-folders/999999", obj{"name": "不存在"}, 404)
	f.req(t, "DELETE", "/api/asset-folders/999999", nil, 404)
	listed := f.req(t, "GET", "/api/asset-folders?page_size=1", nil, 200)["items"].([]any)
	names := []string{}
	for _, raw := range listed {
		item := raw.(obj)
		names = append(names, item["name"].(string))
		if _, ok := item["parent_id"]; !ok || item["created_by"] == nil || item["created_at"] == "" || item["count"] != float64(0) {
			t.Fatal("文件夹返回字段不正确", item)
		}
		if id(item) == child && item["parent_id"] != nil {
			t.Fatal("未移到顶层", item)
		}
	}
	if len(names) != 8 || !sort.StringsAreSorted(names) {
		t.Fatal("未按名称平铺返回全部文件夹", names)
	}
	f.assertAudit(t, "assets.folder.create", "assets.folder.update")
}

func TestAssetFolderScopeCountsFiltersAndBatch(t *testing.T) {
	f := newFixture(t, nil)
	root := folder(t, f, "团队目录", nil)
	child := folder(t, f, "下级", root)
	visibleProduct := f.product(t, "可见商品", 1)
	hiddenProduct := f.product(t, "隐藏商品")
	visible := f.upload(t, visibleProduct, imageBytes(t, 20, 20))
	archived := f.upload(t, visibleProduct, imageBytes(t, 20, 20))
	hidden := f.upload(t, hiddenProduct, imageBytes(t, 20, 20))
	childAsset := f.upload(t, visibleProduct, imageBytes(t, 20, 20))
	wbExec(t, f, "UPDATE assets SET folder_id=? WHERE id IN (?,?,?)", root, visible, archived, hidden)
	wbExec(t, f, "UPDATE assets SET archived=1 WHERE id=?", archived)
	wbExec(t, f, "UPDATE assets SET folder_id=? WHERE id=?", child, childAsset)
	f.user(t, "operator", 2, 1)
	f.login(t, "operator")
	ownedProduct := f.product(t, "本人创建但无店铺")
	owned := f.upload(t, ownedProduct, imageBytes(t, 20, 20))
	for _, raw := range f.req(t, "GET", "/api/asset-folders", nil, 200)["items"].([]any) {
		if raw.(obj)["count"] != float64(1) {
			t.Fatal("计数包含隐藏、归档或下级素材", raw)
		}
	}
	listed := f.req(t, "GET", fmt.Sprintf("/api/assets?folder_id=%d", root), nil, 200)
	if listed["total"] != float64(1) || id(listed["items"].([]any)[0]) != visible || listed["items"].([]any)[0].(obj)["folder_id"] != float64(root) {
		t.Fatal("直接文件夹筛选不正确", listed)
	}
	unfiled := f.req(t, "GET", "/api/assets?folder_id=0", nil, 200)
	if unfiled["total"] != float64(1) || id(unfiled["items"].([]any)[0]) != owned || unfiled["items"].([]any)[0].(obj)["folder_id"] != nil {
		t.Fatal("未归类筛选不正确", unfiled)
	}
	if out := f.req(t, "GET", "/api/assets?folder_id=999999", nil, 200); out["total"] != float64(0) {
		t.Fatal("不存在的文件夹应为空", out)
	}
	for _, value := range []string{"-1", "bad", "1.5", ""} {
		f.req(t, "GET", "/api/assets?folder_id="+value, nil, 400)
	}
	f.req(t, "POST", "/api/assets/batch", obj{"action": "folder", "asset_ids": []int64{visible}, "folder_id": 999999}, 404)
	f.req(t, "POST", "/api/assets/batch", obj{"action": "folder", "asset_ids": []int64{visible}, "folder_id": 0}, 400)
	result := f.req(t, "POST", "/api/assets/batch", obj{"action": "folder", "asset_ids": []int64{visible, hidden, owned}, "folder_id": child}, 200)["items"].([]any)
	if result[0].(obj)["ok"] != true || result[1].(obj)["error"] == nil || result[2].(obj)["ok"] != true {
		t.Fatal("批量归类没有逐张检查数据范围", result)
	}
	assertAssetFolder(t, f, visible, child)
	assertAssetFolder(t, f, owned, child)
	assertAssetFolder(t, f, hidden, root)
	f.req(t, "POST", "/api/assets/batch", obj{"action": "folder", "asset_ids": []int64{visible}, "folder_id": nil}, 200)
	assertAssetFolder(t, f, visible, nil)
	f.assertAudit(t, "assets.batch.folder")
}

func TestAssetFolderUploadAndProcessInheritance(t *testing.T) {
	f := newFixture(t, nil)
	product := f.product(t, "文件夹上传", 1)
	root := folder(t, f, "原图", nil)
	out := folderUpload(t, f, product, fmt.Sprintf("?folder_id=%d", root), 201)
	item := out["items"].([]any)[0].(obj)
	assetID := id(item)
	if item["folder_id"] != float64(root) {
		t.Fatal("上传返回缺少文件夹", item)
	}
	assertAssetFolder(t, f, assetID, root)
	folderUpload(t, f, product, "?folder_id=999999", 404)
	for _, value := range []string{"0", "-1", "bad", ""} {
		folderUpload(t, f, product, "?folder_id="+value, 400)
	}
	generated := newID(t, processJob(t, f, []int64{assetID}, obj{"format": "png"}, false))
	assertAssetFolder(t, f, generated, root)
	detail := f.req(t, "GET", fmt.Sprintf("/api/assets/%d", generated), nil, 200)
	if detail["asset"].(obj)["folder_id"] != float64(root) {
		t.Fatal("详情缺少文件夹", detail)
	}
	for _, version := range detail["versions"].([]any) {
		if version.(obj)["folder_id"] != float64(root) {
			t.Fatal("版本链缺少文件夹", version)
		}
	}
	for _, raw := range f.req(t, "GET", fmt.Sprintf("/api/products/%d/assets", product), nil, 200)["items"].([]any) {
		if raw.(obj)["folder_id"] != float64(root) {
			t.Fatal("商品素材列表缺少文件夹", raw)
		}
	}
	f.req(t, "DELETE", folderPath(root), nil, 200)
	generated = newID(t, processJob(t, f, []int64{assetID}, obj{"format": "png"}, false))
	assertAssetFolder(t, f, generated, nil)
}

func TestAssetFolderDeleteSubtreeAtomicAndKeepsFiles(t *testing.T) {
	f := newFixture(t, nil)
	root := folder(t, f, "删除整树", nil)
	child := folder(t, f, "子级", root)
	leaf := folder(t, f, "孙级", child)
	keep := folder(t, f, "保留", nil)
	product := f.product(t, "素材保留", 1)
	imageData := imageBytes(t, 24, 32)
	ids := []int64{}
	files := []string{}
	for _, target := range []int64{root, child, leaf, keep} {
		assetID := f.upload(t, product, imageData)
		ids = append(ids, assetID)
		wbExec(t, f, "UPDATE assets SET folder_id=? WHERE id=?", target, assetID)
		var file string
		if err := f.c.DB.QueryRow("SELECT file FROM assets WHERE id=?", assetID).Scan(&file); err != nil {
			t.Fatal(err)
		}
		files = append(files, filepath.Join(f.c.Dir(product), filepath.FromSlash(file)))
	}
	wbExec(t, f, "UPDATE assets SET archived=1 WHERE id=?", ids[1])
	wbExec(t, f, "CREATE TRIGGER folder_delete_failure BEFORE DELETE ON asset_folders BEGIN SELECT RAISE(ABORT,'test rollback'); END")
	f.req(t, "DELETE", folderPath(root), nil, 500)
	for index, target := range []int64{root, child, leaf, keep} {
		assertAssetFolder(t, f, ids[index], target)
	}
	wbExec(t, f, "DROP TRIGGER folder_delete_failure")
	if out := f.req(t, "DELETE", folderPath(root), nil, 200); out["unfiled"] != float64(3) {
		t.Fatal("删除子树的未归类数量不正确", out)
	}
	for _, assetID := range ids[:3] {
		assertAssetFolder(t, f, assetID, nil)
	}
	assertAssetFolder(t, f, ids[3], keep)
	for _, file := range files {
		actual, err := os.ReadFile(file)
		if err != nil || !bytes.Equal(actual, imageData) {
			t.Fatal("删除文件夹改变了图片文件", file, err)
		}
	}
	listed := f.req(t, "GET", "/api/asset-folders", nil, 200)["items"].([]any)
	if len(listed) != 1 || id(listed[0]) != keep {
		t.Fatal("子孙文件夹未全部删除", listed)
	}
	f.assertAudit(t, "assets.folder.delete")
}

func TestAssetFolderSharesAndOwnershipPermissions(t *testing.T) {
	f := newFixture(t, nil)
	root := folder(t, f, "管理员创建", nil)
	product := f.product(t, "分享商品", 1)
	assetID := f.upload(t, product, imageBytes(t, 20, 20))
	f.user(t, "photographer", 4, 1)
	f.user(t, "manager", 3, 1)
	guest := f.user(t, "guest", 5)
	f.user(t, "warehouse", 6, 1)
	f.req(t, "POST", fmt.Sprintf("/api/products/%d/shares", product), obj{"user_id": guest, "can_upload": true}, 201)
	f.login(t, "photographer")
	f.req(t, "PATCH", folderPath(root), obj{"name": "越权改名"}, 403)
	f.req(t, "PATCH", folderPath(root), obj{"parent_id": nil}, 403)
	f.req(t, "DELETE", folderPath(root), nil, 403)
	own := folder(t, f, "摄影创建", nil)
	f.req(t, "PATCH", folderPath(own), obj{"name": "本人改名", "parent_id": root}, 200)
	f.login(t, "admin")
	f.req(t, "PATCH", folderPath(own), obj{"name": "管理员改名"}, 200)
	wbExec(t, f, "UPDATE roles SET perms=? WHERE id=3", core.JS([]string{core.ProductView, core.AssetUpload, core.AssetDeleteAny}))
	f.login(t, "manager")
	f.req(t, "PATCH", folderPath(root), obj{"name": "全素材管理员改名"}, 200)
	f.req(t, "PATCH", folderPath(own), obj{"parent_id": nil}, 200)
	f.req(t, "DELETE", folderPath(own), nil, 200)
	f.login(t, "photographer")
	own = folder(t, f, "本人删除", nil)
	f.req(t, "DELETE", folderPath(own), nil, 200)
	f.login(t, "guest")
	if out := f.req(t, "GET", "/api/asset-folders", nil, 200); len(out["items"].([]any)) != 0 {
		t.Fatal("共享账号看到了团队文件夹", out)
	}
	f.req(t, "POST", "/api/asset-folders", obj{"name": "共享创建"}, 403)
	for _, folderID := range []int64{root, 999999} {
		f.req(t, "PATCH", folderPath(folderID), obj{"name": "共享改名"}, 403)
		f.req(t, "DELETE", folderPath(folderID), nil, 403)
		f.req(t, "POST", "/api/assets/batch", obj{"action": "folder", "asset_ids": []int64{assetID}, "folder_id": folderID}, 403)
		folderUpload(t, f, product, fmt.Sprintf("?folder_id=%d", folderID), 403)
	}
	f.req(t, "POST", "/api/assets/batch", obj{"action": "folder", "asset_ids": []int64{assetID}, "folder_id": nil}, 403)
	f.login(t, "warehouse")
	f.req(t, "GET", "/api/asset-folders", nil, 200)
	f.req(t, "POST", "/api/asset-folders", obj{"name": "无上传权限"}, 403)
	f.req(t, "POST", "/api/assets/batch", obj{"action": "folder", "asset_ids": []int64{assetID}, "folder_id": root}, 403)
}

func TestAssetFolderMigrationPreservesExistingAssets(t *testing.T) {
	f := newFixture(t, nil)
	product := f.product(t, "迁移前素材", 1)
	assetID := f.upload(t, product, imageBytes(t, 20, 20))
	wbExec(t, f, "DROP INDEX assets_folder_id")
	wbExec(t, f, "ALTER TABLE assets DROP COLUMN folder_id")
	wbExec(t, f, "DROP TABLE asset_folders")
	tx, err := f.c.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, migration := range assets.New(f.c).Migrations() {
		if migration.Version == 6 {
			if err = migration.Up(tx); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertAssetFolder(t, f, assetID, nil)
	folder(t, f, "唯一根级名称", nil)
	if _, err = f.c.DB.Exec("INSERT INTO asset_folders(name,created_by,created_at,updated_at) VALUES('唯一根级名称',1,?,?)", core.Now(), core.Now()); err == nil {
		t.Fatal("根级重名缺少数据库唯一约束")
	}
}
