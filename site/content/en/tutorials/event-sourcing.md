---
title: "Event Sourcing"
description: "Don't store state; append immutable events only"
weight: 10
---

# Event Sourcing

## In One Sentence

**Instead of directly storing the "current state", repeatedly "append" one immutable event after another; any time you need state, recompute it from those events.**

## Compared with the Traditional Approach

| | Traditional (State Snapshot) | Event Sourcing |
|---|---|---|
| What is stored | The "current shape" (e.g. `turn=3`), overwritten on every change | "What happened" (one event appended at a time), never deleted |
| Crash / corrupted write | The old state is unrecoverable after an overwrite | The event log survives and can be replayed back to any point in time |
| Audit / replay | Only the current value exists; history relies on external logs | The full history is the data itself, naturally replayable |
| Derived state | Kept as a single snapshot | Fold one out on demand at any time |

## In Dsh-Go

```go
sl := session.NewSessionLog(brand.NewSessionID("demo"))
sl.Append(session.UserMessageData{Content: "Hello"})
sl.Append(session.AssistantMessageData{Content: "Hi, I'm Dsh-Go."})

evs := sl.Events() // read out this "immutable fact log"
```

Key points:

- **append-only**: only append; never modify or delete history
- **temporal invariants**: paired `turn` open/close, `tool call` ↔ `result` matching — violations are rejected immediately
- **single write entry point**: `Append()`, with consistency guaranteed by the engine

## Benefits and Costs

{{< callout emoji="✅" >}}
**Benefits**: auditable, replayable, forkable/compactable, crash-recoverable
{{< /callout >}}

{{< callout emoji="⚠️" >}}
**Costs**: the log keeps growing, so you need compaction and projection to read state
{{< /callout >}}

## Source Reference

- `pkg/session/session.go` — the event log and 45+ event vocabulary
- Runnable example: [`examples/tutorial`](https://github.com/JopenChen/dsh-go/blob/master/examples/tutorial/main.go) step 1

## Next Steps

→ [Learn fold Projection](../fold-projection/): how state is "derived" from events