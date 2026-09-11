// Package tests 的 subagent 委托深度核算验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/subagent"
)

func TestChildDepthIncrements(t *testing.T) {
	d, err := subagent.ChildDepth(0)
	if err != nil || d != 1 {
		t.Fatalf("top-level child should be depth 1, got %d err=%v", d, err)
	}
	d, _ = subagent.ChildDepth(2)
	if d != 3 {
		t.Fatalf("depth 2 child should be 3, got %d", d)
	}
}

func TestChildDepthRejectNegative(t *testing.T) {
	if _, err := subagent.ChildDepth(-1); err != subagent.ErrInvalidDepth {
		t.Fatalf("negative parent rejected, got %v", err)
	}
}

func TestResolveDepthTakesMax(t *testing.T) {
	d, _ := subagent.ResolveDepth(2, 1)
	if d != 2 {
		t.Fatalf("header 2 should win over runtime 1, got %d", d)
	}
	d, _ = subagent.ResolveDepth(1, 3)
	if d != 3 {
		t.Fatalf("runtime 3 deepens header 1, got %d", d)
	}
}

func TestCanDelegate(t *testing.T) {
	if !subagent.CanDelegate(0, 3) {
		t.Fatal("depth 0 under cap 3 should delegate")
	}
	if subagent.CanDelegate(3, 3) {
		t.Fatal("depth equal to cap cannot delegate")
	}
	if !subagent.CanDelegate(100, -1) {
		t.Fatal("negative cap means unlimited")
	}
}
