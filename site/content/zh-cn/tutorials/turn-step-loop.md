---
title: "Turn / Step 双循环"
description: "Agent 如何将对话结构化为嵌套的 Turn 和 Step 循环，以及严格单调编号和配对不变量"
weight: 30
---

# Turn / Step 双循环

## 一句话总结

**每次 Agent 对话都被结构化为一个嵌套的双循环：一个 *Turn* 是一次用户请求及其完整解决过程，而每个 Turn 内部包含一个或多个 *Step*，每个 Step 代表一次 LLM 调用及其工具执行——两个循环都携带严格单调递增的编号，并由 session 不变量强制保证。**

## 为什么需要两个循环？

单循环不够，因为一次用户请求通常需要**多次 LLM 调用**才能完成：

```
用户："列出文件，然后统计每个 .go 文件的行数"
  └─ Turn 0 开始
       ├─ Step 1：LLM 决定调用 `ls` → 工具执行 → 返回结果
       ├─ Step 2：LLM 看到文件列表，决定对每个文件调用 `wc -l` → 工具执行 → 返回结果
       └─ Step 3：LLM 综合最终答案（不再有工具调用）
  └─ Turn 0 结束
```

**Turn** 边界回答的问题是：*"这次用户请求是否已解决？"*
**Step** 边界回答的问题是：*"这次 LLM 调用是否产生了需要下一轮的工具调用？"*

这种分离使得以下能力成为可能：
- **按 Turn 取消** — 中止一次用户请求而不丢失会话历史
- **按 Step 重试** — 重新运行失败的 LLM 调用而不重启整个 Turn
- **精确的 Token 统计** — 按 Step 统计 Token，按 Turn 汇总
- **工具调用作用域** — 每个 `tool/call` 必须属于一个打开的 Step，而 Step 必须属于一个打开的 Turn

## 四种事件标记

双循环完全通过 session 日志中的四种事件类型来表达：

| 事件 | 数据 | 含义 |
|---|---|---|
| `turn/start` | `{ turn: uint64 }` | 新的 Turn 开始 |
| `turn/end` | `{ turn: uint64, reason: TurnEndReason }` | 当前 Turn 关闭 |
| `step/start` | `{ turn: uint64, step: uint64 }` | 当前 Turn 内新的 Step 开始 |
| `step/end` | `{ turn: uint64, step: uint64 }` | 当前 Step 关闭 |

还有第五种辅助事件：

| 事件 | 数据 | 含义 |
|---|---|---|
| `turn/stopping` | `{ reason?: string }` | Turn 进入关闭流程（goal-round-driver 等监听器在此挂钩） |

```go
// pkg/session/session.go
type TurnStartData struct {
    Turn uint64 `json:"turn"`
}

type TurnEndData struct {
    Turn   uint64        `json:"turn"`
    Reason TurnEndReason `json:"reason"`
}

type StepStartData struct {
    Turn   uint64 `json:"turn"`
    Step   uint64 `json:"step"`
    StepSeq uint64 `json:"stepSeq,omitempty"` // 旧字段别名
}

type StepEndData struct {
    Turn   uint64 `json:"turn"`
    Step   uint64 `json:"step"`
    StepSeq uint64 `json:"stepSeq,omitempty"` // 旧字段别名
}
```

## 严格单调编号

这是最重要的不变量——也是最容易被忽略的。

### Turn 编号

- Turn 从 **0** 开始编号
- 每个 `turn/start` 必须携带 `turn == nextTurn`
- `turn/end` 之后，`nextTurn` 递增 1

```
turn/start {turn: 0}  → nextTurn 是 0 ✓ → nextTurn 保持 0（turn 打开中）
turn/end   {turn: 0}  → 匹配 openTurn 0 ✓ → nextTurn 变为 1
turn/start {turn: 1}  → nextTurn 是 1 ✓
turn/start {turn: 0}  → ✗ 被拒绝：期望 turn 1，得到 0
```

### Step 编号

- Step 从 **1** 开始编号（不是 0）
- 每个新的 Turn 会将 `nextStep` **重置**为 1
- 每个 `step/start` 必须携带 `step == nextStep`
- `step/end` 之后，`nextStep` 递增 1

```
turn/start {turn: 0}  → nextStep 重置为 1
step/start {turn:0, step:1}  → nextStep 是 1 ✓ → nextStep 变为 2
step/end   {turn:0, step:1}  → 匹配 openStep 1 ✓
step/start {turn:0, step:2}  → nextStep 是 2 ✓
turn/end   {turn: 0}
turn/start {turn: 1}  → nextStep 再次重置为 1
step/start {turn:1, step:1}  → nextStep 是 1 ✓（重置生效）
```

### 为什么要严格？

严格单调编号有三个目的：

1. **检测丢失的事件** — 如果 `turn/end {turn: 3}` 之后出现 `turn/start {turn: 5}`，你就知道 Turn 4 丢失了（或者日志已损坏）
2. **支持确定性重放** — 给定事件序列，你总能无歧义地重建循环状态
3. **捕获生产者 Bug** — 如果 Agent 忘记关闭 Step 就打开新的 Step，编号不匹配会立即暴露

## 嵌套规则

双循环有严格的嵌套层次：

```
turn/start
  ├─ (user/message)
  ├─ step/start
  │    ├─ (agent/request)
  │    ├─ (assistant/chunk...)
  │    ├─ (tool/call)
  │    ├─ (tool/result)
  │    └─ (assistant/message)
  ├─ step/end
  ├─ step/start  （可选：下一轮 LLM 调用）
  ├─ step/end
  └─ (turn/stopping)
turn/end
```

### 强制不变量

| 规则 | 违规情况 | 错误信息 |
|---|---|---|
| `turn/start` 要求没有打开的 Turn | 两个 `turn/start` 之间没有 `turn/end` | `turn/start while turn already open` |
| `turn/end` 要求有打开的 Turn | 没有 `turn/start` 就 `turn/end` | `turn/end without open turn/start` |
| `turn/end` 要求没有打开的 Step | Step 打开时就 `turn/end` | `turn/end while step still open` |
| `step/start` 要求有打开的 Turn | Turn 外就 `step/start` | `step/start without open turn` |
| `step/start` 要求没有打开的 Step | 两个 `step/start` 之间没有 `step/end` | `step/start while step already open` |
| `step/end` 要求有打开的 Step | 没有 `step/start` 就 `step/end` | `step/end without open step/start` |
| `tool/call` 要求有打开的 Step | Step 外就 `tool/call` | `tool/call without open step` |
| Turn 编号严格递增 | `turn/end {turn: 0}` 之后 `turn/start {turn: 0}` | `turn/start expected turn 1, got 0` |
| Step 编号严格递增 | `step/end {step: 1}` 之后 `step/start {step: 1}` | `step/start expected step 2, got 1` |
| Step 的 turn 匹配打开的 turn | 打开的 turn 是 0 时 `step/start {turn: 5}` | `step/start in turn 5 but open turn is 0` |

所有这些都在 `SessionLog.applyState()` 中强制执行——这是唯一的写入路径。没有任何事件能绕过这些检查。

## TurnEndReason：完整词汇表

Turn 可以因六种原因关闭：

| 原因 | 含义 | 典型触发 |
|---|---|---|
| `completed` | Turn 正常解决 | Agent 产生了最终答案且没有工具调用 |
| `interrupted` | Turn 在执行中被中断 | 上下文取消、用户中止 |
| `aborted` | Turn 被故意中止 | `Agent.Cancel()` 带取消原因 |
| `blocked` | Turn 被安全门阻止 | 审批被拒绝、沙箱升级被拒绝 |
| `error` | Turn 因内部错误失败 | LLM API 错误、工具执行 panic |
| `max-tokens` | Turn 达到 Token 限制 | LLM 返回 `finish_reason: length` |

```go
// pkg/session/session.go
const (
    ReasonCompleted   TurnEndReason = "completed"
    ReasonFinished    TurnEndReason = "finished"    // 旧字段别名，等同于 completed
    ReasonInterrupted TurnEndReason = "interrupted"
    ReasonAborted     TurnEndReason = "aborted"
    ReasonBlocked     TurnEndReason = "blocked"
    ReasonError       TurnEndReason = "error"
    ReasonMaxTokens   TurnEndReason = "max-tokens"
)
```

> **注意：** `finished` 作为旧字段别名保留以保证向后兼容。新代码应使用 `completed`。

## 状态投影：查询循环状态

`SessionLog` 内部维护完整的循环状态，并暴露只读查询方法：

```go
// 下一个 turn 编号（从 0 开始，每次 turn/end 后递增）
func (sl *SessionLog) NextTurn() uint64

// 当前 turn 内下一个 step 编号（从 1 开始，每次 turn/start 时重置）
func (sl *SessionLog) NextStep() uint64

// 当前打开的 turn（如果没有打开的 turn，返回 0, false）
func (sl *SessionLog) OpenTurn() (uint64, bool)

// 当前打开的 step（如果没有打开的 step，返回 0, false）
func (sl *SessionLog) OpenStep() (uint64, bool)
```

### Agent 如何使用它们

Agent 在创建 `turn/start` 事件之前查询 `NextTurn()`，确保编号始终正确：

```go
// pkg/agent/agent.go — runTurn
func (a *Agent) runTurn(req *turnReq) {
    turnIdx := a.log.NextTurn()  // 查询当前编号

    if _, err := a.log.Append(session.TurnStartData{Turn: turnIdx}); err != nil {
        return
    }
    // ... steps ...
    a.log.Append(session.TurnEndData{Turn: turnIdx, Reason: session.ReasonFinished})
}
```

这种模式——**追加前先查询**——是确保严格单调编号的规范方式，无需在 Agent 中维护单独的计数器。

## 崩溃修复：孤儿 Turn

如果进程在 Turn 中途崩溃，持久化的日志将包含一个 `turn/start` 而没有匹配的 `turn/end`。持久层的 `repairOrphanTurn` 函数会检测到这种情况，并追加一个合成的 `turn/end {reason: interrupted}`：

```go
// pkg/persistence/jsonl.go — repairOrphanTurn
func repairOrphanTurn(events *[]session.SessionEvent) int {
    // 扫描所有事件，判断末尾是否有打开的 turn
    turnOpen := false
    var openTurn uint64
    for _, ev := range *events {
        switch ev.Type {
        case session.EventTurnStart:
            turnOpen = true
            if td, ok := ev.Data.(session.TurnStartData); ok {
                openTurn = td.Turn  // 捕获 turn 编号
            }
        case session.EventTurnEnd:
            turnOpen = false
        }
    }
    if !turnOpen {
        return 0
    }
    // 追加合成的 turn/end，携带正确的 turn 编号
    last := (*events)[len(*events)-1]
    repaired := session.SessionEvent{
        Seq:  last.Seq + 1,
        Time: last.Time,
        Type: session.EventTurnEnd,
        Data: session.TurnEndData{Turn: openTurn, Reason: session.ReasonInterrupted},
    }
    *events = append(*events, repaired)
    return 1
}
```

修复保留了正确的 `turn` 编号——它不是简单地追加一个裸的 `turn/end`。这确保修复后的日志仍然通过所有编号不变量。

## 与其他子系统的交互

### 工具流水线

每个 `tool/call` 和 `tool/result` 都必须发生在打开的 Step 内部。工具流水线本身不管理 Turn/Step——它依赖 Agent 在调用工具之前已经打开了适当的 Step。

### 审批

审批请求（`approval/request`、`approval/decided`）可以发生在 Step 内部。审批系统不强制 Turn/Step 嵌套——那是 session 不变量的职责。

### 持久化

JSONL 后端原样持久化所有 Turn/Step 事件。加载时，它运行 `repairOrphanTurn` 来修复任何崩溃导致的孤儿。分片/异步后端（H02）批量处理事件但保留顺序和编号。

### 取消

`Agent.Cancel(cause)` 记录取消原因，Turn 以 `aborted` 关闭（如果取消来自上下文则为 `interrupted`）。取消原因通过 `ExtractCancelCause(events)` 提取，扫描带有取消标记的 `turn/stopping` 事件。

## 好处与代价

### 好处

- **确定性重放** — 任何日志都可以被重放以重建精确的循环状态
- **早期 Bug 检测** — 编号不匹配在写入时就捕获生产者 Bug，而不是几小时后
- **清晰的取消边界** — Turn 边界给出了自然的中止点，不会损坏状态
- **精确的指标** — 按 Turn 和按 Step 的 Token/延迟统计变得微不足道
- **崩溃可恢复** — 孤儿 Turn 可以被确定性地检测和修复

### 代价

- **生产者复杂度** — 每个事件创建者都必须知道正确的 turn/step 编号
- **不支持乱序写入** — 事件必须按严格的循环顺序追加；不能稍后"回填"一个 Step
- **迁移负担** — 更改编号方案（例如 Step 从 0 而不是 1 开始）需要完整的日志迁移
- **测试面** — 每个创建 Turn/Step 事件的测试都必须使用正确的编号，否则不变量会拒绝它们

## 源码对照

| 概念 | Go 实现 | 官方 TypeScript |
|---|---|---|
| 事件数据结构 | `pkg/session/session.go` — `TurnStartData`、`TurnEndData`、`StepStartData`、`StepEndData` | `packages/core/session/src/types.ts` — `SessionEventMap` |
| TurnEndReason | `pkg/session/session.go` — `TurnEndReason` 常量块 | `packages/core/session/src/types.ts` — `TurnEndReasonMap` |
| 配对与嵌套不变量 | `pkg/session/session.go` — `applyState()` | `packages/core/session/src/invariant.ts` — `validateEvent()` |
| 状态计数器 | `pkg/session/session.go` — `sessionState.nextTurn/nextStep/openTurn/openStep` | `packages/core/session/src/invariant.ts` — `SessionTrace` |
| 查询方法 | `pkg/session/session.go` — `NextTurn()`、`NextStep()`、`OpenTurn()`、`OpenStep()` | （官方未暴露；内部维护） |
| Agent 循环 | `pkg/agent/agent.go` — `runTurn()`、`runStep()` | `packages/core/agent/src/` — agent 循环 |
| 崩溃修复 | `pkg/persistence/jsonl.go` — `repairOrphanTurn()` | `packages/core/session/src/repair.ts` |

## 下一步

- **[沙箱与受控执行](./sandbox-execution)** — Step 打开后工具调用如何被限制
- **[Session 事件溯源](./session-event-sourcing)** — 完整的事件词汇表和不变量系统
- **[Agent 循环与取消](./agent-loop)** — Agent 如何驱动 Turn/Step 循环和处理取消
