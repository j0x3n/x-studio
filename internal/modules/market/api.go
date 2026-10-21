package market

import (
	"database/sql"
	"sort"
	"time"

	"xstudio/internal/core"
)

type Keyword struct {
	Word         string
	SearchVolume *int64
	CVRBps       *int64
	Score        *float64
	Competition  *int64
	Date         string
}

func keywordScore(volume, cvr *int64) *float64 {
	if volume == nil || cvr == nil {
		return nil
	}
	s := float64(*volume) * float64(*cvr) / 10000
	return &s
}

func nullInt(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func settingInt(c *core.Core, key string, def int) int {
	v, ok := c.SettingWithDefaults("market")[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return def
	}
}

func TopKeywords(c *core.Core, shopID int64, category string, n int) ([]Keyword, error) {
	out := []Keyword{}
	if c == nil || n <= 0 || shopID < 1 {
		return out, nil
	}
	today := chinaToday()
	rows, err := c.DB.Query(`SELECT word, date, search_volume, cvr_bps, competition FROM market_keywords WHERE shop_id=? AND category=? AND date>=? AND date<=? ORDER BY date DESC, id DESC`, shopID, category, addDays(today, -29), today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var k Keyword
		var vol, cvr, comp sql.NullInt64
		if err = rows.Scan(&k.Word, &k.Date, &vol, &cvr, &comp); err != nil {
			return nil, err
		}
		if seen[k.Word] {
			continue
		}
		seen[k.Word] = true
		k.SearchVolume = nullInt(vol)
		k.CVRBps = nullInt(cvr)
		k.Competition = nullInt(comp)
		k.Score = keywordScore(k.SearchVolume, k.CVRBps)
		out = append(out, k)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Score == nil && b.Score == nil {
			return a.Word < b.Word
		}
		if a.Score == nil {
			return false
		}
		if b.Score == nil {
			return true
		}
		if *a.Score != *b.Score {
			return *a.Score > *b.Score
		}
		return a.Word < b.Word
	})
	if n > len(out) {
		n = len(out)
	}
	return out[:n], nil
}

func TitleChangeAllowed(c *core.Core, listingID int64) (bool, time.Time, error) {
	var n int
	if err := c.DB.QueryRow("SELECT count(*) FROM listings WHERE id=?", listingID).Scan(&n); err != nil {
		return false, time.Time{}, err
	}
	if n == 0 {
		return false, time.Time{}, sql.ErrNoRows
	}
	days := settingInt(c, "title_change_days", 7)
	if days < 0 {
		days = 0
	}
	var last string
	err := c.DB.QueryRow("SELECT created_at FROM listing_title_history WHERE listing_id=? ORDER BY created_at DESC, id DESC LIMIT 1", listingID).Scan(&last)
	if err == sql.ErrNoRows {
		return true, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, last)
	if err != nil {
		t, err = time.Parse(time.RFC3339, last)
	}
	if err != nil {
		return false, time.Time{}, err
	}
	next := t.Add(time.Duration(days) * 24 * time.Hour)
	return !time.Now().Before(next), next, nil
}

func RecordTitleChange(tx *sql.Tx, listingID int64, oldTitle, newTitle, reason string, userID int64) error {
	if tx == nil || listingID < 1 {
		return sql.ErrNoRows
	}
	_, err := tx.Exec("INSERT INTO listing_title_history(listing_id, old_title, new_title, reason, created_by, created_at) VALUES(?,?,?,?,?,?)", listingID, oldTitle, newTitle, reason, userID, core.Now())
	return err
}
