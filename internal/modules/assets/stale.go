package assets

import (
	"context"
	"database/sql"
)

// MarkStale 标记 AI 成品是不是旧版本：被重画、改字替换掉的，或者人工弃用的，stale=1，素材库默认不显示。
// 由 creative 模块在同一个事务里调用；只改 kind='output' 的图，实拍不受影响。
func MarkStale(ctx context.Context, tx *sql.Tx, ids []int64, stale bool) error {
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, "UPDATE assets SET stale=? WHERE id=? AND kind='output'", stale, id); err != nil {
			return err
		}
	}
	return nil
}
