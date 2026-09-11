---
title: "Plugin Kernel & Event System"
description: "How everything-is-a-plugin lands in Go: a registry, four event dispatch modes, and automatic resource cleanup"
weight: 40
---

# Plugin Kernel & Event System

## In One Sentence

**Upstream uses the Cordis runtime for "everything is a plugin": a plugin exports apply, registers capabilities through ctx, and communicates loosely via events; dsh-go does not copy Cordis but splits the same contract into three Go primitives — `registry.Freezable` for registering and freezing capabilities, `eventbus.Bus` for four synchronous dispatch modes, and `waterfall.Chain` for onion-style delegation.**

## Three Plugin Forms Upstream

In the TypeScript version, a plugin is a module invoked by the framework on load with a `ctx`, through which it registers listeners, tools, and LLM adapters. It has three forms:

| Form | When to use | Key trait |
|---|---|---|
| Function | Most plugins | export `(ctx) => { ... }` |
| Object | Separate name/inject | `{ name, inject, apply(ctx) }` |
| Class | Provide a named service | extend `Service`, `super(ctx, 'name')` |

**Go has no runtime module-loading layer**, so a "plugin" in dsh-go becomes compile-time wiring: you register constructed capabilities into a registry at the composition root (main / wire function). We replicate the **contract and lifecycle semantics**, not the Cordis module loader.

## Registry: The Single Entry to Capabilities

Upstream plugins look up capabilities through named services like `ctx.tools`, `ctx.llm`, `ctx.agents` — by key, never by importing an implementation. The matching storage primitive in dsh-go is `pkg/registry.Freezable`:

```go
reg := registry.NewFreezable[string, *tools.Tool]()
_ = reg.Put("bash", bashTool)       // hot registration during startup
tool, ok := reg.Get("bash")         // before freeze: read lock
reg.Freeze()                        // wiring done, build read-only snapshot
_ = reg.Put("x", xTool)             // after freeze: ErrFrozen
tool, ok = reg.Get("bash")          // after freeze: lock-free snapshot read
```

Its two-phase lifecycle matches the assembly phase versus the running phase of an Agent:

- **Before freeze**: reads and writes take `sync.RWMutex`, allowing hot registration and dynamic loading;
- **Freeze()**: builds a one-time read-only snapshot (irreversible); afterwards reads are fully lock-free and writes return `ErrFrozen`.

## Event System: Four Synchronous Modes

How do plugins communicate loosely? Events. Cordis offers a dispatch method per interaction contract, and **each event has exactly one mode**. dsh-go fixes the contracts via three methods of `pkg/eventbus.Bus[T,R]`:

| Mode | Semantics | dsh-go method | Typical use |
|---|---|---|---|
| emit | broadcast; all listeners run; results ignored | `Emit(payload)` | change notifications, metrics |
| bail | sequential; first hit short-circuits | `Bail(payload, shouldShort)` | capability lookup, policy hit |
| serial | sequential; aggregate results into a slice | `Serial(payload)` | gather candidates |
| waterfall | onion next delegation | `pkg/waterfall.Chain` | multi-level interception |

The key difference: in emit/bail/serial listeners are independent and unaware of each other; in waterfall every middleware holds `next()` and **actively decides whether to delegate**, enabling enter-dive-return two-layer interception.

## Automatic Cleanup

A key Cordis rule: **listeners registered via `ctx.on` are removed automatically when the plugin unloads**; every registration is reversible. In dsh-go, `On` returns a `dispose` closure you must keep and invoke on your cleanup path:

```go
dispose := bus.On(handler)
defer dispose()          // removed on teardown; idempotent
```

This "register returns its inverse" pattern runs throughout the project: registering a tool returns its removal, subscribing returns unsubscription — giving every resource a clear owner and reclamation point.

## Why These Trade-offs?

Copying Cordis's dynamic plugin container into Go would introduce runtime reflection and global mutable state, against Go's "explicit over implicit" habit. dsh-go keeps **replaceability** (interface + key lookup), keeps **reversible lifecycle** (dispose), and drops **dynamic module loading** in favor of compile-time wiring — trading runtime hot-swap for stronger type safety and zero reflection.

## Source Map

| Concept | Go implementation | Upstream TypeScript |
|---|---|---|
| Named service storage | `pkg/registry/registry.go` — `Freezable` | Cordis service |
| Read-only freeze | `pkg/registry/registry.go` — `Freeze()` | (dsh-go addition) |
| Emit/bail/serial | `pkg/eventbus/eventbus.go` | Cordis emit/bail/serial |
| Onion delegation | `pkg/waterfall/waterfall.go` — `Chain.Run` | Cordis waterfall |
| Auto cleanup | `pkg/eventbus/eventbus.go` — `On` returns dispose | Cordis unload cleanup |

## Next Steps

- **[Tool Execution Pipeline](./tool-pipeline)**
- **[Capability Seams & LLM Adapter](./capability-seams)**
- **[Turn / Step Dual Loop](./turn-step-loop)**
