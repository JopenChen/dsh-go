---
title: "教程"
description: "循序渐进的 Agent 内核学习路径"
weight: 2
---

教学区是 Dsh-Go 的核心价值所在。我们按"从零理解 Agent 内核"设计了渐进学习路径，每一步都有可运行的代码示例与源码对照。

## 核心内核（三步）

1. [事件溯源](event-sourcing/) —— 为什么"只记事件不记状态"更稳
2. [fold 投影](fold-projection/) —— 状态如何从事件日志"算"出来
3. [Goal 状态机](goal-state-machine/) —— Agent 如何把目标变成可续轮的执行循环

## Agent 循环

4. [Turn / Step 双循环](turn-step-loop/) —— 对话如何被结构化为嵌套的 Turn 和 Step 循环，以及严格单调编号
5. [Agent 循环与运行时](agent-loop/) —— Agent 如何驱动循环、管理状态、收件箱、取消和请求错误恢复

## 安全与治理

6. [沙箱与受控执行](sandbox-execution/) —— Agent 能在哪儿写？三种沙箱模式、策略解析与 fail-closed 强制约束

## 扩展与工程

7. [插件内核与事件系统](plugin-kernel/) —— 一切皆插件如何落地：注册中心、四种事件分发与自动清理
8. [工具执行流水线](tool-pipeline/) —— 一次工具调用经过的固定环节、三态决策与单调守卫
9. [能力三角色与 LLM 适配器](capability-seams/) —— Definition / Provider / Consumer 与任意模型接入
10. [组合包与配置分层](bundle-profile/) —— bundle/profile 如何落地为 Preset 组合与分层 Settings
11. [防御性编程与事故复盘](defensive-patterns/) —— 结果报告、资源清理、凭据保护与复盘四问
12. [上下文供给](context-feeding/) —— 指令文件发现、时钟上下文与进程内调度

## 运行方式

```bash
# 核心内核教程（三步合一）
go run ./examples/tutorial

# 沙箱与审批深入
go run ./examples/sandbox_approval
```

## 对照源码

- `pkg/session/session.go` —— 事件日志与事件词汇（含 Turn/Step 数据结构）
- `pkg/session/fold.go` —— fold 投影函数族
- `pkg/goal/goal.go` —— Goal 状态机
- `pkg/agent/agent.go` —— Agent 循环，驱动 Turn/Step 双循环
- `pkg/sandbox/sandbox.go` —— 三种沙箱模式与 fail-closed 强制约束
- `pkg/approval/approval.go` —— 审批策略（另一道安全闸）
- `pkg/registry/registry.go` —— 可冻结的能力注册中心
- `pkg/eventbus/eventbus.go` —— 事件总线（emit/bail/serial）
- `pkg/waterfall/waterfall.go` —— 洋葱式 waterfall 链
- `pkg/tools/` —— 工具流水线、三态决策、单调守卫与分层掩码
- `pkg/llm/llm.go` —— LLMAdapter 与 StreamChunk 流式协议
- `pkg/presets/`、`pkg/settings/` —— 能力档组合与分层配置
- `pkg/credentials/credentials.go` —— 凭据引用与最小暴露

## 后续规划

{{< callout emoji="🚀" >}}
本项目定位参考实现与教学，后续会持续补充更多专题教程：工具治理、子代理编排、MCP 桥接、缓存亲和等。
{{< /callout >}}
