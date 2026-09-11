// Package tests 的沙箱升级机制（Escalation）验收测试。
//
// 覆盖：
//   - WIDER_MODES 严格更宽阶梯表
//   - ESCALATION_TARGETS 封闭目标词汇
//   - ValidateEscalationArgs 参数成对校验
//   - SandboxDenialMarker 拒绝标记
//   - EscalationHintMarker 升级提示标记
//   - ApproveEscalation 核心流程：严格更宽检查 → 审批通道 → 结果映射
package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/sandbox"
)

// TestWiderModes 验证严格更宽阶梯表。
func TestWiderModes(t *testing.T) {
	// read-only 可以升级到 workspace-write 和 danger-full-access
	if !sandbox.CanEscalateTo(sandbox.ModeReadOnly, sandbox.ModeWorkspaceWrite) {
		t.Fatal("read-only 应可升级到 workspace-write")
	}
	if !sandbox.CanEscalateTo(sandbox.ModeReadOnly, sandbox.ModeDangerFullAccess) {
		t.Fatal("read-only 应可升级到 danger-full-access")
	}
	// workspace-write 只能升级到 danger-full-access
	if !sandbox.CanEscalateTo(sandbox.ModeWorkspaceWrite, sandbox.ModeDangerFullAccess) {
		t.Fatal("workspace-write 应可升级到 danger-full-access")
	}
	// 不能横向或向下升级
	if sandbox.CanEscalateTo(sandbox.ModeReadOnly, sandbox.ModeReadOnly) {
		t.Fatal("read-only 不应升级到自身")
	}
	if sandbox.CanEscalateTo(sandbox.ModeWorkspaceWrite, sandbox.ModeReadOnly) {
		t.Fatal("workspace-write 不应降级到 read-only")
	}
	if sandbox.CanEscalateTo(sandbox.ModeDangerFullAccess, sandbox.ModeReadOnly) {
		t.Fatal("danger 不应降级")
	}
}

// TestEscalationTargets 验证封闭目标词汇。
func TestEscalationTargets(t *testing.T) {
	targets := sandbox.EscalationTargets()
	if len(targets) != 2 {
		t.Fatalf("应有 2 个升级目标, 实际 %d", len(targets))
	}
	// read-only 是底线，不在目标中
	for _, target := range targets {
		if target == sandbox.ModeReadOnly {
			t.Fatal("read-only 不应在升级目标中（底线）")
		}
	}
}

// TestValidateEscalationArgs 验证参数成对校验。
func TestValidateEscalationArgs(t *testing.T) {
	// 两者都有 → 通过
	if err := sandbox.ValidateEscalationArgs("workspace-write", "需要写入日志文件"); err != nil {
		t.Fatalf("成对参数应通过: %v", err)
	}
	// 只有 sandbox_permissions → 报错
	if err := sandbox.ValidateEscalationArgs("workspace-write", ""); err == nil {
		t.Fatal("缺少 justification 应报错")
	}
	// 只有 justification → 报错
	if err := sandbox.ValidateEscalationArgs("", "需要写入"); err == nil {
		t.Fatal("缺少 sandbox_permissions 应报错")
	}
	// justification 为空字符串 → 报错
	if err := sandbox.ValidateEscalationArgs("workspace-write", "   "); err == nil {
		t.Fatal("空白 justification 应报错")
	}
}

// TestSandboxDenialMarker 验证拒绝标记格式。
func TestSandboxDenialMarker(t *testing.T) {
	marker := sandbox.SandboxDenialMarker(sandbox.ModeReadOnly)
	expected := "[sandbox: file access denied under read-only mode]"
	if marker != expected {
		t.Fatalf("拒绝标记格式不符\n期望: %s\n实际: %s", expected, marker)
	}
}

// TestEscalationHintMarker 验证升级提示标记。
func TestEscalationHintMarker(t *testing.T) {
	hint := sandbox.EscalationHintMarker("command")
	if hint == "" {
		t.Fatal("升级提示不应为空")
	}
	// 应包含 sandbox_permissions 关键词
	if !containsStr(hint, "sandbox_permissions") {
		t.Fatalf("升级提示应包含 sandbox_permissions, 实际: %s", hint)
	}
	// 应包含 justification 关键词
	if !containsStr(hint, "justification") {
		t.Fatalf("升级提示应包含 justification, 实际: %s", hint)
	}
}

// fakeApprover 是测试用的审批者。
type fakeApprover struct {
	outcome sandbox.EscalationOutcome
	err     error
}

func (f *fakeApprover) Request(ctx context.Context, req sandbox.EscalationApprovalRequest) (sandbox.EscalationOutcome, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.outcome, nil
}

// TestApproveEscalationAllowed 验证审批通过时返回目标模式。
func TestApproveEscalationAllowed(t *testing.T) {
	approver := &fakeApprover{outcome: sandbox.EscalationAllowedOnce}
	result, err := sandbox.ApproveEscalation(context.Background(), sandbox.EscalationRequest{
		RequestedMode: sandbox.ModeWorkspaceWrite,
		Justification: "需要写入日志",
		EffectiveMode: sandbox.ModeReadOnly,
		Subject:       "command",
		ToolName:      "bash",
		CallID:        "call-1",
	}, approver)
	if err != nil {
		t.Fatalf("审批通过不应报错: %v", err)
	}
	if result != sandbox.ModeWorkspaceWrite {
		t.Fatalf("应返回 workspace-write, 实际 %s", result)
	}
}

// TestApproveEscalationNotWider 验证非严格更宽时拒绝（不询问用户）。
func TestApproveEscalationNotWider(t *testing.T) {
	approver := &fakeApprover{outcome: sandbox.EscalationAllowedOnce}
	_, err := sandbox.ApproveEscalation(context.Background(), sandbox.EscalationRequest{
		RequestedMode: sandbox.ModeReadOnly, // 从 read-only 升级到 read-only，非更宽
		Justification: "需要写入",
		EffectiveMode: sandbox.ModeReadOnly,
		Subject:       "command",
		ToolName:      "bash",
		CallID:        "call-1",
	}, approver)
	if err == nil {
		t.Fatal("非严格更宽应报错")
	}
}

// TestApproveEscalationRejected 验证用户拒绝时返回错误。
func TestApproveEscalationRejected(t *testing.T) {
	approver := &fakeApprover{outcome: sandbox.EscalationRejected}
	_, err := sandbox.ApproveEscalation(context.Background(), sandbox.EscalationRequest{
		RequestedMode: sandbox.ModeWorkspaceWrite,
		Justification: "需要写入",
		EffectiveMode: sandbox.ModeReadOnly,
		Subject:       "command",
		ToolName:      "bash",
		CallID:        "call-1",
	}, approver)
	if err == nil {
		t.Fatal("用户拒绝应报错")
	}
	if !errors.Is(err, sandbox.ErrEscalationRejected) {
		t.Fatalf("应返回 ErrEscalationRejected, 实际: %v", err)
	}
}

// TestApproveEscalationCancelled 验证审批取消时返回错误。
func TestApproveEscalationCancelled(t *testing.T) {
	approver := &fakeApprover{outcome: sandbox.EscalationCancelled}
	_, err := sandbox.ApproveEscalation(context.Background(), sandbox.EscalationRequest{
		RequestedMode: sandbox.ModeWorkspaceWrite,
		Justification: "需要写入",
		EffectiveMode: sandbox.ModeReadOnly,
		Subject:       "command",
		ToolName:      "bash",
		CallID:        "call-1",
	}, approver)
	if err == nil {
		t.Fatal("审批取消应报错")
	}
}

// TestApproveEscalationUnavailable 验证审批通道不可用时返回错误。
func TestApproveEscalationUnavailable(t *testing.T) {
	approver := &fakeApprover{outcome: sandbox.EscalationUnavailable}
	_, err := sandbox.ApproveEscalation(context.Background(), sandbox.EscalationRequest{
		RequestedMode: sandbox.ModeWorkspaceWrite,
		Justification: "需要写入",
		EffectiveMode: sandbox.ModeReadOnly,
		Subject:       "command",
		ToolName:      "bash",
		CallID:        "call-1",
	}, approver)
	if err == nil {
		t.Fatal("审批不可用应报错")
	}
}

// TestApproveEscalationNoApprover 验证无审批者时 fail-closed。
func TestApproveEscalationNoApprover(t *testing.T) {
	_, err := sandbox.ApproveEscalation(context.Background(), sandbox.EscalationRequest{
		RequestedMode: sandbox.ModeWorkspaceWrite,
		Justification: "需要写入",
		EffectiveMode: sandbox.ModeReadOnly,
		Subject:       "command",
		ToolName:      "bash",
		CallID:        "call-1",
	}, nil)
	if err == nil {
		t.Fatal("无审批者应 fail-closed")
	}
}
