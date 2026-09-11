---
title: "Sandbox & Controlled Execution"
description: "Where can the Agent write? Three sandbox modes, policy resolution, and fail-closed confinement"
weight: 40
---

# Sandbox & Controlled Execution

## In One Sentence

**Even after a tool call is "approved", the Sandbox decides *where* its file-system side effects are allowed to happen — read-only, workspace-only, or fully unrestricted — and it must fail closed when no backend is available.**

## The Two Safety Gates

Before an Agent touches real files or processes, it passes through two independent safety gates:

| Gate | Question | Package |
|---|---|---|
| **Approval** | *Can* this tool run at all? (allow / deny / ask) | `pkg/approval` |
| **Sandbox** | *Where* can its side effects happen? (read-only / workspace / danger) | `pkg/sandbox` |

Approval decides **whether** the call proceeds; Sandbox decides **the boundary** of what it can touch. They are orthogonal: a tool can be `allow`ed but still confined to `read-only`, or `ask`-approved once but run with `danger-full-access`.

## Three Sandbox Modes

The Sandbox only constrains **file-system side effects** — it does not govern network, CPU, or memory (those belong to the OS / container layer).

| Mode | Meaning | Typical Use |
|---|---|---|
| `read-only` | No writes anywhere; all file mutations rejected | Untrusted code, audit mode, `safe` preset |
| `workspace-write` | Writes confined to the session's `workspaceRoot` | Normal development, `custom` preset |
| `danger-full-access` | No confinement; raw `argv` executed directly | Trusted automation, `danger` preset |

```go
const (
    ModeReadOnly         SandboxMode = "read-only"
    ModeWorkspaceWrite   SandboxMode = "workspace-write"
    ModeDangerFullAccess SandboxMode = "danger-full-access"
)
```

`danger-full-access` is special: the consumer (Bash / FS) **bypasses the sandbox entirely** and spawns the original `argv`. This is why it is called "danger" — there is no safety net below it.

## Policy Resolution: Three Layers

The effective sandbox mode for a single tool call is resolved by priority (highest wins):

```
explicit override (req.Mode)  ← highest priority
        ↓
session-logged mode (last sandbox/mode event)
        ↓
deployment default (DefaultMode)  ← lowest priority
```

And the `workspaceRoot` (the boundary for `workspace-write`) resolves as:

```
session cwd (immutable working directory)
        ↓
configured FallbackRoot (for agentless / no-cwd sessions)
```

```go
ps := sandbox.NewPolicyService(sandbox.ModeReadOnly, "/fallback/root")

// 1) Session logged "workspace-write" → that wins over the default
p1 := ps.Resolve(sandbox.SandboxPolicyRequest{Session: sess})
// p1.Mode = "workspace-write", p1.WorkspaceRoot = sess.Cwd()

// 2) Explicit override → highest priority, bypasses session log
danger := sandbox.ModeDangerFullAccess
p2 := ps.Resolve(sandbox.SandboxPolicyRequest{Session: sess, Mode: &danger})
// p2.Mode = "danger-full-access", p2.IsDanger() = true

// 3) No session (agentless) → falls back to default + fallback root
p3 := ps.Resolve(sandbox.SandboxPolicyRequest{})
// p3.Mode = "read-only", p3.WorkspaceRoot = "/fallback/root"
```

The resolved result is a `SandboxExecutionPolicy` — a complete tuple of `{Mode, WorkspaceRoot, SessionID}` that both Bash and FS consumers share, so neither re-implements the priority logic.

## The Provider Seam: Confine & Fail-Closed

`SandboxProvider` is the abstraction that actually wraps a command to enforce the policy:

```go
type SandboxProvider interface {
    // Confine wraps argv so it executes under the given policy.
    // Must FAIL CLOSED: if no backend is available or wrapping fails,
    // return an error — never silently allow unconfined execution.
    Confine(argv []string, policy SandboxPolicy) (ConfinedArgv, error)
}
```

The returned `ConfinedArgv` contains:
- **`Argv`** — the wrapped command (runner + profile + separator + original argv)
- **`Enforcement`** — `full` or `partial` (how completely the backend covers the policy)
- **`DenialSignatures`** — stderr substrings that indicate the sandbox rejected a command
- **`RunnerFailureRules`** — structured rules for distinguishing runner failures from command failures

### Fail-Closed is Mandatory

When no sandbox backend is configured (e.g., a bare Go process without `landlock` / `bwrap`), the default provider is `UnavailableProvider`:

```go
type UnavailableProvider struct{}

func (*UnavailableProvider) Confine([]string, SandboxPolicy) (ConfinedArgv, error) {
    return ConfinedArgv{}, &SandboxUnavailableError{
        Msg: "no usable sandbox backend",
    }
}
```

**It always returns `SANDBOX_UNAVAILABLE`.** This is intentional: silently running a "confined" command without an actual backend would be worse than no sandbox at all, because the caller *believes* it is safe.

> **Design principle**: A sandbox that cannot guarantee confinement must refuse to run. Fail-closed > fail-open.

## Implementation Deep Dive

### How `Confine()` Actually Wraps a Command

The `Confine()` method does not "magically" restrict a process — it **rewrites the command line** so that the original command runs *inside* a platform-specific sandbox runner. The resulting `Argv` has a fixed four-part structure:

```
[runner] [profile args...] [--] [original argv...]
   │          │                │          │
   │          │                │          └─ the user's command, untouched
   │          │                └─ separator: everything after this is the command
   │          └─ sandbox-specific flags (mounts, grants, rules)
   └─ the sandbox runner binary (bwrap, landlock-run, sandbox-exec, etc.)
```

For example, with **bwrap (Bubblewrap)** on Linux, a `workspace-write` policy transforms:

```bash
# original command
["bash", "-c", "echo hello > /workspace/app/out.txt"]

# after Confine() with bwrap
["bwrap",
 "--ro-bind", "/", "/",           # mount root as read-only
 "--dev", "/dev",                  # provide /dev
 "--unshare-pid",                  # isolate PID namespace
 "--proc", "/proc",                # mount /proc
 "--die-with-parent",              # kill sandbox if parent dies
 "--tmpfs", "/tmp",                # /tmp is writable (ephemeral)
 "--bind", "/workspace/app", "/workspace/app",  # workspace is read-write
 "--",                              # separator
 "bash", "-c", "echo hello > /workspace/app/out.txt"]
```

With **landlock** (Linux kernel security module), the same policy uses an allow-list approach:

```bash
["landlock-run",
 "--read-only", "/",               # everything readable
 "--read-write", "/dev/null",      # /dev/null writable (required)
 "--read-write", "/tmp",           # /tmp writable
 "--read-write", "/workspace/app", # workspace writable
 "--",
 "bash", "-c", "..."]
```

With **seatbelt (`sandbox-exec`)** on macOS, the profile is an SBPL (Sandbox Profile Language) script passed via `-p`:

```bash
["sandbox-exec",
 "-p",
 "(version 1) (allow default) (deny file-write*) (allow file-write* (literal \"/dev/null\")) (allow file-write* (subpath \"/workspace/app\"))",
 "--",
 "bash", "-c", "..."]
```

On **Windows**, the `sandbox-windows-acl` backend takes a different approach: it creates a restricted access token via Win32 FFI, grants ACL entries only on the workspace root, and spawns the process with that token — no command-line rewriting needed.

### The `ConfinedArgv` Structure, Field by Field

```go
type ConfinedArgv struct {
    // Argv is the wrapped command: runner + profile args + separator + original argv.
    Argv []string `json:"argv"`

    // Enforcement reports how completely the backend covers the policy promise.
    //   "full"    — the backend enforces every file-effect the policy claims
    //   "partial" — active backend or old kernel ABI only covers a subset;
    //               consumers that require absolute boundaries must NOT treat as full
    Enforcement SandboxEnforcement `json:"enforcement"`

    // DenialSignatures are stderr substrings that mean "the sandbox said no".
    // When the wrapped command fails, the consumer scans stderr for these
    // to distinguish "sandbox denied the write" from "the command itself failed".
    DenialSignatures []string `json:"denialSignatures"`

    // RunnerFailureRules are structured rules for distinguishing "the sandbox
    // runner itself failed to start" from "the command ran and exited non-zero".
    // This matters: a runner failure is an infrastructure problem (retry / alert),
    // while a command failure is a normal business outcome.
    RunnerFailureRules []RunnerFailureRule `json:"runnerFailureRules"`
}
```

The model-facing denial marker is standardized across both Bash and FS families so the LLM recognizes a policy rejection identically:

```
[sandbox: file access denied under read-only mode]
```

### How `RunnerFailureRule.Matches()` Works

A `RunnerFailureRule` applies a three-stage filter to determine whether a non-zero exit was caused by the sandbox runner itself:

```go
type RunnerFailureRule struct {
    AllowedExitCodes   []int    // non-zero exit codes that *could* be runner failures
    FatalSignatures    []string // stderr substrings that confirm a runner failure
    InformationalLines []string // full stderr lines to ignore before matching (e.g. fallback warnings)
}

func (r RunnerFailureRule) Matches(exitCode int, stderr string) (string, bool)
```

**Algorithm** (applied in order):

1. **Exit-code gate**: if `exitCode == 0`, return `false` (success). If `AllowedExitCodes` is non-empty and `exitCode` is not in it, return `false` (not a runner-failure code).
2. **Remove informational lines**: drop any full stderr line (case-insensitive, trimmed) that matches `InformationalLines`. This prevents benign warnings like `"landlock: fallback to baseline"` from masking a real failure.
3. **Fatal signature match**: scan the remaining stderr lines (case-insensitive) for any substring in `FatalSignatures`. If found, return `(matchedSignature, true)`.

```go
rule := RunnerFailureRule{
    AllowedExitCodes:  []int{125},
    FatalSignatures:   []string{"cannot start sandbox runner", "exec format error"},
    InformationalLines: []string{"landlock: fallback to baseline"},
}

// exitCode=125, stderr has a fallback warning + a fatal line
stderr := "landlock: fallback to baseline\ncannot start sandbox runner: permission denied\n"
sig, ok := rule.Matches(125, stderr)
// ok = true, sig = "cannot start sandbox runner"
// (the informational line was removed before matching)
```

### `SandboxExecutionPolicy` vs `SandboxPolicy`

There are two policy types, and understanding the distinction prevents mistakes:

| Type | Contains | Used by |
|---|---|---|
| `SandboxExecutionPolicy` | `Mode` (any of 3 modes) + `WorkspaceRoot` + `SessionID` | The **resolution layer** — `PolicyService.Resolve()` returns this; it may be `danger-full-access` |
| `SandboxPolicy` | embeds `SandboxExecutionPolicy` but overrides `Mode` to `ConfinedSandboxMode` (read-only / workspace-write only) | The **enforcement layer** — `Provider.Confine()` accepts this; it can **never** be danger |

The conversion is one-way and fail-closed:

```go
// SandboxExecutionPolicy → SandboxPolicy
func (p SandboxExecutionPolicy) ToConfined() (SandboxPolicy, error) {
    switch p.Mode {
    case ModeReadOnly:
        return SandboxPolicy{SandboxExecutionPolicy: p, Mode: ConfinedReadOnly}, nil
    case ModeWorkspaceWrite:
        return SandboxPolicy{SandboxExecutionPolicy: p, Mode: ConfinedWorkspaceWrite}, nil
    default:
        // danger-full-access CANNOT be confined — caller must bypass the sandbox
        return SandboxPolicy{}, fmt.Errorf("sandbox: cannot confine danger-full-access policy")
    }
}
```

This is why the consumer pattern checks `IsDanger()` **before** calling `ToConfined()`: a danger policy must bypass the sandbox entirely, and attempting to confine it is a programming error.

### Escalation: Requesting a Wider Mode at Runtime

A tool call running under `read-only` may discover it needs to write a file. Rather than failing, it can **request escalation** to a wider mode — but only through a strictly-ordered, fail-closed approval flow.

The allowed escalation ladder is **strictly wider** (you can only go up, never sideways or down):

```
read-only  ──►  workspace-write  ──►  danger-full-access
     │                  │
     └──────────────────┘
       (read-only can also jump directly to danger)
```

Escalation requires **two paired arguments**: `sandbox_permissions` (the target mode) and `justification` (a non-empty sentence explaining why). Either one alone is a validation error — an approval request without a reason, or a reason driving nothing, is malformed.

The escalation flow is:

1. Tool call is denied under current mode (e.g., `read-only` rejects a write)
2. Denial response carries an escalation hint: the model can retry with `sandbox_permissions` + `justification`
3. The escalation request goes through the **approval channel** (the same `ask` mechanism as `ask-dangerous`)
4. If approved, the *single call* runs with the wider mode (`allowed-once` — not a permanent session change)
5. If denied, the call fails with the standard denial marker

This keeps the safety model intact: wider access is always explicit, justified, and auditable — never silently granted.

### Shared `writableRoots`: Why Sandbox and FS Never Drift

A subtle but critical design point: the set of writable roots is computed by a **single shared helper** (`writableRoots(policy)`), used by both:

- The **sandbox backend** (to decide which directories to mount as read-write)
- The **in-process FS fence** (`pkg/fs` observation policy, to decide which writes to allow before even spawning a process)

Because both read from the same canonical, deduplicated root list, they can **never disagree**: a directory that the sandbox allows is also allowed by the FS fence, and vice versa. This eliminates a class of bugs where "the FS said it was OK but the sandbox denied it" (or the reverse).

#### `canonicalPath`: Why Symlink Resolution Matters

Before comparing paths, `writableRoots` calls `canonicalPath(path)` which uses `filepath.EvalSymlinks` to resolve symbolic links. This is essential because:

- On **macOS**, `/tmp` is actually a symlink to `/private/tmp`
- On **Linux**, `/var/run` may symlink to `/run`
- Workspace roots may themselves be symlinks (e.g., `~/project` → `/home/user/project`)

If the sandbox granted write access to `/tmp` but the FS fence checked against `/private/tmp`, they would disagree. `canonicalPath` ensures both sides compare the same resolved path.

**Conservative fallback**: if `EvalSymlinks` fails (path doesn't exist yet, or a prefix is unreadable), `canonicalPath` returns the original cleaned path rather than inventing a fallback. A non-existent root matches nothing until it exists — that's the safe outcome.

```go
func CanonicalPath(path string) string {
    resolved, err := filepath.EvalSymlinks(path)
    if err != nil {
        return filepath.Clean(path) // conservative: return original
    }
    abs, _ := filepath.Abs(resolved)
    return filepath.Clean(abs)
}
```

### `sandbox/mode` Events: Session-Level Overrides

The sandbox mode can be changed at runtime via a **log-only event** (`sandbox/mode`). This is not a configuration store — it's an event in the session log, which means:

- It's **durable**: survives session reload (replay from event log)
- It's **auditable**: every mode change is timestamped in the event history
- It's **last-write-wins**: the most recent `sandbox/mode` event determines the effective mode
- It's **session-scoped**: doesn't affect other sessions or the deployment default

```go
type SandboxModeData struct {
    Mode   string // "read-only" | "workspace-write" | "danger-full-access"
    Source string // "user" | "delegation" | "escalation"
}
```

The fold function `EffectiveSandboxMode(events)` scans from the end and returns the last `sandbox/mode` event's mode:

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

**Delegation marker**: when a sub-agent sets the mode, `Source = "delegation"` records that this change came from a delegated agent, not the user directly. This helps audit who changed the safety level.

### Backend Detection Chain: How `LocalProvider` Chooses the Right Backend

`LocalProvider` implements `SandboxProvider` and automatically selects the strongest available backend on the current platform. The detection chain is **platform-specific** and **cached** (runs once per process):

```
Linux:   bwrap ──► landlock ──► unavailable
macOS:   seatbelt ──► unavailable
Windows: windows-acl (always available via restricted tokens)
```

**Detection method**: for each candidate backend, `LocalProvider` runs a minimal probe — a `read-only` profile wrapping the `true` command. If the probe exits successfully, the backend is usable.

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

**Caching**: `sync.Once` ensures detection runs exactly once. Subsequent calls to `Backend()`, `Runner()`, or `Confine()` return the cached result.

**Config override**: `LocalConfig` allows forcing a specific backend + runner command, bypassing detection entirely (useful for testing or custom runners):

```go
provider := sandbox.NewLocalProvider(&sandbox.LocalConfig{
    RunnerCommand: "custom-bwrap-wrapper",
    BackendType:   sandbox.BackendBwrap,
})
```

**Backend-specific denial signatures**: each backend returns its own `DenialSignatures` so the consumer can correctly identify "the sandbox said no":

| Backend | Denial Signatures |
|---|---|
| `bwrap` | `"Read-only file system"`, `"EROFS"` |
| `landlock` | `"Permission denied"`, `"EACCES"` |
| `seatbelt` | `"Operation not permitted"`, `"EPERM"` |
| `windows-acl` | `"Access is denied"`, `"ERROR_ACCESS_DENIED"` |

### Windows ACL Backend: Restricted Tokens + Directory ACLs

On Windows, there's no equivalent of `bwrap` or `seatbelt` — no system-level sandbox that rewrites the command line. Instead, the `windows-acl` backend uses two Win32 mechanisms:

1. **Restricted access token** (`CreateRestrictedToken` / `DuplicateTokenEx`): creates a copy of the current process token with admin groups removed and privileges disabled
2. **Directory ACLs** (`SetNamedSecurityInfo` / `SetEntriesInAcl`): grants the current user's SID read-write access only on the workspace root and per-session temp directory

The process is then spawned with `CreateProcessAsUser` using the restricted token. Because the token lacks admin privileges and other directories retain their default ACLs, the process is effectively confined.

```go
// 1. Create restricted token (DuplicateTokenEx)
var dupToken windows.Token
windows.DuplicateTokenEx(procToken, windows.TOKEN_ALL_ACCESS,
    nil, windows.SecurityImpersonation, windows.TokenPrimary, &dupToken)

// 2. Grant ACL on workspace root
// (SetEntriesInAcl + SetNamedSecurityInfo with FILE_ALL_ACCESS)

// 3. Spawn with restricted token
windows.CreateProcessAsUser(dupToken, nil, cmdLine,
    nil, nil, false, flags, envBlock, workDirPtr, &si, &pi)
```

**Graceful degradation**: if `DuplicateTokenEx` fails (e.g., the process doesn't have `SeCreateTokenPrivilege`), the backend doesn't fail — it falls back to spawning with the current process token and reports `Enforcement = "partial"`. This ensures `Confine()` never returns an error on Windows, while still signaling that full confinement wasn't achieved.

**Per-session temp directory**: `SetupTempDir()` creates a unique temp directory per provider instance, sets ACLs on it, and returns its path. This is passed to the child process as `%TEMP%` / `%TMP%`, ensuring each sandboxed process has its own isolated scratch space.

**Cleanup**: `Cleanup()` closes the token handle and removes the temp directory. Always defer this after use.

### Invariant Checks: Development-Mode Safety Nets

The sandbox package includes **invariant checks** that validate internal consistency during development. These are controlled by a global switch:

```go
sandbox.InvariantEnabled = true  // development (default)
sandbox.InvariantEnabled = false // production (skip checks for performance)
```

When enabled, the following invariants are checked:

| Check | What it validates |
|---|---|
| `AssertValidMode` | Mode is one of the three valid values |
| `AssertExecutionPolicy` | `workspace-write` has non-empty `WorkspaceRoot` |
| `AssertConfinedPolicy` | Confined mode is read-only or workspace-write (never danger) |
| `AssertNotDangerBeforeConfine` | `ToConfined()` is never called on a danger policy |
| `AssertEscalationTarget` | Escalation target is in the closed vocabulary |
| `AssertConfinedArgv` | Confined argv is non-empty with valid enforcement |
| `AssertWritableRoots` | workspace-write has non-empty roots; other modes have empty |

On failure, invariants **panic** with a package-qualified message: `[sandbox invariant] invalid sandbox mode "..."`. This catches programming errors early in development, while production builds can disable the checks to avoid any overhead.

### Escalation Deep Dive: `ApproveEscalation` Flow

The escalation mechanism is implemented in `escalation.go` with a strictly-ordered, fail-closed flow:

```go
func ApproveEscalation(ctx context.Context, req EscalationRequest, approver EscalationApprover) (SandboxMode, error)
```

**Step 1 — Strictly-wider check** (`CanEscalateTo`): the requested target must be strictly wider than the current effective mode. Non-wider requests are rejected **before asking the user** — there's no point prompting for approval to stay at the same level or go down.

```go
var widerModes = map[SandboxMode][]SandboxMode{
    ModeReadOnly:       {ModeWorkspaceWrite, ModeDangerFullAccess},
    ModeWorkspaceWrite: {ModeDangerFullAccess},
    // danger-full-access: nothing (it's the top)
}
```

**Step 2 — Argument validation** (`ValidateEscalationArgs`): `sandbox_permissions` and `justification` must both be present or both absent. A justification without a target, or a target without a reason, is a validation error.

**Step 3 — Approval channel** (`EscalationApprover`): the request goes through the same `ask` mechanism as `ask-dangerous`. The approver interface is:

```go
type EscalationApprover interface {
    Request(ctx context.Context, req EscalationApprovalRequest) (EscalationOutcome, error)
}
```

**Step 4 — Outcome mapping**: the approval result is mapped to either the granted mode or a specific error:

| Outcome | Result |
|---|---|
| `allowed-once` | Returns the target mode (single call only) |
| `rejected` | `ErrEscalationRejected` |
| `cancelled` | `ErrEscalationCancelled` |
| `unavailable` | `ErrEscalationUnavailable` |

**No approver = fail-closed**: if `approver` is `nil`, `ApproveEscalation` returns `ErrEscalationNoApprover`. Wider access is never silently granted.

**Escalation targets are a closed vocabulary**: `[workspace-write, danger-full-access]`. `read-only` is the baseline and is never an escalation target — you don't "escalate to" the safest mode.

## How Bash & FS Consume the Policy

Both the Shell (`pkg/shell`) and Filesystem (`pkg/fs`) consumers follow the same pattern:

1. Call `SandboxPolicyService.Resolve()` to get the `SandboxExecutionPolicy`
2. If `IsDanger()` → spawn raw `argv` (bypass sandbox)
3. Otherwise → call `ToConfined()` to get a `SandboxPolicy`, then `provider.Confine()`
4. If `Confine` returns an error → **reject the call** (fail-closed)

```go
policy := ps.Resolve(req)
if policy.IsDanger() {
    return exec.Command(argv[0], argv[1:]...) // bypass sandbox entirely
}
confined, err := provider.Confine(argv, policy.ToConfined())
if err != nil {
    return nil, err // SANDBOX_UNAVAILABLE → fail closed
}
return exec.Command(confined.Argv[0], confined.Argv[1:]...)
```

## Permission Presets: Sandbox + Approval Combined

In practice, sandbox mode and approval policy are selected together via **permission presets** (`pkg/presets`):

| Preset | Sandbox Mode | Approval Policy | When to Use |
|---|---|---|---|
| `safe` | `read-only` | `ask-dangerous` | Untrusted sessions, audit |
| `danger` | `danger-full-access` | `allow-all` | Fully trusted automation |
| `review` | `read-only` | `ask-dangerous-tool-edit` | Read-only + every edit reviewed |
| `custom` | `workspace-write` | `ask-dangerous` | Normal dev (user can override) |

```go
preset, _ := presets.Resolve("safe")
// preset.SandboxMode    = "read-only"
// preset.ApprovalPolicy = "ask-dangerous"
```

The `custom` preset supports **derived overrides**: the user can independently override the sandbox mode or approval policy, and `DerivedState` reports whether the current configuration is custom.

## Benefits and Costs

{{< callout emoji="✅" >}}
**Benefits**: least-privilege by default, fail-closed safety, shared policy resolution (no duplicated logic), preset-based one-click security levels, audit-friendly enforcement levels (`full` / `partial`)
{{< /callout >}}

{{< callout emoji="⚠️" >}}
**Costs**: requires a real OS-level backend (landlock / bwrap / Windows ACL) for `full` enforcement; without one, confined modes refuse to run (fail-closed); `danger-full-access` is an explicit escape hatch that must be audited
{{< /callout >}}

## Source Reference

- `pkg/sandbox/sandbox.go` — three modes, policy resolution, provider seam, fail-closed
- `pkg/sandbox/session_mode.go` — `sandbox/mode` event fold + set, delegation marker
- `pkg/sandbox/roots.go` — `CanonicalPath` + `WritableRoots` shared computation
- `pkg/sandbox/escalation.go` — strictly-wider ladder, `ApproveEscalation` flow, markers
- `pkg/sandbox/invariant.go` — development-mode invariant checks
- `pkg/sandbox/profiles.go` — bwrap / landlock / seatbelt profile argument builders
- `pkg/sandbox/local.go` — `LocalProvider` multi-backend detection chain
- `pkg/sandbox/windows_acl.go` — Windows restricted token + ACL backend
- `pkg/approval/approval.go` — the other safety gate (allow / deny / ask)
- `pkg/presets/permission_presets.go` — safe / danger / review / custom presets
- Runnable example: [`examples/sandbox_approval`](https://github.com/JopenChen/dsh-go/blob/master/examples/sandbox_approval/main.go)

## Next Steps

→ [Learn Goal State Machine](../goal-state-machine/): how an Agent turns a goal into a self-driving execution loop

→ Back to [Tutorials index](../): the full three-step learning path
