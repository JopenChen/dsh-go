---
title: "Tutorials"
description: "A step-by-step learning path into the Agent kernel"
weight: 2
---

The tutorial section is at the heart of what Dsh-Go offers. We have designed a three-step progressive path around "building an understanding of the Agent kernel from scratch", where every step ships with a runnable code example (`examples/tutorial`) and a source reference.

## Three-Step Path

1. [Event Sourcing](event-sourcing/) — why "recording events instead of state" is more robust
2. [fold Projection](fold-projection/) — how state is "derived" from the event log
3. [Goal State Machine](goal-state-machine/) — how an Agent turns a goal into an execution loop with round continuation

## Running It

```bash
go run ./examples/tutorial
```

## Source Reference

- `pkg/session/session.go` — the event log and its event vocabulary
- `pkg/session/fold.go` — the fold projection function family
- `pkg/goal/goal.go` — the Goal state machine

## Roadmap

{{< callout emoji="🚀" >}}
This project is positioned as a reference implementation and teaching material. More topic-specific tutorials will keep being added here: tool governance, sub-agent orchestration, MCP bridging, cache affinity, hands-on event sourcing, and more.
{{< /callout >}}