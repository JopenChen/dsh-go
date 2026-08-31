// Package tests 的 Shell/Bash 接缝（M37）验收测试。
//
// 覆盖：
//   - bash('seq 1 100000') → 默认 64kb 截断 → 读 spillPath 能拿到完整 100000 行
//   - 超时 → RunResult.timedOut=true（正交字段）
//   - 正常退出 → exitCode=0，未设置 timeoutMs=0
package tests

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/shell"
)

// bigSeqCmd 生成一个输出极多行的命令。
func bigSeqCmd() string {
	if runtime.GOOS == "windows" {
		return "for /L %i in (1,1,20000) do @echo line %i"
	}
	return "seq 1 100000"
}

// TestBashSpillFullContent 验证 64KB 截断 + spill 拿完整内容。
func TestBashSpillFullContent(t *testing.T) {
	s := shell.New("workspace-write")
	res, br, err := s.Run(shell.ExecRequest{Command: bigSeqCmd(), Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("正常命令应 exitCode=0, 实际 %+v", res)
	}
	if res.TimeoutMs != 0 {
		t.Fatalf("未设超时应 timeoutMs=0, 实际 %d", res.TimeoutMs)
	}
	if !br.Truncated {
		t.Fatal("大输出应被截断")
	}
	if br.SpillPath == "" {
		t.Fatal("截断时应提供 spillPath")
	}
	// 从 spill 读完整流。
	data, err := os.ReadFile(br.SpillPath)
	if err != nil {
		t.Fatalf("读 spill 失败: %v", err)
	}
	rows := strings.Count(string(data), "\n")
	want := 100000
	if runtime.GOOS == "windows" {
		want = 20000
	}
	if rows < want-10 {
		t.Fatalf("spill 应含约 %d 行, 实际 %d 行", want, rows)
	}
	// 模型向结果引导读 spill。
	formatted := shell.FormatOutput(&br)
	if !strings.Contains(formatted, "full output at") {
		t.Fatalf("格式输出应引导读 spill: %q", formatted)
	}
}

// TestBashTimeout 验证超时 → timedOut=true。
func TestBashTimeout(t *testing.T) {
	s := shell.New("workspace-write")
	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "ping -n 30 127.0.0.1 >nul"
	} else {
		cmd = "sleep 30"
	}
	res, br, err := s.Run(shell.ExecRequest{Command: cmd, Cwd: t.TempDir(), TimeoutMs: 500})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatalf("应 timedOut=true, 实际 %+v", res)
	}
	if res.TimeoutMs != 500 {
		t.Fatalf("timeoutMs 应记录 500, 实际 %d", res.TimeoutMs)
	}
	_ = br
}

// TestBashOutputLines 验证正常短输出直接内联返回（不截断）。
func TestBashOutputLines(t *testing.T) {
	s := shell.New("workspace-write")
	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "echo hello"
	} else {
		cmd = "echo hello"
	}
	_, br, err := s.Run(shell.ExecRequest{Command: cmd, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if br.Truncated {
		t.Fatal("短输出不应截断")
	}
	if !strings.Contains(br.Output, "hello") {
		t.Fatalf("输出应含 hello, 实际 %q", br.Output)
	}
}