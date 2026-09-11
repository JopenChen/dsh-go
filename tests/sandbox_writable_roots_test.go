// Package tests 的 writableRoots 与 canonicalPath 验收测试。
//
// 覆盖：
//   - canonicalPath：符号链接解析、路径不存在时返回原路径
//   - writableRoots：read-only 返回空；workspace-write 返回 [workspaceRoot, /tmp, os.TempDir()] 去重规范化
//   - 与 FS 防护栏共享单一真相源的设计验证
package tests

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/sandbox"
)

// TestCanonicalPathExisting 验证存在路径的规范化。
func TestCanonicalPathExisting(t *testing.T) {
	dir := t.TempDir()
	canonical := sandbox.CanonicalPath(dir)
	if canonical == "" {
		t.Fatal("canonicalPath 不应返回空")
	}
	// 规范化后路径应存在
	if _, err := os.Stat(canonical); err != nil {
		t.Fatalf("规范化后路径应存在: %v", err)
	}
}

// TestCanonicalPathMissing 验证路径不存在时返回原路径（保守策略）。
func TestCanonicalPathMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nonexistent", "path")
	result := sandbox.CanonicalPath(missing)
	if result != missing {
		t.Fatalf("路径不存在时应返回原路径, 期望 %q, 实际 %q", missing, result)
	}
}

// TestWritableRootsReadOnly 验证 read-only 模式返回空列表。
func TestWritableRootsReadOnly(t *testing.T) {
	policy := sandbox.SandboxExecutionPolicy{
		Mode:          sandbox.ModeReadOnly,
		WorkspaceRoot: "/workspace/app",
	}
	roots := sandbox.WritableRoots(policy)
	if len(roots) != 0 {
		t.Fatalf("read-only 模式应返回空列表, 实际 %v", roots)
	}
}

// TestWritableRootsDanger 验证 danger-full-access 模式返回空列表（绕过沙箱，不需要可写根）。
func TestWritableRootsDanger(t *testing.T) {
	policy := sandbox.SandboxExecutionPolicy{
		Mode:          sandbox.ModeDangerFullAccess,
		WorkspaceRoot: "/workspace/app",
	}
	roots := sandbox.WritableRoots(policy)
	if len(roots) != 0 {
		t.Fatalf("danger 模式应返回空列表, 实际 %v", roots)
	}
}

// TestWritableRootsWorkspaceWrite 验证 workspace-write 模式返回包含 workspaceRoot 和临时目录。
func TestWritableRootsWorkspaceWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上 /tmp 路径语义不同，跳过")
	}
	policy := sandbox.SandboxExecutionPolicy{
		Mode:          sandbox.ModeWorkspaceWrite,
		WorkspaceRoot: "/workspace/app",
	}
	roots := sandbox.WritableRoots(policy)
	if len(roots) < 2 {
		t.Fatalf("workspace-write 应至少返回 workspaceRoot 和 tmpdir, 实际 %v", roots)
	}
	// 验证包含 workspaceRoot
	foundWorkspace := false
	for _, r := range roots {
		if r == "/workspace/app" {
			foundWorkspace = true
		}
	}
	if !foundWorkspace {
		t.Fatalf("应包含 workspaceRoot /workspace/app, 实际 %v", roots)
	}
}

// TestWritableRootsDeduplication 验证重复路径被去重。
func TestWritableRootsDeduplication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上跳过")
	}
	// 当 workspaceRoot 等于 /tmp 时，不应重复出现
	policy := sandbox.SandboxExecutionPolicy{
		Mode:          sandbox.ModeWorkspaceWrite,
		WorkspaceRoot: "/tmp",
	}
	roots := sandbox.WritableRoots(policy)
	seen := map[string]bool{}
	for _, r := range roots {
		if seen[r] {
			t.Fatalf("路径重复: %s, 全部 %v", r, roots)
		}
		seen[r] = true
	}
}

// TestWritableRootsCanonical 验证返回的路径经过规范化。
func TestWritableRootsCanonical(t *testing.T) {
	dir := t.TempDir()
	policy := sandbox.SandboxExecutionPolicy{
		Mode:          sandbox.ModeWorkspaceWrite,
		WorkspaceRoot: dir,
	}
	roots := sandbox.WritableRoots(policy)
	// workspaceRoot 应被规范化
	found := false
	for _, r := range roots {
		if r == sandbox.CanonicalPath(dir) {
			found = true
		}
	}
	if !found {
		t.Fatalf("应包含规范化后的 workspaceRoot, 实际 %v", roots)
	}
}
