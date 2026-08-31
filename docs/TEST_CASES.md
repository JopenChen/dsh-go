# Dsh-Go 全面测试用例表 (TEST_CASES.md)

> 本文件基于 [tasks.json](./tasks.json) 的 **98 项功能点**逐一分析生成，是 Dsh-Go 项目的**全量测试用例设计表**。
>
> - 覆盖范围：M01-M48（MUST）/ S01-S16（SHOULD）/ C01-C17（COULD）/ N01-N09（缓存命中率）/ H01-H08（并发加固），共 98 项。
> - 用例编号：`TC-<任务ID>-<序号>`，如 `TC-M01-01` 表示 M01 任务的第 1 条用例。
> - 状态口径：以 tasks.json **逐条 status** 为准（`completed` / `pending` / `deferred` / `skipped`）。
> - 关联文件：已完成任务标注其已有测试文件（`tests/*_test.go`）；pending 任务标注**待实现后补测**的目标测试文件。

---

## 0. 概览

### 0.1 任务分布统计（按 tasks.json 逐条 status 统计）

| 级别 | 簇 | 任务数 | ✅完成 | ⚪待做 | ⏳延后 | ⚫跳过 |
|---|---|---|---|---|---|---|
| MUST | cluster-1~7 (M01-M48) | 48 | 48 | 0 | 0 | 0 |
| MUST | cluster-8 缓存命中率 (N01-N09) | 9 | 9 | 0 | 0 | 0 |
| SHOULD | cluster-4~7 (S01-S16) | 16 | 9 | 7 | 0 | 0 |
| COULD | extensions / ui-skip (C01-C17) | 17 | 0 | 0 | 11 | 6 |
| MUST | cluster-9 并发加固 (H01-H08) | 8 | 0 | 8 | 0 | 0 |
| **合计** | | **98** | **66** | **15** | **11** | **6** |

> ⚠️ 注：tasks.json 的 `meta.taskCountByStatus` 仍停留在 completed=65 / pending=16（S03 已完成但 meta 未同步），本表按逐条 status 统计为 **completed=66 / pending=15**，以逐条为准。

### 0.2 测试策略说明

1. **分层覆盖**：每个功能点至少覆盖 3 类用例——**正向主路径**（验收标准）、**边界/异常路径**（错误码、越界、并发）、**集成路径**（跨模块依赖）。
2. **已完成项**：用例与 `tests/` 目录已有测试一一对应，可直接用于回归；未覆盖的边界在「补充用例」中列出。
3. **待做项**：用例为**设计稿**，标注 `[待实现]`，实现完成后必须按此用例补测。
4. **延后/跳过项**：标注 `[延期]`/`[跳过]`，仅记录验收标准，不要求当前实现。

### 0.3 用例字段说明

| 字段 | 含义 |
|---|---|
| 用例ID | `TC-<任务ID>-<序号>`，全局唯一 |
| 类型 | `正向` / `边界` / `异常` / `并发` / `集成` |
| 关联任务 | 该用例验证的功能点 |
| 前置条件 | 运行前必须满足的状态 |
| 测试步骤 | 可执行的操作序列 |
| 预期结果 | 与验收标准对齐的期望输出 |
| 状态 | `已实现`（有代码+测试）/ `待实现` / `延期` / `跳过` |
| 关联文件 | 已有或规划中的测试文件 |

---

## 1. 簇 1：核心驱动（cluster-1-core-driver）

### M01 Branded ID 类型封装（✅ completed）

> 目标：Branded/Bytes 两类 branded 结构体（SessionID、ToolCallID、ApprovalRequestId、JobId、SkillId、AttachmentId、CredentialRef、WorkspaceId、ProjectionId 等），避免字符串 ID 混传。
> 关联文件：`tests/brand_ids_test.go`（6 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M01-01 | 正向 | 9 种品牌 ID 构造函数 | 无 | 分别用 `NewXxxID` 创建 9 种品牌 ID | 各类型生成正确的 branded 值，`String()` 返回原始字符串 | 已实现 | brand_ids_test.go |
| TC-M01-02 | 正向 | ID 序列化 round-trip | 已创建各类 ID | 对 ID 执行 `MarshalJSON` → `UnmarshalJSON` | 反序列化后与原 ID 完全相等 | 已实现 | brand_ids_test.go |
| TC-M01-03 | 边界 | Zero/IsZero 判定 | 无 | 创建 `Zero` 值并调用 `IsZero` | 零值判定正确；非零 ID 判定为非零 | 已实现 | brand_ids_test.go |
| TC-M01-04 | 异常 | 错误类型传入（编译期） | Go 源码调用点 | 将 `SessionID` 传入期望 `JobID` 参数的函数 | **编译期报错**（类型不匹配），杜绝字符串 ID 混传 | 已实现 | brand_ids_test.go |
| TC-M01-05 | 边界 | 空字符串/畸形 ID Parse | 无 | 对空串、含非法字符的字符串执行 `Parse` | 返回错误或零值，不 panic | 补充用例 | — |

### M02 Waterfall 中间件链原语（✅ completed）

> 目标：泛型 `WaterfallFunc[T]` + Chain 组合 + typed next()；agent/pre-step/request/tools 四级链 + approval + plan/goal 注入全部复用一套实现。
> 关联文件：`tests/waterfall_chain_test.go`（8 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M02-01 | 正向 | 多级拦截严格顺序 | 组装 3 级 Chain | 依次执行并记录每级调用序 | 拦截顺序与注册顺序一致（洋葱模型） | 已实现 | waterfall_chain_test.go |
| TC-M02-02 | 异常 | next() 未调用短路 | 某中间件不调用 next | 执行 Chain | 短路并返回该中间件定义的 error，后续级不执行 | 已实现 | waterfall_chain_test.go |
| TC-M02-03 | 正向 | payload 跨级改写 | 中间件改写 payload | 执行完整 Chain | 下一级收到改写后的 payload，最终结果含全部改写 | 已实现 | waterfall_chain_test.go |
| TC-M02-04 | 异常 | 重复调用 next() | 某中间件调 2 次 next | 执行 Chain | panic 保护生效（RunSafe 兜底）或报错，不产生重复副作用 | 已实现 | waterfall_chain_test.go |
| TC-M02-05 | 集成 | 四级链复用 | 构造 agent/pre-step/request/tools 四级链 | 注入同一套 Chain 实现运行 | 四个层级均走同一实现，行为一致 | 补充用例 | — |

### M03 Scope 分层注册表原语（✅ completed）

> 目标：ScopeKey + Layer + ScopedLayers merge（nearest-scope-wins + rank），供 Tools/Skills/Commands/Credentials 所有注册表复用。
> 关联文件：`tests/scope_layers_merge_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M03-01 | 正向 | host + scope 叠加优先级 | 注册 host 层 + scope 层同名 entry | 读取该 entry | nearest-scope-wins：scope 层值覆盖 host 层 | 已实现 | scope_layers_merge_test.go |
| TC-M03-02 | 边界 | 匿名 entry 与 named entry 隔离 | 注册匿名与命名 entry | 分别读取 | 互不冲突，各取各值 | 已实现 | scope_layers_merge_test.go |
| TC-M03-03 | 边界 | rank 相同时后注册覆盖 | 同 rank 注册两条 | 读取 | 后注册的覆盖先注册的（注册序决胜） | 已实现 | scope_layers_merge_test.go |
| TC-M03-04 | 异常 | 多层 scope 深度合并 | 构造 3 层 scope | Merge 后读取 | 最内层 wins，Merge 结果确定且可复现 | 已实现 | scope_layers_merge_test.go |

### M04 Session 事件溯源 & 53 种词汇表（✅ completed）

> 目标：53 种事件类型（Map→Derived Union 模式）+ `SessionEvent{seq,time,type,data,surfaceOp?,sourceEventSeqs?}` + append 严格不变量（turn 开闭 / step 配对 / tool call↔result 匹配）。
> 关联文件：`tests/session_event_vocab_test.go` + `tests/session_event_ordering_test.go`（7 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M04-01 | 正向 | 53 种事件 round-trip | 构造每种事件 | 序列化→反序列化 | 逐字段一致 | 已实现 | session_event_vocab_test.go |
| TC-M04-02 | 异常 | turn/end 无前 turn/start | 直接 append turn/end | 触发 append | 开发 invariant 报错 | 已实现 | session_event_ordering_test.go |
| TC-M04-03 | 异常 | tool/call 缺失 tool/result | append tool/call 不配对 | 触发 append | invariant 报错（配对缺失） | 已实现 | session_event_ordering_test.go |
| TC-M04-04 | 边界 | seq/time 连续单调 | 追加 100 事件 | 校验 seq 与 time | seq 严格连续、time 单调递增 | 已实现 | session_event_ordering_test.go |
| TC-M04-05 | 正向 | surfaceOp/sourceEventSeqs 携带 | append 带 surfaceOp 的事件 | 序列化 round-trip | surfaceOp 与 sourceEventSeqs 字段完整保留 | 补充用例 | — |

### M05 Session 派生投影函数族（✅ completed）

> 目标：`deriveMessages()` + foldRequestHeader + foldEffectiveSandboxMode + foldEffectiveApprovalPolicy + foldGoalChange + foldTodoWrite + foldPlanMode + foldPermissionPreset + foldSessionTitle。
> 关联文件：`tests/session_fold_consistency_test.go`（3 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M05-01 | 正向 | 重放 vs 热 append 一致性 | 1k+ 事件 log | 全量重放各 fold vs 热 append 各 fold | 各 fold 结果逐字段一致 | 已实现 | session_fold_consistency_test.go |
| TC-M05-02 | 集成 | compaction replace 后 deriveMessages | 执行 compaction replace | 重新 deriveMessages | 输出缩短且新消息继续正确派生 | 已实现 | session_fold_consistency_test.go |
| TC-M05-03 | 正向 | FoldAll 聚合 | 构造混合事件流 | 调用 FoldAll | 各投影聚合结果与逐个 fold 一致 | 已实现 | session_fold_consistency_test.go |

### M06 SessionHeader 元数据（✅ completed）

> 目标：Version/ID/CreatedAt/Cwd/ParentSession/SeedLength/Origin/DelegationDepth/AgentPreset；Persistence 键目录、fork lineage、subagent 递归深度全靠它。
> 关联文件：`tests/session_header_versioning_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M06-01 | 正向 | Header 序列化 round-trip | 构造完整 Header | 序列化→反序列化 | 字段一致且 SESSION_FORMAT_VERSION 匹配 | 已实现 | session_header_versioning_test.go |
| TC-M06-02 | 异常 | 未知 Header 版本 fail-closed | Header 带未知版本号 | 读取 | 拒绝读取并报格式错误 | 已实现 | session_header_versioning_test.go |
| TC-M06-03 | 集成 | fork/cold-resume 谱系写入 | 执行 fork 或 cold-resume | 检查新 Header | ParentSession/SeedLength/DelegationDepth 正确写入 | 已实现 | session_header_versioning_test.go |
| TC-M06-04 | 边界 | subagent 递归深度 | 多层 subagent fork | 检查每层 Header | DelegationDepth 逐层 +1 | 补充用例 | — |

### M07 LLM Provider 接缝 + 流式协议（✅ completed）

> 目标：LLMAdapter 接口 + Message/ContentBlock(text/tool_use/tool_result/image) + StreamChunk(text/reasoning/tool-call) + ToolSchema + LlmFailure 分类 + DeepSeek REST+SSE 实现。
> 关联文件：`tests/llm_stream_roundtrip_test.go`（4 用例 PASS，mock SSE）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M07-01 | 正向 | SSE 流式 chunk 解析 | mock SSE 响应 | 读取完整流 | text/reasoning/tool-call chunk 顺序与内容正确 | 已实现 | llm_stream_roundtrip_test.go |
| TC-M07-02 | 正向 | 4 类 ContentBlock round-trip | 构造各类型 block | 序列化→反序列化 | 类型与内容一致 | 已实现 | llm_stream_roundtrip_test.go |
| TC-M07-03 | 异常 | LlmFailure 分类映射 | 模拟 overload/rate-limit/refusal/context-overflow | 触发各失败 | 映射到正确分类 | 已实现 | llm_stream_roundtrip_test.go |
| TC-M07-04 | 集成 | DeepSeek 真实客户端（需密钥） | 配置真实 API key | 调用 /chat/completions(stream=true) | 拿到 reasoning + tool_call 完整 chunk | 待实现(需密钥) | llm_provider_deepseek_integration_test.go |

### M08 Agent Registry + Turn/Step 双循环 Loop（✅ completed）

> 目标：Agent 接口 + Inbox 双队列 + Turn 循环 + Step 循环 + 单 Turn 串行保证。
> 关联文件：`tests/agent_loop_turn_step_dual_test.go`（5 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M08-01 | 并发 | 双 Followup 串行排队 | Agent running 中 | 提交 2 条 Followup | 串行排队执行，pending turn 正确 reject | 已实现 | agent_loop_turn_step_dual_test.go |
| TC-M08-02 | 异常 | step/tool 错误收尾 | 工具执行抛错 | 触发错误 | agent/error 写入 log，turn 以 interrupted 关闭 | 已实现 | agent_loop_turn_step_dual_test.go |
| TC-M08-03 | 正向 | Turn→Step 全流程事件序 | 正常对话 | 观察事件流 | turn/start→pre-step→step 循环→stopping→turn/end 顺序正确 | 已实现 | agent_loop_turn_step_dual_test.go |
| TC-M08-04 | 边界 | 单 Turn 串行保证 | 同 Agent 并发触发 | 并发提交请求 | 任意时刻只有一个 Turn 在执行 | 已实现 | agent_loop_turn_step_dual_test.go |

### M18 Agent Cancel 原因分类（✅ completed）

> 目标：{kind:user}/{parent}/{hook reason}/{disposed}/{legacy} 5 种 → TurnEndReason.aborted。
> 关联文件：`tests/agent_cancel_cause_classify_test.go`（3 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M18-01 | 正向 | 5 类取消原因区分 | 构造 user/parent/hook/disposed/legacy 取消 | 逐一触发取消 | turn/end 的 aborted.reason 分别对应正确分类 | 已实现 | agent_cancel_cause_classify_test.go |
| TC-M18-02 | 集成 | RecordCancel 写日志 | Agent 运行中取消 | 调用 RecordCancel | 取消事件写入 session log，turn 以 aborted 收尾 | 已实现 | agent_cancel_cause_classify_test.go |
| TC-M18-03 | 异常 | 无取消记录时分类 | 正常结束的 turn | ExtractCancelCause | 返回无取消/默认分类，不误报 | 已实现 | agent_cancel_cause_classify_test.go |

### M19 Request Header 快照 + request/context 路由（✅ completed）

> 目标：EpochHeader{config/system/tools} + RequestContext{provider/model/contextWindow} + reason in {initial/resume/change/series}。
> 关联文件：`tests/request_header_rebuild_test.go`（2 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M19-01 | 集成 | 快照重建一致性 | 写入 request/header 快照 | 用最新快照重建 Prompt+Schema | 与实际发送 payload 逐字段一致 | 已实现 | request_header_rebuild_test.go |
| TC-M19-02 | 边界 | reason 路由 | 设置 initial/resume/change/series | 读取 RequestContext | reason 正确路由与记录 | 已实现 | request_header_rebuild_test.go |
| TC-M19-03 | 集成 | compaction 后重建 | 执行 compaction | RebuildFromHeader | 不依赖原始 events 即可重建 | 补充用例 | — |

### M20 session/end-seed 种子边界 marker（✅ completed）

> 目标：Resume/Fork 后第一条 live 写入的 marker 事件，定位 cold stored vs live work 分界。
> 关联文件：`tests/end_seed_bracket_test.go`（3 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M20-01 | 正向 | Fork/Resume 后必写 end-seed | 执行 Fork/Resume | 检查事件流 | end-seed marker 写入且携带父会话 | 已实现 | end_seed_bracket_test.go |
| TC-M20-02 | 边界 | SeedEndSeq 分界判定 | 有 end-seed 的 log | 判断事件位于 seed 前后 | IsAfterEndSeed 判定正确 | 已实现 | end_seed_bracket_test.go |
| TC-M20-03 | 集成 | compaction 半开括号定位 | 执行 compaction | 利用 end-seed 定位 | 半开括号定位正确（不跨 seed 截断 live 内容） | 已实现 | end_seed_bracket_test.go |

### M21 SurfaceOp(append/replace) + foldSurface（✅ completed）

> 目标：{op:'replace',start,end} 世代；foldSurface(events)=>nodes+replacements；compaction 替换节点就是 surface replace。
> 关联文件：`tests/compaction_replace_surface_test.go`（2 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M21-01 | 正向 | replace range 读时替换 | append 100 事件后 replace [3,80] | deriveMessages | 前后消息列表字节一致，源事件未改动 | 已实现 | compaction_replace_surface_test.go |
| TC-M21-02 | 边界 | append-only 纯净性 | 执行 replace 后 | 检查源事件数组 | 源事件 append-only，无 in-place 修改 | 已实现 | compaction_replace_surface_test.go |
| TC-M21-03 | 边界 | 非法 replace 区间 | start>end 或越界 | foldSurface | 报错或安全拒绝，不产生错误替换 | 补充用例 | — |

### M23 Tool Execution 四级 Waterfall 链（✅ completed）

> 目标：pre-execute → execute(可换 signal) → post-execute(accept/block/attach ctx) → result。
> 关联文件：`tests/tools_waterfall_test.go`（8 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M23-01 | 正向 | 四级 middleware 各通道工作 | 注册 deny/换参/截断/加 meta 中间件 | 执行工具调用 | 各通道均生效 | 已实现 | tools_waterfall_test.go |
| TC-M23-02 | 异常 | execute 换 signal cancel | 执行中取消 | post-execute → result | result 带 isError，取消语义正确 | 已实现 | tools_waterfall_test.go |
| TC-M23-03 | 正向 | WithTool 注入真实实现 | 构造工具 | 执行调用 | 真实实现被执行并返回结果 | 已实现 | tools_waterfall_test.go |
| TC-M23-04 | 边界 | 四级顺序 | 注册多中间件 | 记录调用序 | pre→execute→post→result 严格顺序 | 已实现 | tools_waterfall_test.go |

### M32 Agent Preset 接缝（✅ completed）

> 目标：ctx.agentPresets.{mount/composeFrom/standingKeyFor/recompose/select}。
> 关联文件：`tests/agent_preset_compose_test.go`（3 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M32-01 | 正向 | 不同 preset 独立组合 | 挂载 2 个 preset | 分别创建 Agent 实例 | 各自 tools/prompt 组合独立正确 | 已实现 | agent_preset_compose_test.go |
| TC-M32-02 | 正向 | ComposeFrom 工具并集+prompt 拼接 | 组合 2 个 preset | 调用 ComposeFrom | 工具并集、prompt 拼接正确 | 已实现 | agent_preset_compose_test.go |
| TC-M32-03 | 边界 | standing key | 多次 Select 同 preset | 检查 standing key | standing key 稳定一致 | 已实现 | agent_preset_compose_test.go |

### M33 Agent Initiator 上下文（✅ completed）

> 目标：withInitiator/requireInitiator/withoutInitiator；安全归因。
> 关联文件：`tests/initiator_causal_trace_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M33-01 | 异常 | requireInitiator 无上下文 | 无 withInitiator 包裹 | 调用 requireInitiator | panic 或返回 Unauthorized 结构化错误 | 已实现 | initiator_causal_trace_test.go |
| TC-M33-02 | 正向 | withInitiator→require 归因 | 正确包裹 | 调用 requireInitiator | 返回 AgentID/Op 正确 | 已实现 | initiator_causal_trace_test.go |
| TC-M33-03 | 边界 | withoutInitiator 清除 | 有 initiator 后清除 | 调用 withoutInitiator 再 require | 归因被清除 | 已实现 | initiator_causal_trace_test.go |
| TC-M33-04 | 集成 | 因果链追踪 | 多层嵌套 initiator | 追踪归因链 | 各层因果链正确 | 已实现 | initiator_causal_trace_test.go |

### M34 agent/request-error 重试瀑布（✅ completed）

> 目标：RequestErrorAction{kind:'retry'}，LLM 过载/速率限制显式重试通道。
> 关联文件：`tests/agent_request_error_retry_test.go`（5 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M34-01 | 正向 | 可重试错误 → retry | 触发 overload/rate-limit | 走 request-error waterfall | 返回 retry action | 已实现 | agent_request_error_retry_test.go |
| TC-M34-02 | 异常 | 超过 max 次重试 | 持续失败 | 重试至上限 | 以 error 关闭 turn | 已实现 | agent_request_error_retry_test.go |
| TC-M34-03 | 集成 | 与 S15 Retry 协同 | 配置 S15 Retry | 触发过载 | 两者协同不冲突，事件正确 | 已实现 | agent_request_error_retry_test.go |
| TC-M34-04 | 正向 | RecordRequestError 写日志 | 请求失败 | 记录错误 | agent/error 事件正确写入 | 已实现 | agent_request_error_retry_test.go |

### M47 Tool Presentation 中立 vocabulary（✅ completed）

> 目标：ToolCallView/ToolResultView 9 种 card（generic/terminal/diff/search/read/web 等）。
> 关联文件：`tests/tools_presentation_cards_test.go`（5 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M47-01 | 正向 | 9 种卡片字段对齐 | 构造 9 种 card | 检查各字段 | 与上游原版一一对应 | 已实现 | tools_presentation_cards_test.go |
| TC-M47-02 | 正向 | CardOf 判别 | 提供自定义 card | 调用 CardOf | 自定义回落 other，判别正确 | 已实现 | tools_presentation_cards_test.go |
| TC-M47-03 | 边界 | 空/最小卡片 | 构造缺字段 card | 序列化 | 不 panic，字段缺失可容忍 | 已实现 | tools_presentation_cards_test.go |

### M48 DefineTool JSON Schema 强校验子集（✅ completed）

> 目标：JsonSchemaNode + object root assert + validate + INFER；subagent/workflow 结构化输出同样复用。
> 关联文件：`tests/tools_jsonschema_subset_test.go`（8 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M48-01 | 正向 | oneOf/items/enum/const/default/examples 语义 | 构造复杂 schema | Validate 各约束 | 与原版语义 1:1 一致 | 已实现 | tools_jsonschema_subset_test.go |
| TC-M48-02 | 异常 | 不支持关键字 fail-closed | schema 含未知关键字 | Compile | 报错而非静默忽略 | 已实现 | tools_jsonschema_subset_test.go |
| TC-M48-03 | 正向 | Infer 反推 | 给定 Go 值 | Infer schema | 反推出的 schema 约束正确 | 已实现 | tools_jsonschema_subset_test.go |
| TC-M48-04 | 正向 | additionalProperties=false | 关闭额外属性 | Validate | 额外属性报错 | 已实现 | tools_jsonschema_subset_test.go |
| TC-M48-05 | 边界 | 确定性 Marshal | 同 schema 多次 Marshal | 比较输出 | 字节一致（字典序） | 已实现 | tools_jsonschema_subset_test.go |

## 2. 簇 2：规划能力（cluster-2-planning）

### M09 SystemPrompt 组装 + Section 注册表（✅ completed）

> 目标：PromptSection{name/order/text} + 排序合并 + tools schema 注入；顺序错会导致模型行为漂移。
> 关联文件：`tests/sysprompt_section_ordering_test.go`（5 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M09-01 | 正向 | 组装顺序与原版一致 | 注册全部 section | Assemble | persona→policy→runtime-context-snapshot→plan:policy→tools schema 顺序逐行 diff 一致 | 已实现 | sysprompt_section_ordering_test.go |
| TC-M09-02 | 边界 | order 常量正确 | 无 | 校验各 section order | 100/200/300/500/600/700 符合设计 | 已实现 | sysprompt_section_ordering_test.go |
| TC-M09-03 | 正向 | tools schema 字典序注入 | 多工具 | 生成 ToolsSectionText | 工具按字典序注入 | 已实现 | sysprompt_section_ordering_test.go |
| TC-M09-04 | 边界 | 同名 section 覆盖 | 注册同名 section | Assemble | 后注册覆盖先注册，不出现重复 | 补充用例 | — |

### M10 PromptContext 动态注册与快照（✅ completed）

> 目标：PromptContext{name/order/text(AssembleCtx)} + runtime-context-snapshot user/message。
> 关联文件：`tests/prompt_context_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M10-01 | 正向 | 动态添加/移除生效 | 注册 context | 添加/移除后 assemble | 下一轮 assemble 生效/失效 | 已实现 | prompt_context_test.go |
| TC-M10-02 | 集成 | compaction 保留快照 | 执行 compaction | 检查 runtime-context-snapshot | 最新快照保留不丢 | 已实现 | prompt_context_test.go |
| TC-M10-03 | 集成 | GoalRoundContext 续轮注入 | goal.active | 观察注入 | 续轮提示正确，complete 后停止 | 已实现 | prompt_context_test.go |
| TC-M10-04 | 边界 | Compute 稳定/变化 | 状态不变 vs 变化 | 多次 Compute | 无变化 hash 稳定，变化 hash 改变 | 已实现 | prompt_context_test.go |

### M11 Plan Mode 软引导 + 审批退出（✅ completed）

> 目标：plan:policy Prompt Section + plan/mode log + exit_plan_mode 工具 + UserQuestion 审批。
> 关联文件：`tests/plan_mode_approval_test.go`（3 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M11-01 | 正向 | 进入 plan mode 注入 policy | 执行 Enter | 下一轮请求 | plan:policy section 成功注入 | 已实现 | plan_mode_approval_test.go |
| TC-M11-02 | 正向 | exit 审批通过移除 | 调 exit_plan_mode 且审批通过 | 下一轮请求 | plan:policy section 不再出现 | 已实现 | plan_mode_approval_test.go |
| TC-M11-03 | 异常 | exit 审批拒绝保留 | 审批拒绝 | 下一轮请求 | plan:policy section 仍存在 | 已实现 | plan_mode_approval_test.go |
| TC-M11-04 | 正向 | plan/mode 事件记录 | Enter/Exit | 检查事件流 | plan/mode 事件正确写入 | 补充用例 | — |

### M12 Goal 系统（状态机+续轮驱动+6工具）（✅ completed）

> 目标：GoalPhase/CAS Revision/goal/change 事件 + goal-round-driver turn-stopping 监听 + 6 个 goal_* 工具 + concludeTurn。
> 关联文件：`tests/goal_rounddriver_cas_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M12-01 | 集成 | 自动续轮 5 轮 | set_goal(active, maxRounds=5) | 无用户输入运行 | 自动续轮跑完 5 轮 | 已实现 | goal_rounddriver_cas_test.go |
| TC-M12-02 | 正向 | report_blocker → concludeTurn | 调用 goal_report_blocker | 观察 turn | turn/end 正确写出，本轮结束 | 已实现 | goal_rounddriver_cas_test.go |
| TC-M12-03 | 并发 | CAS Revision 冲突 | 并发修改 goal | 冲突写 | CAS 冲突报错并发保护 | 已实现 | goal_rounddriver_cas_test.go |
| TC-M12-04 | 正向 | 6 个 goal_* 工具 | 逐一调用 | 检查效果 | list/set_phase/set_description/set_max_rounds/add_blocker/report_blocker 全部正确 | 已实现 | goal_rounddriver_cas_test.go |

### M13 Todo 整体替换写入（✅ completed）

> 目标：todo/write 事件 + foldTodoWrite + todo_write 工具。
> 关联文件：`tests/todo_write_replace_test.go`（1 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M13-01 | 正向 | last-write-wins 整体替换 | 多次 todo/write | fold | 每次整体替换，与原版 last-write-wins 一致 | 已实现 | todo_write_replace_test.go |
| TC-M13-02 | 正向 | todo_write 工具写事件 | 调用工具 | 检查事件流 | todo/write 事件正确写入 | 补充用例 | — |
| TC-M13-03 | 边界 | 空 todo 列表替换 | 传入空列表 | fold | 清空生效 | 补充用例 | — |

### M14 User Questions 接缝（✅ completed）

> 目标：UQ 服务接口 + provider_stub(同步阻塞) + SDK 注入真实 impl；支持 multiSelect + intent 标签。
> 关联文件：`tests/userq_provider_swap_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M14-01 | 正向 | stub 返回默认选项 | 使用 stub | Ask | 返回默认选项 idx | 已实现 | userq_provider_swap_test.go |
| TC-M14-02 | 正向 | 替换 provider 路由 | SetProvider 自定义 | Ask | 路由到自定义实现 | 已实现 | userq_provider_swap_test.go |
| TC-M14-03 | 边界 | multiSelect 多选返回 | 启用 multiSelect | AskDetailed | 返回多选结果 | 已实现 | userq_provider_swap_test.go |
| TC-M14-04 | 边界 | custom 输入返回 | 用户自定义输入 | Ask | 返回 custom string | 已实现 | userq_provider_swap_test.go |

### M15 / M41 Commands（slash 命令）（✅ completed）

> 目标：CommandDefinition + command/run & command/done 事件 + ctx.commands register/list/execute；/plan /goal 命令入口。M41 为 M15 的 cluster-7 索引（合并实现）。
> 关联文件：`tests/commands_slash_dispatch_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M15-01 | 正向 | /plan off 直写事件 | 执行 /plan off | 检查事件流 | 直接写 plan/mode(off) 而非 user/message | 已实现 | commands_slash_dispatch_test.go |
| TC-M15-02 | 正向 | command/run & done 完整 | 执行任意命令 | 观察事件流 | command/run + command/done 完整配对 | 已实现 | commands_slash_dispatch_test.go |
| TC-M15-03 | 正向 | register/list/execute | 注册新命令 | 执行 | 可注册、列出、执行 | 已实现 | commands_slash_dispatch_test.go |
| TC-M15-04 | 异常 | 未注册命令 | 执行未知命令 | dispatch | 报错不 panic | 已实现 | commands_slash_dispatch_test.go |

### M16 Session Projections 投影注册中心（✅ completed）

> 目标：ProjectionDefinition[State any] + register / snapshot / subscribe(changelog)；SDK 读取派生状态的唯一标准接口。
> 关联文件：`tests/session_projection_test.go`（2 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M16-01 | 集成 | 4 投影 subscribe 增量 | 注册 Goal/Todo/Plan/Sandbox 投影 | ApplyEvents + subscribe | changelog 增量正确 | 已实现 | session_projection_test.go |
| TC-M16-02 | 集成 | snapshot 与 fold 一致 | 执行多事件 | snapshot vs fold* | 结果一致 | 已实现 | session_projection_test.go |
| TC-M16-03 | 边界 | rebuild 全量重建 | 删除状态后 | rebuild | 从事件流完整重建状态 | 补充用例 | — |

### M17 Session References（跨会话 & 文件 mention）（✅ completed）

> 目标：@session/xxx 与 #path/file 语法 mention 解析 + PreparedReferencedMessage + 稳定错误码。
> 关联文件：`tests/session_reference_test.go`（7 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M17-01 | 正向 | 多 mention 拼入上下文 | 消息 mention 3 个 session+文件 | Prepare | PreparedReferencedMessage 拼入上下文正确 | 已实现 | session_reference_test.go |
| TC-M17-02 | 异常 | 引用不存在 session | mention 无效 session | Prepare | SESSION_NOT_FOUND 分类错误码 | 已实现 | session_reference_test.go |
| TC-M17-03 | 异常 | 文件越界/不存在 | mention 越界/缺失文件 | Prepare | FILE_OUT_OF_WORKSPACE / FILE_NOT_FOUND 错误码 | 已实现 | session_reference_test.go |
| TC-M17-04 | 边界 | 自引用拒绝 | 引用自身 | Prepare | 自引用错误码 | 已实现 | session_reference_test.go |
| TC-M17-05 | 边界 | 引用超限/预算拒绝 | 超数量上限或预算不足 | Prepare | 拒绝引用错误码 | 已实现 | session_reference_test.go |
| TC-M17-06 | 集成 | 写入 source=reference | 解析成功后 | 检查 user/message | source=reference 正确写入 | 已实现 | session_reference_test.go |
| TC-M17-07 | 边界 | path-only 越界判定 | workspace 外路径 | WorkspaceFileResolver | 判定越界正确 | 已实现 | session_reference_test.go |

## 3. 簇 3：安全（cluster-3-safety）

### M22 PreToolDecision 三态（allow/deny/ask）（✅ completed）

> 目标：tools/pre-execute waterfall 返回三态 + approval ask→allowed-once 语义。
> 关联文件：`tests/tools_predecision_ask_once_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M22-01 | 正向 | deny 短路 | pre-execute 返回 deny | 执行工具 | 工具不执行，返回 deny 结果 | 已实现 | tools_predecision_ask_once_test.go |
| TC-M22-02 | 正向 | ask 通过 allowed-once | ask 被用户通过 | 执行本次调用 | 仅本次 tool/call 执行 | 已实现 | tools_predecision_ask_once_test.go |
| TC-M22-03 | 边界 | 下次同工具继续 ask | 同工具再次调用 | 再次执行 | 不被永久放行，继续 ask | 已实现 | tools_predecision_ask_once_test.go |
| TC-M22-04 | 边界 | ask 拒绝 deny | ask 被拒 | 执行 | 等同 deny，工具不执行 | 已实现 | tools_predecision_ask_once_test.go |

### M24 Tool Restriction allow/deny（✅ completed）

> 目标：Scope 级 tool mask + intersect + scope tool exempt；Subagent 父限子能力 / Preset 隐藏工具。
> 关联文件：`tests/tools_restriction_intersect_test.go`（5 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M24-01 | 正向 | host deny + scope exempt 恢复 | host deny 某工具 | scope exempt 后调用 | 工具恢复可用 | 已实现 | tools_restriction_intersect_test.go |
| TC-M24-02 | 集成 | 两层相交 | host + scope 均设限制 | Filter | 两层交集正确 | 已实现 | tools_restriction_intersect_test.go |
| TC-M24-03 | 集成 | 子代理父限子 | 父限子能力 | Filter 子代理 | 子代理能力被限制 | 已实现 | tools_restriction_intersect_test.go |
| TC-M24-04 | 集成 | 预设隐藏工具 | preset 隐藏工具 | Filter | 工具不可见 | 已实现 | tools_restriction_intersect_test.go |
| TC-M24-05 | 边界 | nearest-scope-wins 逐层 | 多层 scope | 解析限制 | 最内层 scope 优先 | 已实现 | tools_restriction_intersect_test.go |

### M25 ToolRunContext deferContext + concludeTurn（✅ completed）

> 目标：composite tool 嵌套分发通道 + 终止本 turn 的权威标记。
> 关联文件：`tests/tools_conclude_turn_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M25-01 | 集成 | concludeTurn 终止本 turn | report_blocker 调 concludeTurn | 观察 turn | turn 立即走 turn-stopping/turn/end，不再下一个 Step | 已实现 | tools_conclude_turn_test.go |
| TC-M25-02 | 正向 | deferContext 传播 | 嵌套 composite tool | 分发子工具 | 嵌套分发通道正确 | 已实现 | tools_conclude_turn_test.go |
| TC-M25-03 | 边界 | 无 conclude 不误触发 | 正常工具调用 | 观察 | 不终止 turn | 已实现 | tools_conclude_turn_test.go |
| TC-M25-04 | 集成 | 多层 context 传播 | WithConclude 嵌套 | ConcludeFrom | 传播链正确 | 已实现 | tools_conclude_turn_test.go |

### M26 Sandbox 接缝 3 模式（✅ completed）

> 目标：mode in {read-only / workspace-write / danger-full-access} + {root path, enforced} 元组；Bash/FS 消费者统一。
> 关联文件：`tests/sandbox_mode_apply_test.go`（6 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M26-01 | 集成 | Bash & FS 消费一致策略 | 设置 Sandbox Mode | 两消费者取策略 | 均得到一致 ExecutionPolicy（enforced + scope） | 已实现 | sandbox_mode_apply_test.go |
| TC-M26-02 | 正向 | 3 模式切换 | 逐一设置 3 模式 | 取策略 | 各模式映射正确 | 已实现 | sandbox_mode_apply_test.go |
| TC-M26-03 | 边界 | 优先级：显式>会话>默认 | 配置不同层级 | 解析 | 显式优先，根回落 | 已实现 | sandbox_mode_apply_test.go |
| TC-M26-04 | 异常 | Confine fail-closed | Provider 不可用 | Confine | SANDBOX_UNAVAILABLE 错误 | 已实现 | sandbox_mode_apply_test.go |
| TC-M26-05 | 正向 | RunnerFailureRule 证据规则 | 命令失败 | 判定 | 证据规则正确 | 已实现 | sandbox_mode_apply_test.go |

### M27 Approval Policy 接缝（✅ completed）

> 目标：policy 4 枚举 + 用户级/会话级 override + ask→allowed-once。
> 关联文件：`tests/approval_override_order_test.go`（7 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M27-01 | 集成 | 三层 override 顺序 | preset+user+session 各设 policy | Effective | 会话>用户>预设，顺序正确 | 已实现 | approval_override_order_test.go |
| TC-M27-02 | 正向 | ask 调用 UQ 成功 | ask 策略 | 触发工具 | UQ ask 成功链路完整 | 已实现 | approval_override_order_test.go |
| TC-M27-03 | 异常 | UQ 失败 fail-closed | UQ 抛错 | ask | 降级为 deny | 已实现 | approval_override_order_test.go |
| TC-M27-04 | 边界 | allowed-once | ask 通过后同工具再调 | 再次触发 | 仅放行当次，下次继续 ask | 已实现 | approval_override_order_test.go |
| TC-M27-05 | 正向 | 4 枚举策略 | 逐一设置 | Effective | allow-all/deny-all/ask-dangerous/ask-dangerous-tool-edit 正确 | 已实现 | approval_override_order_test.go |
| TC-M27-06 | 边界 | Source 审计 | 解析生效策略 | 检查 Source | 审计字段标记来源层 | 已实现 | approval_override_order_test.go |

### M28 Permission Presets 组合旋钮（✅ completed）

> 目标：{sandboxMode, approvalPolicy} 预设表 + 用户自定义派生状态。
> 关联文件：`tests/permission_presets_combo_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M28-01 | 正向 | 四预设组合对应 | 选择 safe/danger/review/custom | Resolve | 组合元组与原版一一对应 | 已实现 | permission_presets_combo_test.go |
| TC-M28-02 | 边界 | 未知预设回落 custom | 传未知 preset | Resolve | 回落 custom | 已实现 | permission_presets_combo_test.go |
| TC-M28-03 | 正向 | DerivedState 派生标记 | custom 组合 | 读取派生状态 | custom 派生标记正确 | 已实现 | permission_presets_combo_test.go |
| TC-M28-04 | 集成 | Mapper 接线 | 注入 ApprovalMapper/SandboxMapper | 生效 | 单向接线正确 | 已实现 | permission_presets_combo_test.go |

### M29 Attachment 图片引用模式（✅ completed）

> 目标：ImageBlock{url? / reference:AttachmentId} + attachment storage + durable reference 解析。
> 关联文件：`tests/attachment_ref_resolve_test.go`（5 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M29-01 | 正向 | 保存图片 content-addressed | 保存图片 | 生成 AttachmentID | sha256 摘要正确 | 已实现 | attachment_ref_resolve_test.go |
| TC-M29-02 | 集成 | durable 跨会话/压缩解析 | 会话压缩后 | ResolveReference | durable 路径解析不失效 | 已实现 | attachment_ref_resolve_test.go |
| TC-M29-03 | 异常 | 读取重算摘要不匹配 | 文件被篡改 | 读取 | 报错或校验失败 | 已实现 | attachment_ref_resolve_test.go |
| TC-M29-04 | 正向 | ImageBlock reference 引用 | 构造引用 block | 序列化 | reference 字段正确 | 已实现 | attachment_ref_resolve_test.go |

### M30 Invariant Registry 不变量校验（✅ completed）

> 目标：ctx.invariants.register(pkgName, installer) + 包归属报错；开发期启用，生产关。
> 关联文件：`tests/invariant_pkg_attribution_test.go`（6 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M30-01 | 异常 | 违规报错带包名 | turn 已开再次 turn/start | 触发 invariant | INVARIANT 报错带包名 prefix | 已实现 | invariant_pkg_attribution_test.go |
| TC-M30-02 | 边界 | 生产关闭不报错 | SetEnabled(false) | 触发违规 | 不报错 | 已实现 | invariant_pkg_attribution_test.go |
| TC-M30-03 | 异常 | panic 隔离 | 单 invariant panic | 运行 | 隔离不扩散 | 已实现 | invariant_pkg_attribution_test.go |
| TC-M30-04 | 正向 | 违规账本登记 | 多次违规 | 检查账本 | 每条违规被登记 | 已实现 | invariant_pkg_attribution_test.go |

### M31 Token Meter 计量与预算（✅ completed）

> 目标：per-request prompt/completion token + session-level budget cap + 表面节点定价。
> 关联文件：`tests/token_meter_budget_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M31-01 | 正向 | 预算达标后拒绝 | 预算 10k tokens | 达到上限后下一轮请求前 | budget deny 拒绝且不产生真实 LLM 调用 | 已实现 | token_meter_budget_test.go |
| TC-M31-02 | 正向 | per-request 记录 | 每请求 | Record | prompt/completion 正确累计 | 已实现 | token_meter_budget_test.go |
| TC-M31-03 | 边界 | 未达预算正常放行 | 预算充足 | 请求 | 正常通过 | 已实现 | token_meter_budget_test.go |
| TC-M31-04 | 正向 | SurfacePricing 定价 | 表面节点 | 定价 | 定价正确 | 已实现 | token_meter_budget_test.go |

### S09 Message Feedback（✅ completed）

> 目标：rating/note/version(CAS)/createdAt/updatedAt + fail taxonomy + list/put/delete。
> 关联文件：`tests/feedback_cas_failure_taxonomy_test.go`

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S09-01 | 异常 | CAS 冲突分类 | 并发更新 feedback | Put | VERSION_CONFLICT 分类错误 | 已实现 | feedback_cas_failure_taxonomy_test.go |
| TC-S09-02 | 异常 | 不存在 session | 无效 session | Put | SESSION_NOT_FOUND | 已实现 | feedback_cas_failure_taxonomy_test.go |
| TC-S09-03 | 正向 | list/put/delete | 完整 CRUD | 执行 | 数据正确，version 递增 | 已实现 | feedback_cas_failure_taxonomy_test.go |
| TC-S09-04 | 异常 | 不存在记录 | 无效 id | Get/Delete | NOT_FOUND 分类 | 补充用例 | — |

## 4. 簇 4：持久化（cluster-4-persistence）

### M43 Persistence 接缝 + Flush Checkpoint + Batch Window + Crash Repair（✅ completed）

> 目标：SessionPersistence{locate/load/inspect/append/list/snapshot} + flush checkpoint + batch 窗口 + repair 孤儿 turn。
> 关联文件：`tests/persistence_crash_repair_test.go`（3 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M43-01 | 异常 | 崩溃后 repair 孤儿 turn | append 中 kill | reload → repair | 补写 interrupted turn/end | 已实现 | persistence_crash_repair_test.go |
| TC-M43-02 | 正向 | 已写事件全部保留 | 崩溃前写 chunk/tool/call | reload | 全部保留 | 已实现 | persistence_crash_repair_test.go |
| TC-M43-03 | 边界 | batch window 缓冲 | 批量 append | flush | 缓冲正确合并落盘 | 已实现 | persistence_crash_repair_test.go |
| TC-M43-04 | 集成 | Flush Checkpoint | 强制 flush | 检查 checkpoint | checkpoint 记录正确 | 补充用例 | — |

### M44 SessionHeader 格式拒绝 & 版本号（✅ completed）

> 目标：SESSION_FORMAT_VERSION + SessionFormatUnsupportedError + CorruptionError + KNOWN_SESSION_EVENT_TYPES 拒绝未知事件。
> 关联文件：`tests/session_format_reject_test.go`（3 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M44-01 | 异常 | 未知事件类型 fail-closed | 读入未知类型 | LoadSession | 拒绝 | 已实现 | session_format_reject_test.go |
| TC-M44-02 | 异常 | 跨版本拒绝 | 不同 VERSION 文件 | 加载 | 互相拒绝 | 已实现 | session_format_reject_test.go |
| TC-M44-03 | 异常 | 损坏事件 | 畸形 JSON | 加载 | CorruptionError | 已实现 | session_format_reject_test.go |

### M45 Storage Domain KV 抽象（✅ completed）

> 目标：hub → backend(JSONL/SQLite) → domain + CAS version mismatch + typed read/write。
> 关联文件：`tests/storage_domain_cas_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M45-01 | 正向 | filekv / sqlitekv 双后端 | 切换后端 | 读写 | 行为一致 | 已实现 | storage_domain_cas_test.go |
| TC-M45-02 | 异常 | CAS version mismatch | 过期版本写 | 写入 | VERSION_MISMATCH 错误一致 | 已实现 | storage_domain_cas_test.go |
| TC-M45-03 | 集成 | 跨后端迁移 | 数据迁移 | 执行迁移 | 幂等可跑，数据不丢 | 已实现 | storage_domain_cas_test.go |
| TC-M45-04 | 正向 | 类型化 typed read/write | Domain[T] | 读写 | 类型化正确 | 已实现 | storage_domain_cas_test.go |

### S01 Compaction（LLM 摘要 + Surface Replace）（⚪ pending）

> 目标：>N tokens 长对话触发 → 让 LLM 压缩老 surface → 生成 assistant/message{replace range}。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S01-01 | 正向 | 超阈值触发压缩 | 长对话 >N tokens | 触发 compact | LLM 生成摘要并写入 replace | 待实现 | compaction_replace_surface_test.go(复用 M21) |
| TC-S01-02 | 集成 | compact 后继续对话 | 已 compact | 发新消息 turn | turn 正常继续 | 待实现 | compaction_engine_basic_test.go |
| TC-S01-03 | 集成 | header rebuild 不依赖原始 events | compact 后 | RebuildFromHeader | 重建成功 | 待实现 | — |
| TC-S01-04 | 边界 | 低于阈值不触发 | 短对话 | 观察 | 不触发压缩 | 待实现 | — |

### S03 SQLite 持久化后端 + FTS5 索引（✅ completed）

> 目标：sqlite-session 单 DB + session 事件行化 + FTS5 + 原子写入。
> 关联文件：`tests/persistence_sqlite_fts5_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S03-01 | 性能 | 10k 事件写入 | 空库 | 写入 10k 事件 | 单查询 <100ms | 已实现 | persistence_sqlite_fts5_test.go |
| TC-S03-02 | 正向 | FTS5 标题/内容命中 | 建索引 | 搜索关键词 | 命中正确 | 已实现 | persistence_sqlite_fts5_test.go |
| TC-S03-03 | 边界 | 事务原子写正文+FTS | 批量写入 | AppendBatch | 正文与 FTS 一致 | 已实现 | persistence_sqlite_fts5_test.go |
| TC-S03-04 | 边界 | 降级 LIKE 搜索 | <3 字或 FTS 不可用 | Search | 降级 LIKE 可用 | 已实现 | persistence_sqlite_fts5_test.go |

### S04 Session Query + 搜索（SQLite FTS5）（⚪ pending）

> 目标：SessionListRequest/SessionSearchRequest + by title/created + ctrl+f 定位。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S04-01 | 正向 | 标题前缀过滤 | 多会话 | 按标题前缀查询 | 结果准确 | 待实现 | session_query_title_content_test.go |
| TC-S04-02 | 正向 | 创建时间范围 | 多会话 | 按时间范围查询 | 结果准确 | 待实现 | session_query_title_content_test.go |
| TC-S04-03 | 正向 | 包含关键词 | 多会话 | 关键词搜索 | 结果准确 | 待实现 | session_query_title_content_test.go |
| TC-S04-04 | 边界 | 三元组合过滤 | 多会话 | 组合条件 | 过滤结果精确 | 待实现 | session_query_title_content_test.go |

### S05 Session Telemetry hooks（✅ completed）

> 目标：sessionTelemetry{event/chunk/tool latency}。
> 关联文件：`tests/session_telemetry_hooks_fired_test.go`（5 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S05-01 | 正向 | 三类钩子触发 | 注册 3 钩子 | append/chunk/tool | 三类钩子都触发 | 已实现 | session_telemetry_hooks_fired_test.go |
| TC-S05-02 | 正向 | latency 合理区间 | 注入可控时钟 | 记录 tool | latency 在区间内 | 已实现 | session_telemetry_hooks_fired_test.go |
| TC-S05-03 | 异常 | 单钩子 panic 隔离 | 某钩子 panic | 触发 | callSafe 隔离不扩散 | 已实现 | session_telemetry_hooks_fired_test.go |
| TC-S05-04 | 并发 | 并发安全 | 并发触发 | 记录 | RWMutex 安全 | 已实现 | session_telemetry_hooks_fired_test.go |

### S07 OTel Telemetry 导出（⚪ pending）

> 目标：OTLP exporter + resource detector + baggage；与 S05 hook 对接。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S07-01 | 集成 | 本地 Collector 收 span | 起本地 OTel Collector | 产生 span | span 含 session/turn/step baggage | 待实现 | otel_collector_integration_test.go(可选) |
| TC-S07-02 | 正向 | 三信号导出 | 配置 OTLP | 触发 trace/metric/log | 三类信号均可导出 | 待实现 | — |
| TC-S07-03 | 边界 | 无 Collector 不阻塞 | Collector 不可达 | 触发 | 降级不阻塞主流程 | 待实现 | — |

### S08 Session Title（✅ completed）

> 目标：latest-wins fold + fallback human msg prefix 30 字 + LLM 辅助生成。
> 关联文件：`tests/session_title_fallback_llm_test.go`（8 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S08-01 | 正向 | fallback 30 字截断 | 未启用 LLM | 生成标题 | rune 前缀 30 字截断，多字节安全 | 已实现 | session_title_fallback_llm_test.go |
| TC-S08-02 | 正向 | LLM 生成标题 | 启用 LLM | Generate | 标题写入 session/title | 已实现 | session_title_fallback_llm_test.go |
| TC-S08-03 | 异常 | LLM 失败回退 | LLM 抛错 | Generate | 回退 fallback | 已实现 | session_title_fallback_llm_test.go |
| TC-S08-04 | 异常 | LLM 空输出回退 | LLM 返回空 | Generate | 回退 fallback | 已实现 | session_title_fallback_llm_test.go |
| TC-S08-05 | 集成 | latest-wins fold | 多次写入 | FoldSessionTitle | 最新标题生效 | 已实现 | session_title_fallback_llm_test.go |

## 5. 簇 5：子代理（cluster-5-subagent）

### S02 Subagent 接缝（3 后端）（⚪ pending）

> 目标：in-process fork / ACP child / fork-copy-process 三个 Provider。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S02-01 | 正向 | 3 后端各自单测 | 构造 3 Provider | 逐一运行 | 各自行为正确 | 待实现 | subagent_fork_inprocess_test.go |
| TC-S02-02 | 集成 | 父 dispose 子自动 drain | 子运行中 dispose 父 | 观察 | 子自动 drain | 待实现 | — |
| TC-S02-03 | 集成 | fork lineage 正确 | fork 子代理 | 检查 header | parent/session lineage 正确 | 待实现 | — |

### S10 Terminal PTY（⚪ pending）

> 目标：TerminalBackend/Session + spawn/send/read/signal/close + bounded scrollback + waitReason + 单 agent 独占活动。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S10-01 | 正向 | spawn→send→read | 起 PTY | 发 echo hello | read 返回 hello | 待实现 | terminal_pty_interactive_test.go |
| TC-S10-02 | 正向 | close 退出码 0 | 起 PTY | close | pty 进程退出码 0 | 待实现 | terminal_pty_interactive_test.go |
| TC-S10-03 | 边界 | bounded scrollback | 大量输出 | read | 滚动缓冲受限 | 待实现 | — |
| TC-S10-04 | 并发 | 单 agent 独占 | 多 agent 抢 | 触发 | 仅一个 agent 活动 | 待实现 | — |

### S12 Workflow Engine + tool-workflow（⚪ pending）

> 目标：Worker thread engine + 脚本全局(pipeline/parallel/agent/phase) + Chat Durable Records fold invariant。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S12-01 | 正向 | parallel 3 subagents | 编排 parallel | 运行 2 步 | 汇总结果正确 | 待实现 | workflow_parallel_subagents_test.go |
| TC-S12-02 | 集成 | workflow cancel 级联 | workflow 运行中 | cancel | 子 agent 级联 cancel | 待实现 | — |
| TC-S12-03 | 边界 | Chat Durable Records fold invariant | 记录 chat 记录 | fold 校验 | 不变量成立 | 待实现 | — |

### S13 MCP Client（⚪ pending）

> 目标：MCPTransport + MCPClient + list tools + call tool → 自动映射 ToolDefinition。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S13-01 | 集成 | 连接示例 MCP server | 起 filesystem-server | list tools | 工具自动出现在 ToolRegistry | 待实现 | mcp_client_tool_bridge_test.go |
| TC-S13-02 | 正向 | call tool 映射 | 连接后 | 调用工具 | 正常调用并返回 | 待实现 | — |
| TC-S13-03 | 异常 | 连接失败处理 | server 不可达 | connect | 稳定错误，不 panic | 待实现 | — |

### S14 Workspace Registry（✅ completed）

> 目标：workspace record{id/root/sessionGroup/resume-on-open} + ctx.workspaces。
> 关联文件：`tests/workspace_group_session_test.go`（6 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S14-01 | 正向 | 相同 root 幂等复用 | 创建两次同 root | Create | 返回同一 id | 已实现 | workspace_group_session_test.go |
| TC-S14-02 | 边界 | 异 root 异 id | 不同 root | Create | id 不同 | 已实现 | workspace_group_session_test.go |
| TC-S14-03 | 正向 | resume-on-open 记录 | 设置默认 session | SetResumeOnOpen | 记录正确，可清除 | 已实现 | workspace_group_session_test.go |
| TC-S14-04 | 正向 | 会话分组 | 分组会话 | SetSessionGroup | 分组正确 | 已实现 | workspace_group_session_test.go |
| TC-S14-05 | 异常 | 空 root 拒绝 | 空 root | Create | 拒绝 | 已实现 | workspace_group_session_test.go |

### S15 LLM Retry（✅ completed）

> 目标：exponential backoff + max attempts + jitter + 写 llm/retry event。
> 关联文件：`tests/llm_retry_backoff_event_test.go`（3 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S15-01 | 正向 | 3 次 overload 后成功 | 模拟 3 次 overload | 重试 | 写 3 条 llm/retry，第 4 次成功 | 已实现 | llm_retry_backoff_event_test.go |
| TC-S15-02 | 边界 | 超过 max 转 request-error | 持续失败 | 重试至 max | 转 M34 request-error waterfall | 已实现 | llm_retry_backoff_event_test.go |
| TC-S15-03 | 正向 | 指数退避 + jitter | 观察退避 | 记录 | backoff 指数增长带 jitter | 已实现 | llm_retry_backoff_event_test.go |

### S16 Output Retention（✅ completed）

> 目标：post-execute 后仍保留 canonical value；concurrent reader 保护。
> 关联文件：`tests/output_retention_concurrent_read_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S16-01 | 并发 | 10MB 并发 reader 完整 | 保留 10MB 结果 | 2 reader 并发读 | 均返回完整字节切片 | 已实现 | output_retention_concurrent_read_test.go |
| TC-S16-02 | 正向 | Canonicalize 支持 | []byte/string/JSON | Canonicalize | 各类型归一正确 | 已实现 | output_retention_concurrent_read_test.go |
| TC-S16-03 | 边界 | Remove 逻辑删除 | 保留后删除 | Read | 缺失正确 | 已实现 | output_retention_concurrent_read_test.go |

## 6. 簇 6：文件 & 进程执行（cluster-6-file-process）

### M35 Filesystem 接缝 + obs-policy + tool-fs（✅ completed）

> 目标：FsTarget/Version/Info/EditRequest/WriteIntent + write/edit/resolve/listDir/stat + 观察政策（先读后写）。
> 关联文件：`tests/filesystem_obs_policy_test.go`（6 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M35-01 | 异常 | obs-policy 拒绝裸写 | 未观察的版本 | fs_write | 拒绝 | 已实现 | filesystem_obs_policy_test.go |
| TC-M35-02 | 并发 | 并发编辑同一行 | 2 个 fs_edit 同一行 | 执行 | 第二个 FS_STALE_VERSION | 已实现 | filesystem_obs_policy_test.go |
| TC-M35-03 | 正向 | resolve/stat/read/listDir | 正常路径 | 执行 | 返回正确 | 已实现 | filesystem_obs_policy_test.go |
| TC-M35-04 | 正向 | 原子写 + 版本令牌 | 写文件 | 检查 | 原子写生效，版本更新 | 已实现 | filesystem_obs_policy_test.go |
| TC-M35-05 | 异常 | 稳定错误码 | 路径越界/不存在 | 操作 | 分类错误码 | 已实现 | filesystem_obs_policy_test.go |

### M36 Subprocess 接缝（✅ completed）

> 目标：SubprocessSpawnSpec + tree terminate + CollectedOutput{truncated/spillPath} + scrubbedParentEnv。
> 关联文件：`tests/subprocess_tree_terminate_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M36-01 | 集成 | Win taskkill /T 全树清理 | 起子进程树 | Terminate | 无残留进程 | 已实现 | subprocess_tree_terminate_test.go |
| TC-M36-02 | 集成 | *nix SIGTERM→grace→SIGKILL | 起子进程树 | Terminate | 树清理正确 | 已实现 | subprocess_tree_terminate_test.go |
| TC-M36-03 | 正向 | 输出超阈值 spill | 大输出 | CollectedOutput | 截断+spillPath 正确 | 已实现 | subprocess_tree_terminate_test.go |
| TC-M36-04 | 边界 | DSH_* env scrub | 环境含 DSH_* | spawn | 父环境被清理 | 已实现 | subprocess_tree_terminate_test.go |

### M37 Shell/Bash 接缝 + tool-bash（✅ completed）

> 目标：ShellExecRequest→resolve()→ExecSpec + RunResult 5 字段正交 + SandboxExecutionPolicy + 前台 run / 后台 Job。
> 关联文件：`tests/bash_subprocess_spill_test.go`（3 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M37-01 | 正向 | 长输出截断+spill 完整 | bash('seq 1 100000') | 运行 | 64kb 截断，读 spillPath 拿完整 100000 行 | 已实现 | bash_subprocess_spill_test.go |
| TC-M37-02 | 边界 | 超时 timedOut | timeout 30s | 运行长命令 | RunResult.timedOut=true | 已实现 | bash_subprocess_spill_test.go |
| TC-M37-03 | 正向 | 5 正交字段 | 各场景运行 | 检查 RunResult | exitCode/signal/timedOut/aborted/timeoutMs 正交正确 | 已实现 | bash_subprocess_spill_test.go |

### M42 Spill Storage 溢出接缝（✅ completed）

> 目标：ctx.spill{previewBytes/maxTotalBytes/writeFile ref} + spill-policy 作为 post-tool/Bash listener。
> 关联文件：`tests/spill_recovery_roundtrip_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M42-01 | 正向 | 工具/Bash 结果分别 spill | 结果超阈值 | Apply | 均正确 spill | 已实现 | spill_recovery_roundtrip_test.go |
| TC-M42-02 | 集成 | 读引用还原一致 | 已 spill | 读引用 | 内容字节一致 | 已实现 | spill_recovery_roundtrip_test.go |
| TC-M42-03 | 边界 | 私有 0700 + 独占写 | 创建 spill 文件 | 检查 | 权限 0700，wx 独占 | 已实现 | spill_recovery_roundtrip_test.go |
| TC-M42-04 | 异常 | 保存失败 best-effort | 写失败 | Apply | 不阻塞主流程 | 已实现 | spill_recovery_roundtrip_test.go |

### M46 Job 生命周期 owner 绑定（✅ completed）

> 目标：owner Agent 绑定 dispose cancel + JobSnapshot{ownerSession}。
> 关联文件：`tests/jobs_incremental_output_test.go`（覆盖）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M46-01 | 集成 | Dispose → 取消名下全部 Jobs | 多 Job 运行 | DisposeOwner | 全部走 cancel hook | 已实现 | jobs_incremental_output_test.go |
| TC-M46-02 | 集成 | 孤儿进程清理 | Dispose 后 | 检查 | 孤儿进程彻底清理 | 已实现 | jobs_incremental_output_test.go |
| TC-M46-03 | 正向 | JobSnapshot ownerSession | 创建 Job | 读取快照 | ownerSession 正确 | 已实现 | jobs_incremental_output_test.go |

### S11 Jobs Runtime（✅ completed）

> 目标：JobStart/Hooks/Snapshot + read/waitForDone/cancel/listByOwner。
> 关联文件：`tests/jobs_incremental_output_test.go`

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S11-01 | 正向 | 增量 read | 后台循环输出 | 每 100ms read | 读到下一个数字 | 已实现 | jobs_incremental_output_test.go |
| TC-S11-02 | 正向 | Job cancel | 运行中 Job | cancel | 取消生效 | 已实现 | jobs_incremental_output_test.go |
| TC-S11-03 | 正向 | listByOwner | 多 owner | 列出 | 按 owner 过滤正确 | 已实现 | jobs_incremental_output_test.go |

## 7. 簇 7：配置 & 凭证 & 技能（cluster-7-config）

### M38 Settings 接缝 + pathop + CAS + secrets（✅ completed）

> 目标：SettingsNamespace + SettingsScope(get/watch/update/replace) + SettingsPathOp{set/unset} + describe(redactSecrets:true) + expectedRevision CAS + secrets role redaction。
> 关联文件：`tests/settings_pathop_secrets_test.go`（3 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M38-01 | 正向 | secret 脱敏 describe | secret 字段 | describe(redactSecrets:true) | 只给 path + set/unset 操作位，value 全脱敏 | 已实现 | settings_pathop_secrets_test.go |
| TC-M38-02 | 并发 | CAS expectedRevision | 并发两个 replace | 写 | 后写 expectedRevision 不符触发 CAS 错误 | 已实现 | settings_pathop_secrets_test.go |
| TC-M38-03 | 正向 | host/session 分层 | 两层设置 | Get | nearest-wins 正确 | 已实现 | settings_pathop_secrets_test.go |
| TC-M38-04 | 正向 | PathOp set/unset | 操作配置 | 检查 | set/unset 生效 | 补充用例 | — |

### M39 Credentials & Authorization 接缝（✅ completed）

> 目标：CredentialRef(POSIX 变量名品牌) + 每请求 resolve + describe/writable + set/unset + AuthorizationFlow + record modify CAS。
> 关联文件：`tests/credentials_per_request_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M39-01 | 正向 | 每请求 resolve | LLM 请求中 | 每轮 resolve | 每轮解析一次 CredentialRef | 已实现 | credentials_per_request_test.go |
| TC-M39-02 | 正向 | 修改 env 后新值可见 | 修改 env | 下一轮请求 | 看到新值 | 已实现 | credentials_per_request_test.go |
| TC-M39-03 | 正向 | 授权流注册并 begin | 注册 flow | begin | 成功 | 已实现 | credentials_per_request_test.go |
| TC-M39-04 | 正向 | Storage Domain CAS 持久化 | 修改 credential | 检查 | CAS 持久化正确 | 已实现 | credentials_per_request_test.go |

### M40 Skill 系统 6 层 rank + fsnotify 观察 + tool-skill（✅ completed）

> 目标：SkillProvider.list/get + SkillCandidate{rank/locator} + 6 层 rank + fsnotify + skills/change 事件 + invocable 策略。
> 关联文件：`tests/skills_registry_fsnotify_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-M40-01 | 集成 | 新建 skill 生效 | 新建 my-skill.md | skills.list | 下一次 list 出现 | 已实现 | skills_registry_fsnotify_test.go |
| TC-M40-02 | 集成 | 删除 skill 消失 | 删除 skill | skills.list | 下一次不出现 | 已实现 | skills_registry_fsnotify_test.go |
| TC-M40-03 | 正向 | 6 层 rank 同名决胜 | 多层同 skill | 解析 | rank 高者胜 | 已实现 | skills_registry_fsnotify_test.go |
| TC-M40-04 | 集成 | skill(name) 注入 | 调用工具 | 检查 | injected-context user/message 正确 | 已实现 | skills_registry_fsnotify_test.go |

### S06 Authorization Service（OAuth 流 stub）（✅ completed）

> 目标：credentials 的 flow 部分；stub 能列出/开始/取消即可。
> 关联文件：`tests/authorization_flow_stub_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-S06-01 | 集成 | list→begin→回调→resolve | 注册 flow | 完整链路 | resolved credential 可被 M39 resolve | 已实现 | authorization_flow_stub_test.go |
| TC-S06-02 | 边界 | cancel | begin 后 | cancel | 状态 cancelled | 已实现 | authorization_flow_stub_test.go |
| TC-S06-03 | 异常 | 双完成拒绝 | 已完成 flow | 再次 Complete | 拒绝 | 已实现 | authorization_flow_stub_test.go |
| TC-S06-04 | 异常 | 未知 token | 无效 token | 查询 | 错误 | 已实现 | authorization_flow_stub_test.go |

## 8. 簇 8：缓存命中率（cluster-8-cache-affinity）

### N01 DeepSeek Prefix Cache 探针埋点（✅ completed）

> 目标：TokenUsage 补全 + CacheStats + ComputeHitRatio() 防 0/0 NaN + DeepSeek 客户端 SSE 解析 + Token Meter 增强。
> 关联文件：`tests/cache_probe_unit_test.go`（5）+ `tests/cache_probe_deepseek_client_test.go`（1）PASS

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-N01-01 | 正向 | 缓存字段补全 | TokenUsage | 序列化 | PromptCacheHitTokens + PromptCacheMissTokens 存在 | 已实现 | cache_probe_unit_test.go |
| TC-N01-02 | 边界 | ComputeHitRatio 防 NaN | 0/正常/全命中/全未命中 | 计算 | 各场景确定值 | 已实现 | cache_probe_unit_test.go |
| TC-N01-03 | 正向 | parseUsage 解析 SSE | mock SSE 响应 | parseUsage | 缓存字段解析正确 | 已实现 | cache_probe_deepseek_client_test.go |
| TC-N01-04 | 正向 | 日志含 hit_ratio | 每次 LLM 请求 | 检查日志 | cache.hit_ratio=... 字段存在 | 已实现 | cache_probe_unit_test.go |

### N02 D1 严格 append-only fold + SurfaceOp 强化（✅ completed）

> 目标：Session 仅暴露 Append() + SurfaceOp 表面替换 + 8 条不变量 day-1 校验。
> 关联文件：`tests/session_invariant_8_test.go`（7 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-N02-01 | 正向 | 仅 Append 写路径 | 编译检查 | 扫描 Session 方法 | 无其他 public/private 写方法 | 已实现 | session_append_only_test.go |
| TC-N02-02 | 边界 | 8 条不变量 | 构造反例 | VerifyInvariants | 全部覆盖 | 已实现 | session_invariant_8_test.go |
| TC-N02-03 | 异常 | 反例捕获 | 注入 seq 跳号/time 回退/turn 不配对 | 校验 | 被 invariant 捕获 | 已实现 | session_invariant_8_test.go |
| TC-N02-04 | 异常 | 失败拒绝 append + 账本 | invariant 失败 | append | 抛 InvariantError + 拒绝 + 登记 ledger | 已实现 | session_invariant_8_test.go |
| TC-N02-05 | 正向 | 50 轮 seq/time 严格 | 50 轮对话 | 校验文件 | seq 严格 1..N + time 单调 | 已实现 | session_invariant_8_test.go |

### N03 D2 System Prompt 模板只拷原版 + order 写死 + 静态检测（✅ completed）

> 目标：sections 严格拷贝原版 + Order() 读 const + 禁插动态值 + 运行时 Recorder + CI 静态检测。
> 关联文件：`tests/sysprompt_static_check_test.go`（5 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-N03-01 | 正向 | 渲染 1000 次逐字节相同 | 各 Section | Render 1000 次 | 逐字节相同（纯函数） | 已实现 | sysprompt_static_check_test.go |
| TC-N03-02 | 异常 | 动态值反模式检测 | 含 time.Now/rand 等 | StaticCheck | 检测出 violation | 已实现 | sysprompt_static_check_test.go |
| TC-N03-03 | 正向 | 系统 hash 跨轮稳定 | 50 轮对话 | 比较 hash | system_prompt_hash 跨轮相同 | 已实现 | sysprompt_static_check_test.go |
| TC-N03-04 | 边界 | Order() 读 const | 运行时尝试覆盖 | 读取 | 不能被覆盖 | 已实现 | sysprompt_static_check_test.go |

### N04 D3 Skills catalog 稳定序列化 + change-only 注入（✅ completed）

> 目标：CatalogText 字典序稳定 + MaybeInjectCatalog hash diff change-only 注入 + fsnotify 刷新。
> 关联文件：`tests/skill_catalog_change_only_test.go`（3 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-N04-01 | 正向 | CatalogText 1000 次逐字节相同 | 多 skill | 调用 1000 次 | 无随机/无时间戳 | 已实现 | skill_catalog_change_only_test.go |
| TC-N04-02 | 集成 | 增删 skill 触发 inject | 增删 skill | next call | hash 变化 → inject 触发 | 已实现 | skill_catalog_change_only_test.go |
| TC-N04-03 | 边界 | 50 轮不变只注入 1 次 | 50 轮无变化 | 观察 | <available_skills> 只注入 1 次 | 已实现 | skill_catalog_change_only_test.go |

### N05 D4 PromptContext change-only 注入 + Compaction 保留 snapshot（✅ completed）

> 目标：PromptContext 注册表 + change-only hash 持久化 + GoalRoundContext 续轮 + Compaction 保留最后 context snapshot。
> 关联文件：`tests/prompt_context_change_only_test.go`（5 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-N05-01 | 边界 | Compute hash 稳定/变化 | 状态变/不变 | Compute | 无变化稳定，变化改变 | 已实现 | prompt_context_change_only_test.go |
| TC-N05-02 | 集成 | goal.complete 停止注入 | goal complete | 观察 | 自动停止 | 已实现 | prompt_context_change_only_test.go |
| TC-N05-03 | 集成 | compaction 保留最新 snapshot | 执行 compaction | 查 derived messages | 最后 snapshot 仍可找到 | 已实现 | prompt_context_change_only_test.go |
| TC-N05-04 | 集成 | 变更走 user-msg 不修改 system | plan/goal/approval 变更 | 检查 | 全部 user-msg 追加 | 已实现 | prompt_context_change_only_test.go |
| TC-N05-05 | 性能 | 50 轮 + 切换后命中率 | 50 轮+plan 切换+goal | E2E | 命中率 ≥ 95% | 已实现 | prompt_context_change_only_test.go |

### N06 4 类反模式防御 + 自定义 lint 工具（✅ completed）

> 目标：static_check + 编译期拒绝 + JSON Schema 定序 + catalog 字典序；internal/lint + check-cache-safety.sh。
> 关联文件：`tests/cache_safety_lint_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-N06-01 | 异常 | 4 类反模式 AST 扫描 | 注入反模式代码 | ScanDir/ScanFile | 检测出 Violation | 已实现 | cache_safety_lint_test.go |
| TC-N06-02 | 正向 | schema 定序输出 | 复杂 schema | compileToJSONSchema | properties/required/enum 字典序 | 已实现 | cache_safety_lint_test.go |
| TC-N06-03 | 边界 | ToolRegistry.List 稳定 | 多次 List | 比较 | 字典序稳定 | 已实现 | cache_safety_lint_test.go |
| TC-N06-04 | 集成 | check-cache-safety.sh 0 violations | 运行脚本 | 扫描 | 0 violations | 已实现 | cache_safety_lint_test.go |

### N07 缓存命中率 E2E 验收套件（5 个测试）（✅ completed）

> 目标：tests/cache_hit_rate_e2e_test.go 提供 5 个 E2E 测试 + deepseek_mock.go 提供 Mock 前缀缓存模拟。
> 关联文件：`tests/cache_hit_rate_e2e_test.go`（T1-T5 全 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-N07-01 | 性能 | T1 50 轮稳定率 | 稳定场景 | 跑 50 轮 | 平均命中率 ≥ 95% | 已实现 | cache_hit_rate_e2e_test.go |
| TC-N07-02 | 边界 | T2 切 preset 失效 | 切 preset | 观察 | 前 5 轮 < 50%，后稳定，末 5 轮 ≥ 80% | 已实现 | cache_hit_rate_e2e_test.go |
| TC-N07-03 | 集成 | T3 compaction 恢复 | compaction 后 | 观察 | 30 轮内恢复 ≥ 95% | 已实现 | cache_hit_rate_e2e_test.go |
| TC-N07-04 | 并发 | T4 多 session 并发 | 10 并发 | 运行 | 各 ≥ 85% | 已实现 | cache_hit_rate_e2e_test.go |
| TC-N07-05 | 性能 | T5 工具数量不降 | 5/10/20 工具 | 运行 | 命中率均 ≥ 95% | 已实现 | cache_hit_rate_e2e_test.go |

### N08 缓存破窗告警（✅ completed）

> 目标：CacheAlert：单次突降 > 30% warn；连续 5 次 < 50% error + 可选 webhook。
> 关联文件：`tests/cache_alert_threshold_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-N08-01 | 异常 | 单次突降 >30% warn | 命中率骤降 | Observe | warn 日志含 session/current/previous | 已实现 | cache_alert_threshold_test.go |
| TC-N08-02 | 异常 | 连续 5 次 <50% error | 连续低命中 | Observe | error 日志 + 可选 webhook | 已实现 | cache_alert_threshold_test.go |
| TC-N08-03 | 正向 | 恢复后重置 | 命中率恢复 | Observe | consecutiveFails 归零 | 已实现 | cache_alert_threshold_test.go |
| TC-N08-04 | 边界 | 阈值可配置 | 修改 Config | Observe | 新阈值生效 | 已实现 | cache_alert_threshold_test.go |

### N09 Grafana 缓存命中率看板 + OTel 探针（✅ completed）

> 目标：4 个 OTel 指标 + 3 面板看板 + 告警规则。
> 关联文件：`tests/cache_metrics_otel_integration_test.go`（4 用例 PASS）

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-N09-01 | 集成 | 4 指标正确导出 | Mock OTel collector | 触发 | 含 session/preset/turn 标签 | 已实现 | cache_metrics_otel_integration_test.go |
| TC-N09-02 | 集成 | Grafana 看板 schema 校验 | 看板 JSON | 校验 | 3 面板完整 | 已实现 | dsh-cache-dashboard.json |
| TC-N09-03 | 边界 | 告警规则 | hit_ratio_p50<0.8 持续 10min | 触发 | fire | 已实现 | cache_metrics_otel_integration_test.go |
| TC-N09-04 | 正向 | broken_count 计数 | 检测破缓存模式 | MarkBroken | +1 | 已实现 | cache_metrics_otel_integration_test.go |

## 9. 簇 9：并发加固（cluster-9-concurrency-hardening）

### H01 Agent 请求 ctx 透传（⚪ pending）

> 目标：取消/超时/追踪传播进工具实现与 LLM 请求；压测无调用泄漏。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-H01-01 | 集成 | 客户端取消传播 | 工具/LLM 执行中 | 取消 | 均收到 Done，Agent 以 aborted/interrupted 收尾 | 待实现 | ctx_cancel_propagation_test.go |
| TC-H01-02 | 边界 | 超时预算 | 预算到期 | 继续请求 | 不再发起新调用，已有调用按取消语义关闭 | 待实现 | agent_timeout_budget_test.go |
| TC-H01-03 | 集成 | trace/span 传播 | 有入口 span | 调用工具 | span 一路带到工具 ctx | 待实现 | — |
| TC-H01-04 | 并发 | 压测无泄漏 | 高并发 | 监控 | goroutine/in-flight 收敛 | 待实现 | — |

### H02 持久化全局锁分片化（⚪ pending）

> 目标：按 SessionID 哈希分片锁或每会话异步批量 writer。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-H02-01 | 并发 | 32 并发会话无锁热点 | 32 会话并发 | Append | shard 锁均匀分布，无等待热点 | 待实现 | persistence_concurrent_shard_test.go |
| TC-H02-02 | 集成 | 崩溃修复语义一致 | 分片/异步 writer | 触发崩溃 | 孤儿 turn 仍正确补写 | 待实现 | persistence_async_writer_flush_test.go |
| TC-H02-03 | 集成 | 退出全落盘 | 批量落盘 | 进程退出 | Flush Checkpoint 全部落盘不丢 | 待实现 | — |

### H03 LLM HTTP 客户端超时与连接池调优（⚪ pending）

> 目标：可配置 Transport + Client.Timeout + 429/过载与 S15 退避联动。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-H03-01 | 边界 | MaxConnsPerHost 生效 | 超上限并发 | 请求 | 排队而非无限建连 | 待实现 | llm_http_timeout_transport_test.go |
| TC-H03-02 | 异常 | Timeout 映射 | 请求超时 | LLM 调用 | 返回 LlmFailure{timeout} 而非挂死 | 待实现 | llm_http_timeout_transport_test.go |
| TC-H03-03 | 集成 | 429 联动 S15 | 触发 429 | 观察 | S15 退避 + llm/retry 事件，恢复后不再触发 | 待实现 | llm_ratelimit_backoff_link_test.go |

### H04 Session 派生增量 fold（⚪ pending）

> 目标：去掉 O(N²) 全量重放；尾部增量 fold + 游标失效重建。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-H04-01 | 正向 | 增量与全量一致 | 长会话 | 增量 fold vs 全量 | 逐字段一致 | 待实现 | derive_incremental_fold_test.go |
| TC-H04-02 | 性能 | 1k+ 事件近 O(1) | 1k+ 事件 | 单步增量 fold | 耗时与事件数解耦 | 待实现 | derive_incremental_fold_test.go |
| TC-H04-03 | 边界 | surface replace 失效重建 | 执行 replace | 读派生 | 游标失效重建，不返回陈旧派生 | 待实现 | derive_cursor_surface_invalidate_test.go |

### H05 持久化 IO 内存与读取复用（⚪ pending）

> 目标：sync.Pool bytes.Buffer + 预分配 + Snapshot 游标避免全量读。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-H05-01 | 性能 | 100 次 Append+Flush 内存下降 | 基准对比 | 运行 bench | 分配数显著下降 | 待实现 | persistence_io_reuse_bench_test.go |
| TC-H05-02 | 性能 | 10k 事件 Snapshot 免反序列化 | 10k 事件 | Snapshot() | 不再全量 JSON 反序列化 | 待实现 | persistence_snapshot_cursor_test.go |
| TC-H05-03 | 集成 | 行为一致 | 既有场景 | 回归 | 与 crash_repair 等测试一致 | 待实现 | — |

### H06 工具流水线对象池与懒分配（⚪ pending）

> 目标：Meta 惰性初始化 + sync.Pool 复用核心对象。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-H06-01 | 性能 | Meta 不写不分配 | 不写 meta | Run | 不分配 map | 待实现 | tools_pipeline_pool_bench_test.go |
| TC-H06-02 | 并发 | 池复用并发安全 | 高并发工具 | go test -race | 无竞态、无数据串扰 | 待实现 | tools_pipeline_pool_safety_test.go |
| TC-H06-03 | 集成 | 既有测试 PASS | 回归 | 运行 | tools_waterfall/conclude_turn PASS | 待实现 | — |

### H07 共享注册表只读化 + schema 预编译缓存（⚪ pending）

> 目标：启动期冻结只读 + copy-on-write 换表 + tools schema 预编译缓存。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-H07-01 | 并发 | 冻结后无锁并发读 | 冻结注册表 | 并发读 | atomic.Pointer 读安全 | 待实现 | registry_readonly_schema_cache_test.go |
| TC-H07-02 | 集成 | copy-on-write 换表 | 运行期变更 | 读注册表 | 读者不感知撕裂 | 待实现 | registry_copy_on_write_swap_test.go |
| TC-H07-03 | 性能 | schema 只编译 1 次 | 同配置 N 轮 | prompt 组装 | 只编译 1 次 | 待实现 | — |
| TC-H07-04 | 边界 | 缓存保持字典序 | 缓存命中 | 输出 | 仍保持 N06 定序 | 待实现 | — |

### H08 goroutine 资源治理 + 单一 watcher（⚪ pending）

> 目标：Agent 空闲回收 + skills 单 watcher 统一分发。

| 用例ID | 类型 | 用例名称 | 前置条件 | 测试步骤 | 预期结果 | 状态 | 关联文件 |
|---|---|---|---|---|---|---|---|
| TC-H08-01 | 边界 | 空闲回收 | 超空闲阈值 | 观察 | worker goroutine 退出，新请求可重建 | 待实现 | agent_idle_reclaim_test.go |
| TC-H08-02 | 集成 | resume 冷启动恢复 | resume-on-open | 重开 | 正确冷启动 | 待实现 | — |
| TC-H08-03 | 集成 | 单 watcher 不重复 | skill 变更 | 观察 | 仍触发 N04 注入且不重复 | 待实现 | skills_single_watcher_test.go |

## 10. COULD 级扩展（extensions / ui-skip）

> 以下任务均为 **deferred（延期）** 或 **skipped（跳过）** 状态，用例仅记录验收标准，**当前不要求实现与执行**。

### C01 web_fetch 工具自定义接入（⏳ deferred）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C01-01 | 集成 | 按 ToolDefinition 接入 HTTP 工具 | 业务侧注册 web_fetch 工具 | 可正常调用 | 延期 |

### C02 Web 能力接缝 ctx.web（⏳ deferred）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C02-01 | 集成 | 浏览器/HTTP 抽象复用 | 供 C01 复用 | 抽象层可用 | 延期 |

### C03 Webhook runtime（⏳ deferred）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C03-01 | 集成 | 事件调度与会话创建 | 触发 webhook | 事件正确调度、会话创建 | 延期 |

### C04 LSP 语言服务器集成（⏳ deferred）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C04-01 | 正向 | 语义跳转/重命名/引用 | 连接 LSP | 功能可用（或 grep+AST 兜底） | 延期 |

### C05 Typert CLI 框架（⚫ skipped）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C05-01 | — | 无头后端用 Gin/gRPC 替代 | 不实现 | 无验收 | 跳过 |

### C06 Schedule 会话级 cron 提醒（⏳ deferred）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C06-01 | 集成 | 业务定时任务系统对接 | 对接 M08 Agent | 提醒生效 | 延期 |

### C07 Agent Teams 实验性功能（⏳ deferred）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C07-01 | 集成 | roster/mailbox/task DAG | 基于 S02+M45 构建 | 团队协作正确 | 延期 |

### C08 Slots UI 组合系统（⚫ skipped）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C08-01 | — | UI 相关，无头后端跳过 | 不实现 | 无验收 | 跳过 |

### C09 Extensions / 动态 Cordis 插件（⏳ deferred）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C09-01 | 集成 | Go Plugin 接口 + 组合注册 | 实现插件接口 | 插件可加载组合 | 延期 |

### C10 Code Runtime(PTC) worker 线程（⏳ deferred）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C10-01 | 集成 | PTC worker 执行 | 模型侧支持后 | worker 正常执行 | 延期 |

### C11 Conversation Node 引擎（⚫ skipped）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C11-01 | — | UI 折叠，服务端不实现 | 不实现 | 无验收 | 跳过 |

### C12 Client Modules / Web Client / Remote API（⚫ skipped）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C12-01 | — | UI 及远程 API | 不实现 | 无验收 | 跳过 |

### C13 Web Server + HTTP API（⏳ deferred）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C13-01 | 集成 | Gin/gRPC 暴露能力 | 业务侧实现 | HTTP API 可用 | 延期 |

### C14 Approval UI / Settings Panel / Command Palette（⚫ skipped）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C14-01 | — | UI | 不实现 | 无验收 | 跳过 |

### C15 Attachment 消费端（image resize / OCR）（⏳ deferred）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C15-01 | 集成 | 多模态扩展 | 基于 M29 | resize/OCR 可用 | 延期 |

### C16 Sandbox landlock/bwrap 平台强制（⏳ deferred）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C16-01 | 集成 | Linux/macOS 平台 Provider | 基于 M26 | 平台强制生效 | 延期 |

### C17 Session Control Frame（⚫ skipped）

| 用例ID | 类型 | 用例名称 | 测试步骤 | 预期结果 | 状态 |
|---|---|---|---|---|---|
| TC-C17-01 | — | 服务端 stream/channel 替代 | 不实现 | 无验收 | 跳过 |

---

## 11. 用例汇总与回归建议

### 11.1 用例统计

| 分类 | 任务数 | 用例数 | 已实现(有测试) | 补充建议 | 待实现/延期 |
|---|---|---|---|---|---|
| MUST (M01-M48) | 48 | 188 | 175 | 13 | 0 |
| MUST (N01-N09) | 9 | 38 | 38 | 0 | 0 |
| SHOULD (S01-S16) | 16 | 59 | 34 | 1 | 24 |
| COULD (C01-C17) | 17 | 17 | 0 | 0 | 17(延期/跳过) |
| MUST (H01-H08) | 8 | 26 | 0 | 0 | 26 |
| **合计** | **98** | **328** | **247** | **14** | **67** |

> 说明：
> - 「已实现」= 对应功能已有代码 + 现有 `tests/` 测试覆盖的用例；
> - 「补充建议」= 现有测试未明确覆盖、建议后续补充的边界/集成用例；
> - 「待实现」= S 簇 pending 7 项 + H 簇 8 项（实现后必须按表补测）；
> - 「延期/跳过」= C 簇 17 项（deferred/skipped），当前不要求执行。
> - 实际 `tests/` 目录含约 75 个测试文件，单个测试文件内通常包含多个子用例，与上表用例号大致对应。

### 11.2 回归优先级建议

1. **P0 基础回归**：M01-M08 + M23 + M30 + M48（基座四件套与内核驱动），任何上层改动后必跑。
2. **P1 安全回归**：M22/M24/M27/M31 + N 簇全部（安全与缓存命中率耦合最紧密）。
3. **P2 功能回归**：M09-M21、M32-M47 + S 簇已实现项。
4. **P3 扩展回归**：H 簇实现后加入；C 簇延期项实现时按表补测。

### 11.3 测试运行命令

```bash
# 运行全部单元测试
go test ./... -count=1

# 运行缓存命中率 E2E 验收套件
go test ./tests/ -run TestE2E -v -count=1

# 运行竞态检测（H 簇并发用例依赖）
go test -race ./...

# 静态缓存安全检测
bash scripts/check-cache-safety.sh
```

---

> 本文档与 [tasks.json](./tasks.json)、[TASKS.md](./TASKS.md) 同步维护。任务状态变化时，请同步更新本文档对应用例的「状态」列与统计表。

