package orders

import "time"

var chinaTime = time.FixedZone("CST", 8*60*60)

// Dates mean midnight in China; RFC3339 values retain their explicit offset.
func orderTime(s string) (time.Time, error) {
	if len(s) == 10 {
		return time.ParseInLocation("2006-01-02", s, chinaTime)
	}
	return time.Parse(time.RFC3339Nano, s)
}

// Fixed-width UTC timestamps sort by instant without losing fractional seconds.
func orderTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
