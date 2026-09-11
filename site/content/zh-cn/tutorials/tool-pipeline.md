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

## PTC 模式：让模型写程序组合工具

标准模式下模型逐个发出工具调用，每调用一次走一遍流水线。官方还提供 **PTC（Programmatic Tool Calling）模式**：模型不再逐个调用，而是写一段程序（async 函数体），程序内部通过 `tools.name(args)` 组合多步调用，最后只把精选结果返回。

```
标准模式：模型 → toolA → 模型 → toolB → 模型 → 汇总（多轮）
PTC 模式：模型 → run_code(一段程序) → 程序内连续调用 toolA/toolB → 一次返回
```

dsh-go 的对应实现分两层：

- **`pkg/coderuntime` 固化执行缝契约**：`RunRequest`（程序 + 绑定命名空间）、`RunResult`（完成值 + logs + 六类失败）、`Runtime` 接口。Go 进程内没有 JS 引擎，因此不内置语言后端，由使用方实现接口（外部进程 / 嵌入式解释器 / 远程服务）。
- **`pkg/tools.NewRunCodeTool` 做桥接**：把当前工具集映射为一个 `"tools"` 绑定命名空间，每个成员在被程序调用时仍走同一条工具流水线——因此权限、沙箱、单调守卫对程序内调用**同样生效**，不会因为换了调用方式就绕过安全策略。

关键语义是**子调度留痕、外层结果入史**：程序内每次工具调用都可被记录用于重建，而只有外层 run_code 的精选结果进入模型历史，避免一段程序把几十条中间结果灌爆上下文。

### 为什么 Go 不内置代码引擎？

PTC 上游唯一发布的后端是 Node 工作线程执行 TypeScript，这是 JS 生态的天然能力。强行在 Go 里嵌入 JS 引擎既笨重又脆弱。dsh-go 的取舍是**复刻协议与桥接语义、把执行后端留成可替换接口**——需要 PTC 时接入合适的 Runtime，不需要时这层抽象零成本。

## 源码对照

| 概念 | Go 实现 | 官方 TypeScript |
|---|---|---|
| 四级流水线 | `pkg/tools/pipeline.go` — `Pipeline` | `packages/core/tools` |
| 三态决策 | `pkg/tools/predecision.go` — `PreDecisionMiddleware` | `packages/core/tools/pre-execute` |
| 单调守卫 | `pkg/tools/monotonic.go` — `MonotonicGuard` | tools monotonic guard |
| 分层掩码 | `pkg/tools/restriction.go` — `RestrictionSet` | `packages/core/tools/restriction` |
| PTC 执行缝 | `pkg/coderuntime/coderuntime.go` — `Runtime` | `packages/code-runtime` |
| run_code 桥 | `pkg/tools/ptc.go` — `NewRunCodeTool` | `packages/core/tools/src/ptc.ts` |
| 对象池 | `pkg/tools/pooled.go` — `SetPooled` | （dsh-go 性能增强） |

## 下一步

- **[插件内核与事件系统](./plugin-kernel)** — waterfall 与其他三种分发模式的区别
- **[沙箱与受控执行](./sandbox-execution)** — pre-execute 中沙箱策略如何落地
- **[能力三角色与 LLM 适配器](./capability-seams)** — 工具能力如何被定义与替换
