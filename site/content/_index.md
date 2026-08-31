---
title: "dsh-go"
layout: hextra-home
---

<div class="hx-mt-6 hx-mb-6">
{{< hextra/hero-badge >}}
  <div class="hx-w-2 hx-h-2 hx-rounded-full hx-bg-primary-400"></div>
  <span>An in-process Go reference implementation of the DeepSeek Harness Agent</span>
{{< /hextra/hero-badge >}}

{{< hextra/hero-headline >}}
  Understand the Agent kernel — start with dsh-go
{{< /hextra/hero-headline >}}

{{< hextra/hero-subtitle >}}
  An in-process Go reference implementation of the DeepSeek Harness Agent — event sourcing, fold projection, Goal planning, tool governance, all translated into Go.
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx-mb-6">
{{< hextra/hero-button text="Quick Start" link="/en/tutorials/" >}}
{{< hextra/hero-button text="FAQ" link="/en/faq/" >}}
{{< hextra/hero-button text="GitHub" link="https://github.com/JopenChen/dsh-go" >}}
</div>

<div class="hx-mt-6"></div>

## Three core tracks

| Track | What you learn | Tutorial |
|---|---|---|
| **Event Sourcing** | Append-only events, replayable anytime | [Start](/en/tutorials/event-sourcing/) |
| **Fold Projection** | State = pure function of events, O(N) | [Start](/en/tutorials/fold-projection/) |
| **Goal State Machine** | 4-phase planning + stable error codes | [Start](/en/tutorials/goal-state-machine/) |

## 9 zero-dependency examples

All in [`examples/`](https://github.com/JopenChen/dsh-go/tree/master/examples). Start with `go run ./examples/tutorial`.

## From source

- [GitHub repository](https://github.com/JopenChen/dsh-go)
- [Documentation](/en/docs/)
- [FAQ](/en/faq/)
