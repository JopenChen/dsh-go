---
title: "工具执行流水线"
description: "一次工具调用从模型发出到结果返回经过的固定环节、三态决策与单调守卫"
weight: 41
---

# 工具执行流水线

## 一句话总结

**模型发出一条工具调用后，并不是"直接执行函数"那么简单，而是按固定顺序经过 pre-execute → 单调守卫 → execute → post-execute → result 多个环节，每个环节承载一类策略；dsh-go 用四级 waterfall 链承载这些环节，并用三态决策和单调守卫保证安全策略只能收紧、不能被绕过。**

## 流水线的整体顺序

官方把顺序概括为：`tools/pre-execute → 单调守卫 → tools/execute → tools/post-execute → tools/result`。dsh-go 的 `pkg/tools.Pipeline` 用四条独立的 waterfall 链对应：

```
ToolCallRequest
   │
   ├─ pre-execute   拦截/改写入参（权限、沙箱、钩子），可 deny 短路
   ├─ 单调守卫       最终防线：决策只收紧不放宽
   ├─ execute       真正调用工具实现（可换 signal=cancel）
   ├─ post-execute  结果后处理（accept/block、截断、附加 meta）
   └─ result        最终加工（统一包装、打点）
   │
ToolCallResult
```

前三个可插拔环节都是 waterfall：监听器可以调用 `next()` 把决定权委托下去，也可以直接返回决策短路。多个策略插件的先后顺序可以在装配时调整，因此 pre-execute 被称为"可重排的策略层"。

### 各环节职责

| 环节 | 职责 | 短路后果 |
|---|---|---|
| pre-execute | 承载钩子、权限、沙箱这类可重排策略 | deny → 工具不执行，结果 `IsError` |
| execute | 包裹真实工具实现，透传取消/超时 ctx | signal=cancel → 结果标记错误 |
| post-execute | accept/block、截断、写审计 meta | block → 结果 `IsError` |
| result | 统一包装、指标采集 | — |

## 三态决策：allow / deny / ask

pre-execute 阶段返回一个类型化决策 `PreToolDecision`，有三种取值：

| 决策 | 含义 | 后续行为 |
|---|---|---|
| `PreAllow` | 放行本次调用 | 继续进入 execute |
| `PreDeny` | 拒绝 | 短路，工具不执行，结果 isError |
| `PreAsk` | 询问用户 | 由 AskFunc 请求用户，准许才继续 |

关键语义是 **allowed-once**：`ask` 场景下用户"准许"只放行**这一次调用**，下一次同一工具被调用时仍然会继续 ask，系统不做"按工具名永久放行"。这避免了用户一次点头、之后该工具被无限次自动调用的风险。

```go
mw := tools.PreDecisionMiddleware(decide, ask)
pipe := tools.NewPipeline().UsePre(mw).WithTool(tool)
res := pipe.Run(ctx, req, tool)
```

dsh-go 复用了 `pkg/approval.Decision` 作为三态载体，因此工具决策与审批服务共享同一套语义，可以由 `approval.Service.Evaluate` 直接映射而来。

## 单调守卫：只收紧、不放宽

pre-execute 是可重排的策略层，多个策略插件都可能给出决策——那如果靠前的策略说 allow、靠后的策略说 deny，或者反过来，该听谁的？这就是**单调守卫**存在的意义，它是夹在策略层与真实执行之间的最终防线。

`pkg/tools.MonotonicGuard` 给三态定义严格度偏序：**deny > ask > allow**：

- 首次赋值总是接受；
- 后续决策只允许朝"更严格"方向收敛（allow→deny、ask→deny 都可以）；
- 试图把已经更严的决策放宽（deny→allow）→ 返回 `ErrGuardRelaxed`，且**保持原决策不变**。

```go
g := tools.NewMonotonicGuard()
_ = g.Update(tools.PreAllow)
_ = g.Update(tools.PreDeny)              // 收紧，接受
err := g.Update(tools.PreAllow)         // 放宽，拒绝 → ErrGuardRelaxed
// g.Decision() 仍是 PreDeny（fail closed）
```

这是一条典型的 fail-closed 设计：安全策略的优先级永远高于便利策略，任何想"撤销"一个已成立的拒绝的尝试都会被明确拒绝，而不是被静默放行。

## 分层工具掩码

除了单次调用的决策，工具在"能不能被看到/选用"这一层还有 `Restriction` 掩码。`RestrictionSet` 以有序层栈（host 最外、作用域越近越优先）承载多层限制，解析采用 **nearest-scope-wins**：

- 从最近层向 host 层回溯，第一个提及该工具的层决定结果；
- `host deny + scope allow(exempt)` → 工具恢复可用；
- 两层都 deny → 拒绝；所有层都未提及 → 默认放行。

它服务于 Subagent 父限子能力、Preset 隐藏工具等场景，`Filter` 直接按掩码过滤工具列表。

## 源码对照

| 概念 | Go 实现 | 官方 TypeScript |
|---|---|---|
| 四级流水线 | `pkg/tools/pipeline.go` — `Pipeline` | `packages/core/tools` |
| 三态决策 | `pkg/tools/predecision.go` — `PreDecisionMiddleware` | `packages/core/tools/pre-execute` |
| 单调守卫 | `pkg/tools/monotonic.go` — `MonotonicGuard` | tools monotonic guard |
| 分层掩码 | `pkg/tools/restriction.go` — `RestrictionSet` | `packages/core/tools/restriction` |
| 对象池 | `pkg/tools/pooled.go` — `SetPooled` | （dsh-go 性能增强） |

## 下一步

- **[插件内核与事件系统](./plugin-kernel)** — waterfall 与其他三种分发模式的区别
- **[沙箱与受控执行](./sandbox-execution)** — pre-execute 中沙箱策略如何落地
- **[能力三角色与 LLM 适配器](./capability-seams)** — 工具能力如何被定义与替换
