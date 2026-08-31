// Package subprocess 提供子进程（Subprocess）接缝。
//
// 对齐上游：packages/subprocess/subprocess + subprocess-local
//
// 本文件对应任务 M36：Subprocess 接缝。
//
// 设计要点：
//   - SubprocessSpawnSpec 完全显式、不自带默认（argv/cwd/stdio/grace/env 全部给全）；
//   - 托管 DSH_* 环境命名空间：spawn 时先 scrubbedParentEnv() 丢弃环境中的 DSH_* 名，
//     再把调用方显式 env 合并上去（缺失的普通环境项也可用 tombstone 移除）；
//   - CollectedOutput 报告每个流的截断与 spill 恢复状态：超过 maxBytes 保留 TAIL，
//     并可在 spill 开启时把完整流写入 spill 文件；
//   - 结束只在进程树上生效：terminate() 是唯一终止动词，树作用域；Windows 用
//     taskkill /T，*nix 用 SIGTERM → grace → SIGKILL；子进程树全部清理无残留。
package subprocess

import (
	"bytes"
	"sync"
)

// ============================================================================
// 类型
// ============================================================================

// CollectedOutput 是一条已捕获流的文本 + 恢复信息。
type CollectedOutput struct {
	// Text 已收集文本——截断时为流的 TAIL。
	Text string `json:"text"`
	// Truncated 是否从 Text 中丢弃过字节。
	Truncated bool `json:"truncated"`
	// SpillPath 当截断且可用时，含完整流的文件路径。
	SpillPath string `json:"spillPath,omitempty"`
}

// SubprocessCollect 是一条输出流的受限内存收集配置（可选完整流 spill 文件）。
type SubprocessCollect struct {
	// MaxBytes 内存上限/字节；溢出保留 TAIL。
	MaxBytes int
	// SpillMaxBytes 完整流 spill 上限；<=0 表示不启用 spill（仅诊断尾）。
	SpillMaxBytes int
}

// SubprocessStdinMode 是 stdin 布局：ignore 把 fd0 指向 /dev/null，pipe 暴露 stdin 流。
type SubprocessStdinMode string

const (
	StdinIgnore SubprocessStdinMode = "ignore"
	StdinPipe   SubprocessStdinMode = "pipe"
	// StdinData 写入字节后即关闭（批处理形态）。
	StdinData = "data"
)

// SubprocessSpawnSpec 是完全独立决定的 spawn 请求（本接缝不自带默认）。
type SubprocessSpawnSpec struct {
	// Argv 可执行程序与参数；程序是 argv[0]；此处绝不做 shell 解释。
	Argv []string
	// Cwd 子进程工作目录。
	Cwd string
	// StdoutMaxBytes / StderrMaxBytes 收集模式内存上限；<=0 表示缺省 64KB。
	StdoutMaxBytes int
	StderrMaxBytes int
	// SpillRoot 完整流 spill 根目录；空表示不启用 spill。
	SpillRoot string
	// GraceMs SIGTERM → SIGKILL 的宽限毫秒。
	GraceMs int
	// Env 合并到 scrubbedParentEnv 之上的显式环境条目。
	Env map[string]string
	// StdinData 给 stdin 的可选数据（非空时关闭 stdin）。
	StdinData string
}

// SubprocessOutcome 是已关闭进程的退出事实（不含超时/取消分类，调用方读它自有信号）。
type SubprocessOutcome struct {
	// ExitCode 退出码；进程被信号杀死时为 -1。
	ExitCode int
	// Signal 终止信号名（如 SIGTERM）；正常退出为空。
	Signal string
}

// SubprocessHandle 是一个以自己进程树为根的活子进程。
type SubprocessHandle struct {
	// Pid 进程 id（树根）。
	Pid int
	// collected 收集模式输出（offset-based 非消费读取）。
	collected *collectedOutputs
	// done 进程关闭时解析为退出事实；spawn 级失败才 reject（用 err channel 表达）。
	done     chan SubprocessOutcome
	doneErr  chan error
	termOnce sync.Once
	terminateFn func()
}

// Collected 返回收集模式输出读取器（也可在退出后可读）。
func (h *SubprocessHandle) Collected() *collectedOutputs { return h.collected }

// Done 返回进程关闭时的退出事实。
func (h *SubprocessHandle) Done() <-chan SubprocessOutcome { return h.done }

// Terminate 在进程树上开始 SIGTERM → grace → SIGKILL 升级（Windows 立即 force）。
// 幂等；树消失后为 no-op。
func (h *SubprocessHandle) Terminate() {
	h.termOnce.Do(h.terminateFn)
}

// ============================================================================
// 收集输出读取器
// ============================================================================

// collectedOutputs 保存 stdout/stderr 收集流。
type collectedOutputs struct {
	stdout *outputReader
	stderr *outputReader
}

// Stdout 返回 stdout 读取器（spawn 时若为收集模式则非 nil）。
func (c *collectedOutputs) Stdout() *outputReader { return c.stdout }

// Stderr 返回 stderr 读取器。
func (c *collectedOutputs) Stderr() *outputReader { return c.stderr }

// outputReader 是光标无关的增量输出读取器：offset 是整流字节坐标，由调用方持有，
// 多个独立 reader 互不消费对方输出；readFrom(0) 是在定稿后的批量结果。
type outputReader struct {
	mu       sync.Mutex
	trunc    bytes.Buffer // 保留的 TAIL（或未截断时的全部）。
	fullSpill string      // 完整流 spill 文件路径。
	truncated bool        // 是否截断（溢出了内存窗）。
	total     int         // 整流总字节数。
}

func (r *outputReader) readFrom(fromByte int) (text string, nextOffset int, lossy bool, spillPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.truncated {
		// 请求 offset 滑出内存窗 → lossy，返回整个保留 TAIL。
		lossy = true
		return r.trunc.String(), r.total, lossy, r.fullSpill
	}
	cur := r.trunc.Bytes()
	if fromByte >= len(cur) {
		return "", fromByte, false, r.fullSpill
	}
	delta := cur[fromByte:]
	return string(delta), fromByte + len(delta), false, r.fullSpill
}

// ReadOutput 是一次增量读取（便捷方法）。
func (r *outputReader) ReadOutput(from int) CollectedOutput {
	text, _, lossy, spill := r.readFrom(from)
	return CollectedOutput{Text: text, Truncated: lossy, SpillPath: spill}
}

// append 追加字节；超内存上限时切换到截断模式：保留 TAIL，可写 spill。
func (r *outputReader) append(data []byte, memCap, spillCap int, spillDir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(data) == 0 {
		return
	}
	r.total += len(data)
	switch {
	case r.truncated:
		// 已截断：继续把新字节 append 到 spill（若启用），并刷新 TAIL。
		if spillDir != "" {
			r.fullSpill = writeSpillFile(spillDir, r.fullSpill, data)
		}
		r.appendTail(data, memCap)
	default:
		if r.trunc.Len()+len(data) <= memCap {
			r.trunc.Write(data)
			return
		}
		// 溢出 → 截断：写入完整流 spill（若启用），刷新 TAIL。
		if spillDir != "" {
			r.fullSpill = writeSpillFile(spillDir, r.fullSpill, r.trunc.Bytes())
			r.trunc.Reset()
			r.truncated = true
			r.fullSpill = writeSpillFile(spillDir, r.fullSpill, data)
		} else {
			r.truncated = true
		}
		r.appendTail(data, memCap)
	}
}

// appendTail 维护截断模式下的内存 TAIL（保留最后 memCap 字节）。
func (r *outputReader) appendTail(data []byte, memCap int) {
	r.trunc.Write(data)
	if r.trunc.Len() <= memCap {
		return
	}
	over := r.trunc.Len() - memCap
	buf := r.trunc.Bytes()
	r.trunc.Reset()
	r.trunc.Write(buf[over:])
}