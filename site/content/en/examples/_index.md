---
title: "Examples"
description: "9 zero-dependency teaching examples"
weight: 4
---

The repository ships **9 runnable examples**, all zero dependencies out of the box (except `chat`, which needs an API Key). Source code lives in [`examples/`](https://github.com/JopenChen/dsh-go/tree/master/examples).

## Running the examples

```bash
# Teaching entry point (start with this)
go run ./examples/tutorial

# The rest of the examples
go run ./examples/agent_loop
go run ./examples/usage
go run ./examples/todo
go run ./examples/workflow
go run ./examples/subagent
go run ./examples/sandbox_approval
go run ./examples/mcp
go run ./examples/chat   # requires DEEPSEEK_API_KEY
```

## Example overview

| Example | Demonstrated content | Dependency |
|---|---|---|
| `tutorial` | Event Sourcing → fold projection → Goal state machine (teaching entry point) | none |
| `agent_loop` | Agent Turn/Step dual loop + tool continuation steps | none |
| `usage` | Session/projection/Goal tools/commands/persistence | none |
| `todo` | Todo wholesale replacement of the todo list (planning primitive) | none |
| `workflow` | Pipeline serial / Parallel parallel / cancellation cascading | none |
| `subagent` | Multi-backend derivation + family tree + parent-release cascading | none |
| `sandbox_approval` | Approval three states + sandbox mode | none |
| `mcp` | MCP client → tool bridge (in-memory Transport) | none |
| `chat` | Real DeepSeek multi-turn conversation | API Key required |

## Companion tutorials

Each example ships with source-mapping guidance (see [tutorials](/en/tutorials/)). It is recommended to read the "example + source + tutorial" trio together.