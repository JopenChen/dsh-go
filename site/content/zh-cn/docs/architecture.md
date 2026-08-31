---
title: "架构"
description: "事件溯源 + Turn/Step 双循环 + 工具流水线"
weight: 20
---

# 架构

## 总体流程

```
Session (事件溯源) ──► fold / 投影 ──► Prompt 组装 ──► Agent Turn/Step 循环
                                                                 │
                        Tool Waterfall (pre → execute → post → result) ◄─┘
```

## 三大支柱

### 1. 事件溯源（Event Sourcing）

- 会话状态完全由**追加式事件日志**派生
- 每次写入都是 append 一条不可变事件，引擎强制时序不变量（turn 开闭配对、tool call↔result 匹配）
- 任何时刻可回放、可 fork、可压缩、可持久化

### 2. Turn/Step 双循环（Agent Loop）

- **Turn**：一次完整对话回合（用户消息 → Agent 处理 → 结束）
- **Step**：Turn 内的工具续步（模型请求工具 → 执行 → 把结果喂回模型）
- 取消 / 超时 / 追踪经 ctx 逐层传播到工具与 LLM

### 3. 工具流水线（Tool Waterfall）

- 四级链：`pre → execute → post → result`
- 每级是可插拔中间件：审批、沙箱、token 预算、限制等都可挂载
- 支持对象池、只读注册表等并发加固

## 能力接缝（Capability Seam）

每项能力都是 **服务定义 + Provider** 结构：替换 Provider 即改变整体行为，与官方设计一致。

## 相关

- [能力包总览](capabilities/)
- [性能数据](https://github.com/JopenChen/dsh-go#-performance)
