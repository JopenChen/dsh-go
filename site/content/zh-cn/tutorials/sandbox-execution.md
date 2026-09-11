---
title: "沙箱与受控执行（Sandbox）"
description: "Agent 能在哪儿写？三种沙箱模式、策略解析与 fail-closed 强制约束"
weight: 40
---

# 沙箱与受控执行（Sandbox）

## 一句话

**即便一个工具调用已经被"批准"，沙箱（Sandbox）仍然决定它的文件系统副作用**只能发生在哪里**——只读、仅工作区可写、还是完全不受限——并且在没有可用后端时必须 fail-closed（拒绝执行）。**

## 两道安全闸

在 Agent 操作真实文件或进程之前，它要经过两道相互独立的安全闸：

| 安全闸 | 回答的问题 | 归属包 |
|---|---|---|
| **审批（Approval）** | 这个工具**能不能**运行？（allow / deny / ask） | `pkg/approval` |
| **沙箱（Sandbox）** | 它的副作用**在哪儿**发生？（read-only / workspace / danger） | `pkg/sandbox` |

审批决定调用**是否放行**，沙箱决定它能触碰的**边界**。两者正交：一个工具可以被 `allow` 但仍被约束在 `read-only`，也可以被 `ask` 单次放行后以 `danger-full-access` 运行。

## 三种沙箱模式

沙箱**只约束文件系统副作用**——不管理网络、CPU 或内存（那些属于操作系统 / 容器层）。

| 模式 | 含义 | 典型场景 |
|---|---|---|
| `read-only` | 任何位置都不可写，所有文件变更被拒绝 | 不可信代码、审计模式、`safe` 预设 |
| `workspace-write` | 写入被限制在会话的 `workspaceRoot` 内 | 正常开发、`custom` 预设 |
| `danger-full-access` | 无约束，直接执行原始 `argv` | 受信自动化、`danger` 预设 |

```go
const (
    ModeReadOnly         SandboxMode = "read-only"
    ModeWorkspaceWrite   SandboxMode = "workspace-write"
    ModeDangerFullAccess SandboxMode = "danger-full-access"
)
```

`danger-full-access` 比较特殊：消费者（Bash / FS）会**完全绕过沙箱**，直接 spawn 原始 `argv`。这就是它叫 "danger" 的原因——它下面没有安全网。

## 策略解析：三层优先级

单次工具调用的生效沙箱模式按优先级解析（高优先级覆盖低优先级）：

```
显式 override（req.Mode）          ← 最高优先级
        ↓
会话日志记录的模式（最近一次 sandbox/mode 事件）
        ↓
部署默认模式（DefaultMode）         ← 最低优先级
```

而 `workspaceRoot`（`workspace-write` 的边界）解析为：

```
会话 cwd（不可变工作目录）
        ↓
配置的 FallbackRoot（agentless / 无 cwd 会话的回落根）
```

```go
ps := sandbox.NewPolicyService(sandbox.ModeReadOnly, "/fallback/root")

// 1) 会话记录了 workspace-write → 覆盖默认模式
p1 := ps.Resolve(sandbox.SandboxPolicyRequest{Session: sess})
// p1.Mode = "workspace-write", p1.WorkspaceRoot = sess.Cwd()

// 2) 显式 override → 最高优先级，越过会话记录
danger := sandbox.ModeDangerFullAccess
p2 := ps.Resolve(sandbox.SandboxPolicyRequest{Session: sess, Mode: &danger})
// p2.Mode = "danger-full-access", p2.IsDanger() = true

// 3) 无会话（agentless）→ 回落部署默认 + 回落根
p3 := ps.Resolve(sandbox.SandboxPolicyRequest{})
// p3.Mode = "read-only", p3.WorkspaceRoot = "/fallback/root"
```

解析结果是一个 `SandboxExecutionPolicy`——包含 `{Mode, WorkspaceRoot, SessionID}` 的完整元组。Bash 和 FS 两个消费者共享同一份解析，避免各自重复实现优先级逻辑。

## Provider 接缝：Confine 与 Fail-Closed

`SandboxProvider` 是实际包裹命令以强制执行策略的抽象：

```go
type SandboxProvider interface {
    // Confine 把 argv 包裹为在指定策略下受约束执行的形式。
    // 必须 FAIL CLOSED：无可用后端或包裹失败时返回错误，
    // 绝不允许静默无约束放行。
    Confine(argv []string, policy SandboxPolicy) (ConfinedArgv, error)
}
```

返回的 `ConfinedArgv` 包含：
- **`Argv`** — 包裹后的命令（runner + profile + 分隔符 + 原始 argv）
- **`Enforcement`** — `full` 或 `partial`（后端对该策略的强制完整度）
- **`DenialSignatures`** — 表示沙箱拒绝了某条命令的 stderr 子串集合
- **`RunnerFailureRules`** — 区分 runner 失败与命令失败的结构化规则

### Fail-Closed 是硬性要求

当没有配置沙箱后端时（例如一个没有 `landlock` / `bwrap` 的裸 Go 进程），默认提供者是 `UnavailableProvider`：

```go
type UnavailableProvider struct{}

func (*UnavailableProvider) Confine([]string, SandboxPolicy) (ConfinedArgv, error) {
    return ConfinedArgv{}, &SandboxUnavailableError{
        Msg: "no usable sandbox backend",
    }
}
```

**它永远返回 `SANDBOX_UNAVAILABLE`。** 这是刻意设计的：在没有真实后端的情况下静默运行"被约束"的命令，比没有沙箱更糟糕——因为调用方**误以为**它是安全的。

> **设计原则**：无法保证约束的沙箱必须拒绝执行。Fail-closed > fail-open。

## 实现深度解析

### `Confine()` 如何实际包裹一条命令

`Confine()` 方法并不是"魔法般地"限制一个进程——它**重写命令行**，让原始命令在**平台特定的沙箱 runner 内部**运行。生成的 `Argv` 有固定的四段结构：

```
[runner] [profile args...] [--] [original argv...]
   │          │                │          │
   │          │                │          └─ 用户的命令，原封不动
   │          │                └─ 分隔符：之后的所有内容都是命令
   │          └─ 沙箱特定的参数（挂载、授权、规则）
   └─ 沙箱 runner 二进制（bwrap、landlock-run、sandbox-exec 等）
```

例如，在 Linux 上使用 **bwrap (Bubblewrap)**，一个 `workspace-write` 策略会把：

```bash
# 原始命令
["bash", "-c", "echo hello > /workspace/app/out.txt"]

# Confine() 之后（bwrap）
["bwrap",
 "--ro-bind", "/", "/",                    # 根目录以只读挂载
 "--dev", "/dev",                           # 提供 /dev
 "--unshare-pid",                           # 隔离 PID 命名空间
 "--proc", "/proc",                         # 挂载 /proc
 "--die-with-parent",                       # 父进程死亡时杀掉沙箱
 "--tmpfs", "/tmp",                         # /tmp 可写（临时）
 "--bind", "/workspace/app", "/workspace/app",  # 工作区可读写
 "--",                                       # 分隔符
 "bash", "-c", "echo hello > /workspace/app/out.txt"]
```

使用 **landlock**（Linux 内核安全模块）时，同一策略采用白名单方式：

```bash
["landlock-run",
 "--read-only", "/",                # 所有内容可读
 "--read-write", "/dev/null",       # /dev/null 可写（必需）
 "--read-write", "/tmp",            # /tmp 可写
 "--read-write", "/workspace/app",  # 工作区可写
 "--",
 "bash", "-c", "..."]
```

在 macOS 上使用 **seatbelt (`sandbox-exec`)** 时，profile 是通过 `-p` 传入的 SBPL（Sandbox Profile Language）脚本：

```bash
["sandbox-exec",
 "-p",
 "(version 1) (allow default) (deny file-write*) (allow file-write* (literal \"/dev/null\")) (allow file-write* (subpath \"/workspace/app\"))",
 "--",
 "bash", "-c", "..."]
```

在 **Windows** 上，`sandbox-windows-acl` 后端采用不同的方式：通过 Win32 FFI 创建受限访问令牌，仅在工作区根目录上授予 ACL 条目，然后用该令牌 spawn 进程——不需要重写命令行。

### `ConfinedArgv` 结构逐字段解析

```go
type ConfinedArgv struct {
    // Argv 是包裹后的命令：runner + profile 参数 + 分隔符 + 原始 argv。
    Argv []string `json:"argv"`

    // Enforcement 报告后端对策略承诺的覆盖完整度。
    //   "full"    — 后端强制执行策略声明的每一种文件效果
    //   "partial" — 活动后端或旧内核 ABI 只覆盖子集；
    //               要求绝对边界的消费者不得当作 full 使用
    Enforcement SandboxEnforcement `json:"enforcement"`

    // DenialSignatures 是表示"沙箱拒绝了"的 stderr 子串集合。
    // 当包裹后的命令失败时，消费者扫描 stderr 查找这些子串，
    // 以区分"沙箱拒绝了写入"和"命令本身失败了"。
    DenialSignatures []string `json:"denialSignatures"`

    // RunnerFailureRules 是结构化规则，用于区分"沙箱 runner 自身启动失败"
    // 和"命令运行后以非零码退出"。这很重要：runner 失败是基础设施问题
    // （重试 / 告警），而命令失败是正常的业务结果。
    RunnerFailureRules []RunnerFailureRule `json:"runnerFailureRules"`
}
```

面向模型的拒绝标记在 Bash 和 FS 两个家族中是标准化的，这样 LLM 能一致地识别策略拒绝：

```
[sandbox: file access denied under read-only mode]
```

### `RunnerFailureRule.Matches()` 的工作原理

`RunnerFailureRule` 应用三阶段过滤器来判断非零退出是否由沙箱 runner 自身引起：

```go
type RunnerFailureRule struct {
    AllowedExitCodes   []int    // 可能是 runner 失败的非零退出码
    FatalSignatures    []string // 确认 runner 失败的 stderr 子串
    InformationalLines []string // 匹配前忽略的完整 stderr 行（如 fallback 警告）
}

func (r RunnerFailureRule) Matches(exitCode int, stderr string) (string, bool)
```

**算法**（按顺序执行）：

1. **退出码门控**：如果 `exitCode == 0`，返回 `false`（成功）。如果 `AllowedExitCodes` 非空且 `exitCode` 不在其中，返回 `false`（不是 runner 失败码）。
2. **移除信息行**：删除任何匹配 `InformationalLines` 的完整 stderr 行（不区分大小写、去首尾空白）。这防止像 `"landlock: fallback to baseline"` 这样的良性警告掩盖真实失败。
3. **致命签名匹配**：在剩余的 stderr 行中（不区分大小写）扫描 `FatalSignatures` 中的任何子串。如果找到，返回 `(matchedSignature, true)`。

```go
rule := RunnerFailureRule{
    AllowedExitCodes:   []int{125},
    FatalSignatures:    []string{"cannot start sandbox runner", "exec format error"},
    InformationalLines: []string{"landlock: fallback to baseline"},
}

// exitCode=125，stderr 包含 fallback 警告 + 致命行
stderr := "landlock: fallback to baseline\ncannot start sandbox runner: permission denied\n"
sig, ok := rule.Matches(125, stderr)
// ok = true, sig = "cannot start sandbox runner"
// （信息行在匹配前已被移除）
```

### `SandboxExecutionPolicy` 与 `SandboxPolicy` 的区别

有两种策略类型，理解它们的区别可以避免错误：

| 类型 | 包含 | 使用者 |
|---|---|---|
| `SandboxExecutionPolicy` | `Mode`（3 种模式之一）+ `WorkspaceRoot` + `SessionID` | **解析层** — `PolicyService.Resolve()` 返回这个；可能是 `danger-full-access` |
| `SandboxPolicy` | 嵌入 `SandboxExecutionPolicy` 但将 `Mode` 覆盖为 `ConfinedSandboxMode`（仅 read-only / workspace-write） | **执行层** — `Provider.Confine()` 接受这个；**永远不可能**是 danger |

转换是单向的且 fail-closed：

```go
// SandboxExecutionPolicy → SandboxPolicy
func (p SandboxExecutionPolicy) ToConfined() (SandboxPolicy, error) {
    switch p.Mode {
    case ModeReadOnly:
        return SandboxPolicy{SandboxExecutionPolicy: p, Mode: ConfinedReadOnly}, nil
    case ModeWorkspaceWrite:
        return SandboxPolicy{SandboxExecutionPolicy: p, Mode: ConfinedWorkspaceWrite}, nil
    default:
        // danger-full-access 无法被约束 — 调用方必须绕过沙箱
        return SandboxPolicy{}, fmt.Errorf("sandbox: cannot confine danger-full-access policy")
    }
}
```

这就是为什么消费者模式在调用 `ToConfined()` **之前**先检查 `IsDanger()`：danger 策略必须完全绕过沙箱，试图约束它是编程错误。

### 升级（Escalation）：运行时请求更宽的模式

一个在 `read-only` 下运行的工具调用可能发现它需要写入文件。与其失败，它可以**请求升级**到更宽的模式——但只能通过严格有序、fail-closed 的审批流程。

允许的升级阶梯是**严格更宽**的（只能向上，不能横向或向下）：

```
read-only  ──►  workspace-write  ──►  danger-full-access
     │                  │
     └──────────────────┘
       （read-only 也可以直接跳到 danger）
```

升级需要**两个成对的参数**：`sandbox_permissions`（目标模式）和 `justification`（非空句子解释原因）。缺少任何一个都是验证错误——没有理由的审批请求，或不驱动任何事情的理由，都是格式错误。

升级流程：

1. 工具调用在当前模式下被拒绝（例如 `read-only` 拒绝写入）
2. 拒绝响应携带升级提示：模型可以用 `sandbox_permissions` + `justification` 重试
3. 升级请求通过**审批通道**（与 `ask-dangerous` 相同的 `ask` 机制）
4. 如果批准，**单次调用**以更宽模式运行（`allowed-once`——不是永久会话变更）
5. 如果拒绝，调用以标准拒绝标记失败

这保持了安全模型的完整性：更宽的访问总是显式的、有理由的、可审计的——永远不会被静默授予。

### 共享 `writableRoots`：为什么沙箱和 FS 永远不会漂移

一个微妙但关键的设计点：可写根目录集合由**单一共享辅助函数**（`writableRoots(policy)`）计算，同时被以下两者使用：

- **沙箱后端**（决定哪些目录以读写方式挂载）
- **进程内 FS 防护栏**（`pkg/fs` 观察策略，在 even spawn 进程之前决定允许哪些写入）

因为两者都从同一个规范化、去重的根列表读取，它们**永远不会不一致**：沙箱允许的目录也被 FS 防护栏允许，反之亦然。这消除了一类 bug——"FS 说可以但沙箱拒绝了"（或反过来）。

#### `canonicalPath`：为什么符号链接解析很重要

在比较路径之前，`writableRoots` 会调用 `canonicalPath(path)`，它使用 `filepath.EvalSymlinks` 解析符号链接。这是必不可少的，因为：

- 在 **macOS** 上，`/tmp` 实际上是指向 `/private/tmp` 的符号链接
- 在 **Linux** 上，`/var/run` 可能符号链接到 `/run`
- 工作区根目录本身可能是符号链接（例如 `~/project` → `/home/user/project`）

如果沙箱授予对 `/tmp` 的写访问权限，但 FS 防护栏检查的是 `/private/tmp`，它们就会不一致。`canonicalPath` 确保双方比较的是同一个解析后的路径。

**保守回退**：如果 `EvalSymlinks` 失败（路径尚不存在，或前缀不可读），`canonicalPath` 返回原始的清理后路径，而不是发明一个回退路径。不存在的根在它存在之前匹配不到任何东西——这是安全的结果。

```go
func CanonicalPath(path string) string {
    resolved, err := filepath.EvalSymlinks(path)
    if err != nil {
        return filepath.Clean(path) // 保守：返回原路径
    }
    abs, _ := filepath.Abs(resolved)
    return filepath.Clean(abs)
}
```

### `sandbox/mode` 事件：会话级覆盖

沙箱模式可以在运行时通过**仅日志事件**（`sandbox/mode`）更改。这不是配置存储——它是会话日志中的一个事件，这意味着：

- 它是**持久的**：会话重载后仍然有效（从事件日志 replay）
- 它是**可审计的**：每次模式更改都在事件历史中带有时间戳
- 它是**最后写入生效**：最近的 `sandbox/mode` 事件决定有效模式
- 它是**会话范围的**：不影响其他会话或部署默认值

```go
type SandboxModeData struct {
    Mode   string // "read-only" | "workspace-write" | "danger-full-access"
    Source string // "user" | "delegation" | "escalation"
}
```

fold 函数 `EffectiveSandboxMode(events)` 从末尾向前扫描，返回最后一个 `sandbox/mode` 事件的模式：

```go
func EffectiveSandboxMode(events []session.SessionEvent) (SandboxMode, bool) {
    for i := len(events) - 1; i >= 0; i-- {
        if events[i].Type == session.EventSandboxMode {
            data := events[i].Data.(session.SandboxModeData)
            return SandboxMode(data.Mode), true
        }
    }
    return "", false
}
```

**委派标记**：当子代理设置模式时，`Source = "delegation"` 记录此更改来自委派的代理，而不是用户直接操作。这有助于审计谁更改了安全级别。

### 后端探测链：`LocalProvider` 如何选择正确的后端

`LocalProvider` 实现了 `SandboxProvider`，并自动选择当前平台上可用的最强后端。探测链是**平台特定的**且**缓存的**（每个进程运行一次）：

```
Linux:   bwrap ──► landlock ──► unavailable
macOS:   seatbelt ──► unavailable
Windows: windows-acl（通过受限令牌始终可用）
```

**探测方法**：对于每个候选后端，`LocalProvider` 运行一个最小探测——用 `read-only` profile 包裹 `true` 命令。如果探测成功退出，后端就可用。

```go
func (p *LocalProvider) tryBwrap() bool {
    if _, err := exec.LookPath("bwrap"); err != nil {
        return false
    }
    cmd := exec.Command("bwrap",
        "--ro-bind", "/", "/", "--dev", "/dev",
        "--unshare-pid", "--proc", "/proc", "--die-with-parent",
        "--", "true")
    return cmd.Run() == nil
}
```

**缓存**：`sync.Once` 确保探测只运行一次。后续对 `Backend()`、`Runner()` 或 `Confine()` 的调用返回缓存的结果。

**配置覆盖**：`LocalConfig` 允许强制指定特定后端 + runner 命令，完全绕过探测（用于测试或自定义 runner）：

```go
provider := sandbox.NewLocalProvider(&sandbox.LocalConfig{
    RunnerCommand: "custom-bwrap-wrapper",
    BackendType:   sandbox.BackendBwrap,
})
```

**后端特定的拒绝签名**：每个后端返回自己的 `DenialSignatures`，以便消费者能正确识别"沙箱说不"：

| 后端 | 拒绝签名 |
|---|---|
| `bwrap` | `"Read-only file system"`, `"EROFS"` |
| `landlock` | `"Permission denied"`, `"EACCES"` |
| `seatbelt` | `"Operation not permitted"`, `"EPERM"` |
| `windows-acl` | `"Access is denied"`, `"ERROR_ACCESS_DENIED"` |

### Windows ACL 后端：受限令牌 + 目录 ACL

在 Windows 上，没有等同于 `bwrap` 或 `seatbelt` 的东西——没有重写命令行的系统级沙箱。相反，`windows-acl` 后端使用两种 Win32 机制：

1. **受限访问令牌**（`CreateRestrictedToken` / `DuplicateTokenEx`）：创建当前进程令牌的副本，移除管理员组并禁用特权
2. **目录 ACL**（`SetNamedSecurityInfo` / `SetEntriesInAcl`）：仅在工作区根目录和 per-session 临时目录上授予当前用户的 SID 读写权限

然后使用 `CreateProcessAsUser` 以受限令牌启动进程。因为令牌缺少管理员特权，其他目录保留其默认 ACL，进程实际上被限制了。

```go
// 1. 创建受限令牌（DuplicateTokenEx）
var dupToken windows.Token
windows.DuplicateTokenEx(procToken, windows.TOKEN_ALL_ACCESS,
    nil, windows.SecurityImpersonation, windows.TokenPrimary, &dupToken)

// 2. 在工作区根目录上授予 ACL
// （SetEntriesInAcl + SetNamedSecurityInfo with FILE_ALL_ACCESS）

// 3. 以受限令牌启动进程
windows.CreateProcessAsUser(dupToken, nil, cmdLine,
    nil, nil, false, flags, envBlock, workDirPtr, &si, &pi)
```

**优雅降级**：如果 `DuplicateTokenEx` 失败（例如进程没有 `SeCreateTokenPrivilege`），后端不会失败——它回退到使用当前进程令牌启动，并报告 `Enforcement = "partial"`。这确保 `Confine()` 在 Windows 上永远不会返回错误，同时仍然表明没有实现完全限制。

**Per-session 临时目录**：`SetupTempDir()` 为每个 provider 实例创建唯一的临时目录，在其上设置 ACL，并返回其路径。这作为 `%TEMP%` / `%TMP%` 传递给子进程，确保每个沙箱进程都有自己隔离的临时空间。

**清理**：`Cleanup()` 关闭令牌句柄并删除临时目录。使用后始终 defer 此调用。

### 不变量校验：开发模式的安全网

沙箱包包含**不变量校验**，在开发期间验证内部一致性。这些由全局开关控制：

```go
sandbox.InvariantEnabled = true  // 开发模式（默认）
sandbox.InvariantEnabled = false // 生产模式（跳过校验以提高性能）
```

启用时，会校验以下不变量：

| 校验 | 验证内容 |
|---|---|
| `AssertValidMode` | 模式是三个有效值之一 |
| `AssertExecutionPolicy` | `workspace-write` 有非空的 `WorkspaceRoot` |
| `AssertConfinedPolicy` | 受约束模式是 read-only 或 workspace-write（绝不是 danger） |
| `AssertNotDangerBeforeConfine` | `ToConfined()` 永远不会在 danger 策略上调用 |
| `AssertEscalationTarget` | 升级目标在封闭词汇表中 |
| `AssertConfinedArgv` | 受约束 argv 非空且有有效的 enforcement |
| `AssertWritableRoots` | workspace-write 有非空 roots；其他模式有空 |

失败时，不变量会 **panic** 并带有包限定的消息：`[sandbox invariant] invalid sandbox mode "..."`。这在开发早期捕获编程错误，而生产构建可以禁用以避免任何开销。

### 升级机制深度解析：`ApproveEscalation` 流程

升级机制在 `escalation.go` 中实现，具有严格有序、fail-closed 的流程：

```go
func ApproveEscalation(ctx context.Context, req EscalationRequest, approver EscalationApprover) (SandboxMode, error)
```

**步骤 1 — 严格更宽检查**（`CanEscalateTo`）：请求的目标必须严格比当前有效模式更宽。非更宽的请求在**询问用户之前**就被拒绝——没有理由提示批准保持在同一级别或下降。

```go
var widerModes = map[SandboxMode][]SandboxMode{
    ModeReadOnly:       {ModeWorkspaceWrite, ModeDangerFullAccess},
    ModeWorkspaceWrite: {ModeDangerFullAccess},
    // danger-full-access：无（它是顶点）
}
```

**步骤 2 — 参数校验**（`ValidateEscalationArgs`）：`sandbox_permissions` 和 `justification` 必须同时存在或同时不存在。没有目标的理由，或没有理由的目标，都是校验错误。

**步骤 3 — 审批通道**（`EscalationApprover`）：请求通过与 `ask-dangerous` 相同的 `ask` 机制。审批者接口是：

```go
type EscalationApprover interface {
    Request(ctx context.Context, req EscalationApprovalRequest) (EscalationOutcome, error)
}
```

**步骤 4 — 结果映射**：审批结果映射为授予的模式或特定错误：

| 结果 | 返回 |
|---|---|
| `allowed-once` | 返回目标模式（仅单次调用） |
| `rejected` | `ErrEscalationRejected` |
| `cancelled` | `ErrEscalationCancelled` |
| `unavailable` | `ErrEscalationUnavailable` |

**无审批者 = fail-closed**：如果 `approver` 为 `nil`，`ApproveEscalation` 返回 `ErrEscalationNoApprover`。更宽的访问永远不会被静默授予。

**升级目标是封闭词汇表**：`[workspace-write, danger-full-access]`。`read-only` 是基线，永远不是升级目标——你不会"升级到"最安全的模式。

## Bash 与 FS 如何消费策略

Shell（`pkg/shell`）和文件系统（`pkg/fs`）两个消费者遵循相同的模式：

1. 调用 `SandboxPolicyService.Resolve()` 获取 `SandboxExecutionPolicy`
2. 如果 `IsDanger()` → 直接 spawn 原始 `argv`（绕过沙箱）
3. 否则 → 调用 `ToConfined()` 得到 `SandboxPolicy`，再调用 `provider.Confine()`
4. 如果 `Confine` 返回错误 → **拒绝调用**（fail-closed）

```go
policy := ps.Resolve(req)
if policy.IsDanger() {
    return exec.Command(argv[0], argv[1:]...) // 完全绕过沙箱
}
confined, err := provider.Confine(argv, policy.ToConfined())
if err != nil {
    return nil, err // SANDBOX_UNAVAILABLE → fail closed
}
return exec.Command(confined.Argv[0], confined.Argv[1:]...)
```

## 终端输出清洗

PTY 的原始输出夹杂大量 ANSI 转义（颜色、光标移动、标题设置），直接回灌给模型既浪费 token 又干扰阅读。`terminal.StripANSI` 剥离 CSI/OSC 与两字节转义序列，`NormalizeText` 把 CRLF/孤立 CR 归一为 LF 并移除 BEL，得到干净的纯文本。

## 权限预设：沙箱 + 审批的组合

实际使用中，沙箱模式和审批策略通过**权限预设**（`pkg/presets`）一起选择：

| 预设 | 沙箱模式 | 审批策略 | 适用场景 |
|---|---|---|---|
| `safe` | `read-only` | `ask-dangerous` | 不可信会话、审计 |
| `danger` | `danger-full-access` | `allow-all` | 完全受信的自动化 |
| `review` | `read-only` | `ask-dangerous-tool-edit` | 只读 + 每次编辑都审核 |
| `custom` | `workspace-write` | `ask-dangerous` | 正常开发（用户可 override） |

```go
preset, _ := presets.Resolve("safe")
// preset.SandboxMode    = "read-only"
// preset.ApprovalPolicy = "ask-dangerous"
```

`custom` 预设支持**派生 override**：用户可以独立覆盖沙箱模式或审批策略，`DerivedState` 会报告当前配置是否为自定义形态。

## 好处与代价

{{< callout emoji="✅" >}}
**好处**：默认最小权限、fail-closed 安全、共享策略解析（无重复逻辑）、基于预设的一键安全等级、可审计的强制程度（`full` / `partial`）
{{< /callout >}}

{{< callout emoji="⚠️" >}}
**代价**：需要真实的操作系统级后端（landlock / bwrap / Windows ACL）才能达到 `full` 强制；没有后端时受约束模式会拒绝执行（fail-closed）；`danger-full-access` 是一个需要审计的显式逃生口
{{< /callout >}}

## 对照源码

- `pkg/sandbox/sandbox.go` —— 三种模式、策略解析、Provider 接缝、fail-closed
- `pkg/sandbox/session_mode.go` —— `sandbox/mode` 事件 fold + set、委派标记
- `pkg/sandbox/roots.go` —— `CanonicalPath` + `WritableRoots` 共享计算
- `pkg/sandbox/escalation.go` —— 严格更宽阶梯、`ApproveEscalation` 流程、标记
- `pkg/sandbox/invariant.go` —— 开发模式不变量校验
- `pkg/sandbox/profiles.go` —— bwrap / landlock / seatbelt profile 参数构建
- `pkg/sandbox/local.go` —— `LocalProvider` 多后端探测链
- `pkg/sandbox/windows_acl.go` —— Windows 受限令牌 + ACL 后端
- `pkg/approval/approval.go` —— 另一道安全闸（allow / deny / ask）
- `pkg/presets/permission_presets.go` —— safe / danger / review / custom 预设
- `pkg/terminal/sanitize.go` —— ANSI 转义剥离与文本归一化
- 可运行示例：[`examples/sandbox_approval`](https://github.com/JopenChen/dsh-go/blob/master/examples/sandbox_approval/main.go)

## 下一步

→ [学习 Goal 状态机](../goal-state-machine/)：Agent 如何把一个目标变成自驱动的执行循环

→ 返回 [教程索引](../)：完整的三步学习路径
