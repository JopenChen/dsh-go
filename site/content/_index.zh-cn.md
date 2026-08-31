---
title: "Dsh-Go"
layout: hextra-home
---

<!-- ========== Hero ========== -->
<div class="hx-mt-10 hx-mb-8 hx-flex hx-flex-col hx-items-center">
  <img class="dsh-hero-icon" src="img/dsh-icon.jpg" alt="Dsh-Go 图标" width="104" height="104" />
  {{< hextra/hero-badge >}}
  <div class="hx-w-2 hx-h-2 hx-rounded-full hx-bg-primary-400"></div>
  <span>DeepSeek Harness Agent 的进程内 Go 参考实现</span>
{{< /hextra/hero-badge >}}

{{< hextra/hero-headline >}}
  基于 Dsh-Go 动手构建<br/>读懂 Agent 内核
{{< /hextra/hero-headline >}}

{{< hextra/hero-subtitle >}}
  一份纯 Go、进程内的 DeepSeek Harness Agent 参考实现——把事件溯源、fold 投影、
  Goal 规划、工具治理等核心能力，逐词对译为地道的 Go 代码，
  让你能够阅读、调试、复刻一个真实 Agent 的内部运作。
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx-mb-10">
{{< hextra/hero-button text="快速开始" link="/zh-cn/tutorials/" >}}
{{< hextra/hero-button text="阅读文档" link="/zh-cn/docs/" >}}
{{< hextra/hero-button text="GitHub" link="https://github.com/JopenChen/dsh-go" >}}
</div>

<div class="hx-mt-8"></div>

<!-- ========== 架构图（带动效数据流） ========== -->
{{< arch-diagram >}}

<div class="hx-mt-8"></div>

<!-- ========== 玻璃特性卡片 ========== -->
## Dsh-Go 能给你什么

Agent 内核由三大支柱构成。每一项都是 **Capability Seam（能力接缝）**：服务定义 + 可替换的 Provider，
替换一个 Provider 即改变整体行为，而无需触碰其它部分。

{{< cards >}}
  {{< card link="/zh-cn/tutorials/event-sourcing/" icon="database" title="事件溯源"
          subtitle="每个会话都是只追加的事件日志。可随时回放、分叉、压缩与持久化，并由引擎强制时序不变量。" >}}
  {{< card link="/zh-cn/tutorials/fold-projection/" icon="chart-bar" title="fold 投影"
          subtitle="会话状态是事件日志的纯函数。增量 O(N) 的投影让视图既廉价又一致。" >}}
  {{< card link="/zh-cn/docs/intro/" icon="shield-check" title="Goal 与工具治理"
          subtitle="四态 Goal 状态机 + 稳定错误码，叠加四级工具流水线，承载审批、沙箱与配额。" >}}
{{< /cards >}}

## 拆解 Agent 内核

Dsh-Go 只回答一个问题：**一个真实 Agent 内部到底是怎么运作的？**它不给你黑盒，
而是把真实机制摊开——每一块都足够小，一下午就能读懂。

| 核心概念 | 含义 | 学习入口 |
|---|---|---|
| **事件溯源** | 不存可变状态；只追加不可变事件，状态由事件派生 | [教程](/zh-cn/tutorials/event-sourcing/) |
| **fold 投影** | 状态 = 事件日志的纯函数，增量 O(N) | [教程](/zh-cn/tutorials/fold-projection/) |
| **Goal 状态机** | `active / paused / blocked / complete` 四态规划 + 稳定错误码 | [教程](/zh-cn/tutorials/goal-state-machine/) |
| **Turn / Step 循环** | 每个用户消息一个 Turn；Step 执行工具并把结果喂回模型 | [架构](/zh-cn/docs/architecture/) |
| **工具流水线** | `pre → execute → post → result` 四级链 + 可插拔中间件 | [能力总览](/zh-cn/docs/capabilities/) |

## 9 个零依赖教学示例

从 [`examples/`](https://github.com/JopenChen/dsh-go/tree/master/examples) 开始，
运行 `go run ./examples/tutorial`——除最后一个示例外，都不需要大模型 API Key。

| 示例 | 演示内容 |
|---|---|
| `tutorial` | 事件溯源 → fold 投影 → Goal 状态机（教学入口） |
| `agent_loop` | Agent Turn/Step 双循环 + 工具续步 |
| `usage` | 会话 / 投影 / Goal 工具 / 命令 / 持久化 |
| `todo` | 端到端整体替换一个能力 |
| `workflow` | Pipeline 串行 / Parallel 并行 / 取消级联 |
| `subagent` | 多 Provider 派生 + 父释放级联 |
| `sandbox_approval` | 审批三态 + 沙箱模式 |
| `mcp` | MCP 客户端 → 工具桥 |
| `chat` | 真实 DeepSeek 多轮对话（需 API Key） |
