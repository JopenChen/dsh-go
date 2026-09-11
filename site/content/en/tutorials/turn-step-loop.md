---
title: "Turn / Step Dual Loop"
description: "How the Agent structures a conversation as nested Turn and Step loops, with strict monotonic numbering and pairing invariants"
weight: 30
---

# Turn / Step Dual Loop

## In One Sentence

**Every Agent conversation is structured as a nested dual loop: a *Turn* is one user request and its complete resolution, and inside each Turn, one or more *Steps* each represent one LLM call plus its tool execution — both loops carry strictly monotonic numbers and are enforced by session invariants.**

## Why Two Loops?

A single loop is not enough because a user request often requires **multiple LLM calls** before it is complete:

```
User: "List files, then count lines in each .go file"
  └─ Turn 0 starts
       ├─ Step 1: LLM decides to call `ls` → tool executes → result
       ├─ Step 2: LLM sees file list, decides to call `wc -l` on each → tool executes → result
       └─ Step 3: LLM synthesizes final answer (no more tool calls)
  └─ Turn 0 ends
```

The **Turn** boundary answers: *"Is this user request resolved?"*
The **Step** boundary answers: *"Did this LLM call produce tool calls that need another round?"*

This separation is what enables:
- **Per-turn cancellation** — abort one user request without losing session history
- **Per-step retry** — re-run a failed LLM call without restarting the whole turn
- **Accurate token accounting** — count tokens per step, sum per turn
- **Tool-call scoping** — every `tool/call` must belong to an open Step, which must belong to an open Turn

## The Four Event Markers

The dual loop is expressed entirely through four event types in the session log:

| Event | Data | Meaning |
|---|---|---|
| `turn/start` | `{ turn: uint64 }` | A new Turn begins |
| `turn/end` | `{ turn: uint64, reason: TurnEndReason }` | The current Turn closes |
| `step/start` | `{ turn: uint64, step: uint64 }` | A new Step begins inside the current Turn |
| `step/end` | `{ turn: uint64, step: uint64 }` | The current Step closes |

Plus a fifth auxiliary event:

| Event | Data | Meaning |
|---|---|---|
| `turn/stopping` | `{ reason?: string }` | Turn is entering shutdown (listeners like goal-round-driver hook here) |

```go
// pkg/session/session.go
type TurnStartData struct {
    Turn uint64 `json:"turn"`
}

type TurnEndData struct {
    Turn   uint64        `json:"turn"`
    Reason TurnEndReason `json:"reason"`
}

type StepStartData struct {
    Turn   uint64 `json:"turn"`
    Step   uint64 `json:"step"`
    StepSeq uint64 `json:"stepSeq,omitempty"` // legacy alias
}

type StepEndData struct {
    Turn   uint64 `json:"turn"`
    Step   uint64 `json:"step"`
    StepSeq uint64 `json:"stepSeq,omitempty"` // legacy alias
}
```

## Strict Monotonic Numbering

This is the most important invariant — and the one most easily missed.

### Turn numbering

- Turns are numbered from **0**
- Each `turn/start` must carry `turn == nextTurn`
- After `turn/end`, `nextTurn` increments by 1

```
turn/start {turn: 0}  → nextTurn was 0 ✓ → nextTurn stays 0 (turn open)
turn/end   {turn: 0}  → matches openTurn 0 ✓ → nextTurn becomes 1
turn/start {turn: 1}  → nextTurn was 1 ✓
turn/start {turn: 0}  → ✗ REJECTED: expected turn 1, got 0
```

### Step numbering

- Steps are numbered from **1** (not 0)
- Each new Turn **resets** `nextStep` to 1
- Each `step/start` must carry `step == nextStep`
- After `step/end`, `nextStep` increments by 1

```
turn/start {turn: 0}  → nextStep reset to 1
step/start {turn:0, step:1}  → nextStep was 1 ✓ → nextStep becomes 2
step/end   {turn:0, step:1}  → matches openStep 1 ✓
step/start {turn:0, step:2}  → nextStep was 2 ✓
turn/end   {turn: 0}
turn/start {turn: 1}  → nextStep reset to 1 again
step/start {turn:1, step:1}  → nextStep was 1 ✓ (reset worked)
```

### Why strict?

Strict monotonic numbering serves three purposes:

1. **Detect missing events** — if `turn/start {turn: 5}` appears after `turn/end {turn: 3}`, you know turns 4 was lost (or the log is corrupted)
2. **Enable deterministic replay** — given a sequence of events, you can always reconstruct the loop state without ambiguity
3. **Catch producer bugs** — if the Agent forgets to close a Step before opening a new one, the numbering mismatch surfaces immediately

## Nesting Rules

The dual loop has a strict nesting hierarchy:

```
turn/start
  ├─ (user/message)
  ├─ step/start
  │    ├─ (agent/request)
  │    ├─ (assistant/chunk...)
  │    ├─ (tool/call)
  │    ├─ (tool/result)
  │    └─ (assistant/message)
  ├─ step/end
  ├─ step/start  (optional: another LLM round)
  ├─ step/end
  └─ (turn/stopping)
turn/end
```

### Enforced invariants

| Rule | Violation | Error |
|---|---|---|
| `turn/start` requires no open Turn | Two `turn/start` without `turn/end` | `turn/start while turn already open` |
| `turn/end` requires an open Turn | `turn/end` without `turn/start` | `turn/end without open turn/start` |
| `turn/end` requires no open Step | `turn/end` while Step is open | `turn/end while step still open` |
| `step/start` requires an open Turn | `step/start` outside Turn | `step/start without open turn` |
| `step/start` requires no open Step | Two `step/start` without `step/end` | `step/start while step already open` |
| `step/end` requires an open Step | `step/end` without `step/start` | `step/end without open step/start` |
| `tool/call` requires an open Step | `tool/call` outside Step | `tool/call without open step` |
| Turn numbers strictly increase | `turn/start {turn: 0}` after `turn/end {turn: 0}` | `turn/start expected turn 1, got 0` |
| Step numbers strictly increase | `step/start {step: 1}` after `step/end {step: 1}` | `step/start expected step 2, got 1` |
| Step's turn matches open turn | `step/start {turn: 5}` while open turn is 0 | `step/start in turn 5 but open turn is 0` |

All of these are enforced in `SessionLog.applyState()` — the single write path. No event can bypass these checks.

## TurnEndReason: The Complete Vocabulary

A Turn can close for six reasons:

| Reason | Meaning | Typical Trigger |
|---|---|---|
| `completed` | Turn resolved normally | Agent produced final answer with no tool calls |
| `interrupted` | Turn was interrupted mid-execution | Context cancellation, user abort |
| `aborted` | Turn was deliberately aborted | `Agent.Cancel()` with a cancel cause |
| `blocked` | Turn was blocked by a safety gate | Approval denied, sandbox escalation rejected |
| `error` | Turn failed due to an internal error | LLM API error, tool execution panic |
| `max-tokens` | Turn hit the token limit | LLM returned `finish_reason: length` |

```go
// pkg/session/session.go
const (
    ReasonCompleted   TurnEndReason = "completed"
    ReasonFinished    TurnEndReason = "finished"    // legacy alias for completed
    ReasonInterrupted TurnEndReason = "interrupted"
    ReasonAborted     TurnEndReason = "aborted"
    ReasonBlocked     TurnEndReason = "blocked"
    ReasonError       TurnEndReason = "error"
    ReasonMaxTokens   TurnEndReason = "max-tokens"
)
```

> **Note:** `finished` is retained as a legacy alias for backward compatibility. New code should use `completed`.

## State Projection: Querying the Loop State

The `SessionLog` maintains the full loop state internally and exposes read-only query methods:

```go
// Next turn number (starts at 0, increments after each turn/end)
func (sl *SessionLog) NextTurn() uint64

// Next step number in current turn (starts at 1, resets on each turn/start)
func (sl *SessionLog) NextStep() uint64

// Currently open turn (0, false) if no turn is open
func (sl *SessionLog) OpenTurn() (uint64, bool)

// Currently open step (0, false) if no step is open
func (sl *SessionLog) OpenStep() (uint64, bool)
```

### How the Agent uses them

The Agent queries `NextTurn()` before creating a `turn/start` event, ensuring the number is always correct:

```go
// pkg/agent/agent.go — runTurn
func (a *Agent) runTurn(req *turnReq) {
    turnIdx := a.log.NextTurn()  // query current number

    if _, err := a.log.Append(session.TurnStartData{Turn: turnIdx}); err != nil {
        return
    }
    // ... steps ...
    a.log.Append(session.TurnEndData{Turn: turnIdx, Reason: session.ReasonFinished})
}
```

This pattern — **query before append** — is the canonical way to ensure strict monotonic numbering without maintaining a separate counter in the Agent.

## Streaming Block Assembly

Model output arrives as a chunk stream. `llm.BlockAssembler` is the single canonical assembler: it merges consecutive text/reasoning deltas, fixes tool-call chunks into tool-use blocks, preserves stream order, and yields the assistant message via `Message()`. Size is estimated by `tokenmeter.EstimateMessage` at fixed density (4 chars/token); crossing the budget triggers compaction — an assemble → estimate → compact loop.

## Crash Repair: Orphan Turns

If the process crashes mid-Turn, the persisted log will contain a `turn/start` without a matching `turn/end`. The persistence layer's `repairOrphanTurn` function detects this and appends a synthetic `turn/end {reason: interrupted}`:

```go
// pkg/persistence/jsonl.go — repairOrphanTurn
func repairOrphanTurn(events *[]session.SessionEvent) int {
    // Scan all events to determine if a turn is open at the end
    turnOpen := false
    var openTurn uint64
    for _, ev := range *events {
        switch ev.Type {
        case session.EventTurnStart:
            turnOpen = true
            if td, ok := ev.Data.(session.TurnStartData); ok {
                openTurn = td.Turn  // capture the turn number
            }
        case session.EventTurnEnd:
            turnOpen = false
        }
    }
    if !turnOpen {
        return 0
    }
    // Append synthetic turn/end with the correct turn number
    last := (*events)[len(*events)-1]
    repaired := session.SessionEvent{
        Seq:  last.Seq + 1,
        Time: last.Time,
        Type: session.EventTurnEnd,
        Data: session.TurnEndData{Turn: openTurn, Reason: session.ReasonInterrupted},
    }
    *events = append(*events, repaired)
    return 1
}
```

The repair preserves the correct `turn` number — it doesn't just append a bare `turn/end`. This ensures the repaired log still passes all numbering invariants.

## Interaction with Other Subsystems

### Tool Pipeline

Every `tool/call` and `tool/result` must occur inside an open Step. The tool pipeline does not manage Turn/Step itself — it relies on the Agent to have opened the appropriate Step before invoking tools.

### Approval

Approval requests (`approval/request`, `approval/decided`) can occur inside a Step. The approval system does not enforce Turn/Step nesting — that's the session invariant's job.

### Persistence

The JSONL backend persists all Turn/Step events verbatim. On load, it runs `repairOrphanTurn` to fix any crash-induced orphans. The shard/async backend (H02) batches events but preserves order and numbering.

### Cancellation

`Agent.Cancel(cause)` records a cancel cause and the Turn closes with `aborted` (or `interrupted` if the cancellation comes from context). The cancel cause is extracted via `ExtractCancelCause(events)` by scanning for `turn/stopping` events with cancel markers.

## Benefits and Costs

### Benefits

- **Deterministic replay** — any log can be replayed to reconstruct exact loop state
- **Early bug detection** — numbering mismatches catch producer bugs at write time, not hours later
- **Clean cancellation boundaries** — Turn boundaries give natural points to abort without corrupting state
- **Accurate metrics** — per-turn and per-step token/latency accounting is trivial
- **Crash recoverability** — orphan turns can be detected and repaired deterministically

### Costs

- **Producer complexity** — every event creator must know the correct turn/step number
- **No out-of-order writes** — events must be appended in strict loop order; you can't "backfill" a step later
- **Migration burden** — changing the numbering scheme (e.g., step from 0 instead of 1) requires a full log migration
- **Testing surface** — every test that creates Turn/Step events must use correct numbers or the invariant rejects them

## Source Reference

| Concept | Go Implementation | Official TypeScript |
|---|---|---|
| Event data structures | `pkg/session/session.go` — `TurnStartData`, `TurnEndData`, `StepStartData`, `StepEndData` | `packages/core/session/src/types.ts` — `SessionEventMap` |
| TurnEndReason | `pkg/session/session.go` — `TurnEndReason` const block | `packages/core/session/src/types.ts` — `TurnEndReasonMap` |
| Pairing & nesting invariants | `pkg/session/session.go` — `applyState()` | `packages/core/session/src/invariant.ts` — `validateEvent()` |
| State counters | `pkg/session/session.go` — `sessionState.nextTurn/nextStep/openTurn/openStep` | `packages/core/session/src/invariant.ts` — `SessionTrace` |
| Query methods | `pkg/session/session.go` — `NextTurn()`, `NextStep()`, `OpenTurn()`, `OpenStep()` | (not exposed in official; maintained internally) |
| Agent loop | `pkg/agent/agent.go` — `runTurn()`, `runStep()` | `packages/core/agent/src/` — agent loop |
| Crash repair | `pkg/persistence/jsonl.go` — `repairOrphanTurn()` | `packages/core/session/src/repair.ts` |
| Block assembly | `pkg/llm/assembler.go` — `BlockAssembler` | `packages/llm/llm/src/assembler.ts` |

## Next Steps

- **[Sandbox & Controlled Execution](./sandbox-execution)** — how tool calls are confined after the Step opens
- **[Session Event Sourcing](./session-event-sourcing)** — the full event vocabulary and invariant system
- **[Agent Loop & Cancellation](./agent-loop)** — how the Agent drives the Turn/Step loop and handles cancellation
