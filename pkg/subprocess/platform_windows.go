//go:build windows

// 平台相关实现（Windows）：新进程组 + taskkill 树终止 + 无信号词汇。
package subprocess

import (
	"os/exec"
	"syscall"
)

// syscallSIGTERM/SIGKILL 在 Windows 上无等价物（用 taskkill /T 直接截断树）。
const (
	syscallSIGTERM = 0
	syscallSIGKILL = 0
)

// platformSetSysProcAttr 创建新的进程组（Windows：CREATE_NEW_PROCESS_GROUP），
// 使 taskkill /T 能按树根终止该组。
func platformSetSysProcAttr(cmd *exec.Cmd) {
	if cmd.SysProcAttr != nil {
		cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// platformExitSignal Windows 无信号模型，返回 ""（退出码决定结果）。
func platformExitSignal(*exec.ExitError) string { return "" }

// platformSignalGroup Windows 不通过信号投递进程组（killTree 走 taskkill /T）。
func platformSignalGroup(int, int) error { return nil }