---
title: "Agent Loop & Runtime"
description: "How the Agent drives the Turn/Step dual loop, manages status, inbox, cancellation, and request-error recovery"
weight: 35
---

# Agent Loop & Runtime

## In One Sentence

**The Agent is the runtime driver that turns a SessionLog into a living conversation — it owns the Turn/Step dual loop, lifecycle status (idle/running), a dual-queue Inbox for pending work, cancellation semantics, and request-error recovery — all expressed as durable session events.**

## What the Agent Owns

The Agent sits between the **SessionLog** (durable truth) and the **LLM/Tools** (external services). It owns five runtime concerns:

| Concern | Question Answered | Key Types |
|---|---|---|
| **Turn/Step Loop** | How does a user request become LLM calls + tool execution? | `runTurn()`, `runStep()` |
| **Lifecycle Status** | Is the Agent busy or quiescent? | `AgentStatus` (`idle`/`running`), `agent/status` event |
| **Inbox** | What work is queued for the next turn/step? | `Inbox` (next-turn + next-step queues) |
| **Cancellation** | How is an active turn aborted cleanly? | `CancelCause` (5 kinds), `CancelOptions` |
| **Request Recovery** | What happens when an LLM request fails? | `RequestErrorAction` (`retry`/`abort`), `RequestErrorWaterfall` |

## AgentOptions: Immutable Configuration

Created once at construction time, never changes at runtime:

```go
type AgentOptions struct {
    Provider         string        // provider route (e.g. "deepseek")
    Model            string        // model id (e.g. "deepseek-chat")
    ReasoningEffort  string        // adapter-owned reasoning level
    MaxTokens        int           // max output tokens per request
    DefaultTimeout   time.Duration // default run timeout (0 = none)
}
```

```go
agent := NewAgent(id, log, sys, pipeline, adapter, AgentOptions{
    Provider: "deepseek",
    Model:    "deepseek-chat",
    MaxTokens: 4096,
})
```

> Persona and system-prompt sections belong to `pkg/sysprompt`, not AgentOptions.

## AgentStatus: Lifecycle State Machine

The Agent has exactly two observable states, emitted via `agent/status` events on every transition:

```
         turn/start (runTurn begins)
    ┌──────────────────────────────┐
    │                              ▼
  idle                          running
    ▲                              │
    └──────────────────────────────┘
         turn/end (runTurn returns)
```

| State | Meaning | When Entered |
|---|---|---|
| `idle` | No driver is active | Initial state; after every `runTurn` returns |
| `running` | A driver is actively processing | At the start of `runTurn` (before `turn/start`) |

```go
// Query current status
status := agent.Status()  // "idle" or "running"

// Wait for quiescence (blocks until idle)
err := agent.WhenIdle(ctx)
```

### WhenIdle: Waiting for Quiescence

`WhenIdle(ctx)` resolves after the current whole-agent activity reaches quiescence. If already idle, it returns immediately. This is useful for:

- Tests that need to assert final state
- Graceful shutdown (wait for active turn to finish)
- Batch processing (wait for one turn before starting next)

```go
agent.Run("do something")
if err := agent.WhenIdle(context.Background()); err != nil {
    // context cancelled
}
// agent is now idle, session log is complete
```

## Inbox: Dual-Queue Pending Work

The Inbox is the Agent-owned projection of durable pending messages. It maintains **two ordered lists**:

| Queue | Purpose | Consumed At |
|---|---|---|
| `next-turn` | Prompts awaiting individual turns | Turn boundary (one per turn) |
| `next-step` | Input awaiting the next step boundary | Step boundary (all at once) |

### Why Two Queues?

A user request may arrive **mid-turn** (while the Agent is processing). Rather than interrupting the active turn, the message is queued:

- **next-step**: consumed at the next step boundary — the LLM sees it in the very next step
- **next-turn**: consumed only when the current turn ends — it becomes the next user request

This separation enables **steering** (mid-turn input) without violating turn boundaries.

### Core Operations

```go
inbox := agent.Inbox()

// Check if there's pending work
inbox.HasPending()  // true if either queue non-empty

// Push a message to next-step (steering input)
inbox.Push(log, session.InboxNextStep, UserMessage{Content: "also check tests"})

// Push a message to next-turn (next user request)
inbox.Push(log, session.InboxNextTurn, UserMessage{Content: "now do this"})

// Claim the batch for a step (removes + returns next-step + optionally one next-turn)
batch := inbox.Claim(log, session.InboxNextTurn, turnIdx)

// Clear all pending work (on cancellation)
inbox.Clear(log)
```

### Durability: agent/inbox/spliced Events

Every Inbox mutation is persisted as an `agent/inbox/spliced` event:

```go
type InboxSplicedData struct {
    Target       InboxTarget // "next-turn" or "next-step"
    Start        int         // splice position (-1 = append)
    RemovedCount int         // number removed
    Inserted     []string    // contents inserted
    Outcome      string      // "canceled" when cleared by cancellation
}
```

On Agent construction, the Inbox **replays all historical spliced events** to reconstruct its state. This means the Inbox is fully crash-recoverable.

## Cancellation: Five Causes, One Mechanism

The Agent supports five cancellation causes (aligned with the official `AgentCancelCause`):

| Cause | Meaning | Typical Trigger |
|---|---|---|
| `user` | User主动取消 | User clicks "stop" |
| `parent` | Parent agent cancels (subagent) | Parent turn ends |
| `hook` | Hook rejection triggers cancel | Pre-step hook returns reject |
| `disposed` | Agent is being disposed | `agent.Dispose()` |
| `legacy` | Legacy/compatibility path | Old cancellation code |

```go
// Cancel the active turn
agent.Cancel(CancelUser)

// Cancel but preserve queued inbox items
agent.Cancel(CancelUser)  // with CancelOptions{KeepInbox: true}
```

### What Cancellation Does

1. Records the cancel cause via `turn/stopping` event
2. The active `runTurn` detects cancellation (via context) and aborts
3. The turn closes with `reason: interrupted` (or `aborted` for explicit cancel)
4. By default, the Inbox is cleared (with `outcome: canceled`)
5. With `KeepInbox: true`, queued items survive for a later turn

## PreStepDecision: Gatekeeping Step Entry

Before entering a proposed step, listeners can return a `PreStepDecision`:

```go
type PreStepDecision struct {
    Kind               string        // "reject" or "enter"
    Messages           []UserMessage // messages to carry in (enter only)
    StartsRequestSeries bool         // start a new model-message series
}
```

| Decision | Effect |
|---|---|
| `reject` | Do not enter this step; turn may close early |
| `enter` | Enter the step with the specified messages |

This is the extension point for **hooks**, **approval gates**, and **steering logic** that need to inspect or modify input before the LLM sees it.

## RequestError Recovery: Retry Waterfall

When an LLM request fails, the Agent runs a **request-error waterfall** to decide the action:

```
LLM request fails
    ↓
RequestErrorWaterfall (middleware chain)
    ├─ middleware 1: check error kind → retry?
    ├─ middleware 2: check retry count → abort?
    └─ ...
    ↓
TerminalRetryDecision (fallback)
    ├─ retryable error + under max → retry
    └─ otherwise → abort (turn closes with error)
```

```go
type RequestErrorAction struct {
    Kind   RequestErrorActionKind // "retry" or "abort"
    Reason string                 // audit reason
}

// Build a waterfall with custom middleware
chain := NewRequestErrorWaterfall(
    func(ctx context.Context, p *RequestErrorPayload, next func()) {
        if p.RetryableError && p.RetryCount < 3 {
            p.Action = &RequestErrorAction{Kind: ActionRetry, Reason: "custom retry policy"}
            return  // short-circuit
        }
        next()
    },
)

// Resolve the final action
action := ResolveRequestError(chain, payload)
```

### Retryable Errors

Only **overload** and **rate-limit** errors are retryable (classified by `llm.ClassifyLlmError`). Other errors (auth, invalid request, etc.) immediately abort.

## Interaction with Other Subsystems

### SessionLog

The Agent is the **primary writer** to the SessionLog. Every runtime transition — turn/start, step/start, agent/request, tool/call, agent/status, agent/inbox/spliced — is a durable event. The Agent never mutates state directly; it appends events and lets projections derive state.

### LLM Adapter

The Agent calls the adapter's `Stream()` method inside `runStep()`. The adapter is selected based on `AgentOptions.Provider`. Request errors flow into the RequestErrorWaterfall.

### Tool Pipeline

Tool calls discovered in the LLM response are executed through the 4-stage pipeline (resolve → approve → sandbox → execute). The pipeline is injected at construction time.

### Sandbox

The Sandbox decides **where** tool side effects can happen. The Agent doesn't interact with the Sandbox directly — it's the Tool Pipeline's responsibility.

### Approval

Approval decides **whether** a tool can run. Like the Sandbox, this is handled inside the Tool Pipeline, not by the Agent directly.

## Benefits and Costs

### Benefits

- **Event-sourced runtime** — every state transition is durable and replayable
- **Clean cancellation** — turn boundaries give natural abort points; inbox can be preserved or cleared
- **Steering without interruption** — next-step queue lets users inject input mid-turn
- **Status observability** — idle/running is a first-class event, not a derived guess
- **Composable error recovery** — waterfall middleware lets plugins customize retry logic

### Costs

- **Two queues to reason about** — next-turn vs next-step is subtle; misuse can cause messages to be delayed or lost
- **Status is coarse** — only idle/running; no "waiting for tool" or "streaming" sub-states
- **Cancellation is cooperative** — the Agent must poll context; a stuck tool call may not cancel promptly
- **Inbox replay overhead** — constructing an Agent replays all spliced events; very long sessions have O(n) construction cost

## Source Reference

| Concept | Go Implementation | Official TypeScript |
|---|---|---|
| Agent core loop | `pkg/agent/agent.go` — `runTurn()`, `runStep()` | `packages/core/agent/src/index.ts` |
| AgentOptions | `pkg/agent/options.go` — `AgentOptions` | `packages/core/agent/src/runtime-types.ts` — `AgentOptions` |
| AgentStatus | `pkg/agent/options.go` — `AgentStatus`; `pkg/session/session.go` — `AgentStatusData` | `packages/core/agent/src/runtime-types.ts` — `AgentStatus` |
| WhenIdle | `pkg/agent/agent.go` — `WhenIdle()` | `packages/core/agent/src/runtime-types.ts` — `whenIdle()` |
| Inbox | `pkg/agent/inbox.go` — `Inbox` | `packages/core/agent/src/inbox.ts` — `Inbox` |
| Inbox events | `pkg/session/session.go` — `InboxSplicedData`, `InboxTarget` | `packages/core/agent/src/types.ts` — `agent/inbox/spliced` |
| Cancellation | `pkg/agent/cancel.go` — `CancelCause`, `RecordCancel` | `packages/core/agent/src/runtime-types.ts` — `cancel()` |
| PreStepDecision | `pkg/agent/options.go` — `PreStepDecision` | `packages/core/agent/src/runtime-types.ts` — `PreStepDecision` |
| RequestError | `pkg/agent/requesterror.go` — `RequestErrorWaterfall` | `packages/core/agent-loop/` — request-error waterfall |
| Initiator | `pkg/agent/initiator.go` — `Initiator` | `packages/core/agent/src/index.ts` — initiator tracking |

## Next Steps

- **[Turn / Step Dual Loop](./turn-step-loop)** — the nested loop structure in detail
- **[Sandbox & Controlled Execution](./sandbox-execution)** — where tool side effects are confined
- **[Session Event Sourcing](./event-sourcing)** — the durable event log that powers everything
