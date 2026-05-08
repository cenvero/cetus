package main

import (
	"strings"
	"testing"
	"time"
)

func TestFrameProgressLineIncludesElapsedAndETA(t *testing.T) {
	got := frameProgressLine(50, 100, 10*time.Second)
	for _, want := range []string{"50/100", "50.0%", "elapsed 10s", "eta 10s"} {
		if !strings.Contains(got, want) {
			t.Fatalf("frameProgressLine() = %q, want %q", got, want)
		}
	}
}

func TestFrameProgressLineOmitsETAAtStartAndFinish(t *testing.T) {
	for _, got := range []string{
		frameProgressLine(0, 100, 0),
		frameProgressLine(100, 100, 10*time.Second),
	} {
		if strings.Contains(got, "eta") {
			t.Fatalf("frameProgressLine() = %q, want no eta", got)
		}
	}
}

func TestProgressBucketClampsToRange(t *testing.T) {
	tests := []struct {
		name      string
		completed int
		total     int
		want      int
	}{
		{name: "below zero", completed: -5, total: 100, want: 0},
		{name: "middle", completed: 45, total: 100, want: 45},
		{name: "above total", completed: 125, total: 100, want: 100},
		{name: "empty total", completed: 1, total: 0, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := progressBucket(tt.completed, tt.total); got != tt.want {
				t.Fatalf("progressBucket() = %d, want %d", got, tt.want)
			}
		})
	}
}
