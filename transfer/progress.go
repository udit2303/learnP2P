package transfer

import (
	"fmt"
	"strings"
	"time"
)

// humanBytes renders a size like 1.2 MiB, 850 KiB, etc.
func humanBytes(n int64) string {
	const (
		KiB = 1024
		MiB = 1024 * KiB
		GiB = 1024 * MiB
	)
	switch {
	case n >= GiB:
		return fmt.Sprintf("%.2f GiB", float64(n)/float64(GiB))
	case n >= MiB:
		return fmt.Sprintf("%.2f MiB", float64(n)/float64(MiB))
	case n >= KiB:
		return fmt.Sprintf("%.2f KiB", float64(n)/float64(KiB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func humanRate(bytesPerSec float64) string {
	if bytesPerSec <= 0 {
		return "0 B/s"
	}
	const (
		KiB = 1024
		MiB = 1024 * KiB
		GiB = 1024 * MiB
	)
	switch {
	case bytesPerSec >= GiB:
		return fmt.Sprintf("%.2f GiB/s", bytesPerSec/float64(GiB))
	case bytesPerSec >= MiB:
		return fmt.Sprintf("%.2f MiB/s", bytesPerSec/float64(MiB))
	case bytesPerSec >= KiB:
		return fmt.Sprintf("%.2f KiB/s", bytesPerSec/float64(KiB))
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}

func renderBar(pct float64, width int) string {
	if width <= 0 {
		return ""
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(pct*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	bar := make([]rune, width)
	for i := 0; i < width; i++ {
		if i < filled {
			bar[i] = '#'
		} else {
			bar[i] = '-'
		}
	}
	return string(bar)
}

func formatETA(remaining int64, rate float64) string {
	if rate <= 1e-9 {
		return "--:--"
	}
	secs := time.Duration(float64(remaining)/rate) * time.Second
	if secs < 0 {
		secs = 0
	}
	h := int(secs.Hours())
	m := int((secs % time.Hour) / time.Minute)
	s := int((secs % time.Minute) / time.Second)
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// printProgress prints a single-line progress with carriage return for in-place updates.
// Example: "Sending file.bin |##########------|  62.3%  12.3 MiB/19.7 MiB  8.4 MiB/s  ETA 00:01"
var lastProgressLen int

func printProgress(prefix, name string, done, total int64, start time.Time) {
	if total <= 0 {
		total = 1
	}
	pct := float64(done) / float64(total)
	elapsed := time.Since(start).Seconds()
	if elapsed < 1e-9 {
		elapsed = 1e-9
	}
	rate := float64(done) / elapsed
	eta := formatETA(total-done, rate)
	bar := renderBar(pct, 20)
	line := fmt.Sprintf("%s %s |%s| %6.2f%%  %s/%s  %s  ETA %s",
		prefix, name, bar, pct*100,
		humanBytes(done), humanBytes(total), humanRate(rate), eta,
	)
	// Pad with spaces if the new line is shorter than the previous to clear leftovers
	if lastProgressLen > len(line) {
		line += strings.Repeat(" ", lastProgressLen-len(line))
	}
	fmt.Print("\r" + line)
	lastProgressLen = len(line)
	// On completion, reset tracking so next progress starts cleanly
	if done >= total {
		lastProgressLen = 0
	}
}
