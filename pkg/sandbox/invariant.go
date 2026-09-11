// 本文件实现沙箱不变量校验。
//
// 对齐官方：packages/sandbox/sandbox/src/invariant.ts + sandbox-policy/src/invariant.ts + sandbox-local/src/invariant.ts
//
// 设计要点：
//   - 不变量校验在开发模式下启用，生产模式下可关闭（通过 InvariantEnabled 开关）；
//   - 校验失败时 panic 并携带包归属信息（sandbox），便于定位；
//   - 覆盖：模式合法性、policy 字段一致性、workspace-write 必须有 root、
//     danger 模式不能 ToConfined、升级目标合法性、ConfinedArgv 非空等。
package sandbox

import (
	"fmt"
	"strings"
)

// InvariantEnabled 控制不变量校验是否启用。
// 开发模式下默认启用，生产模式可设置为 false 以跳过校验。
var InvariantEnabled = true

// invariantError 是不变量校验失败的错误类型，携带包归属信息。
type invariantError struct {
	pkg     string
	message string
}

func (e *invariantError) Error() string {
	return fmt.Sprintf("[%s invariant] %s", e.pkg, e.message)
}

// assert 是不变量校验的内部函数：条件不满足时 panic。
func assert(condition bool, format string, args ...any) {
	if !InvariantEnabled {
		return
	}
	if !condition {
		panic(&invariantError{pkg: "sandbox", message: fmt.Sprintf(format, args...)})
	}
}

// AssertValidMode 校验沙箱模式是否合法。
func AssertValidMode(mode SandboxMode) {
	assert(mode == ModeReadOnly || mode == ModeWorkspaceWrite || mode == ModeDangerFullAccess,
		"invalid sandbox mode %q (expected read-only, workspace-write, or danger-full-access)", mode)
}

// AssertExecutionPolicy 校验 SandboxExecutionPolicy 的字段一致性。
func AssertExecutionPolicy(policy SandboxExecutionPolicy) {
	AssertValidMode(policy.Mode)
	// workspace-write 模式必须有非空的 workspaceRoot
	if policy.Mode == ModeWorkspaceWrite {
		assert(strings.TrimSpace(policy.WorkspaceRoot) != "",
			"workspace-write mode requires non-empty workspaceRoot")
	}
}

// AssertConfinedPolicy 校验 SandboxPolicy 的字段一致性（受约束模式）。
func AssertConfinedPolicy(policy SandboxPolicy) {
	assert(policy.Mode == ConfinedReadOnly || policy.Mode == ConfinedWorkspaceWrite,
		"confined policy mode must be read-only or workspace-write, got %q", policy.Mode)
	if policy.Mode == ConfinedWorkspaceWrite {
		assert(strings.TrimSpace(policy.WorkspaceRoot) != "",
			"workspace-write confined policy requires non-empty workspaceRoot")
	}
}

// AssertNotDangerBeforeConfine 校验在调用 ToConfined 之前模式不是 danger。
func AssertNotDangerBeforeConfine(mode SandboxMode) {
	assert(mode != ModeDangerFullAccess,
		"cannot confine danger-full-access policy (must bypass sandbox and spawn raw argv)")
}

// AssertEscalationTarget 校验升级目标是否在封闭目标词汇中。
func AssertEscalationTarget(target SandboxMode) {
	assert(target == ModeWorkspaceWrite || target == ModeDangerFullAccess,
		"escalation target %q is not in closed target vocabulary [workspace-write, danger-full-access]", target)
}

// AssertConfinedArgv 校验 Confine 返回的 ConfinedArgv 非空且合法。
func AssertConfinedArgv(result ConfinedArgv) {
	assert(len(result.Argv) > 0, "confined argv must not be empty")
	assert(result.Enforcement == EnforcementFull || result.Enforcement == EnforcementPartial,
		"invalid enforcement level %q (expected full or partial)", result.Enforcement)
}

// AssertWritableRoots 校验可写根列表：workspace-write 模式非空，其他模式为空。
func AssertWritableRoots(mode SandboxMode, roots []string) {
	if mode == ModeWorkspaceWrite {
		assert(len(roots) > 0, "workspace-write mode should have non-empty writable roots")
	} else {
		assert(len(roots) == 0, "mode %q should have empty writable roots, got %v", mode, roots)
	}
}
