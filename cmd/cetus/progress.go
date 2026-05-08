package main

import (
	"fmt"
	"io"
	"math"
	"time"

	"github.com/cenvero/cetus/internal/browser"
)

type renderProgressLogger struct {
	out        io.Writer
	frameStart time.Time
	lastUpdate time.Time
	lastBucket int
}

func newRenderProgressLogger(out io.Writer) *renderProgressLogger {
	return &renderProgressLogger{
		out:        out,
		lastBucket: -1,
	}
}

func (l *renderProgressLogger) Stage(format string, args ...any) {
	if l == nil || l.out == nil {
		return
	}
	fmt.Fprintf(l.out, "%s\n", fmt.Sprintf(format, args...))
}

func (l *renderProgressLogger) Frames(progress browser.CaptureProgress) {
	if l == nil || l.out == nil || progress.TotalFrames <= 0 {
		return
	}

	completed := clamp(progress.CompletedFrames, 0, progress.TotalFrames)
	bucket := progressBucket(completed, progress.TotalFrames)
	now := time.Now()
	if completed == 0 || l.frameStart.IsZero() {
		l.frameStart = now
	}
	if completed != 0 && completed != progress.TotalFrames && now.Sub(l.lastUpdate) < time.Second && bucket < l.lastBucket+10 {
		return
	}

	l.lastUpdate = now
	l.lastBucket = bucket
	fmt.Fprintln(l.out, frameProgressLine(completed, progress.TotalFrames, now.Sub(l.frameStart)))
}

func frameProgressLine(completed, total int, elapsed time.Duration) string {
	completed = clamp(completed, 0, total)

	percent := 0.0
	if total > 0 {
		percent = float64(completed) * 100 / float64(total)
	}

	line := fmt.Sprintf("Rendering frames: %d/%d (%.1f%%, elapsed %s)", completed, total, percent, roundDuration(elapsed))
	if completed > 0 && completed < total && elapsed > 0 {
		rate := float64(completed) / elapsed.Seconds()
		if rate > 0 {
			remaining := time.Duration((float64(total-completed) / rate) * float64(time.Second))
			line += fmt.Sprintf(", eta %s", roundDuration(remaining))
		}
	}
	return line
}

func progressBucket(completed, total int) int {
	if total <= 0 {
		return 0
	}
	return int(math.Floor(float64(clamp(completed, 0, total)) * 100 / float64(total)))
}

func roundDuration(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	if d < time.Second {
		return d.Round(100 * time.Millisecond)
	}
	return d.Round(time.Second)
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
