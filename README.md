# dsh-go

<div align="center">

**把 DeepSeek Agent 规划能力嵌入 Go 后端的进程内 SDK · Process-internal Go SDK for the DeepSeek Agent**

<img src="assets/dsh-cover.jpg" alt="dsh-go — Go SDK for DeepSeek Agent planning" width="100%" />

[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![DSH compat](https://img.shields.io/badge/DeepSeek_Harness-cd5ef81-blue)](#compatibility)
[![Event Sourcing](https://img.shields.io/badge/architecture-event--sourcing-7c3aed)](#features)
[![Go Report](https://img.shields.io/badge/Go_Report-A+-0ea5e9)](https://goreportcard.com)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen)](#contributing)
[![License](https://img.shields.io/badge/license-see%20LICENSE-lightgrey)](LICENSE)

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

---

> **dsh-go** 是一个纯 Go、**进程内**的 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) Agent 实现 —— 让任意 Go 后端能以内嵌库的方式直接获得一个等价、具备**规划能力**的 Agent，**无需界面、无需独立运行时**。它不是又一个 ReAct 骨架，而是对 DSH 全量能力接缝的系统级复刻。

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

- **唯一对齐整套 DSH Agent 的 Go 库**：官方仅提供 TS 主仓与 Python minimal SDK；社区或偏 TS、或偏 CLI，没有一个 Go 项目同时做齐 60+ 子系统且可进程内 `import`。
- **复用成本低**：进程内使用，不引入独立服务/进程；`go get` 即得，业务侧只关心事件与工具。
- **可观测、可溯源**：事件溯源（Event Sourcing）底座让每次对话都可回放、分叉、压缩、持久化，天然适合审计与调试。
- **缓存友好**：基于 append-only 会话与纯 system prompt 组装，助力 DeepSeek 97–99% 前缀缓存命中率。

## ✨ Features

- **事件溯源会话**（`pkg/session`）：追加式日志 + 派生 `fold` 投影；45+ 事件词汇表；时序不变量由引擎强制。
- **Turn/Step 双循环**（`pkg/agent`）：取消 / 超时 / 追踪经 ctx 逐层传播到工具与 LLM（H01）。
- **内建规划**：[Plan Mode](./pkg/plan)、[Goal](./pkg/goal)（状态机 + 续轮驱动 + CAS + 稳定错误码）、[Todo](./pkg/todo)、[Skills](./pkg/skills)（6 层 Provider + fsnotify）。
- **四段工具流水线**（`pkg/tools`）：`pre → execute → post → result` 中间件链；带 `sync.Pool` 与只读注册表，高并发更稳。
- **DeepSeek Provider**（`pkg/llm/provider_deepseek`）：流式 SSE + 生产级连接池 + 与官方 `error.ts` 对齐的稳定失败分类。
- **30+ 能力包**：文件系统、shell、子进程、spill、jobs、终端、工作区、权限、凭证、设置、子代理、工作流、MCP……
- **可观测性**（`pkg/telemetry`、`pkg/tokenmeter`）：OTel 桥、会话遥测钩子、token 计量与预算、缓存指标。

## 🚀 Quick Start

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

仓库内置三个可直接运行的完整示例：

```bash
go run ./examples/agent_loop   # 完整 Agent Turn/Step 循环（含工具续步）
go run ./examples/usage        # 会话/投影/Goal 工具/命令/DeepSeek Provider/持久化
go run ./examples/chat         # 真实调用 DeepSeek 大模型的多轮对话（需 DEEPSEEK_API_KEY）
```

- [examples/agent_loop/main.go](examples/agent_loop/main.go) —— 装配 SessionLog + SystemPrompt + 工具流水线 + LLM 适配器，演示 Turn 内「工具续步 → 结束」。
- [examples/usage/main.go](examples/usage/main.go) —— 事件溯源、fold 投影、Goal 工具（含稳定错误码）、slash 命令、DeepSeek 连接池与失败分类、JSONL 持久化读回。
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

- [Docs index](./docs) —— 详细设计、任务表、缓存方案、测试用例矩阵。
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