package finance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxMoney int64 = 100_000_000_000_000
	maxRate  int64 = 1_000_000_000_000_000
	cnyRate  int64 = 1_000_000
)

var (
	china       = time.FixedZone("CST", 8*3600)
	errOverflow = errors.New("overflow")
)

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func validCurrency(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func validKind(s string) bool {
	switch s {
	case "bank", "alipay", "shopee", "cash":
		return true
	default:
		return false
	}
}

func validKey(s string) bool {
	if s == "" || runeLen(s) > 80 {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validText(s string, n int) bool { return runeLen(s) <= n }

func validMoney(n int64, allowZero bool) bool {
	if n < 0 || n > maxMoney {
		return false
	}
	return allowZero || n > 0
}

func addNonNeg(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, errOverflow
	}
	return a + b, nil
}

func addSigned(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, errOverflow
	}
	return a + b, nil
}

func neg(n int64) (int64, error) {
	if n == math.MinInt64 {
		return 0, errOverflow
	}
	return -n, nil
}

func ratio(a, b, d int64) (int64, error) {
	if a < 0 || b < 0 || d <= 0 {
		return 0, errOverflow
	}
	n := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	n.Add(n, big.NewInt(d/2))
	n.Quo(n, big.NewInt(d))
	if !n.IsInt64() {
		return 0, errOverflow
	}
	return n.Int64(), nil
}

func toCNY(amount, rateMicros int64) (int64, error) {
	if rateMicros <= 0 {
		return 0, errOverflow
	}
	sign := int64(1)
	if amount < 0 {
		v, err := neg(amount)
		if err != nil {
			return 0, err
		}
		amount = v
		sign = -1
	}
	v, err := ratio(amount, rateMicros, cnyRate)
	if err != nil {
		return 0, err
	}
	if sign < 0 {
		return neg(v)
	}
	return v, nil
}

func hashParts(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func stamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func parseTime(s string) (time.Time, error) {
	if len(s) == 10 {
		return time.ParseInLocation("2006-01-02", s, china)
	}
	return time.Parse(time.RFC3339Nano, s)
}

func normalizeTime(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return stamp(time.Now()), nil
	}
	t, err := parseTime(s)
	if err != nil {
		return "", err
	}
	return stamp(t), nil
}

func chinaDate(s string) (string, error) {
	t, err := parseTime(strings.TrimSpace(s))
	if err != nil {
		return "", err
	}
	return t.In(china).Format("2006-01-02"), nil
}

func monthRange(month string) (time.Time, time.Time, error) {
	start, err := time.ParseInLocation("2006-01", month, china)
	if err != nil || start.Format("2006-01") != month {
		return time.Time{}, time.Time{}, err
	}
	return start, start.AddDate(0, 1, 0), nil
}

func inSpan(s string, start, end time.Time) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	t, err := parseTime(s)
	if err != nil {
		return false
	}
	return !t.Before(start) && t.Before(end)
}

func dayStart(s string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", s, china)
	if err != nil || t.Format("2006-01-02") != s {
		return time.Time{}, err
	}
	return t, nil
}

func monthLikes(start time.Time) []string {
	return []string{
		start.AddDate(0, -1, 0).Format("2006-01") + "%",
		start.Format("2006-01") + "%",
		start.AddDate(0, 1, 0).Format("2006-01") + "%",
	}
}

func likeSQL(column string) string {
	return "(" + column + " LIKE ? OR " + column + " LIKE ? OR " + column + " LIKE ?)"
}

func idList(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func uniq(ids []int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func chunks(ids []int64, n int) [][]int64 {
	if n < 1 {
		n = 100
	}
	var out [][]int64
	for len(ids) > 0 {
		if len(ids) < n {
			n = len(ids)
		}
		out = append(out, ids[:n])
		ids = ids[n:]
	}
	return out
}

func i64s(n int64) string { return strconv.FormatInt(n, 10) }
