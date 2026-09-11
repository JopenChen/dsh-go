---
title: "能力三角色与 LLM 适配器"
description: "Definition / Provider / Consumer 构成的可替换能力缝，以及如何接入任意模型端点"
weight: 42
---

# 能力三角色与 LLM 适配器

## 一句话总结

**官方每一项可替换能力都由三个角色构成：Definition（定义接口契约）、Provider（提供具体实现）、Consumer（按契约消费而不依赖实现）；LLM 能力是最典型的例子——通过统一的 LLMAdapter 接口与 StreamChunk 流式协议，dsh-go 可以接入任意 OpenAI 兼容模型端点而不改动上层循环。**

## 能力三角色

官方文档里反复出现 Service Definition、Service Provider、Consumer 三个词，它们合在一起构成一项能力的 **seam（缝）**——也就是可以在不改动消费方的前提下替换实现的接口边界：

| 角色 | 职责 | dsh-go 中的对应 |
|---|---|---|
| Definition | 定义能力的接口与数据契约 | Go `interface` + 请求/结果结构体 |
| Provider | 给出一个具体实现并注册 | 实现接口，在装配时 `reg.Put` |
| Consumer | 按 key/接口查找并使用，不导入实现 | `reg.Get` 或构造时注入接口 |

关键约束是**消费方依赖抽象而非具体实现**：Agent 循环只知道自己拿到了一个 `LLMAdapter`，不关心背后是 DeepSeek、本地模型还是一个测试桩。正因如此，替换模型提供方、在测试里注入假实现，都不需要改动 Agent 一行代码。

```go
// Definition：接口即契约
type LLMAdapter interface {
    Name() string
    Chat(ctx context.Context, req ChatRequest, cb func(StreamChunk)) (Usage, error)
}
// Consumer：只依赖接口
func NewAgent(llm LLMAdapter) *Agent { ... }
// Provider：任意实现该接口的具体适配器
```

## LLM 适配器：接入任意模型

模型提供方不止一家。想把 Agent 接到自己的模型端点，只需实现 `pkg/llm.LLMAdapter` 两个方法：

- `Name()` 返回适配器标识（如 `"deepseek"`）；
- `Chat(ctx, req, cb)` 发起一次**流式**对话，每个分片通过 `cb` 回调，最终返回本次用量 `Usage`。

适配器负责把 dsh-go 统一的 `ChatRequest`（消息列表、工具声明、采样参数）翻译成目标端点的线格式，再把端点返回的 SSE 流翻译回统一的 `StreamChunk`。上层因此完全模型无关，模型路由也无需重启。

## StreamChunk 流式协议

`Chat` 的 `cb` 到底往外出什么数据？`pkg/llm.StreamChunk` 用一个 `Kind` 字段区分四种分片：

| Kind | 载荷含义 |
|---|---|
| `ChunkText` | 正文增量（逐 token 拼接） |
| `ChunkReasoning` | 思维链增量 |
| `ChunkToolCall` | 工具调用增量（参数可能分片到达） |
| `ChunkDone` | 本次流结束 |

```go
_, _ = llm.Chat(ctx, req, func(c llm.StreamChunk) {
    switch c.Kind {
    case llm.ChunkText:
        ui.Append(c.Text)
    case llm.ChunkToolCall:
        // 累积工具调用，可能需要跨分片拼接参数
    }
})
```

### 流中的错误处理

流式场景的错误比一次性请求更隐蔽：连接可能在中途断开、某个分片可能畸形。dsh-go 的处理原则是：

- **分片级容错**：单个分片解析失败不应让已收到的正文丢失，记录错误并决定是否继续；
- **错误链**：`pkg/llm/errorchain.go` 把底层网络错误、HTTP 状态错误、上下文取消统一包装为可判别的链，可用 `errors.Is/As` 区分"可重试"与"确定性失败"；
- **重试**：`pkg/llm/retry.go` 只对可重试错误（超时、429、5xx）退避重试，确定性失败（4xx 鉴权错误）立即返回，不做无意义重试；
- **缓存**：`pkg/cache` 对确定性请求提供命中，降低重复调用成本与延迟。

## 为什么三角色很重要？

这套结构是"一切皆插件、能力可自由替换"的落地方式。它的收益：

- **可替换**：换 Provider 不动 Consumer，模型/工具/存储皆然；
- **可测试**：测试里注入实现接口的桩，即可离线验证整个 Agent 循环；
- **可组合**：多个 Provider 可以被更上层的路由适配器聚合，按场景选择。

代价是前期要先想清楚接口契约——Definition 设计得不稳，所有 Consumer 都会跟着改。

## 源码对照

| 概念 | Go 实现 | 官方 TypeScript |
|---|---|---|
| 适配器契约 | `pkg/llm/llm.go` — `LLMAdapter` | `packages/core/llm` adapter |
| 流式协议 | `pkg/llm/llm.go` — `StreamChunk`/`StreamChunkKind` | stream chunk 协议 |
| 错误链 | `pkg/llm/errorchain.go` | （dsh-go 增强） |
| 重试 | `pkg/llm/retry.go` | （dsh-go 增强） |
| 能力注册 | `pkg/registry/registry.go` — `Freezable` | Cordis service 注册 |

## 下一步

- **[工具执行流水线](./tool-pipeline)** — LLM 发出工具调用后如何被执行
- **[插件内核与事件系统](./plugin-kernel)** — Provider 如何被注册与查找
- **[Agent 循环与运行时](./agent-loop)** — Consumer 侧如何驱动整个循环
