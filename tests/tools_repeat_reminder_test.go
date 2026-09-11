// Package tests 的连续重复调用检测验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/tools"
)

func TestCanonicalizeArgsIgnoresKeyOrder(t *testing.T) {
	a := map[string]any{"x": 1, "y": 2}
	b := map[string]any{"y": 2, "x": 1}
	if tools.CanonicalizeArgs(a) != tools.CanonicalizeArgs(b) {
		t.Fatal("different key order must canonicalize identically")
	}
}

func TestRepeatStateCountsConsecutive(t *testing.T) {
	var r tools.RepeatState
	args := map[string]any{"p": 1}
	c1 := r.Observe("ls", args)
	c2 := r.Observe("ls", args)
	c3 := r.Observe("ls", args)
	if c1 != 1 || c2 != 2 || c3 != 3 {
		t.Fatalf("consecutive counts should be 1,2,3 got %d,%d,%d", c1, c2, c3)
	}
}

func TestRepeatStateResetsOnDifferentCall(t *testing.T) {
	var r tools.RepeatState
	r.Observe("ls", map[string]any{"p": 1})
	r.Observe("ls", map[string]any{"p": 1})
	c := r.Observe("cat", map[string]any{"p": 1})
	if c != 1 {
		t.Fatalf("different tool resets chain, got %d", c)
	}
}

func TestRepeatStateManualReset(t *testing.T) {
	var r tools.RepeatState
	r.Observe("ls", map[string]any{"p": 1})
	r.Reset()
	c := r.Observe("ls", map[string]any{"p": 1})
	if c != 1 {
		t.Fatal("after reset count restarts at 1")
	}
}

func TestHitThreshold(t *testing.T) {
	if !tools.HitThreshold(3, tools.DefaultRepeatThresholds) {
		t.Fatal("3 is a default threshold")
	}
	if tools.HitThreshold(4, tools.DefaultRepeatThresholds) {
		t.Fatal("4 is not a threshold")
	}
}

func TestDetailedReminderBounded(t *testing.T) {
	s := tools.DetailedReminder("ls", 5, `{"long":"abcdef"}`, 5)
	if len(s) == 0 {
		t.Fatal("reminder should be non-empty")
	}
}
