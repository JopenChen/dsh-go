// Package tests 的沙箱不变量校验验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/sandbox"
)

// TestInvariantValidMode 验证合法模式不 panic。
func TestInvariantValidMode(t *testing.T) {
	sandbox.InvariantEnabled = true
	defer func() { sandbox.InvariantEnabled = true }()
	// 不应 panic
	sandbox.AssertValidMode(sandbox.ModeReadOnly)
	sandbox.AssertValidMode(sandbox.ModeWorkspaceWrite)
	sandbox.AssertValidMode(sandbox.ModeDangerFullAccess)
}

// TestInvariantInvalidMode 验证非法模式 panic。
func TestInvariantInvalidMode(t *testing.T) {
	sandbox.InvariantEnabled = true
	defer func() { sandbox.InvariantEnabled = true }()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("非法模式应 panic")
		}
	}()
	sandbox.AssertValidMode(sandbox.SandboxMode("invalid"))
}

// TestInvariantExecutionPolicy 校验 policy 字段一致性。
func TestInvariantExecutionPolicy(t *testing.T) {
	sandbox.InvariantEnabled = true
	defer func() { sandbox.InvariantEnabled = true }()
	// workspace-write 必须有 root
	sandbox.AssertExecutionPolicy(sandbox.SandboxExecutionPolicy{
		Mode:          sandbox.ModeWorkspaceWrite,
		WorkspaceRoot: "/workspace",
	})
	// read-only 可以没有 root
	sandbox.AssertExecutionPolicy(sandbox.SandboxExecutionPolicy{
		Mode: sandbox.ModeReadOnly,
	})
}

// TestInvariantExecutionPolicyNoRoot 校验 workspace-write 无 root 时 panic。
func TestInvariantExecutionPolicyNoRoot(t *testing.T) {
	sandbox.InvariantEnabled = true
	defer func() { sandbox.InvariantEnabled = true }()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("workspace-write 无 root 应 panic")
		}
	}()
	sandbox.AssertExecutionPolicy(sandbox.SandboxExecutionPolicy{
		Mode: sandbox.ModeWorkspaceWrite,
	})
}

// TestInvariantDisabled 校验关闭不变量时不 panic。
func TestInvariantDisabled(t *testing.T) {
	sandbox.InvariantEnabled = false
	defer func() { sandbox.InvariantEnabled = true }()
	// 关闭后不应 panic
	sandbox.AssertValidMode(sandbox.SandboxMode("invalid"))
	sandbox.AssertExecutionPolicy(sandbox.SandboxExecutionPolicy{
		Mode: sandbox.ModeWorkspaceWrite,
	})
}
