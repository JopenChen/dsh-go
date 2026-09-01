---
title: "fold Projection"
description: "State = a pure function of the event log"
weight: 20
---

# fold Projection

## In One Sentence

**The pure function that defines "how to compute a view from an event log" is the fold; the view it produces is the Projection.**

## Breaking It Down

- **fold (fold/reduce)**: accumulates events one by one into a result. Input = all events, output = a derived state, **a pure function with no side effects**.
- **Projection**: the view produced by a fold, such as "the current conversation message list", "the Goal state", or "the session title" — each is one projection.

## Core Philosophy

```
Event log (the single Source of Truth)
   ├─ fold → Projection A (message list)
   ├─ fold → Projection B (Goal state)
   └─ fold → Projection C (session title)
```

## In Dsh-Go

```go
proj := session.FoldAll(sl.Events())
for _, m := range proj.Messages {
    fmt.Printf("[%s] %s\n", m.Role, m.Content)
}
```

- `FoldAll` folds the entire log into a `SessionProjection` (containing the `Messages` / `Goal` / `Todo` / `PlanMode` sub-projections)
- **Incremental fold (H04)**: instead of recomputing on every read, it is maintained incrementally in O(N)

## Performance Data

{{< callout emoji="⚡" >}}
Incremental fold vs. brute-force recompute: **16.9s → 4.9ms, ≈ 3437×** (10k events, read per step)
{{< /callout >}}

## Source Reference

- `pkg/session/fold.go` — the projection function family
- `pkg/session/incremental.go` — incremental projection (H04)
- Runnable example: [`examples/tutorial`](https://github.com/JopenChen/dsh-go/blob/master/examples/tutorial/main.go) step 2

## Next Steps

→ [Learn the Goal State Machine](../goal-state-machine/): how an Agent plans and drives execution