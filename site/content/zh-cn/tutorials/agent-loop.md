---
title: "Agent 循环与运行时"
description: "Agent 如何驱动 Turn/Step 双循环，管理状态、收件箱、取消和请求错误恢复"
weight: 35
---

# Agent 循环与运行时

## 一句话总结

**Agent 是将 SessionLog 转化为活的对话的运行时驱动者——它拥有 Turn/Step 双循环、生命周期状态（idle/running）、用于待处理工作的双队列 Inbox、取消语义和请求错误恢复——所有这些都表达为持久化的 session 事件。**

## Agent 拥有什么

Agent 位于 **SessionLog**（持久化真相）和 **LLM/工具**（外部服务）之间。它拥有五个运行时关注点：

| 关注点 | 回答的问题 | 关键类型 |
|---|---|---|
| **Turn/Step 循环** | 用户请求如何变成 LLM 调用 + 工具执行？ | `runTurn()`、`runStep()` |
| **生命周期状态** | Agent 是忙碌还是静止？ | `AgentStatus`（`idle`/`running`）、`agent/status` 事件 |
| **Inbox** | 有什么工作在排队等待下一个 turn/step？ | `Inbox`（next-turn + next-step 队列） |
| **取消** | 如何干净地中止活动的 turn？ | `CancelCause`（5 种）、`CancelOptions` |
| **请求恢复** | LLM 请求失败时会发生什么？ | `RequestErrorAction`（`retry`/`abort`）、`RequestErrorWaterfall` |

## AgentOptions：不可变配置

在构造时创建一次，运行时永不改变：

```go
type AgentOptions struct {
    Provider         string        // provider 路由（如 "deepseek"）
    Model            string        // 模型 ID（如 "deepseek-chat"）
    ReasoningEffort  string        // 适配器拥有的推理级别
    MaxTokens        int           // 每次请求的最大输出 token 数
    DefaultTimeout   time.Duration // 默认运行超时（0 = 无）
}
```

```go
agent := NewAgent(id, log, sys, pipeline, adapter, AgentOptions{
    Provider: "deepseek",
    Model:    "deepseek-chat",
    MaxTokens: 4096,
})
```

> Persona 和 system-prompt 部分属于 `pkg/sysprompt`，不在 AgentOptions 中。

## AgentStatus：生命周期状态机

Agent 恰好有两个可观察状态，在每次转换时通过 `agent/status` 事件发出：

```
         turn/start（runTurn 开始）
    ┌──────────────────────────────┐
    │                              ▼
  idle                          running
    ▲                              │
    └──────────────────────────────┘
         turn/end（runTurn 返回）
```

| 状态 | 含义 | 何时进入 |
|---|---|---|
| `idle` | 没有活动的驱动程序 | 初始状态；每次 `runTurn` 返回后 |
| `running` | 驱动程序正在主动处理 | 在 `runTurn` 开始时（在 `turn/start` 之前） |

```go
// 查询当前状态
status := agent.Status()  // "idle" 或 "running"

// 等待静止（阻塞直到 idle）
err := agent.WhenIdle(ctx)
```

### WhenIdle：等待静止

`WhenIdle(ctx)` 在当前整个 Agent 活动达到静止后解析。如果已经是 idle，则立即返回。这适用于：

- 需要断言最终状态的测试
- 优雅关闭（等待活动 turn 完成）
- 批量处理（等待一个 turn 完成后再开始下一个）

```go
agent.Run("做某事")
if err := agent.WhenIdle(context.Background()); err != nil {
    // 上下文已取消
}
// agent 现在是 idle，session 日志已完成
```

## Inbox：双队列待处理工作

Inbox 是 Agent 拥有的持久化待处理消息的投影。它维护**两个有序列表**：

| 队列 | 用途 | 消费时机 |
|---|---|---|
| `next-turn` | 等待各个 turn 的提示 | Turn 边界（每个 turn 消费一个） |
| `next-step` | 等待下一个 step 边界的输入 | Step 边界（一次性全部消费） |

### 为什么需要两个队列？

用户请求可能在 **turn 中途**到达（Agent 正在处理时）。与其中断活动的 turn，不如将消息排队：

- **next-step**：在下一个 step 边界消费——LLM 在下一个 step 就能看到它
- **next-turn**：仅在当前 turn 结束时消费——它成为下一个用户请求

这种分离使得**引导**（mid-turn 输入）成为可能，而不会违反 turn 边界。

### 核心操作

```go
inbox := agent.Inbox()

// 检查是否有待处理工作
inbox.HasPending()  // 如果任一队列非空则为 true

// 推送消息到 next-step（引导输入）
inbox.Push(log, session.InboxNextStep, UserMessage{Content: "也检查一下测试"})

// 推送消息到 next-turn（下一个用户请求）
inbox.Push(log, session.InboxNextTurn, UserMessage{Content: "现在做这个"})

// 认领一个 step 的批次（移除并返回 next-step + 可选的一个 next-turn）
batch := inbox.Claim(log, session.InboxNextTurn, turnIdx)

// 清除所有待处理工作（取消时）
inbox.Clear(log)
```

### 持久性：agent/inbox/spliced 事件

每次 Inbox 变更都持久化为一个 `agent/inbox/spliced` 事件：

```go
type InboxSplicedData struct {
    Target       InboxTarget // "next-turn" 或 "next-step"
    Start        int         // 拼接位置（-1 = 追加）
    RemovedCount int         // 移除的数量
    Inserted     []string    // 插入的内容
    Outcome      string      // 取消清除时为 "canceled"
}
```

在 Agent 构造时，Inbox **重放所有历史 spliced 事件**来重建其状态。这意味着 Inbox 是完全可崩溃恢复的。

## 取消：五种原因，一种机制

Agent 支持五种取消原因（与官方 `AgentCancelCause` 对齐）：

| 原因 | 含义 | 典型触发 |
|---|---|---|
| `user` | 用户主动取消 | 用户点击"停止" |
| `parent` | 父代理取消（subagent 场景） | 父 turn 结束 |
| `hook` | Hook 拒绝触发取消 | Pre-step hook 返回 reject |
| `disposed` | Agent 被释放 | `agent.Dispose()` |
| `legacy` | 历史/兼容性路径 | 旧的取消代码 |

```go
// 取消活动的 turn
agent.Cancel(CancelUser)

// 取消但保留排队的 inbox 项
agent.Cancel(CancelUser)  // 配合 CancelOptions{KeepInbox: true}
```

### 取消做了什么

1. 通过 `turn/stopping` 事件记录取消原因
2. 活动的 `runTurn` 检测到取消（通过 context）并中止
3. Turn 以 `reason: interrupted` 关闭（或显式取消时为 `aborted`）
4. 默认情况下，Inbox 被清除（带 `outcome: canceled`）
5. 使用 `KeepInbox: true` 时，排队的项会保留供后续 turn 使用

## PreStepDecision：Step 入口把关

在进入提议的 step 之前，监听器可以返回一个 `PreStepDecision`：

```go
type PreStepDecision struct {
    Kind               string        // "reject" 或 "enter"
    Messages           []UserMessage // 携带的消息（仅 enter）
    StartsRequestSeries bool         // 启动新的 model-message 系列
}
```

| 决策 | 效果 |
|---|---|
| `reject` | 不进入这个 step；turn 可能提前关闭 |
| `enter` | 携带指定消息进入 step |

这是 **hooks**、**审批门**和**引导逻辑**的扩展点，它们需要在 LLM 看到输入之前检查或修改输入。

## 请求错误恢复：重试瀑布

当 LLM 请求失败时，Agent 运行一个**请求错误瀑布**来决定动作：

```
LLM 请求失败
    ↓
RequestErrorWaterfall（中间件链）
    ├─ 中间件 1：检查错误类型 → 重试？
    ├─ 中间件 2：检查重试次数 → 中止？
    └─ ...
    ↓
TerminalRetryDecision（兜底）
    ├─ 可重试错误 + 未超上限 → 重试
    └─ 否则 → 中止（turn 以 error 关闭）
```

```go
type RequestErrorAction struct {
    Kind   RequestErrorActionKind // "retry" 或 "abort"
    Reason string                 // 审计原因
}

// 构建带自定义中间件的瀑布
chain := NewRequestErrorWaterfall(
    func(ctx context.Context, p *RequestErrorPayload, next func()) {
        if p.RetryableError && p.RetryCount < 3 {
            p.Action = &RequestErrorAction{Kind: ActionRetry, Reason: "自定义重试策略"}
            return  // 短路
        }
        next()
    },
)

// 解析最终动作
action := ResolveRequestError(chain, payload)
```

### 可重试错误

只有 **overload** 和 **rate-limit** 错误是可重试的（由 `llm.ClassifyLlmError` 分类）。其他错误（auth、无效请求等）立即中止。

## ConsumedWork：结算被消费的工作

只看 turn/end 会混淆两类外形相同的 turn："在首个 step 前就中止的 turn"和"被拒绝/空 claim 产生的 no-op turn"——要么把半途而废误记为完成，要么冤枉每个 no-op。

`agent.FoldConsumedWork` 单遍折叠日志，给出一份结算：

- **End**：最后一个真正结算了工作的 turn——它进入过模型 step，或 claim 了输入后失败/被阻止；
- **DroppedUnrun**：是否有工作在取消时被丢弃、从未运行（任何 turn 都来不及打开）。

判定规则 `accountsForClaim`：一个 claim 了输入却没走到 step 的 turn，只有 `completed` 不结算（claim 被改写后已无东西可跑），`blocked/aborted/error/interrupted` 都算那份输入的结局。这份账让"干了活"与"丢了活"在取消路径上也能被准确区分。

## 模型选择双快照：切换不撕裂

模型可以在运行期切换，但"prompt 装配面"与"请求路由面"不能各用一个模型。`agent.ModelSelectionRef` 用双快照保证一致性：

- `Select` 更新下一步要用的 `current`；
- `Capture` 在一次 step 的 prompt 装配边界把 current 冻结为 `assembled`；
- `Apply` 在发请求时用 assembled 覆盖 provider/model/effort。

于是一次并发切换只在**后续 step** 生效，当前 step 从装配到请求始终用同一个模型。捕获选择未带推理努力时，`Apply` 会清掉继承的努力以恢复所选模型的默认行为。

## 与其他子系统的交互

### SessionLog

Agent 是 SessionLog 的**主要写入者**。每次运行时转换——turn/start、step/start、agent/request、tool/call、agent/status、agent/inbox/spliced——都是一个持久化事件。Agent 从不直接改变状态；它追加事件并让投影派生状态。

### LLM 适配器

Agent 在 `runStep()` 内部调用适配器的 `Stream()` 方法。适配器基于 `AgentOptions.Provider` 选择。请求错误流入 RequestErrorWaterfall。

### 工具流水线

在 LLM 响应中发现的工具调用通过四级流水线执行（resolve → approve → sandbox → execute）。流水线在构造时注入。

### 沙箱

沙箱决定工具副作用可以发生在**哪里**。Agent 不直接与沙箱交互——这是工具流水线的职责。

### 审批

审批决定工具**能否**运行。与沙箱一样，这在工具流水线内部处理，而不是由 Agent 直接处理。

## 好处与代价

### 好处

- **事件溯源运行时**——每次状态转换都是持久化且可重放的
- **干净的取消**——turn 边界给出自然的中止点；inbox 可以保留或清除
- **不中断的引导**——next-step 队列让用户可以在 turn 中途注入输入
- **状态可观察性**——idle/running 是一等事件，不是派生猜测
- **可组合的错误恢复**——瀑布中间件让插件可以自定义重试逻辑

### 代价

- **两个队列需要推理**——next-turn vs next-step 很微妙；误用可能导致消息被延迟或丢失
- **状态是粗粒度的**——只有 idle/running；没有"等待工具"或"流式传输"子状态
- **取消是协作式的**——Agent 必须轮询 context；卡住的工具调用可能不会及时取消
- **Inbox 重放开销**——构造 Agent 时重放所有 spliced 事件；非常长的会话有 O(n) 构造开销

## 源码对照

| 概念 | Go 实现 | 官方 TypeScript |
|---|---|---|
| Agent 核心循环 | `pkg/agent/agent.go` — `runTurn()`、`runStep()` | `packages/core/agent/src/index.ts` |
| AgentOptions | `pkg/agent/options.go` — `AgentOptions` | `packages/core/agent/src/runtime-types.ts` — `AgentOptions` |
| AgentStatus | `pkg/agent/options.go` — `AgentStatus`；`pkg/session/session.go` — `AgentStatusData` | `packages/core/agent/src/runtime-types.ts` — `AgentStatus` |
| WhenIdle | `pkg/agent/agent.go` — `WhenIdle()` | `packages/core/agent/src/runtime-types.ts` — `whenIdle()` |
| Inbox | `pkg/agent/inbox.go` — `Inbox` | `packages/core/agent/src/inbox.ts` — `Inbox` |
| Inbox 事件 | `pkg/session/session.go` — `InboxSplicedData`、`InboxTarget` | `packages/core/agent/src/types.ts` — `agent/inbox/spliced` |
| 取消 | `pkg/agent/cancel.go` — `CancelCause`、`RecordCancel` | `packages/core/agent/src/runtime-types.ts` — `cancel()` |
| PreStepDecision | `pkg/agent/options.go` — `PreStepDecision` | `packages/core/agent/src/runtime-types.ts` — `PreStepDecision` |
| 请求错误 | `pkg/agent/requesterror.go` — `RequestErrorWaterfall` | `packages/core/agent-loop/` — request-error waterfall |
| Initiator | `pkg/agent/initiator.go` — `Initiator` | `packages/core/agent/src/index.ts` — initiator tracking |
| ConsumedWork | `pkg/agent/consumed.go` — `FoldConsumedWork` | `packages/core/agent/src/consumed-work.ts` |
| 模型选择 | `pkg/agent/model_selection.go` — `ModelSelectionRef` | `packages/core/agent/src/model-selection.ts` |

## 下一步

- **[Turn / Step 双循环](./turn-step-loop)**——详细的嵌套循环结构
- **[沙箱与受控执行](./sandbox-execution)**——工具副作用被限制在哪里
- **[Session 事件溯源](./event-sourcing)**——驱动一切的持久化事件日志
