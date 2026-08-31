// Package sandbox 提供进程沙箱（Process Sandbox）接缝。
//
// 对齐上游：packages/sandbox/sandbox + sandbox-policy
//
// 本文件对应任务 M26：Sandbox 接缝 3 模式 + {root, enforced} 元组。
//
// 设计要点：
//   - SandboxMode 只约束文件系统副作用：read-only / workspace-write / danger-full-access。
//   - SandboxExecutionPolicy 是「每次能力调用」都携带的完整文件效果策略：
//     即便是 danger 模式也会带上 workspaceRoot，以便消费者一次性解析再决定是否绕过。
//   - 消费者（Bash / FS）各自调用 SandboxPolicyService.Resolve()，得到对同一会话一致的策略，
//     这避免了两个消费者各自重复实现模式优先级与根回落逻辑。
//   - Provider.confine() 只对受约束（非 danger）模式生效；无可用后端必须 fail-closed，
//     绝不静默放行（SANDBOX_UNAVAILABLE）。
package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// ============================================================================
// 模式与强制程度
// ============================================================================

// SandboxMode 沙箱模式：只约束文件系统副作用。
type SandboxMode string

// 沙箱模式枚举（与 pkg/session.SandboxMode 对齐）。
const (
	ModeReadOnly        SandboxMode = "read-only"
	ModeWorkspaceWrite  SandboxMode = "workspace-write"
	ModeDangerFullAccess SandboxMode = "danger-full-access"
)

// ConfinedSandboxMode 是可交给 Provider 的受约束模式（排除 danger-full-access）。
// danger-full-access 的消费者直接使用原始 argv，不调用 ctx.sandbox。
type ConfinedSandboxMode string

// 受约束模式集合。
const (
	ConfinedReadOnly       ConfinedSandboxMode = "read-only"
	ConfinedWorkspaceWrite ConfinedSandboxMode = "workspace-write"
)

// SandboxEnforcement 是强制完成的报告事实：full 表示后端覆盖该模式承诺的全部文件效果；
// partial 表示活动后端或旧内核 ABI 只覆盖子集，要求绝对边界的消费者不得当 full 使用。
type SandboxEnforcement string

// 强制枚举。
const (
	EnforcementFull    SandboxEnforcement = "full"
	EnforcementPartial SandboxEnforcement = "partial"
)

// ============================================================================
// 执行策略
// ============================================================================

// SandboxExecutionPolicy 是一次能力调用解析出的完整文件效果策略。
// 即便在 danger 模式下也携带 workspaceRoot，供消费者一次性解析后决定执行路径。
type SandboxExecutionPolicy struct {
	// Mode 是本次执行的文件效果模式。
	Mode SandboxMode `json:"mode"`
	// WorkspaceRoot 是 workspace-write 可写作业的绝对根目录。
	WorkspaceRoot string `json:"workspaceRoot"`
	// SessionID 是调用会话的标识（无头调用时为空，后端退化为每次调用的自有状态）。
	SessionID brand.SessionID `json:"sessionId,omitempty"`
}

// IsDanger 报告该策略是否为危险全访问（需绕过沙箱直接 spawn 原始 argv）。
func (p SandboxExecutionPolicy) IsDanger() bool {
	return p.Mode == ModeDangerFullAccess
}

// SandboxPolicy 是每次受约束执行允许触及的边界，按调用携带而非固定在后端上。
type SandboxPolicy struct {
	SandboxExecutionPolicy
	// Mode 覆盖为受约束模式（read-only / workspace-write）。
	Mode ConfinedSandboxMode `json:"mode"`
}

// ============================================================================
// 策略解析服务
// ============================================================================

// Session 是策略服务所依赖的会话抽象：不可变 cwd 即 workspace 边界，
// 最后一次 logged 的沙箱模式作为会话级覆盖。
type Session interface {
	// ID 返回会话标识（branded SessionId）。
	ID() brand.SessionID
	// Cwd 返回会话不可变工作目录（workspace-write 的边界）；空串表示无 cwd。
	Cwd() string
	// SandboxMode 返回会话日志中最后一次沙箱模式覆盖；未记录时 ok=false。
	SandboxMode() (SandboxMode, bool)
}

// SandboxPolicyRequest 是选择一次能力调用的沙箱策略的输入。
type SandboxPolicyRequest struct {
	// Session 是调用会话；其不可变 cwd 成为 workspace 边界。
	Session Session
	// Mode 是显式审批/覆盖后的模式，优先级高于会话策略。
	Mode *SandboxMode
}

// SandboxPolicyService 是 ctx.sandboxPolicy 的 Go 实现。
// 它拥有默认模式优先级与根回落逻辑，Bash/FS 两个消费者复用同一份解析。
type SandboxPolicyService struct {
	// DefaultMode 是部署默认模式（agentless 时回落）。
	DefaultMode SandboxMode
	// FallbackRoot 是 agentless / 无 cwd 会话的回落工作区根。
	FallbackRoot string
}

// NewPolicyService 创建策略服务。
func NewPolicyService(defaultMode SandboxMode, fallbackRoot string) *SandboxPolicyService {
	return &SandboxPolicyService{DefaultMode: defaultMode, FallbackRoot: fallbackRoot}
}

// OverrideOf 读取会话的沙箱模式覆盖，不应用部署默认；无覆盖返回 ok=false。
func (s *SandboxPolicyService) OverrideOf(session Session) (SandboxMode, bool) {
	if session == nil {
		return "", false
	}
	return session.SandboxMode()
}

// Resolve 为一次能力调用解析完整策略。
//
// 优先级（高 → 低）：显式审批覆盖 req.Mode → 会话最后 logged 模式 → 部署默认模式。
// workspaceRoot：会话 cwd（已规范化）→ 配置的回落根（agentless）。
func (s *SandboxPolicyService) Resolve(req SandboxPolicyRequest) SandboxExecutionPolicy {
	mode := s.DefaultMode
	if req.Session != nil {
		if m, ok := req.Session.SandboxMode(); ok {
			mode = m
		}
	}
	if req.Mode != nil {
		mode = *req.Mode
	}

	// 解析 workspace 根：会话 cwd 优先，否则回落根。
	root := s.FallbackRoot
	if req.Session != nil {
		if cwd := req.Session.Cwd(); cwd != "" {
			root = cwd
		}
	}
	root = canonicalize(root)

	var sid brand.SessionID
	if req.Session != nil {
		sid = req.Session.ID()
	}
	return SandboxExecutionPolicy{
		Mode:          mode,
		WorkspaceRoot: root,
		SessionID:     sid,
	}
}

// ToConfined 把执行策略降级为受约束策略（调用方需先确认非 danger）。
func (p SandboxExecutionPolicy) ToConfined() (SandboxPolicy, error) {
	switch p.Mode {
	case ModeReadOnly:
		return SandboxPolicy{SandboxExecutionPolicy: p, Mode: ConfinedReadOnly}, nil
	case ModeWorkspaceWrite:
		return SandboxPolicy{SandboxExecutionPolicy: p, Mode: ConfinedWorkspaceWrite}, nil
	default:
		return SandboxPolicy{}, fmt.Errorf("sandbox: cannot confine danger-full-access policy %+v", p)
	}
}

// canonicalize 规范化根路径为绝对路径（文件系统语义后做词法规范化）。
func canonicalize(root string) string {
	if root == "" {
		return root
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	return filepath.Clean(abs)
}

// ============================================================================
// Provider 接缝 + 受限 argv
// ============================================================================

// RunnerFailureRule 是辨识「沙箱 runner 在执行命令前失败」的证据规则。
type RunnerFailureRule struct {
	// AllowedExitCodes 非零进程退出码白名单；省略表示允许任意非零退出码。
	AllowedExitCodes []int `json:"allowedExitCodes,omitempty"`
	// FatalSignatures 单条 stderr 行上的致命诊断子串（不区分大小写）。
	FatalSignatures []string `json:"fatalSignatures"`
	// InformationalLines 在致命匹配前被排除的无害 stderr 全行（不区分大小写精确匹配）。
	InformationalLines []string `json:"informationalLines,omitempty"`
}

// Matches 应用规则：先按退出码门控，移除 info 行，再在剩余 stderr 行内做致命子串匹配。
func (r RunnerFailureRule) Matches(exitCode int, stderr string) (string, bool) {
	if exitCode == 0 {
		return "", false
	}
	if len(r.AllowedExitCodes) > 0 {
		allowed := false
		for _, c := range r.AllowedExitCodes {
			if c == exitCode {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", false
		}
	}
	// 移除 info 行（不区分大小写精确全行）。
	info := map[string]bool{}
	for _, l := range r.InformationalLines {
		info[strings.ToLower(l)] = true
	}
	var remaining []string
	for _, line := range strings.Split(stderr, "\n") {
		if !info[strings.ToLower(strings.TrimSpace(line))] {
			remaining = append(remaining, line)
		}
	}
	// 逐行做致命子串匹配。
	joined := strings.Join(remaining, "\n")
	lower := strings.ToLower(joined)
	for _, sig := range r.FatalSignatures {
		if strings.Contains(lower, strings.ToLower(sig)) {
			return sig, true
		}
	}
	return "", false
}

// ConfinedArgv 是 Provider.confine 的产物：替换后的 argv + 该后端达到的强制程度 +
// 两个正交的 stderr 分类器（denial + runner-failure）。
type ConfinedArgv struct {
	// Argv 是包裹后的 argv（runner、profile、分隔符再到调用方 argv）。
	Argv []string `json:"argv"`
	// Enforcement 是所选后端对该策略能实现的强制完整度。
	Enforcement SandboxEnforcement `json:"enforcement"`
	// DenialSignatures 是该后端被某些命令拒绝时的 stderr dialect 子串集合。
	DenialSignatures []string `json:"denialSignatures"`
	// RunnerFailureRules 是结构化 runner 失败证据规则。
	RunnerFailureRules []RunnerFailureRule `json:"runnerFailureRules"`
}

// SandboxProvider 是抽象进程沙箱服务（ctx.sandbox）。
type SandboxProvider interface {
	// Confine 把 argv 包裹使其在 policy 下受约束执行；必须 fail-closed
	// （无可用后端或包裹失败返回错误），绝不允许静默无约束放行。
	Confine(argv []string, policy SandboxPolicy) (ConfinedArgv, error)
}

// SandboxUnavailableErrorCode 是沙箱不可用错误的稳定分类码。
const SandboxUnavailableErrorCode = "SANDBOX_UNAVAILABLE"

// SandboxUnavailableError 表示没有可用后端。
type SandboxUnavailableError struct {
	Msg string
}

func (e *SandboxUnavailableError) Error() string {
	return fmt.Sprintf("%s: %s", SandboxUnavailableErrorCode, e.Msg)
}

// UnavailableProvider 是 fail-closed 桩：任何 confine 都报 SANDBOX_UNAVAILABLE，
// 用于「无可用后端」的生产缺省。
type UnavailableProvider struct{}

// Confine 实现 fail-closed：始终不可用。
func (*UnavailableProvider) Confine([]string, SandboxPolicy) (ConfinedArgv, error) {
	return ConfinedArgv{}, &SandboxUnavailableError{Msg: "no usable sandbox backend"}
}