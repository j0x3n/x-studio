package creative

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/scenes"
)

type SceneSummary struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func sceneSummary(snapshot scenes.Snapshot) *SceneSummary {
	if snapshot.ID == 0 {
		return nil
	}
	return &SceneSummary{ID: snapshot.ID, Name: snapshot.Name}
}
func (module *Module) setScene(ctx context.Context, id int64) (scenes.Snapshot, error) {
	var raw string
	err := module.DB.QueryRowContext(ctx, "SELECT scene_snapshot FROM creative_items WHERE set_id=? ORDER BY id LIMIT 1", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return scenes.Snapshot{}, nil
	}
	if err != nil {
		return scenes.Snapshot{}, err
	}
	return typed[scenes.Snapshot](core.JSONObj(raw))
}
func scenePrompts(snapshot scenes.Snapshot) (string, string) {
	// 每段补上句末标点再拼：中文段用「。」紧接，英文段用「. 」，内置场景是英文、用户写的可能是中文
	join := func(parts ...string) string {
		var out strings.Builder
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			last, _ := utf8.DecodeLastRuneInString(part)
			if !strings.ContainsRune(".!?。！？", last) {
				if last < utf8.RuneSelf {
					part += "."
				} else {
					part += "。"
				}
			}
			out.WriteString(part)
			if last, _ = utf8.DecodeLastRuneInString(part); last < utf8.RuneSelf {
				out.WriteString(" ")
			}
		}
		return strings.TrimSpace(out.String())
	}
	return join(snapshot.Still, snapshot.Light, snapshot.Props), join(snapshot.Wear, snapshot.Light)
}
func sceneStylePaths(snapshot scenes.Snapshot, mode string) []string {
	paths := []string{}
	if mode == "photo" {
		return paths
	}
	use := "still"
	if mode == "model" {
		use = "wear"
	}
	for _, ref := range snapshot.Refs {
		if ref.Use == use {
			paths = append(paths, ref.Path)
		}
	}
	return paths
}
func photoCodeNames(category *core.Category, codes []string) string {
	labels := make([]string, 0, len(codes))
	for _, code := range codes {
		label := code
		if category != nil {
			for _, entry := range category.Codes {
				if entry.Name != "" && (code == entry.Code || strings.HasPrefix(code, entry.Code+"_")) {
					label = entry.Name + "（" + code + "）"
					break
				}
			}
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, "、")
}
func aboutPrompt(prompt string, category *core.Category, about string) string {
	if category != nil {
		lines := strings.Split(category.Rules, "\n")
		out := []string{}
		skip := false
		for _, line := range lines {
			if strings.HasPrefix(line, "### ") {
				skip = strings.Contains(line, "about") || strings.Contains(line, "关于我们")
			}
			if !skip {
				out = append(out, line)
			}
		}
		if category.Rules != "" {
			prompt = strings.ReplaceAll(prompt, category.Rules, strings.Join(out, "\n"))
		}
	}
	return prompt + "\n关于我们文案以店铺设置为准，逐字使用以下内容：\n" + about
}
func (module *Module) backfillStale() error {
	ctx := context.Background()
	tx, err := module.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var done int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM settings WHERE key='creative.stale_backfill'").Scan(&done); err != nil {
		return err
	}
	if done > 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT DISTINCT output_asset_id FROM creative_items WHERE output_asset_id<>0 AND (superseded=1 OR human IN ('discard','reject'))")
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if err = assets.MarkStale(ctx, tx, ids, true); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO settings(key,value,updated_at) VALUES('creative.stale_backfill','true',?)", core.Now()); err != nil {
		return err
	}
	if err = auditTx(ctx, tx, 0, "creative.stale_backfill", "creative", 0, core.Object{"count": len(ids)}); err != nil {
		return err
	}
	return tx.Commit()
}
func (module *Module) setDelete(writer http.ResponseWriter, request *http.Request) {
	set, ok := module.setScope(writer, request)
	if !ok {
		return
	}
	if module.setBusy(set.ID) {
		core.Fail(writer, 409, "套装正在处理中，请等任务完成再删除")
		return
	}
	tx, err := module.DB.BeginTx(request.Context(), nil)
	if err != nil {
		fail(writer, err)
		return
	}
	defer tx.Rollback()
	if err = authorizeTx(request.Context(), tx, core.CurrentUser(request).ID, set.ListingID, core.CreativeRun); err != nil {
		fail(writer, err)
		return
	}
	rows, err := tx.QueryContext(request.Context(), "SELECT output_asset_id FROM creative_items WHERE set_id=?", set.ID)
	if err != nil {
		fail(writer, err)
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		fail(writer, err)
		return
	}
	if err = assets.MarkStale(request.Context(), tx, ids, true); err != nil {
		fail(writer, err)
		return
	}
	if _, err = tx.ExecContext(request.Context(), "DELETE FROM creative_items WHERE set_id=?", set.ID); err == nil {
		_, err = tx.ExecContext(request.Context(), "DELETE FROM creative_sets WHERE id=?", set.ID)
	}
	if err == nil {
		err = core.AuditTx(tx, request, "creative.delete", "creative_set", set.ID, core.Object{"listing_id": set.ListingID})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(writer, err)
		return
	}
	core.WriteJSON(writer, 200, core.Object{"ok": true})
}
