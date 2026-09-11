// 本文件实现沙箱升级机制（Escalation）：严格更宽阶梯 + 审批集成 + 标记。
//
// 对齐官方：packages/sandbox/sandbox/src/escalation.ts
//
// 设计要点：
//   - WIDER_MODES 严格更宽阶梯表：read-only → [workspace-write, danger-full-access]，
//     workspace-write → [danger-full-access]；danger 是顶点，不能再升级；
//   - ESCALATION_TARGETS 封闭目标词汇：[workspace-write, danger-full-access]，
//     read-only 是底线，不在目标中（任何模式都不会升级到 read-only）；
//   - 严格更宽是执行时检查（针对本次调用的 effectiveMode），不是 schema 约束
//     （schema 枚举是封闭目标词汇，effectiveMode 是每次调用的真实状态）；
//   - 升级需要 sandbox_permissions + justification 成对参数，缺一不可，
//     justification 必须是非空句子；
//   - approveEscalation 是有序 fail-closed 流程：
//     严格更宽检查 → 审批通道（approver.Request）→ 结果映射
//     （allowed-once 返回目标模式，rejected/cancelled/unavailable 抛精确错误）；
//   - 非更宽请求绝不询问用户（先检查再问人）。
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ============================================================================
// 严格更宽阶梯表
// ============================================================================

// widerModes 是严格更宽表：某个 effectiveMode 可以升级到哪些目标模式。
// 检查在执行时进行，针对本次调用的 effectiveMode。
var widerModes = map[SandboxMode][]SandboxMode{
	ModeReadOnly:       {ModeWorkspaceWrite, ModeDangerFullAccess},
	ModeWorkspaceWrite: {ModeDangerFullAccess},
}

// CanEscalateTo 判断从 effectiveMode 是否可以严格更宽升级到 targetMode。
// 严格更宽：只能向上，不能横向或向下；danger 是顶点，不能再升级。
func CanEscalateTo(effectiveMode, targetMode SandboxMode) bool {
	targets, ok := widerModes[effectiveMode]
	if !ok {
		return false // danger 或未知模式不能升级
	}
	for _, t := range targets {
		if t == targetMode {
			return true
		}
	}
	return false
}

// EscalationTargets 返回封闭的升级目标词汇：[workspace-write, danger-full-access]。
// read-only 是底线，不在目标中（任何模式都不会升级到 read-only）。
// 在挂载的能力通告升级字段时使用此词汇。
func EscalationTargets() []SandboxMode {
	return []SandboxMode{ModeWorkspaceWrite, ModeDangerFullAccess}
}

// ============================================================================
// 参数校验
// ============================================================================

// ValidateEscalationArgs 校验升级参数配对：
// sandbox_permissions 和 justification 必须同时出现或同时不出现，
// 且 justification 必须是非空句子（trim 后非空）。
//
// 没有理由的审批请求，或不驱动任何事情的理由，都是格式错误。
func ValidateEscalationArgs(sandboxPermissions, justification string) error {
	if sandboxPermissions != "" && justification == "" {
		return errors.New("invalid escalation: sandbox_permissions requires a justification")
	}
	if justification != "" && sandboxPermissions == "" {
		return errors.New("invalid escalation: justification is only valid together with sandbox_permissions")
	}
	if justification != "" && strings.TrimSpace(justification) == "" {
		return errors.New("invalid justification: expected a non-empty sentence")
	}
	return nil
}

// ============================================================================
// 标记（Markers）
// ============================================================================

// SandboxDenialMarker 返回面向模型的拒绝标记——Bash 和 FS 两个强制执行家族
// 都使用并报告这个统一词汇，这样模型无论内核拒绝了 bash 文件效果还是
// FS 防护栏拒绝了变更，都能一致地识别策略拒绝。
func SandboxDenialMarker(mode SandboxMode) string {
	return fmt.Sprintf("[sandbox: file access denied under %s mode]", mode)
}

// EscalationHintMarker 返回同轮升级提示——当组合通告了升级字段时，
// 拒绝响应携带此提示，指导模型用 sandbox_permissions（足够的最窄更宽模式）
// + justification 重试完全相同的操作。提示位于决策点，这样合规重试
// 不依赖模型回忆工具描述。
//
// subject 是被拒绝操作的家族名词（bash 用 "command"，fs 用 "operation"）。
func EscalationHintMarker(subject string) string {
	return fmt.Sprintf(
		"[sandbox: escalation available — retry this exact %s once with sandbox_permissions (the narrowest wider mode that suffices) + justification; the approval prompt asks the user]",
		subject,
	)
}

// ============================================================================
// 审批接口与结果
// ============================================================================

// EscalationOutcome 是一次升级询问的封闭结果词汇——
// 结构上与审批接缝的 ApprovalOutcome 相同。
type EscalationOutcome string

const (
	// EscalationAllowedOnce 表示用户准许了本次升级（仅本次调用，非永久）。
	EscalationAllowedOnce EscalationOutcome = "allowed-once"
	// EscalationRejected 表示用户拒绝了升级。
	EscalationRejected EscalationOutcome = "rejected"
	// EscalationCancelled 表示审批被取消。
	EscalationCancelled EscalationOutcome = "cancelled"
	// EscalationUnavailable 表示审批通道不可用。
	EscalationUnavailable EscalationOutcome = "unavailable"
)

// EscalationApprovalRequest 是升级审批请求——审计自包含（agent、工具、调用 ID、理由）。
type EscalationApprovalRequest struct {
	// ToolName 是工具名（记录在审批请求上）。
	ToolName string
	// CallID 是工具调用 ID（审批提示附着的调用）。
	CallID string
	// Reason 是审计理由（包含目标模式 + justification）。
	Reason string
}

// EscalationApprover 是升级审批者接口——工具层持有 ctx.approval，
// 本包只判断结果，不导入审批或 agent 包。
type EscalationApprover interface {
	// Request 向用户请求批准一次操作，返回封闭结果。
	Request(ctx context.Context, req EscalationApprovalRequest) (EscalationOutcome, error)
}

// EscalationRequest 是一次升级请求，由 ApproveEscalation 判断。
type EscalationRequest struct {
	// RequestedMode 是请求的目标模式（schema 固定为 EscalationTargets）。
	RequestedMode SandboxMode
	// Justification 是模型的一句话理由，原样展示给用户在审计理由中。
	Justification string
	// EffectiveMode 是调用的生效模式（会话覆盖 ?? 组合默认），请求必须严格更宽。
	EffectiveMode SandboxMode
	// Subject 是用户可见文本中被升级操作的家族名词（bash 用 "command"，fs 用 "operation"）。
	Subject string
	// ToolName 是记录在审批请求上的工具名。
	ToolName string
	// CallID 是审批提示附着的工具调用 ID。
	CallID string
}

// ============================================================================
// 错误
// ============================================================================

var (
	// ErrEscalationNotWider 表示请求的目标模式不比当前生效模式严格更宽。
	ErrEscalationNotWider = errors.New("escalation not strictly wider")
	// ErrEscalationNoApprover 表示没有审批服务（fail-closed）。
	ErrEscalationNoApprover = errors.New("escalation requires approval, but no approver is available")
	// ErrEscalationRejected 表示用户拒绝了升级。
	ErrEscalationRejected = errors.New("escalation rejected by user")
	// ErrEscalationCancelled 表示审批被取消。
	ErrEscalationCancelled = errors.New("escalation approval cancelled")
	// ErrEscalationUnavailable 表示审批通道不可用。
	ErrEscalationUnavailable = errors.New("escalation requires approval, but no approval channel is available")
)

// ============================================================================
// 核心流程：ApproveEscalation
// ============================================================================

// ApproveEscalation 在任何执行之前解析沙箱升级请求：
//  1. 针对调用的生效模式检查严格更宽（非更宽绝不询问用户）；
//  2. 校验参数配对；
//  3. 解析审批通道（无审批者 fail-closed）；
//  4. 映射每种结果——返回授予的模式（仅本次调用），或抛出每种路径的精确错误文本。
//
// 返回的授予模式仅消费于发起请求的那一次调用。
func ApproveEscalation(ctx context.Context, req EscalationRequest, approver EscalationApprover) (SandboxMode, error) {
	// 1. 严格更宽检查（执行时检查，针对本次调用的 effectiveMode）
	if !CanEscalateTo(req.EffectiveMode, req.RequestedMode) {
		return "", fmt.Errorf("%w: sandbox escalation to %q is not strictly wider than this call's current %q mode",
			ErrEscalationNotWider, req.RequestedMode, req.EffectiveMode)
	}

	// 2. 参数配对校验
	if err := ValidateEscalationArgs(string(req.RequestedMode), req.Justification); err != nil {
		return "", err
	}

	// 3. 审批通道（fail-closed）
	if approver == nil {
		return "", fmt.Errorf("%w: escalation to %q requires approval, but no approver is composed",
			ErrEscalationNoApprover, req.RequestedMode)
	}

	// 4. 调用审批者（审计自包含：理由包含目标模式 + justification）
	outcome, err := approver.Request(ctx, EscalationApprovalRequest{
		ToolName: req.ToolName,
		CallID:   req.CallID,
		Reason:   fmt.Sprintf("escalate sandbox to %s: %s", req.RequestedMode, req.Justification),
	})
	if err != nil {
		return "", fmt.Errorf("escalation approval request failed: %w", err)
	}

	// 5. 结果映射
	switch outcome {
	case EscalationAllowedOnce:
		return req.RequestedMode, nil
	case EscalationRejected:
		return "", fmt.Errorf("%w: the user rejected escalating this %s to %q",
			ErrEscalationRejected, req.Subject, req.RequestedMode)
	case EscalationCancelled:
		return "", fmt.Errorf("%w: approval for escalating to %q was cancelled",
			ErrEscalationCancelled, req.RequestedMode)
	case EscalationUnavailable:
		return "", fmt.Errorf("%w: escalation to %q requires approval, but no approval channel is available",
			ErrEscalationUnavailable, req.RequestedMode)
	default:
		return "", fmt.Errorf("unknown escalation outcome: %q", outcome)
	}
}
