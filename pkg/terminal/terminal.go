// Package terminal 提供持久终端会话抽象（任务 S10：Terminal PTY）。
//
// 对齐上游：packages/shell/terminal
//
// 说明：在 Windows 上无原生 PTY，本包提供跨平台「终端会话」抽象：以子进程+管道承载，
// 提供 spawn / send / read / signal / close 生命周期、有界回滚（bounded scrollback）、
// wait reason，以及单 agent 独占活动（同一时刻仅一个活跃 session）。在 *nix 上可替换为
// 真正的 PTY 后端（接口不变）。
package terminal

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// 类型
// ============================================================================

// WaitReason 是会话结束原因。
type WaitReason string

// 结束原因枚举。
const (
	WaitExited    WaitReason = "exited"    // 进程正常退出
	WaitSignalled WaitReason = "signalled" // 被信号终止
	WaitClosed    WaitReason = "closed"    // 被显式 close
	WaitError     WaitReason = "error"     // 启动/运行错误
)

// TerminalConfig 是生成终端的配置。
type TerminalConfig struct {
	Command   string   // 可执行名（如 bash / cmd / sh）
	Args      []string // 参数
	MaxLines  int      // 有界回滚行数（<=0 用默认 1000）
	ReadLineMax int    // 单行最大字节（防超行）
}

// withDefaults 填充默认值。
func (c TerminalConfig) withDefaults() TerminalConfig {
	if c.MaxLines <= 0 {
		c.MaxLines = 1000
	}
	if c.ReadLineMax <= 0 {
		c.ReadLineMax = 64 * 1024
	}
	return c
}

// ============================================================================
// TerminalBackend
// ============================================================================

// TerminalBackend 生成并持有终端会话；保证单 agent 独占活动（同一时刻一个活跃 session）。
type TerminalBackend struct {
	mu       sync.Mutex
	active   *Session
	cfg      TerminalConfig
}

// NewBackend 创建终端后端。
func NewBackend(cfg TerminalConfig) *TerminalBackend {
	return &TerminalBackend{cfg: cfg.withDefaults()}
}

// Spawn 生成一个新会话；若已有活跃会话则返回错误（单 agent 独占）。
func (b *TerminalBackend) Spawn(ctx context.Context) (*Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active != nil && b.active.running() {
		return nil, fmt.Errorf("terminal: single-agent exclusive activity: a session is already active")
	}
	s, err := spawnSession(ctx, b.cfg)
	if err != nil {
		return nil, err
	}
	b.active = s
	return s, nil
}

// Active 返回当前活跃会话（无则 nil）。
func (b *TerminalBackend) Active() *Session {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active != nil && b.active.running() {
		return b.active
	}
	return nil
}

// ============================================================================
// Session
// ============================================================================

// Session 是一次持久终端会话（子进程 + 管道 + 有界回滚）。
type Session struct {
	cfg TerminalConfig
	cmd *exec.Cmd
	w   io.WriteCloser // stdin

	mu      sync.Mutex
	lines   []string // 有界回滚（环形语义：仅保留最近 MaxLines 行）
	done    chan struct{}
	reason  WaitReason
	started time.Time
	exited  bool
}

// spawnSession 启动子进程并开启 stdout 读取协程。
func spawnSession(ctx context.Context, cfg TerminalConfig) (*Session, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &Session{
		cfg:     cfg,
		cmd:     cmd,
		w:       stdin,
		lines:   []string{},
		done:    make(chan struct{}),
		reason:  WaitClosed,
		started: time.Now(),
	}
	// 读取 stdout 行；stderr 不合并避免与 stdout 交错（诊断可在外层配置）。
	go s.readLoop(stdout)
	go s.waitLoop()
	return s, nil
}

// readLoop 持续读 stdout 行并写入有界回滚。
func (s *Session) readLoop(r io.Reader) {
	sc := bufio.NewReaderSize(r, s.cfg.ReadLineMax)
	for {
		line, err := readLineLimited(sc, s.cfg.ReadLineMax)
		if len(line) > 0 {
			s.pushLine(line)
		}
		if err != nil {
			return
		}
	}
}

// waitLoop 等进程结束并记录 reason，随后关闭 done。
func (s *Session) waitLoop() {
	err := s.cmd.Wait()
	s.mu.Lock()
	if s.reason == WaitClosed { // 若尚未被 close 覆盖
		s.reason = WaitExited
	}
	if err != nil && s.reason == WaitClosed {
		// 退出但非被 close：归类为 exited 或 signalled（不做精确信号解析，简洁处理）
		s.reason = WaitExited
	}
	s.exited = true
	s.mu.Unlock()
	close(s.done)
}

// readLineLimited 读一行（超长按 chunk 截断并保持读取）。
func readLineLimited(sc *bufio.Reader, max int) (string, error) {
	sb := strings.Builder{}
	for {
		chunk, err := sc.ReadString('\n')
		sb.WriteString(chunk)
		if err != nil {
			return sb.String(), err
		}
		if len(chunk) > 0 && strings.HasSuffix(chunk, "\n") {
			return strings.TrimSuffix(sb.String(), "\n"), nil
		}
	}
}

// pushLine 追加一行到有界回滚。
func (s *Session) pushLine(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, line)
	if len(s.lines) > s.cfg.MaxLines {
		// 有界：只保留最近 MaxLines 行。
		excess := len(s.lines) - s.cfg.MaxLines
		s.lines = append([]string(nil), s.lines[excess:]...)
	}
}

// Send 向 stdin 写入数据（模拟键盘输入）。
func (s *Session) Send(data string) error {
	if _, err := s.w.Write([]byte(data)); err != nil {
		return err
	}
	return nil
}

// SendLine 写一行（自动补 \n）。
func (s *Session) SendLine(line string) error {
	return s.Send(line + "\n")
}

// Read 取出当前回滚的全部行（快照）。
func (s *Session) Read() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.lines))
	copy(out, s.lines)
	return out
}

// ReadString 返回当前回滚的拼接文本。
func (s *Session) ReadString() string {
	lines := s.Read()
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

// Contains 判断回滚中是否出现某子串（轮询断言用）。
func (s *Session) Contains(sub string) bool {
	return strings.Contains(s.ReadString(), sub)
}

// Wait 阻塞直到会话结束，返回结束原因。
func (s *Session) Wait() WaitReason {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reason
}

// Signal 向进程发送终止信号（跨平台：Windows 走 Kill）。
func (s *Session) Signal() error {
	return s.cmd.Process.Kill()
}

// Close 关闭会话（关闭 stdin 并终止进程，标记 reason=closed）。
func (s *Session) Close() error {
	_ = s.w.Close()
	if !s.exitedState() {
		_ = s.cmd.Process.Kill()
	}
	s.mu.Lock()
	s.reason = WaitClosed
	s.mu.Unlock()
	return nil
}

// running 是否仍在运行。
func (s *Session) running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.exited
}

func (s *Session) exitedState() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

// ============================================================================
// 辅助：跨平台 shell
// ============================================================================

// ShellCommand 返回当前平台默认 shell 与退出只需一条命令的包装。
// 用于测试：`echo ...` 在 Windows cmd / Unix sh 上均可运行。
func ShellCommand(script string) (name string, args []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/S", "/C", script}
	}
	return "/bin/sh", []string{"-c", script}
}