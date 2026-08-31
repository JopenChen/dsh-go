---
title: "Examples"
description: "9 个零依赖教学示例"
weight: 4
---

# Examples

仓库内置 **9 个可直接运行的示例**，全部零依赖可跑（除 `chat` 需 API Key）。源码在 [`examples/`](https://github.com/JopenChen/dsh-go/tree/master/examples)。

## 运行方式

```bash
# 教学入口（推荐先跑这个）
go run ./examples/tutorial

# 其余示例
go run ./examples/agent_loop
go run ./examples/usage
go run ./examples/todo
go run ./examples/workflow
go run ./examples/subagent
go run ./examples/sandbox_approval
go run ./examples/mcp
go run ./examples/chat   # 需要 DEEPSEEK_API_KEY
```

## 示例一览

| 示例 | 演示内容 | 依赖 |
|---|---|---|
| `tutorial` | 事件溯源 → fold 投影 → Goal 状态机（教学入口） | 无 |
| `agent_loop` | Agent Turn/Step 双循环 + 工具续步 | 无 |
| `usage` | 会话/投影/Goal 工具/命令/持久化 | 无 |
| `todo` | Todo 整体替换待办（规划原语） | 无 |
| `workflow` | Pipeline 串行 / Parallel 并行 / 取消级联 | 无 |
| `subagent` | 多后端派生 + 家谱 + 父释放级联 | 无 |
| `sandbox_approval` | 审批三态 + 沙箱模式 | 无 |
| `mcp` | MCP 客户端 → 工具桥（内存 Transport） | 无 |
| `chat` | 真实 DeepSeek 多轮对话 | 需 API Key |

## 对照教程

每个示例都配有对照源码指引（见[教程](/en/tutorials/)），建议"示例 + 源码 + 教程"三件套搭配阅读。
