package gui

import (
	"fmt"
	"time"
)

// humanSize renders bytes as KB/MB/GB, one decimal above 1 MB (cli-conventions section 5).
func humanSize(n int64) string {
	const k = 1024.0
	f := float64(n)
	switch {
	case n < 0:
		return ""
	case n == 0:
		return "0 B"
	case f < k:
		return fmt.Sprintf("%d B", n)
	case f < k*k:
		return fmt.Sprintf("%.0f KB", f/k)
	case f < k*k*k:
		return fmt.Sprintf("%.1f MB", f/(k*k))
	}
	return fmt.Sprintf("%.1f GB", f/(k*k*k))
}

// clock renders a duration as mm:ss, or h:mm:ss from one hour.
func clock(d time.Duration) string {
	s := max(int(d.Round(time.Second).Seconds()), 0)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

// age renders how old a cached answer is: 1m, 5m, 3h, 2d.
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
