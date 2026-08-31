# FAQ · 常见问题

> 本文回答关于 **dsh-go** 定位、价值、以及它与主流 Agent 框架关系的高频问题。
> 定位：参考实现与学习素材（详见 [README](../README.md)）。

---

## Q1. dsh-go 到底是什么？

一句话：**一份纯 Go、进程内的 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) Agent 参考实现**。

它把官方 DSH 的 Turn/Step 双循环、事件溯源、Goal 规划、工具治理等核心能力接缝，逐词对译为 Go 代码。不是"又一个 ReAct 骨架"，而是对官方语义体系的一套**可阅读、可调试、可复刻**的翻译。

## Q2. 它和 Eino / LangChain / LangGraph / CrewAI 是什么关系？

它们是**不同物种**，不是同层级替代关系：

| 项目 | 本质 | 目标 |
|---|---|---|
| Eino（字节）/ LangChain·LangGraph | 通用 **LLM 应用编排框架** | 用代码编排任意 LLM 应用（Agent / RAG / Workflow） |
| 官方 DeepSeek Harness | DeepSeek 官方的 Agent 实现（TS） | DeepSeek 官方 Agent 产品 |
| **dsh-go（本文）** | 对 DSH 的 **Go 参考实现** | 让开发者**读懂** Agent 内核 |

一句话区分：**Eino/LangChain 是"拿来用的框架"，dsh-go 是"拿来读的参考实现"。** 它不打算、也没能力替代前者。

## Q3. 那 dsh-go 相对 Eino/LangChain 有什么优势？

优势不是"可用性/生态"，而是**三块非对称的硬实力**：

1. **与官方语义逐词对齐**：53 种事件、`LlmFailure` 稳定分类、Goal 四态（active/paused/blocked/complete）+ 9 个 `GOAL_` 稳定错误码、工具 presentation，都对着官方 `error.ts` / `types.ts` 复刻。若你认同 DSH 语义，迁移理解几乎为零。
2. **事件溯源 + fold 派生投影（结构性差异）**：会话、Goal、Todo、计划全部由 append-only 事件日志派生；增量 fold 做到 O(N) 读投影（相对 O(N²) 约 3437×）。这是 Eino 的"状态快照"模型不具备的。
3. **前缀缓存亲和（ DeepSeek 降本）**：为 DeepSeek 前缀缓存做了探针、反模式 AST 扫描、命中率 E2E 验收、破窗告警、OTel/Grafana 指标。通用框架不会为单一 provider 的缓存定价挖到这么深。

外加工具治理纵深（四级 Waterfall + PreToolDecision + sandbox + approval + token deny）与生产级并发加固。

## Q4. 用成熟的 Agent 框架（Eino/LangChain）做不到合规审计吗？

**能做到。** "能不能审计"不是 dsh-go 的独占能力——成熟框架靠回调/中间件、Checkpoint 持久化、外部落库完全可以做"外审计"。

真正的差异在**能力归属与证据强度**：

| | 成熟框架 | dsh-go |
|---|---|---|
| 审计是内建还是外挂 | 外挂（回调/中间件/业务自行落库） | 内建（append-only 事件即 source of truth） |
| 能否重放历史中间态 | 通常只能还原最终态 + 业务自己的日志 | 可回放任一时间点的完整派生状态（fold） |
| 审计与状态一致性 | 靠业务保证两边一致 | 事件即数据，状态由它派生，天然一致 |
| 韧性 | 依赖业务正确接好钩子并落库 | 内置时序不变量 + 崩溃修复 |

**判断准则**：
- 只需"外审计"（记录谁做了啥、出问题能查） → 成熟框架足够，没必要为此引入 dsh-go。
- 需要"内审计 + 强回放"（监管级逐帧重放、日志即真相的不可抵赖、金融/医疗全量留痕） → 事件溯源才有**结构性优势**。

## Q5. 它适合什么企业级业务场景？

主战场是三要素叠加、且锚定 DeepSeek 的规划型 Agent 平台：

1. **对客卖 Agent 且须审计回放**（B2B / SaaS）——事件溯源 = 合规证据链；
2. **重度 DeepSeek 依赖、在意成本**——前缀缓存命中全链路可观测、可压测、可报警，直接折算账单缩减；
3. **Agent 需受控操作生产系统**——工具策略化治理（审批/沙箱/预算拒绝）是接生产的前置条件。

**中场景**：从官方 DSH 迁到 Go 后端（语义 1:1 保真）。

**不适用**：通用多模型编排、简单对话（杀鸡用牛刀）、团队完全不懂 DSH 语义（学习曲线陡）。

## Q6. 本项目到底有没有真实价值？

诚实的三层判断：

1. **技术价值是真实的**：逐词复刻官方语义、事件溯源/增量 fold/缓存/并发加固、且有基准数据（3437×、全量回归 PASS）——即便只作为参考实现，也非零价值。
2. **但技术价值 ≠ 外部生产采纳价值**：生态=0、无官方背书、非官方、无社区，现阶段几乎不会有正式系统敢于直接采用它。
3. **决策权在读者**：
   - 若你要（自用 / 当教材内容持续运营 / 当作品集·研究产出）→ 有持续价值；
   - 若只是要一个能上生产的 Agent 内核 → 请用 Eino/LangChain/官方 DSH。

**结论**：dsh-go 的价值是"验证性 + 参考性"的，不是"外部生产采纳性"的。当学习素材与参考实现，它很称职；别指望它替代成熟框架。

## Q7. 想入门，推荐的学习路径是什么？

1. 先跑 [`examples/tutorial`](../examples/tutorial/main.go)（三步：事件溯源 → fold 投影 → Goal 状态机），对照注释阅读；
2. 再对照 `pkg/session/session.go`（45+ 事件词汇）与 `pkg/session/fold.go`（投影函数族）精读核心；
3. 接着看 `pkg/goal/goal.go`（Goal 状态机四态 + 稳定错误码）；
4. 想更深入：`pkg/agent`（Turn/Step 双循环）、`pkg/tools`（四级流水线）、`pkg/llm/provider_deepseek`（SSE + 失败分类）；
5. 最后看工程加固（`pkg/session/incremental.go` 增量投影、`pkg/persistence/jsonl.go` 分片异步落盘、`pkg/llm/cache.go` 前缀缓存探针）。

## Q8. 我想复刻/二次实现 Agent，dsh-go 对我有什么用？

它是一份**现成的对译范本**：官方能力的**每个接缝都拆成了独立小模块**，配合注释与测试，基本等于一份"Agent 内核实现的拆解说明书"。你想用 TS/C++/Rust 重新实现一份 Agent 时，可直接对照它来对齐语义与边界条件。

## Q9. 什么是事件溯源（Event Sourcing）？

**一句话：不直接存"当前状态"，只不断"追加"一条条不可变的事件；任何时候需要状态，就从这些事件里重新算出来。**

| | 传统（状态快照） | 事件溯源 |
|---|---|---|
| 存什么 | 存"现在的样子"（如 turn=3, user=Carla），改一次覆盖一次 | 存"发生过什么"（append 一条条事件），永不删除 |
| 崩溃/写坏 | 覆盖后旧状态找不回 | 事件日志还在，可重放回任意时刻 |
| 审计/回放 | 只有当前值，历史靠外部日志 | 完整历史即数据，天然可回放 |
| 派生状态 | 存一份 | 随时 fold 算一份 |

在 dsh-go 里（见 [教程第 1 步](../examples/tutorial/main.go)）：`sl.Append(UserMessageData{...})` 就是追加事件，`sl.Events()` 读出这条"不可变事实日志"。官方叫 **append-only event log**，且引擎强制**时序不变量**（turn 开闭配对、tool call↔result 匹配等），写坏会立刻被抓。

> 好处：可审计、可回放、可 fork/compact；代价：日志越来越长，需要压缩和投影来读状态。

## Q10. 什么是 fold 投影（fold / Projection）？

**一句话：定义"如何从一条事件日志算出一个视图"的纯函数就是 fold；算出来的那个视图就是投影（Projection）。**

- **fold（折叠/规约）**：把一条条事件累积成一个结果。输入 = 全部事件，输出 = 一个派生状态，**纯函数、无副作用**——同一堆事件必然算出同一结果。
- **投影（Projection）**：fold 得到的视图，如"当前对话消息列表""Goal 状态""会话标题"，每种都是一份投影。

核心哲学（见 [教程第 2 步](../examples/tutorial/main.go)）：

```
事件日志（唯一真相 Source of Truth）
   ├─ fold → 投影A（消息列表）
   ├─ fold → 投影B（Goal 状态）
   └─ fold → 投影C（会话标题）
```

在 dsh-go 里：`session.FoldAll(events)` 把整个日志折叠成 `SessionProjection`（内含 `Messages` / `Goal` / `Todo` / `PlanMode` 子投影）；H04 将其做成**增量 fold**，O(N) 增量维护，相对 O(N²) 实测约 **3437×** 加速。

## Q11. 除了 DeepSeek，还支持其他大模型吗？怎么用？

**目前只内置了 DeepSeek（`pkg/llm/provider_deepseek`），没有现成的 OpenAI / Claude / 本地模型 provider。**

但架构上**支持扩展**——它提供一个统一接缝：

```go
// pkg/llm/llm.go
type LLMAdapter interface {
    Name() string
    Chat(ctx context.Context, req ChatRequest, cb func(StreamChunk)) (Usage, error)
}
```

任何模型只要实现这 3 个方法，就能被 Agent 的 LLM 层调用。接一个**OpenAI 兼容 provider** 也不难（OpenAI function calling 风格 + SSE），一般流程是：组装请求体 → 调 `/v1/chat/completions?stream=true` → 解析 SSE `data` 映射为 `ChunkText / ChunkToolCall / ChunkDone` → 返回 `Usage`。然后把它注入 Agent：

```go
a := agent.NewAgent(id, sl, sys, pipeline, myProvider) // myProvider 换成你的实现
```

**诚实提示**：本项目定位参考实现/教学，内置仅 DeepSeek 一个适配器，OpenAI/Claude 需你自行实现 provider 且未经社区验证。**要"多模型可用"请用 Eino / LangChain / 官方 SDK**；若想**学习"如何写一个 LLM provider 接缝"**，对照 `pkg/llm/provider_deepseek/deepseek.go` + `LLMAdapter` 实现一个 OpenAI provider 会是很好的练手。

---

> 本文档定位随时间演进，欢迎 PR 修正过时信息（见 [Contributing](../README.md#contributing)）。