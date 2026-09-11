// Package tests 的本地后端多探测链 + LocalProvider 验收测试。
package tests

import (
	"runtime"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/sandbox"
)

// TestLocalProviderBackendType 验证后端类型常量。
func TestLocalProviderBackendType(t *testing.T) {
	if string(sandbox.BackendBwrap) != "bwrap" {
		t.Fatal("BackendBwrap 应为 bwrap")
	}
	if string(sandbox.BackendLandlock) != "landlock" {
		t.Fatal("BackendLandlock 应为 landlock")
	}
	if string(sandbox.BackendSeatbelt) != "seatbelt" {
		t.Fatal("BackendSeatbelt 应为 seatbelt")
	}
	if string(sandbox.BackendWindowsACL) != "windows-acl" {
		t.Fatal("BackendWindowsACL 应为 windows-acl")
	}
}

// TestLocalProviderConfig 验证配置强制后端。
func TestLocalProviderConfig(t *testing.T) {
	p := sandbox.NewLocalProvider(&sandbox.LocalConfig{
		RunnerCommand: "custom-runner",
		BackendType:   sandbox.BackendBwrap,
	})
	if p.Backend() != sandbox.BackendBwrap {
		t.Fatalf("应强制使用 bwrap, 实际 %s", p.Backend())
	}
	if p.Runner() != "custom-runner" {
		t.Fatalf("应使用 custom-runner, 实际 %s", p.Runner())
	}
}

// TestLocalProviderAvailable 验证可用检查。
func TestLocalProviderAvailable(t *testing.T) {
	p := sandbox.NewLocalProvider(nil)
	// Windows 上 windows-acl 后端应该可用
	if runtime.GOOS == "windows" {
		if !p.Available() {
			t.Fatal("Windows 上 windows-acl 后端应可用")
		}
		if p.Backend() != sandbox.BackendWindowsACL {
			t.Fatalf("Windows 上应选择 windows-acl, 实际 %s", p.Backend())
		}
	}
}

// TestLocalProviderConfineWindowsACL 验证 Windows ACL 后端的 Confine。
func TestLocalProviderConfineWindowsACL(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅在 Windows 上测试 windows-acl")
	}
	p := sandbox.NewLocalProvider(nil)
	policy := sandbox.SandboxPolicy{
		SandboxExecutionPolicy: sandbox.SandboxExecutionPolicy{
			Mode:          sandbox.ModeReadOnly,
			WorkspaceRoot: "/workspace",
		},
		Mode: sandbox.ConfinedReadOnly,
	}
	result, err := p.Confine([]string{"echo", "hello"}, policy)
	if err != nil {
		t.Fatalf("Confine 不应报错: %v", err)
	}
	if result.Enforcement != sandbox.EnforcementFull {
		t.Fatalf("enforcement 应为 full, 实际 %s", result.Enforcement)
	}
	if p.Backend() != sandbox.BackendWindowsACL {
		t.Fatalf("backend 应为 windows-acl, 实际 %s", p.Backend())
	}
}

// TestLocalProviderConfineCustomBwrap 验证自定义 bwrap 后端的 Confine 组装。
func TestLocalProviderConfineCustomBwrap(t *testing.T) {
	p := sandbox.NewLocalProvider(&sandbox.LocalConfig{
		RunnerCommand: "bwrap",
		BackendType:   sandbox.BackendBwrap,
	})
	policy := sandbox.SandboxPolicy{
		SandboxExecutionPolicy: sandbox.SandboxExecutionPolicy{
			Mode:          sandbox.ModeReadOnly,
			WorkspaceRoot: "/workspace",
		},
		Mode: sandbox.ConfinedReadOnly,
	}
	result, err := p.Confine([]string{"echo", "hello"}, policy)
	if err != nil {
		t.Fatalf("Confine 不应报错: %v", err)
	}
	// argv 应以 bwrap 开头
	if result.Argv[0] != "bwrap" {
		t.Fatalf("argv[0] 应为 bwrap, 实际 %s", result.Argv[0])
	}
	// 应包含 -- 分隔符
	foundSep := false
	for _, a := range result.Argv {
		if a == "--" {
			foundSep = true
		}
	}
	if !foundSep {
		t.Fatal("argv 应包含 -- 分隔符")
	}
	// 应以原始 argv 结尾
	if result.Argv[len(result.Argv)-2] != "echo" || result.Argv[len(result.Argv)-1] != "hello" {
		t.Fatalf("argv 应以 echo hello 结尾, 实际 %v", result.Argv)
	}
}

// TestLocalProviderDenialSignatures 验证后端拒绝签名。
func TestLocalProviderDenialSignatures(t *testing.T) {
	p := sandbox.NewLocalProvider(&sandbox.LocalConfig{
		RunnerCommand: "bwrap",
		BackendType:   sandbox.BackendBwrap,
	})
	sigs := p.DenialSignatures()
	if len(sigs) == 0 {
		t.Fatal("bwrap 应有拒绝签名")
	}
	// 应包含 Read-only file system
	found := false
	for _, s := range sigs {
		if s == "Read-only file system" {
			found = true
		}
	}
	if !found {
		t.Fatalf("bwrap 应包含 'Read-only file system' 签名, 实际 %v", sigs)
	}
}
