package main

import (
	"fmt"
	"time"
)

// formatMillis renders a Unix millisecond timestamp in local time.
func formatMillis(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	return time.UnixMilli(ms).Format("2006-01-02 15:04")
}

// humanBytes renders a byte count. The arena's cap is 300 MiB, so binary units
// are what the limits are actually expressed in.
func humanBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGT"[exp])
}
