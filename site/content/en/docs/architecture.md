---
title: "Architecture"
description: "Event Sourcing + Turn/Step Loop + Tool Waterfall"
weight: 20
---

# Architecture

## Overall Flow

```
Session (Event Sourcing) ──► fold / Projection ──► Prompt Assemble ──► Agent Turn/Step Loop
                                                                        │
                        Tool Waterfall (pre → execute → post → result) ◄─┘
```

## Three Pillars

### 1. Event Sourcing

- Session state is fully derived from an **append-only event log**
- Every write appends one immutable event; the engine enforces ordering invariants (turn open/close pairing, tool call↔result matching)
- At any moment the log can be replayed, forked, compacted, and persisted

### 2. Turn/Step Loop (Agent Loop)

- **Turn**: one complete conversational round-trip (user message → Agent processing → end)
- **Step**: a tool continuation within a Turn (the model requests a tool → execute → feed the result back to the model)
- Cancellation, timeouts, and tracing are propagated layer by layer through `ctx` to both tools and the LLM

### 3. Tool Waterfall

- A four-stage chain: `pre → execute → post → result`
- Each stage is a pluggable middleware: approval, sandbox, token budget, and limits can all be mounted
- Supports concurrency hardening such as object pools and read-only registries

## Capability Seam

Every capability is a **service definition + provider** structure: replacing the provider changes the overall behavior, consistent with the official design.

## Related

- [Capability Overview](../capabilities/)
- [Performance data](https://github.com/JopenChen/dsh-go#-performance)