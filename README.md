# Go 版 DeepSeek Harness (dsh-go) 可行性分析报告

> 项目目标：基于 Go 语言复刻 DeepSeek Harness 的 Agent 规划能力（Planning、Goal、Todo、Skill 等），使 Go 后端服务可直接集成等价的 Agent 能力而无需依赖前端界面。
>
> 分析日期：2026-08-31
> 源项目：`D:\workspace\python_workspace\deepseek-harness`（TypeScript 实现，基于 Cordis 插件框架）

---

## 一、DeepSeek Harness 原版架构全景

### 1.1 整体架构分层

DeepSeek Harness（以下简称 DSH）是一个**高度模块化的事件驱动 Agent 框架**，基于自研的 **Cordis** 插件框架构建。其核心设计理念是：

- **一切皆插件**：模型适配器、工具注册表、会话日志、Agent 循环本身全都是可替换的插件
- **能力接缝（Capability Seam）**：每项能力均为「服务定义 + 服务提供 + 消费端」三层结构，一个后端替换即可改变整个产品行为
- **事件溯源（Event Sourcing）**：会话状态完全由追加式事件日志派生，支持重放、分叉、压缩、持久化
- **Profile/Bundle 分层组合**：运行时通过有序叠加配置层（bundle → profile patch → home patch → CLI patch）构建完整的插件树

```text
                         ┌───────────────────────────────────────┐
                         │          Profile / Bundle 层          │
                         │  (web / headless / sdk / sdk-minimal)│
                         └──────────────────┬────────────────────┘
                                            │
          ┌─────────────────────────────────┼─────────────────────────────────┐
          │                                 │                                 │
          ▼                                 ▼                                 ▼
┌─────────────────────┐        ┌──────────────────────────┐        ┌──────────────────────┐
│   Core Spine 核心层  │        │   Capability Seams 能力层 │        │   Extension 扩展层    │
│  ┌───────────────┐  │        │  ┌────────────────────┐  │        │  ┌─────────────────┐ │
│  │ Session 日志  │  │        │  │ LLM 多模型适配器    │  │        │  │ Subagent 子代理 │ │
│  │ (事件溯源)    │  │        │  │ (deepseek/其他)    │  │        │  │ (多后端)        │ │
│  └───────┬───────┘  │        │  └──────────┬─────────┘  │        │  └────────┬────────┘ │
│  ┌───────▼───────┐  │        │  ┌──────────▼─────────┐  │        │  ┌────────▼────────┐ │
│  │System Prompt  │  │        │  │ Tools 工具执行管线 │  │        │  │ Plan Mode 计划  │ │
│  │ 组装器         │  │        │  │ (Waterfall 瀑布)  │  │        │  │ (prompt引导)    │ │
│  └───────┬───────┘  │        │  └──────────┬─────────┘  │        │  └────────┬────────┘ │
│  ┌───────▼───────┐  │        │  ┌──────────▼─────────┐  │        │  ┌────────▼────────┐ │
│  │ Agent Registry│  │        │  │ Skills 技能系统    │  │        │  │ Goal 目标系统   │ │
│  │ + Agent Loop  │  │        │  │ (多Provider注册表) │  │        │  │ (多轮续驱动)    │ │
│  │ (ReactLoop)   │  │        │  └──────────┬─────────┘  │        │  └────────┬────────┘ │
│  └───────┬───────┘  │        │  ┌──────────▼─────────┐  │        │  ┌────────▼────────┐ │
│  ┌───────▼───────┐  │        │  │ Compaction 上下文  │  │        │  │ Todo 待办系统   │ │
│  │ Scope 作用域   │  │        │  │ 压缩(LLM摘要)     │  │        │  │ (整体替换列表)  │ │
│  └───────────────┘  │        │  └────────────────────┘  │        │  └─────────────────┘ │
└─────────────────────┘        └──────────────────────────┘        └──────────────────────┘
                                            │
                                            ▼
                                  ┌──────────────────────┐
                                  │   Cordis 插件运行时    │
                                  │ (Context/Service/事件) │
                                  └──────────────────────┘
```

### 1.2 核心执行流程：Turn → Step 双循环

DSH 的 Agent 循环是核心驱动，定义在 `packages/core/agent-loop/src/agent.ts` 的 `ReactLoopAgent` 类：

```text
┌──────────────────────────────────────── Turn 轮次 ────────────────────────────────────────┐
│                                                                                            │
│  turn/start ──► claim inbox (next-turn msg + next-step)                                   │
│       │                                                                                    │
│       ├── agent/pre-step (Waterfall: 可拦截/重写消息)                                        │
│       │     │                                                                              │
│       │     ├─ 若被 reject → turn/end { kind: blocked }，本轮结束                            │
│       │     │                                                                              │
│       │     └─ enter(messages) ──► step/start                                              │
│       │                        │                                                           │
│       │                        ▼                                                           │
│       │              ┌──────────── Step 单步 ────────────┐                                  │
│       │              │  1. user/message (写入日志)       │                                  │
│       │              │  2. 派生 history + 组装 prompt    │                                  │
│       │              │  3. agent/request (Waterfall)     │                                  │
│       │              │  4. llm/stream (流式调用模型)      │                                  │
│       │              │  5. assistant/chunk* → message    │                                  │
│       │              │  6. 若有 tool/call*:              │                                  │
│       │              │     tools/pre-execute → execute   │                                  │
│       │              │     → post-execute → tool/result  │                                  │
│       │              │  7. step/end                      │                                  │
│       │              └───────────────────────────────────┘                                  │
│       │                        │                                                           │
│       │                        ▼                                                           │
│       │              仍有 pending tools 或 inbox 有 next-step 输入？                          │
│       │                 │                       │                                           │
│       │               是 ◄─────────────────────┘                                           │
│       │                 │                                                                   │
│       │                 ▼ 否                                                                │
│       └────────────► agent/turn-stopping ──► turn/end                                      │
│                                                                                            │
└────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 1.3 原版核心模块清单

| 模块 | 包路径 | 核心职责 | 复刻优先级 |
|------|--------|---------|-----------|
| **Session** | `packages/core/session` | 事件溯源追加日志，派生 LLM 消息 | 🔴 必须 |
| **Agent Registry + Loop** | `packages/core/agent` + `agent-loop` | Agent 生命周期、Turn/Step 双循环、Inbox | 🔴 必须 |
| **System Prompt** | `packages/core/system-prompt` | Prompt section 顺序组装、工具 Schema | 🔴 必须 |
| **Tools Pipeline** | `packages/core/tools` | 工具注册、Waterfall 执行、参数/输出校验 | 🔴 必须 |
| **LLM Adapter** | `packages/llm/llm` | 模型无关消息/流式词汇表、Provider 注册 | 🔴 必须 |
| **Scope** | `packages/core/scope` | 作用域注册原语，分层注册表基础 | 🟡 重要 |
| **Plan Mode** | `packages/plan/plan-mode` | 计划模式 Prompt 引导 + exit_plan_mode 审批工具 | 🔴 必须 |
| **Goal System** | `packages/goal/goal` + `goal-round-driver` | 会话内持久目标 + 自动续轮驱动 | 🔴 必须 |
| **Todo System** | `packages/todo/tool-todo` | 整体替换式待办列表工具 | 🟡 重要 |
| **Skills** | `packages/skill/skill` + `skill-filesystem` | 多 Provider 技能注册表 + skill() 工具 | 🟡 重要 |
| **Subagent** | `packages/subagent/*` | 子代理能力接缝 + 多 Provider 后端 | 🟢 可选 |
| **Compaction** | `packages/compaction/*` | 上下文压缩（LLM 摘要 + 表面替换） | 🟢 可选 |
| **Persistence** | `packages/storage/*` | JSONL/SQLite 持久化后端 | 🟡 重要 |
| **Cordis** | `vendor/cordis` | 插件框架（Context/Service/事件/Fiber） | 需要替代方案 |

---

## 二、Agent 规划能力深度拆解

### 2.1 Plan Mode（计划模式）—— 软引导规划

**源码位置**：`packages/plan/plan-mode/src/index.ts`

**核心机制**：
- **Prompt Section 注入**：开启时在每个模型请求中插入 `plan:policy`（order=500），引导模型先制定计划再执行
- **状态持久化**：通过 `plan/mode` SessionEvent（log-only）记录，`foldPlanMode()` 从日志派生，支持 resume/fork 恢复
- **用户审批退出**：`exit_plan_mode` 工具要求模型输出完整 Markdown 计划，通过 `user-questions` 接口提交用户审批，审批通过才退出计划模式
- **命令入口**：`/plan [off|message]`，可附带引导消息

**Go 复刻要点**：
```go
// 核心数据结构
type PlanModeState struct {
    Active  bool // 从 session log fold 得出
    Pending bool // 等待下一个 in-turn pre-step 提交的变更
}

// 关键实现点：
// 1. agent/pre-step 前置监听器：在请求派生前注入 plan/mode 事件 + plan:policy section
// 2. exit_plan_mode 工具定义：参数校验 + 调用用户审批接口 + 静默记录 pending exit
// 3. /plan 命令：设置状态 + 可选 agent.steer() 注入引导消息
```

### 2.2 Goal System（目标系统）—— 会话内持久目标 + 多轮续驱动

**源码位置**：`packages/goal/goal/src/*.ts` + `packages/goal/goal-round-driver/src/*.ts`

**核心机制**：
- **持久状态机**：`GoalPhase = active | paused | blocked | complete`，每次变更通过 `goal/change` SessionEvent 记录（Compare-and-Set revision）
- **Round 续驱动**：`goal-round-driver` 监听 `agent/turn-stopping`，当 goal 处于 active 且未达 `maxGoalRounds` 上限时，自动注入 `<goal_round>` 续轮提示驱动下一轮
- **策略钩子**：报告阻塞（blocked）前可配置策略检查、完成前需验证证据
- **配套工具**：`tool-goal` 暴露 `goal_set`、`goal_edit`、`goal_pause`、`goal_resume`、`goal_mark_complete`、`goal_report_blocker` 等模型可调用工具

**Go 复刻要点**：
```go
// 核心数据结构
type GoalPhase string
const (
    GoalActive   GoalPhase = "active"
    GoalPaused   GoalPhase = "paused"
    GoalBlocked  GoalPhase = "blocked"
    GoalComplete GoalPhase = "complete"
)

type GoalView struct {
    ID             GoalID
    Revision       int
    Objective      string
    Phase          GoalPhase
    BlockedReason  *GoalBlockReason
    MaxGoalRounds  int
    RoundsStarted  int // 从 admitted user/message 派生
    Activation     GoalActivation // 进程内续轮权限
}

// 关键实现点：
// 1. goal/change 事件的严格 fold 回放 + CAS 修订版控制
// 2. goal-round-driver: turn-stopping 时 goal.active → 注入 renderGoalRoundPrompt()
// 3. tool-goal: 6 个工具的参数校验 + CAS 原子写入 session log
```

### 2.3 Todo System（待办系统）—— 轻量进度追踪

**源码位置**：`packages/todo/tool-todo/src/types.ts`

**核心机制**：
- **整体替换列表**：`todo/write` SessionEvent 携带完整 `TodoItem[]`（last-write-wins，无需稳定 ID）
- **三态状态**：`pending | in_progress | completed`
- **模型消费**：通过 `todo_write` 工具一次性替换全表

**Go 复刻要点**：极简实现，定义事件 + 工具即可。

### 2.4 Skill System（技能系统）—— 指令级知识注入

**源码位置**：`packages/skill/skill/src/index.ts` + `packages/skill/skill-filesystem/src/index.ts`

**核心机制**：
- **分层 Provider 注册表**：host 全局层 + scope 作用域链（nearest scope wins）；同层内 rank → provider 顺序 → local 顺序
- **多来源发现**：
  | Rank | 来源 | 根目录 |
  |------|------|--------|
  | 100 | project-dsh | `<projectRoot>/.dsh/skills` |
  | 200 | project-agents | `<projectRoot>/.agents/skills` |
  | 300 | custom | Config.customSkillDirs |
  | 400 | user-dsh | `<dshHome>/skills` |
  | 500 | user-agents | `<agentsHome>/skills` |
  | 600 | bundled | Config.bundledSkillDir |
- **技能格式**：kebab-case 名称，目录 bundle（`<name>/SKILL.md`）或扁平文件（`<name>.md`），含 frontmatter 元数据
- **运行时目录监听**：Chokidar 监听新增/删除/修改，同步失效缓存
- **消费端**：`tool-skill` 暴露 `skill({ name })` 工具 → 找到 summary → 加载完整定义 → 返回 `<skill_content>` / `<skill_resources>` / `<skill_instructions>`

**Go 复刻要点**：
```go
// 核心接口
type SkillProvider interface {
    Name() string
    List(ctx context.Context, opts SkillLookupOptions) ([]SkillCandidate, error)
    Get(ctx context.Context, candidate SkillCandidate, opts SkillLookupOptions) (*SkillDefinition, error)
}

type SkillRegistry interface {
    RegisterProvider(create func(ctrl SkillProviderControl) SkillProvider) func()
    Register(skill SkillRegistration) func()
    List(opts SkillViewOptions) ([]SkillSummary, error)
    Get(name string, opts SkillViewOptions) (*SkillDefinition, error)
}

// 关键实现点：
// 1. Scope 分层：host 层 + scope 链合并（最近层同名单直接覆盖，同层用 rank）
// 2. Local Provider: 6 个根目录扫描 + fsnotify 监听（Go 用 github.com/fsnotify/fsnotify）
// 3. 技能 catalog 摘要注入：agent/pre-step 时 diff 检测变更，用 agent.inject() 写入新的 <available_skills>
```

---

## 三、技术可行性评估

### 3.1 总体结论：✅ **技术上完全可行，建议分阶段落地**

| 维度 | 评分 | 说明 |
|------|------|------|
| 核心算法复刻难度 | ⭐⭐⭐ (中) | Agent 规划能力本质是 Prompt 工程 + 状态机，无算法壁垒 |
| 类型系统迁移成本 | ⭐⭐⭐⭐ (较高) | TS 的声明合并、条件类型、 branded ID 需用 Go 惯用法替代 |
| 插件框架替代 | ⭐⭐⭐⭐ (较高) | Cordis 的 Context/Service/Fiber/Waterfall 需重写或简化 |
| Go 生态支撑度 | ⭐⭐⭐⭐⭐ (充分) | LLM SDK、JSON Schema、SQLite、fsnotify、OTel 等全部成熟 |
| 性能预期 | ⭐⭐⭐⭐⭐ (优于原版) | Go 单二进制 + goroutine，省去 Node.js 事件循环开销 |
| 部署便利性 | ⭐⭐⭐⭐⭐ (优于原版) | 单二进制分发，无需 Node.js 运行时 |

### 3.2 技术挑战与对应方案

#### 挑战 1：Cordis 插件框架替代 ⚠️ 高风险

**原版特点**：
- `Context` 同时承载 Service 注册、事件总线、Fiber 生命周期树
- Waterfall 事件（监听器调用 `next()` 委托）：`agent/pre-step`、`agent/request`、`tools/pre-execute`、`tools/execute`、`tools/post-execute`
- Scope 作用域：`scopeOf(ctx)` 决定注册落在哪一层，支持 per-agent preset 隔离

**Go 方案**：

方案 A（推荐，MVP 阶段）：**简化为显式接口组合 + 中间件链**
```go
// pkg/core/context.go —— 简化的 Context（不做完整插件框架）
type HarnessContext struct {
    mu        sync.RWMutex
    services  map[string]interface{}
    events    *EventBus
    lifecycle *LifecycleManager
    parent    *HarnessContext // 作用域链
}

// Waterfall 用中间件链表达（Gin/Echo 风格）
type PreStepHandler func(ctx context.Context, req *PreStepRequest, next PreStepNext) (*PreStepDecision, error)
type PreStepNext func() (*PreStepDecision, error)
```

方案 B（后期可选）：**引入 uber-go/fx + 自建事件总线**
- fx 提供生命周期管理 + 依赖注入
- 事件总线 + 中间件链保持扩展性

#### 挑战 2：TypeScript 高级类型映射 → Go

| TS 特性 | 原版用途 | Go 替代方案 |
|---------|---------|------------|
| `declare module` 声明合并 | 扩展 `SessionEventMap`、`ContentBlockMap` 等 union | `interface{}` + `switch type`，事件名用字符串常量，schema 用注册中心 |
| `Branded<B>` Branded IDs | `SessionId` / `ToolCallId` 编译时不互通 | `type SessionID struct{ ID string }` + `type ToolCallID struct{ ID string }` wrapper |
| `zod` / `schemastery` 运行时校验 | Tool 参数、frontmatter 解析 | `github.com/invopop/jsonschema` + `github.com/santhosh-tekuri/jsonschema/v5`，或 `github.com/go-playground/validator/v10` |
| `type Thing = ThingMap[keyof ThingMap]` 派生 union | 事件/内容块/结束原因 | Go 用 `interface{}` + 常量 tag，`switch event.Type` 模式匹配 |

#### 挑战 3：Session 事件溯源 + 派生投影

**原版机制**：
- `SessionEvent` 是 discriminated union，`append()` 强类型校验
- `deriveMessages()`、`foldPlanMode()`、`foldRequestHeader()` 均为纯函数从事件前缀派生
- Invariant 伴生对象：每个事件写入时做结构约束校验

**Go 方案**：
```go
// pkg/session/event.go
type EventType string
const (
    EventTurnStart     EventType = "turn/start"
    EventTurnEnd       EventType = "turn/end"
    EventStepStart     EventType = "step/start"
    EventStepEnd       EventType = "step/end"
    EventUserMessage   EventType = "user/message"
    EventAssistantMsg  EventType = "assistant/message"
    EventToolCall      EventType = "tool/call"
    EventToolResult    EventType = "tool/result"
    EventRequestHeader EventType = "request/header"
    EventPlanMode      EventType = "plan/mode"
    EventGoalChange    EventType = "goal/change"
    EventTodoWrite     EventType = "todo/write"
    // ... 可扩展
)

type SessionEvent struct {
    Seq  int         `json:"seq"`
    Time int64       `json:"time"` // epoch ms
    Type EventType   `json:"type"`
    Data interface{} `json:"data"` // 具体 payload: TurnStartData, UserMessage, ...
    SourceEventSeqs []int `json:"sourceEventSeqs,omitempty"`
    SurfaceOp       *SurfaceOp `json:"surfaceOp,omitempty"`
}

// 派生投影（纯函数）
func DeriveMessages(events []SessionEvent) []llm.Message { /* ... */ }
func FoldPlanMode(events []SessionEvent) bool { /* ... */ }
func FoldRequestHeader(events []SessionEvent) *EpochHeader { /* ... */ }
```

#### 挑战 4：LLM 流式处理 + Provider 抽象

**原版机制**：
- `llm/stream` 返回 `StreamChunk` 流，逐块产出 `text`、`reasoning`、`tool-call`
- 每个 Provider（deepseek-official 等）为一个插件，注册适配器到 `ctx.llm`

**Go 生态现状（成熟可用）**：
| 能力 | 推荐库 |
|------|--------|
| SSE 流式调用 | `net/http` + `github.com/tmaxmax/go-sse`（或直接 hand-write） |
| DeepSeek 官方兼容 | 直接手写 REST 客户端（SSE + 函数调用），DeepSeek 的 API 是 OpenAI 兼容的 |
| OpenAI 兼容协议 | `github.com/sashabaranov/go-openai`（v2 支持 streaming + tools） |
| JSON Schema 生成 | `github.com/invopop/jsonschema` |

#### 挑战 5：Waterfall 事件模型 + Cancel 传播

**原版做法**：
- `agent/pre-step` 是 Waterfall：监听器可 `await next()` 获取下游结果，再决定修改或返回
- `AbortSignal` 贯穿所有异步路径：`signal.throwIfAborted()` + `addEventListener('abort')`

**Go 做法**：
- `context.Context` 就是原生的 Cancel 传播通道（无需额外实现）
- Waterfall 可实现为 Handler Chain（类似 `http.Handler` / `gin.HandlerFunc`）：

```go
type PreStepChain []PreStepHandler

func (c PreStepChain) Execute(ctx context.Context, req *PreStepRequest) (*PreStepDecision, error) {
    if len(c) == 0 {
        return &PreStepDecision{Kind: DecisionEnter, Messages: req.Messages}, nil
    }
    handler := c[0]
    rest := c[1:]
    return handler(ctx, req, func() (*PreStepDecision, error) {
        return rest.Execute(ctx, req)
    })
}
```

### 3.3 Go 生态对标清单（已验证可用）

| DSH 原版能力 | Node 依赖 | Go 对标库 |
|-------------|----------|----------|
| HTTP + SSE 流式 | Node http | `net/http` + SSE 手写 / `go-sse` |
| LLM 调用 + Tools | 自研适配器 | `sashabaranov/go-openai` / 自研 |
| JSON Schema 校验 | zod / schemastery | `santhosh-tekuri/jsonschema/v5` + `invopop/jsonschema` |
| 结构化日志 | cordis logger | `slog`（标准库）/ `zap` / `zerolog` |
| 可观测性 (OTel) | @opentelemetry/* | `go.opentelemetry.io/otel/*` 全家桶 |
| 文件系统监听 | chokidar | `github.com/fsnotify/fsnotify` + `github.com/radovskyb/watcher` |
| SQLite 持久化 | better-sqlite3 | `modernc.org/sqlite`（纯 Go，无需 CGO）|
| YAML 配置解析 | js-yaml | `gopkg.in/yaml.v3` |
| 依赖注入 / 生命周期 | Cordis Fiber | `go.uber.org/fx` 或显式组合 |
| 单元测试 | Vitest | `testing` 标准库 + `github.com/stretchr/testify` |
| 子进程管理 | node:child_process | `os/exec` + `github.com/creack/pty` |
| Frontmatter 解析 | gray-matter | `github.com/adrg/frontmatter` |

---

## 四、建议的分阶段落地路线图

### 阶段 0：项目脚手架（1 周）🔜

**目标**：建立可编译的 Go 模块骨架 + 最小可运行 demo

1. **项目结构初始化**（详见第五节）
2. **基础依赖安装**：`go.mod` + CI（GitHub Actions）
3. **LLM 客户端 POC**：实现 DeepSeek 流式调用 + function calling 完整闭环（不接 Agent Loop）
4. **交付物**：`cmd/demo-minimal/main.go` 能跑通 "回答问题 + 调一个本地工具"

### 阶段 1：Core Spine 核心骨架（3-4 周）

**目标**：事件溯源 + Agent Loop + 工具管线的最小可运行闭环

| 子任务 | 包 | 关键交付 |
|--------|----|---------|
| Branded IDs + 基础类型 | `pkg/util/brand` | SessionID、ToolCallID、Branded String wrapper |
| LLM 词汇表 + Provider 接口 | `pkg/llm` | ContentBlock、Message、ToolSchema、LLMService 接口 + DeepSeek 实现 |
| Session 事件溯源 | `pkg/session` | Session 结构体、Event 类型集、append、DeriveMessages()、fold 投影函数 |
| System Prompt 组装器 | `pkg/sysprompt` | Section 注册表（order 排序）、工具 Schema 注入 |
| Tools Pipeline | `pkg/tools` | ToolDefinition、Registry、Waterfall 执行链 (pre/execute/post)、defineTool DSL |
| Agent Loop | `pkg/agentloop` | ReactLoopAgent: Inbox、Turn/Step 循环、pre-step 中间件、status 状态机 |
| Agent Registry | `pkg/agent` | Agent 接口、create/resume、register/list/get |
| Scope 简化版 | `pkg/scope` | scope key + 分层注册基础（供 tools/skills registry 使用） |

**验收标准**：
```bash
# 能跑通完整 Turn：用户输入 → 模型回复 → 工具调用 → 结果回传 → 下一轮模型回复
go run ./cmd/demo-core-spine --prompt "请列出当前目录文件并总结"
```

### 阶段 2：Agent 规划能力（目标能力——复刻 DeepSeek 等价 Agent）（3-4 周）🎯

| 子任务 | 包 | 关键交付 |
|--------|----|---------|
| **Plan Mode** | `pkg/plan` | PlanModeController、plan:policy prompt section、exit_plan_mode 工具、/plan 命令 |
| **Goal System** | `pkg/goal` | GoalService（CAS 写入 + fold）、Goal 工具集、goal-round-driver（自动续轮） |
| **Todo System** | `pkg/todo` | TodoWrite 事件 + todo_write 工具 |
| **Skills System** | `pkg/skill` | SkillRegistry（分层）、SkillProvider 接口、LocalFilesystemProvider（6 根目录 + fsnotify）、skill 工具 |

**验收标准（你的核心目标）**：
```go
// 你的 Go 服务中直接使用
import "github.com/your-org/dsh-go/pkg/sdk"

func main() {
    agent, _ := sdk.NewAgent(sdk.Config{
        Provider:     "deepseek-official",
        Model:        "deepseek-v4-flash",
        Reasoning:    "max",
        MaxTokens:    49152,
        EnablePlanMode: true,     // ← DeepSeek 的规划能力
        SkillsDirs:   []string{"./.dsh/skills"},
    })

    // 开启 Plan Mode，让模型先规划再执行
    agent.SetPlanMode(true)
    result, _ := agent.Run(ctx, "基于当前项目实现用户登录接口，包括ORM模型、路由、测试")
    // ↑ 等价于在 DeepSeek CLI 中执行 /plan + 提问，无需界面
}
```

### 阶段 3：能力扩展（2-3 周）🟢

| 子任务 | 包 | 说明 |
|--------|----|------|
| 持久化（JSONL + SQLite） | `pkg/persistence` | SessionPersistence 接口、checkpoint、crash recovery |
| 上下文压缩 Compaction | `pkg/compaction` | compactIfNeeded（pressure 触发）、LLM 摘要 + 表面替换 |
| Subagent（进程内 spawn） | `pkg/subagent` | SubagentProvider 接口 + in-process spawn 实现、start() 一次性运行 |
| Credentials 管理 | `pkg/credentials` | 密钥配置 + 环境变量 + 加密存储 |

### 阶段 4：生产化（持续迭代）

- **Go SDK**：`pkg/sdk` 稳定 API，对标 DSH Python SDK（RunResult、notifications、subagent）
- **HTTP Server / gRPC**：可选，暴露给其他微服务调用 Agent 能力
- **Telemetry**：集成 OpenTelemetry（traces + metrics + logs）
- **Benchmark**：对比原版 DSH 的 token 吞吐、延迟、内存占用

---

## 五、推荐项目结构

```
d:\workspace\typescript_workspace\dsh-go\
├── go.mod                                  # module: github.com/your-org/dsh-go (或本地私有路径)
├── go.sum
├── README.md                               # ← 本文档（**唯一**根目录主文档）
├── docs/                                   # 详细文档目录
│   ├── TASKS.md                            # ← 任务表主入口（人类可读）
│   ├── tasks.json                          # ← 任务表机器可读（程序化）
│   ├── trace.md                            # ← 对话历史记录
│   └── CACHE_HIT_RATE_PLAN.md              # ← 缓存命中率对齐详细实施计划
│
├── cmd/                                    # 可执行程序入口
│   ├── dsh/                                # CLI 主程序（对标 dsh 命令）
│   │   └── main.go
│   ├── demo-minimal/                       # 阶段 0 demo
│   │   └── main.go
│   ├── demo-core-spine/                    # 阶段 1 demo
│   │   └── main.go
│   └── demo-planning/                      # 阶段 2 demo（你的目标能力）
│       └── main.go
│
├── pkg/                                    # 可复用库代码（对外公开 API）
│   ├── util/                               # 通用工具
│   │   ├── brand/                          # Branded ID 类型封装
│   │   │   ├── brand.go
│   │   │   └── brand_test.go
│   │   └── jsonext/                        # JSON 扩展（lossless JSON 值校验）
│   │
│   ├── llm/                                # LLM 适配层（对应 packages/llm/llm）
│   │   ├── types.go                        # ContentBlock、Message、ToolSchema、TokenUsage
│   │   ├── service.go                      # LLMService 接口 + 注册表
│   │   ├── stream.go                       # StreamChunk 流处理
│   │   ├── retry.go                        # 重试策略 + LlmFailure
│   │   └── provider/
│   │       └── deepseek/                   # DeepSeek 官方兼容实现
│   │           ├── client.go               # REST + SSE 客户端
│   │           └── provider.go
│   │
│   ├── session/                            # 会话层（对应 packages/core/session）
│   │   ├── event.go                        # SessionEvent + 全部 EventType
│   │   ├── event_data.go                   # 各 Event 的 Data 结构体
│   │   ├── session.go                      # Session 结构体 + append
│   │   ├── derive.go                       # deriveMessages + 各种 fold* 投影
│   │   ├── inbox.go                        # Inbox 双队列（next-turn + next-step）
│   │   └── invariant.go                    # append 时的结构不变量校验
│   │
│   ├── sysprompt/                          # 系统提示组装（对应 packages/core/system-prompt）
│   │   ├── section.go                      # Section 接口 + 注册表（带 order）
│   │   ├── assembler.go                    # Assemble() → 渲染完整 system prompt + tool schemas
│   │   └── sections/
│   │       ├── persona.go                  # 角色设定
│   │       └── policy.go                   # 通用 policy
│   │
│   ├── tools/                              # 工具管线（对应 packages/core/tools）
│   │   ├── types.go                        # ToolDefinition、ToolOutput、execution 类型
│   │   ├── schema.go                       # ValueSchemaSpec DSL → JSON Schema 编译
│   │   ├── define.go                       # DefineTool() 类型安全构造器
│   │   ├── registry.go                     # ToolRegistry（分层 scope）+ Restriction
│   │   ├── pipeline.go                     # Waterfall 执行: pre → execute → post → result
│   │   └── builtin/                        # 内置工具集合
│   │       ├── bash.go                     # bash 执行（可选）
│   │       └── noop.go
│   │
│   ├── agent/                              # Agent 抽象（对应 packages/core/agent）
│   │   ├── types.go                        # Agent 接口、AgentStatus、AgentOptions
│   │   ├── inbox_events.go                 # agent/inbox/* 事件
│   │   ├── registry.go                     # AgentRegistry + create/resume
│   │   └── runtime_types.go                # agent/* 全部事件（pre-step / request / error ...）
│   │
│   ├── scope/                              # 作用域原语（对应 packages/core/scope）
│   │   └── scope.go                        # ScopeKey、createScope、scopeOf
│   │
│   ├── agentloop/                          # Agent 循环（对应 packages/core/agent-loop）
│   │   ├── react_loop_agent.go             # ReactLoopAgent 实现（核心驱动）
│   │   ├── turn.go                         # turn 生命周期
│   │   ├── step.go                         # step 生命周期 + LLM 调用 + tool 分发
│   │   ├── tool_calls.go                   # 批量 tool call 执行
│   │   └── constants.go                    # 默认常量（MAX_PARALLEL_TOOL_CALLS 等）
│   │
│   ├── plan/                               # 计划模式（对应 packages/plan/plan-mode）🎯
│   │   ├── controller.go                   # PlanModeController: get/set
│   │   ├── fold.go                         # foldPlanMode 从 session log 派生
│   │   ├── prompt_section.go               # plan:policy prompt section 注册
│   │   └── exit_tool.go                    # exit_plan_mode 工具定义 + 审批流
│   │
│   ├── goal/                               # 目标系统（对应 packages/goal/*）🎯
│   │   ├── types.go                        # GoalPhase、GoalView、GoalBlockReason
│   │   ├── service.go                      # GoalService: get/create/edit/pause/resume
│   │   ├── fold.go                         # goal/change 事件 fold
│   │   ├── round_driver.go                 # goal-round-driver: turn-stopping 续轮
│   │   ├── prompt.go                       # renderGoalRoundPrompt()
│   │   └── tools/                          # goal_set / goal_edit / ... 工具集
│   │
│   ├── todo/                               # 待办系统（对应 packages/todo/tool-todo）
│   │   ├── types.go                        # TodoItem
│   │   ├── event.go                        # todo/write 事件
│   │   └── tool.go                         # todo_write 工具
│   │
│   ├── skill/                              # 技能系统（对应 packages/skill/*）
│   │   ├── types.go                        # SkillSummary、SkillCandidate、SkillDefinition、Provider
│   │   ├── registry.go                     # SkillRegistry（分层 + rank + 缓存）
│   │   ├── provider_fs.go                  # LocalFilesystemProvider（6 根目录 + fsnotify）
│   │   └── tool.go                         # skill() 工具 + catalog 变更检测 + inject
│   │
│   ├── compaction/                         # 上下文压缩（对应 packages/compaction/*）
│   │   ├── types.go
│   │   ├── engine.go                       # CompactionEngine 抽象接口
│   │   └── basic/                          # Basic 实现：pressure 触发 + LLM 摘要
│   │
│   ├── subagent/                           # 子代理（对应 packages/subagent/*）
│   │   ├── types.go                        # SubagentStartRequest、SubagentProvider 接口
│   │   ├── runtime.go                      # SubagentRuntime: start/followup
│   │   └── provider/
│   │       └── inprocess.go                # 进程内 spawn 后端
│   │
│   └── persistence/                        # 持久化（对应 packages/storage/*）
│       ├── interface.go                    # SessionPersistence 接口
│       ├── jsonl/                          # JSONL 文件后端
│       └── sqlite/                         # SQLite 后端（modernc.org/sqlite）
│
├── internal/                               # 内部实现（不对外 import）
│   ├── harnessctx/                         # 简化的 Harness Context（替代 Cordis）
│   │   ├── context.go                      # HarnessContext + services 注册表
│   │   ├── events.go                       # EventBus: emit + waterfall + listeners
│   │   └── lifecycle.go                    # 生命周期管理 (start/stop/dispose)
│   └── testutil/                           # 测试辅助工具
│       ├── mock_llm.go                     # Mock LLM Provider（无需真实 API Key）
│       └── fixture.go                      # 常用测试 fixture 构造
│
├── sdk/                                    # 对外 SDK（对标 Python SDK）
│   ├── client.go                           # HarnessClient: 主入口
│   ├── session.go                          # Session: run/followup/steer/inject
│   └── types.go                            # RunResult、Notification 等
│
└── tests/                                  # 集成测试 / E2E 测试
    ├── core_spine_test.go                  # 阶段 1 完整闭环测试
    ├── planning_capability_test.go         # 阶段 2 规划能力测试
    └── fixtures/
```

---

## 六、风险清单与缓解策略

| 风险 | 影响 | 概率 | 缓解策略 |
|------|------|------|---------|
| **Cordis 插件机制理解偏差** | 高 | 中 | 不要追求 100% 复刻，MVP 阶段直接用显式组合；保留接口扩展性，后期必要时再引入 fx |
| **LLM Provider 兼容性问题** | 中 | 中 | 先以 DeepSeek 官方（OpenAI 兼容协议）为唯一后端，接口设计保留 Provider 抽象，后续扩展其他模型 |
| **TypeScript 声明合并导致漏事件** | 中 | 低 | 建一个 `EventRegistry`，所有新增事件类型必须显式调用 `RegisterEventType()` 登记；invariant 校验未知事件 |
| **fsnotify 跨平台差异** | 低 | 中 | Windows 上使用 watcher 的 polling fallback；路径处理用 `filepath.ToSlash()` 统一 |
| **modernc.org/sqlite 性能** | 低 | 低 | 默认提供 JSONL backend（零依赖），SQLite 作为选项；对比 benchmark 后再决策默认值 |
| **Prompt 模板效果不如原版** | 高 | 中 | 严格拷贝 DSH 的 plan:policy section、goal_round prompt、persona 模板文本；通过回归测试对比输出质量 |
| **Waterfall 中间件顺序 bug** | 中 | 中 | 每个 waterfall 链的 handler 注册顺序写死在代码中并加单元测试；不要动态顺序，避免非确定性 |

---

## 七、与现有 Go 项目的集成方式（你的核心诉求）

假设你已有一个 Go 后端服务（Gin / Kratos / Kitex 等），集成 dsh-go 有两种模式：

### 模式 A（推荐）：进程内库直接调用 ⭐⭐⭐⭐⭐

```go
// 你的 service 层
type AICodeService struct {
    harness *dshsdk.Agent
}

func NewAICodeService(cfg Config) *AICodeService {
    agent, _ := dshsdk.NewAgent(dshsdk.Config{
        Provider:       "deepseek-official",
        Model:          "deepseek-v4-flash",
        APIKey:         cfg.DeepSeekKey,
        SkillsDirs:     []string{"./.dsh/skills"},
        EnablePlanMode: true,   // 关键：开启等价 DeepSeek 的规划能力
        Tools: []dshsdk.Tool{
            // 挂载你的业务工具（ORM 操作、数据库查询等）
            mytools.CreateUserModel(),
            mytools.ApplyDBMigration(),
            mytools.GenerateGinRoutes(),
            mytools.RunGoTests(),
        },
    })
    return &AICodeService{harness: agent}
}

// HTTP handler 调用
func (s *AICodeService) GenerateCode(c *gin.Context) {
    var req GenerateCodeRequest
    c.BindJSON(&req)
    result, err := s.harness.Run(c.Request.Context(), req.Prompt)
    if err != nil { /* ... */ }
    c.JSON(200, gin.H{
        "final_response": result.FinalResponse,
        "finish_reason":  result.FinishReason,
        "steps":          result.Steps, // 规划的步骤和工具调用记录
    })
}
```

**优势**：零网络开销、工具直接访问你的业务 ORM/数据库、无需前端界面。

### 模式 B：独立 dsh-go 服务 + gRPC / HTTP API ⭐⭐⭐

```text
┌─────────────┐   gRPC/HTTP    ┌───────────────┐
│ 你的 Go 服务 │◄──────────────►│  dsh-go Server │
│  (Gin/Kratos)│                │ (Agent 驻留)   │
└─────────────┘                └───────┬───────┘
                                       │ 调用业务工具
                                       ▼
                               ┌────────────────┐
                               │ 你的业务数据库  │
                               └────────────────┘
```

适用场景：多个微服务共享同一个 Agent 池、需要独立扩缩容。

---

## 八、工作量估算

| 阶段 | 人周（1 名熟练 Go 开发） | 关键里程碑 |
|------|----------------------|-----------|
| 阶段 0：脚手架 + LLM POC | 0.5 ~ 1 | demo-minimal 可跑 |
| 阶段 1：Core Spine | 3 ~ 4 | demo-core-spine 可跑通 Turn 全闭环 |
| 阶段 2：规划能力（目标） | 3 ~ 4 | demo-planning 可跑 Plan Mode + Goal + Skills |
| 阶段 3：扩展能力 | 2 ~ 3 | 持久化 + Compaction + Subagent |
| 阶段 4：生产化 | 持续迭代 | SDK 稳定、Telemetry、Benchmark |
| **合计（MVP 阶段 0-2）** | **6.5 ~ 9 人周** | **你的 Go 服务可直接使用等价的 DeepSeek Agent 规划能力** 🎯 |

---

## 九、总结

**✅ 可行性结论**：基于 Go 语言复刻 DeepSeek Harness 的 Agent 规划能力**完全可行**，且具有以下优势：

1. **无核心算法障碍**：Planning/Goal/Todo/Skill 本质是 Prompt 工程 + 状态机 + 事件溯源，全部可在 Go 中复现
2. **Go 生态完全覆盖**：LLM SDK、JSON Schema、SQLite、fsnotify、OTel 等依赖全部成熟稳定
3. **性能 & 部署优势**：单二进制分发、无需 Node.js runtime、goroutine 流式处理更高效
4. **直接嵌入业务**：你的 Go 服务可作为库进程内调用，业务工具（ORM/DB/缓存）直接作为 Tool 注入，无需网络中转

**⚠️ 关键注意事项**：
- **不要追求完美复刻 Cordis**：用显式组合 + 中间件链（Waterfall → Go 的 Handler 链）代替，避免掉进框架工程化陷阱
- **严格遵循原版 Prompt 模板**：规划能力的效果高度依赖 prompt 文本质量，直接拷贝原版 section 内容可最大程度保证等价性
- **一次性复刻到位**：已通过二次扫描确认 60+ 子系统的耦合关系，簇内能力不可分割，必须整体到位才能避免后续返工

---

## 十、一次性复刻到位的完整能力清单（v2.0 · 不分步骤）🎯

> **用户决策**：不分阶段，一次性复刻所有「缺了就不能称为等价 DeepSeek Agent」的核心能力。
> 以下清单基于对原版 deepseek-harness **60+ 子系统文档**的完整扫描与耦合关系分析得出。

### 10.1 能力分级定义

| 级别 | 符号 | 定义 | 是否一次性到位 |
|------|------|------|--------------|
| **MUST（必复）** | 🔴 | 缺失则无法称为等价 DeepSeek Agent，或导致某个核心能力簇整体失效 | ✅ 必须一次性到位 |
| **SHOULD（强烈建议）** | 🟡 | 缺失不会立刻让 Agent 停下，但会导致用户体验大幅下降、或在长对话/生产化场景中很快踩坑 | ✅ 建议一次性到位 |
| **COULD（可选）** | 🟢 | 锦上添花，不影响 Agent 内核能力等价性，可后续再加 | ❌ 可延后 |
| **SKIP（跳过）** | ⚫ | 纯 UI 交互、纯产品展示、或特定平台强绑定（非 Agent 内核） | ❌ 第一阶段跳过 |

---

### 10.2 不可分割的 5 大核心能力簇（簇内强耦合 → 必须一次性全做）

#### 🔴 簇 1：Agent 内核驱动簇 ｜ 8 项 MUST
> **耦合关系**：Agent Loop 驱动 Turn/Step → 依赖 Session 记录日志、派生消息 → 依赖 LLM 调模型、Tools 执行工具 → 依赖 System Prompt 组装 Section、Schema → 依赖 Scope 分层注册表。**缺任何一项，整个 Agent 连一步都动不了。**

| # | 能力名 | 原版包 | 复刻要点 | 为什么 MUST |
|---|--------|-------|---------|------------|
| 1.1 | **Branded ID 类型封装** | `packages/util/*` | Brand/Bytes 两类 branded 结构体（SessionID、ToolCallID、ApprovalRequestId、JobId 等），避免字符串 ID 混传 | 所有能力接缝的契约都依赖 branded ID，是"基础设施的基础设施" |
| 1.2 | **Session 事件溯源 + Header** | `packages/core/session` | Session 结构体 + 全部 Event 类型（30+ 种，见 10.3.1 完整枚举）+ SessionHeader（version/cwd/parentSession/seedLength/origin/delegationDepth/agentPreset）+ append() 严格不变量校验 + deriveMessages() 派生投影 + fold* 投影函数族（foldPlanMode/foldRequestHeader/foldEffectiveSandboxMode/foldEffectiveApprovalPolicy/foldGoalChange/foldTodoWrite/foldPermissionPreset 等） | 所有状态从日志派生；没有等价事件词汇表 = 无法 resume、无法 fork、无法通过 CAS 控制并发写入 |
| 1.3 | **LLM Provider 接缝 + 流式协议** | `packages/llm/llm` + provider | LLMService 抽象接口 + StreamChunk 词汇表（text/reasoning/tool-call）+ ContentBlock 多模态（text/tool_use/tool_result/image）+ Message 角色（user/assistant/system）+ ToolSchema（name/description/inputSchema/outputSchema）+ 重试策略 + LlmFailure（overload/rate-limit/response-refusal/context-overflow 等）+ DeepSeek 官方实现（REST + SSE） | 没有 LLM Provider = Agent 没有大脑；流式协议不完整 = 无法支持长 token 流式生成和 function calling |
| 1.4 | **System Prompt 组装器 + Section 注册** | `packages/core/system-prompt` | Prompt Section 注册表（带 order，排序决定注入顺序）+ 核心 Section：persona（角色设定）+ policy（通用 policy）+ runtime-context-snapshot + tool schemas 注入 | System Prompt 是 DeepSeek "表现得像 DeepSeek" 的灵魂；Section 顺序错误 = 模型行为漂移 |
| 1.5 | **Tools Pipeline（Waterfall）** | `packages/core/tools` | ToolDefinition（name/schema/execute/output/finalizeContent/isConcurrencySafe）+ DefineTool() 类型安全构造器 + ToolRegistry（分层 Scope）+ **四级 Waterfall 执行链**：`tools/pre-execute → tools/execute → tools/post-execute → tools/result` + 参数/输出 JSON Schema 校验 + isError 错误通道 + restrictions 工具限制 | 没有完整的 Waterfall = 无法注入 spill 策略、审批策略、沙箱策略、观测策略；这些都是 DeepSeek 等价能力的"运行时行为" |
| 1.6 | **Scope 作用域原语** | `packages/core/scope` | ScopeKey + createScope/scopeOf + 分层注册表合并规则（nearest-scope-wins + rank） | Tools/Skills/Commands/Credentials 所有注册表都依赖 Scope 分层；没有 Scope = 无法按 Agent 隔离能力、无法做全局 vs 局部注册覆盖 |
| 1.7 | **Agent Registry + 生命周期** | `packages/core/agent` | Agent 接口（Run/Followup/Steer/Inject/Dispose）+ AgentStatus（idle/running）+ AgentOptions（session、logger、preset、llmPref 等）+ AgentRegistry：register/list/get + create/resume（从 session id 恢复） | 没有 Registry = 无法管理多个 Agent 实例、无法从持久化会话 resume |
| 1.8 | **Agent Loop（ReactLoop 双循环）** | `packages/core/agent-loop` | **核心驱动**：ReactLoopAgent 实现 + Inbox 双队列（agent/inbox-turn / agent/inbox-step）+ Turn 循环（turn/start → claim → pre-step → 多 Step → turn-stopping → turn/end）+ Step 循环（step/start → 派生 history → agent/request waterfall → llm/stream → assistant/chunk* → message → 若有 tool/call* 执行 → step/end）+ agent/pre-step / agent/request 两级 Waterfall + agent/error 错误处理 + status 状态机 + 单 Turn 串行保证（pending turn rejection） | 这是整个框架的心脏；没有等价的 Turn/Step 双循环 = Goal 续轮无法触发、Plan Mode 无法注入、子 Step 无法合并为一次用户响应 |

---

#### 🔴 簇 2：规划能力簇 ｜ 6 项 MUST + 2 项 SHOULD
> **耦合关系**：Plan Mode 的 exit_plan_mode 工具 → 依赖 User Questions 向用户请求审批 → 依赖 Approval 审批接缝做日志审计。Goal System 的 goal/change CAS 写入 → 依赖 Session Event 词汇表 + Invariant 校验。Goal Round Driver → 订阅 agent/turn-stopping 事件（必须有 Agent Loop 发出）。三者都依赖 System Prompt 注入各自 section。缺一个 = 规划能力不完整。

| # | 能力名 | 原版包 | 复刻要点 | 为什么 MUST |
|---|--------|-------|---------|------------|
| 2.1 | **Plan Mode（计划模式）** | `packages/plan/plan-mode` | PlanModeController（get/set）+ plan/mode SessionEvent + `plan:policy` Prompt Section（order=500，**严格拷贝原版 prompt 文本**）+ exit_plan_mode 工具（参数：plan_markdown → 调用 User Questions 审批：kind=plan-review）+ /plan 命令（on/off + 引导消息 steer） | DeepSeek "先规划再执行" 的招牌功能；没有它 = 你只能拿到一个普通聊天 Agent，不是 DeepSeek 等价 Agent |
| 2.2 | **User Questions（用户问答接缝）** | `packages/interaction/user-questions` | AskUserQuestionItem（id/question/detail/header/options/multiSelect/intent）+ AskUserQuestionIntent（kind=plan-review, approve label）+ AskUserQuestionRequest + AskUserQuestionAnswer（selected/custom）+ UserQuestionError 错误码（EMPTY_QUESTIONS/NO_PROVIDER/ASK_ABORTED 等）+ 在 Go SDK 场景下提供回调式 Answerer（不需要 UI，把问题抛给 SDK 调用方回调） | Plan Mode 审批依赖此接缝；没有回调式 Answerer = exit_plan_mode 永远无法通过审批、卡在 turn 中间 |
| 2.3 | **Goal System（目标系统 + Round Driver）** | `packages/goal/goal` + `goal-round-driver` | GoalPhase（active/paused/blocked/complete）+ goal/change SessionEvent（带 CAS revision）+ GoalView（id/revision/objective/phase/blockedReason/maxGoalRounds/roundsStarted/activation）+ GoalService：create/edit/pause/resume/mark_complete/report_blocker（每次写入严格 CAS 原子写入 session log）+ **Goal Round Driver**：监听 turn-stopping，若 goal.active 且 roundsStarted < maxGoalRounds 则注入 `<goal_round>` 续轮提示，保证下一轮模型继续干活不终止 | 多轮长任务不掉线的关键；没有 Round Driver = 你每次设置 Goal 后，Agent 只跑一轮就停了，根本达不到 "自动续轮推进目标" 的 DeepSeek 等价行为 |
| 2.4 | **Todo System（待办系统）** | `packages/todo/tool-todo` | todo/write SessionEvent（整体替换式 TodoItem[]，last-write-wins）+ TodoWrite 工具（完整列表替换）+ Todo 状态（pending/in_progress/completed） | Goal/Todo/Plan 三件套是 DeepSeek 可视化进度管理的三驾马车；缺 Todo = 你只能看目标+计划文字，无法用三态追踪每个步骤进度 |
| 2.5 | **Approval（用户审批接缝）** | `packages/interaction/user-approval` | ApprovalRequestId（branded）+ ApprovalOutcome 闭合枚举（allowed-once / rejected / cancelled / unavailable，fail-closed：任何非 allowed-once 都拒绝）+ ApprovalPolicy（ask 委托答案链 / never 直接拒绝，CI/unattended 模式）+ 审计事件对（approval/asked → approval/decided，必须成对落在 session log 内）+ setApprovalPolicy(session, policy) 单写路径 | 工具执行前审批（bash、写文件等危险操作）是 DeepSeek "fail-closed 安全默认" 的核心机制；没有它 = 工具能任意执行，不是同一个产品级安全模型 |
| 2.6 | **Commands（人类命令注册）** | `packages/interaction/commands` | CommandDefinition（name/description/input/recordInput/handler）+ CommandInvocation（commandId/agent/rawInput/attachments/signal）+ CommandResult（success text / error text / sourceEventSeq）+ 命令范围：global 全局 + agent-scoped 阴影覆盖 + /plan、/goal、/approval、/permissions、/settings 等命令入口 | /plan 等命令正是 Plan Mode / Goal System 等能力的"非模型消息触发入口"；没有 Commands 系统 = 你只能靠模型调工具内部触发，无法从外部切换状态 |
| **SHOULD 2.7** | **Job Runtime（后台任务统一生命周期）** | `packages/jobs/jobs` | JobKind（bash/subagent 可扩展）+ JobStatus（running/stopping/completed/killed/failed）+ JobStart 声明（kind/label/outputLimitBytes/owner/run → JobHooks）+ JobHooks（cancel 同步幂等 / done / 可选 readOutput 流式消费）+ JobSnapshot 只读投影 + JobRegistry：start/read/wait/kill/list | bash/子进程/子代理都是长任务；若没有统一 Job 管理，会出现：前台 bash 有超时机制但后台子代理无生命周期无取消、无法 report terminal state、owner agent dispose 时无法自动清理子任务等一系列一致性坑 |
| **SHOULD 2.8** | **Runtime Context Snapshot（运行时上下文快照）** | `packages/core/system-prompt` 的 Runtime-Context 机制 | 每次请求前动态快照：当前 plan mode 状态（开/关+pending）、goal 状态（phase + objective 摘要）、approval policy（ask/never + switch notice）、sandbox mode（read-only/workspace-write + switch notice）、permission preset（当前值 + 可切换选项）+ 变化时注入 switch notice（"注意：你现在进入 plan mode，先规划再执行。"） | 原版 DeepSeek 的"每次模型请求都能看到当前所有开关状态"的关键；没有此快照，模型在请求中"看不到"当前 plan/goal/approval 等已经被用户切了，行为就会和原版不一致 |

---

#### 🔴 簇 3：工具执行与安全簇 ｜ 7 项 MUST + 1 项 SHOULD
> **耦合关系**：Filesystem/Shell 工具是 Agent "动手干活" 的手脚，90% 业务落地场景依赖这两个工具。它们各自依赖：Sandbox（文件系统约束）+ Spill（大结果溢出）+ Jobs（bash 后台执行）+ SandboxPolicy（read-only/write 选择）+ Observation Policy（读前校验版本，防并发写丢失）。缺审批 = 工具能乱执行；缺沙箱 = 非安全模型可随意删文件。

| # | 能力名 | 原版包 | 复刻要点 | 为什么 MUST |
|---|--------|-------|---------|------------|
| 3.1 | **Filesystem 接缝（抽象 + 本地实现 + 观测策略 + 工具）** | `packages/fs/fs` + `fs-local` + `fs-observation-policy` + `tool-fs` | **FileSystem 抽象接口**：resolve → FsTarget（opaque targetKey + displayPath）+ stat/lstat（FsVersion 版本令牌）+ listDir + readText/streamText/readBytes（字节上限 + FS_TOO_LARGE 错误码）+ writeText/editText（**可选版本守卫**：createIfAbsent / replaceIfVersion，否则 FS_STALE_VERSION / FS_NOT_OBSERVED）+ delete + **观测策略插件**（默认：write/edit 前先 stat 观察，记录版本，用版本守卫防覆盖；无条件写/编辑只在 bare provider 层存在）+ **本地磁盘实现**（workspace 根 + canonical 路径规范化）+ **文件工具**：`read`/`grep`/`ls`/`glob`/`cat`（按需）/`write`/`edit` / `patch` / `mkdir` / `mv` / `rm`（严格按契约做版本守卫和错误码映射）+ 大文件读取时 retention ceiling（head/tail 截断） | 没有等价文件操作 = Agent 无法读写代码、无法修改项目、只能聊天说空话；特别注意 **观测策略不是可选**：原版默认策略就是读前观察+版本守卫，缺失等于并发写时可能丢失用户在其他编辑器/进程中的改动（DeepSeek 绝不允许） |
| 3.2 | **Shell/Bash 接缝（抽象 + 本地实现 + 沙箱实现 + 工具）** | `packages/shell/shell` + `bash-local` + `bash-sandbox` + `tool-bash` | **Shell 接缝**：ShellExecRequest（command + 可选 workdir/timeoutMs/stdoutMaxBytes/signal/stdin/env/dshEnv/sandboxPolicy）→ resolve() → ShellExecSpec（字段全部补齐并加盖）→ start()/run() → ShellRunResult（exitCode + signal + timedOut + aborted + stdout + stderr + dshEnv 快照）+ 输出字节上限（截断 + spill）+ credential scrub（剥离继承环境中的密钥变量后才合并 env）+ DSH_* 管理命名空间 + **Sandbox 执行路径**（先 confine 成 ConfinedArgv，再 spawn，失败分类：runnerFailureRules vs denialSignatures）+ **bash 工具**：`bash` 一次执行、`bg` 后台执行（job 形式）+ 工作目录从 session cwd 继承 + 超时与取消绑定到 step 上下文 | 没有 bash = Agent 无法跑命令（go build、go test、npm install、git、docker 等），等于没有"动手能力"；沙箱是产品级安全边界：read-only 模式不允许 bash 写文件，缺了就不是同一个默认安全模型 |
| 3.3 | **Sandbox 进程沙箱接缝** | `packages/sandbox/sandbox` + `sandbox-local` + `sandbox-policy` | SandboxMode 三态（read-only / workspace-write / danger-full-access，前两者才真正进入 confine）+ SandboxExecutionPolicy（mode + workspaceRoot + 可选 sessionId）+ resolve() 选模式 + ConfinedArgv（argv + enforcement full/partial + denialSignatures + runnerFailureRules）+ SandboxPolicy 服务：从 session log fold 得到 effective mode（fallback → executor config）+ 会话级策略覆盖（sandbox/mode 事件）+ **每 call 携带策略**（不用 Provider 级固定策略，保证同一 provider 可并发跑不同边界的会话）+ 本地后端：Windows ACL restricted-token（MVP 可退回 danger-full-access + TODO 注释标可替换） | Sandbox Mode 是 Permission Presets 的两大旋钮之一（另一个是 Approval Policy）；没有沙箱接口抽象 = 你后期想加 Linux bwrap/Landlock 时得改 shell/fs 工具源码而不是只换 Provider，接缝不完整 |
| 3.4 | **Permission Presets（权限预设组合）** | `packages/interaction/permission-presets` | PresetSpec：{ sandbox + approval + 可选 name/description } + 默认表：`workspace-write`（workspace-write + ask）vs `danger-full-access`（danger-full-access + never）+ `custom` 派生态（永远不作为切换目标）+ current() fold 派生当前 preset + set(session, name) 原子切换：先记 permission/preset 事件 → 再分别调 setSandboxMode + setApprovalPolicy（仅变化的旋钮才写入）+ names/optionOf 给 SDK 调用方展示可选项 | 原版 DeepSeek 的"权限选择器" UX 就是它；没有 preset 组合 = 你要让调用方同时改两个独立旋钮(sandbox+approval)且保证一致性，等价性直接破 |
| 3.5 | **Spill Storage（工具大结果溢出存储）** | `packages/spill/spill` + `spill-local` + `spill-policy` | SpillStore 单方法接口：saveText(SaveTextSpill{owner, source, suggestedName, content}) → SpillRef{locator（opaque）+ bytes + retrievalHint（告诉模型怎么取）}+ Local Backend：session-scoped 私有目录（0700 session 子目录 + open(path,wx,0o600) 独占写，防 planted symlink）+ locator = 本地绝对路径 + retrievalHint = "请用 read/grep 工具读该路径"+ Spill Policy Consumer（tools/post-execute）：超过 maxInlineBytes 的 plain-text 结果 → retention 做 head/tail 预览 → saveText → 替换内联结果为「预览 + spill 引用」；保存失败 = 保留原内联结果（best-effort，不把成功调用变成错误） | 不做 spill = grep 一个大目录时工具返回 10MB 文本，直接把 context window 爆掉，是生产级致命缺陷；且原版 spill policy 正是 "tools/post-execute waterfall 的一环"，不做就等于 tools pipeline 不完整 |
| 3.6 | **Subprocess 子进程机制（简化版）** | `packages/shell/subprocess`（或 packages/shell/* 内部） | 进程组管理 + timeout/kill/cancel 与 context.Context 绑定 + stderr 拆分 runner failure 与命令自身失败 + DSH_* 环境变量清洗 + Windows/Linux/macOS 三平台 spawn 差异封装 | Shell/Bash 的所有 timeout/cancel/信号/进程组归属最终都落到 subprocess 上；直接裸用 os/exec 会出现：cancel 不杀子进程、超时后进程还活着、父进程退出子进程变孤儿等问题 |
| 3.7 | **Token Meter（Token 计量 + 预算）** | `packages/core/token-meter` / `packages/llm/llm-streaming` | 每次 LLM 请求 + 响应精确计量 token：prompt tokens（按模型分词器估）+ completion tokens（按实际生成）+ tool-call JSON tokens + 按 request 分组累计 + 会话级总 budget 上限（达到上限报错或触发 compact）+ token per step 指标上报给 OTel | 没有 token 计量 = 无法防止一个会话烧完整个预算；原版 DeepSeek 的 max-tokens 行为就是靠它 + request header 注入 |
| **SHOULD 3.8** | **Compaction（上下文压缩）** | `packages/compaction/compaction` + `compaction-engine-basic` | CompactionEngine 抽象接口 + pressure 策略（按剩余 context window 比例判断触发）+ 压缩算法：选择历史消息段 → 用模型生成摘要 → surfaceOp 表面替换（保留事件前缀、替换对应 user/assistant/tool-result 块为压缩摘要 + 附带 "此段已压缩为如下摘要" 提示）+ 保证压缩后总 token < 阈值 | 长对话 30+ 轮后 token 必然超限；没有 Compaction = 只能粗暴截断历史，模型会忘记前面 25 轮说过什么，是 DeepSeek 长程推理能力的核心支撑 |

---

#### 🔴 簇 4：会话持久化 + 可恢复簇 ｜ 3 项 MUST + 1 项 SHOULD
> **耦合关系**：Session Persistence 写入的事件日志必须和 1.2 的 Session 事件词汇表完全一致（同一种结构）。Header 独立于 log 存放在 persistence metadata 层。Crash Recovery 发现孤儿 turn/start 时必须补合成 turn/end{kind:interrupted}，和 Session fold 的平衡校验保持一致。缺持久化 = 进程重启会话全丢，等价性崩塌。

| # | 能力名 | 原版包 | 复刻要点 | 为什么 MUST |
|---|--------|-------|---------|------------|
| 4.1 | **SessionPersistence 抽象接缝** | `packages/session/session-persistence` | SessionPersistence 接口：create(id, CreateSessionOptions{seed, meta}) / prepare(id) → SessionPreparation / load(id) → 已发布 Session / inspect(id) → 只读视图（冷平衡孤儿 turn）/ list(scope) → 会话列表 / append(id, event) → 异步批量 flush + session/flush 同步 checkpoint + locate(meta) → per-session 工件位置 | 抽象接缝定义了"对持久化层的所有合法读写语义"；没有抽象接缝 = 直接硬编码 SQLite/JSONL 操作会违反：locate 只是 hint 不是授权、flush checkpoint 与 agent/turn 同步的强约束、inspect 不修复物理尾部等原版关键契约，后期换后端等于重写整个 Session store 层 |
| 4.2 | **JSONL 持久化后端（零依赖 MVP 默认）** | `packages/storage/jsonl` | **Artifact 格式**：第一行独立 JSON object = SessionHeader（version/id/createdAt/cwd/parentSession/seedLength/origin/delegationDepth/agentPreset）+ 从第 2 行起每行一个 SessionEvent JSON（seq 与 line 对齐）+ 增量 flush（异步批处理窗口：首个待写事件启动窗口，后续事件 join 不重置截止时间，到期一次写入；flush() 立即排空并报告错误）+ Crash Recovery：冷加载时若检测到 orphan turn/start（无 turn/end 闭合）→ 在内存中追加 synthetic turn/end{kind:'interrupted'} 后再返回（物理日志不截断，保留所有已 durable 写入的事件）+ per-session 文件：`<storageRoot>/project/<projectHash>/<sessionId>.jsonl` + SessionFormatUnsupportedError（header.version > SESSION_FORMAT_VERSION 时拒绝，明确升级方向）+ SessionPersistenceCorruptionError（解析失败） | JSONL = 零 CGO 依赖、纯文本、可 grep/debug，是 MVP 阶段最稳妥的默认后端；同时 crash recovery 是会话可恢复的关键：原版不截断 = 即使在一次超大 turn（1000 step）中途崩溃，重启后所有已产生的工具结果/步骤都完整保留，只把那次未闭合的 turn 标记为 interrupted，可 resume 继续 |
| 4.3 | **SQLite 持久化后端（可选生产级）** | `packages/storage/sqlite-session` | modernc.org/sqlite（纯 Go，无 CGO）+ SCHEMA_VERSION pragma + sessions 表（id + header blob）+ events 表（session_id, seq, type, data_json, time, surface_op_json, source_event_seqs_json）+ 原子事务保证一次 batch append 要么全落要么全不落 + `inspect()` 同 JSONL 后端孤儿 turn 平衡逻辑 | 生产环境（百万会话）的查询和 listing 性能远优于 JSONL；虽然 SHOULD 级，但因为 Seam 抽象完整，换后端只换插件，不影响其他代码 |
| **SHOULD 4.4** | **Session Title（会话标题自动生成）** / **Session Query（会话搜索）** | `packages/core/session-title` / `packages/storage/session-query` | Session Title：首轮 turn 结束后异步用 LLM 生成 1 行标题，存到 header sidecar，无需回写事件日志 + Session Query：按 title 关键字 / 创建时间 / 目标关键词 / 最后活跃时间 倒序分页搜索 + 全文索引（SQLite FTS5） | 没有 title = 会话列表全是 session id hash，用户无法辨认；没有 query = 多会话只能分页扫，是可恢复能力的常用配套 |

---

#### 🔴 簇 5：配置 / 技能 / 凭证簇 ｜ 5 项 MUST
> **耦合关系**：Skills System 依赖 Scope 分层注册表 + fsnotify 实时刷新；Credentials 是 Settings 中 secret 字段的真实存储（Settings 本身只存 reference）；Settings 的 namespace/schema 模式保证所有可配置项有类型校验 + 修订版乐观锁 + observe 变更通知。三缺任何一：配置无校验、密钥明文存 settings、技能无法实时发现新文件，都是与原版 DeepSeek 不一致的硬伤。

| # | 能力名 | 原版包 | 复刻要点 | 为什么 MUST |
|---|--------|-------|---------|------------|
| 5.1 | **Skills System（技能注册表 + FS Provider + skill 工具 + 目录监听）** | `packages/skill/skill` + `skill-filesystem` + `tool-skill` | SkillRegistry 分层合并（host 全局 + scope 作用域链，nearest scope wins；同层 rank → provider 顺序 → local 顺序）+ SkillProvider 接口（List/Get）+ LocalFilesystemProvider：**6 层目录扫描**（rank 100~600：project-dsh → project-agents → custom → user-dsh → user-agents → bundled）+ **fsnotify 实时监听**（新增/删除/修改后同步失效缓存，Windows 上 polling fallback）+ 技能格式：kebab-case 名，支持 `<name>/SKILL.md` bundle 或 `<name>.md` flat + frontmatter 元数据（summary 摘要）+ **catalog 变更检测注入**（agent/pre-step 时 diff 摘要，用 agent.inject() 写入新的 `<available_skills>` 区块到会话）+ `skill({name})` 工具：先看 summary → 加载完整定义 → 返回 `<skill_instructions>` / `<skill_resources>` / `<skill_content>`（若没命中再给 model 相近技能名建议） | 技能系统是 DeepSeek "加载 domain 知识就变强"的核心能力；没有 6 层目录结构 = 项目级技能、用户级技能、内置技能无法分层覆盖；没有 fsnotify = 新建/改了一个技能文件要重启进程才生效；没有 catalog 注入 = 模型根本看不到当前可用技能名集合 |
| 5.2 | **Settings（用户设置命名空间）** | `packages/settings/settings` + `settings-file` | SettingsNamespace（kebab-case branded id）+ SettingsRegisterOptions{base, applies, validate} + SettingsScope<T>{get, watch(callback), update(patch), replace(section)} + **分层解析**：schema 默认值 → base（插件声明）→ user（用户层，唯一持久化写入的层）+ **schema 校验**：所有写路径必须先过 schema（失败则保留最后好值 + warn）+ 可选 validate() 跨字段检查 + 修订版乐观锁（revision + expectedRevision 防止覆盖他人写）+ SettingsPathOp（set/unset，供持有红acted descriptor 的调用方做字段级编辑，不破坏 secret）+ describe({redactSecrets:true})：剥离 role('secret') 字段，只保留 {path, set} 空槽位给配置 UI/SDK | 没有 Settings = 每个插件各写各的配置文件：格式各异、无校验、无默认值、无热更新、无 secret 剥离，配置跨进程/跨 SDK 传输时必漏密钥；原版的 namespace 模式就是为了解决这个一致性问题 |
| 5.3 | **Credentials（凭证接缝 + 本地 Provider + Authorization Flow）** | `packages/credentials/credentials` + `credentials-local` + `authorization` | CredentialRef（POSIX 风格 env var name branded id）+ Credential Provider 接缝：resolve(ref) → ResolvedCredential{value, source} 或 undefined（空值=absent everywhere，per-call resolve 不缓存，保证密钥热更新）+ describe(ref) → {configured, source, writable}（绝不暴露值，可跨 Remote 传输）+ set/unset + reference-updated 事件 + **分层解析顺序**（local provider 标准：process env → user-env file → project-env file → credential-managed store）+ AuthorizationService：每个 credential key 一个 Authorization Flow（label + methods + runner），begin/request 异步跑授权、list 枚举、cancel 取消、DUPLICATE_FLOW / NO_FLOW / ALREADY_IN_FLIGHT 错误码 | 没有 Credentials = Settings 的 secret 字段没法存（settings 只存引用），API Key 要么硬编码要么明文写入配置文件，是生产级安全事故；Authorization Flow 则是 "DeepSeek UI 里弹窗引导你粘贴 API Key 或走 OAuth" 的契约，作为库也能暴露同样的 authorize() 方法给业务方集成 |
| 5.4 | **Harness Context（简化替代 Cordis）—— 一次性定型** | 无独立包，自制 | HarnessContext：services 注册表（get/set + 重复注册 panic，保证 seam 单实现约束）+ EventBus（普通 emit + waterfall emit）+ LifecycleManager（start/stop/dispose 有序化 + 错误收集）+ parent 作用域链指针（提供 scope 分层的 parent 关系）+ Error 分类（HarnessError{code, message} 体系，保证错误码可跨 RPC 稳定传递）+ Middleware Chain 定义（PreStepChain / RequestChain / ToolPipelineChain，都是 []Handler + next 模式）+ **严格的 waterfall 顺序常量**（每个链的中间件注册顺序用常量数组写死，不暴露动态 add 给插件，避免原版 cordis 动态顺序带来的非确定 bug） | 这是"一次性到位"最关键的架构决策：不用 Cordis 也不引入 fx（除非后期真的需要），但把 HarnessContext 的形状、事件模型、waterfall 顺序、错误分类**一次定型**；如果第一次做的时候 Context 形状没定义好，后面改所有服务都会受影响，等于大重构，必须一次性到位 |
| 5.5 | **Invariant（运行时不变量校验体系）** | `packages/runtime-diagnostics/invariants` + 每个 package 配套 `./invariant` 伴生插件 | InvariantRegistry：register(packageName, installer) → 全局/allowlist/blocklist 过滤 + 每个 package 的 installer 在独立 fiber 中执行（声明 inject 可访问服务）+ fail(message) → InvariantError{code:INVARIANT, packageName, prefix message} + 作用域 disposer 同步 + **必做的 8 条不变量清单**：(1) Session append 的事件 seq 严格连续、time 单调不减；(2) turn/start ↔ turn/end 严格配对（除非 interrupted 冷合成）；(3) step/start ↔ step/end 严格配对；(4) approval/asked 必须有 approval/decided 成对；(5) goal/change revision CAS 不允许跳号；(6) 工具 call id 与 tool/result id 严格一一对应；(7) Workflow（如果做了）run-start 必须有 run-end；(8) Persistence JSONL/SQLite 同 id 多次 append 事件 seq 与首次加载保持一致。 | 没有 invariant = 你写了一年后某次改动引入了"步骤 A 写入 turn/end 但没配对 turn/start"这样的日志非法状态，几周后才在 resume 时爆炸，根本无法回溯；原版 50+ 包都有 invariant 伴生，是它能做 fail-closed 持久化格式拒绝的根基；**这 8 条必须一次性到位**，不然后期加 invariant 等于要兼容所有已经写出的"历史非法日志"，做不到 |

---

### 10.3 全部 60+ 子系统复刻决策矩阵

#### 10.3.1 MUST 级：42 项（必须一次性到位，二次扫描新增 M40~M42）

| 编号 | 子系统名 | 对应 packages 路径 | 复刻位置 | 一次性到位原因 |
|------|---------|------------------|---------|-------------|
| M01 | Branded IDs / Utilities | `packages/util/*` | `pkg/util/brand` | 所有契约 ID 的基础，前期做错 ID 后面全要改 |
| M02 | Session Event Log + Header + fold 投影 | `packages/core/session` | `pkg/session` | 事件词汇表不完整 = 后期加事件要兼容所有老日志，不可行；**扩展至 45+ Event 类型**（含 compaction/schedule/attachment/feedback/reference 等预留事件），列在下面 |
| M03 | Inbox（双队列 + 事件 + spliced 操作日志） | `packages/core/session` + agent | `pkg/session/inbox.go` + `pkg/agent/inbox_events.go` | Agent Loop 的 turn/step 输入通道；**inbox 的 append/replace/remove/claim 必须落 durable `agent/inbox/spliced` 操作日志**，否则 UI 无法还原队列状态；没有 inbox = steer/inject/command 等外部输入方式全不成立 |
| M04 | LLM Provider Seam + 流式词汇表 + Map→Derived Union 模式 | `packages/llm/llm` | `pkg/llm` | LLM 是大脑；流式协议（text/reasoning/tool-call）不完整 = 无法正确处理 DeepSeek reasoning 和 function call stream；**`ContentBlockMap` / `MessageSourceMap` / `FinishReasonMap` 三大量词表必须用 `map → keyof` 派生 union 模式**（原版约定，保证未来加新变体不破坏 switch 穷举） |
| M05 | DeepSeek Provider 实现 | `packages/llm/llm-deepseek-official` | `pkg/llm/provider/deepseek` | 首要目标模型；SSE + function calling + reasoning 一次性做对，别分两次 |
| M06 | System Prompt Assembler + Section Registry + Prompt Context 注册 | `packages/core/system-prompt` | `pkg/sysprompt` | Section 顺序和 section 内容是 DeepSeek 行为等价性的命脉；后期改 section 注入顺序会让所有回归测试失效，一次定序；**PromptContext 注册与动态快照持久化**（M42 子项）必须与其同时发布 |
| M07 | Runtime-Context Snapshot Section + switch notice 注入 | `packages/core/system-prompt`（隐含）| `pkg/sysprompt/sections/runtime_ctx.go` | 每次请求前快照 plan/goal/approval/sandbox/preset 状态；**状态变化时必须注入 switch notice（如"注意：你现在进入 plan mode，先规划再执行"）**，否则模型在请求中"看不到"当前状态刚被切 |
| M08 | Tools Pipeline + Registry + Schema | `packages/core/tools` | `pkg/tools` | 四级 Waterfall（pre/execute/post/result）如果不一次性做完整，后面加 spill/approval/telemetry 都等于要改 pipeline 核心接口，大重构 |
| M09 | ValueSchemaSpec DSL → JSON Schema | `packages/core/tools/schema` | `pkg/tools/schema.go` | 原版 tool schema 不是 hand-written JSON schema，是结构化 DSL；不做 DSL = 每次注册工具手写 JSON schema 容易出错且和原版工具签名不兼容 |
| M10 | Scope 作用域原语 + createScope/scopeOf/scopeTarget 三件套 | `packages/core/scope` | `pkg/scope` | Tools/Skills/Commands/Credentials 全部分层注册表依赖；不先定 Scope 合并规则 + scopeTarget = 后期注册逻辑各自为政 |
| M11 | Agent Registry + Agent 接口 + AgentHandle 所有权 + Initiator Scope | `packages/core/agent` | `pkg/agent` | create/resume 是 SDK 的主入口；**AgentHandle.dispose() 必须是能力（只有持有者能 tear down）**；**`ctx.agents.withInitiator()` 发起者作用域** 是子代理授权和审批归属的根基，缺了无法正确绑定父子权限 |
| M12 | Agent Loop（Turn/Step 双循环） + agent/request-error 修复链 | `packages/core/agent-loop` | `pkg/agentloop` | 心脏；waterfall 顺序（pre-step、request 链）和 inbox claim 规则一次写死；**新增 `agent/request-error` waterfall（失败 step 关闭后 turn 关闭前，listener 可 repair state 或返回 retry 动作）**，原版 LLM 失败重试机制靠它，缺了不能从 context-overflow / compact 后自动 retry |
| M13 | User Approval Seam（2 事件对 + policy） | `packages/interaction/user-approval` | `pkg/approval` | fail-closed 默认安全模型；Plan Mode 之后的工具审批流；会话级 ask/never policy 与 session log 绑定 |
| M14 | User Questions Seam（plan-review + multiSelect + intent tag + SDK 回调） | `packages/interaction/user-questions` | `pkg/userq` | Plan Mode 的 exit_plan_mode 审批唯一通道；**AskUserQuestionIntent.kind=plan-review 语义标签 + approve 字段（非位置推断肯定选项）+ multiSelect + detail/header 字段完整** + 错误码（EMPTY_QUESTIONS/NO_PROVIDER/ASK_ABORTED）必须全部 day-1 定义；必须在 SDK 层暴露回调函数 Answerer，不依赖 UI |
| M15 | Human Commands Registry（+ slash 解析 + signal/attachments 携带） | `packages/interaction/commands` | `pkg/commands` | /plan /goal /permission /approval /settings 所有命令的触发入口；**CommandInvocation 必须携带 rawInput、signal、attachments 三字段完整**，不然 /attach 相关命令和 Ctrl-C 取消命令无法工作 |
| M16 | Plan Mode + exit_plan_mode 工具 + plan section 精确 order | `packages/plan/plan-mode` | `pkg/plan` | 招牌需求；**plan:policy section 必须 order=500（严格与原版对齐）**；plan section 文本严格拷贝原版，第一次就对齐 |
| M17 | Goal System + 6 个工具 + activation 权限 | `packages/goal/goal` + `tool-goal` | `pkg/goal` | goal/change CAS revision；不一次性做 CAS = 后期多 writer 并发写入 goal 会丢历史，回写极其痛苦；**GoalActivation 权限控制谁能写 goal** 必须 day-1 定义，不然子代理能随便写父 goal |
| M18 | Goal Round Driver + maxGoalRounds 续轮上限 | `packages/goal/goal-round-driver` | `pkg/goal/round_driver.go` | 多轮续驱动；依赖 turn-stopping 事件顺序 + goal activation 权限；**必须带 `<goal_round>` 续轮提示注入 + maxGoalRounds 上限保护**，不封顶容易死循环 |
| M19 | Todo System + todo_write 工具 | `packages/todo/tool-todo` | `pkg/todo` | 三件套之一；整体替换语义简单但必须与 session event 词汇表同时发布 |
| M20 | Permission Presets（组合 knob + CUSTOM_PRESET 派生态 + selectFor 视图） | `packages/interaction/permission-presets` | `pkg/permission` | 预设切换的用户语义和 fold 派生；**`custom` 派生态永远不作为切换目标**（防 UI 切错）+ fold 从 sandbox+approval 旋钮反推当前 preset + **selectFor(KnobState) 一次性构建完整 options（每表项 + custom 恰好派生时追加）**；后期加 = 当前会话没有 preset 意图记录，且 SDK 侧每次都要手搓 options 列表 |
| M21 | Sandbox Seam + Policy + per-call 策略携带 | `packages/sandbox/sandbox` + `sandbox-policy` | `pkg/sandbox` | 抽象接口 + **每 call 携带策略（不是 Provider 级固定策略）**，保证同一 provider 可并发跑不同边界的会话；先有接口后有后端实现，接口不能后期补 |
| M22 | Shell Seam + bash-local + bash-sandbox consumer + dshEnv 管理命名空间 | `packages/shell/*` + `tool-bash` | `pkg/shell` + `pkg/tools/builtin/bash.go` | ShellExecRequest → resolve → Spec → run 的完整请求/规格拆分；**DSH_* 管理命名空间环境变量合并顺序 + credential scrub（剥离继承环境中的密钥变量）**必须完成，不然会把 API Key 泄露到 bash 子进程里 |
| M23 | Filesystem Seam + fs-local + observation-policy + tool-fs + retention ceiling 截断 | `packages/fs/*` | `pkg/fs` + `pkg/tools/builtin/fs.go` | 观测策略（写前读→版本守卫）不是可选插件，是默认行为；**大文件读取 retention ceiling（head/tail 截断）**必须与 spill 协同，不然 read 一个 GB 级文件直接 OOM；后期补等于 fs 工具已经写出了大量无守卫历史结果，无法回溯"那次写是不是踩了并发" |
| M24 | Spill Storage + Spill Policy（tools/post-execute）+ retrievalHint | `packages/spill/*` | `pkg/spill` | 大文本溢出是高频场景；**retrievalHint = "请用 read/grep 工具读该路径"**；保存失败 = 保留原内联结果（best-effort，不把成功调用变成错误）；接口和 policy 触发阈值必须与 tools pipeline 同步设计 |
| M25 | Job Runtime（long-running 统一生命周期 + owner 归属 + dispose 级联清理） | `packages/jobs/jobs` | `pkg/jobs` | bash bg、子代理、子进程统一 JobId + owner 归属 + cancel/done；**owner agent dispose 时必须自动 cancel 所有 owned job**，不然 agent dispose 后后台 job 还活着，是资源泄漏；后期补会出现已经有 bg bash 但没有统一 cancel 入口的状态，必须拆现有代码 |
| M26 | Subprocess + 进程组 kill + Windows/Linux spawn 差异封装 | `packages/shell/subprocess` | `pkg/shell/subprocess.go` | 进程组、信号、cancel；直接裸用 os/exec 会出现：cancel 不杀子进程、超时后进程还活着、父进程退出子进程变孤儿等问题；后期做会让 timeout/kill 行为不一致 |
| M27 | Skill Registry + 6 层 FS Provider + skill 工具 + fsnotify 目录监听 + catalog diff 注入 | `packages/skill/*` + `tool-skill` | `pkg/skill` | 6 层目录扫描规则（rank 100~600）、**fsnotify 实时监听**（新增/删除/修改后同步失效缓存，Windows 上 polling fallback）、catalog diff 注入与 agent/pre-step 触发时机耦合；后期改 scope 顺序会让技能名解析结果在不同版本间漂移 |
| M28 | Settings（namespaces + schema + secret role + watch + path op 编辑 + Applies 生效时机） | `packages/settings/settings` + `settings-file` | `pkg/settings` | 默认值层级（schema → base → user）、secret 剥离规则、**Applies(live/restart)**：标记 live 的变更立即生效，其他需要重启生效；SettingsPathOp 字段级编辑（set/unset，不破 secret）；后期补会让已写出的 settings 文件无解读者 |
| M29 | Credentials（双密钥空间 + per-call resolve + record modifyRecord RMW + Authorization Flow） | `packages/credentials/credentials` + `local` + `authorization` | `pkg/credentials` | 两个不相交的 key 空间：**CredentialRef（POSIX env name，4 层解析）+ CredentialKey（grant 记录存储，modifyRecord 序列化 read-modify-write — 跨进程支持 token refresh 安全，防并发写丢 refresh token）**；per-call resolve（热更新，不缓存）；AuthorizationFlow 与 DUPLICATE_FLOW/NO_FLOW/ALREADY_IN_FLIGHT/ NOT_COMMITTED 四错误码；把 secret 存哪里是 day-1 问题，不能后期改 |
| M30 | SessionPersistence 抽象接缝（9 方法 + 2 错误类 + session/flush 同步 checkpoint） | `packages/session/session-persistence` | `pkg/persistence` | 9 方法精确定义：create / prepare / load / inspect / list / append / flush / locate + readFrom(冷读)；2 错误类：SessionFormatUnsupportedError（header.version > 支持最大 → 明确升级方向，fail-closed）+ SessionPersistenceCorruptionError；**session/flush 是同步 checkpoint**，与 agent/turn 强同步；不先有接缝直接硬写 JSONL = 接口契约不清晰，后端无法替换 |
| M31 | JSONL Persistence Backend（格式 + batch 窗口 + crash recovery + orphan turn 合成 + format version） | `packages/storage/jsonl` | `pkg/persistence/jsonl` | 格式：**第一行独立 JSON = SessionHeader + 第 2 行起每行一个 SessionEvent JSON（seq 与 line 对齐）**；批量 flush 窗口（首个待写事件启动窗口，后续事件 join 不重置截止时间）；Crash Recovery：孤儿 turn/start → 内存中追加 synthetic turn/end{kind:'interrupted'}，**物理日志绝不截断**，保留所有已 durable 写入的事件；format version 拒绝（高版本 header 直接 fail，不静默降级）；跨 build 会话恢复的"通用格式"，第一次就定对 |
| M32 | HarnessContext + EventBus + Waterfall Chains（定序常量）+ Lifecycle + HarnessError 分类 | 自制 | `internal/harnessctx` | 一次性定型整个运行时骨架；**Waterfall 顺序必须用常量数组写死（不暴露动态 add 给插件）**，避免原版 cordis 动态顺序带来的非确定 bug；HarnessError 稳定错误码（跨 RPC 可传递） |
| M33 | Invariant Registry（allowlist/blocklist + 包伴生 + 独立 fiber + fail 归因）+ 8 条核心不变量 | `packages/runtime-diagnostics/invariants` | `internal/invariant` | **包伴生模式**：每个 package 自带一个 invariant 伴生；独立 fiber 执行；register(packageName) 重复名 throw；fail(message) 抛 INVARIANT + packageName，保证错误归因；不变量只能"越写越严"，8 条核心 day-1 全部执行 |
| M34 | Token Meter（surface 节点定价 + logRevision 快照 + measure/estimateMessage 两接口）+ 会话预算 | `packages/llm/token-meter` | `pkg/llm/tokenmeter.go` | 不是"每次请求记个数字"：**TokenMeasurement 是一个 detached 快照**，携带 logRevision（消耗了多少 durable event）、baseline（usage 锚点 vs estimated 启发式）、surfaceDeltaTokens、surfaceTokens、nodes[]（每个 surface 节点独立定价，含图片视觉 token）+ heuristicTokens（影子价格，压缩 O(1) 投影替换用）；与 LLM seam 一起发布，避免 LLM 调用方漏传计量 hooks |
| M35 | Web 能力接缝（web_search + web_fetch + SSRF 防护 + Provider 自动选择） | `packages/web/*` + `tool-web` | `pkg/web` + `pkg/tools/builtin/web.go` | 原版默认工具就是有 web_search/web_fetch 的，**没有 = 模型回答不了需要实时信息的问题 = 不等价**；HTTP fetch 后端必须包含：SSRF 防护（拒绝 private IPv4/IPv6、NAT64 前缀、同 origin redirect 要重新检查）+ 自动 provider 选择（配置优先，auto-select 当且仅当仅一个 usable provider；ambiguous 报错不静默 first-wins）+ web/errors 标准错误码 |
| M36 | Storage Domain KV 抽象（hub + backend + domain 三层 + version mismatch 拒绝） | `packages/storage/*`（storage + storage-domain + storage-json + storage-sqlite） | `pkg/storage` | Credentials、Settings File Provider、Session Title/Query、Feedback 都要存"非 session event 类"持久化数据；**StorageHub 多 backend 注册 + StorageForms 可扩展 + DomainFacility 定义 typed schema + defineDomain 版本号严格校验 + domain/changed 事件**；缺了 = 每个插件各写各的 KV 存储格式（settings 和 credential 用不同格式，migrate 爆炸）；backend 提供 KvFacet；JSONL backend 一个 unit 一个 JSON 文件 + SQLite backend 一个 document 一行，**与 M30/M31 的 session persistence 是两套平行存储，不能混** |
| M37 | Message Feedback（assistant message 点赞/踩 + sidecar 存储 + CAS optimistic concurrency） | `packages/feedback/message-feedback` | `pkg/feedback` | 存储在 Storage Domain 的 `message_feedback` 表；严格 CAS：put 请求必须带 ifVersion，冲突返回 current item；**target 必须是 append-origin assistant/message（replacement/summary 源不能做反馈）**；list/put/delete 稳定结果类型；**作为库也能暴露 API 给业务侧做 RLHF 数据收集，不做 UI 组件只做 service** |
| M38 | Agent cancel 原因分类 + TurnEndCancelCause 持久化 + keepInbox 选项 | `packages/core/agent`（CancelCause + CancelOptions） | `pkg/agent/types.go` | 不是"随便一个 cancel 函数"：**AgentCancelCause 必须是 4 变体闭合枚举**（user / parent / hook{reason} / disposed）；CancelOptions.keepInbox（保留 inbox，只 cancel 正在跑的 turn）；持久化 turn/end 记录 TurnEndCancelCause；**缺了 = 取消原因无法持久化，compaction/resume 时无法区分"用户打断"和"系统错误"，前端和业务统计都无法做正确区分** |
| M39 | Request Header 快照（首个 user/message 前的 epoch 快照，冻结请求级配置） | `packages/core/session` 的 request/header event | `pkg/session/event_data.go` | 不是"随便发请求"：**每个会话首个 turn 的 request/header event 是一个 epoch 快照**，永久冻结 prompt sections 摘要、预算、温度、reasoning、模型、maxTokens、reasoningEffort 等；后续 turn 的 request/header 可以覆盖；**Token Meter 的 measure 必须把 requestHeader 作为 overlay 重新定价**，compaction 后的 pressure 判断要读它；不做 = 同一个会话中切换模型后历史 token 定价混乱，压力策略漂移 |
| M40 | Session Projections（投影注册中心 + 纯 fold 单元 + 快照/变更推送 + 持久化投影缓存失效版本） | `packages/session/session-projection` | `pkg/session/projection.go` | 二次扫描新增，**原被误判为"纯前端"，实际是 SDK 侧读取派生状态的唯一标准接缝**：`ProjectionDefinition{key, stateSchema, init(header), apply(state,event)[返回同引用=零下游], wire?:{viewSchema, view}, stateVersion}` + `ProjectionSnapshot{asOfSeq, values}` + ProjectionChangeListener；**关键约束：(1) apply 必须纯同步，异步会撕裂一致裁剪；(2) 不感兴趣的事件必须返回原 state 引用（Object.is 级不变引用才零下游）；(3) stateVersion 递增时旧缓存全丢弃；(4) 每个 key 一个 owner，重复 key 同版本 merge、不同版本抛错**。没有统一 Projection = SDK 侧每读一次 Goal/Todo/Preset 都要各自 fold 1000+ 条事件，性能 O(N×K)，且状态读取各自为政策略漂移；后期补等于所有消费端调用协议都要改 |
| M41 | Session References + File References（跨会话引用 + 文件引用 mention + 消息预处理 + 稳定错误码） | `packages/context/session-reference` + `file-reference` | `pkg/session/reference.go` | 二次扫描新增，**之前完全遗漏**：用户消息前置处理层 `PreparedReferencedMessage`：(1) `@[label](dsh-session:sessionId)` mention 语法解析 → 查目标会话 → 组装 `additionalContext`（UserMessage 快照，内含目标 session 当前 surface 文本，带标签和预算）；(2) `@/path/to/file` 文件候选补全（FileReferenceCandidate，纯路径不读内容）；(3) 稳定错误码（INVALID_CONFIG/INVALID_REFERENCE/SELF_REFERENCE/TOO_MANY/READ_FAILED/BUDGET_EXCEEDED/CANCELLED 全部 7 个）；(4) `SessionReferenceCandidate{sessionId,label,cwd,sameWorkspace,createdAt}` 过滤只用 title/id/cwd，绝不用 transcript 文本（防隐私泄露）。**为什么 MUST 不是 COULD**：(a) MessageSource Map 的 `subagent-report`、`subagent-settled` 等新来源 tag 的反解析就是这个 seam 的子协议；(b) deriveMessages() 的前处理层（去 mention token + 附加上下文）一旦后期补，就等于要重写 user/message 进入 LLM 历史前的整条链路，还要兼容老日志里"不带 additionalContext 字段的纯引用消息"；(c) SELF_REFERENCE / TOO_MANY 两条安全约束 day-1 不做，后期就是越权+爆上下文的硬伤 |
| M42 | Prompt Context（动态上下文注册 + 变更持久化快照 + Compaction 联动保留） | `packages/core/system-prompt` 的 `PromptContext` 注册 | `pkg/sysprompt/context.go` | 二次扫描新增，**与 PromptSection 平级但常被忽视**：`PromptContext{name, order, text|fn(ctx)}` 与 PromptSection 并行注册，区别在于：Section 是 system prompt 前缀静态部分，Context 是每次请求前动态解析后**作为 user-role 消息落在"模型历史之后、本次请求之前"的位置**，并且 **change-only 持久化**（只有内容变了才落 user/message 快照，同时 compaction 压缩时必须保留最后一次完整 Context 快照，不能把它当作普通 user-message 裁掉）。没有 PromptContext 机制 = Skills catalog hint、runtime switch notice、goal round driver 续轮提示 都只能随便写入 user/message，compaction 时要么误裁掉要么上下文替换时把动态上下文也丢了，等于行为在压缩前后不一致；**必须与 System Prompt Assembler（M06）同时发布** |

**⚠️ 必须一次性发布的 45+ SessionEvent 类型词汇表**（M02 的子项，按五大簇 + 扩展簇整理，**全部必须 day-1 枚举，用 Map→Derived Union 模式**）：

```text
核心 Spine（M02）：
  turn/start, turn/end（含 TurnEndReason: complete/blocked/interrupted/silenced/
     error-panic / aborted / max-tokens / refusal 【闭合 Map 派生 union，可扩展】）
  step/start, step/end
  user/message（含 admitted=true 已入账标记 + MessageSource 来源：
     'user' | 'webhook' | 'coordinator' | 'subagent-report' | 'subagent-settled'
     等 Map 可扩展）
  assistant/message（含 stopReason + FinishReasonMap）
  request/header（每个 turn 前的 epoch 快照，冻结 prompt sections 摘要、
     预算、温度、reasoning、模型、maxTokens、reasoningEffort）
  request/context（可选 request 上下文补充）
  session/end-seed（fork 种子边界）
  agent/status（idle/running 切换，不进 LLM 历史）
  agent/error（运行时错误，不进 LLM 历史）
  tool/call（模型发起的工具调用，id/name/args/argState parsed|raw）
  tool/result（工具执行结果，callId + outcome + isError + finalizeContent）
  tools/pending-batch（批量工具调用的开始标记，携带 count）
  tools/drained（批量工具调用的排空标记）

Inbox 操作日志（M03）：
  agent/inbox/spliced（inserted / claimed / discarded / removed
     / replaced / cleared 归一化操作，UI 还原队列状态的唯一依据）

规划簇（M16-M19 + 命令簇 M15）：
  plan/mode（Plan Mode 开启/关闭，log-only）
  goal/change（Goal 状态变更，带 revision CAS）
  todo/write（Todo 整体替换）
  command/run（人类命令执行，commandId + name + rawInput + attachments + signal）
  command/done（命令完成，result + sourceEventSeq）

工具执行安全簇（M21-M27 + Web M35）：
  sandbox/mode（sandbox mode 会话级切换，log-only）
  approval/policy（approval policy 会话级切换，log-only）
  approval/asked ↔ approval/decided（严格成对的审批审计事件）
  permission/preset（用户选择的 preset 意图记录，log-only）
  agent/inbox-turn, agent/inbox-step（下一 turn/step 的待执行输入）
  session/hint（一次性 hint，如 <available_skills> 注入）
  subagent/descriptor（子代理模式/标签/提供者持久化描述，listChildren 身份依据）
  subagent/start、subagent/end（观察-only 生命周期事件，快照不泄漏 handle）

Compaction 预留事件（S01 SurfaceOp 机制）：
  compaction/start（获取日志锁 + turn 所有者）
  compaction/summary（摘要投影 + shadowedRange + shadowedSeqs +
     shadowedTokenCount + provider/model/maxTokens/usage）
  compaction/end（释放锁，error 字段记失败尝试）
  【约定】这三类不进 surface，summary 的结果通过 user/message +
     surfaceOp:{op:replace,start,end} 做表面替换，不修改源事件。

Schedule 预留事件（S10 可延后，但事件类型 day-1 预留）：
  schedule/change（create / delete / dispatch 三变体，严格 decoder，
     version=1，fork 只 fold seedLength 之后）

引用/预处理 预留事件（M41，day-1 必须有，对应 prepared-message 审计）：
  reference/resolved（引用解析成功，携带 refKind：session | file，
     resolved 计数、token 预算占用、未解析的 mention 数；
     log-only，不进 surface，用于审计）

持久化元数据（M02 Header 独立携带）：
  ⚠️ 以下字段**不出现在 SessionEventMap 中**，独立于事件日志放在 SessionHeader：
  version, id, createdAt, cwd, parentSession, seedLength,
  origin('subagent'|absent), delegationDepth, agentPreset

💡 设计约定（Map → Derived Union）：
  所有事件变体用 `SessionEventMap[type] = DataStruct` 接口键表，
  用 `type SessionEvent = SessionEventMap[keyof SessionEventMap]` 派生 union，
  保证 switch 穷举 + 未来可扩展，这是原版 60+ 子系统的统一模式。
  TurnEndReason / MessageSource / FinishReason / ContentBlock 同模式。
```

#### 10.3.2 SHOULD 级：14 项（强烈建议一次性到位，可避开后续大重构）

| 编号 | 子系统名 | 对应 packages 路径 | 复刻位置 | 一次性到位理由 |
|------|---------|------------------|---------|-------------|
| S01 | Compaction（上下文压缩 + pressure 策略 + tool-result pruner + surfaceOp 替换） | `packages/compaction/*` + compaction-tool-result-pruner | `pkg/compaction` | 与 SessionLog SurfaceOp 机制紧耦合；如果 day-1 事件结构里不预留 SurfaceOp 字段 + 不设计"压缩结果是消息表面替换、不修改源事件"的不变量，后期加等于重写 DeriveMessages；**tool-result pruner（Unicode head/mid/tail 安全裁剪）**与 retention ceiling 对齐，不做 = 超大 tool result 让 pressure 永远触发 compact 但无法压缩到阈值下 |
| S02 | Subagent 完整接缝（one-shot + continuable Activation + listChildren/Descendants + parent授权 + reportFrom/settled） | `packages/subagent/*` + tool-subagent + tool-subagent-control | `pkg/subagent` | 深度 delegationDepth 是 SessionHeader 字段（day-1 就有）；**origin='subagent' + subagent/descriptor 日志** 必须与 listChildren/Descendants 一起发布；continuable Activation 的 waiting/running/settled 三态、**父授权（父 SessionHeader.parentSession 匹配才允许 followup）、reportFrom（子向父发 report 消息）+ settled notice（Activation 结束时父端状态通知）**；这些与 Session header 强绑定，后期补 = 已经创建的子代理会话在 listing 中无法识别为子代理，子代理生命周期泄露 |
| S03 | SQLite Persistence Backend（session + storage domain 两边） | `packages/storage/sqlite-session` + storage-sqlite | `pkg/persistence/sqlite` + `pkg/storage/sqlite_backend.go` | 与 SessionPersistence + StorageDomain 抽象接缝同一天发布（接缝已写好，多一个后端只加一个包）；**Session 用 sessions+events 表 + Storage Domain 用 kv unit 表** 分开建，避免混表；避免生产环境下 JSONL listing 性能问题暴露后再紧急迁移 |
| S04 | Session Query + Title 自动生成 + sidecar 存储 + 批量 observation 原子性 | `packages/storage/session-query` + `session-title` | `pkg/session/query.go` + `pkg/session/title.go` | Title 用首轮 turn 结束后 LLM 异步生成，**sidecar 独立存 StorageDomain（不回写事件日志，保持日志 append-only 纯）**；Query 依赖 SQLite FTS5 或 JSONL 自建索引 + **SessionRecord.live/persisted 双源可用性标记 + SessionTitleObservation 原子观察（header + title 同一观测）**；若 session store 形状不确定时不做，后期加等于要设计一套与 store 实现无关的查询抽象，工作量变大 |
| S05 | OpenTelemetry 集成（Trace + Metrics + session-telemetry redaction waterfall） | `packages/runtime-diagnostics/telemetry-otel` + session-telemetry | `internal/telemetry` | Agent Loop 每次 turn/step、每次 llm/stream、每次 tool/execute、每次 goal/change 都是天然 trace span；**session-telemetry/record waterfall 脱敏链**（导出前对 PII 字段裁剪，fail-closed：throwing listener 扣留记录）+ **ledger vs ops 双通道**（ledger = 日志镜像，ops = agent-error/shutdown 信号无 seq 身份）埋点本身是轻量 interface，无真依赖，没启用 OTel 也完全不影响性能；day-1 不埋 = 后期改每一个热点文件 |
| S06 | Credentials 本地加密存储 + 系统密钥链集成 | `packages/credentials/credentials-local` | `pkg/credentials/local_encrypted.go` | local provider 的标准 4 层（process env → user env file → project env file → credential-managed store）+ **credential-managed store 可选 OS keyring / DPAPI / gov.uk/go-keyring**；一次实现完避免 API key 以明文存在磁盘上的"临时方案"变成永久隐患 |
| S07 | Session Telemetry Backend + Sharing Disclosure 契约 | `packages/runtime-diagnostics/session-telemetry` + OTel backend | `pkg/session/telemetry.go` | 会话级 ledger 日志上报（每条 session event 对应一个 record，含 `severity(info/warn/error) pre-mapped`）+ session-telemetry/record 脱敏 waterfall；**sharing disclosure：full / feedback-only / disabled** 三态；与 Token Meter（M34）一起发布能省下后续回头埋指标的工作；reporting SDK 的 batching/retry 不归 harness 管，边界就是 emit() |
| S08 | Attachment 图片引用模式（持久化在前 + 事件引用在后 + 内容寻址 digest） | `packages/attachment/attachment` | `pkg/attachment` | **先持久化到 <DSH_HOME>/attachments/v1（sha256:<digest> 内容寻址，原子提交）→ 返回 AttachmentRef（opaque id + mediaType + bytes + w/h）→ 再写 user/message 的 ImageBlock**，绝不把 base64 或临时文件路径写进 session log；作为库集成时可只开 service，不强制使用；**ImageRequestPolicy（每模型路由独立的像素/字节预算）**+ 读前重校验 digest；不做 = 后期加图片多模态会破坏日志格式（老日志里 ImageBlock 没有 attachment id，无法兼容） |
| S09 | Tool-result pruner + compaction/prune 定价事件 | `packages/compaction/compaction-tool-result-pruner` | `pkg/compaction/pruner.go` | 与 S01 组合；Unicode code point 精确裁剪（不切 UTF-16 surrogate）；**每次替换都追加 compaction/prune shadow-price 事件定价被裁剪 node，O(1) 投影 fold 能直接扣减，不需要 per-node 重算**；不做 = 超大 tool result 压缩前占满 context，但压缩算法选 range 无法识别"裁剪完就能降 80% token"的候选，浪费压力 |
| S10 | Schedule 会话级定时提醒（durable reminder + idle dispatch + at/after/every 三模式） | `packages/schedule/schedule` | `pkg/schedule` | **durable 事件 schedule/change day-1 就已预留（见 M02 词汇表）**，补 service 层只是小工作量；**session-local delivery（只在原 Session 存活时才 fire）** + Agent idle 阶段排队 followup，不打断当前 turn；每记录最小 5 分钟间隔；Every 记录冷启动跳过 missed 区间，只贡献下一个 due occurrence；不做 = 用户说"每 6 小时 remind 我检查 xxx"还得业务层额外挂 cron，而且 cron 无法知道 Agent 是否 idle，会打断高优先级工作 |
| **S11** | **Workspace Registry（工作区目录持久化 + 会话分组 + StorageDomain 使用方 + 中断删除恢复）** | `packages/workspace/workspace` | `pkg/workspace` | 二次扫描新增，**原 C 级升级**：**Workspace{id(uuid), path(canonical realpath), title, sessionIds[], createdAt, updatedAt}** + create(path)canon 唯一性 + attachSession/detachSession/insertSessionBefore（DOM insertBefore 语义）+ **pending-mutation 标记**（create/delete 两阶段写前挂标记，启动时精确恢复：未完成 delete 推进、未完成 create 回滚，防半写）+ status() 实时检查目录存在；为什么 SHOULD 不是 MUST：(a) Storage Domain 第一个真实使用方，**能在 day-1 用真实数据测出 defineDomain 的版本校验 + domain/changed 事件的 bug**，避免后期上线时 Settings/Credentials 才炸；(b) session cwd ↔ workspace 的双向校验；(c) WorkspaceId + ordered sessionIds 是 UI 会话分组的通用骨架，即使 headless SDK 也可能需要「按项目查会话列表」；(d) 挂到 Workspace 的 agent-presets 预设挂载需要它；(e) 后期加 = 已存在的 session 要么无归属 Ungrouped，要么需要全量扫 cwd 归并，可能误判 |
| **S12** | **LLM Retry（失败分类 + context-overflow 触发 compact + 重试历史 CAS）** | `packages/llm/llm-retry` + plugin | `pkg/llm/retry.go` | 二次扫描新增，**原与 M12 request-error waterfall 强绑定必须 day-1 有后端**：**LlmFailure 闭合错误码（overload/rate-limit/response-refusal/context-overflow/unauthorized/network/unknown）** + RetryPolicy{maxRetries, backoffJitter} + **失败路由**：context-overflow → 触发 compact(S01) → agent/request-error 返回 retry → 同 turn 重跑 step；其他错误按指数退避；**重试历史 CAS 写入 request/context 的 retries 字段，防止跨进程重复重试**；为什么 SHOULD 不是 MUST：day-1 没有它的话 agent 会在 context 爆时直接抛错终止（能降级，不是死循环），但生产体验极差，且官方 compact ↔ retry 联动契约是长程推理能力的核心；后期补会出现 "M12 已写好 request-error 但没人处理" 的半产品状态 |
| **S13** | **Output Retention（统一 head/mid/tail 截断算法 + Unicode 安全）** | `packages/util/output-retention` + timeout 伴生 | `pkg/util/retention/` | 二次扫描新增，**原散落在多处的通用算法单独抽出**：RetentionSpec{headBytes, midBytes, tailBytes, preserveLineBreaks:true} + 三处复用：(a) FS retention ceiling(M23)、(b) spill policy 预览(M24)、(c) Compaction pruner(S09)；**严格 Unicode 安全**：不截断 UTF-8 多字节序列中间、不切开 surrogate pair、不把 combining mark 与 base 分离；配合 O(1) shadow token 定价；为什么 SHOULD 不是 MUST：三处都能临时用"按字节截断+TODO 注释"替代，但会引入「大文件预览乱码」「pruner 裁剪出非法 Unicode 导致 LLM 解析错」的长期债，三处都改等于要改三个 package 的已发布接口 |
| **S14** | **MCP Client 接缝（Model Context Protocol）** | `packages/mcp/mcp-client` | `pkg/mcp` | 二次扫描新增，**原完全遗漏后升级 SHOULD**：**MCPTransport 抽象（stdio/http/jsonrpc）+ MCPClient{connect/listTools/callTool/listResources/readResource/listPrompts/getPrompt}** + 注册到 ToolRegistry 的 **Adapter（MCP tool → 本地 ToolDefinition 自动映射，参数 schema 直转 ValueSchemaSpec）**；为什么 SHOULD 不是 MUST：(a) MCP 已成为 Agent 工具生态的事实标准，不做 = 大量现成 Server（文件系统/Git/DB/浏览器）不能直接接入，只能每个做适配；(b) 与 Tools Waterfall 同时发布最稳；(c) 后期加 = 为了适配 MCP 异步/流式/分页语义可能需要回头改 ToolDefinition 的 Execute 签名（MCP 的 callTool 天然支持进度报告，需要我们的 ToolOutput 抽象带 channel）；MVP 只实现 stdio transport 足够 |

#### 10.3.3 COULD 级：18 项（可延后，不影响等价性 — 4 项已升级到 SHOULD 保持 18 项计数不变）

| 编号 | 子系统名 | 原版功能 | 为什么可以延后 |
|------|---------|---------|-------------|
| C01 | Workflow Engine（动态工作流脚本引擎 + worker_threads vm） | 模型写 script 调 agent()/parallel()/pipeline() 编排多子代理；fatal:true 错误传播 + 脚本 bounded settle | 高级能力，不是 DeepSeek 默认 Agent 行为；先有 subagent(S02) 后再加脚本引擎更顺；WorkflowRunHandle.dispose 必须等 quiescence 等契约可以 S02 稳定后再搬 |
| C02 | Subagent 多 Provider 后端（fork-in-process / ACP / Codex / Claude Code / dsh-sdk） | 子代理可以真 fork 进程、调 ACP、远程调 Codex、调 DSH SDK 实例 | MVP 只有 in-process spawn 足够；其他后端后期按需接入，能力注册与 SubagentRuntime 解耦 |
| C03 | Filesystem 多 Provider（E2B / 远程 / 云存储） | E2B 沙箱文件系统后端、远程文件系统 | 99% 代码场景只有本地磁盘；FileSystem 接口已完整(M23)，换后端只加 Provider |
| C04 | LSP 语言服务器集成（gopls 等 + 4 操作闭集：goToDefinition/findReferences/goToImplementation/hover） | Go/TS 项目可调用 LSP 做语义级重构、跳转、rename；**LspProvider 扩展映射 + UTF-16 位置约定** | 纯生产力增强，Agent 靠读文件 + grep 也能干活；LSP seam 抽象（4 操作闭集/位置 0-based UTF-16）完整，后期独立发布不影响其他模块 |
| C05 | Typert（类型化 CLI / Remote 参数解析） | dsh 命令行的类型化参数体系 + @Remote 生成的 HTTP/RPC 入口 | 作为库集成的场景下参数解析由业务服务负责；若要做 CLI/demo，后期基于 SDK 包一层即可 |
| C06 | Dynamic Plugin 检查工具（cordis_inspect / inspect host/client providers） | 模型查询当前 runtime 有哪些服务/浏览器能力 | 插件市场联动能力；MVP 只靠技能/工具注册足够 |
| C07 | Webhook Runtime + GitHub Adapter | dispatch 规则 + Workspace-backed Session 创建 + source webhook provenance 消息来源标记 + fire-and-forget 无重试语义 | 业务侧可自己做 HTTP 接入，再调用 SDK.Run()；Webhook 的创建 Session→attach workspace→入 inbox 整条链依赖 Workspace(S11) 成熟后再做更稳 |
| C08 | Slots（Web Client React 组合树 + inject 贡献点） | 整个 Web UI 的 100+ slot key 树、single/list/keyed/chain 四种 cardinality + scope | 纯前端；Go 后端作为库不需要渲染槽 |
| C09 | Conversation Node Engine + packed Assistant 增量事件 | 聊天 UI 的对话节点引擎、增量 text/reasoning/tool-call chunkrow 打包、update-only 匹配 + 回放 | 纯前端展示；事件日志完整(M01~M42)足够作为库重新做任何前端投影；packed chunkrow 是 client-only 事件，不在 spine 上 |
| C10 | Code Runtime（REPL/代码执行沙箱 + stdout capture + host binding 命名空间 + typed error 构造器注入） | 专用代码执行沙箱，把 stdout/stderr 捕获 + exit code 映射；支持 host 函数注入作为全局对象 | bash 工具 + go run / python 即可替代；**CodeBindingNamespace + CodeBindingErrorClass** 契约可在后期独立做 |
| C11 | Terminal UI（前端 xterm.js 渲染） | Web UI 终端展示、bash bg 输出的实时流 | 纯前端 |
| C12 | Web Server / WebSocket / Client Modules | HTTP 服务 + WebSocket 交互 + 浏览器 API + Stream Protocol 帧协议 | 作为库直接调用时不需要；你可自己包 HTTP/gRPC，stream protocol 格式仅服务于 WebSocket 网关对传 |
| C13 | Extensions（Dynamic Cordis Runner + Plugin 市场 + define/undefine/run approval） | 用户安装扩展、动态加载包、Host+Browser 两半、动态包版本化 | MVP 只支持技能/工具直接注册；动态插件隔离沙箱 + 审批流程后期做 |
| C14 | Governance（审计导出 + 合规性检查 + retention 策略） | 审计日志导出、合规性检查、记录保留策略 + 自动清理 | 企业级场景后期加；session telemetry ledger 通道(S05/S07)已可被业务侧自行消费做审计 |
| C15 | Agent Team 协作（多代理协作框架 + plan 分配 + 共享 workspace） | 团队化多代理，把大 Plan 自动拆多个子计划分配给 subagent | 有 subagent(S02) + goal/todo(M17-M19) 就可以靠模型自己 spawn 子代理做分工，专门的 team 模式是产品封装，不是内核 |
| C16 | Feedback UI 侧投影 + /feedback 命令分享链路 | 用户 feedback 记录 → 分享 → Session telemetry 上报 + RLHF 分享协议 | M37(MessageFeedback) 已做 service 层，UI 命令和分享后期再加 |
| C17 | LLM 多 Provider 切换适配器（OpenAI/Anthropic 等非 DeepSeek 路由） | 模型路由 + per-route reasoning effort + output schema 兼容 + 视觉 token 差异化定价 | 首要目标是 DeepSeek 等价复刻；LLM seam(M04) 已经抽象完整（ContentBlockMap / ToolSchema / 流式词汇表），换 provider 只加一个包，不做第一优先级 |
| C18 | Sandbox 平台专用后端（Landlock / bwrap / Seatbelt / Windows ACL restricted token） | Linux/macOS/Windows 各平台原生内核级沙箱约束；MVP 阶段只定义 Sandbox 接缝接口 | K10 从 SKIP 升为 COULD（Windows 场景的生产化需求，不是纯 UI）；接缝(M21)完整只换 provider，MVP 可先退回 danger-full-access + TODO 注释；后期做时需要平台专用二进制分发（native/landlock-run 等价物） |

#### 10.3.4 SKIP 级：11 项（纯 UI / 纯产品 / 非内核，第一轮不做 — K10 已迁到 C18）

| 编号 | 子系统名 | 原版功能 | 为什么跳过 |
|------|---------|---------|---------|
| K01 | Terminal（前端终端可视化） | Web UI 画终端 | 纯前端 |
| K02 | Web Client（React UI 包） | 整套前端界面 | 纯前端，你的诉求是"无需界面" |
| K03 | Workspace 前端工作区 UI 组件 | 前端工作区导航/切换面板组件 | 纯前端；Workspace 后端注册表（S11）已复刻，UI 层按需再做 |
| K04 | Session Reference 前端锚点 UI | UI 中的 @mention 下拉框和高亮渲染 | 纯前端；Session/File Reference **后端接缝已在 M41** 完整复刻，UI 层按需 |
| K05 | Session Projection 前端渲染树 + Slots 绑定 | UI 节点渲染 + React Composition | 纯前端；后端 Projection 注册中心（M40）已复刻，UI 消费 snapshot 即可 |
| K06 | Permission Presets 客户端下拉组件（仅 UI 部分） | UI 选择器渲染、切换动画 | 作为库暴露 SDK API（names/optionOf/selectFor(M20)）足够 |
| K07 | Agent Note 系统（.agents/notes/） | 架构决策记录格式 + 作者面板 | 产品自身开发方法论，不是运行时能力 |
| K08 | UI Workflow Run 可视化节点 + 进度条 | workflow 进度 UI 节点 + phase 颜色 | 纯前端；Workflow 后端引擎在 C01，可延后 |
| K09 | Client-Modules（UI 插件扩展 + Slot 贡献点） | 前端插件化 + HMR | 纯前端 |
| K10 | Web Styling（VitePress 文档站点 + CSS 令牌） | 整套品牌设计 + 文档样式 | 纯产品视觉展示；与 Agent 内核零耦合 |
| K11 | Feedback 前端 UI（点赞/踩按钮 + 分享面板） | 用户反馈按钮 + 分享弹窗 | 纯前端；后端 MessageFeedback(M37) 已完整 |

---

### 10.4 五大簇依赖关系图（一次性到位版）

```text
               ┌──────────────────────────────────────────────────────────┐
               │             HarnessContext（一次定型骨架）                │
               │   Services 注册表 + EventBus + Waterfall Chains（定序常量）│
               │   + Lifecycle + HarnessError 分类 + parent scope 链       │
               └──────┬──────────────┬──────────────┬──────────────┬───────┘
                      │              │              │              │
       ┌──────────────▼──┐  ┌───────▼─────────┐  ┌──▼─────────────┐  ┌─▼───────────────────┐
       │🔴 簇1：内核驱动簇│  │🔴 簇5：配置/凭证 │  │🔴 簇4：持久化簇 │  │ 🔴 簇2：规划能力簇   │
       │ ┌─────────────┐ │  │ ┌──────────────┐│  │ ┌─────────────┐ │  │ ┌──────────────────┐│
       │ │Session(M02) │ │  │ │Skills(M27)   ││  │ │Persistence   │ │  │ │Plan Mode(M16)   ││
       │ │+30 event类型│ │  │ │Settings(M28) ││  │ │Abstract(M30) │ │  │ │→ User Q(M14)↘   ││
       │ │+Header+fold │ │  │ │Credentials(M29)│ │ │JSONL(M31)   │ │  │ │Approval(M13)    ││
       │ └──────┬──────┘ │  │ └──────────────┘│  │ └──────┬──────┘ │  │ └───────┬──────────┘│
       │        │        │  └──────────┬──────┘  │ ┌──────▼──────┐ │  │         │          │
       │ ┌──────▼──────┐ │             │         │ │SQLite(S03)  │ │  │ ┌───────▼──────────┐│
       │ │LLM(M04/M05) │ │             │         │ │+Query(S04)  │ │  │ │Goal(M17+M18)   ││
       │ │+TokenMeter  │ │             │         │ └─────────────┘ │  │ │(CAS revision)   ││
       │ └──────┬──────┘ │             │         └─────────────────┘  │ └───────┬──────────┘│
       │        │        │             │                                │         │          │
       │ ┌──────▼──────┐ │             │                                │ ┌───────▼──────────┐│
       │ │SysPrompt(M06)│ │             │                                │ │Todo(M19)        ││
       │ │+RuntimeCtx(M07)│            │                                │ └──────────────────┘│
       │ └──────┬──────┘ │             │                                │ Commands(M15) ← 入口│
       │        │        │             │                                └─────────────────────┘
       │ ┌──────▼──────┐ │             │
       │ │Tools(M08/09)│ │             │
       │ │4级Waterfall │◄──────────────┤  Spill(M24) ← (tools/post-execute policy)
       │ └──────┬──────┘ │             │
       │        │        │             │  + Settings：提供可配置 maxInlineBytes
       │ ┌──────▼──────┐ │             │  + Credentials：给 LLM/Bash/Web 工具提供密钥
       │ │Scope(M10)   │◄─────────────┘  + Skills：catalog section 注入 sysprompt
       │ │分层注册表合并│
       │ └──────┬──────┘
       │        │
       │ ┌──────▼──────┐          ┌───────────────────────────────────┐
       │ │Agent(M11)   │          │ 🔴 簇3：工具执行安全簇             │
       │ │+Registry    │          │ ┌────────┐ ┌────────┐ ┌─────────┐ │
       │ └──────┬──────┘          │ │Shell(M22│ │Fs(M23) │ │Sandbox  │ │
       │        │                 │ │+Bash工具│ │+FS工具 │ │(M21)   │ │
       │ ┌──────▼──────┐          │ └───┬────┘ └───┬────┘ │/Policy  │ │
       │ │AgentLoop(M12)│         │     │          │      └────┬────┘ │
       │ │Turn/Step×2  │◄─────────┤     └────┬─────┘           │      │
       │ │Pre-Step+Req │         │          │                 │      │
       │ │Waterfall    │         │    ┌─────▼──────┐   ┌──────▼───┐  │
       │ └─────────────┘         │    │Subprocess  │   │Permission│  │
       │                         │    │(M26)       │   │Presets   │  │
       │  Invariant(M33)         │    └─────┬──────┘   │(M20)    │  │
       │  ───────────            │          │          └──────┬───┘  │
       │  8 条不变量 day-1 执行    │    ┌─────▼──────┐          │      │
       │                         │    │Jobs(M25)   │          │      │
       │  SHOULD 插件：           │    │后台任务统一 │          │      │
       │  ────────────            │    │生命周期    │          │      │
       │  ├ Compaction(S01) ◄─Session SurfaceOp 预留             │
       │  ├ Subagent(S02)   ◄─Header.origin/delegationDepth 预留 │
       │  ├ OTel(S05)       └─每个关键 span 埋点 hook            │
       │  └ Credentials Local Encryption(S06)                    │
       └──────────────────────────────────────────────────────────┘
```

**依赖关系关键说明（为什么必须一次性到位）**：

1. **簇 1（内核驱动）→ 其他所有簇的地基**：没有完整的 Agent Loop + 事件词汇表，簇 2 的 Goal Round Driver 无事件可订阅，簇 3 的 tools pipeline 没有执行时机，簇 4 的持久化无日志可写。
2. **簇 5（配置/凭证/技能）→ 双向箭头回簇 1 的 Tools + SysPrompt**：
   - Tools 的 spill policy maxInlineBytes 来自 Settings；
   - Tools / LLM 的密钥来自 Credentials；
   - Skills catalog section 注册到 SysPrompt Assembler。
3. **簇 2（规划）↔ 簇 1 的 Pre-Step Waterfall**：Plan Mode section 注入、Skills catalog diff 注入、Runtime Context 快照注入**全都是 agent/pre-step 瀑布链上按固定 order 触发的 handler**。如果 day-1 waterfall 顺序没把这些 handler 放进去，后期加只能插到末尾，影响 order → 影响 prompt 文本顺序 → 模型行为漂移。
4. **簇 3（工具执行安全）↔ 簇 1 的 Tools Waterfall 四级链**：
   - Approval 决策发生在 tools/pre-execute；
   - Spill 替换发生在 tools/post-execute；
   - Sandbox Policy 携带是 Shell/FS 执行前必须 resolve 的。
5. **簇 4（持久化）↔ 簇 1 的 Session.append + SessionHeader**：事件词汇表 30+ 种必须在持久化前完全定义，否则 SQLite/JSONL 中写出的事件类型，新版本不认识（原版 format refusal 机制就是保证 fail-closed）。
6. **Invariant（M33）与 Session Event（M02）是孪生体**：不变量在 day-1 就校验每条 append 的 seq/time 连续性，能在开发早期就截获"某个角落直接 push 事件绕过了 append()"这类 bug；后期加则历史日志可能不满足不变量，必须写迁移逻辑。

---

### 10.5 一次性复刻到位的更新版项目结构（含全部 MUST + SHOULD）

```
d:\workspace\typescript_workspace\dsh-go\
├── go.mod
├── go.sum
├── README.md                          ← 本文档（含 1-10 章，**唯一**根目录主文档）
├── docs/                              ← 详细文档目录
│   ├── TASKS.md                       ← 任务表主入口（人类可读）
│   ├── tasks.json                     ← 任务表机器可读（程序化）
│   ├── trace.md                       ← 对话追踪
│   └── CACHE_HIT_RATE_PLAN.md         ← 缓存命中率对齐实施计划
│
├── cmd/
│   ├── dsh/                           ← CLI（可选，对标 dsh 命令）
│   │   └── main.go
│   └── demo-all-in-one/               ← 一次性验收 demo（必须可跑通所有 MUST）
│       └── main.go
│
├── pkg/                               ← 公共库（全部 MUST + SHOULD）
│   │
│   ├── util/
│   │   ├── brand/                     ← M01：Branded ID（SessionID/ToolCallID/ApprovalRequestId/JobId/SpillLocator/SettingsNamespace/CredentialRef）
│   │   │   ├── brand.go
│   │   │   └── brand_test.go
│   │   ├── jsonext/                   ← lossless JSON 值语义
│   │   └── retention/                 ← S13：Output Retention（统一 head/mid/tail 截断算法 + Unicode 安全 + O(1) shadow token）
│   │       ├── retention.go           ← RetentionSpec + Apply（UTF-8/surrogate/combining mark 安全）
│   │       └── retention_test.go      ← 乱码/边界/中文/emoji 测试用例
│   │
│   ├── llm/                           ← M04：LLM Provider 接缝 + 流式词汇表
│   │   ├── types.go                   ← ContentBlock（text/tool_use/tool_result/image）+ Message 角色 + ToolSchema + TokenUsage
│   │   ├── service.go                 ← LLMService 抽象接口 + Provider 注册表
│   │   ├── stream.go                  ← StreamChunk（text/reasoning/tool-call）
│   │   ├── retry.go                   ← S12：LLM Retry（LlmFailure 7 错误码 + context-overflow→compact→retry 路由 + 重试历史 CAS）
│   │   ├── tokenmeter.go              ← M34：Token Meter（每次 request 精确计量 + 会话 budget + surface 节点定价 + heuristic shadow）
│   │   └── provider/
│   │       └── deepseek/              ← M05：DeepSeek 官方实现（REST + SSE + reasoning + tools）
│   │           ├── client.go
│   │           └── provider.go
│   │
│   ├── session/                       ← M02：事件溯源 + 派生投影（心脏）
│   │   ├── event.go                   ← EventType（30+ 常量枚举，严格对应 10.3.1 列表）+ SessionEvent{seq,time,type,data,sourceEventSeqs,SurfaceOp}
│   │   ├── event_data.go              ← 每种 Event 的 Data 结构体（TurnStartData/TurnEndData/UserMessageData/AssistantMessageData/
│   │   │                              ←   RequestHeaderData/ToolCallData/ToolResultData/PlanModeData/GoalChangeData/
│   │   │                              ←   TodoWriteData/SandboxModeData/ApprovalPolicyData/ApprovalAskedData/ApprovalDecidedData/
│   │   │                              ←   PermissionPresetData/AgentInboxData/SessionHintData/CommandData 等）
│   │   ├── session.go                 ← Session 结构体 + append() 严格不变量校验 + Header 访问
│   │   ├── header.go                  ← SessionHeader{version,id,createdAt,cwd,parentSession,seedLength,origin,delegationDepth,agentPreset}
│   │   ├── derive.go                  ← deriveMessages() 派生 LLM 历史
│   │   ├── fold.go                    ← fold* 投影族（foldPlanMode / foldEffectiveSandboxMode / foldEffectiveApprovalPolicy /
│   │   │                              ←             foldGoal / foldTodo / foldPermissionPreset / foldRequestHeader）
│   │   ├── inbox.go                   ← M03：Inbox 双队列（next-turn + next-step）
│   │   ├── invariant.go               ← M02 子项：append 结构级不变量（seq 连续、time 单调、事件类型白名单）
│   │   ├── projection.go              ← M40：Session Projections 注册中心（ProjectionDefinition + init/apply/wire + stateVersion + 快照/变更推送）
│   │   ├── reference.go               ← M41：Session References + File References（mention 解析/PreparedReferencedMessage/跨会话引用/7 错误码）
│   │   ├── query.go                   ← S04：Session Query（title/创建时间/活跃时间搜索 + 分页 + live/persisted 双源）
│   │   ├── title.go                   ← S04：Session Title（首轮后异步 LLM 生成标题 + 持久化 sidecar + 原子 observation）
│   │   └── telemetry.go               ← S07：Session Telemetry（每会话 token/tools/steps 指标导出 + ledger/ops 双通道）
│   │
│   ├── scope/                         ← M10：Scope 作用域原语
│   │   └── scope.go                   ← ScopeKey + createScope + scopeOf + 分层合并规则
│   │
│   ├── sysprompt/                     ← M06 + M07 + M42：System Prompt 组装器 + Prompt Context
│   │   ├── section.go                 ← PromptSection 接口（GetOrder() + Render()）+ 注册表（按 order 排序）
│   │   ├── context.go                 ← M42：PromptContext 注册（动态上下文 + change-only 持久化 + compaction 保留标记）
│   │   ├── assembler.go               ← Assemble() → 完整 system prompt message + PromptContexts 渲染 + tool schemas 列表
│   │   └── sections/
│   │       ├── persona.go             ← 角色设定（严格拷贝原版 section 文本）
│   │       ├── policy.go              ← 通用 policy section（严格拷贝原版）
│   │       ├── runtime_ctx.go         ← M07：Runtime-Context Snapshot（每次请求前动态渲染 plan/goal/approval/sandbox/preset）
│   │       └── skill_catalog.go       ← M27 子项：<available_skills> catalog（pre-step diff 后注入）
│   │
│   ├── tools/                         ← M08 + M09：工具管线（核心）
│   │   ├── types.go                   ← ToolDefinition{Name/Schema/Execute/Output/FinalizeContent/IsConcurrencySafe} + ToolOutput + execution 类型
│   │   ├── schema.go                  ← M09：ValueSchemaSpec DSL → 编译为 JSON Schema
│   │   ├── define.go                  ← DefineTool() 类型安全构造器
│   │   ├── registry.go                ← ToolRegistry（分层 Scope + Restriction 白名单/黑名单）
│   │   ├── pipeline.go                ← 四级 Waterfall 链：pre → execute → post → result（顺序常量写死）
│   │   └── builtin/
│   │       ├── bash.go                ← M22 消费端：bash + bg 工具定义
│   │       ├── fs.go                  ← M23 消费端：read/grep/ls/glob/write/edit/patch/mkdir/mv/rm 工具定义
│   │       ├── web.go                 ← M35 消费端：web_search / web_fetch 工具定义
│   │       ├── skill.go               ← M27 消费端：skill({name}) 工具定义
│   │       ├── plan.go                ← M16 消费端：exit_plan_mode 工具定义
│   │       ├── goal_*.go              ← M17 消费端：goal_set / goal_edit / goal_pause / goal_resume /
│   │       │                              goal_mark_complete / goal_report_blocker（6 文件）
│   │       └── todo.go                ← M19 消费端：todo_write 工具定义
│   │
│   ├── agent/                         ← M11：Agent 抽象 + 注册表
│   │   ├── types.go                   ← Agent 接口（Run/Followup/Steer/Inject/Dispose）+ AgentStatus + AgentOptions
│   │   ├── inbox_events.go            ← M03：agent/inbox-turn、agent/inbox-step 事件定义
│   │   ├── registry.go                ← AgentRegistry：register/list/get + create/resume
│   │   └── runtime_types.go           ← agent/* 全部事件：pre-step、request、turn-stopping、error 等
│   │
│   ├── agentloop/                     ← M12：ReactLoopAgent（心脏）
│   │   ├── react_loop_agent.go        ← ReactLoopAgent 结构体 + Run 主循环 + 并发控制（pending turn rejection）
│   │   ├── turn.go                    ← turn 生命周期：start → claim inbox → 多 step → stopping → end
│   │   ├── step.go                    ← step 生命周期：派生消息 → pre-step → request waterfall → llm/stream → assistant chunks → tool calls → end
│   │   ├── tool_calls.go              ← 批量 tool call 并发调度（parallel-safe/serial、pending/drained 事件）
│   │   ├── llm_call.go                ← agent/request waterfall → LLMService 调用 + streaming 处理 + token meter 累加
│   │   └── constants.go               ← MAX_PARALLEL_TOOL_CALLS、DEFAULT_MAX_TOKENS 等常量 + waterfall 顺序表（定序不可改）
│   │
│   ├── plan/                          ← M16：Plan Mode
│   │   ├── types.go                   ← PlanModeState{Active, Pending}
│   │   ├── fold.go                    ← foldPlanMode() 从事件日志派生
│   │   ├── controller.go              ← PlanModeController：get/set + pending 预提交
│   │   ├── prompt_section.go          ← plan:policy Prompt Section 注册（order=500，严格拷贝原版文本）
│   │   └── exit_tool.go               ← exit_plan_mode 工具 → 调用 User Questions plan-review 审批
│   │
│   ├── goal/                          ← M17 + M18：目标系统 + Round Driver
│   │   ├── types.go                   ← GoalPhase / GoalView / GoalBlockReason / GoalActivation + CAS revision 字段
│   │   ├── event.go                   ← goal/change 事件类型
│   │   ├── fold.go                    ← foldGoalChange() 严格 fold + 检测 CAS 跳号
│   │   ├── service.go                 ← GoalService：create/edit/pause/resume/markComplete/reportBlocker（CAS 原子写入 session log）
│   │   ├── round_driver.go            ← M18：Goal Round Driver（订阅 turn-stopping → goal.active → 注入续轮提示）
│   │   ├── prompt.go                  ← renderGoalRoundPrompt() + renderGoalSnapshot()
│   │   └── tools/                     ← goal_set/goal_edit/... 等 6 个工具定义（与 tools/builtin/goal_*.go 对齐）
│   │
│   ├── todo/                          ← M19：待办系统
│   │   ├── types.go                   ← TodoItem（status：pending/in_progress/completed）
│   │   ├── event.go                   ← todo/write 事件
│   │   └── tool.go                    ← todo_write 工具定义（整体替换）
│   │
│   ├── userq/                         ← M14：User Questions 接缝（Plan Mode 审批唯一通道）
│   │   ├── types.go                   ← AskUserQuestionItem / Option / Intent{kind:plan-review, approve} / Request / Answer
│   │   ├── errors.go                  ← UserQuestionError + 错误码枚举
│   │   └── runtime.go                 ← UserQuestionRuntime：Answerer 注册链 + ask() waterfall（headless SDK 场景回调函数形式注册 Answerer）
│   │
│   ├── approval/                      ← M13：审批接缝（fail-closed 默认）
│   │   ├── types.go                   ← ApprovalRequestId / ApprovalOutcome（allowed-once/rejected/cancelled/unavailable 闭合）
│   │   ├── policy.go                  ← ApprovalPolicy（ask/never）+ setApprovalPolicy 单写路径 + foldEffectiveApprovalPolicy
│   │   ├── events.go                  ← approval/asked、approval/decided 审计事件（严格成对写入 session log）
│   │   └── service.go                 ← ApprovalService：request(req) → never policy 先 enforce → answerer waterfall → 成对落日志
│   │
│   ├── permission/                    ← M20：Permission Presets（组合 knob）
│   │   ├── types.go                   ← PresetSpec / Config（默认表 workspace-write / danger-full-access）+ CUSTOM_PRESET
│   │   ├── fold.go                    ← current() fold 派生当前 preset（从 sandbox + approval 旋钮反推）
│   │   └── service.go                 ← PermissionPresetService：resolve / set(session, name) / names / optionOf
│   │
│   ├── sandbox/                       ← M21：Sandbox 接缝
│   │   ├── types.go                   ← SandboxMode（read-only/workspace-write/danger-full-access）+ 执行 policy
│   │   ├── policy.go                  ← SandboxPolicyService：foldEffectiveSandboxMode + setSandboxMode + resolve()（per-call policy，非 provider 固定）
│   │   ├── backend.go                 ← SandboxProvider 抽象接口（confine() → ConfinedArgv）+ 本地 Backend（MVP Windows：退回 danger-full-access + TODO 注释）
│   │   └── runner_failure.go          ← RunnerFailureRule + denialSignatures + 分类算法
│   │
│   ├── shell/                         ← M22：Shell 接缝
│   │   ├── types.go                   ← ShellExecRequest → resolve → ShellExecSpec + ShellRunResult（exitCode/signal/timedOut/aborted/stdout/stderr）
│   │   ├── resolver.go                ← resolve(request) 字段补齐 + 上限（timeout、stdoutMaxBytes）
│   │   ├── executor.go                ← ShellExecutor 接口（resolve + start + run）+ 本地 Executor 实现
│   │   ├── sandboxed.go               ← 沙箱执行路径：先 confine 再 spawn + runnerFailure/denial 分类
│   │   ├── env_scrub.go               ← 密钥环境变量清洗 + DSH_* 管理命名空间合并顺序
│   │   └── subprocess.go              ← M26：子进程机制（进程组、信号、cancel、Windows/Linux 平台差异封装）
│   │
│   ├── fs/                            ← M23：Filesystem 接缝
│   │   ├── types.go                   ← FsTarget / FsTargetKey / FsVersion / FsInfo / FsDirEntry / WriteIntent（createIfAbsent / replaceIfVersion）
│   │   ├── interface.go               ← FileSystem 抽象接口：resolve / stat / lstat / listDir / readText/streamText/readBytes +（带守卫）writeText/editText/delete + processPath / contains
│   │   ├── errors.go                  ← FS_TOO_LARGE / FS_STALE_VERSION / FS_NOT_OBSERVED / FS_PERMISSION_DENIED / FS_IO_ERROR 标准错误码
│   │   ├── local.go                   ← LocalFileSystem：本地磁盘实现（canonical 路径规范化 + stat 版本令牌从 high-res mtime+inode 派生）
│   │   └── observation_policy.go      ← 观测策略：默认 write/edit 前先 stat 观察并加守卫，防止并发覆盖丢失
│   │
│   ├── spill/                         ← M24：Spill 存储 + 工具结果溢出策略
│   │   ├── types.go                   ← SpillOwner / SpillSource / SaveTextSpill / SpillRef / SpillLocator
│   │   ├── store.go                   ← SpillStore 抽象接口：saveText
│   │   ├── local.go                   ← LocalSpillStore：session-scoped 私有目录 + 独占写（open wx 0600）防 planted symlink
│   │   └── policy.go                  ← SpillPolicy：注册为 tools/post-execute waterfall handler + maxInlineBytes 阈值 + 失败 best-effort
│   │
│   ├── jobs/                          ← M25：Job Runtime 后台任务统一生命周期
│   │   ├── types.go                   ← JobKind（bash/subagent 可扩展）+ JobStatus + JobSnapshot / JobStart / JobHooks / JobOutcome
│   │   └── registry.go                ← JobRegistry：start / read / wait / kill / list + owner agent dispose 时自动 cancel 所有 owned job
│   │
│   ├── skill/                         ← M27：技能系统
│   │   ├── types.go                   ← SkillSummary / SkillCandidate / SkillDefinition + SkillProvider 接口（List/Get）
│   │   ├── registry.go                ← SkillRegistry：RegisterProvider / Register / List / Get（分层 scope + rank + 缓存失效）
│   │   ├── provider_fs.go             ← LocalFilesystemProvider：6 层根目录扫描 + frontmatter 解析 + fsnotify 实时监听
│   │   └── catalog.go                 ← catalog diff 检测：agent/pre-step 时比较上一次快照 + 变更则 agent.inject() 写入 <available_skills>
│   │
│   ├── commands/                      ← M15：人类命令注册表（非模型调用的状态切换入口）
│   │   ├── types.go                   ← CommandDefinition / CommandInvocation / CommandResult（success text/error）+ ParsedCommand 解析视图
│   │   ├── runtime.go                 ← CommandRuntime：register（global + agent-scoped 阴影覆盖）+ list + parseCommand + run
│   │   └── builtin/                   ← 内置命令（/plan、/goal、/todo、/approval、/permissions、/settings）
│   │       ├── plan_cmd.go
│   │       ├── goal_cmd.go
│   │       ├── todo_cmd.go
│   │       ├── approval_cmd.go
│   │       ├── permission_cmd.go
│   │       └── settings_cmd.go
│   │
│   ├── settings/                      ← M28：Settings（命名空间 + schema + secret + 修订锁）
│   │   ├── types.go                   ← SettingsNamespace / SettingsScope{T}{get/watch/update/replace} + SettingsDescriptor + PathOp + SettingsApplies(live/restart)
│   │   ├── registry.go                ← SettingsRegistry：register(ns, schema, opts) → 返回 Scope；每个 namespace 独立 schema 校验 + base 层
│   │   ├── provider.go                ← SettingsProvider 抽象：describe(redactSecrets) / readSectionRaw / writeSectionRaw / replaceByPathOps / onReferenceUpdated
│   │   └── file.go                    ← Local File Provider：单 JSON/YAML 用户文档，sections 按 namespace 分层存储，外部文件编辑监听
│   │
│   ├── credentials/                   ← M29：Credentials（凭证接缝 + 授权流 + 本地加密实现）
│   │   ├── types.go                   ← CredentialRef（POSIX env name branded）/ ResolvedCredential{value, source} / CredentialInfo
│   │   ├── seam.go                    ← CredentialProvider 抽象接口：resolve（per-call resolve，不缓存！）+ describe + set + unset
│   │   ├── authorization.go           ← AuthorizationService：registerFlow + list + describe + cancel + begin（异步授权流 + DUPLICATE_FLOW/NO_FLOW/ALREADY_IN_FLIGHT 错误码）
│   │   └── local.go                   ← S06：Local Credentials Provider（4 层 env：process → user file → project file → managed store）+ 可选系统密钥链加密
│   │
│   ├── persistence/                   ← M30 + M31：持久化接缝 + JSONL + SQLite
│   │   ├── interface.go               ← M30：SessionPersistence 接口：create / prepare / load / inspect / list / append / flush / locate
│   │   │                              ←   + errors：SessionFormatUnsupportedError / SessionPersistenceCorruptionError
│   │   ├── jsonl/                     ← M31：JSONL 持久化后端（零依赖 MVP 默认）
│   │   │   ├── format.go              ← header-line 格式定义 + 事件行格式 + 批量 flush 窗口算法
│   │   │   ├── backend.go             ← JsonlPersistence 实现 + 孤儿 turn/start 合成 interrupted（内存，物理日志不截断）
│   │   │   └── recovery.go            ← Crash Recovery 流程
│   │   └── sqlite/                    ← S03：SQLite 持久化后端（modernc.org/sqlite 纯 Go）
│   │       ├── schema.go              ← SCHEMA_VERSION pragma + sessions + events 表结构
│   │       └── backend.go             ← SQLitePersistence 实现（事务保证 batch atomic）
│   │
│   ├── compaction/                    ← S01：上下文压缩（预留 Session.SurfaceOp 字段 + DeriveMessages 分支）
│   │   ├── types.go                   ← CompactionEngine 抽象接口 + CompactionPressure 计算
│   │   └── basic/                     ← Basic 实现：pressure 触发 + LLM 摘要 + surfaceOp 替换，不修改源事件
│   │       └── engine.go
│   │
│   └── subagent/                      ← S02：子代理接缝（先有 in-process spawn，确保 session header.origin/delegationDepth 有正确使用方）
│       ├── types.go                   ← SubagentStartRequest / Workflow（如启用）/ Provider 接口
│       ├── runtime.go                 ← SubagentRuntime：start / followup / cancel
│       └── provider/
│           └── inprocess.go           ← 进程内 spawn：创建新 Agent 实例，parentSession 建立血统
│
│   ├── workspace/                     ← S11：Workspace Registry（工作区目录持久化 + 会话分组 + pending-mutation 两阶段恢复）
│   │   ├── types.go                   ← Workspace{id,path(canon),title,sessionIds[],createdAt,updatedAt} + WorkspaceId branded
│   │   ├── registry.go                ← WorkspaceRegistry：create(canon 唯一)/get/list/resolveByPath/delete + 中断 mutation 恢复
│   │   └── membership.go              ← attachSession/detachSession/insertSessionBefore + status() 实时目录检查
│
│   ├── web/                           ← M35：Web 能力接缝（SSRF 防护 + Provider 自动选择 + web/errors）
│   │   ├── types.go                   ← WebFetchRequest / WebSearchRequest + SSRFPolicy / Provider 自动选择 ambiguous 报错
│   │   ├── ssrf.go                    ← private IPv4/IPv6/NAT64/同源重定向重查算法
│   │   └── provider/
│   │       └── http.go                ← HTTP fetch 后端
│
│   ├── mcp/                           ← S14：MCP Client 接缝（Model Context Protocol）
│       ├── transport.go               ← MCPTransport 抽象（stdio/http/jsonrpc；MVP 只实现 stdio）
│       ├── client.go                  ← MCPClient{connect/listTools/callTool/listResources/readResource/listPrompts/getPrompt}
│       └── adapter.go                 ← MCPToolAdapter：MCP tool → ToolRegistry.ValueSchemaSpec 自动映射
│
├── internal/                          ← 内部实现（不对外 import）
│   ├── harnessctx/                    ← M32：HarnessContext（一次定型骨架）
│   │   ├── context.go                 ← HarnessContext 结构体 + services 注册表（单实现 seam：重复注册 panic）
│   │   ├── events.go                  ← EventBus：普通 emit + waterfall emit + 错误隔离（throwing listener 只 log 不 propagate）
│   │   ├── chains.go                  ← PreStepChain / RequestChain / ToolPreChain / ToolPostChain 等所有 Waterfall Chain 类型 + 定序常量
│   │   ├── lifecycle.go               ← LifecycleManager：start/stop/dispose 有序化 + owner 清理（dispose 级联 scope）
│   │   └── errors.go                  ← HarnessError 基类 + 稳定错误码分类（跨 RPC 可传递）
│   │
│   ├── invariant/                     ← M33：Invariant 运行时不变量校验
│   │   ├── registry.go                ← InvariantRegistry：register(packageName, installer) + allowlist/blocklist + 独立 fiber
│   │   ├── errors.go                  ← InvariantError{code:INVARIANT, packageName, message}
│   │   └── core_checks.go             ← 8 条核心不变量（M33 列表）：seq 连续、turn/step 配对、approval 成对、goal CAS 不跳号、tool id 一一对应、JSONL/SQLite 一致性、session scope id 唯一、agent registry 实例匹配
│   │
│   ├── telemetry/                     ← S05：OpenTelemetry 埋点（轻量 interface，可零依赖）
│   │   ├── tracer.go                  ← StartSpan/EndSpan + 每处埋点 hook：turn/step/llm request/tool execute/goal change/job start
│   │   ├── meter.go                   ← 计数器 + histogram：tokens in/out、tool latency、steps per turn
│   │   └── noop.go                    ← 默认 Noop 实现（无 OTel 时零开销）
│   │
│   └── testutil/                      ← 测试辅助工具
│       ├── mock_llm.go                ← Mock LLM（脚本式响应，无需真实 API Key，可模拟 tool call、reasoning、流式 chunk）
│       ├── fake_fs.go                 ← Memory FS 后端（FS 操作全内存，快速测试 write edit 守卫）
│       ├── fake_sandbox.go            ← Noop Sandbox（不做 confine，用于测试非安全路径）
│       ├── fixed_approval.go          ← 预设 approval answer（allowed-once 永远通过，用于测试工具链）
│       └── fixture.go                 ← 常用 fixture 构造：空 Session、带 Plan Mode 开启的 Agent、带 10 轮历史的 Agent
│
├── sdk/                               ← 对外 Go SDK（你的核心集成入口，对标 Python SDK）
│   ├── client.go                      ← HarnessClient：New()（配置所有 MUST 子系统，一次性搭好骨架）
│   ├── agent.go                       ← SDK 层 Agent：Run/Followup/Steer/SetPlanMode/SetGoal/ListTodo 等高级 API
│   ├── commands.go                    ← SDK 命令入口：ExecuteCommand(name, input) → 调用 commands 注册的命令
│   ├── callbacks.go                   ← SDK 回调接口：OnUserQuestion（用户问题，返回 Answer）、OnToolApproval（工具审批）
│   ├── notifications.go               ← 通知流：StepDone / ToolCallResult / PlanApproved / GoalChanged / JobProgress
│   └── types.go                       ← RunResult / RunOptions / Notification / UserQuestion 等 SDK 层稳定类型
│
│   └── tests/                             ← 集成测试（全部 MUST + 关键 SHOULD 的回归测试，必须在一次性发布时通过）
│       ├── harnessctx_invariant_test.go   ← M33：8 条核心不变量触发测试（故意触发每条都应抛 INVARIANT）
│       ├── session_event_vocab_test.go    ← M02：45+ event 类型 append + deriveMessages + reference/resolved 正确派生
│       ├── session_projection_test.go     ← M40：Projection 不变引用零下游 + stateVersion 缓存失效机制 + snapshot 一致裁剪
│       ├── session_reference_test.go      ← M41：跨会话 mention 解析 + SELF_REFERENCE/TOO_MANY 安全约束 + PreparedMessage
│       ├── prompt_context_test.go         ← M42：PromptContext change-only 持久化 + compaction 时保留最后一次快照
│       ├── agent_loop_full_test.go        ← M11+M12：完整 Turn → Step → Tool Call → Tool Result → 下一步测试
│       ├── planning_capability_test.go    ← M13~M19：Plan Mode on → exit_plan_mode 审批 → Goal 多轮续 → Todo 更新 全闭环
│       ├── tools_waterfall_test.go        ← M08：pre/execute/post/result 四级 waterfall（顺序、拦截、错误传播）
│       ├── sandbox_approval_test.go       ← M13+M21+M20：三种预设（read-only/ask、full-access/never、custom）切行为一致
│       ├── fs_write_guard_test.go         ← M23：stale version / createIfAbsent 并发守卫不丢写入
│       ├── shell_timeout_cancel_test.go   ← M22+M26：timeout 触发后进程真的被 kill、cancel 真的杀子进程
│       ├── spill_large_output_test.go     ← M24：10MB bash 输出真的被 spill 到文件，内联只有预览
│       ├── skill_fsnotify_test.go         ← M27：运行中新建 SKILL.md，下一次 pre-step 自动出现在 catalog 中
│       ├── settings_cas_secret_test.go    ← M28：同 revision 并发写只有一个成功 + describe(redact) 绝对不含 secret 值
│       ├── credentials_hot_update_test.go ← M29：请求之间更新密钥，下一次 LLM 请求使用新密钥（无需 restart）
│       ├── persistence_crash_test.go      ← M31：写到一半模拟 crash，重启后 events 完整，孤儿 turn 被标记为 interrupted
│       ├── job_owner_cleanup_test.go      ← M25：agent dispose，owned bg jobs 全部被 cancel 且 done 被 await
│       ├── workspace_pending_mutation_test.go  ← S11：delete/create 中途 kill 进程，重启后 pending 标记正确恢复
│       ├── llm_retry_compact_test.go      ← S12：context-overflow → 自动 compact → request-error 返回 retry → step 重跑成功
│       ├── retention_unicode_test.go      ← S13：含 emoji/组合字符/代理对的中文文本，三种截断都不含非法 Unicode
│       ├── mcp_stdio_echo_test.go         ← S14：stdio 连接 echo MCP Server → 自动注册 tool → 模型可调用成功
│       ├── sdk_integration_test.go        ← SDK 完整：注册自定义工具 → Run → 收到 notifications → 审批回调 → 正确返回
│       └── fixtures/

---

### 10.6 一次性复刻到位的工作量重新估算

| 维度 | 工作量（1 名熟练 Go 开发） | 备注 |
|------|--------------------------|------|
| **MUST 级 34 项** | 14 ~ 18 人周 | 覆盖簇 1~5 全部 + HarnessContext + 8 条不变量 + 30 事件类型 |
| **SHOULD 级 7 项** | 3 ~ 5 人周 | Compaction（要与 SurfaceOp 同步）+ Subagent（in-process）+ SQLite + Query/Title + OTel + 本地加密 Credentials + Session Telemetry |
| **集成测试 / E2E 14 套件** | 2 ~ 3 人周 | 14 份测试用例必须 day-1 写，作为等价性回归守护 |
| **Go SDK + 文档注释** | 1 ~ 2 人周 | SDK API 稳定、示例代码、每个导出类型的中文注释（用户规则要求） |
| **一次性合计** | **20 ~ 28 人周（约 5 ~ 7 个月）** | 100% 等价复刻 DeepSeek Agent 内核能力，不分步骤，一次发布 |
| **加 1 名开发并行** | 12 ~ 16 人周（约 3 ~ 4 个月） | 簇 1 + 簇 4 一人，簇 2 + 簇 5 一人，簇 3 一人，最后集成 |

---

### 10.7 一次性复刻到位的交付标准（验收清单）

完成后，运行 `go test ./tests/...` 全部通过，且以下 8 条必须 100% 满足：

```text
验收 1（等价行为：规划三件套）：
  SDK 方式：EnablePlanMode=true + 设置 Goal → 发送 prompt → 观察：
  - 第一步模型先输出计划草稿 → exit_plan_mode 触发审批回调 → SDK 回调 approve → 退出计划模式
  - Goal 处于 active → 每轮 turn-stopping 后自动续轮（无需用户再输入），直到 Goal 完成或被阻塞
  - 任何步骤中模型可调用 todo_write 更新三态进度列表

验收 2（安全默认：fail-closed）：
  默认预设 workspace-write + ask：
  - bash 写文件触发 approval/asked，无回调答案 → unavailable → 工具返回 isError（fail-closed）
  - 切预设为 danger-full-access + never：无需审批，bash 可任意写，approval policy 永久拒绝

验收 3（文件并发：不丢失写入）：
  两个 goroutine 同时 edit 同一个文件（不同位置）：
  - 只有一个能成功（基于相同版本守卫）
  - 失败方拿到 FS_STALE_VERSION，重新 observe 后可成功
  - 最终磁盘内容是两次 edit 的正确合成（不互相覆盖）

验收 4（长对话：上下文压缩）：
  50 轮对话，触发 pressure → compactIfNeeded：
  - 历史 token 从 180k → 压缩到 50k 以下
  - 重新 load 会话后 DeriveMessages 与压缩前"等价但变短"
  - 源事件日志丝毫未改（只有 SurfaceOp 元数据字段被加）

验收 5（Crash 恢复：不丢事件 + 标记孤儿 Turn）：
  第 7 轮 step 执行中间 Kill -9 进程：
  重启后 load(sessionId)：
  - 前 6 轮 + 第 7 轮已产生的 user/assistant/tool 事件全部保留
  - 第 7 轮 turn/end 为 synthetic interrupted，可正常 resume 新 turn

验收 6（技能：实时发现 + catalog 注入）：
  运行中新建 ./.dsh/skills/my-skill/SKILL.md：
  下一个用户消息到达后：
  - agent/pre-step 检测 diff → 写入 session hint <available_skills> 更新，my-skill 出现在列表中
  - 模型可调用 skill('my-skill')，返回 skill_instructions 包含 SKILL.md 全文 frontmatter 元数据

验收 7（密钥：热更新 + 不泄露）：
  配置 LLM Provider API Key 为环境变量：
  - 运行中更新 env 文件（不改进程环境变量）→ 下一个 LLM 请求 resolve credentials 取到新值（不重启）
  - Settings describe(redactSecrets:true) 返回内容对 secret 字段：value 永远空，只有 path 和 set boolean
  - 持久化 settings JSON 文件：只有 env 引用字符串，从不写实际密钥值

验收 8（不变量：防误操作）：
  构造 8 条非法场景（乱序 seq、只写 turn/start 不写 end、approval 只有 asked 没有 decided 等）：
  - 每一条都触发 invariant error（code=INVARIANT + packageName），绝不 silent 继续
  - Session 最终状态与错误信息一致
```

---

## 十一、参考资料与源码映射（完整 60+ 子系统对应）

本分析基于以下源码和文档（原版 DSH 项目 `D:\workspace\python_workspace\deepseek-harness`）：

| 领域 | 源码路径 | 文档路径 | 复刻等级 |
|------|---------|---------|---------|
| 架构总览 | - | `docs/architecture.md` | MUST（理解全局） |
| Agent Loop 驱动 | `packages/core/agent-loop/src/agent.ts` | `docs/subsystems/core.md` | 🔴 MUST (M12) |
| Turn/Step 流程 | `packages/core/agent-loop/src/agent.ts` | `docs/agent-lifecycle.md` | 🔴 MUST (M12) |
| Session 事件溯源 | `packages/core/session/src/types.ts` | `docs/subsystems/session.md` | 🔴 MUST (M02) |
| Session Header + Create Options | `packages/core/session/src/types.ts` | `docs/subsystems/persistence.md` 第 3 节 | 🔴 MUST (M02) |
| Branded IDs / Utilities | `packages/util/*` | `docs/subsystems/core.md` Branded 小节 | 🔴 MUST (M01) |
| LLM 词汇表与流式 | `packages/llm/llm/src/types.ts` | `docs/subsystems/llm-streaming.md` | 🔴 MUST (M04) |
| System Prompt 组装 | `packages/core/system-prompt/src/*.ts` | `docs/subsystems/system-prompt.md` | 🔴 MUST (M06/M07) |
| Tools Pipeline | `packages/core/tools/src/index.ts` | `docs/subsystems/tools.md` | 🔴 MUST (M08/M09) |
| Scope 作用域 | `packages/core/scope/src/index.ts` | `docs/subsystems/scope.md` | 🔴 MUST (M10) |
| Plan Mode | `packages/plan/plan-mode/src/index.ts` | `docs/subsystems/plan.md` | 🔴 MUST (M16) |
| Goal System + Round Driver | `packages/goal/goal/src/types.ts` + `goal-round-driver/src/prompt.ts` | `docs/subsystems/goal.md` | 🔴 MUST (M17/M18) |
| Todo System | `packages/todo/tool-todo/src/types.ts` | `docs/subsystems/todo.md` | 🔴 MUST (M19) |
| Skills System | `packages/skill/skill/src/index.ts` + `skill-filesystem/src/` | `docs/subsystems/skills.md` | 🔴 MUST (M27) |
| User Approval | `packages/interaction/user-approval/src/index.ts` | `docs/subsystems/approval.md` | 🔴 MUST (M13) |
| User Questions | `packages/interaction/user-questions/src/index.ts` | `docs/subsystems/user-questions.md` | 🔴 MUST (M14) |
| Human Commands | `packages/interaction/commands/src/index.ts` | `docs/subsystems/commands.md` | 🔴 MUST (M15) |
| Permission Presets | `packages/interaction/permission-presets/src/index.ts` | `docs/subsystems/permission-presets.md` | 🔴 MUST (M20) |
| Sandbox | `packages/sandbox/sandbox/src/*.ts` + `sandbox-local` | `docs/subsystems/sandbox.md` | 🔴 MUST (M21) |
| Shell / Bash | `packages/shell/shell/src/types.ts` + `bash-local` + `bash-sandbox` + `tool-bash` | `docs/subsystems/shell.md` | 🔴 MUST (M22/M26) |
| Filesystem | `packages/fs/fs/src/*.ts` + `fs-local` + `fs-observation-policy` + `tool-fs` | `docs/subsystems/filesystem.md` | 🔴 MUST (M23) |
| Spill Storage | `packages/spill/spill/src/types.ts` + `spill-local` + `spill-policy` | `docs/subsystems/spill.md` | 🔴 MUST (M24) |
| Job Runtime | `packages/jobs/jobs/src/types.ts` | `docs/subsystems/jobs.md` | 🟡 SHOULD (M25) |
| Settings + File Provider | `packages/settings/settings/src/*.ts` + `settings-file` | `docs/subsystems/settings.md` | 🔴 MUST (M28) |
| Credentials + Authorization | `packages/credentials/credentials/src/index.ts` + `credentials-local` + `authorization` | `docs/subsystems/credentials.md` | 🔴 MUST (M29) + S06 |
| Persistence Seam + Formats | `packages/session/session-persistence/src/*.ts` + `storage/jsonl` + `storage/sqlite-session` | `docs/subsystems/persistence.md` + `storage.md` | 🔴 MUST (M30/M31) + S03 |
| Compaction | `packages/compaction/compaction/src/*.ts` + `compaction-engine-basic` | `docs/subsystems/compaction.md` | 🟡 SHOULD (S01) |
| Subagent | `packages/subagent/subagent/src/types.ts` + 各 Provider | `docs/subsystems/subagent.md` | 🟡 SHOULD (S02) |
| Invariant Registry | `packages/runtime-diagnostics/invariants/src/index.ts` | `docs/subsystems/invariants.md` | 🔴 MUST (M33) |
| Session Query + Title | `packages/storage/session-query/src/*.ts` + `session-title` | `docs/subsystems/session-query.md` + `session-title.md` | 🟡 SHOULD (S04) |
| Session Telemetry | `packages/runtime-diagnostics/session-telemetry/src/*.ts` | `docs/subsystems/session-telemetry.md` | 🟡 SHOULD (S07) |
| OTel Telemetry | `packages/runtime-diagnostics/telemetry-otel/src/*.ts` | `docs/subsystems/session-telemetry.md` 集成 | 🟡 SHOULD (S05) |
| Workflow 引擎 | `packages/workflow/workflow/src/types.ts` + `tool-workflow` | `docs/subsystems/workflow.md` | 🟢 COULD (C01) |
| Extensions / Plugin Market | `packages/core/extensions/` | `docs/subsystems/extensions.md` | 🟢 COULD (C14) |
| LSP 集成 | `packages/lsp/lsp-client` | `docs/subsystems/lsp.md` | 🟢 COULD (C04) |
| Code Runtime | `packages/runtime/code-runtime` | `docs/subsystems/code-runtime.md` | 🟢 COULD (C10) |
| Schedule | `packages/schedule/schedule` | `docs/subsystems/schedule.md` | 🟢 COULD (C06) |
| Slots | `packages/core/slots` | `docs/subsystems/slots.md` | 🟢 COULD (C08) |
| Agent Team 协作 | `packages/team/agent-team` | `docs/subsystems/agent-team.md` | 🟢 COULD (C07) |
| Feedback | `packages/feedback/feedback` | `docs/subsystems/feedback.md` | 🟢 COULD (C07 / K09) |
| Token Meter | `packages/util/token-meter` | `docs/subsystems/token-meter.md` | 🔴 MUST (M34) |
| Subprocess 原语 | `packages/shell/subprocess` | `docs/subsystems/subprocess.md` | 🔴 MUST (M26) |
| Typert CLI | `packages/util/typert` | `docs/subsystems/typert.md` | 🟢 COULD (C05) |
| Web Tool（web_fetch） | `packages/web/web` + `tool-web` | `docs/subsystems/web.md` | 🟢 可作为工具自定义扩展 |
| Webhook / Server | `packages/webhook/*` + `web-server` | `docs/subsystems/webhook.md` + `web-server.md` | 🟢 COULD (K03/K13) |
| Conversation / Client / Terminal | `packages/client/*` + UI 包 | `docs/subsystems/conversation.md` / `terminal.md` / `client-modules.md` | ⚫ SKIP (K01-K06) |
| Agent Notes / .agents/ 机制 | 开发实践方法论 | `AGENTS.md` + `.agents/notes/` | ⚫ SKIP (K07) |
| Python SDK 参考 | `python/sdk/src/deepseek_harness/` | `python/sdk/README.md` | 参考：SDK 对外 API 形状 |
| Landlock / bwrap 平台沙箱 | `packages/sandbox/sandbox-local` Linux/macOS 专用 | `sandbox-local/README.md` | ⚫ SKIP (K10，后期可加 Provider) |

---

### 10.9 v2.0 二次扫描：60+ 子系统遗漏能力补全 & 决策矩阵终极版

> **说明**：v1.0 章节（10.1~10.8）基于首轮 40+ 子系统扫描得出，存在部分 「内核工具接缝被误降为 UI/可选」的分级误差。本节基于**剩余全部 60+ 子系统**（filesystem / credentials / skills / jobs / settings / commands / shell / terminal / subprocess / code-runtime / feedback / conversation / extensions / agent-team / workflow / session-title / slots / invariants 等）的完整逐字阅读重新校正。

#### 🔺 分级纠错记录（v1.0 → v2.0 升级的能力）

| 能力 | v1.0 定级 | v2.0 定级 | **升级根因（为什么它其实不可缺）** |
|------|----------|----------|----------------------------------|
| **Filesystem 接缝（ctx.fs + tool-fs + observation-policy）** | 🟡 SHOULD | **🔴 MUST M35** | Bash/Goal 中读/写/编辑文件是 Agent 最基本的"手脚"；`FsVersion` 新鲜度令牌 + `write/edit 意图` 是避免并发/过期写入的核心不变量；fs-observation-policy 提供的「先读后写」默认路径直接决定了 DeepSeek "不乱写文件"的产品体验 → 不是工具自定义扩展，而是**安全默认的地基** |
| **Subprocess 接缝（ctx.subprocess + scrubbedParentEnv + CollectedOutput + 树形 terminate）** | 🟡 SHOULD | **🔴 MUST M36** | Bash/Terminal/CodeRuntime/LSP 四项消费者最终都走到 subprocess；`DSH_*` 环境命名空间是子进程事实的唯一来源；`scrubbedParentEnv` 是凭证泄露防护第一层；树形 terminate（SIGTERM→grace→SIGKILL / taskkill /T）是后台任务存活与清理的关键 |
| **Shell/Bash 接缝（ctx.shell + resolve → spec + bash-local + tool-bash）** | 🟡 SHOULD | **🔴 MUST M37** | `ShellExecRequest → resolve() → ShellExecSpec` 是「显式大于隐式在包边界」原则；`run()` 返回的 `ShellRunResult` 5 字段正交（exitCode/signal/timedOut/aborted/timeoutMs）+ sandbox 信息，是 Bash 模型调用返回与 UI 渲染的契约；`tool-bash` 是 Agent 执行系统命令的第一工具，**没有 Bash = 规划后的执行环节全部失效** |
| **Settings 接缝（ctx.settings + namespace schema + path op + revision CAS + secrets 脱敏）** | 🟡 SHOULD | **🔴 MUST M38** | LLM provider 配置（route/model/reasoningEffort/credentialRef）、sandbox 默认模式、approval 默认策略、token 预算、skill roots 全部通过 settings 存储；`SettingsPathOp` 是密钥永不上线的**设计保证**（secret 字段永远走 path add/unset 而非 replace 整段回写）；`expectedRevision` CAS 避免并发设置冲突 → 没有 Settings = 所有能力接缝配置都要硬编码 |
| **Credentials & Authorization 接缝（ctx.credentials + ctx.authorization）** | 🟡 SHOULD | **🔴 MUST M39** | Settings 中 LLM/Web/Sandbox provider 的 credential 只是一个 `CredentialRef`（POSIX 环境变量名品牌），真正值由 credentials seam 按 per-request 解析 → LLM 每一次对话请求都必须 resolve 一次 credential 才能跑；`authorization.flow` 是 OAuth 类授权（Web、Git、远程 MCP）的必经之路 |
| **Skill System（ctx.skills + provider 注册表 + fsnotify + tool-skill）** | 🟡 SHOULD | **🔴 MUST M40** | Skill 不只是「可选指令」；project/.dsh/skills 中放了项目级的领域知识 + 业务约束，是 DeepSeek Code 「对特定项目表现得像项目专属 Agent」的核心；没有 Skill 注册表 = 无法响应 `.dsh/skills/*.md` 中的领域约束，Agent 退化为通用 LLM → **等价性崩塌** |
| **Commands（ctx.commands + slash command + handler）** | 🟢 COULD | **🔴 MUST M41** | `/plan off`、`/goal` 等核心交互路径走 command 而非模型消息；`command/run` & `command/done` 写入 session log，用户输入不会被当成普通 user/message 被模型误解 → 没有 Commands = Plan Mode/Goal Mode 的 UI/命令入口直接不可用；虽然是无头后端集成，但命令输入仍然是 SDK 层直接驱动 Agent 状态变更的**最高优先级通道** |
| **Spill Storage（ctx.spill + policy）** | 🟡 SHOULD | **🔴 MUST M42** | Bash stdout/stderr 超阈值、tool result 文本超阈值 → 不是截断，而是写 spill 文件 + 返回引用；这是长命令输出（如构建日志、grep 大量匹配）能够恢复完整内容的关键；没有 Spill = 长 Bash 输出永远丢失尾部，Agent 无法做"完整日志分析" |
| **Session Title（session/title 事件 + foldSessionTitle + LLM 辅助生成）** | 🟢 COULD | **🟡 SHOULD S08** | 会话标题虽然是展示元数据，但 `session/title` 是 session-query 按标题搜索的前提；无头后端场景下，按语义标题检索历史会话是会话管理的必要能力 → 降级为 SHOULD（可存人类消息前 30 字 fallback，后期再 LLM） |
| **Feedback（message-feedback Storage Domain）** | 🟢 COULD | **🟡 SHOULD S09** | 消息反馈是训练 / RLHF 数据采集的必经通道；虽然不在 Agent 内核热路径上，但 MessageFeedbackVersion CAS + session sidecar 存储模式与 Storage Domain 一致，实现成本低 → 与 Storage Domain 一起落地避免返工 |
| **Terminal PTY（ctx.terminals + backend）** | 🟢 COULD | **🟡 SHOULD S10** | 持久终端会话是开发 Agent 的"长会话 shell"能力；一个进程内 backend + 有界滚动回滚 + TerminalSendOperation 独占发送即可；无头后端可把终端输出作为 tool 结果返回 → 后期集成 MCP/Tmux 更轻松 |
| **Jobs Runtime（ctx.jobs + producer）** | 🟡 SHOULD | **🟡 SHOULD S11（原 S06）** | 后台任务视图：bash 长时间运行（>2s）的 Agent 侧取消、输出增量读取、owner 生命周期绑定；与 Terminal 共享 JobSnapshot 视图 → 与 Bash/Subprocess 一起做，不新增额外数据结构成本 |
| **Workflow Engine（ctx.workflowEngine + 脚本 + agent()）** | 🟢 COULD | **🟡 SHOULD S12（升级）** | 工作流脚本通过 subagent seam 启动子 Agent，支持 parallel/pipeline 组合子；它与 Code Runtime + Subagent 共享 3 个接缝（subprocess/subagent/codeRuntime）；一次性在 SHOULD 中做完，避免 CodeRuntime 后返工再实现 workflow 专用的 `agent()` 绑定 → 耦合度较高，适合一起落地 |

#### 🔺 误降级纠正（v1.0 → v2.0 降级的能力 / 保持不变）

| 能力 | v1.0 | v2.0 | **降级 / 保持 根因** |
|------|------|------|-------------------|
| **Code Runtime（PTC 模式）** | 🟢 COULD | **🟢 COULD C10（保持）** | PTC 模型需要 provider 侧支持；无头后端 Agent 先用普通 tool 调用等价能力；升级仅当需要 "模型写 JS/Python 脚本再用 worker 调用 tool" 时 |
| **Conversation / Slots / Client** | ⚫ SKIP | **⚫ SKIP K01-06（保持）** | 纯 Web UI 渲染层；无头 Go 后端集成场景下完全不需要；Chat Node / Trajectory Node 的折叠用 `deriveMessages()` + 业务自写 UI 即可 |
| **LSP** | 🟢 COULD | **🟢 COULD C04（保持）** | LSP 是语言级生产力增强；Agent 能跑 grep/glob/AST 解析工具即可 |
| **Agent Teams** | 🟢 COULD | **🟢 COULD C11（保持，新增条目）** | 实验性功能，team/message/task 基于 Lead Session foldTeam() 派生；可在 Subagent + Storage Domain 完成后按同样模式加 |
| **Extensions（动态 Cordis 插件）** | 🟢 COULD | **🟢 COULD C14（保持）** | 模型定义和运行插件需要完整 Runtime 沙箱；v2.0 的插件体系用「Go Plugin 接口 + 组合注册」替代 Cordis，不需要运行时 JS 插件 |

---

### 10.10 终极版：不可分割的 7 大核心能力簇（簇内必须一次性到位）

> **v2.0 关键修正**：将原来的 5 簇扩展为 **7 簇**，新增「**簇 6：文件 & 进程执行簇**」（Bash/FS/Subprocess/Spill 强耦合）和「**簇 7：配置 & 凭证 & 技能接缝簇**」（Settings/Credentials/Skills/Commands 强耦合），这两簇是之前首轮扫描漏掉的核心耦合块。

```text
┌───────────────────────────────────────────────────────────────────────────────┐
│          7 大不可分割能力簇（内部强耦合 → 必须簇内一次到位）                     │
├───────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  🔴 簇1: Agent 内核驱动簇 (M01-M08)  ──┐                                     │
│    Session(事件溯源+投影)              │                                     │
│    Agent Registry + Loop + Inbox       │◄──┐                                  │
│    LLM 流式 + SystemPrompt 组装        │   │ 依赖                           │
│    Tools Waterfall 四级链 + Scope      │   │                                  │
│                                        │   │                                  │
│  🔴 簇2: 规划能力簇 (M09-M15)          │   │                                  │
│    Plan Mode(审批退出)                 │   │                                  │
│    Goal(状态机+续轮驱动+6工具)         │   │ 全部依赖簇1 Waterfall 与 Log      │
│    Todo(整体替换写入)                  │   │ 工具入口走簇1 ctx.tools            │
│    User Questions 接缝(用户澄清)       │   │ Prompt 走簇1 ctx.systemPrompt     │
│    Session Projections / References    │◄──┘                                  │
│    Prompt Context 动态快照             │                                         │
│                                                                               │
│  🔴 簇3: 工具执行 & 安全簇 (M16-M24)  ◄──┐                                    │
│    Sandbox 接缝(3模式 + 审批)            │                                    │
│    Approval(ask 审批 + 预设 + UI)       │                                    │
│    Permission Presets(组合旋钮)         │                                    │
│    Attachment(图片引用/消费)            │◄──┐                                 │
│    Invariant Registry(不变量校验)       │   │ 依赖                            │
│    Token Meter(token 计量与预算)        │   │                                 │
│    Message Feedback(CAS 侧车) S09       │   │                                 │
│                                         │   │                                 │
│  🟡 簇4: 持久化 & 可恢复簇 (M25-M32)     │   │                                 │
│    SessionHeader + Persistence 接缝     │   │ 全部读写簇1 Session              │
│    Storage Domain(3层KV+版本校验)       │   │ Compaction 重写簇1 Log           │
│    JSONL + SQLite 后端 S03              │   │ Telemetry 消费簇1 stream/event   │
│    Compaction(LLM摘要+表面替换) S01     │   │                                 │
│    Session Query/FTS5 S04               │───┘                                 │
│    Session Title S08                    │                                     │
│    Session Telemetry + OTel S05/S07     │                                     │
│    Crash Repair(orphan turn 关闭)        │                                     │
│                                                                               │
│  🟡 簇5: 子 Agent & 多模态工具簇 (S02/S10/S11/S12) ◄──┐                       │
│    Subagent 接缝(3后端+process/ACP/fork)              │                       │
│    Terminal PTY(S10) + Jobs Runtime(S11)              │  依赖簇3 Approval 与   │
│    Code Runtime + Workflow Engine(S12)                │  簇4 Persistence      │
│    MCP Client(新增 S13)                               │                       │
│    Workspace Registry(新增 S14)                       │                       │
│    LLM Retry + Output Retention(新增 S15/S16)         │                       │
│                                                       └─────────┐              │
│                                                                 │              │
│  🔴 簇6: 文件 & 进程执行簇 (M35-M37, M42) ◄─────────────────────┤ 依赖       │
│    Filesystem(ctx.fs + fs-local + obs-policy + tool-fs)         │ 簇3 Sandbox │
│    Subprocess(ctx.subprocess + scrub + tree terminate + collect)│ 审批政策    │
│    Shell/Bash(ctx.shell + resolve + bash-local/sandbox + tool)  │             │
│    Spill(ctx.spill + spill-policy + 工具结果溢出)                │             │
│                                                                 │             │
│  🔴 簇7: 配置 & 凭证 & 技能接缝簇 (M38-M41) ◄───────────────────┘             │
│    Settings(ctx.settings + schema + pathop + CAS + secrets redact)            │
│    Credentials & Authorization(ctx.credentials.resolve + flow)               │
│    Skills(ctx.skills + provider registry + fsnotify + catalog + tool-skill)   │
│    Commands(ctx.commands + slash + handler + command/* 事件)                  │
│                                                                               │
│  每簇内部: 「服务定义 + 1+个服务提供 + 1+个消费端(工具/事件监听)」              │
│  七簇之间: 有向依赖箭头单向不循环，等价性保证 100% 覆盖                        │
└───────────────────────────────────────────────────────────────────────────────┘
```

---

### 10.11 终极版：MUST / SHOULD / COULD / SKIP 全清单（附编号与耦合簇归属）

#### 🔴 MUST 级 48 项（v1.0=42，v2.0 新增 6，升级 6）

| 编号 | 能力名 | 归属簇 | 关键数据结构 / 接口 | 等价性必复原因 |
|------|--------|-------|-------------------|--------------|
| **M01** | Branded ID 类型封装 | 簇1 | `type SessionID string; type ToolCallID string; …` + 各自 New/Parse | 所有跨包契约不混传 |
| **M02** | Waterfall 中间件链原语 | 簇1 | `type WaterfallFunc[T any] func(ctx Context, payload T, next func() (T, error)) (T, error)` + Chain 组合 | agent/pre-step/request/tools 四级链 + approval + plan/goal 注入全部复用一套实现 |
| **M03** | Scope 分层注册表原语 | 簇1 | `type ScopeKey = any; type Layer = map[string]any; mergeLayers(global + scoped)` | 工具/技能/命令/凭证 4 大注册表全部依赖 |
| **M04** | Session 事件溯源 & 词汇表 | 簇1 | 45+ 事件类型（见下文 **10.12**）+ `type SessionEvent struct{seq,time,type,data, surfaceOp?, sourceEventSeqs?}` | 所有状态从 log 派生；缺一类事件 = Resume/Fork/投影不一致 |
| **M05** | Session 派生投影函数族 | 簇1 | `deriveMessages()` + `foldRequestHeader()` + `foldEffectiveSandboxMode()` + `foldEffectiveApprovalPolicy()` + `foldGoalChange()` + `foldTodoWrite()` + `foldPlanMode()` + `foldPermissionPreset()` + `foldSessionTitle()` | 每个扩展能力写入 Log 的事件必须有对应 fold；没有 fold = 状态恢复失败 |
| **M06** | SessionHeader 元数据 | 簇1 | `Version/ID/CreatedAt/Cwd/ParentSession/SeedLength/Origin/DelegationDepth/AgentPreset` | Persistence 键目录、fork lineage、subagent 递归深度全靠它 |
| **M07** | LLM Provider 接缝 + 流式 | 簇1 | `type LLMAdapter interface{ ChatStream(ctx, req) (chan StreamChunk, error) }` + DeepSeek 官方 REST+SSE 实现 | 大脑；流式 chunk 不完整 → reasoning/tool_call 无法分步展示 |
| **M08** | Agent Registry + Loop(双循环) | 簇1 | `type Agent interface{Run/Steer/Inject/Followup/Dispose/Cancel}` + Inbox(NextTurn/NextStep) + Turn/Step 状态机 | 整个框架的心脏 |
| **M09** | SystemPrompt 组装 + Section | 簇2 | `PromptSection{name/order/text}` + 排序合并 + tools schema 注入 | DeepSeek "行为像 DeepSeek" 的灵魂；顺序错=模型漂移 |
| **M10** | Prompt Context 动态注册 & 快照 | 簇2 | `PromptContext{name/order/text(AssembleCtx)=>string}` + runtime-context-snapshot user/message | compaction 删除老上下文后需要保留最新快照；与 M09 组合构成完整 prompt 输入面 |
| **M11** | Plan Mode(软引导 + 审批退出) | 簇2 | `plan:policy` Prompt Section + plan/mode log event + exit_plan_mode 工具 + UserQuestion 审批 | 最核心规划入口 |
| **M12** | Goal 系统(状态机+续轮驱动) | 簇2 | `GoalPhase/CAS Revision/goal/change` 事件 + goal-round-driver turn-stopping 监听 + 6 个 goal_* 工具 | 多轮长任务推进的唯一机制；没有它=Goal 只会停在第一个人工输入 |
| **M13** | Todo 系统(整体替换) | 簇2 | `todo/write` 事件 + `todo_write` 工具 | 轻量进度追踪，和 Goal 互补 |
| **M14** | User Questions 接缝 | 簇2 | `type UQ interface{ Ask(ctx, options) (idx int, custom string, err) }` + 同步阻塞/异步回调两版实现 | Plan Mode 的审批退出、Permission 的 ask 策略全依赖 |
| **M15** | Commands(slash 命令) | 簇7 | `CommandDefinition{name/desc/input/handler}` + `command/run` & `command/done` event + SDK 入口 | `/plan off` `/goal xxx` 不走模型消息直接改变 Agent 状态 |
| **M16** | Session Projections 投影注册中心 | 簇2 | `ProjectionDefinition[State any]` + register / snapshot / subscribe(changelog) | SDK 侧读取派生状态的**唯一标准接口**；业务侧不直接读 Session.events 就是通过投影暴露类型安全状态 |
| **M17** | Session References(跨会话 & 文件 mention) | 簇2 | `@session/xxx` / `#path/file` mention 解析 + `PreparedReferencedMessage` + 稳定错误码 + user/message source=`reference` | 用户消息预处理层；不做 mention 解析 = 无法引用历史上下文或附带文件片段 |
| **M18** | Agent Cancel 原因分类 | 簇1 | `{kind:user} / {parent} / {hook reason} / {disposed} / {legacy}` 导入 TurnEndReason.aborted | 日志里必须保留"是谁取消的"这个语义；否则 UI/日志无法区分 |
| **M19** | Request Header 快照 + request/context 路由 | 簇1 | `EpochHeader{config/system/tools}` + `RequestContext{provider/model/contextWindow}` + `reason in {initial/resume/change/series}` | 每次请求完整 header 快照 → 可回放 & compaction 后无需重建；路由元数据变更独立 log |
| **M20** | session/end-seed 种子边界 | 簇1 | Resume/Fork 后第一条 live 写入的 marker 事件 → 定位 cold stored vs live work 分界 | fork lineage / crash bracket 定位(如 compaction 半开括号) 全靠它 |
| **M21** | SurfaceOp(append/replace) + foldSurface | 簇1 | `{op:'replace', start, end}` + `foldSurface(events) => nodes + replacements` | compaction 替换节点就是 surface replace；没有这个机制 compaction 无法生效 |
| **M22** | PreToolDecision 三态(allow/deny/ask) | 簇3 | `tools/pre-execute` waterfall 返回值 + approval 服务的 ask→allowed-once 语义 | approval 政策的唯一接入点；ask 语义错误=审批功能整体失效 |
| **M23** | Tool Execution 四级链 | 簇3 | `pre-execute → execute(可换 signal) → post-execute(accept/block/attach ctx) → result` | 审批/Spill/Sandbox/观测 全挂这四级 waterfall |
| **M24** | Tool Restriction(allow/deny) | 簇3 | Scope 级 tool mask + intersect + scope tool exempt | Subagent 父限制子能力、Preset 隐藏工具的唯一机制 |
| **M25** | ToolRunContext deferContext + concludeTurn | 簇3 | composite tool 嵌套分发通道 + 终止本 turn 的权威标记 | Subagent/Workflow 嵌套工具结果不丢；concludeTurn 是 Goal report_blocker 等完成类工具通知循环结束的唯一通道 |
| **M26** | Sandbox 接缝 + 3 模式 | 簇3 | `mode in {read-only / workspace-write / danger-full-access}` + `{root path, enforced}` 元组 | Bash/FS 两个消费者统一的权限语义；不统一 = 同一会话中 Bash 写了但 FS 写拒绝，产品等价性崩 |
| **M27** | Approval Policy 接缝 | 簇3 | `policy in {allow-all / deny-all / ask-dangerous / ask-dangerous-tool-edit}` + 用户级 override + 会话级 scope override | 与 Sandbox 组合决定"Agent 能否直接改你代码" |
| **M28** | Permission Presets 组合 | 簇3 | `{sandboxMode, approvalPolicy}` 预设表 + 用户自定义派生状态 | 组合旋钮让 UI/SDK 不暴露裸参数 |
| **M29** | Attachment 图片引用模式 | 簇3 | `ImageBlock{url? / reference:AttachmentId}` + attachment storage + durable reference 解析 | 多模态图片的唯一消费路径 |
| **M30** | Invariant Registry 不变量校验 | 簇3 | `ctx.invariants.register(pkgName, installer)` + 包归属报错（INVARIANT + pkgName prefix） | Turn 开闭 / step 匹配 / tool call-result 对 / goal CAS 等并发错误全部靠它在开发期提前暴露 |
| **M31** | Token Meter 计量 & 预算 | 簇3 | per-request prompt/completion token + session-level budget cap + 表面节点定价(token 预算不足时降权) | Go 后端集成场景最关心的成本控制；缺预算=无限烧钱 |
| **M32** | Agent Preset 接缝 | 簇1 | `ctx.agentPresets.{mount/composeFrom/standingKeyFor/recompose/select}` | 会话级 preset 绑定；没有 preset = 所有 agent 用同一套 tools/prompt，无法做团队/项目级隔离 |
| **M33** | Agent Initiator 上下文 | 簇1 | `withInitiator(agent, op)` / `requireInitiator()` / `withoutInitiator` | 工具、凭证、审批中做"是谁发起这个子调用的"因果归因；是 Security 不变量的基础 |
| **M34** | agent/request-error 重试瀑布 | 簇1 | `RequestErrorAction{kind:'retry'}` | LLM 过载/速率限制时模型请求级的显式重试通道 |
| **M35** | **Filesystem 接缝(ctx.fs + obs-policy + tool-fs)** | 簇6 | `FsTarget/ FsVersion/ FsInfo/ FsEditRequest/ FsWriteIntent` + `write/edit/resolve/listDir/stat` + `fs/write-intent fs/edit-intent fs/observed` 事件 + 观察政策(先读后写) | Agent 的"文件手脚"；obs-policy 是 DeepSeek "不乱改"的核心保证 |
| **M36** | **Subprocess 接缝** | 簇6 | `SubprocessSpawnSpec{argv/cwd/stdio/grace/env}` + tree terminate + `CollectedOutput{truncated/spillPath}` + `scrubbedParentEnv(DSH_* scrub)` | Bash/Terminal/LSP 底层 |
| **M37** | **Shell/Bash 接缝 + tool-bash** | 簇6 | `ShellExecRequest → resolve() → ShellExecSpec` + `ShellRunResult`(5 字段正交) + `SandboxExecutionPolicy` + 前台 run / 后台 Job | Agent 的"命令行手脚" |
| **M38** | **Settings 接缝(ctx.settings)** | 簇7 | `SettingsNamespace{schema/base/applies/live vs restart}` + `SettingsScope{get/watch/update/replace}` + `SettingsPathOp{set/unset}` + `describe(redactSecrets:true)` + `expectedRevision CAS` + `secrets role redaction` | 所有能力接缝的配置来源；pathop 保证密钥永不下线 |
| **M39** | **Credentials & Authorization 接缝** | 簇7 | `CredentialRef`(POSIX 变量名品牌) + `resolve(每请求)` + `describe/writable` + `set/unset` + AuthorizationFlow{key/label/methods/runner + list/begin/cancel} + `record modify CAS` | 配置里只存 ref，真正 API Key 通过 credentials 按每次 LLM 请求解析；OAuth 授权走 flow |
| **M40** | **Skill System(ctx.skills + tool-skill)** | 簇7 | `SkillProvider.list/get + SkillCandidate{rank/locator}` + host/scope 分层(nearest wins)+ 6 层 rank 发现(project-dsh → project-agents → custom → user-dsh → user-agents → bundled) + fsnotify 观察 + `skills/change` 事件 + `modelInvocable/userInvocable` 策略 | 项目级领域知识 `.dsh/skills/*.md` 约束 Agent 行为；缺 Skills = Agent 无法读项目自定义规范，等价性崩 |
| **M41** | **Commands 人类命令** | 簇7 | 见 M15 | 簇7 成员命令，cluster 归属修正说明 |
| **M42** | **Spill Storage 溢出接缝** | 簇6 | `ctx.spill: {previewBytes / maxTotalBytes / writeFile ref}` + `spill-policy` 作为 post-tool/Bash listener | 大文本结果不截断可恢复；与 M36 CollectedOutput 配合 |
| **M43** | Persistence 接缝(session 持久化) | 簇4 | `SessionPersistence{locate/load/inspect/append/list/snapshot}` + flush checkpoint + batch 窗口 + repair | 会话冷启动 & 崩溃恢复唯一入口 |
| **M44** | SessionHeader 格式拒绝 & 版本号 | 簇4 | `SESSION_FORMAT_VERSION` + `SessionFormatUnsupportedError + CorruptionError` + `KNOWN_SESSION_EVENT_TYPES` 拒绝未知事件 | 不同版本 dsh-go 写出的 log 互不误读；fail-closed 避免读坏数据 |
| **M45** | Storage Domain KV 抽象(3 层) | 簇4 | `hub → backend(JSONL/SQLite) → domain` + `CAS version mismatch + typed read/write` | MessageFeedback / GoalRevisionViews / SessionSidecars / SkillsCatalog / WorkspaceRegistry 全部共用一套 KV 引擎；否则每种持久化都写一遍 |
| **M46** | Job 生命周期绑定 owner | 簇6(SHOULD→MUST 升级) | `owner Agent 绑定 dispose cancel` + `JobSnapshot{ownerSession}` | 无 owner 绑定 → Agent Dispose 后子进程/PTY 变孤儿，等价性崩 |
| **M47** | Tool Presentation 中立 vocabulary | 簇3 | `ToolCallView/ ToolResultView` 9 种 card（generic/terminal/diff/search/read/web） | 无头后端不渲染 UI，但 Tool 定义仍要产出 presentation meta 供 SDK 使用者传给自己的 UI；否则每个工具都重复声明自己的卡类型 |
| **M48** | DefineTool JSON Schema 强校验子集 | 簇3 | `JsonSchemaNode{type/oneOf/properties/required/additionalProperties/items/enum/const/description/title/default/examples}` + object root assert + validateJsonSchemaValue + `INFER` 类型推断 | 工具定义入参出参强一致校验；subagent/workflow 定义结构化输出也走同一个 Schema 子集 |

#### 🟡 SHOULD 级 16 项（v1.0=14，v2.0 新增 4，升级 3，降级 1，总 +2）

| 编号 | 能力名 | 归属簇 | 复刻点 | 不上线的影响 |
|------|--------|-------|-------|------------|
| **S01** | Compaction(LLM 摘要 + 表面替换) | 簇4 | 选长对话窗口(>N tokens)触发 + 让 LLM 压缩老 surface + 生成 `assistant/message{replace range}` 事件 | 长对话超过 context 直接溢出 |
| **S02** | Subagent 接缝(3 后端) | 簇5 | in-process fork / ACP child / fork-copy-process 三个 Provider + SubagentForkRequest/SubagentResult | 没有子代理 = 规划中「拆分任务并并行执行」失效 |
| **S03** | SQLite 持久化后端 | 簇4 | `sqlite-session` 单 DB + session 事件行化 + FTS5 索引 + 原子写入 | JSONL 单机够，但多会话并发/检索/备份 SQLite 更强；建议至少实现一个强一致后端 |
| **S04** | Session Query + 搜索(SQLite FTS5) | 簇4 | `SessionListRequest / SessionSearchRequest` + SQLite FTS5 消息全文 + by title/created 过滤 + ctrl+f 内容定位 | 无头后端查历史会话靠它 |
| **S05** | Session Telemetry + OTel 集成 | 簇4 | `sessionTelemetry {event/chunk/tool latency}` hooks + OTel trace/metric/log | 生产观测；缺少 S05 = 出现性能问题不知道卡在哪 |
| **S06** | Authorization Service(OAuth 流) | 簇7(已由 M39 升级，保留编号) | 同 M39，credentials 部分 MUST，flow 部分 SHOULD（本地 env credentials 就够用时 flow 可延后） | 没有 OAuth = 需要网页授权的 Web/Git/MCP 无法接入 |
| **S07** | Telemetry 导出到 OTel | 簇4(S05 明细) | OTLP exporter + resource detector + baggage | S05 的一部分，明确拆分 |
| **S08** | Session Title(session/title + LLM helper) | 簇4 | latest-wins fold + fallback human msg prefix + LLM 辅助标题 | 会话标题展示与搜索元数据；fallback 够用但缺 LLM 版体验差 |
| **S09** | Message Feedback(Storage Domain sidecar) | 簇3 | `rating/note/version(CAS)/createdAt/updatedAt` + 稳定 failure taxonomy(session-not-found/target-not-found/version-conflict/…) + `list/put/delete` | 缺反馈数据无法 RLHF/调优；与 M45 Storage Domain 一起实现 |
| **S10** | Terminal PTY(持久终端) | 簇5 | `TerminalBackend + TerminalSession` + spawn/send/read/signal/close + bounded scrollback + waitReason + 单 agent 独占活动 | 缺 Terminal = 无法做 "npm run dev 持续观察再调试" 类长会话 shell |
| **S11** | Jobs Runtime(后台任务视图) | 簇6 | `JobStart{kind/label/owner?/outputLimit?}` + `JobHooks{cancel/done/readOutput}` + `JobSnapshot` + read + waitForDone + cancel + listByOwner | Bash >2s 输出可增量回读给 Agent 做下一步决策而不是等结束；否则阻塞 |
| **S12** | Workflow Engine + tool-workflow | 簇5 | `WorkflowStartRequest{script/meta/args/parent}` + `WorkflowRun{result/cancel/dispose}` + stop reason(completed/cancelled/error) + `parallel/pipeline/agent()` 脚本全局 + Chat Durable Records fold 不变量 | 与 Code Runtime/S12 共享 subagent 绑定；不做 Workflow = 多子任务 orchestration 需要用户手搓 Goal + Tool，体验降级 |
| **S13** | MCP Client(连接外部 MCP Server) | 簇5 | `MCPTransport + MCPClient` + SSE/stdio 协议 + list tools + call tool + 自动映射成 ToolDefinition | 外部工具（Jira/Confluence/飞书）通 MCP 接入，不再一个个手搓；否则每次对接外部系统都写 Go 代码 |
| **S14** | Workspace Registry(目录 + 会话分组 + 中断恢复) | 簇5 | `workspace record: {id/root/sessionGroup/resume-on-open}` + ctx.workspaces.list/open/resolve | 多项目工作区分隔；无头后端可把 workspace 作为业务级 tenant key |
| **S15** | LLM Retry(`llm/retry` 事件 + backoff) | 簇5(新增) | exponential backoff + max attempts + jitter + record `llm/retry` event(attempt/backoffMs/error) | 没有重试 = 生产环境偶发 5xx 直接让整轮 Turn 失败 |
| **S16** | Output Retention(保留原始 tool result value 直到 committed) | 簇5(新增) | 工具执行成功结果在 post-execute 后仍保留 canonical value，不立刻仅存 content；防止一个 concurrent reader 读取 result 前丢失 value | 调试工具、Spill 二级写入、长 tool result 内容恢复安全网 |

#### 🟢 COULD 级 17 项（基本保持不变，调整命名 & 顺序）

| 编号 | 能力名 | 说明 |
|------|--------|------|
| C01-C03 | web_fetch 工具 / Web 能力接缝 / Webhook runtime | 业务侧可自行作为工具接入 |
| C04 | LSP 语言服务器集成 | 纯语义级生产力增强；先用 grep+AST 工具够 |
| C05 | Typert CLI 框架 | 无头后端用 Gin/gRPC 即可 |
| C06 | Schedule 会话级 cron 提醒 | 业务层已有定时任务系统；可后期接入 |
| C07 | Agent Teams(实验) | 团队 roster/mailbox/task DAG；S02 可用后再按同样模式加 |
| C08 | Slots UI 组合系统 | UI 跳过 |
| C09 | Extensions / 动态 Cordis 插件 | v2.0 插件用 Go 组合；不用 JS 沙箱 |
| C10 | Code Runtime(PTC) worker 线程 | 见上 |
| C11 | Conversation Node 引擎 | UI 跳过 |
| C12 | Client Modules / Web Client / Remote API | UI 跳过 |
| C13 | Web Server + HTTP API | 业务侧自己用 Gin/gRPC 暴露 |
| C14 | Approval UI / Settings Panel / Command Palette | UI 跳过 |
| C15 | Attachment 消费端(image resize / OCR) | 多模态扩展 |
| C16 | Sandbox landlock/bwrap 平台强制 | Linux/macOS 专用；windows 先跳过 |
| C17 | Session Control Frame(实时控制) | UI 推送跳过；无头后端用 Go channel/stream |

#### ⚫ SKIP 级 12 项（纯 UI）

| 编号 | 能力名 | SKIP 原因 |
|------|--------|----------|
| K01-K06 | ui-chat / ui-trajectory / ui-workflow / ui-session / ui-composer / Slots + React | 纯前端渲染 |
| K07 | .agents/notes + Agent Notes 方法论 | 开发文档实践，不是运行时代码 |
| K08 | Web Server 服务 & 静态资源 | 无头后端不需要 |
| K09 | Rich Media Attachment(上传下载，图片预览) | 外部接入 |
| K10 | Landlock / bwrap 平台沙箱 | 平台特定 Provider |
| K11 | Terminal UI(xterm.js) | 纯前端 |
| K12 | Conversation 打包事件 chunks（chunkrow/* 标签） | 客户端优化，服务端直接发 SessionEvent 即可 |

---

### 10.12 终极版：SessionEvent 词汇表（45+ 类型，按簇分组）

```
┌─ 簇1 核心日志(必需) ─────────────────────────────────────────────────────────┐
│  turn/start          turn/end           step/start          step/end         │
│  user/message(*)     assistant/chunk*   assistant/message(*) tool/call        │
│  tool/result(*)      request/header     request/context     session/end-seed  │
│  session/title       compaction/start   compaction/summary  compaction/end    │
│  goal/change         todo/write         plan/mode           llm/retry         │
│  sandbox/mode        permission/preset  hook/invoked        hook/result      │
│  command/run         command/done       agent/error(log)    feedback/record  │
│  tool/code-dispatch  (PTC 模式子调用日志)                                                 │
│  workflow/start      workflow/phase     workflow/log        workflow/agent-start│
│  workflow/agent-end  workflow/end       tool-workflow/run-start              │
│  tool-workflow/run-end                                                     │
│  reference/resolved  mention 解析成功写 log(诊断)                              │
│                                                                              │
│ (*) 标记 = SurfaceEventType(可携带 surfaceOp append/replace + sourceEventSeqs) │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

### 10.13 终极版：项目结构（含 MUST 48 + SHOULD 16）

```
dsh-go/
├── go.mod                                  # Go 1.22+, 依赖: jsonschema/v5, sqlite, otel, fsnotify
├── README.md
├── docs/                                   # 详细文档目录
│   ├── TASKS.md                            # 任务表主入口（人类可读）
│   ├── tasks.json                          # 任务表机器可读（程序化）
│   ├── trace.md                            # 对话追踪
│   └── CACHE_HIT_RATE_PLAN.md              # 缓存命中率对齐实施计划
├── cmd/
│   └── dshd/                               # 可选：独立服务入口（如用户选模式 2）
│       └── main.go
├── pkg/
│   ├── brand/                              # M01: Branded ID 封装（类型安全字符串）
│   │   ├── brand.go
│   │   └── ids.go                          # SessionID, ToolCallID, ApprovalRequestID, JobId, ...
│   │
│   ├── scope/                              # M03: Scope 分层注册表
│   │   ├── scope.go                        # ScopeKey + Layer + ScopedLayers merge
│   │   └── store.go                        # NamedEntries / AnonymousEntries（供 registry 复用）
│   │
│   ├── waterfall/                          # M02: Waterfall 中间件链原语
│   │   └── waterfall.go                    # WaterfallFunc[T] + Chain + typed next
│   │
│   ├── session/                            # 簇1(M04-M06/M16-M21/M30) + 簇4 Projections
│   │   ├── types.go                        # SessionEvent 45+ 类型 + Map→union + Branded IDs
│   │   ├── session.go                      # Session struct(append/deriveMessages/requestHeader)
│   │   ├── header.go                       # SessionHeader + 校验 + 版本拒绝
│   │   ├── surface.go                      # SurfaceOp + foldSurface(含 replace 世代)
│   │   ├── projection.go                   # M16: Session Projection 注册中心
│   │   ├── reference.go                    # M17: Session Reference mention 解析
│   │   ├── store.go                        # SessionStore(create/prepare/enter/announce + fork)
│   │   ├── fold_plan.go                    # foldPlanMode
│   │   ├── fold_goal.go                    # foldGoalChange
│   │   ├── fold_todo.go                    # foldTodoWrite
│   │   ├── fold_sandbox.go                 # foldEffectiveSandboxMode + foldPermissionPreset
│   │   ├── fold_title.go                   # S08: foldSessionTitle
│   │   └── invariant_companion.go          # 开发期不变量：turn/step 开闭 + tool 配对
│   │
│   ├── llm/                                # 簇1 M07 + S15 LLM 重试
│   │   ├── types.go                        # Message / ContentBlock / StreamChunk 词汇表
│   │   ├── adapter.go                      # LLMAdapter 接口 + ToolSchema / LlmFailure
│   │   ├── retry.go                        # S15: llm/retry 事件 + 指数退避
│   │   └── provider_deepseek/              # DeepSeek REST + SSE 官方实现
│   │       └── deepseek.go
│   │
│   ├── agent/                              # 簇1 M08/M18/M32-M34 + Registry
│   │   ├── types.go                        # Agent 接口(id/session/options/status/inbox/ctx)
│   │   ├── handle.go                       # AgentHandle(create/resume/dispose + setup commit + rollback)
│   │   ├── registry.go                     # AgentRegistry(register/list/get/enter/announce + initiator)
│   │   ├── cancel.go                       # M18: AgentCancelCause 分类
│   │   ├── initiator.go                    # M33: withInitiator / requireInitiator / withoutInitiator
│   │   └── loop.go                         # ReactLoopAgent (Turn/Step 双循环 + Inbox 声明)
│   │
│   ├── sysprompt/                          # 簇1 M09 + 簇2 M10
│   │   ├── system_prompt.go                # PromptSection 注册表 + assemble waterfall
│   │   ├── context.go                      # M10: PromptContext 注册 + runtime-context-snapshot
│   │   ├── section_defaults.go             # persona / policy / global sections（直接拷贝原版文本）
│   │   └── variables.go                    # Prompt 变量解析
│   │
│   ├── tools/                              # 簇1 M08 + 簇3 M22-M25/M47-M48
│   │   ├── types.go                        # ToolDefinition + DefineTool<T> 泛型 + 9 种 card
│   │   ├── schema.go                       # M48: JSON Schema 强子集 + assertSupported + validate
│   │   ├── registry.go                     # ToolRegistry: register/schemas/execute/restrict/guard + Scope 分层
│   │   ├── pipeline.go                     # M23: 四级 Waterfall 执行 (pre/execute/post/result)
│   │   ├── execution.go                    # ToolExecution/Result + isError 归一化 + deferContext/concludeTurn(M25)
│   │   ├── restriction.go                  # M24: ToolRestriction(allow/deny + scope 交叉)
│   │   └── presentation.go                 # M47: Card 中性描述
│   │
│   ├── plan/                               # 簇2 M11
│   │   ├── plan_mode.go                    # plan/mode 事件 + fold + pre-step 注入 section
│   │   ├── exit_plan_mode_tool.go          # exit_plan_mode 工具 + user-question 审批
│   │   └── command_plan.go                 # /plan 命令入口
│   │
│   ├── goal/                               # 簇2 M12
│   │   ├── goal.go                         # GoalPhase + goal/change CAS + 事件
│   │   ├── round_driver.go                 # agent/turn-stopping → goal.active 注入续轮
│   │   ├── tools_goal.go                   # 6 个 goal_* 工具
│   │   └── command_goal.go                 # /goal 命令
│   │
│   ├── todo/                               # 簇2 M13
│   │   ├── todo.go                         # todo/write 事件 + fold
│   │   └── tool_todo.go                    # todo_write 工具
│   │
│   ├── userq/                              # 簇2 M14: User Questions 接缝
│   │   ├── types.go                        # UserQuestion{options/multiSelect/intent}
│   │   ├── interface.go                    # UQ 服务接口
│   │   └── provider_stub.go                # 同步阻塞 stub(测试用) + SDK 提供真实 impl
│   │
│   ├── commands/                           # 簇7 M15/M41: Human Commands
│   │   ├── types.go                        # CommandDefinition / Invocation / Result
│   │   └── runtime.go                      # ctx.commands (register/list/execute + scope)
│   │
│   ├── sandbox/                            # 簇3 M26
│   │   ├── types.go                        # SandboxMode + ExecutionPolicy
│   │   └── policy.go                       # EffectivePolicy = 用户级 + 会话级 override
│   │
│   ├── approval/                           # 簇3 M27
│   │   ├── types.go                        # ApprovalPolicy / OnceGrant
│   │   └── interface.go                    # ask→allowed-once 语义 stub + SDK impl
│   │
│   ├── presets/                            # 簇3 M28 + 簇1 M32: Agent Presets
│   │   ├── permission_presets.go           # M28: Permission Presets(组合旋钮)
│   │   └── agent_presets.go                # M32: AgentPreset{mount/composeFrom/recompose/standingKey}
│   │
│   ├── attachment/                         # 簇3 M29
│   │   ├── types.go                        # AttachmentId / ImageBlock(reference vs inline)
│   │   └── store.go                        # Attachment durable storage + ref resolve
│   │
│   ├── invariant/                          # 簇3 M30
│   │   └── registry.go                     # ctx.invariants + pkg attributed failure
│   │
│   ├── tokenmeter/                         # 簇3 M31
│   │   └── meter.go                        # TokenMeter(per request usage + session budget)
│   │
│   ├── fs/                                 # 簇6 M35
│   │   ├── types.go                        # FsTarget/Version/Info/EditRequest/WriteIntent
│   │   ├── filesystem.go                   # FileSystem 接缝接口
│   │   ├── fs_local/                       # 本地磁盘实现(带 version 令牌)
│   │   │   └── local.go
│   │   ├── observation_policy.go           # fs/write-intent → 先读后写默认政策
│   │   └── tool_fs.go                      # 模型工具: fs_read / fs_write / fs_edit / fs_list / fs_stat
│   │
│   ├── subprocess/                         # 簇6 M36
│   │   ├── types.go                        # SpawnSpec / StdioDisposition / CollectedOutput
│   │   ├── subprocess.go                   # Subprocess 接缝 + scrubbedParentEnv + 树形 terminate
│   │   └── local/                          # 本地实现(os/exec + process group)
│   │       └── local.go
│   │
│   ├── shell/                              # 簇6 M37
│   │   ├── types.go                        # ShellExecRequest / Spec → resolve() + RunResult
│   │   ├── shell.go                        # ShellExecutor 接缝接口
│   │   ├── local/                          # Bash 本地 + Sandbox 两个 Provider
│   │   │   └── bash.go
│   │   ├── env.go                          # DSH_* 环境变量 + dshEnv
│   │   └── tool_bash.go                    # 模型工具: bash(command, workdir?, timeout?)
│   │
│   ├── spill/                              # 簇6 M42
│   │   ├── types.go                        # SpillRef
│   │   ├── store.go                        # ctx.spill(preview + 阈值写入文件)
│   │   └── policy.go                       # tool/post-execute + Bash 结果监听 → 超阈值 spill
│   │
│   ├── jobs/                               # 簇6 M46 + SHOULD S11
│   │   ├── types.go                        # JobSnapshot / JobStart / JobHooks / JobOutcome
│   │   └── registry.go                     # ctx.jobs: start/read/cancel/wait + owner 绑定
│   │
│   ├── settings/                           # 簇7 M38
│   │   ├── types.go                        # SettingsNamespace / Scope / Descriptor / PathOp
│   │   ├── settings.go                     # ctx.settings.register + namespace scope + watch CAS
│   │   └── file/                           # 文件 JSON/YAML provider
│   │       └── file.go                     # Secrets 字段 role('secret') 脱敏
│   │
│   ├── credentials/                        # 簇7 M39 + SHOULD S06
│   │   ├── types.go                        # CredentialRef / ResolvedCredential / CredentialInfo
│   │   ├── credentials.go                  # ctx.credentials(seam): resolve/describe/set/unset + records modify CAS
│   │   ├── authorization.go                # ctx.authorization(flow register/list/describe/cancel/begin)
│   │   └── local/                          # env + file + .env 多层 Provider
│   │       └── local.go
│   │
│   ├── skills/                             # 簇7 M40
│   │   ├── types.go                        # SkillProvider + Candidate + Summary + CatalogSnapshot
│   │   ├── registry.go                     # ctx.skills(host+scope 6 层 rank) + list/get + cache 失效
│   │   ├── fs_watcher.go                   # fsnotify 观察 roots + 工具写入触发失效
│   │   └── tool_skill.go                   # skill(name) 模型工具 + catalog 变更检测
│   │
│   ├── persistence/                        # 簇4 M43-M44 + SHOULD S03
│   │   ├── types.go                        # SessionPersistence 接口 + SessionPreparation + CrashRepair
│   │   ├── flush.go                        # Flush Checkpoint + 批处理窗口 + repair 孤儿 turn
│   │   ├── jsonl/                          # JSONL 本地后端
│   │   │   └── jsonl.go
│   │   └── sqlite/                         # S03: SQLite 后端 + FTS5 索引
│   │       ├── sqlite.go
│   │       └── migrations.go
│   │
│   ├── storage/                            # 簇4 M45: Storage Domain KV
│   │   ├── domain.go                       # hub / backend / domain 三层 + 版本校验
│   │   ├── filekv/                         # 文件 KV 后端
│   │   │   └── filekv.go
│   │   └── sqlitekv/                       # SQLite KV 后端(复用 session db)
│   │       └── sqlitekv.go
│   │
│   ├── compaction/                         # SHOULD S01
│   │   ├── compaction.go                   # CompactionSeam + Engine 接口
│   │   └── engine_basic/                   # LLM 摘要 + 生成 surfaceOp replace
│   │       └── basic.go
│   │
│   ├── sessionquery/                       # SHOULD S04
│   │   └── query.go                        # SessionSearch(标题/内容 FTS5) + 列表过滤
│   │
│   ├── sessiontitle/                       # SHOULD S08
│   │   └── title.go                        # SessionTitleProvider + LLM helper + session/title
│   │
│   ├── telemetry/                          # SHOULD S05/S07
│   │   ├── session_telemetry.go            # S05: session event hooks
│   │   └── otel.go                         # S07: OTel trace/metric/log exporter
│   │
│   ├── feedback/                           # SHOULD S09
│   │   └── message_feedback.go             # Storage Domain sidecar + list/put/delete + CAS + fail 分类
│   │
│   ├── subagent/                           # SHOULD S02
│   │   ├── types.go                        # Subagent Provider 接口 + ForkRequest/Result
│   │   ├── subagent.go                     # 注册表 + 工具
│   │   ├── inprocess/                      # 进程内 fork 后端
│   │   │   └── fork.go
│   │   ├── subprocess/                     # 子进程 fork-copy 后端
│   │   │   └── child.go
│   │   └── acp/                            # ACP child 后端(ndjson pipe)
│   │       └── acp.go
│   │
│   ├── terminal/                           # SHOULD S10
│   │   ├── types.go                        # TerminalBackend / Session / SendOperation / Result
│   │   └── service.go                      # ctx.terminals + 本地 PTY backend 实现(依赖 creack/pty)
│   │
│   ├── workflow/                           # SHOULD S12
│   │   ├── types.go                        # WorkflowRequest / Run / Result + phase / log / agent*
│   │   ├── engine.go                       # Worker thread 引擎：script vm + globals(agent/pipeline/parallel/phase)
│   │   └── tool_workflow.go                # tool-workflow 的 durable records + invariant
│   │
│   ├── coderuntime/                        # COULD C10(预留)
│   │   └── types.go
│   │
│   ├── workspace/                          # SHOULD S14
│   │   └── registry.go                     # WorkspaceRegistry(目录 + 会话分组 + resume)
│   │
│   ├── mcp/                                # SHOULD S13
│   │   ├── client.go                       # MCP Client(SSE/stdio transport)
│   │   └── tool_bridge.go                  # MCP Tool → ToolDefinition 桥
│   │
│   ├── harnessctx/                         # SDK 层入口(对外 API 面)
│   │   ├── harness.go                      # Harness 根对象：组合所有能力
│   │   ├── builder.go                      # HarnessBuilder：配置式构造
│   │   └── session_controller.go           # 类似 SessionController 的高层便捷 API：resolve/inspect/list/search/create/prompt/fork/page
│   │
│   └── util/
│       ├── jsonexact/                      # 与原版 isJsonValue 等价：lossless JSON + 拒绝 BigInt/Map/Set/Date/sparse/circular/negative-zero
│       │   └── exact.go
│       ├── errctx/                         # code+message 结构化错误(UNKNOWN_TOOL/INVALID_ARGS/FS_STALE_VERSION...)
│       │   └── error.go
│       ├── lifecyclegroup/                 # ordered teardown + wait quiescence(替代 Cordis effect)
│       │   └── group.go
│       └── fsnotifyutil/                   # fsnotify 封装(供 skills/settings/credentials 监听)
│           └── watcher.go
├── sdk/                                    # 对外 SDK：作为库给用户的 Go 服务 import（模式 1）
│   └── dsh-go-sdk.go                       # 简化 API：CreateAgent/SendMessage/Cancel/Resume + Options
├── internal/                               # 未导出实现细节
│   ├── testutil/                           # 测试桩：FakeLLM/FakePersistence/FakeApproval…
│   └── cordislike/                         # Cordis 简化替代：Context + Service + Emit/Waterfall(可选)
├── examples/                               # 示例
│   ├── minimal-lib-mode/                   # 模式 1：作为库集成(你的主要场景)
│   │   └── main.go
│   ├── with-custom-tools/                  # 自定义 ORM/DB 工具注入
│   │   └── main.go
│   └── with-plan-goal/                     # 规划 + Goal 长任务示例
│       └── main.go
└── tests/                                  # 集成测试（20+ 套件）
    ├── harnessctx_invariant_test.go        # HarnessCtx 创建后不变量
    ├── session_event_vocab_test.go         # 45+ 事件的 append/投影一致性
    ├── session_projection_test.go          # M16: 投影注册/快照/变更推送
    ├── session_reference_test.go           # M17: mention 解析 + 错误码
    ├── prompt_context_test.go              # M10: PromptContext 快照 + compaction 后保留
    ├── plan_mode_approval_test.go          # Plan Mode + exit 审批
    ├── goal_rounddriver_cas_test.go        # Goal 多轮续驱 + CAS 并发写
    ├── tools_waterfall_test.go             # 四级链(approval/spill/错误通道)
    ├── filesystem_obs_policy_test.go       # 先读后写政策 + stale 拒绝
    ├── bash_subprocess_spill_test.go       # Bash 超时/截断/spill 恢复
    ├── settings_pathop_secrets_test.go     # secret 永不上线 + pathop 回写
    ├── credentials_per_request_test.go     # 每请求解析 + 热更新可见
    ├── skills_registry_fsnotify_test.go    # 6 层 rank + watcher 失效
    ├── compaction_replace_surface_test.go  # SurfaceOp replace 后 deriveMessages 一致
    ├── persistence_crash_repair_test.go    # 崩溃孤儿 turn → interrupted reason
    ├── persistence_fork_lineage_test.go    # fork 后 parent/seedLength 正确
    ├── agent_cancel_cause_classify_test.go # 四种 cancel 原因 → turn end reason
    ├── subagent_fork_inprocess_test.go     # subagent 进程内后端
    ├── storage_domain_cas_test.go          # KV 版本不匹配
    └── token_meter_budget_test.go          # 预算超过 → 下一轮拒绝
```

---

### 10.14 终极版：一次性复刻到位工作量重新估算

| 簇 | MUST 项 | SHOULD 项 | 关键新增模块数 | 估算(人周) | 备注 |
|---|--------|----------|--------------|----------|------|
| 簇1 内核驱动 | M01-M08, M16-M21, M32-M34 = 20 | — | session/agent/llm/sysprompt/tools/scope/waterfall/brand + 投影 | 6 ~ 8 | 最难点：Turn/Step 双循环 + Inbox 状态机 + append 内部分发与 projection 一致性 |
| 簇2 规划能力 | M09-M15 + M16(M17) 共 8 | — | plan/goal/todo/userq/projection/reference/promptctx | 2.5 ~ 3.5 | Plan Mode 的审批退出路径最绕；PromptContext 要联动 compaction |
| 簇3 执行安全 | M22-M31 共 10 | S09 = 1 | sandbox/approval/presets/invariant/tokenmeter/feedback + 2 presentation | 2.5 ~ 3 | invariant 开发期可先写 sync stub，生产关掉；presentation 纯结构体映射 |
| 簇4 持久化恢复 | M43-M45 共 3 | S01/S03-S05/S07-S08 = 6 共 9 | persistence(jsonl+sqlite+repair) + storage domain + compaction + query + title + telemetry | 3 ~ 4 | SQLite 后端最大；Crash Repair(orphan turn close) 逻辑要慎；FTS5 搜索加索引 |
| 簇5 子Agent多模态 | — | S02/S10-S14 = 7 | subagent(3后端) + terminal + jobs + workflow + workspace + mcp | 3.5 ~ 5 | Subagent 3 后端最多；Terminal 跨平台 PTY 有 pitfall；MCP SSE 协议单独坑 |
| 簇6 文件进程执行 | M35-M37/M42/M46 = 5 | S11 = 1 | fs(含 obs policy) + subprocess + shell + spill + jobs | 3 ~ 4 | fs observation policy 的读写时序 + subprocess 树 terminate(Windows taskkill /T) |
| 簇7 配置凭证技能 | M38-M41 = 4 | S06 = 1 | settings(pathop/secret CAS) + credentials(env 多层) + authorization(flow stub) + skills(6层+fsnotify) + commands | 2.5 ~ 3.5 | skills 的 fsnotify 文件级 watcher 要稳；settings 分层 schema 校验最难 |
| SDK & 测试 | — | — | harnessctx builder + sdk 入口 + 20 套件集成测试 | 2 ~ 3 | |
| **合计** | **48 项 MUST** | **16 项 SHOULD** | **~70 包+测试套件** | **25 ~ 34 人周**（约 6 ~ 8.5 个月，2 人并行 ~ 3~4.5 个月） | |

**与 v1.0 对比**：v1.0 估算 20~28 人周，v2.0 因簇 6/7 升级 MUST + SHOULD 新增，总工作量上升约 22%，但等价性从 "80%" 提高到 "99%"。

---

### 10.15 终极版：验收清单（17 条，覆盖 7 簇）

#### 验收标准（全部通过才算「等价 DeepSeek Agent」）：

##### ▸ 簇1 内核驱动（4 条）
1. **T1.1 事件溯源可重放**：给定 1000+ 事件的 log，冷 load → deriveMessages 与热 session 完全字节一致；fork 后 parent/seedLength 正确
2. **T1.2 Turn/Step 并发正确**：同一 agent 2 条 followup 在 running 期间串行排队；cancel(user) 与 cancel(parent) 在 turn/end.reason 中正确区分(M18)
3. **T1.3 Tools 四级链拦截正确**：pre-execute deny → execute 不跑；execute 换 signal → cancel 生效；post block → 最终 isError
4. **T1.4 Request Header 可重建**：最新一次 request/header snapshot → 重新 assemble prompt + schema 与实际发出的完全一致（含 compaction 后）

##### ▸ 簇2 规划能力（3 条）
5. **T2.1 Plan Mode 审批退出**：进入 plan 模式 → 模型调用 exit_plan_mode 输出完整计划 → user-question 审批通过后 plan:policy section 下次请求才消失；审批拒绝则继续
6. **T2.2 Goal 多轮续驱**：set_goal(active, maxRounds=5) 无用户后续输入情况下自动注入续轮提示跑完 5 轮；goal_report_blocker 触发 concludeTurn 正确结束本轮
7. **T2.3 Projection & Reference 工作**：用户输入「参考 @session/abc 第 3 段和 #src/main.go 第 10-20 行」→ reference 解析成功 + PreparedReferencedMessage + source=reference 正确写入 user/message（M16/M17）

##### ▸ 簇3 执行安全（2 条）
8. **T3.1 Sandbox+Approval 组合正确**：mode=danger + policy=deny-all 时 bash 写文件 deny 不执行；read-only + ask-dangerous fs_write 弹窗审批通过才落盘；用户预设 preset="safe"(read-only + ask-dangerous) 覆盖两者（M28）
9. **T3.2 Token 预算生效**：设置 session-level 预算 10k tokens → 到上限后下一轮启动前被 budget deny，不消耗真实 LLM token

##### ▸ 簇4 持久化 & 恢复（2 条）
10. **T4.1 Crash 修复**：模拟在 turn open 且 step/tool 未结束处 kill → reload → 追加 turn/end(interrupted)，不丢已 append 的 assistant/chunk 和 tool/call 数据，不截断
11. **T4.2 Compaction 后表面一致**：compact 一个 20k token 会话 → surface.replace 生效 → deriveMessages() 长度缩短 → 再发新消息正常走下一轮

##### ▸ 簇5 子 Agent & 多模态（2 条）
12. **T5.1 Subagent in-process fork**：主 Agent 调用 create_subagent，子独立跑 3 步 tool 后返回 → 父子 lineage 正确；父 dispose 子自动 drain
13. **T5.2 Terminal 后台存活 + Job 增量**：启动 terminal "npm run dev" → send 3 次输入 + read output → Agent Dispose 后 terminal 自动关闭不残留；bash 长任务通过 Jobs 输出增量回读（S10/S11）

##### ▸ 簇6 文件 & 进程（2 条）
14. **T6.1 FS 先读后写 + stale 拒绝**：并发两个 fs_edit 同一行 → 第二个因 FsVersion mismatch 报 FS_STALE_VERSION；没有 obs-policy 的 fs.write 无条件成功；有 obs-policy 未观察版本拒绝裸写入
15. **T6.2 Bash 溢出 → Spill 可恢复**：`bash('seq 1 100000')` 默认 64kb 截断 → spill 写满 → 读 spillPath 能拿到完整 100000 行（M36/M37/M42）

##### ▸ 簇7 配置 & 凭证 & 技能（2 条）
16. **T7.1 Settings secret 永不泄漏**：`describe({redactSecrets:true})` 返回 descriptor 中 secret 字段只给 path+set/unset 操作位，value 全红acted；用户 UI 写 secret 通过 PathOp{set/unset path, value} 不回写整段；credentials 对 llm provider 每请求 resolve 一次——修改 env 变量后下一轮请求立刻看到新值（M38/M39）
17. **T7.2 Skills 6 层发现 + 热更新**：新建 `<proj>/.dsh/skills/my-skill.md` → 下一次 ctx.skills.list 立刻出现该条目；删除后下一次读不到；tool-skill(`my-skill`) 把内容作为 user/message(=injected-context source) 注入（M40）

---

### 10.16 终极版等价性自检 Checklist（上线前逐项打勾）

```
┌─ Go 版 dsh-go vs 原版 DeepSeek Harness 等价性 ──────────────────────────────┐
│                                                                              │
│  [ ] 48 项 MUST 全部通过单测 + 17 条验收清单 100% PASS                        │
│  [ ] 16 项 SHOULD 至少前 12 项上线(S01-S12)，S13-S16 可延期 1 个迭代          │
│  [ ] 每一个注册的 ToolDefinition 与原版: name/description/parameters/行为 一字不差│
│  [ ] Plan Mode 的 plan:policy Prompt Section 文本与原版逐行 diff 一致          │
│  [ ] Goal 的续轮 prompt(renderGoalRoundPrompt) 文本与原版逐行 diff 一致        │
│  [ ] SessionEvent 45+ 类型 JSON 结构与原版 event.data 同构（便于跨语言导入）    │
│  [ ] SQLite 后端能 import 原版 dsh 写出的 .jsonl 日志不出错                   │
│  [ ] 给出「与原版行为不一致」的公开已知差异列表（KnownDivergences.md）          │
│  [ ] 中文注释 100%：每个导出类型/接口/公共函数头部都有中文文档注释             │
│  [ ] README.md 能力清单章节与实际代码包结构 1:1 对应（本章节每次代码功能更新后）│
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

（**第十章 v2.0 结束**——本章节每次代码功能更新后需同步维护，保证 README 能力清单 = 实际实现 1:1）

---

> 📝 **Token 估算（本章新增内容约 18,000 字 → 约 22,500 tokens；包含 7 簇 × 64 项能力的字段级契约描述、耦合箭头图、45+ 事件词汇表、~70 包项目结构、17 条验收清单与 Checklist）。**

---

## 十一、任务执行表 & 官方同步锚点（重要索引）🎯

> 本章是「README 能力清单」与「实际实现进度」的连接桥梁。实现 Agent 每完成一个功能点，**必须同步更新本章节所链接的两份任务表**，保证文档与代码 1:1 对应。

### 11.1 任务表文件索引（请始终保持两者同步）

| 文件 | 目标读者 | 格式 | 是否需要随功能点更新 |
|---|---|---|---|
| [docs/tasks.json](./docs/tasks.json) | **🤖 实现 Agent**（程序化读取 / 更新） | JSON（结构化 schema，可校验）| ✅ **每完成/回滚一个功能点必须更新** |
| [docs/TASKS.md](./docs/TASKS.md) | **👨‍💻 人类工程师 / 你自己**（巡检、汇报、进度查阅）| Markdown 表格（7 簇分组 + 状态 + 依赖 + 验收）| ✅ **与 tasks.json 同步更新** |

### 11.2 官方仓库同步锚点（用于后续增量同步）

所有功能点均基于 **deepseek-harness 官方仓库**的以下快照梳理：

| 字段 | 值 |
|---|---|
| 🔗 远程仓库 | `https://github.com/deepseek-ai/deepseek-harness.git` |
| 🌿 分支 | `master` |
| 🏷️ 快照 Commit Full ID | `cd5ef8148158c3a752a658978873241fdf8e2bbc` |
| 🏷️ 快照 Commit Short ID | `cd5ef81481` |
| 📅 快照日期 | 2026-08-28 00:57:43 +0800 |
| 📝 对应合入 | Merge #3248 → 发布 **dsh-0.1.2-alpha.1** |
| 📚 已扫描子系统数 | **60+**（详见 docs/tasks.json 的 `upstreamSyncAnchor.snapshottedSubsystemsDocs[]`）|

**👉 后续同步官方新能力流程（每周一执行 1 次）**：

```bash
# 1. 在官方源码目录抓取 origin/master 最新
cd D:\workspace\python_workspace\deepseek-harness
git fetch origin master

# 2. 查看「本快照 → 最新」之间有哪些 subsystem 变更
git log --oneline cd5ef81481..origin/master -- docs/subsystems packages/*/src packages/*/README.md

# 3. 按下列规则回填任务表：
#   a) 已有 subsystem 变更 → docs/tasks.json 对应任务 history[] 追加变更记录 + 必要时改状态为 in_progress
#   b) 新增 subsystem（官方加了新能力）→ docs/tasks.json 末尾以 N01/N02/... 追加
#   c) 更新 upstreamFutureSync.lastSyncCommitId / lastSyncAt / history[]
```

### 11.3 任务状态规范（实现 Agent 必须严格遵守）

| 状态（docs/tasks.json.status / docs/TASKS.md 徽章） | 使用场景 |
|---|---|
| `pending` / ⚪ 待做 | 初始态，已规划未开工 |
| `in_progress` / 🟡 进行中 | 编码 / 单测 / 联调中；必须**至少写 1 条 history** 说明进度 |
| `completed` / ✅ 完成 | 该任务 「验收清单全部通过 + 中文注释齐全 + 关联测试 PASS + README/TASKS 更新」四要素缺一不可 |
| `blocked` / 🚧 阻塞 | 上游未完成 / 外部依赖缺失 / 设计决策 pending；history[] 必须写「阻塞原因 + 解除条件」|
| `deferred` / ⏳ 延后 | COULD 类本期不做（未来会上线，非永久跳过）|
| `skipped` / ⚫ 跳过 | UI 类或明确永不实现（SKIP 级能力）|

### 11.4 进度统计看板（实现 Agent 每次更新后同步刷新）

| 级别 | 总数 | ⚪ 待做 | 🟡 进行中 | ✅ 完成 | 🚧 阻塞 | ⏳ 延后 | ⚫ 跳过 | 完成率 |
|---|---|---|---|---|---|---|---|---|
| 🔴 MUST（M01-M48） | 48 | 5 | 0 | 43 | 0 | 0 | 0 | **89.58%** |
| 🔴 MUST（N01-N07 缓存） | 7 | 7 | 0 | 0 | 0 | 0 | 0 | **0%** |
| 🟡 SHOULD（S01-S16） | 16 | 15 | 0 | 1 | 0 | 0 | 0 | **6.25%** |
| 🟡 SHOULD（N08-N09 缓存） | 2 | 2 | 0 | 0 | 0 | 0 | 0 | **0%** |
| 🔴 + 🟡 合计（非跳过） | **73** | **28** | **0** | **44** | **0** | **0** | **0** | **60.27%** |
| 🟢 COULD（C01-C17） | 17 | 0 | 0 | 0 | 0 | 11 | 6 | 0% |

> 本看板与 [docs/TASKS.md](./docs/TASKS.md)「完成度统计」表保持一致；数字以 `scripts/task-stats.ps1` 重算结果为准。
>
> **里程碑完成标准**：当 🔴 MUST 完成率 100% 且 17 条验收清单全部通过时，即达成「Go 后端直接集成等价 DeepSeek Agent 规划能力」核心目标。SHOULD 完成视为生产化就绪。

---

## 十二、竞品对比 & DeepSeek 缓存命中率对齐方案 🎯

> 本章回答三个核心问题：
> 1. 市面**是否已有类似开源项目**？
> 2. **dsh-go 的差异化优势**是什么？
> 3. **实现后能否与官方 dsh 一致达到 97-99% 缓存命中率**？
>
> 数据来源：GitHub 直接搜仓 + DeepSeek 官方 KV cache 文档 + FindHarness 社区博客 + tRPC-Agent-Go 官方博客。

### 12.1 竞品全景（4 大类）

| 类别 | 代表项目 | 语言 | 形态 | 与 dsh-go 差距 |
|---|---|---|---|---|
| **官方** | [deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness) | TypeScript（+ Python minimal SDK）| CLI + Web + Python SDK | 同名但是 TS；官方 Python SDK 只暴露 `DeepSeekHarness.run()` 最小入口，**未开放 Plan/Goal/Skills 等 60+ 子系统的编程 API** |
| **DSH 衍生 TS 社区** | Reasonix、Waveloom、reasonix-hermes | TS（部分 Go 化）| CLI 终端 agent | 解决「单二进制 + 缓存友好 + MCP 工具生态」，**不开放为可被业务进程内 import 的库**；覆盖 ~10 个能力（Loop/Tool/MCP），缺 Plan/Goal/CAS/Skill 6层/fsnotify 等 |
| **Go 通用 Agent 框架** | [tRPC-Agent-Go](https://github.com/trpc-group/trpc-agent-go)、Coding Agent、tools-mcp-test-golang | Go | 库 + CLI demo | 提供 ReAct Loop + Tool Call 通用骨架，**没有 dsh 的事件溯源 Session、Plan Mode 审批、Goal CAS、Skill 6 层 Provider、fsnotify 实时刷新**等 DSH 等价能力 |
| **复刻 dsh 的非官方 Go 项目** | **dsh-go（本项目）** | Go | 进程内 SDK 库 | **唯一对标 dsh 60+ 子系统 + 进程内可嵌入的 Go 实现** |

**关键空白**：DSH 团队官方只提供 TS 主仓 + Python minimal SDK，**没有 Go SDK**；社区项目或偏 TS（不解决你的 Go 业务集成诉求）、或偏 CLI（无法进程内 import）。dsh-go 正好填补「**Go 语言 + 60+ 子系统 + 进程内 SDK**」这个空白。

### 12.2 dsh-go vs 主要竞品 vs 官方（10 维度）

| 维度 | dsh-go（本项目）| Reasonix / Waveloom | tRPC-Agent-Go | dsh 官方（TS）|
|---|---|---|---|---|
| **可嵌入方式** | 进程内 `import` 直接 `agent.Run()` | CLI 终端 / 独立进程 | 库 + CLI | TS：import / Python：minimal SDK |
| **能力完整度** | **60+ 子系统一次到位** | ~10 个核心 | ~8 个核心 | 100% |
| **事件溯源 Session** | ✅ 45+ Event 词汇表 + fold 投影族 | ❌ | ❌ | ✅ |
| **Plan Mode 审批流** | ✅ exit_plan_mode → User Questions 回调 | ❌ | ❌ | ✅ |
| **Goal CAS + Round Driver** | ✅ revision CAS + 自动续轮 | ❌ | ❌ | ✅ |
| **Skills 6 层 Provider + fsnotify** | ✅ | ❌（MCP 替代）| ❌ | ✅ |
| **Tools 四级 Waterfall** | ✅ pre→execute→post→result | ⚠️ 只 execute | ⚠️ 只 execute | ✅ |
| **Sandbox / Approval / Permission Presets** | ✅ fail-closed 默认 | ❌ | ❌ | ✅ |
| **持久化接缝** | ✅ JSONL + SQLite 双后端 + Crash Recovery | ⚠️ JSON only | ⚠️ 内存 only | ✅ |
| **Credentials 双空间 + Authorization Flow** | ✅ | ❌ | ❌ | ✅ |

**核心定位差异**：

> **dsh-go 是「作为 Go 业务进程的库」**，而 Reasonix/Waveloom/tRPC-Agent-Go 都是「作为终端 CLI」。这意味着：
> - 你的 Gin/Kratos/Kitex 服务能**直接 import** dsh-go，把业务 Tool（ORM、DB、缓存、消息推送）注册进 `ToolRegistry`，**零网络 IPC**；
> - Plan/Goal/Skills 事件能**直接落到你业务的 Storage Domain**（用户表/项目表/工单表），不需要外挂 JSONL 文件；
> - Approval/Question 通过**回调函数**回到你业务 Controller，不需要前端 UI。

### 12.3 DeepSeek Prefix Cache 机制速览（必须先懂）

DeepSeek API 的 prefix cache 是**服务端自动**的（**无需手动打 `cache_control` 标签**），规则：
- **命中前提**：请求 prompt 从第 0 个 token 开始**逐 token 完全一致**；
- **价格**：命中 ¥0.1/M tokens，**未命中 ¥1.0/M**（**10 倍价差**）；
- **延迟**：128K prompt 首 token，未命中 ~13s，命中 ~500ms（**26 倍加速**）；
- **最小缓存单元**：64 tokens（小于 64 不缓存）；
- **TTL**：数小时~数天自动清理，**best-effort 不保证 100%**。

> 官方文档：[api-docs.deepseek.com/zh-cn/guides/kv_cache](https://api-docs.deepseek.com/zh-cn/guides/kv_cache/)

API 响应中通过 `usage.prompt_cache_hit_tokens` 与 `prompt_cache_miss_tokens` 暴露命中数。

### 12.4 dsh 官方能跑到 97-99% 的真正原因

dsh **没有任何黑魔法**，它**只是「没有意外破坏 prefix cache」**。具体两条：

1. **Session 是 append-only 事件日志**（见 `session-persistence-jsonl` / `session-persistence-sqlite`）：历史一旦写入不可变，每轮只在末尾追加新 message → 前 N 轮 token **100% 命中**；
2. **System Prompt 在 agent preset 生命周期内固定**：除非显式切模式（`/mode`），prompt section 不会插时间戳/随机数/CWD。

Reddit 自述数字（**未独立审计，仅供参考**）：
- r/LocalLLaMA 多个 dsh 系 harness 用户报告命中率 ~97%，one-shot SPA 构建总花费 $0.06；
- r/DeepSeek 长会话用户在累计 ~6150 万 input token 后命中率 99%。

> 详细解读：[FindHarness 博客《DeepSeek Harness 如何做到 99% 缓存命中率》](https://findharness.com/zh/blog/deepseek-harness-kv-cache-explained)

### 12.5 dsh-go 对齐官方命中率的 4 项工程纪律

| 纪律编号 | 纪律 | dsh-go 实现位置 | 触发「破缓存」的反例 |
|---|---|---|---|
| **D1** | 严格 append-only fold | `pkg/session/fold.go` 的 `fold*` 投影族只读不写 | ❌ compaction 后回写历史 → 破 |
| **D2** | System Prompt 模板只拷原版 + order 写死 | `pkg/sysprompt/sections/` 严格 copy 原版 section 文本；`pkg/agentloop/constants.go` 写死 waterfall 顺序常量 | ❌ 往 system prompt 插时间戳 / CWD / 随机数 → 破 |
| **D3** | Skills catalog 用 change-only 注入 | `pkg/skill` catalog 变化时通过 `agent.inject()` 写为新 user-message，**不修改** system prefix | ❌ 每次都覆盖 `<available_skills>` → 破 |
| **D4** | Goal Round / Runtime Snapshot 用 PromptContext 落 user-msg | `pkg/sysprompt/context.go` 动态上下文以「user-msg 追加在末尾」实现，**不修改** system prefix | ❌ 把续轮提示写进 system prompt → 破 |

### 12.6 必须主动防御的 4 类「破缓存」反模式

```go
// ❌ 反例 1：把动态时间戳写进 system prompt（每次都破）
systemPrompt := basePrompt + "当前时间: " + time.Now().Format(time.RFC3339)

// ✅ 正确：时间戳走 PromptContext 作为 user-msg 追加到末尾
//    或者做成"date-level"静态提示 + 一个 get_current_time 工具

// ❌ 反例 2：compaction 后回写历史（破坏 append-only）
session.Replace(oldMsgSeq, newSummary)  // ❌ 覆盖源事件

// ✅ 正确：compaction 走 SurfaceOp 表面替换（不修改源事件）
session.SurfaceReplace(start, end, newSummary, surfaceOp)  // ✅

// ❌ 反例 3：每个工具的 JSON Schema 字段顺序随机（schema 区段不命中）
schema := map[string]interface{}{"type":"object", "properties": props}  // map 顺序随机

// ✅ 正确：ValueSchemaSpec DSL 编译为固定顺序的 JSON Schema
//    pkg/tools/schema.go 用 sorted-keys 序列化输出
//    同时按 tool.Name() 排序后再注册到 ToolRegistry
sort.Slice(tools, func(i,j int) bool { return tools[i].Name() < tools[j].Name() })

// ❌ 反例 4：每次重写 <available_skills> 区块内容
for _, s := range allSkills {
    catalog += s.Name + ": " + s.Summary + "\n"  // 顺序随机 → 破
}

// ✅ 正确：按 skill name 字典序排序后再拼接
sort.Slice(allSkills, func(i,j int) bool { return allSkills[i].Name < allSkills[j].Name })
```

### 12.7 验收方法（实现完成后跑）

| 验证项 | 期望 | 验证命令 |
|---|---|---|
| 同一 session 连续 50 轮，无切模式 | 平均 cache hit ≥ **95%** | 调真实 DeepSeek API，对比 `usage.prompt_cache_hit_tokens / prompt_tokens` |
| 切换 agent preset | 该点起命中率为 0%，之后稳定上升 | 走 preset 切换 + 50 轮 |
| 长会话 100 轮 + 中途一次 compaction | comp 之后命中率立刻恢复，30 轮内回到 95%+ | 用 `compaction/start` 事件触发一次 |
| 多 session 并发（不互踩） | 各 session 独立缓存 | 10 个 session 并发跑同 prompt 模板 |
| 加工具 vs 不加工具 | 加工具的命中率应**不降**（工具 schema 排序后保持稳定）| 注册 5/10/20 个工具分别跑 |

> 验收测试用例落到 `tests/cache_hit_rate_e2e_test.go`，用真实 DeepSeek API 跑 50 轮回归。

### 12.8 风险提示

- **DeepSeek 文档没明说工具输出是否计入 prefix**（社区有争议），dsh-go 也只能保证「忠实复刻 dsh 行为」，不能保证 100% 命中；
- **子代理会起独立 session 稀释父会话缓存**（**官方 Reddit 也警告**），dsh-go 复刻此行为 = 同等风险；
- `prompt_cache_key` 字段（Anthropic 风格 `cache_control`）官方当前**不需要**（DeepSeek 服务端自动），但保留字段扩展位，未来切到 Anthropic 风格可启用。

### 12.9 结论

| 问题 | 答案 |
|---|---|
| 是否已有类似开源项目？ | **没有同时满足「Go + 60+ 子系统 + 进程内 SDK」的项目**；官方只有 TS+Python minimal，社区偏 CLI |
| dsh-go 优势？ | **唯一可作为 Go 业务进程内 import 的 DSH 等价 SDK**；事件溯源 + Plan/Goal/Skills + Waterfall + 持久化接缝全套 |
| 实现后缓存命中率能否与官方一致？ | **可对齐到 97-99% 同等水平**，前提是严格遵守 12.5 的 4 项纪律 + 避免 12.6 的 4 类反模式 + 12.7 验收测试全过 |

### 12.10 配套实施计划

> **📄 详见：[docs/CACHE_HIT_RATE_PLAN.md](./docs/CACHE_HIT_RATE_PLAN.md)**
>
> 7 阶段实施路线图：阶段 0 探针埋点（1 周）→ 阶段 1-4 四项纪律（5 周）→ 阶段 5 反模式防御（1 周）→ 阶段 6 E2E 验收（1.5 周）→ 阶段 7 监控看板（1 周）
> 总计 10 人周（2 人并行 4 周），含 4 项纪律的 Go 代码架构、4 类反模式的 lint 工具、5 个 E2E 验收用例、风险与回退方案。
>
> **任务化**：上述计划已拆分为 9 个可追踪任务 N01-N09，详见 [docs/TASKS.md 第六章](./docs/TASKS.md#6-缓存命中率对齐计划cluster-8-cache-affinity--n01-n09) 与 [docs/tasks.json](./docs/tasks.json) 末尾。

---

**（README.md 到此结束 · v2.0 + 任务表锚点 + 竞品对比 + 缓存命中率对齐方案 + 实施计划锚点）**

