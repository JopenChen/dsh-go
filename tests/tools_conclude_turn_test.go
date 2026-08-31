// 本文件对应任务 M25：ToolRunContext deferContext + concludeTurn。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/tools"
)

// TestToolsConcludeTurn 验证 report_blocker 调用 concludeTurn 后 turn 标记为结束。
func TestToolsConcludeTurn(t *testing.T) {
	concluded := ""
	tc := tools.NewToolRunContext(func(reason string) { concluded = reason })

	// 模拟 goal_report_blocker 调用
	if !tc.ConcludeTurn("goal-blocker") {
		t.Fatal("首次 conclude 应返回 true")
	}
	if concluded != "goal-blocker" {
		t.Fatalf("conclude reason = %q, want goal-blocker", concluded)
	}
	if !tc.IsConcluded() {
		t.Fatal("conclude 后 IsConcluded 应为 true")
	}

	// 再次调用不应重复触发
	if tc.ConcludeTurn("again") {
		t.Fatal("重复 conclude 应返回 false")
	}
}

// TestToolsDeferContext 验证 deferred 回调按 LIFO 在 conclude 时执行。
func TestToolsDeferContext(t *testing.T) {
	var order []string
	tc := tools.NewToolRunContext(nil)

	tc.DeferContext(func() { order = append(order, "d1") })
	tc.DeferContext(func() { order = append(order, "d2") })

	_ = tc.ConcludeTurn("cleanup")
	// LIFO：d2 先执行
	if len(order) != 2 || order[0] != "d2" || order[1] != "d1" {
		t.Fatalf("deferred 应按 LIFO 执行: %v", order)
	}
}

// TestToolsRunContextMeta 验证运行元数据读写。
func TestToolsRunContextMeta(t *testing.T) {
	tc := tools.NewToolRunContext(nil)
	tc.SetMeta("started_ms", 100)
	if v, ok := tc.GetMeta("started_ms"); !ok || v != 100 {
		t.Fatalf("meta 读写异常: %v %v", v, ok)
	}
}

// TestToolsConcludeContextPropagation 验证 conclude 回调经 context 传播。
func TestToolsConcludeContextPropagation(t *testing.T) {
	called := false
	tc := tools.NewToolRunContext(func(string) { called = true })

	ctx := tc.Context()
	got, ok := tools.ToolRunContextFrom(ctx)
	if !ok {
		t.Fatal("应能从 context 取 ToolRunContext")
	}
	got.ConcludeTurn("x")
	if !called {
		t.Fatal("conclude 回调应经 context 传播并触发")
	}
}