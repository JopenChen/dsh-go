---
title: "Documentation"
description: "Learn about the architecture, capabilities, and design philosophy of Dsh-Go"
weight: 1
---

Welcome to the dsh-go documentation. This project is positioned as an **in-process Go reference implementation of the DeepSeek Harness Agent**, with the goal of letting developers read, debug, and reproduce the inner workings of a real Agent.

## Getting Started

- [Introduction](intro/) — what it is and why it exists
- [Architecture](architecture/) — Event Sourcing + Turn/Step Loop + Tool Waterfall
- [Capability Seam](capabilities/) — the role and relationships of each capability package

## Key Concepts

- **Event Sourcing**: no stored state — only append-only immutable events; state is derived from events
- **fold Projection**: a pure function that "computes" a view from the event log
- **Goal State Machine**: active / paused / blocked / complete four-state planning
- **Tool Governance**: four-stage pipeline + approval + sandbox

## Further Reading

- [FAQ](/en/faq/) — relationship to Eino/LangChain, scenarios, value
- [Examples](/en/examples/) — 9 zero-dependency teaching examples
- [GitHub repository](https://github.com/JopenChen/dsh-go) — source code and history