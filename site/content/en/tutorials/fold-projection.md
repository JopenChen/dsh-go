---
title: "fold 投影（fold / Projection）"
description: "状态 = 事件日志的纯函数"
weight: 20
---

# fold 投影（fold / Projection）

## 一句话

**定义"如何从一条事件日志算出一个视图"的纯函数就是 fold；算出来的那个视图就是投影（Projection）。**

## 拆开讲

- **fold（折叠/规约）**：把一条条事件累积成一个结果。输入 = 全部事件，输出 = 一个派生状态，**纯函数、无副作用**。
- **投影（Projection）**：fold 得到的视图，如"当前对话消息列表""Goal 状态""会话标题"，每种都是一份投影。

## 核心哲学

```
事件日志（唯一真相 Source of Truth）
   ├─ fold → 投影A（消息列表）
   ├─ fold → 投影B（Goal 状态）
   └─ fold → 投影C（会话标题）
```

## 在 dsh-go 中

```go
proj := session.FoldAll(sl.Events())
for _, m := range proj.Messages {
    fmt.Printf("[%s] %s\n", m.Role, m.Content)
}
```

- `FoldAll` 把整个日志折叠成 `SessionProjection`（内含 `Messages` / `Goal` / `Todo` / `PlanMode` 子投影）
- **增量 fold（H04）**：不是每次读都重算，而是 O(N) 增量维护

## 性能数据

{{< callout emoji="⚡" >}}
增量 fold 相对暴力重算：**16.9s → 4.9ms，≈ 3437×**（10k 事件，每步读）
{{< /callout >}}

## 对照源码

- `pkg/session/fold.go` —— 投影函数族
- `pkg/session/incremental.go` —— 增量投影（H04）
- 可运行示例：[`examples/tutorial`](https://github.com/JopenChen/dsh-go/blob/master/examples/tutorial/main.go) 第 2 步

## 下一步

→ [学习 Goal 状态机](goal-state-machine/)：Agent 如何规划并驱动执行
