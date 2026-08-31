---
title: "文档"
description: "了解 Dsh-Go 的架构、能力与设计哲学"
weight: 1
---

欢迎阅读 Dsh-Go 文档。本项目定位为 **DeepSeek Harness Agent 的进程内 Go 参考实现**，目标是让开发者读懂、调试、复刻一个真实 Agent 的内部运作。

## 从这里开始

- [项目介绍](intro/) —— 它是什么、为什么存在
- [架构](architecture/) —— 事件溯源 + Turn/Step 双循环 + 工具流水线
- [能力接缝](capabilities/) —— 各能力包的定位与关系

## 关键概念

- **事件溯源**：不存状态，只追加不可变事件；状态由事件派生
- **fold 投影**：从事件日志"算出"视图的纯函数
- **Goal 状态机**：active / paused / blocked / complete 四态规划
- **工具治理**：四级流水线 + 审批 + 沙箱

## 延伸阅读

- [FAQ](/zh-cn/faq/) —— 与 Eino/LangChain 的关系、场景、价值
- [示例](/zh-cn/examples/) —— 9 个零依赖教学示例
- [GitHub 仓库](https://github.com/JopenChen/dsh-go) —— 源码与历史
