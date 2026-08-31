---
title: "Tutorials"
description: "循序渐进的 Agent 内核学习路径"
weight: 2
---

# Tutorials

教学区是 dsh-go 的核心价值所在。我们按"从零理解 Agent 内核"设计了三步渐进路径，每一步都有可运行的代码示例（`examples/tutorial`）与源码对照。

## 三步路径

1. [事件溯源](event-sourcing/) —— 为什么"只记事件不记状态"更稳
2. [fold 投影](fold-projection/) —— 状态如何从事件日志"算"出来
3. [Goal 状态机](goal-state-machine/) —— Agent 如何把目标变成可续轮的执行循环

## 运行方式

```bash
go run ./examples/tutorial
```

## 对照源码

- `pkg/session/session.go` —— 事件日志与事件词汇
- `pkg/session/fold.go` —— fold 投影函数族
- `pkg/goal/goal.go` —— Goal 状态机

## 后续规划

{{< callout emoji="🚀" >}}
本项目定位参考实现与教学，后续会持续补充更多专题教程：工具治理、子代理编排、MCP 桥接、缓存亲和、事件溯源实战等。
{{< /callout >}}
