---
title: "Dsh-Go"
layout: hextra-home
---

<!-- ========== Hero ========== -->
<div class="hx-mt-10 hx-mb-8">
{{< hextra/hero-badge >}}
  <div class="hx-w-2 hx-h-2 hx-rounded-full hx-bg-primary-400"></div>
  <span>In-process Go reference implementation of the DeepSeek Harness Agent</span>
{{< /hextra/hero-badge >}}

{{< hextra/hero-headline >}}
  Understand the Agent kernel.<br/>Build with Dsh-Go.
{{< /hextra/hero-headline >}}

{{< hextra/hero-subtitle >}}
  A pure-Go, in-process reference implementation of the DeepSeek Harness Agent.
  Event sourcing, fold projection, Goal planning and tool governance — translated
  line by line into idiomatic Go, so you can read, debug and rebuild a real Agent.
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx-mb-10">
{{< hextra/hero-button text="Quick Start" link="/en/tutorials/" >}}
{{< hextra/hero-button text="Read the Docs" link="/en/docs/" >}}
{{< hextra/hero-button text="GitHub" link="https://github.com/JopenChen/dsh-go" >}}
</div>

<div class="hx-mt-8"></div>

<!-- ========== Glass feature cards ========== -->
## What Dsh-Go gives you

Three pillars make up the Agent kernel. Each is a **Capability Seam**: a service
definition plus a swappable provider, so replacing one provider changes behavior
without touching the rest.

{{< cards >}}
  {{< card link="/en/tutorials/event-sourcing/" icon="database" title="Event Sourcing"
          subtitle="Every session is an append-only event log. Replay, fork, compress and persist — with engine-enforced temporal invariants." >}}
  {{< card link="/en/tutorials/fold-projection/" icon="chart-bar" title="Fold Projection"
          subtitle="Session state is a pure function of the event log. Incremental O(N) projections keep views cheap and consistent." >}}
  {{< card link="/en/docs/intro/" icon="shield-check" title="Goal & Tool Governance"
          subtitle="A four-state Goal machine with stable error codes, plus a four-stage tool waterfall for approval, sandbox and limits." >}}
{{< /cards >}}

## The Agent kernel, decoded

Dsh-Go answers one question: **how does a real Agent work inside?** Instead of a
black box, it gives you the actual machinery — and each piece is small enough to
grasp in an afternoon.

| Core concept | What it means | Learn it |
|---|---|---|
| **Event Sourcing** | No mutable state; append immutable events, derive state from them | [Tutorial](/en/tutorials/event-sourcing/) |
| **Fold Projection** | State = a pure function of the event log, incremental O(N) | [Tutorial](/en/tutorials/fold-projection/) |
| **Goal State Machine** | `active / paused / blocked / complete` planning with stable error codes | [Tutorial](/en/tutorials/goal-state-machine/) |
| **Turn / Step Loop** | One turn per user message; steps run tools and feed results back | [Architecture](/en/docs/architecture/) |
| **Tool Waterfall** | `pre → execute → post → result` pipeline with pluggable middleware | [Capabilities](/en/docs/capabilities/) |

## 9 zero-dependency examples

Start from [`examples/`](https://github.com/JopenChen/dsh-go/tree/master/examples)
and run `go run ./examples/tutorial` — no LLM key needed until the very last one.

| Example | What it demonstrates |
|---|---|
| `tutorial` | Event sourcing → fold projection → Goal machine (the teaching entry) |
| `agent_loop` | Turn/Step double loop + tool continuation |
| `usage` | Session, projection, Goal tool, commands, persistence |
| `todo` | Swapping a capability end-to-end |
| `workflow` | Pipeline / parallel / cancellation cascade |
| `subagent` | Multi-provider derivation + parent-release cascade |
| `sandbox_approval` | Approval states + sandbox mode |
| `mcp` | MCP client → tool bridge |
| `chat` | Real DeepSeek multi-turn chat (requires an API key) |

## Start from source

- [GitHub repository](https://github.com/JopenChen/dsh-go)
- [Documentation](/en/docs/)
- [FAQ](/en/faq/)
