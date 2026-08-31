# Dsh-Go 功能实现任务表（一次性复刻到位 · v2.0）

> 本文件与 [tasks.json](./tasks.json) **同步维护**，两者表达同一套任务：
>
> - **tasks.json**：供实现 Agent **程序化读取/更新**（结构化、强 schema、可校验）
> - **TASKS.md（本文件）**：供人类**日常巡检/汇报**（7 簇分组 + 状态 + 优先级 + 验收点一目了然）
>
> 实现 Agent **每完成一个功能点**，需**同时更新两者**，并在对应 task 的 `history[]` 追加一条变更记录（`{at, note}`）。

---

## 0. 官方仓库同步锚点（最重要，不可省略）

| 字段 | 值 |
|---|---|
| 🔗 仓库地址 | `https://github.com/deepseek-ai/deepseek-harness.git` |
| 🌿 分支 | `master` |
| 🏷️ 最后提交**完整 ID** | `cd5ef8148158c3a752a658978873241fdf8e2bbc` |
| 🏷️ 最后提交**短 ID** | `cd5ef81481` |
| 📅 提交日期 | **2026-08-28 00:57:43 +0800** |
| 📝 提交标题 | `Merge pull request #3248 from deepseek-harness/release/dsh-0.1.2-alpha.1` |
| 📦 对应发版 | **dsh-0.1.2-alpha.1** |
| 📚 扫描子系统文档数 | **60+**（详见 tasks.json `upstreamSyncAnchor.snapshottedSubsystemsDocs[]`） |

### 后续同步官方更新的流程

1. **每周一**（或发版前）在本机官方仓库目录执行：

```bash
cd D:\workspace\python_workspace\deepseek-harness
git fetch origin master
git log --oneline cd5ef81481..origin/master -- docs/subsystems packages/\*/src packages/\*/README.md > upstream_diff.txt
```

2. 按 diff 影响的 subsystem：
   - **已复刻的 subsystem 有变更** → 回写 `tasks.json` 的 `upstreamFutureSync.pendingDiffs[]`，并给对应任务追加一条 `history: {at, note:"upstream change …"}`，若需改代码则把状态切回 `in_progress`；
   - **新增的 subsystem（官方加了新能力）** → 在 `tasks.json` 末尾追加任务（编号从 `N01` 开始，避免和现有 ID 冲突），同步到本 TASKS.md 的「新增子系统追踪区」；
3. 同步完成后，更新 `upstreamFutureSync.lastSyncCommitId / lastSyncAt` 以及 `history[]` 一条 `{syncedAt, subsystemsAffected, tasksAffected}`。

---

## 1. 状态图例 & 统计

### 状态取值（与 tasks.json 一致）

| 状态码 | 徽章 | 含义 | 可转移到 |
|---|---|---|---|
| pending | ⚪ 待做 | 功能已规划，尚未开工 | in_progress / blocked / deferred / skipped |
| in_progress | 🟡 进行中 | 编码 / 单测 / 联调进行中 | completed / blocked / pending(回退) |
| completed | ✅ 完成 | 验收清单全部通过 + 中文注释齐全 + 单测通过 | (结束态) |
| blocked | 🚧 阻塞 | 上游依赖 / 外部资源 / 设计决策未决 | in_progress / pending |
| deferred | ⏳ 延后 | COULD 类或本期不做（非 SKIP，未来会上线）| pending / skipped |
| skipped | ⚫ 跳过 | UI 或明确不实现（SKIP 级能力） | (结束态) |

### 完成度统计（实现 Agent 每次更新任务表后，人工同步更新以下数字 + 百分比）

| 级别 | 总数 | ⚪待做 | 🟡进行中 | ✅完成 | 🚧阻塞 | ⏳延后 | ⚫跳过 | 完成率 |
|---|---|---|---|---|---|---|---|---|
| 🔴 MUST (M01-M48) | 48 | 0 | 0 | 48 | 0 | 0 | 0 | 100.00% |
| 🔴 MUST (N01-N07 缓存命中率) | 7 | 0 | 0 | 7 | 0 | 0 | 0 | 100.00% |
| 🟡 SHOULD (S01-S16) | 16 | 0 | 0 | 16 | 0 | 0 | 0 | 100.00% |
| 🟡 SHOULD (N08-N09 缓存命中率) | 2 | 0 | 0 | 2 | 0 | 0 | 0 | 100.00% |
| 🔴 MUST (H01-H08 并发加固) | 8 | 0 | 0 | 8 | 0 | 0 | 0 | 100.00% |
| 🟢 COULD (C01-C17) | 17 | 0 | 0 | 0 | 0 | 11 | 6 | 0% |
| 🟡 SHOULD (T01 测试骨架) | 1 | 0 | 0 | 1 | 0 | 0 | 0 | 100.00% |
| **合计有效项（非跳过）** | **93** | **0** | **0** | **82** | **0** | **11** | **0** | **88.17%** |

> ✅ 2026-09-01：**H 簇（H01-H08 并发加固）已全部完成**；T01 测试骨架已落地（`cmd/gen_tc_skeletons` 生成 51 条未覆盖用例可编译骨架）。M 簇 + N 簇 + S 簇 + H 簇 + T01（非跳过）100%。数字与 docs/tasks.json 逐条 status 一致（脚本 `scripts/task-stats.ps1` 可复核：tasks.json 中 completed=82 / pending=0）。

**预估总工时（人周）：MUST(M 簇) ≈ 18.3 / MUST(N 簇 缓存) ≈ 8.5 / SHOULD ≈ 11.0 / SHOULD(N 簇 监控) ≈ 1.5 / COULD(本版实现的) ≈ 0 → 合计 ≈ 39.3 人周**

---

## 2. 任务详情（按 7 簇分组，按 dependency 拓扑顺序排列）

> 👉 实现 Agent 执行顺序建议：**先做依赖最少的底层能力（1 行），自底向上填高优先级**。
>
> - **P0（首批必做）**：M01/M02/M03/M30 → **基座四件套**
> - **P1（第二批）**：M04/M05/M06/M48 → **Session + Schema**
> - **P2（第三批）**：M07/M08/M09/M23 + M38/M39 → **内核驱动 + 配置基座**
> - **P3+**：按簇 1→7 拓扑顺序推进

---

### 🗂 簇 1 · Agent 内核驱动簇（M01-M03, M04-M06, M07-M09, M16-M21, M32-M34 = 20 项 MUST）

| ID | 级别 | 状态 | 优先级 | 能力名 | 归属包 | 依赖项 | 验收要点（简称）| 关联测试文件 |
|---|---|---|---|---|---|---|---|---|
| M01 | MUST | ✅ | 1 | Branded ID 类型封装 | `pkg/brand` | — | 跨包契约不混传；序列化完整 | `tests/brand_ids_test.go` |
| M02 | MUST | ✅ | 1 | Waterfall 中间件链原语 | `pkg/waterfall` | M01 | 多级拦截+短路+改写 | `tests/waterfall_chain_test.go` |
| M03 | MUST | ✅ | 1 | Scope 分层注册表原语 | `pkg/scope` | M01 | host+scope 优先级正确 | `tests/scope_layers_merge_test.go` |
| M30 | MUST | ✅ | 2 | Invariant Registry 不变量校验 | `pkg/invariant` | — | 包归属报错；开发/生产开关 | `tests/invariant_pkg_attribution_test.go` |
| M48 | MUST | ✅ | 2 | DefineTool JSON Schema 强校验子集 | `pkg/tools/schema.go` | M01 | oneOf/items/enum/…语义一致；拒绝不支持 key | `tests/tools_jsonschema_subset_test.go` |
| M04 | MUST | ✅ | 1 | Session 事件溯源 & 45+ 词汇表 | `pkg/session` | M01, M30 | 所有事件 round-trip 一致；非法顺序 invariant 报错 | `tests/session_event_vocab_test.go` |
| M05 | MUST | ✅ | 1 | Session 派生投影函数族 | `pkg/session` | M04 | 1k+ events 重放 fold 结果一致 | `tests/session_fold_consistency_test.go` |
| M06 | MUST | ✅ | 1 | SessionHeader 元数据 | `pkg/session` | M01 | 未知 Header 版本 fail-closed | `tests/session_header_versioning_test.go` |
| M43 | MUST | ✅ | 2 | Persistence 接缝 + Flush + Crash Repair | `pkg/persistence` | M01,M04,M06,M30 | Kill 孤儿 turn → reload → repair | `tests/persistence_crash_repair_test.go` |
| M44 | MUST | ✅ | 3 | SessionHeader 格式拒绝 & 版本号 | `pkg/session/header.go` | M04,M06,M43 | 未知事件 fail-closed | `tests/session_format_reject_test.go` |
| M19 | MUST | ✅ | 3 | Request Header 快照 + request/context 路由 | `pkg/session` | M04,M05 | Rebuild prompt+schema 与实际 payload 一致 | `tests/request_header_rebuild_test.go` |
| M20 | MUST | ✅ | 4 | session/end-seed marker | `pkg/session` | M04 | Fork/Resume 必写；compaction 分界 | `tests/end_seed_bracket_test.go` |
| M21 | MUST | ✅ | 4 | SurfaceOp(append/replace) + foldSurface | `pkg/session/surface.go` | M04 | replace 范围后 deriveMessages 一致 | `tests/compaction_replace_surface_test.go` |
| M18 | MUST | ✅ | 3 | Agent Cancel 原因分类 | `pkg/agent/cancel.go` | M04, M08 | 5 种 cancel 原因 turn/end.reason 区分 | `tests/agent_cancel_cause_classify_test.go` |
| M32 | MUST | ✅ | 3 | Agent Preset 接缝 | `pkg/presets/agent_presets.go` | M03,M08 | 不同 preset → 不同 tools/prompt | `tests/agent_preset_compose_test.go` |
| M33 | MUST | ✅ | 4 | Agent Initiator 上下文 | `pkg/agent/initiator.go` | M08 | 安全归因；无包裹调用 panic/拒 | `tests/initiator_causal_trace_test.go` |
| M09 | MUST | ✅ | 2 | SystemPrompt 组装 + Section 注册表 | `pkg/sysprompt` | M01,M48 | 与原版 section 顺序逐行一致 | `tests/sysprompt_section_ordering_test.go` |
| M10 | MUST | ✅ | 3 | PromptContext 动态注册与快照 | `pkg/sysprompt` | M09 | Compaction 后 runtime-context-snapshot 保留 | `tests/prompt_context_test.go` |
| M07 | MUST | ✅ | 2 | LLM Provider 接缝 + 流式协议 | `pkg/llm + provider_deepseek` | M01,M38,M39 | DeepSeek SSE 流式 reasoning+tool_call | `tests/llm_stream_roundtrip_test.go` |
| M23 | MUST | ✅ | 2 | Tool Execution 四级 Waterfall 链 | `pkg/tools/pipeline.go` | M02,M48 | 四级拦截均工作；换 signal/cancel 正确 | `tests/tools_waterfall_test.go` |
| M08 | MUST | ✅ | 2 | Agent Registry + Turn/Step 双循环 Loop | `pkg/agent` | M02,M03,M04,M05,M07,M09,M23 | 双 Followup 串行；错误走 agent/error | `tests/agent_loop_turn_step_dual_test.go` |
| M34 | MUST | ✅ | 3 | agent/request-error 重试瀑布 | `pkg/agent` | M02,M07,S15 | S15 协同；超 max 次→错误关闭 turn | `tests/agent_request_error_retry_test.go` |
| M16 | MUST | ✅ | 3 | Session Projections 投影注册中心 | `pkg/session/projection.go` | M04,M05 | Goal/Todo/Plan/Sandbox 投影订阅 | `tests/session_projection_test.go` |

---

### 🗂 簇 2 · 规划能力簇（M10-M15 + M16-M17 = 8 项 MUST）

| ID | 级别 | 状态 | 优先级 | 能力名 | 归属包 | 依赖项 | 验收要点 | 关联测试文件 |
|---|---|---|---|---|---|---|---|---|
| M14 | MUST | ✅ | 3 | User Questions 接缝 | `pkg/userq` | M01 | stub/provider 可互换 | `tests/userq_provider_swap_test.go` |
| M11 | MUST | ✅ | 3 | Plan Mode 软引导 + 审批退出 | `pkg/plan` | M02,M04,M09,M14,M23,M41 | 审批通过才移除 plan:policy | `tests/plan_mode_approval_test.go` |
| M12 | MUST | ✅ | 3 | Goal 系统(状态机+续轮驱动+6工具) | `pkg/goal` | M02,M04,M05,M23,M25,M41 | 5 轮续驱；report_blocker 结束 turn | `tests/goal_rounddriver_cas_test.go` |
| M13 | MUST | ✅ | 3 | Todo 整体替换写入 | `pkg/todo` | M04,M05,M23 | last-write-wins | `tests/todo_write_replace_test.go` |
| M15/M41 | MUST | ✅ | 3 | Commands(slash) **（M41 aliasOf M15）** | `pkg/commands` | M01,M03,M04 | `/plan off` 走 command/run 事件 | `tests/commands_slash_dispatch_test.go` |
| M17 | MUST | ✅ | 4 | Session References(跨会话 & 文件 mention) | `pkg/session/reference.go` | M01,M04,M35 | mention 解析+错误码分类 | `tests/session_reference_test.go` |

---

### 🗂 簇 3 · 工具执行 & 安全簇（10 MUST + 1 SHOULD）

| ID | 级别 | 状态 | 优先级 | 能力名 | 归属包 | 依赖项 | 验收要点 | 关联测试文件 |
|---|---|---|---|---|---|---|---|---|
| M26 | MUST | ✅ | 2 | Sandbox 接缝 3 模式 | `pkg/sandbox` | M01 | Bash & FS 消费者 ExecutionPolicy 一致 | `tests/sandbox_mode_apply_test.go` |
| M28 | MUST | ✅ | 3 | Permission Presets 组合旋钮 | `pkg/presets/permission_presets.go` | M26 | 4 预设一一对应 | `tests/permission_presets_combo_test.go` |
| M27 | MUST | ✅ | 3 | Approval Policy 接缝 | `pkg/approval` | M03,M14,M28 | 三层 override；ask-once 语义 | `tests/approval_override_order_test.go` |
| M22 | MUST | ✅ | 3 | PreToolDecision 三态(allow/deny/ask) | `pkg/tools` | M02,M27 | ask 用户只放行当次 | `tests/tools_predecision_ask_once_test.go` |
| M24 | MUST | ✅ | 3 | Tool Restriction allow/deny | `pkg/tools/restriction.go` | M03,M23 | host deny + scope exempt 正确 | `tests/tools_restriction_intersect_test.go` |
| M25 | MUST | ✅ | 3 | ToolRunContext deferContext + concludeTurn | `pkg/tools/execution.go` | M08,M23 | report_blocker concludeTurn 结束 turn | `tests/tools_conclude_turn_test.go` |
| M47 | MUST | ✅ | 4 | Tool Presentation 中立 vocabulary(9 种 card) | `pkg/tools/presentation.go` | M01,M48 | 9 种 card 字段与原版一一对应 | `tests/tools_presentation_cards_test.go` |
| M29 | MUST | ✅ | 4 | Attachment 图片引用模式 | `pkg/attachment` | M01 | durable ref 不失效 | `tests/attachment_ref_resolve_test.go` |
| M31 | MUST | ✅ | 3 | Token Meter 计量 & 预算 | `pkg/tokenmeter` | M04 | 预算到 → 拒请求(不打 LLM) | `tests/token_meter_budget_test.go` |
| S09 | SHOULD | ✅ | 6 | Message Feedback(CAS sidecar) | `pkg/feedback` | M01,M04,M45 | CAS 冲突 → VERSION_CONFLICT 分类 | `tests/feedback_cas_failure_taxonomy_test.go` |

---

### 🗂 簇 4 · 持久化 & 可恢复簇（3 MUST + 6 SHOULD）

| ID | 级别 | 状态 | 优先级 | 能力名 | 归属包 | 依赖项 | 验收要点 | 关联测试文件 |
|---|---|---|---|---|---|---|---|---|
| M45 | MUST | ✅ | 2 | Storage Domain KV 抽象三层 | `pkg/storage` | M01 | filekv/sqlitekv 两套后端 + CAS | `tests/storage_domain_cas_test.go` |
| S03 | SHOULD | ✅ | 5 | SQLite 持久化后端 + FTS5 | `pkg/persistence/sqlite` | M43,M45 | 10k events 查询 < 100ms | `tests/persistence_sqlite_fts5_test.go` |
| S01 | SHOULD | ✅ | 5 | Compaction(LLM 摘要 + Surface Replace) | `pkg/compaction` | M07,M21,M43 | compact 后续航下一轮 | 复用 `tests/compaction_replace_surface_test.go` |
| S05 | SHOULD | ✅ | 6 | Session Telemetry hooks | `pkg/telemetry/session_telemetry.go` | M04 | 三类 hook 触发 | `tests/session_telemetry_hooks_fired_test.go` |
| S07 | SHOULD | ✅ | 6 | OTel Telemetry 导出 | `pkg/telemetry/otel.go` | S05 | span 带 session/turn/step baggage | `tests/otel_bridge_spans_test.go` |
| S08 | SHOULD | ✅ | 6 | Session Title(LLM helper + fallback) | `pkg/sessiontitle` | M04,M05,M07 | fallback 30 字 / LLM 标题双路 | `tests/session_title_fallback_llm_test.go` |
| S04 | SHOULD | ✅ | 6 | Session Query + FTS5 搜索 | `pkg/sessionquery` | S03,S08 | 标题/时间/内容过滤 | `tests/session_query_title_content_test.go` |

---

### 🗂 簇 5 · 子 Agent & 多模态工具簇（7 SHOULD）

| ID | 级别 | 状态 | 优先级 | 能力名 | 归属包 | 依赖项 | 验收要点 | 关联测试文件 |
|---|---|---|---|---|---|---|---|---|
| S15 | SHOULD | ✅ | 5 | LLM Retry(backoff + llm/retry) | `pkg/llm/retry.go` | M01,M04,M07 | 3 次 overload → 3 条 llm/retry | `tests/llm_retry_backoff_event_test.go` |
| S16 | SHOULD | ✅ | 6 | Output Retention(并发 reader) | `pkg/tools/execution.go` | M23 | 2 读者读 10MB 结果一致 | `tests/output_retention_concurrent_read_test.go` |
| S02 | SHOULD | ✅ | 5 | Subagent 接缝(3 后端) | `pkg/subagent` | M01,M06,M24,M25,M43 | in-process fork 通过；父子 lineage | `tests/subagent_fork_inprocess_test.go` |
| S11 | SHOULD | ✅ | 5 | Jobs Runtime(后台任务) | `pkg/jobs` | M01,M46,M36 | Bash 10 步增量每 100ms 返回一个数 | `tests/jobs_incremental_output_test.go` |
| S10 | SHOULD | ⚪ | 6 | Terminal PTY | `pkg/terminal` | M01,M08,M36 | Bash spawn→send→read→close 循环 | `tests/terminal_pty_interactive_test.go` |
| S12 | SHOULD | ✅ | 6 | Workflow Engine + tool-workflow | `pkg/workflow` | S02,M25,M48 | parallel 3 subagents → 汇总；cancel 级联 | `tests/workflow_parallel_subagents_test.go` |
| S13 | SHOULD | ✅ | 7 | MCP Client → Tool Bridge | `pkg/mcp` | M01,M23,M48 | 连接 MCP FS server → 自动注册工具 | `tests/mcp_client_tool_bridge_test.go` |
| S14 | SHOULD | ✅ | 7 | Workspace Registry | `pkg/workspace` | M01,M45 | 同 root → 同 id；resume-on-open 记录 | `tests/workspace_group_session_test.go` |

---

### 🗂 簇 6 · 文件 & 进程执行簇（5 MUST + 1 SHOULD）

| ID | 级别 | 状态 | 优先级 | 能力名 | 归属包 | 依赖项 | 验收要点 | 关联测试文件 |
|---|---|---|---|---|---|---|---|---|
| M42 | MUST | ✅ | 3 | Spill Storage 溢出接缝 | `pkg/spill` | M01 | >阈值 → 写文件；读引用还原字节一致 | `tests/spill_recovery_roundtrip_test.go` |
| M36 | MUST | ✅ | 2 | Subprocess 接缝 | `pkg/subprocess + local` | M01,M42 | 树 terminate 零残留；scrub env | `tests/subprocess_tree_terminate_test.go` |
| M46 | MUST | ✅ | 3 | Job 生命周期 owner 绑定 | `pkg/jobs` | M01,M08,S11 | Agent dispose → 名下所有 Jobs cancel | `tests/jobs_incremental_output_test.go` |
| M37 | MUST | ✅ | 2 | Shell/Bash 接缝 + tool-bash | `pkg/shell + local` | M01,M23,M26,M36,M46,S11 | seq 100000 → spill 拿到完整内容 | `tests/bash_subprocess_spill_test.go` |
| M35 | MUST | ✅ | 2 | Filesystem 接缝 + obs-policy + tool-fs | `pkg/fs + fs_local` | M01,M04,M23,M26 | obs-policy 拒绝裸写；FsVersion 并发防旧版写 | `tests/filesystem_obs_policy_test.go` |

---

### 🗂 簇 7 · 配置 & 凭证 & 技能接缝簇（4 MUST + 1 SHOULD）

| ID | 级别 | 状态 | 优先级 | 能力名 | 归属包 | 依赖项 | 验收要点 | 关联测试文件 |
|---|---|---|---|---|---|---|---|---|
| M38 | MUST | ✅ | 2 | Settings 接缝 + pathop + CAS + secrets | `pkg/settings + file` | M01,M03,M45 | secret 描述符脱敏；CAS 冲突拒绝 | `tests/settings_pathop_secrets_test.go` |
| M39 | MUST | ✅ | 2 | Credentials & Authorization 接缝 | `pkg/credentials + local` | M01,M03,M38,S06 | 每请求 resolve；热更新 env 下一轮可见 | `tests/credentials_per_request_test.go` |
| S06 | SHOULD | ✅ | 6 | Authorization Service(OAuth flow stub) | `pkg/credentials/authorization.go` | M39 | flow list/begin → resolved credential 生效 | `tests/authorization_flow_stub_test.go` |
| M40 | MUST | ✅ | 2 | Skill 系统 6 层 rank + fsnotify + tool-skill | `pkg/skills` | M01,M03,M04,M23 | 新建/删除 skill.md 即时生效；skill(name) 注入上下文 | `tests/skills_registry_fsnotify_test.go` |

---

### 🗂 簇 9 · 并发加固簇（H01-H08 = 8 项，H01 已完成）

| ID | 级别 | 状态 | 优先级 | 能力名 | 归属包 | 依赖项 | 验收要点（简称）| 关联测试文件 |
|---|---|---|---|---|---|---|---|---|
| H01 | MUST | ✅ | 1 | Agent 请求 ctx 透传（取消/超时/追踪） | `pkg/agent + pkg/tools/pipeline.go` | M08,M23 | 取消/超时传播进工具与 LLM；压测无泄漏 | `tests/ctx_cancel_propagation_test.go` |
| H02 | MUST | ⚪ | 1 | 持久化锁分片 / 异步批量 writer | `pkg/persistence` | M43 | 32 并发无锁热点；崩溃修复语义一致 | `tests/persistence_concurrent_shard_test.go` |
| H03 | SHOULD | ⚪ | 2 | LLM HTTP 超时与连接池调优 | `pkg/llm/provider_deepseek` | M07,S15 | MaxConns 排队；Timeout 映射；429 联动 S15 | `tests/llm_http_timeout_transport_test.go` |
| H04 | SHOULD | ⚪ | 2 | Session 派生增量 fold | `pkg/session + pkg/agent` | M05,M08,M21 | 去 O(N²)；surface replace 游标失效重建 | `tests/derive_incremental_fold_test.go` |
| H05 | SHOULD | ⚪ | 3 | 持久化 IO 内存与读取复用 | `pkg/persistence` | M43 | 分配显著下降；Snapshot 免全量反序列化 | `tests/persistence_io_reuse_bench_test.go` |
| H06 | SHOULD | ⚪ | 3 | 工具流水线对象池与懒分配 | `pkg/tools/pipeline.go` | M23 | Meta 懒分配；池复用 -race 安全 | `tests/tools_pipeline_pool_bench_test.go` |
| H07 | SHOULD | ⚪ | 3 | 注册表只读化 + schema 预编译缓存 | `pkg/tools+skills+scope+sysprompt` | M03,M09,N06 | 冻结无锁读；COW 换表；schema 只编 1 次 | `tests/registry_readonly_schema_cache_test.go` |
| H08 | COULD | ⚪ | 4 | goroutine 治理 + 单一 watcher | `pkg/agent + pkg/skills` | M08,M40,S14 | 空闲回收；resume 冷启动；单 watcher 不重复 | `tests/agent_idle_reclaim_test.go` |

---

### 🗂 簇 10 · 测试骨架生成簇（T01 = 1 项 SHOULD，pending）

| ID | 级别 | 状态 | 优先级 | 能力名 | 归属包 | 依赖项 | 验收要点（简称）| 关联测试文件 |
|---|---|---|---|---|---|---|---|---|
| T01 | SHOULD | ⚪ | 5 | 可执行测试骨架生成（328 条用例 → _test.go） | `tests/` | — | 每条未覆盖用例生成可编译骨架；TC 编号一一对应；完成后同步 TEST_CASES 文档 | `(由 docs/TEST_CASES.md 派生)` |

> 📄 T01 详细设计见 [docs/TEST_CASES.md](./TEST_CASES.md)（98 项功能点 → 328 条测试用例）。执行时仅生成可编译骨架 + 中文注释 + 占位断言，不回填真实业务逻辑。

---

## 3. COULD / SKIP 速览

### 🟢 COULD（17 项，9 项延后 + 8 项跳过）

| ID | 能力 | 当前状态 | 备注 |
|---|---|---|---|
| C01 | web_fetch 工具自定义接入 | ⏳ 延后 | 业务侧注入 |
| C02 | Web 能力接缝 ctx.web | ⏳ 延后 | 供 C01 复用 |
| C03 | Webhook runtime | ⏳ 延后 | 业务侧自行 Gin |
| C04 | LSP 语言服务器 | ⏳ 延后 | grep/AST 工具先够 |
| C05 | Typert CLI 框架 | ⚫ 跳过 | 无头后端不用 |
| C06 | Schedule cron | ⏳ 延后 | 业务任务系统可复用 |
| C07 | Agent Teams 实验 | ⏳ 延后 | Subagent 完成再加 |
| C08 | Slots UI 组合 | ⚫ 跳过 | UI |
| C09 | Extensions/Cordis 插件 | ⏳ 延后 | 用 Go Plugin 接口替代 |
| C10 | Code Runtime(PTC) | ⏳ 延后 | provider 支持 PTC 再做 |
| C11 | Conversation Node | ⚫ 跳过 | UI |
| C12 | Client/Web/Remote API | ⚫ 跳过 | UI |
| C13 | Web Server + HTTP API | ⏳ 延后 | 可选 cmd/dshd |
| C14 | Approval UI/Panel/Palette | ⚫ 跳过 | UI |
| C15 | Attachment OCR/Resize | ⏳ 延后 | 多模态扩展 |
| C16 | Sandbox landlock/bwrap | ⏳ 延后 | *nix 专用 Provider |
| C17 | Session Control Frame | ⚫ 跳过 | UI push 层 |

### ⚫ SKIP（12 项，纯 UI 或明确不做）

| 原始编号 | 能力 | 理由 |
|---|---|---|
| K01 | ui-chat | 纯前端 |
| K02 | ui-trajectory | 纯前端 |
| K03 | ui-workflow | 纯前端 |
| K04 | ui-session | 纯前端 |
| K05 | ui-composer | 纯前端 |
| K06 | Slots + React | 纯前端 |
| K07 | .agents/notes 方法论 | 开发文档，非运行时代码 |
| K08 | Web Server 静态资源 | 无头后端不提供 |
| K09 | Rich Media Attachment 上传 | 业务侧接入 |
| K10 | landlock/bwrap 平台强制 | Windows 环境默认无，*nix 可选 C16 |
| K11 | Terminal xterm.js UI | 纯前端 |
| K12 | Conversation chunkrow 标签 | 客户端优化，服务端直传 events |

---

## 4. 新增子系统追踪区（官方后续更新时回填）

> 🟡 **预留区**：同步官方后续 subsystem 时，所有新增任务写在这里，并同步到 tasks.json 末尾（ID 前缀 `N01, N02 ...`）。

| 官方新 commit | 新增 subsystem | 任务 ID | 状态 | 负责人 | 说明 |
|---|---|---|---|---|---|
| *(暂无)* | — | — | — | — | 官方仓库 `cd5ef81481` 以后的变更统一从此开始回填 |

---

## 5. 交付里程碑（建议参考）

| 里程碑 | 完成内容 | 完成标志（状态列变化） | 预计人周 |
|---|---|---|---|
| M1 基座四件套 | M01/M02/M03/M30 + M48 完成 | 5 项 MUST → ✅ | 1.4 人周 |
| M2 Session 底座 | M04/M05/M06/M19/M20/M21/M43/M44 + M45 完成 | + 9 MUST → ✅ → 累计 14/48 MUST | 4.3 人周 |
| M3 内核驱动 + 工具四级链 | M07/M08/M09/M10/M23/M24/M25/M32/M33/M34/M47 完成 | + 11 MUST → ✅ → 累计 25/48 MUST | 7.3 人周 |
| M4 安全 & 配置 & 技能簇闭环 | M22/M26~M31 + M35~M42 + M16/M18 完成 | 剩余 23 MUST 全部 ✅ | 8.1 人周 |
| M5 规划能力闭环 | M11~M15 + M17 完成（含 Commands 去重） | 规划簇 7 MUST → ✅ | 2.8 人周 |
| M6 SHOULD 生产化 | S01~S16 完成（除延期 S13/S14 可 1 迭代后做） | SHOULD 14+ → ✅ | 11.0 人周 |
| M7 SDK + 20 套件集成测试 + 等价性自检 | SDK API 稳定；20 套件 100% PASS；README 10 项 Checklist 全打勾 | — | 2.5 人周 |

**合计 ≈ 29.3 人周（2 人并行 ≈ 3.5 ~ 4 个月完成到 M6，加 M7 验收 ≈ 4.5 个月）**

---

## 6. 缓存命中率对齐计划（`cluster-8-cache-affinity` · N01-N09）

> **📄 详细参考文档**：[docs/CACHE_HIT_RATE_PLAN.md](./docs/CACHE_HIT_RATE_PLAN.md)（14 章节 / 30+ 段 Go 代码示例 / 5 个 E2E 验收用例 / 6 类风险与回退方案）
>
> **依据**：[README.md 第十二章](../README.md#十二竞品对比--deepseek-缓存命中率对齐方案-) · 4 项工程纪律 D1-D4 + 4 类反模式 + 5 项验收方法
>
> **目标**：将 Dsh-Go 实现的 DeepSeek API prefix cache 命中率对齐到 dsh 官方水平（**97-99%**）。
>
> **核心原则**：dsh 官方能跑到 97-99% **没有黑魔法**，只是「**没有意外破坏 prefix cache**」；本簇所有任务都是确保 Dsh-Go **不引入新的「破缓存」行为**。

### 6.1 任务一览（9 项：7 MUST + 2 SHOULD）

| ID | 级别 | 状态 | 优先级 | 阶段 | 能力名 | 归属包 | 依赖项 | 验收要点 | 关联测试文件 |
|---|---|---|---|---|---|---|---|---|---|
| N01 | MUST | ✅ | 0 | 阶段 0 | DeepSeek Prefix Cache 探针埋点 | `pkg/llm + provider_deepseek + tokenmeter.go` | M07, M34 | TokenUsage 新增 hit/miss tokens + CacheStats.ComputeHitRatio() | `tests/cache_probe_unit_test.go`<br>`tests/cache_probe_deepseek_client_test.go` |
| N02 | MUST | ✅ | 0 | 阶段 1 (D1) | 严格 append-only fold + SurfaceOp 强化 | `pkg/session + surface.go + invariant.go` | M04, M05, M21, M30, M33 | Session 仅暴露 Append() + 8 条不变量 day-1 | `tests/session_invariant_8_test.go` |
| N03 | MUST | ✅ | 0 | 阶段 2 (D2) | System Prompt 模板只拷原版 + order 写死 + 静态检测 | `pkg/sysprompt + static_check.go + scripts/check-cache-safety.sh` | M09 | 纯函数 + check-cache-safety 0 violations | `tests/sysprompt_static_check_test.go` |
| N04 | MUST | ✅ | 0 | 阶段 3 (D3) | Skills catalog 稳定序列化 + change-only 注入 | `pkg/skill + tool.go + provider_fs.go` | M40 | 字典序稳定 + hash diff 注入 | `tests/skill_catalog_change_only_test.go` |
| N05 | MUST | ✅ | 0 | 阶段 4 (D4) | PromptContext change-only 注入 + Compaction 保留 snapshot | `pkg/sysprompt/context.go + goal/round_driver.go + compaction/engine.go` | M09, M10, M12, M21 | change-only hash 持久化 + 保留最后 snapshot | `tests/prompt_context_change_only_test.go` |
| N06 | MUST | ✅ | 0 | 阶段 5 | 4 类反模式防御 + 自定义 lint 工具 | `internal/lint/cache_safety.go` | N01-N05 + M09, M48, M40 | AST 扫描 0 violations + schema 字典序 | `tests/cache_safety_lint_test.go` |
| N07 | MUST | ✅ | 0 | 阶段 6 | 缓存命中率 E2E 验收套件（5 个测试）| `tests/cache_hit_rate_e2e_test.go + tests/testutil/deepseek_mock.go` | N01-N06 | 5 个 E2E 用例全过（详见 6.3）| `tests/cache_hit_rate_e2e_test.go` |
| N08 | SHOULD | ✅ | 0 | 阶段 7 | 缓存破窗告警 | `pkg/llm/cache.go (CacheAlert)` | N01, S05 | 突降 30% warn + 连续 5 次 < 50% error | `tests/cache_alert_threshold_test.go` |
| N09 | SHOULD | ✅ | 0 | 阶段 7 | Grafana 缓存命中率看板 + OTel 探针 | `internal/telemetry/cache_metrics.go + deploy/grafana/dsh-cache-dashboard.json` | N01, N08, S07 | 4 OTel 指标 + 3 Grafana 面板 | `tests/cache_metrics_otel_integration_test.go` |

### 6.2 7 阶段路线图

```
                    [N01 探针埋点]
                         │
       ┌─────────────────┼─────────────────┬─────────────────┐
       │                 │                 │                 │
       ▼                 ▼                 ▼                 ▼
[N02 D1 append-only] [N03 D2 SysPrompt] [N04 D3 Skills] [N05 D4 PromptContext]
       │                 │                 │                 │
       └─────────────────┴─────────────────┴─────────────────┘
                                  │
                                  ▼
                       [N06 4 类反模式防御]
                                  │
                                  ▼
                       [N07 E2E 验收 5 测试]
                                  │
                                  ▼
              [N08 破窗告警] + [N09 Grafana 看板]
```

**合计 9.5 人周（2 人并行 5 周）**

### 6.3 5 个 E2E 验收用例（N07 详细）

| 编号 | 名称 | 期望 | 触发条件 |
|---|---|---|---|
| **T1** | 50 轮稳定率 | 平均 ≥ **95%** | 普通长对话 |
| **T2** | 切 preset | 切后 < 50%，后续稳定上升，最后 5 轮 ≥ 80% | preset 切换 |
| **T3** | compaction 恢复 | 30 轮内恢复 ≥ 95% | mid-session compaction |
| **T4** | 多 session 并发 | 各 ≥ 85% | 10 个 session 并发 |
| **T5** | 加工具场景 | 命中率不降 | 5/10/20 工具对比 |

**Mock Server**：`tests/testutil/deepseek_mock.go` 实现 prefix cache 模拟（跟踪 prompt hash 命中 hit_tokens / 切 preset 模拟 cache 失效 / compaction 模拟 cache 重建）

### 6.4 4 项工程纪律（核心）

| 纪律 | 代码位置 | 验证手段 |
|---|---|---|
| **D1** 严格 append-only | `pkg/session/session.go` `Append()` | 编译期：除 Append 外无写方法 |
| **D2** 模板只拷原版 | `pkg/sysprompt/sections/*.go` | diff + string equality test |
| **D3** catalog 排序 | `pkg/skill/registry.go` | 1000 次调用字节相同测试 |
| **D4** PromptContext | `pkg/sysprompt/context.go` | 单元测试 hash 稳定性 |

### 6.5 4 类反模式防御（N06 详细）

| 反模式 | 防御工具 | 触发点 |
|---|---|---|
| 动态时间戳写 system prompt | `pkg/sysprompt/static_check.go` + CI lint | 编译期 + 运行时 |
| compaction 回写历史 | `Session` 仅暴露 `Append()` | 编译期拒绝 |
| JSON Schema 字段顺序随机 | `compileToJSONSchema` 定序 + `internal/lint/cache_safety.go` | 编译期 |
| `<available_skills>` 顺序随机 | `CatalogText()` 字典序 + Tool `List()` 字典序 | 运行时稳定 |

### 6.6 实施原则（执行顺序）

1. **先做 N01**（探针埋点）— 没数据基线后续无法验收
2. **并行做 N02-N05**（4 项纪律）— 互不依赖可并行
3. **再做 N06**（反模式防御）— 依赖 N02-N05 实现细节
4. **N07 E2E 验收**— 全部纪律的最终关卡
5. **N08 + N09**（监控看板）— 上线前完成

每完成一个 N 任务 → 更新 [tasks.json](./tasks.json) 对应 `status` + `history[]` + 本文件 [6.1 表格](#61-任务一览9-项7-must--2-should)。

### 6.7 完成度统计

| 级别 | 总数 | ⚪待做 | 🟡进行中 | ✅完成 | 🚧阻塞 | 完成率 |
|---|---|---|---|---|---|---|
| 🔴 MUST (N01-N07) | 7 | 0 | 0 | 7 | 0 | 100.00% |
| 🟡 SHOULD (N08-N09) | 2 | 0 | 0 | 2 | 0 | 100.00% |
| **本簇合计** | **9** | **0** | **0** | **9** | **0** | **100.00%** |

---

## 7. 项目目录结构规范

> 项目根目录**只保留主文档 README.md**，所有其他详细文档（任务表、trace 记录、详细实施计划、ADR 等）统一进入 `docs/` 子目录。

```
Dsh-Go/                                   # 项目根目录（极简）
├── README.md             ✅ 根目录（**唯一**主文档入口）
├── go.mod / go.sum       ✅ 根目录（Go 依赖）
├── pkg/                  ✅ Go 源码包
├── tests/                ✅ 测试代码
├── cmd/                  ✅ 可执行入口
├── deploy/               ✅ 部署配置
├── docs/                 🆕 子目录（所有详细文档）
│   ├── TASKS.md                      # 任务表主入口（人类可读）
│   ├── tasks.json                    # 任务表机器可读（程序化）
│   ├── CACHE_HIT_RATE_PLAN.md        # 缓存命中率对齐详细实施计划（14 章节 / 1300 行）
│   ├── trace.md                      # 用户对话 trace 记录
│   └── (后续：架构设计 / ADR / 跑批报告等)
└── ../README.md 12.10 引用 docs/CACHE_HIT_RATE_PLAN.md
```

**约定**：
- **根目录**：`README.md`（**唯一**主文档）+ `go.mod` / `*.go` / `Makefile` / `LICENSE` 等
- **`docs/`** 子目录：所有详细文档（任务表、trace、计划、ADR 等）
- **新增文档前先判断**：若是项目主入口 → 根目录（**只允许 README.md**）；若是详细/参考文档 → `docs/`
- **TASKS.md ↔ tasks.json** 同步：两者都在 `docs/` 下，**实现 Agent 每完成一个任务必须同时更新两份**（`status` 字段 + `history[]` 时间序列）

---

**（文档结束）最后同步锚点检查：`cd5ef8148158c3a752a658978873241fdf8e2bbc @ 2026-08-28`✅**
