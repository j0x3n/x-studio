package pipeline

import (
	"context"
	"fmt"
	"xstudio/internal/core"
)

func (a *Module) recordEnhancement(ctx context.Context, pid int64, parent Photo, file string, w, h int) error {
	var old int64
	if a.DB.QueryRow("SELECT id FROM assets WHERE parent_id=? AND source='ai_enhanced' AND file=? ORDER BY id DESC LIMIT 1", parent.ID, file).Scan(&old) == nil {
		return nil
	}
	tx, e := a.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("UPDATE assets SET enhanced_file=?,ai_enhanced=1,retouch_log=? WHERE id=?", file, js(parent.RetouchLog), parent.ID); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO assets(product_id,parent_id,file,orig_name,width,height,source,ai_enhanced,created_by,created_at,op) VALUES(?,?,?,?,?,?,'ai_enhanced',1,?,?,?)", pid, parent.ID, file, fmt.Sprintf("高清化_%d.png", parent.ID), w, h, core.Actor(ctx).UserID, now(), js(Object{"feature": "enhance", "crop": parent.Crop, "rotate": parent.Rotate})); e != nil {
		return e
	}
	return tx.Commit()
}
