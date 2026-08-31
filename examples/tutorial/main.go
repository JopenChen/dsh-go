// 教程：从零理解 dsh-go 的 Agent 内核（参考实现 × 教学示例）。
//
// 本示例的定位不是"教你跑通"，而是——当你想搞懂一个 Agent 框架内部
// 到底怎么运作时，dsh-go 作为一份"可读的来源"，比黑盒框架更适合入门。
// 它把官方 DeepSeek Harness 的核心概念翻译成了 Go 代码，且刻意保持模块
// 小而独立。
//
// 我们按三条主线渐进理解，每一条都能独立运行：
//   第 1 步  事件溯源（Event Sourcing）：为什么"只记事件不记状态"更稳。
//   第 2 步  fold 派生投影：状态从事件日志里"算"出来，而不是"存"出来。
//   第 3 步  Goal 状态机：Agent 如何把"目标"变成可续轮的可执行循环。
//
// 运行方式（仓库根目录）：
//   go run ./examples/tutorial
//
// 建议对照阅读源码：
//   - pkg/session/session.go     事件日志与 45+ 事件词汇
//   - pkg/session/fold.go        fold 投影函数族
//   - pkg/goal/goal.go           Goal 状态机（active/paused/blocked/complete）
package main

import (
	"context"
	"fmt"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/goal"
	"github.com/JopenChen/dsh-go/pkg/session"
)

func main() {
	// 每步都是独立的、可复现的小实验。
	step1EventSourcing()
	fmt.Println()
	step2FoldProjection()
	fmt.Println()
	step3GoalStateMachine()
}

// ---------------------------------------------------------------------------
// 第 1 步：事件溯源
//
// 传统做法：把"当前状态"存下来（比如 user=Carla, turn=3），改一次覆盖一次。
// 事件溯源：什么都不直接存，只"追加"一条条不可变的事件，状态随时可推出来。
//
// 好处（也是 dsh-go 选择它的原因）：
//   - 完整历史都在，可回放、可审计（谁何时做了什么一清二楚）；
//   - 不会因为一次写坏覆盖而丢失状态（数据即事件，事件即数据）；
//   - 可以在此基础上做分叉（fork）、压缩（compact）、增量投影。
// ---------------------------------------------------------------------------
func step1EventSourcing() {
	fmt.Println("=== 第 1 步：事件溯源（append-only event log）===")

	// 创建一条会话日志。SessionID 用 brand 包生成（可带可读前缀）。
	sl := session.NewSessionLog(brand.NewSessionID("tutorial-1"))

	// 只通过 Append() 写入事件。这是唯一写入口，引擎会自动校验时序不变量。
	_, _ = sl.Append(session.UserMessageData{Content: "帮我评估 dsh-go"})
	_, _ = sl.Append(session.AssistantMessageData{Content: "好的，我们开始。"})

	// 事件日志不是黑盒——直接读 Events() 就能看到每一步。
	evs := sl.Events()
	fmt.Printf("— 已追加事件数: %d —\n", len(evs))
	for i, ev := range evs {
		fmt.Printf("  #%d seq=%d type=%s\n", i+1, ev.Seq, ev.Type)
	}

	// 关键点：这些事件是"不可变事实"，永远不会被后续写入覆盖。
	fmt.Println("— 结论：状态 = 历史事件的累积结果，而非一份被覆盖的快照 —")
}

// ---------------------------------------------------------------------------
// 第 2 步：fold 派生投影
//
// 事件日志只是"原材料"。你可通过 fold（折叠/规约）把它派生出各种"投影"：
// 比如当前对话消息列表、会话标题、目标等等。
//
// 为什么不直接存投影？因为投影只是"派生值"，可以随时用 fold 从事件里
// 重新算出来；存一份就到账了一份，还要保证它和日志一致。dsh-go 的哲学是：
//   日志是唯一真相（source of truth），一切投影都是它的函数。
// ---------------------------------------------------------------------------
func step2FoldProjection() {
	fmt.Println("=== 第 2 步：fold 派生投影（状态从事件算出来）===")

	sl := session.NewSessionLog(brand.NewSessionID("tutorial-2"))
	_, _ = sl.Append(session.UserMessageData{Content: "你好"})
	_, _ = sl.Append(session.AssistantMessageData{Content: "你好，我是 dsh-go。"})

	// FoldAll 把整个日志折叠成一份"派生状态"，这里重点看 Messages（消息列表）。
	proj := session.FoldAll(sl.Events())
	fmt.Printf("— 派生消息数: %d —\n", len(proj.Messages))
	for _, m := range proj.Messages {
		fmt.Printf("  [%s] %s\n", m.Role, m.Content)
	}

	// 同样的日志，可以派生另一份视角（比如 plan mode / 会话标题）——fold 是纯函数，
	// 输入不变则输出不变，天然可测试、可缓存。
	if proj.PlanMode.Present {
		fmt.Printf("— 派生 plan mode: %v —\n", proj.PlanMode.Mode)
	}
	fmt.Println("— 结论：投影 = f(事件日志)；日志不变，投影永远可复现 —")
}

// ---------------------------------------------------------------------------
// 第 3 步：Goal 状态机
//
// Agent 的"规划能力"核心是一个状态机：一个目标（Goal）有生命周期，
// 从一个阶段流转到下一个；Agent 驱动它续轮执行，直到达成或阻塞。
//
// dsh-go 对齐官方四态：
//   active   进行中（RoundDriver 会继续自动续轮）
//   paused   暂停（不续轮，等待恢复）
//   blocked  被阻塞（某个 blocker 未解决，不续轮）
//   complete 已完成（不再续轮）
//
// 这里我们用 Goal 工具集演示最常用的两个操作：建目标、检查当前状态。
// ---------------------------------------------------------------------------
func step3GoalStateMachine() {
	fmt.Println("=== 第 3 步：Goal 状态机（planning state machine）===")

	// GoalToolset 把这些工具绑定到上面那条会话日志上（目标状态也走事件溯源派生）。
	sl := session.NewSessionLog(brand.NewSessionID("tutorial-3"))
	ts := goal.NewGoalToolset(sl)

	// 1) 列出一共有哪些 goal_* 工具（见 pkg/goal 的 6 个工具）。
	var names []string
	for _, t := range ts.Tools() {
		names = append(names, t.Name)
	}
	fmt.Printf("— goal 工具集: %v —\n", names)

	// 2) 给当前会话设一个目标（对应 goal_* 里的 set_phase / 建目标类操作）。
	//    这里演示设置描述与最大轮数，观察状态机如何接受合法输入。
	if _, err := call(ts, "goal_set_description", map[string]any{"description": "完成一份评估报告"}); err != nil {
		fmt.Printf("  set_description err: %v\n", err)
	}
	if _, err := call(ts, "goal_set_max_rounds", map[string]any{"maxRounds": float64(5)}); err != nil {
		fmt.Printf("  set_max_rounds err: %v\n", err)
	}

	// 3) 读取当前派生出的目标状态（从事件日志 fold 出来）。
	st := sl.Projection().Goal
	fmt.Printf("— 派生目标状态: present=%v, phase=%q, maxRounds=%d —\n",
		st.Present, st.Phase, st.MaxRounds)

	// 4) 教学点：构造一个"非法"输入，观察稳定错误码（而非随意报错）。
	//    这样你能直观看到 Agent 内核是如何把错误"稳定的、可路由地"表达出来，
	//    而不是把原始异常直接抛给上层。
	if _, err := call(ts, "goal_set_max_rounds", map[string]any{"maxRounds": float64(-1)}); err != nil {
		if ge, ok := err.(*goal.GoalError); ok {
			fmt.Printf("— 稳定错误码示例: %s —\n", ge.Code)
		} else {
			fmt.Printf("— 其它错误: %v —\n", err)
		}
	}
	fmt.Println("— 结论：规划 = 状态机；阶段 + 稳定错误码让 Agent 行为可预期、可驱动 —")
}

// call 按名取工具并执行，简化示例调用（教学用途，不做完整流水线）。
func call(ts *goal.GoalToolset, name string, input map[string]any) (any, error) {
	for _, t := range ts.Tools() {
		if t.Name == name {
			return t.Execute(context.Background(), input)
		}
	}
	return nil, fmt.Errorf("demo: tool %q not found", name)
}