---
title: "FAQ"
description: "关于 Dsh-Go 的常见问题"
weight: 3
---

# FAQ

关于 Dsh-Go 的高频问题。完整内容见 [docs/FAQ.md](https://github.com/JopenChen/dsh-go/blob/master/docs/FAQ.md)。

## Q1. Dsh-Go 到底是什么？

一份纯 Go、进程内的 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) Agent **参考实现**——把官方 DSH 的 Turn/Step 双循环、事件溯源、Goal 规划、工具治理等核心能力接缝，逐词对译为 Go 代码。

## Q2. 和 Eino / LangChain 是什么关系？

**不同物种**。Eino/LangChain 是"拿来用的框架"（通用 LLM 编排）；Dsh-Go 是"拿来读的参考实现"（对 DSH 的对译），不打算也没能力替代前者。

## Q3. 相对它们有什么优势？

三块非对称硬实力：**① 与官方语义逐词对齐**（事件/错误码/Goal 四态）；**② 事件溯源 + fold 投影**（结构性差异，增量 3437×）；**③ 前缀缓存亲和**（DeepSeek 降本）。

## Q4. 成熟框架做不到合规审计吗？

能做到。"能不能审计"不是独有能力；差异在**能力归属与证据强度**：成熟框架审计靠外挂（回调/自行落库），Dsh-Go 审计是内建（append-only 事件即真相，可回放中间态）。

## Q5. 适合什么企业场景？

主战场 = **审计回放 + DeepSeek 降本 + 受控工具执行** 三要素叠加、且锚定 DeepSeek 的规划型 Agent 平台。

## Q6. 本项目到底有没有真实价值？

技术价值真实（逐词复刻 + 基准数据），但 ≠ 外部生产采纳价值（生态 0、无背书）。它是**验证性 + 参考性**价值，适合自用/教学/作品集，不适合当生产替代。

## Q7. 想入门，推荐的学习路径？

先跑 [`examples/tutorial`](/en/tutorials/)，再对照 `pkg/session` / `pkg/goal` 精读，然后深入 `pkg/agent` / `pkg/tools` / `pkg/llm`。

## Q8. 想复刻/二次实现 Agent，有什么用？

它是一份**现成的对译范本**：每个接缝拆成独立小模块 + 注释 + 测试，等于一份"Agent 内核实现的拆解说明书"。

## Q9. 什么是事件溯源？

见[教程：事件溯源](/en/tutorials/event-sourcing/)。

## Q10. 什么是 fold 投影？

见[教程：fold 投影](/en/tutorials/fold-projection/)。

## Q11. 除了 DeepSeek 支持其他模型吗？

目前仅内置 `provider_deepseek`；但 `LLMAdapter` 接缝可扩展，实现 3 个方法即可接入（OpenAI 兼容 provider 思路见 [docs/FAQ.md](https://github.com/JopenChen/dsh-go/blob/master/docs/FAQ.md) Q11）。

---

> 完整版（含对比表、代码、判断准则）见 [docs/FAQ.md](https://github.com/JopenChen/dsh-go/blob/master/docs/FAQ.md)。
