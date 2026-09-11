---
title: "Goal 状态机（Goal State Machine）"
description: "四态规划 + 稳定错误码 + 续轮驱动"
weight: 30
---

# Goal 状态机（Goal State Machine）

## 一句话

**Agent 的"规划能力"核心是一个状态机：一个目标（Goal）有生命周期，从一个阶段流转到下一个；Agent 驱动它续轮执行，直到达成或阻塞。**

## 四态（对齐官方）

| 状态 | 含义 | 是否自动续轮 |
|---|---|---|
| `active` | 进行中 | ✅ 是（RoundDriver 继续） |
| `paused` | 暂停 | ❌ 否（等待恢复） |
| `blocked` | 被阻塞（blocker 未解决） | ❌ 否 |
| `complete` | 已完成 | ❌ 否 |

## 合法迁移：不是任意两态都能切换

只校验目标态是四态之一并不够，状态机还约束 from→to 的方向（`goal.CanTransition`）：

```
active   → active / paused / blocked / complete
paused   → active / complete
blocked  → active / complete
complete → （终态，无出边）
```

`paused`/`blocked` 只能从 `active` 进入；`complete` 一旦到达不可再迁移。非法迁移返回 `GOAL_INVALID_TRANSITION`。

## 稳定错误码

与官方 `error.ts` 对齐的 9 个稳定 `GOAL_*` 错误码（如 `GOAL_INVALID_MAX_ROUNDS`、`GOAL_STALE_REVISION`、`GOAL_NOT_FOUND`）。错误按稳定串路由，绝不解析 message 文本。

## 在 Dsh-Go 中

```go
ts := goal.NewGoalToolset(sl) // 绑定到会话日志
// 6 个工具：goal_list / goal_set_phase / goal_set_description
//          / goal_set_max_rounds / goal_add_blocker / goal_report_blocker
```

示例中可以看到稳定错误码如何被干净地表达：

```go
if _, err := call(ts, "goal_set_max_rounds", map[string]any{"maxRounds": float64(-1)}); err != nil {
    if ge, ok := err.(*goal.GoalError); ok {
        fmt.Println(ge.Code) // GOAL_INVALID_MAX_ROUNDS
    }
}
```

## Ralph：目标驱动的自动迭代

当目标明确、希望 Agent 自主推进到完成时，`workflow.RunRalph` 提供一个前台循环：每轮启动一个全新子代理，只携带固定目标与上一轮的结构化 handoff，子代理返回 `continue / complete / blocked` 报告。`continue` 携 handoff 进入下一轮，`complete` 成功收尾（须给出 evidence），`blocked` 交回人工；达到轮数上限则记为 `budget-limited`。每轮 handoff 都有长度上限，避免循环中上下文无限膨胀。

## Todo：整体替换的三态清单

Goal 与 Todo 互补：Goal 管"目标处于什么阶段"，Todo 管"具体要做哪些事"。`todo` 包的待办是**整体替换**（last-write-wins），每条三态：

| 状态 | 含义 |
|---|---|
| `pending` | 未开始 |
| `in_progress` | 正在做（顺序模式至多一个，`AllowParallel` 可放开） |
| `completed` | 已完成 |

`Normalize` 会保证内容非空、不重复，并约束进行中数量。

## 对照源码

- `pkg/goal/goal.go` —— Goal 状态机与 6 工具
- `pkg/goal/errors.go` —— 9 个稳定错误码 + GoalError
- `pkg/goal/transition.go` —— `CanTransition` 迁移合法性
- `pkg/workflow/ralph.go` —— `RunRalph` 目标自动迭代循环
- 可运行示例：[`examples/tutorial`](https://github.com/JopenChen/dsh-go/blob/master/examples/tutorial/main.go) 第 3 步

## 下一步

- 探索[更多教程](../)或[示例](/zh-cn/examples/)
- 查看[FAQ](/zh-cn/faq/)
