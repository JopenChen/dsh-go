---
title: "Capability Overview"
description: "The role and relationships of each capability package"
weight: 30
---

# Capability Overview

Every capability package in Dsh-Go follows the **Capability Seam** pattern: `service definition + provider`, where replacing the provider changes the overall behavior. The following are grouped by functional domain.

## Core Runtime

| Package | Capability |
|---|---|
| [`pkg/session`](https://github.com/JopenChen/dsh-go/tree/master/pkg/session) | Event Sourcing, event vocabulary, fold, invariants, incremental projection |
| [`pkg/agent`](https://github.com/JopenChen/dsh-go/tree/master/pkg/agent) | Agent registry, Turn/Step loop, cancellation reasons, request-error retry |
| [`pkg/tools`](https://github.com/JopenChen/dsh-go/tree/master/pkg/tools) | four-stage pipeline, execution context, presentation, limits, schema, preservation |
| [`pkg/llm`](https://github.com/JopenChen/dsh-go/tree/master/pkg/llm) | LLM seam, streaming protocol, failure classification, retry, cache probe |

## Planning Primitives

| Package | Capability |
|---|---|
| [`pkg/goal`](https://github.com/JopenChen/dsh-go/tree/master/pkg/goal) / [`pkg/todo`](https://github.com/JopenChen/dsh-go/tree/master/pkg/todo) | Goal State Machine (four states + stable error codes), Todo whole replacement |
| [`pkg/plan`](https://github.com/JopenChen/dsh-go/tree/master/pkg/plan) / [`pkg/skills`](https://github.com/JopenChen/dsh-go/tree/master/pkg/skills) | plan patterns, skills system (6-layer provider) |

## Persistence & Storage

| Package | Capability |
|---|---|
| [`pkg/persistence`](https://github.com/JopenChen/dsh-go/tree/master/pkg/persistence) / [`pkg/storage`](https://github.com/JopenChen/dsh-go/tree/master/pkg/storage) | JSONL (sharding/async) and SQLite backends, CAS storage domain |

## Subagents & Workflow

| Package | Capability |
|---|---|
| [`pkg/subagent`](https://github.com/JopenChen/dsh-go/tree/master/pkg/subagent) / [`pkg/workflow`](https://github.com/JopenChen/dsh-go/tree/master/pkg/workflow) / [`pkg/mcp`](https://github.com/JopenChen/dsh-go/tree/master/pkg/mcp) | subagents, workflow engine, MCP client→tool bridge |

## Files & Processes

| Package | Capability |
|---|---|
| [`pkg/fs`](https://github.com/JopenChen/dsh-go/tree/master/pkg/fs) / [`pkg/shell`](https://github.com/JopenChen/dsh-go/tree/master/pkg/shell) / [`pkg/subprocess`](https://github.com/JopenChen/dsh-go/tree/master/pkg/subprocess) / [`pkg/jobs`](https://github.com/JopenChen/dsh-go/tree/master/pkg/jobs) / [`pkg/terminal`](https://github.com/JopenChen/dsh-go/tree/master/pkg/terminal) | file & process execution, jobs, terminal |

## Configuration & Security

| Package | Capability |
|---|---|
| [`pkg/settings`](https://github.com/JopenChen/dsh-go/tree/master/pkg/settings) / [`pkg/credentials`](https://github.com/JopenChen/dsh-go/tree/master/pkg/credentials) / [`pkg/approval`](https://github.com/JopenChen/dsh-go/tree/master/pkg/approval) / [`pkg/sandbox`](https://github.com/JopenChen/dsh-go/tree/master/pkg/sandbox) / [`pkg/scope`](https://github.com/JopenChen/dsh-go/tree/master/pkg/scope) | configuration, credentials, approval, sandbox, scope |

## Observability

| Package | Capability |
|---|---|
| [`pkg/telemetry`](https://github.com/JopenChen/dsh-go/tree/master/pkg/telemetry) / [`pkg/tokenmeter`](https://github.com/JopenChen/dsh-go/tree/master/pkg/tokenmeter) / [`pkg/feedback`](https://github.com/JopenChen/dsh-go/tree/master/pkg/feedback) / [`pkg/sessionquery`](https://github.com/JopenChen/dsh-go/tree/master/pkg/sessionquery) | observability, metering, feedback, search |