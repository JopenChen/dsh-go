---
title: "Tutorials"
description: "A step-by-step learning path into the Agent kernel"
weight: 2
---

The tutorial section is at the heart of what Dsh-Go offers. We have designed a progressive path around "building an understanding of the Agent kernel from scratch", where every step ships with a runnable code example and a source reference.

## Core Kernel (Three Steps)

1. [Event Sourcing](event-sourcing/) — why "recording events instead of state" is more robust
2. [fold Projection](fold-projection/) — how state is "derived" from the event log
3. [Goal State Machine](goal-state-machine/) — how an Agent turns a goal into an execution loop with round continuation

## Agent Loop

4. [Turn / Step Dual Loop](turn-step-loop/) — how a conversation is structured as nested Turn and Step loops with strict monotonic numbering

## Safety & Governance

5. [Sandbox & Controlled Execution](sandbox-execution/) — where can the Agent write? Three sandbox modes, policy resolution, and fail-closed confinement

## Running It

```bash
# Core kernel tutorial (three steps in one)
go run ./examples/tutorial

# Sandbox & approval deep-dive
go run ./examples/sandbox_approval
```

## Source Reference

- `pkg/session/session.go` — the event log and its event vocabulary (including Turn/Step data structures)
- `pkg/session/fold.go` — the fold projection function family
- `pkg/goal/goal.go` — the Goal state machine
- `pkg/agent/agent.go` — the Agent loop driving Turn/Step dual loop
- `pkg/sandbox/sandbox.go` — three sandbox modes and fail-closed confinement
- `pkg/approval/approval.go` — the approval policy (the other safety gate)

## Roadmap

{{< callout emoji="🚀" >}}
This project is positioned as a reference implementation and teaching material. More topic-specific tutorials will keep being added here: tool governance, sub-agent orchestration, MCP bridging, cache affinity, and more.
{{< /callout >}}
