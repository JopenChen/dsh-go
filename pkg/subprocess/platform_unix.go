//go:build !windows

// 平台相关实现（*nix）：进程组设置、信号收集、进程组信号投递。
package subprocess

import (
	"os/exec"
	"syscall"
)

const (
	syscallSIGTERM = syscall.SIGTERM
	syscallSIGKILL = syscall.SIGKILL
)

// platformSetSysProcAttr 把子进程放入独立进程组（便于整组 SIGTERM/SIGKILL）。
func platformSetSysProcAttr(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// platformExitSignal 从 ExitError 中读取终止信号名；未因信号而死返回 ""。
func platformExitSignal(ee *exec.ExitError) string {
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return ws.Signal().String()
	}
	return ""
}

// platformSignalGroup 向进程组（pid 为负）投递信号。
func platformSignalGroup(pid, sig int) error {
	_ = sig
	return syscall.Kill(pid, syscall.Signal(sig))
}