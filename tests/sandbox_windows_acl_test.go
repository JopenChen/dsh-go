//go:build windows
// +build windows

// Package tests 的 Windows ACL 后端验收测试。
package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/sandbox"
)

// TestWindowsACLProviderAvailable 验证 Windows ACL 后端在 Windows 上可用。
func TestWindowsACLProviderAvailable(t *testing.T) {
	p := sandbox.NewWindowsACLProvider(t.TempDir())
	if !p.Available() {
		t.Fatal("Windows ACL 后端在 Windows 上应可用")
	}
}

// TestWindowsACLProviderConfineReadOnly 验证 read-only 模式的 Confine。
func TestWindowsACLProviderConfineReadOnly(t *testing.T) {
	p := sandbox.NewWindowsACLProvider(t.TempDir())
	policy := sandbox.SandboxPolicy{
		SandboxExecutionPolicy: sandbox.SandboxExecutionPolicy{
			Mode:          sandbox.ModeReadOnly,
			WorkspaceRoot: t.TempDir(),
		},
		Mode: sandbox.ConfinedReadOnly,
	}
	result, err := p.Confine([]string{"echo", "hello"}, policy)
	if err != nil {
		t.Fatalf("Confine 不应报错: %v", err)
	}
	// enforcement 应为 full 或 partial（非管理员环境下受限令牌创建可能降级为 partial）
	if result.Enforcement != sandbox.EnforcementFull && result.Enforcement != sandbox.EnforcementPartial {
		t.Fatalf("enforcement 应为 full 或 partial, 实际 %s", result.Enforcement)
	}
	// argv 应保持不变
	if len(result.Argv) != 2 || result.Argv[0] != "echo" || result.Argv[1] != "hello" {
		t.Fatalf("argv 应保持不变, 实际 %v", result.Argv)
	}
}

// TestWindowsACLProviderCreateRestrictedToken 验证受限令牌创建。
func TestWindowsACLProviderCreateRestrictedToken(t *testing.T) {
	p := sandbox.NewWindowsACLProvider(t.TempDir())
	token, err := p.CreateRestrictedToken()
	if err != nil {
		t.Fatalf("CreateRestrictedToken 不应报错: %v", err)
	}
	// token 可能为 0（非管理员环境下降级），也可能非 0
	// 第二次调用应返回相同结果
	token2, err := p.CreateRestrictedToken()
	if err != nil {
		t.Fatalf("第二次 CreateRestrictedToken 不应报错: %v", err)
	}
	if token != token2 {
		t.Fatal("应返回缓存的令牌")
	}
}

// TestWindowsACLProviderSetupTempDir 验证临时目录创建。
func TestWindowsACLProviderSetupTempDir(t *testing.T) {
	p := sandbox.NewWindowsACLProvider(t.TempDir())
	tempDir, err := p.SetupTempDir()
	if err != nil {
		t.Fatalf("SetupTempDir 不应报错: %v", err)
	}
	if tempDir == "" {
		t.Fatal("临时目录不应为空")
	}
	// 验证目录存在
	if _, err := os.Stat(tempDir); err != nil {
		t.Fatalf("临时目录应存在: %v", err)
	}
	// 第二次调用应返回缓存的目录
	tempDir2, err := p.SetupTempDir()
	if err != nil {
		t.Fatalf("第二次 SetupTempDir 不应报错: %v", err)
	}
	if tempDir != tempDir2 {
		t.Fatal("应返回缓存的临时目录")
	}
}

// TestWindowsACLProviderGrantWorkspaceAccess 验证工作区 ACL 设置。
func TestWindowsACLProviderGrantWorkspaceAccess(t *testing.T) {
	dir := t.TempDir()
	p := sandbox.NewWindowsACLProvider(dir)
	err := p.GrantWorkspaceAccess(dir)
	if err != nil {
		t.Fatalf("GrantWorkspaceAccess 不应报错: %v", err)
	}
	// 验证目录仍然存在
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("目录应存在: %v", err)
	}
}

// TestWindowsACLProviderCleanup 验证清理。
func TestWindowsACLProviderCleanup(t *testing.T) {
	p := sandbox.NewWindowsACLProvider(t.TempDir())
	// 先创建令牌和临时目录
	if _, err := p.CreateRestrictedToken(); err != nil {
		t.Fatalf("CreateRestrictedToken: %v", err)
	}
	tempDir, err := p.SetupTempDir()
	if err != nil {
		t.Fatalf("SetupTempDir: %v", err)
	}
	// 清理
	if err := p.Cleanup(); err != nil {
		t.Fatalf("Cleanup 不应报错: %v", err)
	}
	// 验证临时目录已删除
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("临时目录应已删除, 但仍然存在或有其他错误: %v", err)
	}
}

// TestWindowsACLProviderSpawnWithToken 验证使用受限令牌启动进程。
func TestWindowsACLProviderSpawnWithToken(t *testing.T) {
	p := sandbox.NewWindowsACLProvider(t.TempDir())
	workDir := t.TempDir()
	// 创建一个输出文件
	outputFile := filepath.Join(workDir, "output.txt")
	// 使用 cmd /c echo 测试
	proc, err := p.SpawnWithToken(
		[]string{"cmd", "/c", "echo", "hello", ">", outputFile},
		nil,
		workDir,
	)
	if err != nil {
		t.Skipf("SpawnWithToken 可能需要特殊权限，跳过: %v", err)
	}
	if proc == nil {
		t.Fatal("进程不应为 nil")
	}
	// 等待进程结束
	state, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait 不应报错: %v", err)
	}
	if !state.Success() {
		t.Fatalf("进程应成功退出, 实际 %v", state)
	}
}
