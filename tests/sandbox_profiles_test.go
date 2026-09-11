// Package tests 的本地后端 profile 构建验收测试。
//
// 覆盖：
//   - BwrapProfileArgs：bwrap 命令行参数构建
//   - LandlockProfileArgs：landlock 授权参数构建
//   - SeatbeltProfileArgs：macOS sandbox-exec SBPL 脚本构建
package tests

import (
	"runtime"
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/sandbox"
)

// TestBwrapProfileReadOnly 验证 bwrap read-only profile。
func TestBwrapProfileReadOnly(t *testing.T) {
	policy := sandbox.SandboxPolicy{
		SandboxExecutionPolicy: sandbox.SandboxExecutionPolicy{
			Mode:          sandbox.ModeReadOnly,
			WorkspaceRoot: "/workspace",
		},
		Mode: sandbox.ConfinedReadOnly,
	}
	args := sandbox.BwrapProfileArgs(policy)
	// 应包含基础参数
	expected := []string{"--ro-bind", "/", "/", "--dev", "/dev", "--unshare-pid", "--proc", "/proc", "--die-with-parent"}
	for i, e := range expected {
		if args[i] != e {
			t.Fatalf("参数 %d 应为 %q, 实际 %q", i, e, args[i])
		}
	}
	// read-only 不应包含 --bind workspace
	for _, a := range args {
		if a == "--bind" {
			t.Fatal("read-only 不应包含 --bind")
		}
	}
}

// TestBwrapProfileWorkspaceWrite 验证 bwrap workspace-write profile。
func TestBwrapProfileWorkspaceWrite(t *testing.T) {
	policy := sandbox.SandboxPolicy{
		SandboxExecutionPolicy: sandbox.SandboxExecutionPolicy{
			Mode:          sandbox.ModeWorkspaceWrite,
			WorkspaceRoot: "/workspace/app",
		},
		Mode: sandbox.ConfinedWorkspaceWrite,
	}
	args := sandbox.BwrapProfileArgs(policy)
	// 应包含 --tmpfs /tmp
	foundTmpfs := false
	foundBind := false
	for i, a := range args {
		if a == "--tmpfs" && i+1 < len(args) && args[i+1] == "/tmp" {
			foundTmpfs = true
		}
		if a == "--bind" && i+1 < len(args) && args[i+1] == "/workspace/app" {
			foundBind = true
		}
	}
	if !foundTmpfs {
		t.Fatal("workspace-write 应包含 --tmpfs /tmp")
	}
	if !foundBind {
		t.Fatal("workspace-write 应包含 --bind workspaceRoot")
	}
}

// TestLandlockProfileReadOnly 验证 landlock read-only profile。
func TestLandlockProfileReadOnly(t *testing.T) {
	policy := sandbox.SandboxPolicy{
		SandboxExecutionPolicy: sandbox.SandboxExecutionPolicy{
			Mode:          sandbox.ModeReadOnly,
			WorkspaceRoot: "/workspace",
		},
		Mode: sandbox.ConfinedReadOnly,
	}
	args := sandbox.LandlockProfileArgs(policy)
	// read-only：readOnly=[/], readWrite=[/dev/null]
	// 验证包含 --read-only / 和 --read-write /dev/null
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--read-only /") {
		t.Fatalf("应包含 --read-only /, 实际: %s", joined)
	}
	if !strings.Contains(joined, "--read-write /dev/null") {
		t.Fatalf("应包含 --read-write /dev/null, 实际: %s", joined)
	}
	// read-only 不应包含 workspaceRoot 在 read-write 中
	if strings.Contains(joined, "--read-write /workspace") {
		t.Fatalf("read-only 不应在 read-write 中包含 workspace, 实际: %s", joined)
	}
}

// TestLandlockProfileWorkspaceWrite 验证 landlock workspace-write profile。
func TestLandlockProfileWorkspaceWrite(t *testing.T) {
	policy := sandbox.SandboxPolicy{
		SandboxExecutionPolicy: sandbox.SandboxExecutionPolicy{
			Mode:          sandbox.ModeWorkspaceWrite,
			WorkspaceRoot: "/workspace/app",
		},
		Mode: sandbox.ConfinedWorkspaceWrite,
	}
	args := sandbox.LandlockProfileArgs(policy)
	joined := strings.Join(args, " ")
	// workspace-write：readWrite 应包含 /dev/null, /tmp, workspaceRoot
	if !strings.Contains(joined, "--read-write /dev/null") {
		t.Fatalf("应包含 --read-write /dev/null, 实际: %s", joined)
	}
	if !strings.Contains(joined, "--read-write /tmp") {
		t.Fatalf("应包含 --read-write /tmp, 实际: %s", joined)
	}
	if !strings.Contains(joined, "--read-write /workspace/app") {
		t.Fatalf("应包含 --read-write /workspace/app, 实际: %s", joined)
	}
}

// TestSeatbeltProfileReadOnly 验证 seatbelt read-only SBPL 脚本。
func TestSeatbeltProfileReadOnly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("seatbelt 仅在 macOS 上有意义")
	}
	policy := sandbox.SandboxPolicy{
		SandboxExecutionPolicy: sandbox.SandboxExecutionPolicy{
			Mode:          sandbox.ModeReadOnly,
			WorkspaceRoot: "/workspace",
		},
		Mode: sandbox.ConfinedReadOnly,
	}
	args := sandbox.SeatbeltProfileArgs(policy)
	// 第一个参数应为 -p
	if args[0] != "-p" {
		t.Fatalf("第一个参数应为 -p, 实际 %q", args[0])
	}
	// SBPL 脚本应包含基础规则
	sbpl := args[1]
	if !strings.Contains(sbpl, "(version 1)") {
		t.Fatalf("SBPL 应包含 (version 1), 实际: %s", sbpl)
	}
	if !strings.Contains(sbpl, "(allow default)") {
		t.Fatalf("SBPL 应包含 (allow default), 实际: %s", sbpl)
	}
	if !strings.Contains(sbpl, "(deny file-write*)") {
		t.Fatalf("SBPL 应包含 (deny file-write*), 实际: %s", sbpl)
	}
	if !strings.Contains(sbpl, "/dev/null") {
		t.Fatalf("SBPL 应包含 /dev/null, 实际: %s", sbpl)
	}
}

// TestSeatbeltProfileWorkspaceWrite 验证 seatbelt workspace-write SBPL 脚本。
func TestSeatbeltProfileWorkspaceWrite(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("seatbelt 仅在 macOS 上有意义")
	}
	policy := sandbox.SandboxPolicy{
		SandboxExecutionPolicy: sandbox.SandboxExecutionPolicy{
			Mode:          sandbox.ModeWorkspaceWrite,
			WorkspaceRoot: "/workspace/app",
		},
		Mode: sandbox.ConfinedWorkspaceWrite,
	}
	args := sandbox.SeatbeltProfileArgs(policy)
	sbpl := args[1]
	// workspace-write 应包含 workspaceRoot 的 subpath 授权
	if !strings.Contains(sbpl, "/workspace/app") {
		t.Fatalf("SBPL 应包含 workspaceRoot, 实际: %s", sbpl)
	}
	if !strings.Contains(sbpl, "(subpath") {
		t.Fatalf("SBPL 应包含 (subpath ...) 授权, 实际: %s", sbpl)
	}
}
