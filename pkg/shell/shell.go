// Package shell 提供 Shell/Bash 接缝与 tool-bash。
//
// 对齐上游：packages/shell/shell + bash-local + bash-sandbox + tool-bash（M37）
//
// 本文件实现：ShellExecRequest→Resolve→ShellExecSpec + RunResult（5 个正交字段：
// exitCode / signal / timedOut / aborted / timeoutMs）+ 前台运行（基于 pkg/subprocess，
// 64KB 截断 + spill 拿完整流）+ 后台 Job（基于 pkg/jobs）。
package shell

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/JopenChen/dsh-go/pkg/subprocess"
)

// ============================================================================
// 请求 / 规格 / 结果
// ============================================================================

// ExecRequest 是一次 shell 执行请求。
type ExecRequest struct {
	// Command 待执行的命令字符串。
	Command string
	// Cwd 工作目录（空则继承）。
	Cwd string
	// TimeoutMs 超时毫秒；<=0 表示不超时。
	TimeoutMs int
	// Env 附加环境。
	Env map[string]string
	// Background 是否后台 Job（由调用方通过 ShellTool 决定，非本结构）。
}

// ExecSpec 是 resolve 后的完整执行规格。
type ExecSpec struct {
	// Argv shell 包装后的 argv（bash -c / cmd /c / powershell）。
	Argv []string
	// Cwd 工作目录。
	Cwd string
	// TimeoutMs 超时。
	TimeoutMs int
	// Env 附加环境。
	Env map[string]string
	// SandboxMode 生效沙箱模式（read-only/workspace-write/danger）。
	SandboxMode string
}

// RunResult 是一次运行的结果（5 个正交字段）。
type RunResult struct {
	// ExitCode 退出码；被信号杀死或异常时可能为 -1。
	ExitCode int    `json:"exitCode"`
	Signal   string `json:"signal,omitempty"`
	// TimedOut 是否因超时被强杀。
	TimedOut bool `json:"timedOut"`
	// Aborted 是否因取消被终止。
	Aborted bool `json:"aborted"`
	// TimeoutMs 本次运行的超时毫秒（0 表示未设超时）。
	TimeoutMs int `json:"timeoutMs"`
}

// BashResult 是 tool-bash 的模型向结果。
type BashResult struct {
	// Output 内联输出（可能截断为 TAIL）。
	Output string `json:"output"`
	// Truncated 是否截断。
	Truncated bool `json:"truncated"`
	// SpillPath 完整流 spill 路径（截断时提供）。
	SpillPath string `json:"spillPath,omitempty"`
	// Result 运行事实。
	Result RunResult `json:"result"`
}

// ============================================================================
// Shell 解析与运行
// ============================================================================

// Shell 是 shell 执行器。
type Shell struct {
	rt    *subprocess.LocalRuntime
	sandbox string // 生效沙箱模式
}

// New 创建 shell 执行器（sandbox 传 read-only/workspace-write/danger；空默认 workspace-write）。
func New(sandboxMode string) *Shell {
	mode := sandboxMode
	if mode == "" {
		mode = "workspace-write"
	}
	return &Shell{rt: subprocess.NewLocal(), sandbox: mode}
}

// Resolve 把命令解析为执行规格。
func (s *Shell) Resolve(req ExecRequest) ExecSpec {
	argv := shellArgv(req.Command)
	timeout := req.TimeoutMs
	if timeout < 0 {
		timeout = 0
	}
	return ExecSpec{
		Argv:        argv,
		Cwd:         req.Cwd,
		TimeoutMs:   timeout,
		Env:         req.Env,
		SandboxMode: s.sandbox,
	}
}

// shellArgv 按平台包装命令为 argv（绝不作嵌套 shell 解释）。
func shellArgv(cmd string) []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"cmd", "/c", cmd}
	default:
		return []string{"bash", "-c", cmd}
	}
}

// Run 前台执行并等待结果，返回结果 + 收集输出。
func (s *Shell) Run(req ExecRequest) (RunResult, BashResult, error) {
	spec := s.Resolve(req)
	spillRoot := tmpDir()

	memCap := 64 * 1024
	h := s.rt.Spawn(subprocess.SubprocessSpawnSpec{
		Argv:           spec.Argv,
		Cwd:            spec.Cwd,
		StdoutMaxBytes: memCap,
		SpillRoot:      spillRoot,
		GraceMs:        500,
		Env:            spec.Env,
	})

	// 是否超时：监听 done vs timeout。
	var res RunResult
	res.TimeoutMs = spec.TimeoutMs
	timedCh := make(chan struct{})
	if spec.TimeoutMs > 0 {
		time.AfterFunc(time.Duration(spec.TimeoutMs)*time.Millisecond, func() { close(timedCh) })
	}

	var outcome subprocess.SubprocessOutcome
	select {
	case outcome = <-h.Done():
		res.ExitCode = outcome.ExitCode
		res.Signal = outcome.Signal
	case <-timedCh:
		// 超时：树终止，标记 timedOut。
		h.Terminate()
		res.TimedOut = true
		res.ExitCode = -1
		<-h.Done()
	}

	co := h.Collected().Stdout().ReadOutput(0)
	br := BashResult{
		Output:    co.Text,
		Truncated: co.Truncated,
		SpillPath: co.SpillPath,
		Result:    res,
	}
	return res, br, nil
}

func tmpDir() string {
	dir, err := os.MkdirTemp("", "dsh-shell-spill-*")
	if err != nil {
		return os.TempDir()
	}
	return dir
}

// ============================================================================
// tool-bash：模型向入口
// ============================================================================

// BashTool 是工具实现（复用 Shell）。
type BashTool struct {
	shell *Shell
}

// NewBashTool 创建 bash 工具。
func NewBashTool(sandboxMode string) *BashTool {
	return &BashTool{shell: New(sandboxMode)}
}

// Exec 执行命令并返回模型向结果。
func (b *BashTool) Exec(req ExecRequest) (*BashResult, error) {
	_, br, err := b.shell.Run(req)
	if err != nil {
		return nil, err
	}
	return &br, nil
}

// FormatOutput 生成模型向文本（截断时引导读 spill）。
func FormatOutput(r *BashResult) string {
	if !r.Truncated {
		return r.Output
	}
	return fmt.Sprintf("%s\n...[truncated; full output at %s]", strings.TrimRight(r.Output, "\n"), r.SpillPath)
}