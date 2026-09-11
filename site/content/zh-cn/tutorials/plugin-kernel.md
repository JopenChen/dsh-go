---
title: "插件内核与事件系统"
description: "一切皆插件如何在 Go 侧落地：注册中心、事件总线四种分发模式与资源自动清理"
weight: 40
---

# 插件内核与事件系统

## 一句话总结

**官方用 Cordis 运行时承载"一切皆插件"：插件导出 apply、通过 ctx 注册能力、用事件彼此松耦合通信；dsh-go 没有照搬 Cordis，而是把同一套契约拆成三个 Go 原语——`registry.Freezable` 管能力的注册与冻结、`eventbus.Bus` 管四种同步分发、`waterfall.Chain` 管洋葱式委托。**

## 官方插件的三种形态

在 TypeScript 版里，一个插件就是一个被框架加载时调用的模块，框架传入 `ctx`，插件通过它注册事件监听、工具、LLM 适配器。它有三种写法：

| 形态 | 适用场景 | 关键特征 |
|---|---|---|
| 函数形式 | 绝大多数插件 | 直接导出 `(ctx) => { ... }` |
| 对象形式 | 需要 name/inject 分离 | `{ name, inject, apply(ctx) }` |
| 类形式 | 向其他插件提供命名服务 | 继承 `Service`，`super(ctx, '服务名')` |

**Go 没有运行时动态加载模块这一层**，"插件"在 dsh-go 里退化为编译期的装配：你在组合根（main / wire 函数）里把一个个构造好的能力注册进注册中心。理解这一点很重要——我们复刻的是**契约与生命周期语义**，不是 Cordis 的模块加载器。

## 注册中心：能力的唯一入口

官方插件通过 `ctx.tools`、`ctx.llm`、`ctx.agents` 这些命名服务查找能力，消费方按 key 查找而非导入具体实现。dsh-go 对应的存储原语是 `pkg/registry.Freezable`：

```go
reg := registry.NewFreezable[string, *tools.Tool]()
_ = reg.Put("bash", bashTool)          // 启动期热注册
tool, ok := reg.Get("bash")            // 冻结前：读锁
reg.Freeze()                           // 装配完成，构建只读快照
_ = reg.Put("x", xTool)                // 冻结后写入 → ErrFrozen
tool, ok = reg.Get("bash")             // 冻结后：无锁快照直读
```

它的两段式生命周期正好对应 Agent 的装配阶段与运行阶段：

- **冻结前**：读写都走 `sync.RWMutex`，允许启动期热注册、动态装载；
- **Freeze()**：一次性构建只读快照（不可逆），之后读路径完全无锁，写路径返回 `ErrFrozen`。

这是一个性能与安全兼得的设计：高频读在运行期不再竞争锁，而"装配完成后禁止再改"这一约束让能力集合在运行时保持稳定、可审计。

## 事件系统：四种同步分发

插件之间如何松耦合通信？事件是核心。Cordis 为不同交互契约提供不同分发方法，**每个事件有且只有一种分发模式**。dsh-go 用 `pkg/eventbus.Bus[T,R]` 的三个方法把契约固化下来：

| 模式 | 语义 | dsh-go 方法 | 典型用途 |
|---|---|---|---|
| emit | 广播，所有监听器执行，返回值忽略 | `Emit(payload)` | 状态变更通知、打点 |
| bail | 顺序执行，首个命中的监听器短路 | `Bail(payload, shouldShort)` | 能力查找、策略命中 |
| serial | 顺序执行，聚合返回值为切片 | `Serial(payload)` | 收集多来源候选 |
| waterfall | 洋葱式 next 委托 | `pkg/waterfall.Chain` | 工具/Agent 多级拦截 |

```go
bus := eventbus.New[int, error]()
bus.On(func(n int) error { return nil })          // 未命中
bus.On(func(n int) error { return errHit })       // 命中即短路
res, ok := bus.Bail(7, func(e error) bool { return e != nil })
```

注意 waterfall 与前三者的本质区别：emit/bail/serial 的监听器彼此独立、不知道对方存在；而 waterfall 的每个中间件都持有 `next()`，**主动决定是否把决定权委托下去**，因此能形成"进入—下沉—返回"的洋葱双层拦截。工具流水线与 Agent pre-step 用的正是 waterfall。

## 自动清理：资源随插件生命周期回收

Cordis 的一条关键规则是：**通过 `ctx.on` 注册的监听器，在插件卸载时自动移除**，所有注册皆可逆。dsh-go 里 `On` 返回一个 `dispose` 闭包，你必须在自己的清理路径里保留并调用它：

```go
dispose := bus.On(handler)
defer dispose()          // 组件销毁时摘除，幂等可重复调用
```

这种"注册即返回逆操作"的模式贯穿整个项目：注册工具返回注销、订阅事件返回退订。它让每一项资源都有明确的归属与回收点，避免监听器泄漏导致的"幽灵回调"。

## 为什么这样取舍？

照搬 Cordis 的动态插件容器到 Go 会引入大量运行时反射与全局可变状态，违背 Go"显式优于隐式"的习惯。dsh-go 的选择是：

- **能力可替换**（seam）保留——通过接口 + 注册中心按 key 查找；
- **生命周期可逆**保留——每个注册动作返回 dispose；
- **动态模块加载**放弃——改为编译期显式装配，错误提前到编译期。

结果是：你失去了运行时热插拔模块的灵活性，换来了更强的类型安全、更清晰的装配链路，以及零反射的性能。

## 源码对照

| 概念 | Go 实现 | 官方 TypeScript |
|---|---|---|
| 命名服务存储 | `pkg/registry/registry.go` — `Freezable` | Cordis `ctx.service` / `ctx.set` |
| 只读冻结 | `pkg/registry/registry.go` — `Freeze()` | （官方无对应，dsh-go 性能增强） |
| 广播/保释/串行 | `pkg/eventbus/eventbus.go` — `Emit/Bail/Serial` | Cordis `emit/bail/serial` |
| 洋葱委托 | `pkg/waterfall/waterfall.go` — `Chain.Run` | Cordis `waterfall` |
| 自动清理 | `pkg/eventbus/eventbus.go` — `On` 返回 dispose | Cordis 插件卸载自动 `dispose` |

## 下一步

- **[工具执行流水线](./tool-pipeline)** — waterfall 与单调守卫如何串成一条工具流水线
- **[能力三角色与 LLM 适配器](./capability-seams)** — Definition / Provider / Consumer
- **[Turn / Step 双循环](./turn-step-loop)** — 事件如何被组织成嵌套循环
