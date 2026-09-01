---
title: "Introduction"
description: "What Dsh-Go is, why it exists, and what it is not"
weight: 10
---

# Introduction

## What It Is

**Dsh-Go** is a pure-Go, in-process **reference implementation** of the [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) Agent.

It **word-for-word re-expresses** the official DSH's core capability seams — the Turn/Step Loop, Event Sourcing, Goal planning, and tool governance — as Go code. It is not just another ReAct skeleton, but a readable, debuggable, and reproducible translation of the official semantic system.

## Why It Exists

- The official project only ships a TypeScript main repository and a Python minimal version; a readable Go counterpart is missing
- Core concepts such as Event Sourcing, fold Projection, and the Goal State Machine need a **small, self-contained, well-commented** implementation to understand
- It serves as a **semantic reference** when re-implementing an Agent

## What It Is Not

{{< callout emoji="⚠️" >}}
Dsh-Go is **not** a production-grade framework, and it is **not** a replacement for LangChain / Eino / the official DSH. Ecosystem, endorsement, and multi-model adaptation are all out of scope. As a reference implementation and learning material it is well suited; don't expect it to replace mature frameworks.
{{< /callout >}}

## Three Core Threads

| Thread | Core Mechanism | Teaching Example |
|---|---|---|
| Event Sourcing | append-only event log + ordering invariants | [tutorial](/en/tutorials/event-sourcing/) |
| fold Projection | state = pure function of events, incremental O(N) | [tutorial](/en/tutorials/fold-projection/) |
| Goal State Machine | four states + stable error codes + continuation-loop driving | [tutorial](/en/tutorials/goal-state-machine/) |

## Next Steps

- Read the [architecture](../architecture/) to learn about the design
- Get hands-on with the [tutorials](/en/tutorials/)
- Check the [FAQ](/en/faq/) to resolve questions