---
title: "Goal State Machine"
description: "Four-state planning + stable error codes + round continuation"
weight: 30
---

# Goal State Machine

## In One Sentence

**The core of an Agent's "planning capability" is a state machine: a Goal has a lifecycle and transitions from one phase to the next; the Agent drives it with round continuation until it is reached or blocked.**

## The Four States (Aligned with the Official)

| State | Meaning | Auto round continuation |
|---|---|---|
| `active` | In progress | ✅ Yes (RoundDriver continues) |
| `paused` | Paused | ❌ No (waiting to resume) |
| `blocked` | Blocked (blocker unresolved) | ❌ No |
| `complete` | Completed | ❌ No |

## Stable Error Codes

Nine stable `GOAL_*` error codes aligned with the official `error.ts` (e.g. `GOAL_INVALID_MAX_ROUNDS`, `GOAL_STALE_REVISION`, `GOAL_NOT_FOUND`). Errors are routed by stable string, never by parsing message text.

## In Dsh-Go

```go
ts := goal.NewGoalToolset(sl) // bound to the session log
// 6 tools: goal_list / goal_set_phase / goal_set_description
//         / goal_set_max_rounds / goal_add_blocker / goal_report_blocker
```

The example shows how a stable error code is expressed cleanly:

```go
if _, err := call(ts, "goal_set_max_rounds", map[string]any{"maxRounds": float64(-1)}); err != nil {
    if ge, ok := err.(*goal.GoalError); ok {
        fmt.Println(ge.Code) // GOAL_INVALID_MAX_ROUNDS
    }
}
```

## Source Reference

- `pkg/goal/goal.go` — the Goal state machine and its 6 tools
- `pkg/goal/errors.go` — 9 stable error codes + GoalError
- Runnable example: [`examples/tutorial`](https://github.com/JopenChen/dsh-go/blob/master/examples/tutorial/main.go) step 3

## Next Steps

- Explore [more tutorials](../) or [examples](/en/examples/)
- Check out the [FAQ](/en/faq/)