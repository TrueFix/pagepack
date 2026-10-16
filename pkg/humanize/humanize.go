// Package humanize formats byte counts and rates the same way the frontend does
// (binary multiples, labelled KB/MB/GB), so logs and API responses read alike.
package humanize

import (
	"strconv"
	"time"
)

var units = []string{"B", "KB", "MB", "GB", "TB"}

// Bytes renders 1536 as "1.5 KB" and 104857600 as "100 MB".
func Bytes(n int64) string {
	if n < 0 {
		return "-" + Bytes(-n)
	}
	value, unit := float64(n), 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return strconv.FormatInt(n, 10) + " B"
	}
	precision := 1
	if value >= 100 || value == float64(int64(value)) {
		precision = 0
	}
	return strconv.FormatFloat(value, 'f', precision, 64) + " " + units[unit]
}

// Rate renders bytes per second, e.g. "1.2 MB/s".
func Rate(bytesPerSecond int64) string { return Bytes(bytesPerSecond) + "/s" }

// PerSecond returns n/d as an integer rate, or 0 for an empty interval.
func PerSecond(n int64, d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(float64(n) / d.Seconds())
}

// Percent returns part/total*100 rounded to one decimal place, clamped to 0–100.
func Percent(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	p := float64(part) / float64(total) * 100
	if p > 100 {
		p = 100
	} else if p < 0 {
		p = 0
	}
	return float64(int64(p*10+0.5)) / 10
}
