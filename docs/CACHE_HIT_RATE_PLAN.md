# dsh-go 缓存命中率对齐方案 · 实施计划

> **目标**：将 dsh-go 实现的 DeepSeek API prefix cache 命中率对齐到 dsh 官方水平（**97-99%**）。
>
> **依据文档**：[README.md 第十二章](../README.md#十二竞品对比--deepseek-缓存命中率对齐方案-)（竞品对比 & 4 项工程纪律 + 4 类反模式 + 验收方法）。
>
> **核心原则**：dsh 官方能跑到 97-99% **没有黑魔法**，只是「**没有意外破坏 prefix cache**」；本计划的所有纪律、检测、验收，都是在确保 dsh-go **不引入新的「破缓存」行为**。
>
> **关联任务表**：[tasks.json](./tasks.json) 中标记为 `cache-affinity` 标签的所有任务 + [TASKS.md](./TASKS.md) 同标签任务。

---

## 0. 总览：7 阶段路线图

```text
┌──────────────────────────────────────────────────────────────────────────┐
│ 阶段 0 · 缓存探针埋点        │ 1 周  │ 🔴 前置依赖 · 阶段 1-7 全部依赖  │
│   └─ pkg/llm/tokenmeter     │       │ (M34 + 增强：缓存探针字段)         │
├──────────────────────────────────────────────────────────────────────────┤
│ 阶段 1 · D1 append-only fold  │ 1.5周│ 🔴 簇 1 Session (M02) 内           │
│   ├─ fold 函数族纯化          │       │                                     │
│   ├─ SurfaceOp 表面替换实现    │       │                                     │
│   └─ 不变量校验 8 条 day-1    │       │                                     │
├──────────────────────────────────────────────────────────────────────────┤
│ 阶段 2 · D2 System Prompt     │ 1.5周│ 🔴 簇 1 SysPrompt (M06/M07/M42)   │
│   ├─ section 模板 strict copy  │       │                                     │
│   ├─ order 常量写死            │       │                                     │
│   └─ 静态检测：禁插动态值     │       │                                     │
├──────────────────────────────────────────────────────────────────────────┤
│ 阶段 3 · D3 Skills catalog    │ 1 周 │ 🟡 簇 5 Skills (M27) 内            │
│   ├─ change-only diff 注入     │       │                                     │
│   ├─ agent.inject() 实现       │       │                                     │
│   └─ catalog 序列化稳定顺序    │       │                                     │
├──────────────────────────────────────────────────────────────────────────┤
│ 阶段 4 · D4 PromptContext     │ 1.5周│ 🟡 SysPrompt (M42) 增强            │
│   ├─ PromptContext 注册表      │       │                                     │
│   ├─ change-only 持久化        │       │                                     │
│   └─ compaction 保留最后一次   │       │                                     │
├──────────────────────────────────────────────────────────────────────────┤
│ 阶段 5 · 4 类反模式防御       │ 1 周 │ 🟡 横切关注点                        │
│   ├─ Schema 序列化定序          │       │                                     │
│   ├─ Tool 注册排序              │       │                                     │
│   ├─ 静态 lint (custom check) │       │                                     │
│   └─ 运行时 invariant           │       │                                     │
├──────────────────────────────────────────────────────────────────────────┤
│ 阶段 6 · E2E 验收测试         │ 1.5周│ 🔴 验收关卡 (T-A 阶段)              │
│   ├─ 50 轮稳定率 ≥ 95%         │       │                                     │
│   ├─ 切 preset 验证             │       │                                     │
│   ├─ compaction 后恢复         │       │                                     │
│   └─ 多 session 并发           │       │                                     │
├──────────────────────────────────────────────────────────────────────────┤
│ 阶段 7 · 监控 + 看板          │ 1 周 │ 🟢 持续运营                          │
│   ├─ OTel 探针导出             │       │                                     │
│   ├─ Grafana 命中率看板        │       │                                     │
│   └─ 缓存破窗告警               │       │                                     │
└──────────────────────────────────────────────────────────────────────────┘
合计：9 ~ 11 人周（2 人并行 5 ~ 6 周）
```

### 0.1 阶段依赖图

```text
                    [阶段 0 探针]
                         │
       ┌─────────────────┼─────────────────┬─────────────────┐
       │                 │                 │                 │
       ▼                 ▼                 ▼                 ▼
[阶段 1 D1]        [阶段 2 D2]        [阶段 3 D3]        [阶段 4 D4]
  append-only         SysPrompt           Skills            PromptContext
       │                 │                 │                 │
       └─────────────────┴─────────────────┴─────────────────┘
                                  │
                                  ▼
                       [阶段 5 反模式防御]
                                  │
                                  ▼
                       [阶段 6 E2E 验收]
                                  │
                                  ▼
                       [阶段 7 监控看板]
```

> **关键约束**：阶段 5 必须在阶段 1-4 全部完成后才能开始（防御代码需要依赖 4 项纪律的实现细节）；阶段 6 必须依赖阶段 5 的防御机制（否则测试无法稳定通过）。

---

## 1. 阶段 0 · 缓存探针埋点基础设施（前置，1 周）

### 1.1 目标
让 dsh-go **每一次 LLM 请求都能精确记录** DeepSeek 返回的 `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens`，作为后续所有阶段验收的「数据基线」。

### 1.2 涉及包
- `pkg/llm/types.go` — `TokenUsage` 字段增强（M34 子项）
- `pkg/llm/tokenmeter.go` — 新增 `CacheStats` 结构
- `pkg/llm/provider/deepseek/client.go` — 解析 SSE 响应中的 usage 字段
- `pkg/llm/tokenmeter.go` — `Measure()` 函数增强

### 1.3 子任务

#### 1.3.1 `TokenUsage` 字段补全
```go
// 文件：pkg/llm/types.go
// 中文注释：DeepSeek API 响应中 usage 字段的标准映射
//   - prompt_tokens = 总输入（含 cache 命中 + 未命中）
//   - prompt_cache_hit_tokens = 命中部分（按缓存价计费）
//   - prompt_cache_miss_tokens = 未命中部分（按原价计费）
//   - 关系：prompt_tokens = prompt_cache_hit_tokens + prompt_cache_miss_tokens
//   - 注意：dsh 原版 LlmFailure 在网络错误时返回的是估算值，需要在 retry 路径覆盖
type TokenUsage struct {
    InputTokens          int `json:"input_tokens"`           // 未缓存输入（不计算 cache）
    OutputTokens         int `json:"output_tokens"`
    TotalTokens          int `json:"total_tokens,omitempty"`
    CacheReadTokens      int `json:"cache_read_tokens,omitempty"`   // 命中（dsh 字段名）
    CacheWriteTokens     int `json:"cache_write_tokens,omitempty"`  // 写入（dsh 字段名）
    ReasoningTokens      int `json:"reasoning_tokens,omitempty"`
    
    // ↓↓↓ 新增：DeepSeek 原生字段（用于缓存命中率精确计算）↓↓↓
    PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`   // DeepSeek 原生
    PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`  // DeepSeek 原生
}
```

#### 1.3.2 `CacheStats` 探针结构
```go
// 文件：pkg/llm/tokenmeter.go
// 中文注释：单次请求的缓存命中率快照，供阶段 6 E2E 测试断言使用
//   - HitRatio = PromptCacheHitTokens / (PromptCacheHitTokens + PromptCacheMissTokens)
//   - 注意：PromptTokens 总量可能为 0（如纯系统消息请求），此时 HitRatio 强制为 1.0（防 NaN）
type CacheStats struct {
    RequestSeq          int       // session 内第几个 turn
    Model               string
    PromptCacheHit      int       // DeepSeek 返回值
    PromptCacheMiss     int       // DeepSeek 返回值
    HitRatio            float64   // 0.0 ~ 1.0
    SessionID           SessionID
    TurnID              TurnID
    Time                time.Time
}

func (c *CacheStats) ComputeHitRatio() float64 {
    total := c.PromptCacheHit + c.PromptCacheMiss
    if total == 0 {
        return 1.0  // 无 prompt 时不视为"未命中"
    }
    return float64(c.PromptCacheHit) / float64(total)
}
```

#### 1.3.3 DeepSeek 客户端解析
```go
// 文件：pkg/llm/provider/deepseek/client.go
// 中文注释：DeepSeek API 响应 usage 字段映射
//   - DeepSeek 文档：https://api-docs.deepseek.com/zh-cn/guides/kv_cache/
//   - prompt_cache_hit_tokens / prompt_cache_miss_tokens 是 DeepSeek 特有字段
//   - 原版 dsh 在 LlmFailure 处理路径中需要把这些字段单独保留（不与 inputTokens 合并）
func (c *Client) parseUsage(raw map[string]interface{}) TokenUsage {
    getInt := func(k string) int {
        v, ok := raw[k]
        if !ok { return 0 }
        switch n := v.(type) {
        case float64: return int(n)
        case int:     return n
        default:      return 0
        }
    }
    return TokenUsage{
        PromptCacheHitTokens:  getInt("prompt_cache_hit_tokens"),
        PromptCacheMissTokens: getInt("prompt_cache_miss_tokens"),
        // 保留 dsh 原版的"未缓存输入 = total - cache"算法
        InputTokens:  getInt("prompt_tokens") - getInt("prompt_cache_hit_tokens"),
        OutputTokens: getInt("completion_tokens"),
    }
}
```

#### 1.3.4 Token Meter 增强
```go
// 文件：pkg/llm/tokenmeter.go（增强 M34）
// 中文注释：每次 LLM 调用结束后自动累加缓存探针
//   - Measure() 接收 TokenUsage + SessionHeader 上下文，写入会话级累计
//   - 累计数据落到 session telemetry ledger（S07）供阶段 7 看板消费
func (m *TokenMeter) Measure(req LLMRequest, resp TokenUsage) (*TokenMeasurement, error) {
    measurement := &TokenMeasurement{
        CacheStats: CacheStats{
            PromptCacheHit:  resp.PromptCacheHitTokens,
            PromptCacheMiss: resp.PromptCacheMissTokens,
        },
    }
    measurement.CacheStats.HitRatio = measurement.CacheStats.ComputeHitRatio()
    return measurement, nil
}
```

### 1.4 验收标准
- ✅ 单元测试：`pkg/llm/tokenmeter_test.go` 验证 `CacheStats.ComputeHitRatio()` 在 0/正常/全命中 三种场景下正确
- ✅ Mock E2E：`pkg/llm/provider/deepseek/client_test.go` 用 mock SSE 响应验证 `parseUsage` 正确解析
- ✅ 日志：每次 LLM 请求后，日志行包含 `cache.hit_ratio=...` 字段

### 1.5 与现有任务的关联
- 增强 **M34 Token Meter**（MUST 优先级）
- 关联 **S07 Session Telemetry**（SHOULD 阶段产出消费方）

---

## 2. 阶段 1 · D1 严格 append-only fold（1.5 周）

### 2.1 目标
确保 Session 的 fold 投影族（`foldPlanMode` / `foldGoal` / `foldTodo` / `foldRequestHeader` / `foldEffectiveSandboxMode` / `foldEffectiveApprovalPolicy` / `foldPermissionPreset` 等）**全部纯函数化**，**禁止任何写回源事件的路径**，并通过 invariant 在运行时拦截非法写。

### 2.2 涉及包
- `pkg/session/fold.go`（M02 子项）
- `pkg/session/invariant.go`（M02 子项 + M33）
- `pkg/session/event.go`（M02）
- `pkg/session/event_data.go`（M02）
- `pkg/compaction/types.go`（S01 预留）

### 2.3 子任务

#### 2.3.1 fold 投影族纯化
```go
// 文件：pkg/session/fold.go
// 中文注释：所有 fold* 函数必须满足"纯函数"约束：
//   1. 输入：events []SessionEvent（不可变）
//   2. 输出：投影状态（不可变，建议使用 value type 而非 pointer）
//   3. 不允许调用：Session.append()、EventBus.emit()、任何 IO
//   4. 不允许读取：除 events 之外的任何外部状态
//   5. 复杂度：O(N) 一次扫描，N = events 长度
// 这是 D1 的核心保障：fold 永远不能"破"prefix cache，
// 因为它根本不修改事件流。
func FoldPlanMode(events []SessionEvent) (active bool, pending bool) {
    var lastActive, lastPending bool
    for _, ev := range events {
        if ev.Type != EventPlanMode { continue }
        data, ok := ev.Data.(PlanModeData)
        if !ok { continue }  // invariant 应当保证不会发生
        lastActive = data.Active
        lastPending = data.Pending
    }
    return lastActive, lastPending
}

// 同模式：FoldGoal / FoldTodo / FoldRequestHeader / FoldEffectiveSandboxMode / 
//        FoldEffectiveApprovalPolicy / FoldPermissionPreset
```

#### 2.3.2 Session 写路径唯一性
```go
// 文件：pkg/session/session.go
// 中文注释：Session 仅暴露 append() 一个写路径
//   - 所有 fold* 投影必须从 append 后的 events 派生
//   - 禁止任何"重写历史"的 public/private 方法
//   - 唯一允许"修改历史"的是 compaction，但必须通过 SurfaceOp 字段（见 2.3.3）
// 这是 D1 的根本保障：除 compaction 外，没有任何方法能"破坏 append-only"。
type Session struct {
    events []SessionEvent  // 私有，外部只读
    header SessionHeader
    mu     sync.RWMutex
}

// append() 是唯一写路径，返回新事件索引
func (s *Session) Append(ev SessionEvent) (int, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    // invariant: seq 严格连续
    expectedSeq := len(s.events) + 1
    if ev.Seq != expectedSeq {
        return 0, &InvariantError{
            Code: "SEQ_NOT_CONTINUOUS",
            Expected: expectedSeq,
            Actual: ev.Seq,
        }
    }
    
    // invariant: time 单调不减
    if len(s.events) > 0 && ev.Time < s.events[len(s.events)-1].Time {
        return 0, &InvariantError{
            Code: "TIME_NOT_MONOTONIC",
        }
    }
    
    s.events = append(s.events, ev)
    return ev.Seq, nil
}

// 公开的"读"路径
func (s *Session) Events() []SessionEvent {
    s.mu.RLock()
    defer s.mu.RUnlock()
    // 返回深拷贝防止外部修改
    cp := make([]SessionEvent, len(s.events))
    copy(cp, s.events)
    return cp
}
```

#### 2.3.3 SurfaceOp 表面替换（compaction 唯一合法写历史方式）
```go
// 文件：pkg/session/event_data.go
// 中文注释：SurfaceOp 是 compaction 表面替换的不变量
//   - 物理事件流：永远 append-only（seq 连续，time 单调）
//   - 逻辑派生：deriveMessages() 读到 user/message 时，
//     如果后续有 SurfaceOp{op:replace, start:ev.Seq, end:ev.Seq+5} 事件，
//     就用 SurfaceOp.Data 替换原 5 条 user/assistant 消息
//   - 不修改源事件，只是"读时"做表面替换
//   - 这是 D1 的根本保证：compaction 不能"回写"破坏 prefix cache
type SurfaceOp struct {
    Op    string  // "replace"
    Start int     // 起始 event.seq
    End   int     // 结束 event.seq
    Data  string  // 替换内容（如 LLM 生成的摘要）
}
```

#### 2.3.4 不变量校验 8 条 day-1
```go
// 文件：pkg/session/invariant.go（增强 M33）
// 中文注释：M33 必做的 8 条不变量是 D1 的运行时护栏
//   - 每条 Session.append() 都强制校验
//   - 任何一条失败：抛 InvariantError + 拒绝 append
//   - 同时登记到 invariant ledger（M33）便于审计
var CoreInvariants = []Invariant{
    {
        Name: "seq_continuous",
        Check: func(s *Session) error {
            for i, ev := range s.events {
                if ev.Seq != i+1 {
                    return fmt.Errorf("seq break at %d: got %d", i, ev.Seq)
                }
            }
            return nil
        },
    },
    {
        Name: "time_monotonic",
        Check: func(s *Session) error {
            for i := 1; i < len(s.events); i++ {
                if s.events[i].Time < s.events[i-1].Time {
                    return fmt.Errorf("time break at seq %d", s.events[i].Seq)
                }
            }
            return nil
        },
    },
    {
        Name: "turn_start_end_paired",
        Check: func(s *Session) error {
            depth := 0
            for _, ev := range s.events {
                switch ev.Type {
                case EventTurnStart:
                    depth++
                case EventTurnEnd:
                    depth--
                    if depth < 0 {
                        return fmt.Errorf("turn_end before turn_start at seq %d", ev.Seq)
                    }
                }
            }
            // depth 必须为 0（除非 interrupted 冷合成，由 crash recovery 处理）
            if depth != 0 && !s.crashRecovered {
                return fmt.Errorf("unpaired turn events: depth=%d", depth)
            }
            return nil
        },
    },
    // ... 5 more invariants: step_start_end_paired, approval_asked_decided_paired,
    //     goal_revision_cas, tool_call_result_paired, persistence_format_consistent
}
```

### 2.4 验收标准
- ✅ 单元测试：每个 fold* 函数跑 1000 条事件压测（用 go-fuzz 生成随机事件序列验证纯函数性）
- ✅ 单元测试：任何"写回历史"的尝试都被 `Session` 编译期拒绝（不暴露相应方法）
- ✅ 不变量测试：人为制造 seq 跳号、time 回退、turn 不配对，invariant 必须捕获
- ✅ E2E：50 轮正常对话后，session 文件 `seq` 严格 1..N 连续、`time` 严格单调

### 2.5 与现有任务的关联
- 主体：**M02 Session Event Log + Header + fold 投影**（MUST）
- 配套：**M33 Invariant Registry**（MUST）
- 配套：**M40 Session Projections**（MUST 二次扫描新增）
- 配套：**S01 Compaction**（SHOULD）

---

## 3. 阶段 2 · D2 System Prompt 模板 + order 写死（1.5 周）

### 3.1 目标
**严格拷贝 dsh 原版** 的 System Prompt section 文本与 order 顺序，并确保**任何动态内容（如时间戳、CWD）都不能注入到 system prompt**，只能通过 PromptContext（阶段 4）以 user-msg 形式追加。

### 3.2 涉及包
- `pkg/sysprompt/section.go`（M06）
- `pkg/sysprompt/assembler.go`（M06）
- `pkg/sysprompt/sections/persona.go`（M06）
- `pkg/sysprompt/sections/policy.go`（M06）
- `pkg/sysprompt/sections/runtime_ctx.go`（M07）
- `pkg/sysprompt/sections/plan_policy.go`（M16 消费端）
- `pkg/agentloop/constants.go`（M12 — 写死 waterfall 顺序）

### 3.3 子任务

#### 3.3.1 Section 模板严格 copy
```go
// 文件：pkg/sysprompt/sections/persona.go
// 中文注释：必须 day-1 从 dsh 原版 package/core/system-prompt/src/sections/persona.ts
//          严格拷贝文本（一字不差），包括：
//   - 角色定义
//   - 行为约束
//   - 任何标点 / 换行 / 缩进
// 严禁自由发挥 / 改写 / 翻译 / "优化"。
// 这是 D2 的根本：原版 section 文本是"模型表现得像 DeepSeek"的灵魂。
const PersonaSection = `You are DeepSeek, a powerful AI assistant.
...（严格拷贝原版文本，此处省略约 200 行）...
You must always respond in a way that is consistent with the above persona.`
```

```go
// 文件：pkg/sysprompt/sections/plan_policy.go
// 中文注释：Plan Mode section 严格拷贝 dsh 原版 plan/policy
//   - order 必须是 500（与原版对齐）—— 改 order 会改变模型看到 section 的相对位置
//   - 文本严格 copy —— 改一个词都可能让模型在 plan mode 下行为漂移
const PlanPolicyOrder = 500  // ⚠️ 不要改这个数

const PlanPolicySection = `In plan mode, you should first output a detailed plan
...（严格拷贝原版 plan/policy 文本）...
Use the exit_plan_mode tool to submit your plan for user approval.`
```

#### 3.3.2 order 常量写死 + 排序机制
```go
// 文件：pkg/sysprompt/section.go
// 中文注释：所有 section 的 order 必须用 Go 常量定义，编译期写死
//   - Section.Order() 读 const 字段，运行时不能被覆盖
//   - Assembler 排序按 Order 升序拼接
//   - 关键 order 列表（与 dsh 原版对齐）：
//     - persona:    100
//     - policy:     200
//     - runtime_ctx: 300
//     - plan_policy: 500
//     - skill_catalog: 600
//   - 严禁动态 order：plugin 不能注册"order=time.Now()"的 section
type Section interface {
    Name() string
    Order() int       // 读 const 字段
    Render(ctx Context) string  // 纯函数，禁止 IO / 随机数 / time.Now()
}

// 排序保证
func (a *Assembler) Assemble(ctx Context) string {
    sections := a.registry.List()
    sort.SliceStable(sections, func(i, j int) bool {
        return sections[i].Order() < sections[j].Order()
    })
    var sb strings.Builder
    for _, s := range sections {
        sb.WriteString(s.Render(ctx))
        sb.WriteString("\n\n")
    }
    return sb.String()
}
```

#### 3.3.3 静态检测：禁插动态值
```go
// 文件：pkg/sysprompt/static_check.go
// 中文注释：编译期 + 运行时双层防御，禁插动态值
//   - 静态：grep 扫描所有 .go 文件，禁出现以下模式：
//     - systemPrompt + "..." + time.Now()
//     - systemPrompt + fmt.Sprintf(..., runtime.Cwd())
//     - systemPrompt + randomValue()
//   - 运行时：Section.Render() 接受 io.Writer + Recorder，
//     如果检测到调用 time.Now() / os.Getwd() / math/rand 立即 panic
//
// 工具链集成：
//   - Makefile target: `make check-cache-safety`
//   - CI 必跑，否则 merge 阻塞
type PureSection interface {
    Render(ctx Context) string
}

// 运行时检测：通过 context.WithValue 注入 Recorder
type contextKey string
const recorderKey contextKey = "section-recorder"

func RenderWithDetection(p PureSection, ctx Context) (string, error) {
    recorder := &Recorder{}
    ctx = context.WithValue(ctx, recorderKey, recorder)
    defer func() {
        if r := recover(); r != nil {
            // 检测到非法调用
            panic(&InvariantError{
                Code: "SECTION_NOT_PURE",
                Section: p.Name(),
                Reason: fmt.Sprintf("%v", r),
            })
        }
    }()
    return p.Render(ctx), nil
}
```

```bash
# 文件：scripts/check-cache-safety.sh
# 中文注释：CI 跑这个脚本，扫描 pkg/sysprompt/ 下所有 section 文件
#   - grep 动态值模式
#   - go vet 自定义 check
#   - 必须 0 errors 才算通过
#!/bin/bash
set -e

# 1. 扫描动态 API 调用
forbidden_patterns=(
    "time\.Now()"
    "os\.Getwd()"
    "os\.Getenv.*DSH_"
    "math/rand"
    "crypto/rand"
    "runtime\.NumGoroutine"
)

for pat in "${forbidden_patterns[@]}"; do
    matches=$(grep -rn "$pat" pkg/sysprompt/sections/ || true)
    if [ -n "$matches" ]; then
        echo "❌ Cache-safety violation: $pat"
        echo "$matches"
        exit 1
    fi
done

echo "✅ Cache-safety check passed"
```

### 3.4 验收标准
- ✅ 静态扫描：`make check-cache-safety` 通过，0 violations
- ✅ 单元测试：每个 section 的 `Render()` 跑 1000 次，输出**逐字节相同**（证明纯函数）
- ✅ 字符串比对：与 dsh 原版 section 文本 `diff` 必须**完全一致**
- ✅ 集成测试：50 轮对话后，logger 中记录的 `system_prompt_hash` 跨轮**完全相同**

### 3.5 与现有任务的关联
- 主体：**M06 System Prompt Assembler + Section Registry**（MUST）
- 配套：**M07 Runtime-Context Snapshot**（MUST，但**不能**写时间戳到 system prompt）
- 配套：**M16 Plan Mode**（MUST，依赖 plan:policy section）

---

## 4. 阶段 3 · D3 Skills catalog change-only 注入（1 周）

### 4.1 目标
Skills `<available_skills>` 区块只在 catalog 变化时通过 `agent.inject()` 写入新 user-message，**绝不修改 system prompt**。catalog 文本本身按 skill name 字典序稳定输出。

### 4.2 涉及包
- `pkg/skill/registry.go`（M27）
- `pkg/skill/provider_fs.go`（M27）
- `pkg/skill/tool.go`（M27）
- `pkg/agent/inject.go`（M11 子项）

### 4.3 子任务

#### 4.3.1 catalog 内容稳定序列化
```go
// 文件：pkg/skill/registry.go
// 中文注释：catalog 文本必须稳定序列化，跨 session 跨进程一致
//   1. 按 skill name 字典序排序
//   2. 每个 skill 摘要：name + 固定 1 行 summary
//   3. 字段分隔符固定（"|"）
//   4. 严禁插入时间戳、可用性标记、动态状态
//   5. 严禁字段顺序随机
func (r *SkillRegistry) CatalogText() string {
    summaries := r.ListSummaries()  // 内部已按 name 排序
    var sb strings.Builder
    sb.WriteString("<available_skills>\n")
    for _, s := range summaries {
        sb.WriteString(s.Name)
        sb.WriteString("|")
        sb.WriteString(s.Summary)
        sb.WriteString("\n")
    }
    sb.WriteString("</available_skills>")
    return sb.String()
}
```

#### 4.3.2 change-only 检测 + inject
```go
// 文件：pkg/skill/tool.go
// 中文注释：只在 catalog 内容变化时通过 agent.inject() 注入 user-message
//   - 注入位置：在最后一次"user message"之后、"assistant message"之前
//   - 注入内容：完整 catalog 文本（如上）
//   - 不修改 system prompt（保持 system prefix 稳定）
//   - agent.pre-step waterfall 中检测，diff 旧值 vs 新值
type CatalogSnapshot struct {
    Hash      string  // SHA256(CatalogText())
    Text      string
    UpdatedAt time.Time
}

func (s *SkillRegistry) MaybeInjectCatalog(agent Agent) bool {
    newText := s.CatalogText()
    newHash := sha256.Sum256([]byte(newText))
    
    if s.lastSnapshot != nil && s.lastSnapshot.Hash == hex.EncodeToString(newHash[:]) {
        return false  // 无变化，不注入
    }
    
    // 有变化：注入新 user-message
    s.lastSnapshot = &CatalogSnapshot{
        Hash: hex.EncodeToString(newHash[:]),
        Text: newText,
    }
    
    agent.Inject(UserMessage{
        Source: MessageSourceSystem,
        Content: newText,
        Marker: "<skill-catalog-update>",
    })
    return true
}
```

#### 4.3.3 fsnotify 实时刷新 + 缓存失效
```go
// 文件：pkg/skill/provider_fs.go
// 中文注释：fsnotify 监听技能目录，文件变更后失效缓存
//   - 监听 rank 100~600 的 6 层目录
//   - Windows 平台用 polling fallback（Go fsnotify 在 Windows 上有 bug）
//   - 任何变化触发 registry 全量 rescan + CatalogText 重算
//   - 上层 MaybeInjectCatalog 检测到 hash 变化后自动 inject
func (p *LocalFilesystemProvider) watch(ctx context.Context) {
    watcher, _ := fsnotify.NewWatcher()
    for _, dir := range p.scanRoots() {
        watcher.Add(dir)
    }
    for {
        select {
        case ev := <-watcher.Events:
            if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove) != 0 {
                p.invalidateCache()
                // 触发 agent.pre-step 重新跑 MaybeInjectCatalog
            }
        case <-ctx.Done():
            return
        }
    }
}
```

### 4.4 验收标准
- ✅ 单元测试：CatalogText() 跨 1000 次调用**逐字节相同**（无随机、无时间戳）
- ✅ 单元测试：添加一个 skill → next call CatalogText() hash 变化 → inject 触发
- ✅ 单元测试：删除一个 skill → 再次变化 → 再次 inject
- ✅ 集成测试：fsnotify mock 触发 → 5 个 skill 增删改 → inject 次数正确
- ✅ E2E：50 轮对话中 skills 不变时，`<available_skills>` 区块**只注入 1 次**（首次）

### 4.5 与现有任务的关联
- 主体：**M27 Skill Registry + 6 层 FS Provider + skill 工具**（MUST）
- 配套：**M11 Agent Registry**（`Agent.Inject()` 方法）

---

## 5. 阶段 4 · D4 PromptContext 落 user-msg（1.5 周）

### 5.1 目标
所有动态内容（时间戳、Goal 状态、Plan Mode 状态、Approval Policy 状态、Runtime 快照）通过 `PromptContext` 机制以 user-msg 形式追加到历史末尾，**绝不修改 system prompt**。compaction 时**保留最后一次完整 snapshot**，不能误裁。

### 5.2 涉及包
- `pkg/sysprompt/context.go`（M42）
- `pkg/sysprompt/sections/runtime_ctx.go`（M07）
- `pkg/agentloop/step.go`（M12 — pre-step 注入位置）
- `pkg/compaction/engine.go`（S01 — compaction 时保留 snapshot）

### 5.3 子任务

#### 5.3.1 PromptContext 注册表
```go
// 文件：pkg/sysprompt/context.go
// 中文注释：PromptContext 是动态上下文的注册中心
//   - 与 PromptSection 并行注册，区别：
//     Section → system prompt 前缀（静态）
//     Context → user-msg 追加在历史末尾（动态，change-only 持久化）
//   - 每个 context 有一个 hash，hash 变化时才注入新 user-msg
//   - hash 不变时不注入（节省 token + 保持 prefix 稳定）
type PromptContext struct {
    Name string
    Order int  // 多个 context 之间的相对顺序
    Compute func(s *Session) string  // 纯函数，输入 session 状态
    LastHash string  // 内部状态
}

func (a *Assembler) RenderContexts(s *Session) []UserMessage {
    var msgs []UserMessage
    contexts := a.contextRegistry.List()
    sort.SliceStable(contexts, func(i, j int) bool {
        return contexts[i].Order < contexts[j].Order
    })
    
    for _, ctx := range contexts {
        content := ctx.Compute(s)
        h := sha256.Sum256([]byte(content))
        hashStr := hex.EncodeToString(h[:])
        
        if ctx.LastHash == hashStr {
            continue  // 无变化，不注入
        }
        ctx.LastHash = hashStr
        
        msgs = append(msgs, UserMessage{
            Source: MessageSourceContext,
            Name: ctx.Name,
            Content: content,
        })
    }
    return msgs
}
```

#### 5.3.2 各种动态 context 实现
```go
// 文件：pkg/sysprompt/sections/runtime_ctx.go
// 中文注释：runtime context 提供当前 plan/goal/approval/sandbox 状态
//   - 内容是 markdown 表格，纯文本，无随机
//   - 每次 session 状态变化时 hash 变化 → 注入新 user-msg
//   - 注意：CWD 走"date-level"静态提示 + get_cwd 工具，不写 time.Now()
var RuntimeContextOrder = 300

var RuntimeContext = PromptContext{
    Name: "runtime-context",
    Order: RuntimeContextOrder,
    Compute: func(s *Session) string {
        var sb strings.Builder
        sb.WriteString("<runtime-context>\n")
        
        active, _ := FoldPlanMode(s.Events())
        sb.WriteString(fmt.Sprintf("Plan mode: %s\n", 
            map[bool]string{true: "active (you must first output a plan, then call exit_plan_mode)",
                           false: "off"}[active]))
        
        goals := FoldGoal(s.Events())
        sb.WriteString(fmt.Sprintf("Active goals: %d\n", len(goals)))
        for _, g := range goals {
            sb.WriteString(fmt.Sprintf("  - [%s] %s (rounds=%d/%d)\n", 
                g.Phase, g.Objective, g.RoundsStarted, g.MaxGoalRounds))
        }
        
        sb.WriteString(fmt.Sprintf("Approval policy: %s\n", FoldEffectiveApprovalPolicy(s.Events())))
        sb.WriteString(fmt.Sprintf("Sandbox mode: %s\n", FoldEffectiveSandboxMode(s.Events())))
        sb.WriteString("</runtime-context>")
        return sb.String()
    },
}
```

#### 5.3.3 Goal Round Driver 续轮提示
```go
// 文件：pkg/goal/round_driver.go
// 中文注释：goal-round-driver 订阅 turn-stopping 事件
//   - 当 goal.active 且 roundsStarted < maxGoalRounds 时
//   - 注入 <goal-round> 续轮提示作为 user-msg（不是 system prompt）
//   - 这样 goal 内容变化时只破当前这一段，前面的历史仍命中
var GoalRoundContext = PromptContext{
    Name: "goal-round",
    Order: 400,  // 在 runtime context 之后
    Compute: func(s *Session) string {
        goals := FoldGoal(s.Events())
        var sb strings.Builder
        for _, g := range goals {
            if g.Phase != "active" { continue }
            if g.RoundsStarted >= g.MaxGoalRounds { continue }
            sb.WriteString(fmt.Sprintf(
                "<goal-round>\n"+
                "Goal '%s' is still active (round %d/%d).\n"+
                "Continue working on it. Call goal_mark_complete when done.\n"+
                "</goal-round>\n", 
                g.Objective, g.RoundsStarted, g.MaxGoalRounds))
        }
        return sb.String()
    },
}
```

#### 5.3.4 Compaction 保留最后 snapshot
```go
// 文件：pkg/compaction/engine.go
// 中文注释：compaction 必须保留最后一次完整 PromptContext snapshot
//   - 原因：context 是动态状态，comp 删多了模型会"忘记"当前模式/goal
//   - 实现：compaction 算法选 range 时，**强制保留最后一条 context-msg**
//   - 同时保留最近 N 轮的 user/assistant（普通对话可裁，context 不可裁）
func (e *BasicEngine) SelectCompactionRange(events []SessionEvent) (start, end int) {
    // 找到最后一条 Context 消息的 seq
    lastContextSeq := 0
    for _, ev := range events {
        if ev.Type == EventUserMessage {
            data, _ := ev.Data.(UserMessageData)
            if data.Source == MessageSourceContext {
                lastContextSeq = ev.Seq
            }
        }
    }
    
    // 范围 = [0, lastContextSeq-1]
    // 即：从最早开始裁，但保留 lastContextSeq 之后所有内容
    return 0, lastContextSeq - 1
}
```

### 5.4 验收标准
- ✅ 单元测试：RuntimeContext.Compute() 在 session 状态变化时 hash 变化，无变化时 hash 稳定
- ✅ 单元测试：GoalRoundContext 在 goal.complete 后自动停止注入
- ✅ 单元测试：compaction 后，最后一次 context snapshot 仍可在 derived messages 中找到
- ✅ E2E：50 轮对话 + 1 次 plan mode 切换 + 1 次 goal 设置 → 命中率仍 ≥ 95%

### 5.5 与现有任务的关联
- 主体：**M42 Prompt Context**（MUST 二次扫描新增）
- 配套：**M07 Runtime-Context Snapshot**（MUST，但实现方式改为 Context）
- 配套：**M17 Goal System**（MUST）
- 配套：**S01 Compaction**（SHOULD）

---

## 6. 阶段 5 · 4 类反模式防御（1 周）

### 6.1 目标
为 README 12.6 列出的 4 类反模式构建**静态检测 + 运行时防御**双层防护。

### 6.2 涉及包
- `pkg/tools/schema.go`（M09）
- `pkg/tools/registry.go`（M08）
- `pkg/tools/define.go`（M08）
- `internal/lint/cache_safety.go`（自定义 lint 工具）
- `pkg/skill/registry.go`（M27，已在阶段 3 实现 catalog 排序）

### 6.3 子任务

#### 6.3.1 反模式 1 防御：禁插时间戳到 system prompt
```go
// 文件：pkg/sysprompt/static_check.go（阶段 2 已实现）
// 中文注释：见 3.3.3，此处不再重复
//   - 静态扫描 + 运行时 Recorder 双层
//   - 任何 time.Now() / os.Getwd() / rand 调用都被捕获
```

#### 6.3.2 反模式 2 防御：compaction SurfaceOp
```go
// 文件：pkg/session/session.go
// 中文注释：Session 暴露的写路径仅 append()，不允许 Replace()
//   - 唯一允许"修改历史"的是 SurfaceOp
//   - 但 SurfaceOp 是"读时替换"，物理事件流不变
//   - 这保证 compaction 后事件 seq 仍连续、time 仍单调
//
// ❌ 错误：暴露 Replace(seq, newEvent) public 方法
// ✅ 正确：compaction 流程只追加 compaction/* 事件 + SurfaceOp
type CompactionEngine interface {
    Compact(ctx context.Context, s *Session, opts CompactOptions) error
    // Compact 内部只允许调用 s.Append()，不允许任何 in-place 修改
}
```

#### 6.3.3 反模式 3 防御：JSON Schema 序列化定序
```go
// 文件：pkg/tools/schema.go
// 中文注释：ValueSchemaSpec DSL 编译为 JSON Schema 时必须字典序输出
//   1. properties 字段按 key 字典序
//   2. required 字段按字典序
//   3. enum 值按字典序
//   4. 严禁 map[string]interface{} 直接 JSON.Marshal（Go map 顺序随机）
//
// 实现：所有 Schema 编译为固定顺序的 json.RawMessage
func compileToJSONSchema(spec ValueSchemaSpec) json.RawMessage {
    // 1. 排序 properties
    keys := make([]string, 0, len(spec.Properties))
    for k := range spec.Properties {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    
    // 2. 按排序后顺序构造 json.RawMessage
    var buf bytes.Buffer
    buf.WriteString(`{"type":"object","properties":{`)
    for i, k := range keys {
        if i > 0 { buf.WriteString(",") }
        propJSON, _ := compileToJSONSchema(spec.Properties[k])
        buf.WriteString(fmt.Sprintf("%q:%s", k, propJSON))
    }
    buf.WriteString(`}`)
    
    // 3. required 排序
    required := append([]string{}, spec.Required...)
    sort.Strings(required)
    if len(required) > 0 {
        reqJSON, _ := json.Marshal(required)
        buf.WriteString(fmt.Sprintf(",%q:%s", "required", reqJSON))
    }
    
    buf.WriteString(`}`)
    return buf.Bytes()
}
```

#### 6.3.4 反模式 4 防御：Tool 注册排序
```go
// 文件：pkg/tools/registry.go
// 中文注释：ToolRegistry 暴露的工具列表必须按 name 字典序返回
//   - 序列化到 system prompt 的 tool 列表按此顺序
//   - 增删工具后顺序不变（仅当 name 稳定）
//   - 严禁按注册顺序返回（map 迭代随机）
func (r *ToolRegistry) List() []ToolDefinition {
    tools := r.all()  // 内部 map
    names := make([]string, 0, len(tools))
    for name := range tools {
        names = append(names, name)
    }
    sort.Strings(names)  // 字典序
    
    result := make([]ToolDefinition, len(names))
    for i, name := range names {
        result[i] = tools[name]
    }
    return result
}
```

#### 6.3.5 自定义 lint 工具
```go
// 文件：internal/lint/cache_safety.go
// 中文注释：go vet 自定义 check，扫描整个 pkg/ 目录
//   - 检测所有"已知会破缓存"的 API 调用模式
//   - 提供 IDE 集成（VSCode golangci-lint）
//   - 集成到 CI
package lint

import (
    "go/ast"
    "golang.org/x/tools/go/analysis"
)

var CacheSafetyAnalyzer = &analysis.Analyzer{
    Name: "cache_safety",
    Doc:  "检查是否破坏 DeepSeek prefix cache 的常见反模式",
    Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
    for _, file := range pass.Files {
        ast.Inspect(file, func(n ast.Node) bool {
            call, ok := n.(*ast.CallExpr)
            if !ok { return true }
            
            sel, ok := call.Fun.(*ast.SelectorExpr)
            if !ok { return true }
            
            // 检测：pkg/sysprompt/ 下文件调用 time.Now() / os.Getwd()
            if isInPkg(pass, file, "pkg/sysprompt/sections") {
                if isCallTo(sel, "time", "Now") {
                    pass.Reportf(call.Pos(), 
                        "system prompt section 不能调用 time.Now()（会破 cache）")
                }
                if isCallTo(sel, "os", "Getwd") {
                    pass.Reportf(call.Pos(),
                        "system prompt section 不能调用 os.Getwd()（会破 cache）")
                }
            }
            
            // 检测：pkg/tools/schema.go 直接用 json.Marshal(map)
            if isInPkg(pass, file, "pkg/tools") {
                if isCallTo(sel, "json", "Marshal") {
                    // 检查参数是否是 map
                    if len(call.Args) > 0 {
                        if _, isMap := call.Args[0].(*ast.CompositeLit); isMap {
                            pass.Reportf(call.Pos(),
                                "工具 schema 禁止 json.Marshal(map)（map 顺序随机破坏 cache），请用 compileToJSONSchema")
                        }
                    }
                }
            }
            
            return true
        })
    }
    return nil, nil
}
```

```bash
# 文件：Makefile
# 中文注释：CI 必跑 cache-safety 检查
.PHONY: check-cache-safety
check-cache-safety:
    go vet -vettool=$(which cache_safety_lint) ./pkg/...
    bash scripts/check-cache-safety.sh
```

### 6.4 验收标准
- ✅ 静态扫描：所有 4 类反模式触发时，lint 输出明确错误信息
- ✅ 单元测试：人为注入反模式代码 → CI 失败
- ✅ 回归测试：阶段 1-4 完成后，命中率 ≥ 95%；本阶段完成后应 ≥ 97%

### 6.5 与现有任务的关联
- 主体：**M09 ValueSchemaSpec DSL**（MUST）
- 配套：**M08 Tools Pipeline**（MUST）

---

## 7. 阶段 6 · E2E 验收测试（1.5 周）

### 7.1 目标
跑 README 12.7 列出的 5 项验收用例，每项都需要**可重复、可度量**的通过条件。

### 7.2 涉及包
- `tests/cache_hit_rate_e2e_test.go`（新增）
- `tests/testutil/deepseek_mock.go`（Mock DeepSeek 服务器）
- `pkg/llm/provider/deepseek/client.go`（生产代码）

### 7.3 子任务

#### 7.3.1 测试基础设施
```go
// 文件：tests/testutil/deepseek_mock.go
// 中文注释：Mock DeepSeek API 服务器
//   - 实现"prefix cache 模拟"：跟踪每次请求的 prompt hash，hit 时返回 hit_tokens
//   - 模拟"切 preset 后 cache 失效"：preset 变化时丢弃所有缓存
//   - 模拟"compaction 后恢复"：检测到 surface replacement 时重新建立 cache
type MockDeepSeekServer struct {
    cachedPrefixes map[string]int  // hash → cached token count
    currentPreset  string
    mu             sync.Mutex
}

func (m *MockDeepSeekServer) HandleChat(w http.ResponseWriter, r *http.Request) {
    var req ChatRequest
    json.NewDecoder(r.Body).Decode(&req)
    
    m.mu.Lock()
    defer m.mu.Unlock()
    
    // 计算 prompt hash
    promptText := serializeMessages(req.Messages)
    hash := sha256.Sum256([]byte(promptText))
    hashStr := hex.EncodeToString(hash[:])
    
    // 检查是否命中
    hitTokens, ok := m.cachedPrefixes[hashStr]
    if !ok {
        // 模拟服务端的"落盘"逻辑
        m.cachedPrefixes[hashStr] = countTokens(promptText)
        hitTokens = 0
    }
    
    missTokens := countTokens(promptText) - hitTokens
    
    // 返回 SSE 响应
    w.Header().Set("Content-Type", "text/event-stream")
    fmt.Fprintf(w, "data: {\"usage\":{\"prompt_tokens\":%d,\"prompt_cache_hit_tokens\":%d,\"prompt_cache_miss_tokens\":%d,\"completion_tokens\":50}}\n\n",
        countTokens(promptText), hitTokens, missTokens)
    fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Mock response\"}}]}\n\n")
    fmt.Fprintf(w, "data: [DONE]\n\n")
}
```

#### 7.3.2 E2E 测试 1：50 轮稳定率 ≥ 95%
```go
// 文件：tests/cache_hit_rate_e2e_test.go
// 中文注释：50 轮对话，验证平均命中率 ≥ 95%
//   - 每个 user message 后采样一次 CacheStats
//   - 计算 50 次的平均 HitRatio
//   - 必须 ≥ 0.95
func TestE2E_50Turns_StableHitRate(t *testing.T) {
    if testing.Short() { t.Skip("e2e") }
    
    mock := testsutil.NewMockDeepSeekServer()
    defer mock.Close()
    
    agent := dsh.NewAgent(dsh.Config{
        Provider: "deepseek-mock",
        BaseURL:  mock.URL(),
        SkillsDirs: []string{"./fixtures/skills"},
    })
    
    var hitRatios []float64
    for i := 0; i < 50; i++ {
        result, _ := agent.Run(ctx, fmt.Sprintf("测试消息 %d", i))
        hitRatios = append(hitRatios, result.CacheStats.HitRatio)
    }
    
    avg := average(hitRatios)
    require.GreaterOrEqual(t, avg, 0.95, 
        "50 轮平均命中率 %.2f%% 低于 95%%", avg*100)
    
    t.Logf("✅ 50 轮平均命中率: %.2f%%", avg*100)
    for i, r := range hitRatios {
        t.Logf("  Round %2d: %.2f%%", i+1, r*100)
    }
}
```

#### 7.3.3 E2E 测试 2：切 preset 验证
```go
// 中文注释：切换 agent preset 后，命中率从 0 恢复
//   - Round 1-20: standard preset
//   - Round 21-40: 切到 minimal preset（系统 prompt 改变）→ 命中率预期为 0
//   - Round 41-60: 仍 minimal → 命中率应稳定上升
func TestE2E_PresetSwitch_CacheInvalidates(t *testing.T) {
    mock := testsutil.NewMockDeepSeekServer()
    defer mock.Close()
    
    agent := dsh.NewAgent(dsh.Config{...})
    
    // Phase 1: standard
    for i := 0; i < 20; i++ {
        agent.Run(ctx, fmt.Sprintf("msg %d", i))
    }
    
    // Switch preset
    agent.SwitchPreset("minimal")
    
    // Phase 2: minimal, 期望前几轮命中率低
    for i := 0; i < 5; i++ {
        result, _ := agent.Run(ctx, fmt.Sprintf("msg %d", i))
        require.Less(t, result.CacheStats.HitRatio, 0.5,
            "切换 preset 后第 %d 轮命中率应 < 50%%", i)
    }
    
    // Phase 3: minimal 稳定
    for i := 0; i < 15; i++ {
        result, _ := agent.Run(ctx, fmt.Sprintf("msg %d", i))
    }
    // 最后 5 轮平均应 ≥ 80%
}
```

#### 7.3.4 E2E 测试 3：compaction 后恢复
```go
// 中文注释：100 轮 + 中途一次 compaction
//   - 验证 compaction 不破坏 prefix
//   - 验证最后一次 context snapshot 保留
func TestE2E_CompactionHitRateRecovery(t *testing.T) {
    mock := testsutil.NewMockDeepSeekServer()
    defer mock.Close()
    
    agent := dsh.NewAgent(dsh.Config{...})
    sessionID := agent.NewSession()
    
    // Round 1-50: 稳定命中
    for i := 0; i < 50; i++ {
        agent.RunOnSession(ctx, sessionID, fmt.Sprintf("msg %d", i))
    }
    
    // 触发 compaction
    agent.CompactSession(ctx, sessionID, dsh.CompactOptions{
        Strategy: "pressure",
    })
    
    // Round 51-100: 命中率应在 30 轮内恢复到 ≥ 95%
    var recoveredAt int = -1
    for i := 50; i < 100; i++ {
        result, _ := agent.RunOnSession(ctx, sessionID, fmt.Sprintf("msg %d", i))
        if result.CacheStats.HitRatio >= 0.95 && recoveredAt == -1 {
            recoveredAt = i - 50
        }
    }
    
    require.GreaterOrEqual(t, recoveredAt, 0, "compaction 后从未恢复到 95%")
    require.LessOrEqual(t, recoveredAt, 30, "恢复耗时 %d 轮，超过 30 轮上限", recoveredAt)
}
```

#### 7.3.5 E2E 测试 4：多 session 并发
```go
// 中文注释：10 个并发 session 互不干扰
//   - 每个 session 独立命中自己的 cache
//   - 验证 prompt_cache_key 或 session 隔离机制
func TestE2E_MultiSession_NoInterference(t *testing.T) {
    mock := testsutil.NewMockDeepSeekServer()
    defer mock.Close()
    
    var wg sync.WaitGroup
    results := make([][]float64, 10)
    
    for sessionIdx := 0; sessionIdx < 10; sessionIdx++ {
        wg.Add(1)
        go func(idx int) {
            defer wg.Done()
            agent := dsh.NewAgent(dsh.Config{...})
            for i := 0; i < 20; i++ {
                result, _ := agent.Run(ctx, fmt.Sprintf("session-%d-msg-%d", idx, i))
                results[idx] = append(results[idx], result.CacheStats.HitRatio)
            }
        }(sessionIdx)
    }
    wg.Wait()
    
    // 每个 session 单独看，平均命中率应 ≥ 95%
    for idx, ratios := range results {
        avg := average(ratios)
        require.GreaterOrEqual(t, avg, 0.85, 
            "session %d 平均命中率 %.2f%% 低于 85%%（受并发影响）", idx, avg*100)
    }
}
```

#### 7.3.6 E2E 测试 5：加工具 vs 不加工具
```go
// 中文注释：5/10/20 个工具场景下，命中率应不下降
//   - 验证 tool schema 序列化定序 + 工具名排序
func TestE2E_ToolCount_HitRateStable(t *testing.T) {
    for _, toolCount := range []int{5, 10, 20} {
        mock := testsutil.NewMockDeepSeekServer()
        defer mock.Close()
        
        tools := generateTools(toolCount)  // 工具 name 字典序排列
        agent := dsh.NewAgent(dsh.Config{Tools: tools})
        
        var hitRatios []float64
        for i := 0; i < 20; i++ {
            result, _ := agent.Run(ctx, fmt.Sprintf("msg %d", i))
            hitRatios = append(hitRatios, result.CacheStats.HitRatio)
        }
        
        avg := average(hitRatios)
        require.GreaterOrEqual(t, avg, 0.95,
            "%d 工具场景命中率 %.2f%% 低于 95%%", toolCount, avg*100)
    }
}
```

### 7.4 验收标准
- ✅ 所有 5 个 E2E 测试通过
- ✅ 50 轮稳定场景命中率 ≥ 95%
- ✅ 切 preset 后 30 轮内恢复
- ✅ Compaction 后 30 轮内恢复
- ✅ 10 session 并发各 session ≥ 85%
- ✅ 加工具场景命中率不降

### 7.5 与现有任务的关联
- 关联 **M02-M42 全部 MUST 任务**：E2E 是所有 MUST 完成的最终关卡
- 关联 **S05 OpenTelemetry**：E2E 测试应同时上报指标

---

## 8. 阶段 7 · 监控 + 看板（1 周）

### 8.1 目标
提供生产级缓存命中率实时监控 + 破窗告警，确保 dsh-go 上线后持续保持 97%+ 命中率。

### 8.2 涉及包
- `internal/telemetry/cache_metrics.go`（S05）
- `pkg/llm/tokenmeter.go`（M34 增强）
- `cmd/dsh/main.go`（可选：CLI dashboard）

### 8.3 子任务

#### 8.3.1 OTel 探针导出
```go
// 文件：internal/telemetry/cache_metrics.go
// 中文注释：每次 LLM 请求后导出 OTel 指标
//   - dsh.cache.hit_ratio: Histogram（按 session/preset/turn 维度）
//   - dsh.cache.hit_tokens: Counter
//   - dsh.cache.miss_tokens: Counter
//   - dsh.cache.broken_count: Counter（检测到破缓存时 +1）
//   - 标签：session.id, agent.preset, model, turn.seq
import (
    "go.opentelemetry.io/otel/metric"
)

var (
    cacheHitRatio  metric.Float64Histogram
    cacheHitTokens metric.Int64Counter
    cacheMissTokens metric.Int64Counter
    cacheBrokenCount metric.Int64Counter
)

func init() {
    var err error
    cacheHitRatio, err = otel.Meter.Float64Histogram("dsh.cache.hit_ratio",
        metric.WithDescription("每次 LLM 请求的 prefix cache 命中率"),
    )
    // ... 初始化其他指标
}

func RecordCacheStats(stats CacheStats) {
    attrs := []attribute.KeyValue{
        attribute.String("session.id", stats.SessionID.String()),
        attribute.String("model", stats.Model),
    }
    cacheHitRatio.Record(context.Background(), stats.HitRatio, attrs...)
    cacheHitTokens.Add(context.Background(), int64(stats.PromptCacheHit), attrs...)
    cacheMissTokens.Add(context.Background(), int64(stats.PromptCacheMiss), attrs...)
}
```

#### 8.3.2 破窗告警
```go
// 文件：internal/telemetry/cache_alert.go
// 中文注释：检测"破缓存"事件并告警
//   - 单次命中率突降 > 30% → 触发 warn 日志
//   - 连续 5 次命中率 < 50% → 触发 error 日志 + 可选 webhook
//   - 检测到"已知破缓存模式"（如 schema 字段顺序变化）→ 直接 error
type CacheAlert struct {
    threshold       float64  // 默认 0.5
    consecutiveFails int     // 默认 5
}

func (a *CacheAlert) Check(stats CacheStats, history []CacheStats) {
    if stats.HitRatio < a.threshold {
        a.consecutiveFails++
        if a.consecutiveFails >= 5 {
            log.Error("⚠️ cache hit rate continuously low",
                "session", stats.SessionID,
                "current", stats.HitRatio,
                "consecutive_fails", a.consecutiveFails,
            )
        }
    } else {
        a.consecutiveFails = 0
    }
}
```

#### 8.3.3 Grafana 看板
```yaml
# 文件：deploy/grafana/dsh-cache-dashboard.json
# 中文注释：Grafana 看板 JSON 定义
#   - 主面板：近 24h 平均命中率（按 preset 拆分）
#   - 副面板：命中率分布直方图
#   - 告警面板：近 1h 破窗事件
{
  "panels": [
    {
      "title": "Cache Hit Rate (24h)",
      "targets": [{
        "expr": "histogram_quantile(0.5, dsh_cache_hit_ratio)"
      }]
    },
    {
      "title": "Cache Hit Rate by Preset",
      "targets": [{
        "expr": "avg by (preset) (dsh_cache_hit_ratio)"
      }]
    },
    {
      "title": "Broken Cache Events",
      "targets": [{
        "expr": "rate(dsh_cache_broken_count[5m])"
      }]
    }
  ]
}
```

### 8.4 验收标准
- ✅ 集成测试：Mock OTel collector 验证指标正确导出
- ✅ 告警测试：人为注入连续低命中率 → 告警正确触发
- ✅ Grafana 看板 JSON 通过 schema 校验

### 8.5 与现有任务的关联
- 主体：**S05 OpenTelemetry**（SHOULD）
- 配套：**S07 Session Telemetry**（SHOULD）
- 配套：**M34 Token Meter**（MUST）

---

## 9. 关键 Go 代码架构总览

### 9.1 缓存命中率数据流

```text
┌─────────────────┐
│ Agent.Run()     │  ← 业务调用入口
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ AgentLoop       │  ← pkg/agentloop/step.go
│ (pre-step)      │
└────────┬────────┘
         │ 调 waterfall
         ▼
┌─────────────────────────────────────────────┐
│ Assembler.Assemble(ctx, session)             │  ← pkg/sysprompt/assembler.go
│   1. Render PromptSection 列表（按 order）  │  ← 静态
│   2. Render PromptContext 列表（change-only）│  ← 动态 → user-msg
│   3. Tools List() 按 name 字典序            │  ← 稳定
│   4. Skills CatalogText() 按 name 字典序    │  ← 稳定 + change-only inject
└────────┬────────────────────────────────────┘
         │ 返回 []llm.Message
         ▼
┌─────────────────┐
│ LLMService.Call │  ← pkg/llm/service.go
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ DeepSeek Client │  ← pkg/llm/provider/deepseek/client.go
│ (SSE 请求)      │
└────────┬────────┘
         │ HTTP POST
         ▼
┌─────────────────┐
│ DeepSeek API    │  ← 服务端自动 prefix cache
└────────┬────────┘
         │ SSE 响应（含 prompt_cache_hit_tokens）
         ▼
┌─────────────────────────────────────────────┐
│ TokenMeter.Measure()                         │  ← pkg/llm/tokenmeter.go
│   - 解析 prompt_cache_hit_tokens/miss_tokens │
│   - 构造 CacheStats                          │
│   - OTel RecordCacheStats()                  │
│   - 写入 session telemetry ledger            │
└─────────────────────────────────────────────┘
```

### 9.2 4 项纪律 → 代码位置映射

| 纪律 | 代码位置 | 验证手段 |
|---|---|---|
| **D1** 严格 append-only | `pkg/session/session.go` `Append()` | 编译期：除 `Append` 外无写方法 |
| **D1** SurfaceOp | `pkg/session/event_data.go` | 单元测试：compaction 后 events 仍连续 |
| **D1** 不变量 | `pkg/session/invariant.go` | 单元测试：人为破坏 seq/time 触发 |
| **D2** 模板只拷原版 | `pkg/sysprompt/sections/*.go` | diff + string equality test |
| **D2** order 写死 | `pkg/sysprompt/section.go` | go test 验证 Order() 读 const |
| **D2** 禁插动态 | `pkg/sysprompt/static_check.go` + lint | CI 必跑 |
| **D3** catalog 排序 | `pkg/skill/registry.go` | 1000 次调用字节相同测试 |
| **D3** change-only inject | `pkg/skill/tool.go` | fsnotify mock test |
| **D4** PromptContext | `pkg/sysprompt/context.go` | 单元测试 hash 稳定性 |
| **D4** Goal Round Context | `pkg/goal/round_driver.go` | 单元测试 active/complete 切换 |
| **D4** Compaction 保留 | `pkg/compaction/engine.go` | 集成测试：compaction 后 context 仍在 |

---

## 10. 验收 Checklist（实现完成判定）

### 10.1 静态检查
- [ ] `make check-cache-safety` 通过
- [ ] `make lint` 通过
- [ ] `go vet ./...` 通过
- [ ] 静态扫描 0 violations

### 10.2 单元测试
- [ ] `go test ./pkg/llm/...` 通过（含 CacheStats 测试）
- [ ] `go test ./pkg/session/...` 通过（含 fold 纯函数测试 + 不变量测试）
- [ ] `go test ./pkg/sysprompt/...` 通过（含 section 纯函数测试）
- [ ] `go test ./pkg/skill/...` 通过（含 catalog 排序测试）
- [ ] `go test ./pkg/tools/...` 通过（含 schema 序列化测试）
- [ ] `go test ./pkg/compaction/...` 通过（含 context 保留测试）
- [ ] 测试覆盖率 ≥ 80%

### 10.3 集成测试
- [ ] `go test ./tests/cache_hit_rate_e2e_test.go` 全部通过
- [ ] 5 个 E2E 用例全过

### 10.4 性能指标
- [ ] 50 轮平均命中率 ≥ 95%
- [ ] 切 preset 后 30 轮内恢复 ≥ 80%
- [ ] Compaction 后 30 轮内恢复 ≥ 95%
- [ ] 10 session 并发各 ≥ 85%
- [ ] 5/10/20 工具场景命中率不降

### 10.5 文档同步
- [ ] README.md 第三章"3.3 缓存命中率承诺"已写明
- [ ] docs/cache_safety.md 已发布（开发者手册）
- [ ] CHANGELOG.md 已记录"对齐 dsh 官方 97-99% 命中率"
- [ ] Grafana 看板 JSON 已上传

---

## 11. 风险与回退方案

| 风险 | 概率 | 影响 | 回退方案 |
|---|---|---|---|
| **DeepSeek 服务端 cache 行为变化** | 低 | 高 | 探针埋点（阶段 0）实时发现，回退方案为"+cache_control 实验性开启"（参考 pi-opencode-go-cache）|
| **Go map 顺序随机导致 schema 区段不命中** | 中 | 中 | 阶段 5 自定义 lint + 编译期检测 |
| **fsnotify 在 Windows 上失效** | 中 | 低 | Polling fallback（已在 M27 设计）|
| **compaction 算法过度压缩** | 低 | 高 | 阶段 5 强制保留 last context snapshot |
| **多 session 互踩（不存在的风险）** | 极低 | 中 | 阶段 7 OTel 监控及时发现 |
| **DeepSeek 切换为 Anthropic-style cache_control** | 低 | 中 | `CacheStats` 抽象已为未来 cache_control 字段预留扩展位 |

---

## 12. 与现有 MUST/SHOULD 任务的对接总表

| 本计划阶段 | 关联 tasks.json 任务 | 状态 |
|---|---|---|
| 阶段 0 | M34 Token Meter 增强 | pending |
| 阶段 1 | M02 Session Event Log / M33 Invariant / M40 Projections / S01 Compaction | pending |
| 阶段 2 | M06 SysPrompt Assembler / M07 Runtime-Context / M16 Plan Mode | pending |
| 阶段 3 | M27 Skills System / M11 Agent Registry | pending |
| 阶段 4 | M42 Prompt Context（day-1 必做）| pending |
| 阶段 5 | M08 Tools Pipeline / M09 ValueSchemaSpec | pending |
| 阶段 6 | 全 MUST 任务的验收关卡 | pending |
| 阶段 7 | S05 OpenTelemetry / S07 Session Telemetry | pending |

> **执行原则**：每完成本计划的一个子任务，必须同步更新 `tasks.json` 对应任务的 `status` 字段 + `history[]` 时间序列，保证任务表与实际进度 1:1。

---

## 13. 时间线 & 资源估算

| 阶段 | 工作量 | 累计 | 2 人并行 |
|---|---|---|---|
| 阶段 0 探针 | 1 周 | 1 周 | 0.5 周 |
| 阶段 1 D1 | 1.5 周 | 2.5 周 | 1 周 |
| 阶段 2 D2 | 1.5 周 | 4 周 | 1.5 周 |
| 阶段 3 D3 | 1 周 | 5 周 | 1.5 周 |
| 阶段 4 D4 | 1.5 周 | 6.5 周 | 2.5 周 |
| 阶段 5 反模式 | 1 周 | 7.5 周 | 3 周 |
| 阶段 6 E2E | 1.5 周 | 9 周 | 3.5 周 |
| 阶段 7 监控 | 1 周 | 10 周 | 4 周 |
| **合计** | **10 周** | | **4 周** |

> **结论**：1 名熟练 Go 工程师约 10 周（2.5 个月），2 人并行约 4 周（1 个月）。

---

## 14. 下一步行动

按本计划推进时，**必须严格遵守**以下顺序：

1. **先做阶段 0**（探针埋点）— 没数据基线后续无法验收
2. **并行做阶段 1+2+3+4**（4 项纪律）— 互不依赖可并行
3. **再做阶段 5**（反模式防御）— 依赖 1-4 实现细节
4. **阶段 6 E2E 验收**— 全部纪律的最终关卡
5. **阶段 7 监控**— 上线前完成

每完成一个子任务 → 更新 [tasks.json](./tasks.json) + [TASKS.md](./TASKS.md) + [README.md](../README.md) 11.4 看板。

---

**（CACHE_HIT_RATE_PLAN.md · v1.0 · 实施计划文档）**
