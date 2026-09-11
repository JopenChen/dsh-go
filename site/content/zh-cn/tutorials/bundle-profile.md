---
title: "组合包与配置分层"
description: "bundle 与 profile 两个 manifest 如何在 Go 侧落地为 Preset 组合与分层 Settings 叠加"
weight: 43
---

# 组合包与配置分层

## 一句话总结

**官方用 bundle（打包一组能力）与 profile（选择启用哪些能力）两个 manifest 分层叠加出最终配置；dsh-go 没有照搬 manifest 文件格式，而是用 `pkg/presets` 的 AgentPreset 组合承载"能力打包"、用 `pkg/settings` 的分层作用域承载"配置叠加"，并通过 revision 乐观并发保证配置更新的一致性。**

## 两个 manifest 的分工

官方配置体系里有两个容易混淆的概念：

| 概念 | 回答的问题 | 内容 |
|---|---|---|
| bundle 组合包 | "我要带上哪些能力？" | 一组工具/服务/插件的打包清单 |
| profile 配置档 | "这些能力以什么策略运行？" | 沙箱模式、审批策略、模型等参数 |

最终生效的配置是多层叠加的结果，类似官方 `dsh --profile web --dump-config` 可以打印出实际启动的完整配置树。dsh-go 把这两层分别落到 Preset 与 Settings。

## Preset：能力的声明与组合

`pkg/presets.AgentPreset` 是一个能力档：声明该档下启用哪些工具、采用什么权限与沙箱策略。`PresetRegistry` 负责挂载与选择，而 `ComposeFrom` 支持从多个基础档叠加出一个新档：

```go
base := &presets.AgentPreset{ /* 基础能力 */ }
web := presets.ComposeFrom("web", base, extraPreset) // 在 base 之上叠加
reg := presets.NewPresetRegistry()
reg.Mount(web)
p, ok := reg.Select("web")
```

组合遵循"在已有能力集合上增量叠加"的语义，避免为每个场景都从零写一份完整清单。权限侧另有 `PermissionPreset` 与 `Derive`：给定档名 + 沙箱/审批的运行时覆盖项，推导出最终生效的 `DerivedState`，覆盖项优先于档内默认值。

## Settings：分层叠加与路径化读写

`pkg/settings` 把配置建模为一棵按点分路径寻址的树（`Path`，如 `llm.temperature`），`SettingsScope` 承载多层作用域：

- **ApplyHost**：写入 host（最外层）默认值；
- **Update / Replace**：在当前层做增量 set/unset，或整体替换；
- **Get / Merge**：读取时多层合并，近层覆盖外层；
- **Describe**：导出完整配置树，并对被 `MarkSecret` 标记的路径做脱敏。

```go
scope := settings.NewSettingsNamespace("agent")
ss := settings.NewSettingsScope(scope)
_ = ss.ApplyHost("llm.temperature", 0.3)
raw, ok := ss.Get("llm.temperature")
tree := ss.Describe(true) // redactSecrets=true，密钥路径脱敏
```

### Revision 乐观并发

配置在运行期可能被多处更新，`SettingsScope` 用单调递增的 `Revision` 做乐观并发控制：每次 `Update/Replace` 都要带上期望的 revision，不匹配则返回 `ErrRevisionMismatch`（可用 `IsRevisionMismatch` 判别），调用方重新读取最新值后再试。这避免了"读—改—写"之间的静默覆盖。

## 密钥安全

配置树里不可避免会出现 API Key 等敏感项。`MarkSecret` 把某条路径标记为密钥后：

- `Describe(redactSecrets=true)` 输出时对其脱敏，防止密钥随配置转储/日志泄漏；
- 真正需要密钥的环节由 `pkg/credentials` 按请求获取，而不是让密钥在整棵配置树里明文流动。

## 设计取舍

官方 manifest 是声明式文件，便于分发与跨语言共享；dsh-go 作为库，选择用类型化的 Go 结构表达，换来编译期字段检查与 IDE 提示，代价是配置不再以独立文件形式存在、跨进程共享需要自行序列化。分层叠加、近层覆盖、密钥脱敏这些**核心语义被完整保留**。

## 源码对照

| 概念 | Go 实现 | 官方 TypeScript |
|---|---|---|
| 能力档 | `pkg/presets/agent_presets.go` — `AgentPreset` | bundle manifest |
| 档组合 | `pkg/presets/agent_presets.go` — `ComposeFrom` | bundle 叠加 |
| 权限档推导 | `pkg/presets/permission_presets.go` — `Derive` | profile 策略 |
| 分层配置 | `pkg/settings/settings.go` — `SettingsScope.Merge` | profile 叠加 |
| 乐观并发 | `pkg/settings/settings.go` — `Revision`/`ErrRevisionMismatch` | （dsh-go 增强） |
| 密钥脱敏 | `pkg/settings/settings.go` — `MarkSecret`/`Describe` | （dsh-go 增强） |

## 下一步

- **[能力三角色与 LLM 适配器](./capability-seams)** — 配置档里的模型如何被接入
- **[插件内核与事件系统](./plugin-kernel)** — 组合好的能力如何注册
- **[防御性编程与事故复盘](./defensive-patterns)** — 配置与凭据相关的防御纪律
