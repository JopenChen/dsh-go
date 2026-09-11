---
title: "Capability Seams & LLM Adapter"
description: "The Definition / Provider / Consumer replaceable seam, and how to plug in any model endpoint"
weight: 42
---

# Capability Seams & LLM Adapter

## In One Sentence

**Every replaceable capability upstream consists of three roles: Definition (the interface contract), Provider (a concrete implementation), and Consumer (uses the contract without depending on the implementation); the LLM capability is the prime example — through the unified LLMAdapter interface and StreamChunk protocol, dsh-go can attach any OpenAI-compatible endpoint without touching the upper loop.**

## Three Capability Roles

Service Definition, Service Provider, and Consumer together form a capability **seam** — a boundary where the implementation can be swapped without changing consumers:

| Role | Responsibility | dsh-go counterpart |
|---|---|---|
| Definition | interface and data contract | Go `interface` + request/result structs |
| Provider | supply and register an implementation | implement interface, `reg.Put` at wiring |
| Consumer | look up by key/interface, never import impl | `reg.Get` or injected interface |

The key constraint: **consumers depend on the abstraction**. The Agent loop only knows it holds an `LLMAdapter`, not whether it is DeepSeek, a local model, or a test stub.

```go
type LLMAdapter interface {
    Name() string
    Chat(ctx context.Context, req ChatRequest, cb func(StreamChunk)) (Usage, error)
}
func NewAgent(llm LLMAdapter) *Agent { ... }
```

## LLM Adapter: Any Model

To attach your own endpoint, implement two methods of `pkg/llm.LLMAdapter`:

- `Name()` returns the adapter id;
- `Chat(ctx, req, cb)` starts a **streaming** conversation; each chunk goes through `cb`, and it finally returns `Usage`.

The adapter translates the unified `ChatRequest` into the target wire format and the endpoint's SSE stream back into unified `StreamChunk`s. The upper layer is thus fully model-agnostic.

## StreamChunk Protocol

`pkg/llm.StreamChunk` uses a `Kind` field for four chunk types:

| Kind | Payload |
|---|---|
| `ChunkText` | text delta |
| `ChunkReasoning` | reasoning delta |
| `ChunkToolCall` | tool-call delta (args may arrive in pieces) |
| `ChunkDone` | stream finished |

### Error Handling in Streams

- **Chunk-level tolerance**: one bad chunk must not lose received text;
- **Error chain**: `pkg/llm/errorchain.go` wraps network, HTTP, and cancellation errors into a discriminable chain (`errors.Is/As`);
- **Retry**: `pkg/llm/retry.go` only backs off on retryable errors (timeout, 429, 5xx); deterministic failures (4xx) return immediately;
- **Cache**: `pkg/cache` serves deterministic requests to cut cost and latency.

## Input Boundary Validation

External data is validated before entering the core: `attachment.DecodeCanonicalBase64` rejects non-canonical base64 and `NewLimiter` bounds image-transform concurrency; `feedback.ValidateRating/ValidateNote` constrain rating and note; `skills.IsName/RenderContent` enforce the kebab grammar and escape embedded text; `mcp.PublicToolName` derives `mcp__server__raw` and appends an identity hash on lossy normalization to prevent collapse.

## Anonymous Identity

`telemetry.GetOrCreateAnonymousID` mints a random UUID per Harness home, persisted as `.anonymous-user-id`, never derived from host or network. A write failure still returns a usable id.

## Why the Three Roles Matter

This is how "everything is a plugin, freely replaceable" lands: swap a Provider without touching Consumers; inject a stub in tests; compose multiple Providers behind a routing adapter. The cost is upfront interface design — an unstable Definition moves every Consumer.

## Source Map

| Concept | Go implementation | Upstream TypeScript |
|---|---|---|
| Adapter contract | `pkg/llm/llm.go` — `LLMAdapter` | llm adapter |
| Streaming protocol | `pkg/llm/llm.go` — `StreamChunk` | stream chunk |
| Error chain | `pkg/llm/errorchain.go` | (dsh-go addition) |
| Retry | `pkg/llm/retry.go` | (dsh-go addition) |
| Capability registry | `pkg/registry/registry.go` | Cordis service |
| Attachment admission/limiter | `pkg/attachment/admission.go`, `limiter.go` | attachment/admission |
| Feedback validation | `pkg/feedback/validate.go` | message-feedback/spec |
| Skill grammar | `pkg/skills/grammar.go` | skill/src/index |
| MCP public name | `pkg/mcp/publicname.go` | mcp-client/tools |
| Anonymous identity | `pkg/telemetry/anonymous.go` | identity/anonymous-user-id |

## Next Steps

- **[Tool Execution Pipeline](./tool-pipeline)**
- **[Plugin Kernel & Event System](./plugin-kernel)**
- **[Agent Loop & Runtime](./agent-loop)**
