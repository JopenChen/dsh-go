---
title: "Defensive Patterns & Postmortem"
description: "Three defensive classes — result reporting, cleanup, credentials — and the four-question postmortem culture"
weight: 44
---

# Defensive Patterns & Postmortem

## In One Sentence

**A long-running Agent that calls tools autonomously must defend three boundaries: report results faithfully (no swallowed errors, no fake success), clean up resources deterministically (no leaked handles/temp files/listeners), and minimize credential exposure (no keys in logs/results); when a bug still escapes, a postmortem focuses not on the one-line fix but on why every safety net failed and what new guard makes the same problem fail loudly next time.**

## Three Defensive Classes

### 1. Result Reporting: Failure Must Be Visible

- **Never swallow errors**: a tool error must set `IsError`, never return an empty value faking success;
- **Discriminable errors**: an error chain (`pkg/llm/errorchain.go`) separates retryable from deterministic failures;
- **panic backstop**: external callbacks may panic; `waterfall.Chain.RunSafe` turns panic into error at the boundary;
- **Explicit truncation**: oversized results are truncated and labeled by post-execute, never silently dropped.

### 2. Cleanup: Register Returns Its Inverse

```go
dispose := bus.On(handler)
defer dispose()                 // listener removed on teardown

proc, kill, err := subprocess.Start(...)
defer kill()                    // child process reaped at Turn end
```

- Subprocesses/terminal sessions close deterministically at Turn/Step end, cancellation propagated via ctx;
- Temp artifacts land in the spill directory (`pkg/spill`) and are cleaned afterwards;
- dispose/cleanup are **idempotent**, safe across multiple cleanup paths.

### 3. Credentials: Minimal Exposure

- **Reference, not plaintext**: capabilities hold a `CredentialRef`, resolving via `credentials.Store.Resolve` only at use; keys never flow in cleartext in the config tree, prompts, or event log;
- **Dump redaction**: paths marked by `settings.MarkSecret` are redacted in `Describe`; `credentials.Store.Describe` returns metadata only, never values;
- **Per-request fetch**: `pkg/credentials` manages lifecycle (Set/Unset) and authorization flows.

## Three Runtime Guardrails

### Cooperative Timeout

A tool declares `TimeoutMs`; `tools.WrapTimeout` arms the deadline and maps its own expiry to `TOOL_TIMEOUT`. A parent ctx cancelling first reads as an ordinary cancel. It is **cooperative** — Go cannot kill a goroutine, so the tool must observe `ctx.Done()`.

### Repeat Reminder

`tools.RepeatState` counts consecutive identical calls. `CanonicalizeArgs` deep-sorts keys so property order does not matter. At thresholds (default 3/5/8) it emits a gentle-then-detailed reminder, **advisory only, never a veto**. A user interjection resets the chain.

### Tool Pairing

A compaction cut must never sit between a tool_call and its tool_result. `compaction.BalancedCuts` treats tool calls as +1 and results as -1; only zero-balance cuts are legal. `NearestBalancedFrom` moves a desired cut to the nearest balanced position. A single over-long result is handled by `compaction.PruneText`, which keeps the head and tail and replaces the middle with a marker (rune-safe slicing).

## Postmortem: Four Questions

| Question | What to answer |
|---|---|
| What broke | a short paragraph graspable in thirty seconds |
| Mechanism | plain root cause, no blame |
| Why every safety net failed | gaps in tests/tools/conventions, not a one-off typo |
| What guard was added | tests/rules/assertions so it fails loudly next time |

**Not every bug deserves a postmortem** — only when it is simultaneously: (1) **hidden** — the mechanism is non-obvious; (2) **systemic** — it escaped through a gap in tests/tools/conventions; (3) **costly to rediscover** — it consumed real debugging time and would again.

### A Case

Upstream postmortem 0001: a plugin added an extra `export default apply`; the loader got a bare function and dropped its `inject`, so the editor crashed on connect because it could not obtain the `agents` service. Lesson: **the count of green unit tests does not prove wiring is correct** — 178 passing tests never covered the "load a real plugin" path. The added guard is an integration assertion over the loading/wiring chain so a missing dependency fails at startup, not on first call.

## Fix-to-Guard Loop

Defensive patterns push errors left to write/startup/compile time; when something still escapes, a postmortem upgrades the one-off fix into a durable guard — a failing test, an enforced rule, a fail-closed assertion. The safety net grows denser after every incident.

## Source Map

| Concept | Go implementation | Upstream TypeScript |
|---|---|---|
| panic backstop | `pkg/waterfall/waterfall.go` — `RunSafe` | defensive patterns |
| Process cleanup | `pkg/subprocess`, `pkg/terminal` | cleanup conventions |
| Temp artifacts | `pkg/spill` | (dsh-go counterpart) |
| Credential store | `pkg/credentials/credentials.go` — `Store` | credentials |
| Secret redaction | `pkg/settings/settings.go` — `MarkSecret` | (dsh-go addition) |
| Cooperative timeout | `pkg/tools/timeout.go` — `WrapTimeout` | guard/timeout-policy |
| Repeat reminder | `pkg/tools/repeat.go` — `RepeatState` | guard/repeat-tool-reminder |
| Tool pairing | `pkg/compaction/pairing.go` — `BalancedCuts` | compaction/tool-pairing |
| Head-tail prune | `pkg/compaction/pruner.go` — `PruneText` | compaction/compaction-tool-result-pruner |
| Error chain | `pkg/llm/errorchain.go` | (dsh-go addition) |

## Next Steps

- **[Bundle & Profile Layering](./bundle-profile)**
- **[Tool Execution Pipeline](./tool-pipeline)**
- **[Sandbox & Controlled Execution](./sandbox-execution)**
