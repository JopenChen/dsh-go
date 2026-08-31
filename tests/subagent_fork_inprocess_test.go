// 本文件验证任务 S02：Subagent 接缝（in-process fork 后端）。
//
// 覆盖：in-process fork 正确执行并返回父→子家谱；父 dispose → 子句柄自动 drain/cleanup；
// 未注册后端报错；内置 ACP/ForkCopy 桩可运行。
package tests

import (
	"context"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/subagent"
)

// TestSubagentInProcessForkLineage 验证 in-process fork 家谱 parent/session 正确。
func TestSubagentInProcessForkLineage(t *testing.T) {
	ctx := context.Background()
	parent := brand.NewSessionID("parent-1")
	captured := ""

	r := subagent.NewRuntime()
	// 注入 in-process 后端：记录父子家谱并返回输出。
	rp := subagent.NewInProcessProvider(func(_ context.Context, req subagent.SpawnRequest) (brand.SessionID, string, error) {
		sid := brand.NewSessionID("child-1")
		captured = "parent="
		if req.Parent != nil {
			captured += req.Parent.Raw()
		}
		captured += "/session=" + sid.Raw()
		return sid, "子任务完成", nil
	})
	_ = r.RegisterProvider(rp)

	h, err := r.Spawn(ctx, "in-process", subagent.SpawnRequest{
		Parent: &parent, Input: "fork me",
		MaxRounds: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 家谱 parent/session 正确。
	if h.Lineage.Parent == nil || *h.Lineage.Parent != parent {
		t.Fatalf("家谱 parent 应为 %s，实际 %v", parent, h.Lineage.Parent)
	}
	if h.Lineage.Session.Raw() != "child-1" {
		t.Fatalf("子会话应为 child-1，实际 %s", h.Lineage.Session)
	}
	if captured != "parent=parent-1/session=child-1" {
		t.Fatalf("runner 收到的家谱上下文异常: %q", captured)
	}
	// Drain 不阻塞（进程内同步完成）。
	if err := h.Drain(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestSubagentParentDisposeDrainsChild 验证父 dispose → 子句柄自动 drained/cleanup。
func TestSubagentParentDisposeDrainsChild(t *testing.T) {
	ctx := context.Background()
	parent := brand.NewSessionID("parent-2")

	r := subagent.NewRuntime()
	rp := subagent.NewInProcessProvider(nil) // 默认占位 runner
	_ = r.RegisterProvider(rp)

	h, err := r.Spawn(ctx, "in-process", subagent.SpawnRequest{Parent: &parent, Input: "t"})
	if err != nil {
		t.Fatal(err)
	}
	// 父释放名下所有子代理。
	r.DisposeOwner(parent)
	// 子句柄应被 dispose（drained），Drain 立即返回。
	done := make(chan error, 1)
	go func() { done <- h.Drain(ctx) }()
	select {
	case <-done:
		// 释放后 Drain 返回（不阻塞）。
		_ = h.Drain(ctx)
	case <-time.After(2 * time.Second):
		t.Fatal("父 dispose 后子句柄应能 drain，超时未返回")
	}
}

// TestSubagentUnknownProvider 验证未注册后端报错。
func TestSubagentUnknownProvider(t *testing.T) {
	r := subagent.NewRuntime()
	_, err := r.Spawn(context.Background(), "no-such", subagent.SpawnRequest{Input: "x"})
	if !subagent.IsUnknownProvider(err) {
		t.Fatalf("未知后端应报 ErrUnknownProvider，实际 %v", err)
	}
}

// TestSubagentBuiltinStubs 验证内置 ACP/ForkCopy 桩可运行。
func TestSubagentBuiltinStubs(t *testing.T) {
	r := subagent.NewRuntime()
	for _, name := range []string{"acp", "fork-copy"} {
		h, err := r.Spawn(context.Background(), name, subagent.SpawnRequest{Input: "s"})
		if err != nil {
			t.Fatalf("%s 桩 spawn 失败: %v", name, err)
		}
		if h.Provider != name {
			t.Fatalf("%s 句柄 provider 应为 %s，实际 %s", name, name, h.Provider)
		}
	}
	// 共 2 个句柄登记（acp + fork-copy）。
	if r.ActiveCount() != 2 {
		t.Fatalf("应有 2 个句柄登记，实际 %d", r.ActiveCount())
	}
}