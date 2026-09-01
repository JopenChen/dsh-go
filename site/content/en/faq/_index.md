---
title: "FAQ"
description: "Frequently asked questions about Dsh-Go"
weight: 3
---

Frequently asked questions about Dsh-Go. The full version is available in [docs/FAQ.md](https://github.com/JopenChen/dsh-go/blob/master/docs/FAQ.md).

## Q1. What exactly is Dsh-Go?

A pure-Go, in-process [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) Agent **reference implementation** — it translates the official DSH's Turn/Step dual loop, Event Sourcing, Goal planning, tool governance, and other core capabilities seam by seam into Go code.

## Q2. How does it relate to Eino / LangChain?

**Different species.** Eino/LangChain is a "framework to use" (general LLM orchestration); Dsh-Go is a "reference implementation to read" (a transcription of DSH). It neither intends to nor is capable of replacing the former.

## Q3. What advantages does it have over them?

Three asymmetric hard strengths: **① word-for-word alignment with the official semantics** (events/error codes/Goal four states); **② Event Sourcing + fold projection** (a structural difference, 3437× incremental); **③ prefix-cache friendly** (DeepSeek cost reduction).

## Q4. Can't mature frameworks do compliance auditing?

They can. "Being auditable" is not a unique capability; the difference lies in **capability ownership and strength of evidence**: mature frameworks rely on external hooks (callbacks/manual persistence), while Dsh-Go auditing is built-in (append-only events are the source of truth, intermediate states can be replayed).

## Q5. What enterprise scenarios is it suitable for?

Primary battlefield = **audit replay + DeepSeek cost reduction + controlled tool execution**, compounded, and anchored on planning-based Agent platforms built on DeepSeek.

## Q6. Does this project have real value?

The technical value is real (word-for-word reproduction + benchmark data), but it ≠ external production-adoption value (zero ecosystem, no endorsement). It is **verification + reference** value — suitable for personal use, teaching, or a portfolio, not as a production replacement.

## Q7. Recommended learning path to get started?

Start with [`examples/tutorial`](/en/tutorials/), then read through `pkg/session` / `pkg/goal`, and finally dive into `pkg/agent` / `pkg/tools` / `pkg/llm`.

## Q8. Useful for replicating / re-implementing an Agent?

It is a **ready-made transcription template**: every seam is split into independent small modules + comments + tests — effectively a "disassembly manual of an Agent core implementation".

## Q9. What is Event Sourcing?

See [Tutorial: Event Sourcing](/en/tutorials/event-sourcing/).

## Q10. What is fold projection?

See [Tutorial: fold projection](/en/tutorials/fold-projection/).

## Q11. Does it support models other than DeepSeek?

Currently only `provider_deepseek` is built in; however, the `LLMAdapter` seam is extensible — implementing 3 methods is enough to connect a model (an OpenAI-compatible provider approach is described in [docs/FAQ.md](https://github.com/JopenChen/dsh-go/blob/master/docs/FAQ.md) Q11).

---

> The full version (including comparison tables, code, and criteria) is available in [docs/FAQ.md](https://github.com/JopenChen/dsh-go/blob/master/docs/FAQ.md).