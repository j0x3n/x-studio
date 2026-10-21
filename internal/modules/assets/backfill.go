package assets

import (
	"context"
	"fmt"
	"log"
	"time"
	"xstudio/internal/imaging"
)

func (a *Module) startHashBackfill() {
	a.backfillOnce.Do(func() {
		done := make(chan struct{})
		a.OnClose(func() { <-done })
		go func() {
			defer close(done)
			ctx := a.Context()
			delay := time.Duration(0)
			for {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				count, err := a.backfillHashBatch(ctx)
				if ctx.Err() != nil {
					return
				}
				delay = time.Second
				if err != nil {
					log.Printf("素材感知哈希后台补算失败：%v", err)
				} else if count == 50 {
					delay = 25 * time.Millisecond
				}
			}
		}()
	})
}

func (a *Module) calculateHash(p Asset) (string, error) {
	im, ex, err := a.load(p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%016x", imaging.Hash(imaging.Geometry(im, ex.Orientation, 0, nil))), nil
}

func (a *Module) backfillHashBatch(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	a.Mu.Lock()
	items, err := a.readAssets("SELECT " + assetColumns + " FROM assets WHERE phash='' AND media='image' ORDER BY id LIMIT 50")
	a.Mu.Unlock()
	if err != nil {
		return 0, fmt.Errorf("读取待补算素材失败")
	}
	for _, p := range items {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		hash, readErr := a.calculateHash(p)
		if readErr != nil {
			hash = "-"
		}
		a.Mu.Lock()
		res, err := a.DB.ExecContext(ctx, "UPDATE assets SET phash=? WHERE id=? AND product_id=? AND file=? AND phash=''", hash, p.ID, p.ProductID, p.File)
		a.Mu.Unlock()
		if err != nil {
			return 0, fmt.Errorf("保存素材感知哈希失败")
		}
		if n, _ := res.RowsAffected(); readErr != nil && n > 0 {
			log.Printf("素材 %d 感知哈希补算失败，已跳过：%v", p.ID, readErr)
		}
	}
	return len(items), nil
}
