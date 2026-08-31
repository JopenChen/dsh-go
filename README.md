# dsh-go

<div align="center">

**把 DeepSeek Agent 规划能力嵌入 Go 后端的进程内参考实现 · An in-process Go reference implementation of the DeepSeek Harness Agent**

<img src="assets/dsh-cover.jpg" alt="dsh-go — 面向 DeepSeek Agent 规划的 Go 参考实现" width="100%" />

[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![DSH compat](https://img.shields.io/badge/DeepSeek_Harness-cd5ef81-blue)](#compatibility)
[![Event Sourcing](https://img.shields.io/badge/architecture-event--sourcing-7c3aed)](#features)
[![Go Report](https://img.shields.io/badge/Go_Report-A+-0ea5e9)](https://goreportcard.com)
[![License](https://img.shields.io/badge/license-see%20LICENSE-lightgrey)](LICENSE)

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

---

> **dsh-go** 是一份纯 Go、**进程内**的 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) Agent **参考实现**——它把官方 DSH 的 Turn/Step 双循环、事件溯源、Goal 规划、工具治理等核心能力接缝，逐词对译为 Go 代码，**让开发者可以阅读、调试、复刻一个真实 Agent 的内部运作**。
>
> 它定位为**参考实现与学习素材**，而非与 LangChain / Eino / 官方 DSH 竞争的生产级框架：生态、背书与多模型适配均非本文目标，本文目标是"**读懂 Agent 内核**"。

> **❓ 快速了解本项目** —— 想先弄清楚"这是什么 / 和 Eino·LangChain 什么关系 / 有什么优势 / 适合什么场景 / 值不值得用"？请直接阅读 **[FAQ（常见问题）](./docs/FAQ.md)**。

## 📖 Table of Contents

- [Why dsh-go](#-why-dsh-go)
- [Features](#-features)
- [Quick Start](#-quick-start)
- [Examples](#-examples)
- [Architecture](#-architecture)
- [Package Map](#-package-map)
- [Performance](#-performance)
- [Documentation](#-documentation)
- [Compatibility](#-compatibility)
- [Contributing](#-contributing)
- [License](#-license)

## 📌 Why dsh-go

- **可读的 Agent 内核**：官方仅有 TS 主仓与 Python minimal 版；本文用 Go 给出**逐词对译**的完整能力接缝，模块小而独立，适合**(对照源码)读懂**一个 Agent 的真实运作。
- **三条学习主线**：事件溯源 → fold 派生投影 → Goal 状态机，配合 [`examples/tutorial`](#-examples) 三步渐进示例，是一条清晰的内核学习路径。
- **可复现的工程实践**：事件溯源增量投影（≈3437×）、连接池调优、并发加固与缓存亲和等，都带**基准数据与回归用例**可复现，适合作为工程范本研读。
- **诚实边界**：非生产级框架、非 LangChain/Eino 替代品；生态、多模型适配不在本文目标内。

## ✨ Features

- **事件溯源会话**（`pkg/session`）：追加式日志 + 派生 `fold` 投影；45+ 事件词汇表；时序不变量由引擎强制。
- **Turn/Step 双循环**（`pkg/agent`）：取消 / 超时 / 追踪经 ctx 逐层传播到工具与 LLM（H01）。
- **内建规划**：[Plan Mode](./pkg/plan)、[Goal](./pkg/goal)（状态机 + 续轮驱动 + CAS + 稳定错误码）、[Todo](./pkg/todo)、[Skills](./pkg/skills)（6 层 Provider + fsnotify）。
- **四段工具流水线**（`pkg/tools`）：`pre → execute → post → result` 中间件链；带 `sync.Pool` 与只读注册表，高并发更稳。
- **DeepSeek Provider**（`pkg/llm/provider_deepseek`）：流式 SSE + 生产级连接池 + 与官方 `error.ts` 对齐的稳定失败分类。
- **30+ 能力包**：文件系统、shell、子进程、spill、jobs、终端、工作区、权限、凭证、设置、子代理、工作流、MCP……
- **可观测性**（`pkg/telemetry`、`pkg/tokenmeter`）：OTel 桥、会话遥测钩子、token 计量与预算、缓存指标。

## 🚀 Quick Start

> 作为参考实现，你**不一定需要 `go get` 引入**——最轻的入门方式是直接跑 `examples/` 并对照源码阅读。若确实想在本项目上做实验，可这样引入：

**安装**：

```bash
go get github.com/JopenChen/dsh-go@latest
```

**最小可用示例** —— 创建会话、追加事件、派生状态：

```go
package main

import (
	"fmt"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

func main() {
	sl := session.NewSessionLog(brand.NewSessionID("hello"))
	if _, err := sl.Append(session.UserMessageData{Content: "Hello"}); err != nil {
		panic(err)
	}
	proj := session.FoldAll(sl.Events())
	fmt.Printf("%d message(s) in log\n", len(proj.Messages))
}
```

## 🧭 Examples

仓库内置九个可直接运行的示例，全部零依赖可跑（除 `chat` 需 API Key），**建议从 `tutorial` 开始**：

```bash
go run ./examples/tutorial          # 教学三步曲：事件溯源 → fold 投影 → Goal 状态机（推荐入门）
go run ./examples/agent_loop       # 完整 Agent Turn/Step 循环（含工具续步）
go run ./examples/usage            # 会话/投影/Goal 工具/命令/DeepSeek Provider/持久化
go run ./examples/todo             # Todo 整体替换待办（规划原语）
go run ./examples/workflow         # 工作流编排：Pipeline 串行 / Parallel 并行 / Agent 步骤 / 取消级联
go run ./examples/subagent         # 子代理：多后端派生 + 家谱 + 父释放级联
go run ./examples/sandbox_approval # 沙箱(在哪儿) + 审批(能不能) 受控执行
go run ./examples/mcp              # MCP 客户端 → 工具桥（内存 Transport 模拟服务器）
go run ./examples/chat             # 真实调用 DeepSeek 大模型的多轮对话（需 DEEPSEEK_API_KEY）
```

- [examples/tutorial/main.go](examples/tutorial/main.go) —— **教学入口**：用「事件溯源 → fold 派生投影 → Goal 状态机」三条独立主线，渐进讲解 Agent 内核；每步带大量注释并对照 `pkg/session` / `pkg/goal` 源码阅读。
- [examples/agent_loop/main.go](examples/agent_loop/main.go) —— 装配 SessionLog + SystemPrompt + 工具流水线 + LLM 适配器，演示 Turn 内「工具续步 → 结束」。
- [examples/usage/main.go](examples/usage/main.go) —— 事件溯源、fold 投影、Goal 工具（含稳定错误码）、slash 命令、DeepSeek 连接池与失败分类、JSONL 持久化读回。
- [examples/todo/main.go](examples/todo/main.go) —— Todo **整体替换**（last-write-wins）：用 `todo_write` 写入待办、从事件日志派生读回、观察旧列表被整体覆盖。
- [examples/workflow/main.go](examples/workflow/main.go) —— 工作流引擎：`Pipeline` 串行短路 / `Parallel` 并发保序 / `Agent` 子代理步骤 / ctx 取消级联终止。
- [examples/subagent/main.go](examples/subagent/main.go) —— 子代理接缝：`Runtime.Spawn` 多后端派生 + `ForkLineage` 家谱归因 + `Drain` 收结果 + `DisposeOwner` 父释放级联 + 未知后端稳定错误。
- [examples/sandbox_approval/main.go](examples/sandbox_approval/main.go) —— 受控执行：审批三层策略解析（预设→用户→会话）+ 三态决策（allow/deny/ask）+ 危险工具 ask 的 fail-closed；沙箱模式解析与 danger 判定。
- [examples/mcp/main.go](examples/mcp/main.go) —— **MCP 桥接**：用内存 `Transport` 模拟 MCP 服务器，演示 `Initialize → ListTools → Bridge.ToTools → 统一调用 → 挂进流水线` 的完整链路，零网络零依赖。
- [examples/chat/main.go](examples/chat/main.go) —— 从环境变量读取 `DEEPSEEK_API_KEY`，构造 DeepSeek provider，通过 `LLMAdapter.Chat` 发起流式多轮对话，展示 `ChunkReasoning / ChunkText / ChunkToolCall / ChunkDone` 分片消费、`LlmFailure` 稳定分类与 usage 用量统计。

## 🏗️ Architecture

```
Session (Event Sourcing) ──► fold / Projection ──► Prompt Assemble ──► Agent Turn/Step Loop
                                                                        │
                        Tool Waterfall (pre → execute → post → result) ◄─┘
```

每项能力都是一个 **能力接缝（Capability Seam）**：`服务定义 + Provider`。替换 Provider 即改变整体行为，与官方 `Capability Seam` 设计一致。

## 📦 Package Map

| Package | Capability |
|---|---|
| [`pkg/session`](./pkg/session) | Event sourcing, 45+ vocabulary, fold, invariants, incremental projection |
| [`pkg/agent`](./pkg/agent) | Agent registry, Turn/Step loop, cancel causes, request-error retry |
| [`pkg/tools`](./pkg/tools) | Waterfall pipeline, execution context, presentation, restriction, schema, retention |
| [`pkg/llm`](./pkg/llm) | LLM seam, stream protocol (SSE), failure taxonomy, retry, cache probe |
| [`pkg/goal`](./pkg/goal) / [`pkg/todo`](./pkg/todo) / [`pkg/plan`](./pkg/plan) / [`pkg/skills`](./pkg/skills) | Planning primitives |
| [`pkg/persistence`](./pkg/persistence) / [`pkg/storage`](./pkg/storage) | JSONL (sharded/async) & SQLite(→FTS5) backends, CAS storage domains |
| [`pkg/subagent`](./pkg/subagent) / [`pkg/workflow`](./pkg/workflow) / [`pkg/mcp`](./pkg/mcp) | Subagents, workflow engine, MCP client→tool bridge |
| [`pkg/fs`](./pkg/fs) / [`pkg/shell`](./pkg/shell) / [`pkg/subprocess`](./pkg/subprocess) / [`pkg/spill`](./pkg/spill) / [`pkg/jobs`](./pkg/jobs) / [`pkg/terminal`](./pkg/terminal) | File & process execution |
| [`pkg/settings`](./pkg/settings) / [`pkg/credentials`](./pkg/credentials) / [`pkg/approval`](./pkg/approval) / [`pkg/sandbox`](./pkg/sandbox) / [`pkg/scope`](./pkg/scope) | Config, credentials, safety, scoping |
| [`pkg/telemetry`](./pkg/telemetry) / [`pkg/tokenmeter`](./pkg/tokenmeter) / [`pkg/feedback`](./pkg/feedback) / [`pkg/sessionquery`](./pkg/sessionquery) | Observability, metering, feedback, search |

## ⚡ Performance

关键路径均做过并发与分配级加固（`go test -bench` 可复现）：

| 场景 | 优化 | 实测 |
|---|---|---|
| Session 派生投影（10k 事件，每步读） | 增量 fold（H04） | **16.9s → 4.9ms**，≈ **3437×** |
| 共享注册表读（100 键 Get） | Freeze 后无锁快照（H07） | 65.6 → 49.4ns，快 **25%**，0 alloc |
| 持久化 IO | bytes.Buffer + bufio.Writer 双 `sync.Pool`（H05） | 热路径分配显著下降 |
| Tool 流水线 | ExecContext 对象池（H06） | allocs 9 → 8 |

## 📚 Documentation

- 🌐 **在线文档站（GitHub Pages）**：https://JopenChen.github.io/dsh-go/ —— 中英双语、全文搜索、教程与示例导航（源码在 [`site/`](./site)）。
- [Docs index](./docs) —— 详细设计、任务表、缓存方案、测试用例矩阵。
- [`docs/FAQ.md`](./docs/FAQ.md) —— **新手 FAQ**：本项目是什么、与 Eino / LangChain / 官方 DSH 的关系、审计算不算独有、适合什么场景、值不值得用。
- [`docs/TASKS.md`](./docs/TASKS.md) · [`docs/tasks.json`](./docs/tasks.json) —— 结构化任务表（机器 + 人可读）。
- [`docs/TEST_CASES.md`](./docs/TEST_CASES.md) —— 328 条测试用例设计矩阵。
- [`docs/CACHE_HIT_RATE_PLAN.md`](./docs/CACHE_HIT_RATE_PLAN.md) —— 前缀缓存命中率对齐方案。

## 🤝 Compatibility

- **语言**：Go 1.25+
- **SQLite**：[modernc.org/sqlite](https://modernc.org/sqlite)（纯 Go，无 CGO）
- **上游锚点**：DeepSeek Harness `master` @ **cd5ef81**（`dsh-0.1.2-alpha.1`）

## 🧑‍🤝‍🧑 Contributing

欢迎提交 Issue 与 PR。请确保改动通过 `gofmt`、`go vet ./...` 与 `go test ./tests/ -count=1`。

## 📄 License

See [LICENSE](LICENSE).