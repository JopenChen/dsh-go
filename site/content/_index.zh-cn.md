---
title: "dsh-go"
layout: hextra-home
---

<div class="hx-mt-6 hx-mb-6">
{{< hextra/hero-badge >}}
  <div class="hx-w-2 hx-h-2 hx-rounded-full hx-bg-primary-400"></div>
  <span>DeepSeek Harness Agent 的进程内 Go 参考实现</span>
{{< /hextra/hero-badge >}}

{{< hextra/hero-headline >}}
  读懂 Agent 内核，从 dsh-go 开始
{{< /hextra/hero-headline >}}

{{< hextra/hero-subtitle >}}
  一份纯 Go、进程内的 DeepSeek Harness Agent 参考实现——把事件溯源、fold 投影、Goal 规划、工具治理等核心能力逐词对译为 Go 代码。
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx-mb-6">
{{< hextra/hero-button text="快速了解本项目" link="/zh-cn/faq/" >}}
{{< hextra/hero-button text="开始学习" link="/zh-cn/tutorials/" >}}
{{< hextra/hero-button text="GitHub" link="https://github.com/JopenChen/dsh-go" >}}
</div>

<div class="hx-mt-6"></div>

## 三条主线，快速掌握 Agent 内核

| 主线 | 学什么 | 教程 |
|---|---|---|
| **事件溯源** | 不存状态、只追加不可变事件，随时可重放 | [开始学习](/zh-cn/tutorials/event-sourcing/) |
| **fold 投影** | 状态 = 事件日志的纯函数，增量 O(N) | [开始学习](/zh-cn/tutorials/fold-projection/) |
| **Goal 状态机** | 四态规划 + 稳定错误码 + 续轮驱动 | [开始学习](/zh-cn/tutorials/goal-state-machine/) |

## 9 个零依赖教学示例

| 示例 | 演示内容 |
|---|---|
| `tutorial` | 事件溯源 → fold 投影 → Goal 状态机（教学入口） |
| `agent_loop` | Agent Turn/Step 双循环 + 工具续步 |
| `usage` | 会话/投影/Goal 工具/命令/持久化 |
| `todo` | Todo 整体替换待办 |
| `workflow` | Pipeline 串行 / Parallel 并行 / 取消级联 |
| `subagent` | 多后端派生 + 家谱 + 父释放级联 |
| `sandbox_approval` | 审批三态 + 沙箱模式 |
| `mcp` | MCP 客户端 → 工具桥 |
| `chat` | 真实 DeepSeek 多轮对话（需 API Key） |

全部源码见 [`examples/`](https://github.com/JopenChen/dsh-go/tree/master/examples)。

## 从源码开始

- [查看 GitHub 仓库](https://github.com/JopenChen/dsh-go)
- [阅读文档](/zh-cn/docs/)
- [新人 FAQ](/zh-cn/faq/)
