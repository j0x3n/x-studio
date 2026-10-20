package assets

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"xstudio/internal/core"
)

type assetFolder struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parent_id"`
	Name      string `json:"name"`
	Count     int    `json:"count"`
	CreatedBy int64  `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

func folderWriteAllowed(w http.ResponseWriter, r *http.Request) bool {
	if currentUser(r).Role.Scope == "shares" {
		fail(w, 403, "共享账号不能修改素材文件夹")
		return false
	}
	return true
}

func (a *Module) requireFolder(w http.ResponseWriter, r *http.Request, id *int64) bool {
	if id == nil {
		return true
	}
	if *id < 1 {
		fail(w, 400, "文件夹编号不正确")
		return false
	}
	var exists int
	err := a.DB.QueryRowContext(r.Context(), "SELECT 1 FROM asset_folders WHERE id=?", *id).Scan(&exists)
	if err == sql.ErrNoRows {
		fail(w, 404, "文件夹不存在")
	} else if err != nil {
		fail(w, 500, "读取文件夹失败")
	}
	return err == nil
}

func (a *Module) folders(w http.ResponseWriter, r *http.Request) {
	items := []assetFolder{}
	u := currentUser(r)
	if u.Role.Scope == "shares" {
		writeJSON(w, 200, Object{"items": items})
		return
	}
	scope, args := productScopeSQL(u)
	rows, err := a.DB.QueryContext(r.Context(), `SELECT f.id,f.parent_id,f.name,COALESCE(c.total,0),f.created_by,f.created_at
FROM asset_folders f LEFT JOIN (SELECT a.folder_id,count(*) total FROM assets a JOIN products p ON p.id=a.product_id
WHERE a.archived=0 AND `+scope+` GROUP BY a.folder_id) c ON c.folder_id=f.id ORDER BY f.name,f.id`, args...)
	if err != nil {
		fail(w, 500, "读取文件夹失败")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var item assetFolder
		if err = rows.Scan(&item.ID, &item.ParentID, &item.Name, &item.Count, &item.CreatedBy, &item.CreatedAt); err != nil {
			fail(w, 500, "读取文件夹失败")
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		fail(w, 500, "读取文件夹失败")
		return
	}
	writeJSON(w, 200, Object{"items": items})
}

func folderInput(w http.ResponseWriter, r *http.Request, folder *assetFolder, create bool) bool {
	var fields map[string]json.RawMessage
	if !decode(w, r, &fields) {
		return false
	}
	for key, value := range fields {
		switch key {
		case "name":
			if string(value) == "null" || json.Unmarshal(value, &folder.Name) != nil {
				fail(w, 400, "文件夹名称不正确")
				return false
			}
		case "parent_id":
			if json.Unmarshal(value, &folder.ParentID) != nil || (folder.ParentID != nil && *folder.ParentID < 1) {
				fail(w, 400, "父文件夹编号不正确")
				return false
			}
		default:
			fail(w, 400, "不支持的文件夹字段")
			return false
		}
	}
	folder.Name = strings.TrimSpace(folder.Name)
	if (create && fields["name"] == nil) || len([]rune(folder.Name)) < 1 || len([]rune(folder.Name)) > 30 || strings.ContainsAny(folder.Name, `/\`) {
		fail(w, 400, "文件夹名称须为 1–30 个字符，不能包含斜杠")
		return false
	}
	return true
}

func readFolders(tx *sql.Tx) (map[int64]assetFolder, error) {
	rows, err := tx.Query("SELECT id,parent_id,name,created_by,created_at FROM asset_folders")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	folders := map[int64]assetFolder{}
	for rows.Next() {
		var folder assetFolder
		if err = rows.Scan(&folder.ID, &folder.ParentID, &folder.Name, &folder.CreatedBy, &folder.CreatedAt); err != nil {
			return nil, err
		}
		folders[folder.ID] = folder
	}
	return folders, rows.Err()
}

func validFolderPlacement(w http.ResponseWriter, folders map[int64]assetFolder, folder assetFolder) bool {
	depth := 1
	for parent := folder.ParentID; parent != nil; {
		if *parent == folder.ID {
			fail(w, 400, "不能移动到自己或子孙文件夹下")
			return false
		}
		ancestor, ok := folders[*parent]
		if !ok {
			fail(w, 404, "父文件夹不存在")
			return false
		}
		depth++
		if depth > 5 {
			fail(w, 400, "文件夹最多 5 层")
			return false
		}
		parent = ancestor.ParentID
	}
	children := map[int64][]int64{}
	for _, item := range folders {
		if item.ParentID != nil {
			children[*item.ParentID] = append(children[*item.ParentID], item.ID)
		}
	}
	level := []int64{folder.ID}
	for len(level) > 0 {
		if depth > 5 {
			fail(w, 400, "移动后的子孙文件夹超过 5 层")
			return false
		}
		next := []int64{}
		for _, id := range level {
			next = append(next, children[id]...)
		}
		level = next
		depth++
	}
	for _, sibling := range folders {
		sameParent := sibling.ParentID == nil && folder.ParentID == nil || sibling.ParentID != nil && folder.ParentID != nil && *sibling.ParentID == *folder.ParentID
		if sibling.ID != folder.ID && sameParent && sibling.Name == folder.Name {
			fail(w, 409, "同级文件夹名称已存在")
			return false
		}
	}
	return true
}

func canEditFolder(w http.ResponseWriter, r *http.Request, folder assetFolder) bool {
	u := currentUser(r)
	if u.ID != folder.CreatedBy && !u.Has(core.AssetDeleteAny) && u.Role.Key != "admin" {
		fail(w, 403, "只能修改自己创建的文件夹")
		return false
	}
	return true
}

func (a *Module) createFolder(w http.ResponseWriter, r *http.Request) {
	if !folderWriteAllowed(w, r) {
		return
	}
	folder := assetFolder{CreatedBy: currentUser(r).ID, CreatedAt: now()}
	if !folderInput(w, r, &folder, true) {
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "创建文件夹失败")
		return
	}
	defer tx.Rollback()
	folders, err := readFolders(tx)
	if err != nil {
		fail(w, 500, "读取文件夹失败")
		return
	}
	if !validFolderPlacement(w, folders, folder) {
		return
	}
	res, err := tx.Exec("INSERT INTO asset_folders(parent_id,name,created_by,created_at,updated_at) VALUES(?,?,?,?,?)", folder.ParentID, folder.Name, folder.CreatedBy, folder.CreatedAt, folder.CreatedAt)
	if err != nil {
		fail(w, 500, "创建文件夹失败")
		return
	}
	folder.ID, _ = res.LastInsertId()
	if err = tx.Commit(); err != nil {
		fail(w, 500, "创建文件夹失败")
		return
	}
	a.Audit(r, "assets.folder.create", "asset_folder", folder.ID, Object{"name": folder.Name, "parent_id": folder.ParentID})
	writeJSON(w, 201, Object{"id": folder.ID})
}

func (a *Module) patchFolder(w http.ResponseWriter, r *http.Request) {
	if !folderWriteAllowed(w, r) {
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "更新文件夹失败")
		return
	}
	defer tx.Rollback()
	folders, err := readFolders(tx)
	if err != nil {
		fail(w, 500, "读取文件夹失败")
		return
	}
	folder, ok := folders[idParam(r, "id")]
	if !ok {
		fail(w, 404, "文件夹不存在")
		return
	}
	if !canEditFolder(w, r, folder) || !folderInput(w, r, &folder, false) || !validFolderPlacement(w, folders, folder) {
		return
	}
	if _, err = tx.Exec("UPDATE asset_folders SET name=?,parent_id=?,updated_at=? WHERE id=?", folder.Name, folder.ParentID, now(), folder.ID); err != nil {
		fail(w, 500, "更新文件夹失败")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "更新文件夹失败")
		return
	}
	a.Audit(r, "assets.folder.update", "asset_folder", folder.ID, Object{"name": folder.Name, "parent_id": folder.ParentID})
	writeJSON(w, 200, Object{})
}

func (a *Module) deleteFolder(w http.ResponseWriter, r *http.Request) {
	if !folderWriteAllowed(w, r) {
		return
	}
	a.Mu.Lock()
	defer a.Mu.Unlock()
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "删除文件夹失败")
		return
	}
	defer tx.Rollback()
	var folder assetFolder
	err = tx.QueryRow("SELECT id,created_by FROM asset_folders WHERE id=?", idParam(r, "id")).Scan(&folder.ID, &folder.CreatedBy)
	if err == sql.ErrNoRows {
		fail(w, 404, "文件夹不存在")
		return
	}
	if err != nil {
		fail(w, 500, "读取文件夹失败")
		return
	}
	if !canEditFolder(w, r, folder) {
		return
	}
	res, err := tx.Exec(`WITH RECURSIVE subtree(id) AS (
SELECT id FROM asset_folders WHERE id=? UNION ALL SELECT f.id FROM asset_folders f JOIN subtree s ON f.parent_id=s.id)
UPDATE assets SET folder_id=NULL,updated_at=? WHERE folder_id IN (SELECT id FROM subtree)`, folder.ID, now())
	if err != nil {
		fail(w, 500, "清空素材文件夹失败")
		return
	}
	unfiled, err := res.RowsAffected()
	if err != nil {
		fail(w, 500, "读取未归类数量失败")
		return
	}
	if _, err = tx.Exec("DELETE FROM asset_folders WHERE id=?", folder.ID); err != nil {
		fail(w, 500, "删除文件夹失败")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "删除文件夹失败")
		return
	}
	a.Audit(r, "assets.folder.delete", "asset_folder", folder.ID, Object{"unfiled": unfiled})
	writeJSON(w, 200, Object{"unfiled": unfiled})
}
