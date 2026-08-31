---
title: "项目介绍"
description: "dsh-go 是什么、为什么存在、它不是什么"
weight: 10
---

# 项目介绍

## 它是什么

**dsh-go** 是一份纯 Go、进程内的 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) Agent **参考实现**。

它把官方 DSH 的 Turn/Step 双循环、事件溯源、Goal 规划、工具治理等核心能力接缝，**逐词对译为 Go 代码**——不是又一个 ReAct 骨架，而是对官方语义体系的一套可阅读、可调试、可复刻的翻译。

## 它为什么存在

- 官方仅有 TS 主仓与 Python minimal 版，缺少可读的 Go 对译
- 事件溯源、fold 投影、Goal 状态机这些核心概念，需要一份**小而独立、带注释**的实现来理解
- 可作为二次实现 Agent 时的**语义对照范本**

## 它不是什么

{{< callout emoji="⚠️" >}}
dsh-go **不是**生产级框架，**不是** LangChain / Eino / 官方 DSH 的替代品。生态、背书、多模型适配均非本项目目标。当参考实现与学习素材，它很称职；别指望它替代成熟框架。
{{< /callout >}}

## 三条核心主线

| 主线 | 核心机制 | 教学示例 |
|---|---|---|
| 事件溯源 | append-only 事件日志 + 时序不变量 | [tutorial](/zh-cn/tutorials/event-sourcing/) |
| fold 投影 | 状态 = 事件的纯函数，增量 O(N) | [tutorial](/zh-cn/tutorials/fold-projection/) |
| Goal 状态机 | 四态 + 稳定错误码 + 续轮驱动 | [tutorial](/zh-cn/tutorials/goal-state-machine/) |

## 下一步

- 阅读[架构](architecture/)了解设计
- 从[教程](/zh-cn/tutorials/)开始动手
- 查看[FAQ](/zh-cn/faq/)解决疑问
