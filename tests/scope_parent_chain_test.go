// Package tests 的 scope 父作用域链（parent.go）验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/scope"
)

func TestParentTreeBindAndParent(t *testing.T) {
	pt := scope.NewParentTree()
	if err := pt.Bind("child", "parent"); err != nil {
		t.Fatal(err)
	}
	p, ok := pt.ParentOf("child")
	if !ok || p != "parent" {
		t.Fatalf("expected parent, got %q ok=%v", p, ok)
	}
	if _, ok := pt.ParentOf("parent"); ok {
		t.Fatal("root should have no parent")
	}
}

func TestParentTreeRejectDoubleBind(t *testing.T) {
	pt := scope.NewParentTree()
	_ = pt.Bind("c", "p")
	if err := pt.Bind("c", "other"); err != scope.ErrAlreadyBound {
		t.Fatalf("double bind must be rejected, got %v", err)
	}
}

func TestParentTreeRejectCycle(t *testing.T) {
	pt := scope.NewParentTree()
	_ = pt.Bind("a", "b")
	_ = pt.Bind("b", "c")
	// 让 c 的父为 a → a→b→c→a 成环
	if err := pt.Bind("c", "a"); err != scope.ErrCycle {
		t.Fatalf("cycle must be rejected, got %v", err)
	}
}

func TestParentTreeChainNearestFirst(t *testing.T) {
	pt := scope.NewParentTree()
	_ = pt.Bind("a", "b")
	_ = pt.Bind("b", "c")
	chain := pt.ChainOf("a")
	if len(chain) != 3 || chain[0] != "a" || chain[1] != "b" || chain[2] != "c" {
		t.Fatalf("chain should be nearest-first [a b c], got %v", chain)
	}
}

func TestParentTreeRebind(t *testing.T) {
	pt := scope.NewParentTree()
	_ = pt.Bind("a", "b")
	if err := pt.Rebind("a", "c"); err != nil {
		t.Fatal(err)
	}
	p, _ := pt.ParentOf("a")
	if p != "c" {
		t.Fatalf("rebind should update parent, got %q", p)
	}
}
