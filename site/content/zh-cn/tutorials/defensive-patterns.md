---
title: "防御性编程与事故复盘"
description: "结果报告、资源清理、凭据保护三类防御模式，以及事故复盘四问的工程文化"
weight: 44
---

# 防御性编程与事故复盘

## 一句话总结

**一个长期运行、能自主调用工具的 Agent，必须在三类边界上做防御：结果要如实报告（不吞错、不伪造成功）、资源要确定清理（不留句柄/临时文件/监听）、凭据要最小暴露（不进日志、不随结果外泄）；而当 bug 仍然逃逸时，事故复盘关注的不是那行修复，而是"为什么每道安全网都没拦住"以及"新增什么防护让同类问题下次明确报错"。**

## 三类防御模式

### 1. 结果报告：失败必须可见

Agent 会把工具结果回灌给模型，一旦结果被错误地标记为"成功"，模型就会在错误前提上继续推理，错误会层层放大。防御要点：

- **不吞错**：工具返回 error 时结果必须带 `IsError`，不能静默返回空值伪装成功；
- **错误信息可判别**：用错误链（`pkg/llm/errorchain.go`）区分"可重试"与"确定性失败"，避免对鉴权错误做无意义重试、或对瞬时错误直接放弃；
- **panic 兜底**：外部回调可能 panic，边界处用 `waterfall.Chain.RunSafe` 把 panic 转成 error，防止单个工具崩溃拖垮整个 Turn；
- **截断要显式**：结果超限时由 post-execute 明确截断并标注，而不是悄悄丢内容。

### 2. 资源清理：注册即返回逆操作

Agent 长跑会反复打开进程、临时文件、事件监听，任何一处不回收都会累积成泄漏。dsh-go 的统一约定是**每个"获取/注册"动作都返回对应的"释放/注销"**：

```go
dispose := bus.On(handler)
defer dispose()                 // 监听器随组件销毁摘除

proc, kill, err := subprocess.Start(...)
defer kill()                    // 子进程随 Turn 结束回收
```

- 子进程（`pkg/subprocess`）、终端会话要在 Turn/Step 结束时确定关闭，取消信号沿 ctx 传播；
- 临时产物落在 spill（`pkg/spill`）指定目录并在事后清理；
- dispose/cleanup 设计为**幂等**，重复调用不出错，保证多条清理路径并存时安全。

### 3. 凭据保护：最小暴露

API Key、OAuth Token 是最高敏感级：

- **引用而非明文**：能力侧持有 `CredentialRef`，真正用时才经 `credentials.Store.Resolve` 取出，密钥不在配置树、prompt、事件日志里明文流动；
- **转储脱敏**：`settings.MarkSecret` 标记的路径在 `Describe(redactSecrets=true)` 时被脱敏；`credentials.Store.Describe` 只输出元信息（是否存在、来源），不回传值；
- **按请求获取**：由 `pkg/credentials` 统一管理生命周期（Set/Unset）与授权流，避免密钥散落在各组件。

## 运行期三重护栏

除了静态的资源/凭据纪律，Agent 在长时间运行中还会遇到卡死、空转、历史损坏三类问题，对应三重运行期护栏。

### 协作式超时：防止永久卡死

工具用 `TimeoutMs` 声明预算，`tools.WrapTimeout` 武装截止时间：本层计时器到期时把结果映射为结构化 `TOOL_TIMEOUT`，父 ctx 先取消则按普通取消处理，不会误报。它是**协作式**的——Go 无法强杀 goroutine，工具实现必须监听 `ctx.Done()` 自行退出，否则后台仍会继续。

### 重复提醒：打破原地空转

模型有时会以完全相同的参数反复调用同一工具而毫无进展。`tools.RepeatState` 统计连续相同调用：参数经 `CanonicalizeArgs` 深度 key 排序，属性顺序不同也视为相同；达到阈值（默认 3/5/8）时给出温和、再到详细的提醒，但**只提醒、不否决**，决定权仍在后续流程。用户一旦插话，链即重置——跨插话的重复不算循环。

### 工具配平：防止压缩出非法历史

压缩历史时，切点绝不能落在一次 tool_call 与它的 tool_result 之间，否则会留下"有调用没结果"的历史，模型会因此卡住。`compaction.BalancedCuts` 用括号配平：assistant/message 里每个工具调用 +1、tool/result -1，只有进行中计数归零处才是合法切点；`NearestBalancedFrom` 会把期望边界安全后移到最近的配平位置。

## 事故复盘：四问

dsh-go 借鉴上游的复盘文化：一个 bug 出现在"不该出现"的地方（真实运行、已合并代码）时，写一份回顾性记录，回答四个问题：

| 四问 | 要回答什么 |
|---|---|
| 什么坏了 | 简短段落，让读者三十秒内抓住要点 |
| 机制是什么 | 用直白语言说清根因，不归咎个人 |
| 为什么每道安全网都没拦住 | 找测试、工具、约定的缺口，而非一次性笔误 |
| 新增了什么防护 | 测试、规则、断言，让同类问题下次明确报错 |

**不是所有 bug 都值得写复盘**，只有同时满足三个条件才写：

1. **隐蔽**——机制不显而易见，细心的工程师也要费力重新推导；
2. **系统性**——逃逸原因是测试/工具/约定的缺口，而不是一次手抖；
3. **重新发现代价高**——消耗了真实调试时间，且不记录下次还会如此。

### 一个典型案例

上游复盘 0001：某插件多写了一个 `export default apply`，加载器取到裸函数，把命名空间上的 `inject` 整个丢掉，导致编辑器一连上就因拿不到 `agents` 服务而崩溃。它的教训是：**绿色的单元测试数量不能证明装配正确**——178 个单测全过却没覆盖"加载真实插件"这条路径。新增的防护是针对加载/装配链路的集成断言，让"依赖丢失"在启动时就明确报错，而不是延迟到第一次调用。

## 从修复到防护的闭环

防御性编程与事故复盘是一对闭环：

- 防御模式尽量把错误**左移**到写入/启动/编译期；
- 仍有逃逸时，复盘把一次性修复升级为**持久防护**（一个会失败的测试、一条强制规则、一个 fail-closed 断言）；
- 于是系统的安全网随每次事故变得更密，而不是修完即忘。

## 源码对照

| 概念 | Go 实现 | 官方 TypeScript |
|---|---|---|
| panic 兜底 | `pkg/waterfall/waterfall.go` — `RunSafe` | defensive patterns |
| 进程清理 | `pkg/subprocess`、`pkg/terminal` | cleanup 约定 |
| 临时产物 | `pkg/spill` | （dsh-go 对应） |
| 凭据存储 | `pkg/credentials/credentials.go` — `Store` | credentials 管理 |
| 密钥脱敏 | `pkg/settings/settings.go` — `MarkSecret` | （dsh-go 增强） |
| 协作式超时 | `pkg/tools/timeout.go` — `WrapTimeout` | `packages/guard/timeout-policy` |
| 重复提醒 | `pkg/tools/repeat.go` — `RepeatState` | `packages/guard/repeat-tool-reminder` |
| 工具配平 | `pkg/compaction/pairing.go` — `BalancedCuts` | `packages/compaction/tool-pairing` |
| 错误链 | `pkg/llm/errorchain.go` | （dsh-go 增强） |

## 下一步

- **[组合包与配置分层](./bundle-profile)** — 密钥路径如何在配置树中被脱敏
- **[工具执行流水线](./tool-pipeline)** — 结果如何被标记错误与截断
- **[沙箱与受控执行](./sandbox-execution)** — 另一道系统性的安全边界
