---
title: "上下文供给：指令、时钟与调度"
description: "指令文件发现、时钟上下文与进程内调度三类能力如何在 Go 侧落地，让 Agent 知道规则、时间并能被定时唤醒"
weight: 45
---

# 上下文供给：指令、时钟与调度

## 一句话总结

**Agent 不会凭空知道项目规则、当前时间，也不会自己在未来某刻醒来；dsh-go 用 `pkg/instructions` 发现并加载项目指令链、用 `pkg/timecontext` 在准备请求时采样时钟、用 `pkg/schedule` 管理定时提醒，三者共同构成"上下文供给"。**

## 指令文件：项目规则从哪来

官方会从工作目录向上找到项目根，再沿"根 → 工作目录"的祖先链逐层读取指令文件（如 AGENTS.md），越靠近工作目录优先级越高。dsh-go 把这套发现逻辑落在 `pkg/instructions`：

- `FindProjectRoot(cwd, markers)` 逐级向上，返回第一个含 marker（如 `.git`）的目录，找不到则退回 cwd；
- `AncestorChain(root, cwd)` 给出由宽到窄的目录链；
- `DedupByDirectory` 对**同一目录**内 trim 后内容相同的候选去重（不同目录即使内容相同也保留，因为层级语义不同）。

## @文件引用：输入时的路径补全

用户在输入框敲 `@` 时需要路径补全。`pkg/fileref` 复刻这套语法：`ActiveAtToken` 提取光标处正在输入的 token（区分普通与引号路径，邮箱里的 `@` 不触发）；`FormatMention` 把选中路径格式化——含空白用 `@"..."`、目录保留尾斜杠以便继续下钻。

## 时钟上下文：让模型知道"现在"

长时间运行的会话里，模型需要知道当前时间以及距上条消息过了多久。`pkg/timecontext`：

- `FormatElapsed` 把经过时间压缩成紧凑的 `d/h/m/s`；
- `Render(now, loc, previous)` 输出当前时间、时区与距上一条模型可见消息的时长，previous 缺失时记为 `unavailable`。

## 调度：在未来某刻唤醒

`pkg/schedule` 是并发安全的进程内调度器，复刻官方三类规则：

| 规则 | 含义 | 约束 |
|---|---|---|
| `After` | 相对延迟一次性 | 延迟必须为正 |
| `At` | 绝对时刻一次性 | 目标必须在未来 |
| `Every` | 固定周期 | 间隔不低于 5 分钟 |

到点经 `Out()` 通道投递；一次性触发后移除，周期性自动重排；`Cancel/List/Shutdown` 管理生命周期。提醒投递不离开拥有者会话（session-local）。

## 分层环境：配置值的信任顺序

同一个变量可能同时出现在进程环境、项目 `.env`、用户 `.env` 三处。`launchenv` 在启动时把三层冻结成不可变快照，按"进程 > 项目 > 用户"的固定信任顺序解析（Windows 上变量名大小写折叠），避免运行中切换目录后配置被悄悄改变。

## 源码对照

| 概念 | Go 实现 | 官方 TypeScript |
|---|---|---|
| 指令发现 | `pkg/instructions/instructions.go` | `packages/context/agent-instructions/src/files.ts` |
| @文件引用 | `pkg/fileref/fileref.go` | `packages/context/file-reference/src/grammar.ts` |
| 时钟上下文 | `pkg/timecontext/timecontext.go` | `packages/context/time-context/src/index.ts` |
| 进程内调度 | `pkg/schedule/schedule.go` | `packages/schedule/schedule/src/runtime.ts` |
| 分层环境 | `pkg/launchenv/launchenv.go` | `packages/util/launch-environment/src/index.ts` |

## 下一步

- 回看[防御模式]({{< ref "defensive-patterns" >}})了解运行期三重护栏；
- 或回到[能力接缝]({{< ref "capability-seams" >}})理解能力如何被装配。
