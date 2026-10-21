package creative

import (
	"context"
	"database/sql"
	"log"
	"time"
	"xstudio/internal/core"
)

func (a *Module) startReturnScan() { a.returnOnce.Do(a.returnScanner) }
func (a *Module) returnScanner() {
	done := make(chan struct{})
	a.OnClose(func() { <-done })
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-a.Context().Done():
				return
			case <-ticker.C:
				if err := a.scanReturns(a.Context()); err != nil {
					log.Printf("creative 售后扫描：%v", err)
				}
			}
		}
	}()
}
func (a *Module) scanReturns(ctx context.Context) error {
	columns, err := objects(ctx, a.DB, "PRAGMA table_info(order_return_lines)")
	if err != nil {
		return err
	}
	hasReason, hasSet := false, false
	for _, column := range columns {
		if column["name"] == "reason_code" {
			hasReason = true
		}
		if column["name"] == "creative_set_id" {
			hasSet = true
		}
	}
	if !hasReason || !hasSet {
		return nil
	}
	threshold := int(number(a.settings()["return_reason_threshold"]))
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var watermark int64
	if err = tx.QueryRowContext(ctx, "SELECT watermark FROM creative_return_scan WHERE id=1").Scan(&watermark); err != nil {
		return err
	}
	rows, err := objects(ctx, tx, "SELECT rowid AS scan_id,reason_code,creative_set_id FROM order_return_lines WHERE rowid>? ORDER BY rowid", watermark)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return tx.Commit()
	}
	for _, row := range rows {
		watermark = int64(number(row["scan_id"]))
		reason := stringValue(row["reason_code"])
		slots := []string{}
		switch reason {
		case "not_as_pictured", "color":
			slots = []string{"main1", "main2"}
		case "smaller":
			slots = []string{"main4"}
		case "flaw":
			slots = []string{"main3"}
		default:
			continue
		}
		setID := int64(number(row["creative_set_id"]))
		var listingID int64
		err = tx.QueryRowContext(ctx, "SELECT listing_id FROM creative_sets WHERE id=?", setID).Scan(&listingID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO creative_return_counts(listing_id,reason_code,count) VALUES(?,?,1) ON CONFLICT(listing_id,reason_code) DO UPDATE SET count=count+1`, listingID, reason); err != nil {
			return err
		}
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count FROM creative_return_counts WHERE listing_id=? AND reason_code=?", listingID, reason).Scan(&count); err != nil {
			return err
		}
		if count < threshold {
			continue
		}
		for _, slot := range slots {
			var itemID any
			var id int64
			if tx.QueryRowContext(ctx, "SELECT id FROM creative_items WHERE set_id=? AND slot=? AND superseded=0 ORDER BY id DESC LIMIT 1", setID, slot).Scan(&id) == nil {
				itemID = id
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO creative_suggestions(listing_id,set_id,item_id,slot,reason_code,count,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(listing_id,slot,reason_code) DO UPDATE SET count=excluded.count,set_id=excluded.set_id,item_id=excluded.item_id,updated_at=excluded.updated_at`, listingID, setID, itemID, slot, reason, count, core.Now(), core.Now()); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO creative_calibration(category,tightened,tightened_reason,updated_at) VALUES(?,1,?,?) ON CONFLICT(category) DO UPDATE SET tightened=1,tightened_reason=excluded.tightened_reason,updated_at=excluded.updated_at`, "tight:"+slot, reason, core.Now()); err != nil {
				return err
			}
			if err = auditTx(ctx, tx, 0, "creative.return_suggestion", "listing", listingID, core.Object{"reason_code": reason, "slot": slot, "count": count}); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE creative_return_scan SET watermark=? WHERE id=1", watermark); err != nil {
		return err
	}
	if err = auditTx(ctx, tx, 0, "creative.return_scan", "creative_return_scan", 1, core.Object{"watermark": watermark, "rows": len(rows)}); err != nil {
		return err
	}
	return tx.Commit()
}
