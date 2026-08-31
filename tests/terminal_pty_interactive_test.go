// 本文件验证任务 S10：Terminal PTY（跨平台终端会话）。
//
// 覆盖：spawn→read→wait reason=exited；单 agent 独占活动；有界回滚截断；
// 交互 send→read→close 循环。命令脚本按 runtime.GOOS 生成，保证 Windows/Unix 均可跑。
package tests

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/terminal"
)

// TestTerminalSpawnReadWait 验证 spawn → 读取输出 → wait reason=exited。
func TestTerminalSpawnReadWait(t *testing.T) {
	ctx := context.Background()
	name, args := terminal.ShellCommand(echoScript("TERM_MARKER_123"))
	backend := terminal.NewBackend(terminal.TerminalConfig{Command: name, Args: args})
	sess, err := backend.Spawn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	if !waitForOutput(t, sess, "TERM_MARKER_123", 3*time.Second) {
		t.Fatalf("未读到期望输出 TERM_MARKER_123，回滚=%q", sess.ReadString())
	}
	if reason := sess.Wait(); reason != terminal.WaitExited {
		t.Fatalf("wait reason 应为 exited，实际 %s", reason)
	}
}

// TestTerminalBoundedScrollback 验证有界回滚：输出超过 MaxLines 只保留最近行。
func TestTerminalBoundedScrollback(t *testing.T) {
	ctx := context.Background()
	name, args := terminal.ShellCommand(lineLoopScript("line-", 20))
	backend := terminal.NewBackend(terminal.TerminalConfig{Command: name, Args: args, MaxLines: 5})
	sess, err := backend.Spawn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	_ = sess.Wait()

	if lines := sess.Read(); len(lines) > 5 {
		t.Fatalf("有界回滚应 ≤5 行，实际 %d 行", len(lines))
	}
	if !strings.Contains(strings.Join(sess.Read(), "\n"), "line-20") {
		t.Fatalf("应保留最新行 line-20，实际 %q", sess.ReadString())
	}
}

// TestTerminalExclusive 验证单 agent 独占：活跃 session 时再次 spawn 报错。
func TestTerminalExclusive(t *testing.T) {
	ctx := context.Background()
	name, args := sleepScript()
	backend := terminal.NewBackend(terminal.TerminalConfig{Command: name, Args: args})
	sess, err := backend.Spawn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if _, err := backend.Spawn(ctx); err == nil {
		t.Fatal("活跃 session 存在时再次 spawn 应被拒绝（单 agent 独占）")
	}
}

// TestTerminalInteractiveSendRead 验证 send→read→close 交互循环（尽力式：无法交互则跳过）。
func TestTerminalInteractiveSendRead(t *testing.T) {
	name, args := interactiveShell()
	if name == "" {
		t.Skip("当前平台无可用交互 shell")
	}
	ctx := context.Background()
	backend := terminal.NewBackend(terminal.TerminalConfig{Command: name, Args: args})
	sess, err := backend.Spawn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	if err := sess.SendLine("echo INTERACTIVE_OK"); err != nil {
		t.Fatal(err)
	}
	if !waitForOutput(t, sess, "INTERACTIVE_OK", 4*time.Second) {
		t.Fatalf("未读到交互输出 INTERACTIVE_OK，回滚=%q", sess.ReadString())
	}
	_ = sess.SendLine("exit")
}

// ============================================================================
// 跨平台脚本助手
// ============================================================================

// echoScript 生成「输出指定串」的脚本。
func echoScript(text string) string {
	esc := strings.ReplaceAll(text, `"`, `""`)
	if runtime.GOOS == "windows" {
		return "echo " + esc
	}
	return "echo \"" + esc + "\""
}

// lineLoopScript 生成循环输出 1..n 行的脚本（Windows 用 cmd for /L，Unix 用 sh while）。
func lineLoopScript(prefix string, n int) string {
	if runtime.GOOS == "windows" {
		return "for /L %i in (1,1," + itoa(n) + ") do @echo " + prefix + "%i"
	}
	return "i=1; while [ $i -le " + itoa(n) + " ]; do echo " + prefix + "$i; i=$((i+1)); done"
}

// itoa 把整数转为十进制字符串（测试小工具）。
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// sleepScript 生成「运行 1 秒」的脚本。
func sleepScript() (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-Command", "Start-Sleep -Seconds 1"}
	}
	return "sh", []string{"-c", "sleep 1"}
}

// interactiveShell 返回可交互的 REPL；无可交互 shell 时返回空串表示跳过。
func interactiveShell() (string, []string) {
	if runtime.GOOS == "windows" {
		// cmd.exe 交互 REPL 可用（从 stdin 读取命令）。
		return "cmd", nil
	}
	return "sh", nil
}

// waitForOutput 轮询会话回滚直到出现 substr 或超时。
func waitForOutput(t *testing.T, sess *terminal.Session, substr string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if sess.Contains(substr) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return sess.Contains(substr)
}