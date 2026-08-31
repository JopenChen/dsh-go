---
title: "事件溯源（Event Sourcing）"
description: "不存状态，只追加不可变事件"
weight: 10
---

# 事件溯源（Event Sourcing）

## 一句话

**不直接存"当前状态"，只不断"追加"一条条不可变的事件；任何时候需要状态，就从这些事件里重新算出来。**

## 对比传统做法

| | 传统（状态快照） | 事件溯源 |
|---|---|---|
| 存什么 | 存"现在的样子"（如 turn=3），改一次覆盖一次 | 存"发生过什么"（append 一条条事件），永不删除 |
| 崩溃/写坏 | 覆盖后旧状态找不回 | 事件日志还在，可重放回任意时刻 |
| 审计/回放 | 只有当前值，历史靠外部日志 | 完整历史即数据，天然可回放 |
| 派生状态 | 存一份 | 随时 fold 算一份 |

## 在 dsh-go 中

```go
sl := session.NewSessionLog(brand.NewSessionID("demo"))
sl.Append(session.UserMessageData{Content: "你好"})
sl.Append(session.AssistantMessageData{Content: "你好，我是 dsh-go。"})

evs := sl.Events() // 读出这条"不可变事实日志"
```

关键点：

- **append-only**：只追加，永不修改/删除历史
- **时序不变量**：turn 开闭配对、tool call↔result 匹配，违规立刻被拒绝
- **唯一写入口**：`Append()`，引擎保证一致性

## 好处与代价

{{< callout emoji="✅" >}}
**好处**：可审计、可回放、可 fork/compact、崩溃可恢复
{{< /callout >}}

{{< callout emoji="⚠️" >}}
**代价**：日志越来越长，需要压缩（compaction）和投影（projection）来读状态
{{< /callout >}}

## 对照源码

- `pkg/session/session.go` —— 事件日志与 45+ 事件词汇
- 可运行示例：[`examples/tutorial`](https://github.com/JopenChen/dsh-go/blob/master/examples/tutorial/main.go) 第 1 步

## 下一步

→ [学习 fold 投影](fold-projection/)：状态如何从事件"算"出来
