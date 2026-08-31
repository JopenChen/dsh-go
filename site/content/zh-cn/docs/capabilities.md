---
title: "能力包总览"
description: "各能力包的定位与关系"
weight: 30
---

# 能力包总览

Dsh-Go 的每个能力包都遵循 **Capability Seam** 模式：`服务定义 + Provider`，替换 Provider 即改变整体行为。以下按功能域分组。

## 核心运行时

| 包 | 能力 |
|---|---|
| [`pkg/session`](https://github.com/JopenChen/dsh-go/tree/master/pkg/session) | 事件溯源、事件词汇、fold、不变量、增量投影 |
| [`pkg/agent`](https://github.com/JopenChen/dsh-go/tree/master/pkg/agent) | Agent 注册表、Turn/Step 循环、取消原因、请求错误重试 |
| [`pkg/tools`](https://github.com/JopenChen/dsh-go/tree/master/pkg/tools) | 四级流水线、执行上下文、presentation、限制、schema、保留 |
| [`pkg/llm`](https://github.com/JopenChen/dsh-go/tree/master/pkg/llm) | LLM 接缝、流式协议、失败分类、重试、缓存探针 |

## 规划原语

| 包 | 能力 |
|---|---|
| [`pkg/goal`](https://github.com/JopenChen/dsh-go/tree/master/pkg/goal) / [`pkg/todo`](https://github.com/JopenChen/dsh-go/tree/master/pkg/todo) | Goal 状态机（四态 + 稳定错误码）、Todo 整体替换 |
| [`pkg/plan`](https://github.com/JopenChen/dsh-go/tree/master/pkg/plan) / [`pkg/skills`](https://github.com/JopenChen/dsh-go/tree/master/pkg/skills) | 计划模式、技能系统（6 层 Provider） |

## 持久化与存储

| 包 | 能力 |
|---|---|
| [`pkg/persistence`](https://github.com/JopenChen/dsh-go/tree/master/pkg/persistence) / [`pkg/storage`](https://github.com/JopenChen/dsh-go/tree/master/pkg/storage) | JSONL（分片/异步）与 SQLite 后端、CAS 存储域 |

## 子代理与工作流

| 包 | 能力 |
|---|---|
| [`pkg/subagent`](https://github.com/JopenChen/dsh-go/tree/master/pkg/subagent) / [`pkg/workflow`](https://github.com/JopenChen/dsh-go/tree/master/pkg/workflow) / [`pkg/mcp`](https://github.com/JopenChen/dsh-go/tree/master/pkg/mcp) | 子代理、工作流引擎、MCP 客户端→工具桥 |

## 文件与进程

| 包 | 能力 |
|---|---|
| [`pkg/fs`](https://github.com/JopenChen/dsh-go/tree/master/pkg/fs) / [`pkg/shell`](https://github.com/JopenChen/dsh-go/tree/master/pkg/shell) / [`pkg/subprocess`](https://github.com/JopenChen/dsh-go/tree/master/pkg/subprocess) / [`pkg/jobs`](https://github.com/JopenChen/dsh-go/tree/master/pkg/jobs) / [`pkg/terminal`](https://github.com/JopenChen/dsh-go/tree/master/pkg/terminal) | 文件与进程执行、作业、终端 |

## 配置与安全

| 包 | 能力 |
|---|---|
| [`pkg/settings`](https://github.com/JopenChen/dsh-go/tree/master/pkg/settings) / [`pkg/credentials`](https://github.com/JopenChen/dsh-go/tree/master/pkg/credentials) / [`pkg/approval`](https://github.com/JopenChen/dsh-go/tree/master/pkg/approval) / [`pkg/sandbox`](https://github.com/JopenChen/dsh-go/tree/master/pkg/sandbox) / [`pkg/scope`](https://github.com/JopenChen/dsh-go/tree/master/pkg/scope) | 配置、凭证、审批、沙箱、作用域 |

## 可观测性

| 包 | 能力 |
|---|---|
| [`pkg/telemetry`](https://github.com/JopenChen/dsh-go/tree/master/pkg/telemetry) / [`pkg/tokenmeter`](https://github.com/JopenChen/dsh-go/tree/master/pkg/tokenmeter) / [`pkg/feedback`](https://github.com/JopenChen/dsh-go/tree/master/pkg/feedback) / [`pkg/sessionquery`](https://github.com/JopenChen/dsh-go/tree/master/pkg/sessionquery) | 可观测性、计量、反馈、搜索 |
