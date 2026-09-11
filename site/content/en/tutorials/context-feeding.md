---
title: "Context Feeding: Instructions, Clock and Schedule"
description: "How instruction-file discovery, clock context and in-process scheduling let the agent know the rules, the time, and wake up on schedule"
weight: 45
---

# Context Feeding: Instructions, Clock and Schedule

## In One Sentence

**An agent does not magically know project rules or the current time, nor wake itself in the future; dsh-go feeds context via `pkg/instructions` (instruction chain), `pkg/timecontext` (clock sampling) and `pkg/schedule` (reminders).**

## Instruction Files

`pkg/instructions` mirrors the official discovery: `FindProjectRoot(cwd, markers)` walks upward to the first directory containing a marker such as `.git`; `AncestorChain(root, cwd)` yields the broad-to-narrow directory chain; `DedupByDirectory` collapses trimmed duplicates within the same directory only.

## Clock Context

`pkg/timecontext.FormatElapsed` compacts elapsed time into `d/h/m/s`; `Render(now, loc, previous)` emits the current time, zone and elapsed time since the preceding model-visible message, or `unavailable` when absent.

## Schedule

`pkg/schedule` is a concurrency-safe in-process scheduler with three rules:

| Rule | Meaning | Constraint |
|---|---|---|
| `After` | relative one-shot | positive delay |
| `At` | absolute one-shot | future target |
| `Every` | fixed rate | interval >= 5 minutes |

Due items are delivered via `Out()`; one-shots are removed after dispatch while recurring ones re-arm. Delivery stays session-local.

## Source Map

| Concept | Go | Official TypeScript |
|---|---|---|
| Instruction discovery | `pkg/instructions/instructions.go` | `context/agent-instructions/src/files.ts` |
| Clock context | `pkg/timecontext/timecontext.go` | `context/time-context/src/index.ts` |
| In-process schedule | `pkg/schedule/schedule.go` | `schedule/schedule/src/runtime.ts` |

## Next Steps

- Review [Defensive Patterns]({{< ref "defensive-patterns" >}}) for the three runtime guardrails;
- Or return to [Capability Seams]({{< ref "capability-seams" >}}).
