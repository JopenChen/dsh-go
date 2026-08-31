// 教程：子代理（Subagent）——父 Agent 派生子任务并回收（教学示例）。
//
// 复杂 Agent 常把大任务拆给"子代理"去做。本项目通过 Provider 接缝抽象
// "如何起一个子代理"，内置后端：acp（原生协议桩）与 fork-copy（复制进程桩）。
//
// 配套概念：
//   - ForkLineage 记录父子家谱（parent 会话 → 子会话），供因果归因；
//   - Handle 是子代理生命周期句柄：Drain 等子任务完成、Dispose 释放并级联取消；
//   - Runtime.Spawn 按后端名派生子代理并登记句柄；
//   - Runtime.DisposeOwner 释放某父名下的全部子代理（父 dispose → 子清理）。
//
// 本示例演示：Runtime.Spawn 派生子代理 → 观察家谱 → Drain 收结果 →
// 父释放级联清理 → 未知后端返回稳定错误。
//
// 运行方式（仓库根目录）：
//   go run ./examples/subagent
//
// 对照阅读：pkg/subagent/subagent.go（Provider / Runtime / Handle / ForkLineage）
package main

import (
	"context"
	"fmt"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/subagent"
)

func main() {
	ctx := context.Background()

	// 1. 构造父会话（顶层，无 parent）。
	parent := brand.NewSessionID("parent-task")

	// 2. 创建子代理运行时（已内置 acp / fork-copy 桩 Provider）。
	rt := subagent.NewRuntime()

	// ------------------------------------------------------------------
	// 3. 通过指定后端派生子代理。Spawn 返回 Handle；Drain 等子任务完成。
	// ------------------------------------------------------------------
	fmt.Println("— 派生子代理（内置后端）—")
	for _, backend := range []string{"acp", "fork-copy"} {
		h, err := rt.Spawn(ctx, backend, subagent.SpawnRequest{
			Parent:    &parent,
			Input:     "分析这份需求",
			MaxRounds: 3,
		})
		if err != nil {
			fmt.Printf("  [%s] spawn err: %v\n", backend, err)
			continue
		}
		if err := h.Drain(ctx); err != nil {
			fmt.Printf("  [%s] drain err: %v\n", backend, err)
			continue
		}
		// 观察家谱：Parent → Session 的父子关系（因果归因用）。
		parentStr := "(顶层)"
		if h.Lineage.Parent != nil {
			parentStr = h.Lineage.Parent.Raw()
		}
		fmt.Printf("  [%s] session=%s | lineage: parent=%s\n",
			backend, h.Lineage.Session.Raw(), parentStr)
	}

	// ------------------------------------------------------------------
	// 4. 教学点：父释放级联清理。
	//    DisposeOwner 释放父名下的全部子代理句柄（父 dispose → 子清理）；
	//    释放后 Drain 立即返回（不再等待），上层即可安全回收资源。
	//    注：Runtime.handles 是登记映射，Dispose 不删除条目（ActiveCount 不变），
	//    释放语义是"句柄已结束"，而非"从登记中移除"。
	// ------------------------------------------------------------------
	for _, backend := range []string{"acp", "fork-copy"} {
		// 重新派生，方便演示释放后的 Drain 行为。
		h, _ := rt.Spawn(ctx, backend, subagent.SpawnRequest{Parent: &parent, Input: "x"})
		_ = h.Drain(ctx)
	}
	before := rt.ActiveCount()
	rt.DisposeOwner(parent)
	fmt.Printf("\n— 父释放级联：DisposeOwner 后登记数 %d（Drain 均已立即返回）—\n", before)

	// ------------------------------------------------------------------
	// 5. 教学点：未知后端返回稳定错误（ErrUnknownProvider）。
	//    而不是随便抛个 panic——上层可据此路由/提示。
	// ------------------------------------------------------------------
	if _, err := rt.Spawn(ctx, "does-not-exist", subagent.SpawnRequest{Input: "x"}); err != nil {
		if subagent.IsUnknownProvider(err) {
			fmt.Printf("— 未知后端稳定错误: %v —\n", err)
		} else {
			fmt.Printf("— 其它错误: %v —\n", err)
		}
	}
}