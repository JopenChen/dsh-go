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

## 对照源码

- `pkg/goal/goal.go` —— Goal 状态机与 6 工具
- `pkg/goal/errors.go` —— 9 个稳定错误码 + GoalError
- 可运行示例：[`examples/tutorial`](https://github.com/JopenChen/dsh-go/blob/master/examples/tutorial/main.go) 第 3 步

## 下一步

- 探索[更多教程](../)或[示例](/en/examples/)
- 查看[FAQ](/en/faq/)
