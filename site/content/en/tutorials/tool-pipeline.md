---
title: "Tool Execution Pipeline"
description: "The fixed stages a tool call traverses, the three-state decision, and the monotonic guard"
weight: 41
---

# Tool Execution Pipeline

## In One Sentence

**After the model emits a tool call, it is not "just calling a function": it traverses a fixed order of pre-execute → monotonic guard → execute → post-execute → result, each stage carrying one class of policy; dsh-go implements these with a four-level waterfall chain, and the three-state decision plus monotonic guard ensure safety policies can only tighten, never be bypassed.**

## Overall Order

Upstream summarizes the order as `tools/pre-execute → monotonic guard → tools/execute → tools/post-execute → tools/result`. dsh-go's `pkg/tools.Pipeline` maps them to four separate waterfall chains:

```
ToolCallRequest
   ├─ pre-execute    intercept/rewrite args (permission, sandbox, hooks); can deny
   ├─ monotonic guard final line: decisions only tighten
   ├─ execute        call the real implementation (can set signal=cancel)
   ├─ post-execute   post-process (accept/block, truncate, meta)
   └─ result         final wrapping, metrics
ToolCallResult
```

The first pluggable stages are waterfalls: a listener may call `next()` to delegate or return a decision to short-circuit. The ordering of policy plugins is adjustable at wiring time, hence pre-execute is the "reorderable policy layer".

## Three-State Decision: allow / deny / ask

pre-execute returns a typed `PreToolDecision`:

| Decision | Meaning | Follow-up |
|---|---|---|
| `PreAllow` | allow this call | proceed to execute |
| `PreDeny` | reject | short-circuit; tool not run; result isError |
| `PreAsk` | ask the user | AskFunc queries; only proceeds if approved |

The key semantics is **allowed-once**: in an `ask`, user approval only allows **this single call**; the next invocation of the same tool asks again. There is no permanent per-tool allow.

dsh-go reuses `pkg/approval.Decision` as the carrier, so tool decisions and the approval service share semantics and can be mapped directly from `approval.Service.Evaluate`.

## Monotonic Guard: Only Tighten

Multiple policy plugins in pre-execute may each give a decision — which one wins? The **monotonic guard** sits between the policy layer and real execution. `pkg/tools.MonotonicGuard` defines a strictness order **deny > ask > allow**:

- The first assignment is always accepted;
- Later decisions may only move stricter (allow→deny, ask→deny);
- Trying to relax an already stricter decision (deny→allow) returns `ErrGuardRelaxed` and **keeps the original**.

```go
g := tools.NewMonotonicGuard()
_ = g.Update(tools.PreAllow)
_ = g.Update(tools.PreDeny)              // tighten, accepted
err := g.Update(tools.PreAllow)         // relax → ErrGuardRelaxed
// g.Decision() stays PreDeny (fail closed)
```

## Layered Tool Restriction

Beyond per-call decisions, tool visibility uses a `Restriction` mask. `RestrictionSet` holds an ordered layer stack (host outermost, nearer scope wins), resolved **nearest-scope-wins**: scan from the nearest layer back to host; the first layer that mentions a tool decides. `host deny + scope allow(exempt)` restores a tool; both deny → reject; unmentioned → allowed by default. It serves Subagent capability limits and Preset tool hiding via `Filter`.

## Source Map

| Concept | Go implementation | Upstream TypeScript |
|---|---|---|
| Four-level pipeline | `pkg/tools/pipeline.go` — `Pipeline` | `packages/core/tools` |
| Three-state decision | `pkg/tools/predecision.go` | tools pre-execute |
| Monotonic guard | `pkg/tools/monotonic.go` — `MonotonicGuard` | monotonic guard |
| Layered mask | `pkg/tools/restriction.go` — `RestrictionSet` | tools restriction |
| Object pool | `pkg/tools/pooled.go` — `SetPooled` | (dsh-go addition) |

## Next Steps

- **[Plugin Kernel & Event System](./plugin-kernel)**
- **[Sandbox & Controlled Execution](./sandbox-execution)**
- **[Capability Seams & LLM Adapter](./capability-seams)**
