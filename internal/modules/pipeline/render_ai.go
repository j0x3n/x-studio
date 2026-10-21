package pipeline

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"xstudio/internal/core"
	"xstudio/internal/modules/creative"
)

func (a *Module) render(ctx context.Context, p Product) ([]string, error) {
	mode, err := a.listingRenderMode(p.ListingID)
	if err != nil {
		return nil, err
	}
	if mode != "html" {
		return a.renderAI(ctx, p)
	}
	if err = a.renderHTML(ctx, p); err != nil {
		return nil, err
	}
	return nil, nil
}

func (a *Module) listingRenderMode(listingID int64) (string, error) {
	var mode string
	err := a.DB.QueryRow("SELECT coalesce(mode, 'ai') FROM listing_render WHERE listing_id=?", listingID).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return "ai", nil
	}
	if err != nil {
		return "", err
	}
	if mode != "html" {
		return "ai", nil
	}
	return "html", nil
}

func (a *Module) renderAI(ctx context.Context, p Product) ([]string, error) {
	result, err := creative.StartSet(ctx, a.Core, p.ListingID, core.Actor(ctx).UserID, []string{"main", "detail"}, creative.Params{SkipMissing: true, HeroStyle: "vertical"})
	if err != nil {
		return nil, err
	}
	note := fmt.Sprintf("已转 AI 出图：套装 #%d（任务 #%d）", result.SetID, result.JobID)
	if len(result.Missing) > 0 {
		seen := map[string]bool{}
		names := []string{}
		for _, item := range result.Missing {
			name := item.Label
			if name == "" {
				name = item.Slot
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
		note += fmt.Sprintf("；缺素材跳过 %d 张：%s", len(result.Missing), strings.Join(names, "、"))
	}
	jobID := core.Actor(ctx).JobID
	ts := now()
	if _, err = a.DB.Exec(`INSERT OR IGNORE INTO listing_render(listing_id, updated_at) VALUES(?, ?)`, p.ListingID, ts); err != nil {
		return nil, err
	}
	if _, err = a.DB.Exec(`
		UPDATE listing_render
		SET rendered_job_id=?, rendered_template=?, render_note=?, review_status=?, updated_at=?
		WHERE listing_id=?
	`, jobID, "ai", note, "none", ts, p.ListingID); err != nil {
		return nil, err
	}
	return []string{note}, nil
}
