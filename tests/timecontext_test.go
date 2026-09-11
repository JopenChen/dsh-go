// Package tests 的 timecontext 验收测试。
package tests

import (
	"strings"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/timecontext"
)

func TestFormatElapsed(t *testing.T) {
	cases := map[time.Duration]string{
		45 * time.Second:               "45s",
		90 * time.Second:               "1m 30s",
		3661 * time.Second:             "1h 1m 1s",
		(24*3600 + 3600) * time.Second: "1d 1h 0s",
	}
	for in, want := range cases {
		if got := timecontext.FormatElapsed(in); got != want {
			t.Errorf("FormatElapsed(%v) = %q want %q", in, got, want)
		}
	}
}

func TestRenderWithPrevious(t *testing.T) {
	now := time.Date(2026, 9, 12, 11, 0, 0, 0, time.UTC)
	prev := now.Add(-90 * time.Second)
	got := timecontext.Render(now, time.UTC, &prev)
	if !strings.Contains(got, "1m 30s") {
		t.Fatalf("render should include elapsed 1m 30s, got %s", got)
	}
}

func TestRenderNoPrevious(t *testing.T) {
	now := time.Date(2026, 9, 12, 11, 0, 0, 0, time.UTC)
	got := timecontext.Render(now, time.UTC, nil)
	if !strings.Contains(got, "unavailable") {
		t.Fatalf("nil previous → unavailable, got %s", got)
	}
}
