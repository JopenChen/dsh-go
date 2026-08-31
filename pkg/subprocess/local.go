// 提供 pkg/subprocess 的本地实现：os/exec 驱动 + 进程树终止 + 环境 scrub + spill。
package subprocess

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// DSHEnvPrefix 是 DSH 托管环境变量前缀；实现会先丢弃环境中的该前缀名称。
const DSHEnvPrefix = "DSH_"

// writeSpillFile 把 data 追加到 spill 文件并返回其路径。
// 若 existing 非空则在现有文件上追加，否则在上限内新建。
func writeSpillFile(dir, existing string, data []byte) string {
	if existing == "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return ""
		}
		f, err := os.CreateTemp(dir, "spill-*.txt")
		if err != nil {
			return ""
		}
		path := f.Name()
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return path
		}
		_ = f.Close()
		return path
	}
	f, err := os.OpenFile(existing, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return existing
	}
	_, _ = f.Write(data)
	_ = f.Close()
	return existing
}

// ScrubParentEnv 丢弃环境中所有 DSH_* 名，返回 Scala 兼容的环境骨架（含原样其它项）。
func ScrubParentEnv() []string {
	return scrubEnv(os.Environ())
}

func scrubEnv(applied []string) []string {
	out := make([]string, 0, len(applied))
	for _, kv := range applied {
		// 丢弃 DSH_*（DSH_ 前缀且分隔符是首 '='）。
		if strings.HasPrefix(kv, DSHEnvPrefix) {
			continue
		}
		// 丢弃缺失 DELPHI 分隔的伪项
		if !strings.Contains(kv, "=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// mergeEnv 把显式 env 合并到 base 上；键存在覆盖，空值（""值）表示从环境移除（tombstone）。
func mergeEnv(base []string, overrides map[string]string) []string {
	m := make(map[string]string, len(base))
	for _, kv := range base {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	for k, v := range overrides {
		if v == "" {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// LocalRuntime 是本地子进程运行时（os/exec）。
type LocalRuntime struct{}

// NewLocal 创建本地运行时。
func NewLocal() *LocalRuntime { return &LocalRuntime{} }

// Spawn 按 spec 启动一个受管理子进程并立即返回句柄。
func (r *LocalRuntime) Spawn(spec SubprocessSpawnSpec) *SubprocessHandle {
	stdoutR := &outputReader{}
	stderrR := &outputReader{}
	col := &collectedOutputs{stdout: stdoutR, stderr: stderrR}

	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Cwd
	if spec.GraceMs <= 0 {
		spec.GraceMs = 2000
	}
	// 环境：先 scrub，再合并显式条目。
	cmd.Env = mergeEnv(ScrubParentEnv(), spec.Env)
	// 新进程组/新进程（用于树终止）。
	if runtime.GOOS != "windows" {
		// 置进程组 id，便于 SIGTERM 整组（见 terminate）。
		platformSetSysProcAttr(cmd)
	}

	var stdin io.WriteCloser
	if spec.StdinData != "" {
		cmd.Stdin = strings.NewReader(spec.StdinData)
	} else {
		stdin, _ = cmd.StdinPipe()
	}

	// 收集 stdout/stderr。
	outPipe, _ := cmd.StdoutPipe()
	errPipe, _ := cmd.StderrPipe()

	startErr := cmd.Start()
	pid := -1
	if startErr == nil {
		pid = cmd.Process.Pid
	}

	done := make(chan SubprocessOutcome, 1)
	doneErr := make(chan error, 1)
	h := &SubprocessHandle{
		Pid:       pid,
		collected: col,
		done:      done,
		doneErr:   doneErr,
		terminateFn: func() {
			if cmd.Process == nil {
				return
			}
			killTree(cmd, spec.GraceMs)
		},
	}
	_ = stdin // stdin pipe 由调用方持有（本简化实现未暴露）

	// 若 spawn 失败，立即回填 done/doneErr。
	if startErr != nil {
		doneErr <- fmt.Errorf("subprocess: spawn %q: %w", spec.Argv[0], startErr)
		close(doneErr)
		return h
	}

	memOut := spec.StdoutMaxBytes
	if memOut <= 0 {
		memOut = 64 * 1024
	}
	memErr := spec.StderrMaxBytes
	if memErr <= 0 {
		memErr = 64 * 1024
	}

	// 收集协程 → 追加到 reader（offset-based）。
	go pipeToReader(outPipe, stdoutR, memOut, spec.SpillRoot)
	go pipeToReader(errPipe, stderrR, memErr, spec.SpillRoot)

	// 等待退出。
	go func() {
		wErr := cmd.Wait()
		res := SubprocessOutcome{}
		if wErr == nil {
			res.ExitCode = 0
		} else if ee, ok := wErr.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
			// 信号集：Windows 无信号；*nix 可用 ee.Sys() 取 Signal。
			if sig := platformExitSignal(ee); sig != "" {
				res.Signal = sig
				res.ExitCode = -1
			}
		} else {
			doneErr <- wErr
			close(doneErr)
			return
		}
		done <- res
		close(done)
	}()
	return h
}

// pipeToReader 把流复制到 reader，按内存上限与 spill 处理。
func pipeToReader(r io.Reader, rd *outputReader, memCap int, spillRoot string) {
	buf := make([]byte, 16*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			rd.append(buf[:n], memCap, spillCapFor(spillRoot), spillRoot)
		}
		if err != nil {
			return
		}
	}
}

func spillCapFor(root string) int {
	if root == "" {
		return 0
	}
	return 8 << 20 // 8MB 完整流上限
}

// killTree 在进程树上执行终止升级。
func killTree(cmd *exec.Cmd, graceMs int) {
	pid := cmd.Process.Pid
	if pid <= 0 {
		return
	}
	if runtime.GOOS == "windows" {
		// Windows：taskkill /T 立即 force 整棵进程树。
		_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(pid)).Run()
		return
	}
	// *nix：向进程组发 SIGTERM → grace → SIGKILL，保证整棵树（含孙进程）清理。
	_ = platformSignalGroup(-pid, syscallSIGTERM)
	grace := time.Duration(graceMs) * time.Millisecond
	time.Sleep(grace)
	_ = platformSignalGroup(-pid, syscallSIGKILL)
}

// platformSetSysProcAttr / platformExitSignal / platformSignalGroup 由平台文件提供，
// 信号常量 syscallSIGTERM / syscallSIGKILL 随平台而定。