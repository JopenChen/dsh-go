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

## 安全与治理

5. [沙箱与受控执行](sandbox-execution/) —— Agent 能在哪儿写？三种沙箱模式、策略解析与 fail-closed 强制约束

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

## 后续规划

{{< callout emoji="🚀" >}}
本项目定位参考实现与教学，后续会持续补充更多专题教程：工具治理、子代理编排、MCP 桥接、缓存亲和等。
{{< /callout >}}
