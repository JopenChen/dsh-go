// 提供基于 pkg/subprocess 的任务实现：Bash 等长任务作为后台 Job 运行。
package jobs

import (
	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/subprocess"
)

// SubprocessSpec 是使用子进程后端启动任务所需的规格。
type SubprocessSpec struct {
	// Kind 任务类型（缺省 bash）。
	Kind JobKind
	// Label 一行模型向标签（命令）。
	Label string
	// OwnerSession owner 会话 id（M46 绑定）。
	OwnerSession brand.SessionID
	// Argv 可执行与参数（绝不做 shell 解释）。
	Argv []string
	// Cwd 工作目录。
	Cwd string
	// StdoutMaxBytes 内存输出上限（截断前）。
	StdoutMaxBytes int
	// SpillRoot spill 根（超限时写完整流）。
	SpillRoot string
	// GraceMs 终止宽限。
	GraceMs int
}

// StartSubprocess 在 runtime 上启动一个子进程后端任务。
func (r *Runtime) StartSubprocess(spec SubprocessSpec) (*Job, error) {
	kind := spec.Kind
	if kind == "" {
		kind = KindBash
	}
	rt := subprocess.NewLocal()
	handle := rt.Spawn(subprocess.SubprocessSpawnSpec{
		Argv:           spec.Argv,
		Cwd:            spec.Cwd,
		StdoutMaxBytes: spec.StdoutMaxBytes,
		SpillRoot:      spec.SpillRoot,
		GraceMs:        spec.GraceMs,
	})
	return r.Start(JobStart{
		Kind:         kind,
		Label:        spec.Label,
		OwnerSession: spec.OwnerSession,
		Run: func() *JobHooks {
			return &JobHooks{
				// cancel：树终止，孤儿进程清理（M46）。
				Cancel: func(reason string) { handle.Terminate() },
				Done:   translateDone(handle),
				ReadOutput: func() string {
					return handle.Collected().Stdout().ReadOutput(0).Text
				},
			}
		},
	})
}

// translateDone 把子进程退出事实映射为 JobOutcome。
func translateDone(handle *subprocess.SubprocessHandle) chan JobOutcome {
	ch := make(chan JobOutcome, 1)
	go func() {
		res := <-handle.Done()
		out := JobOutcome{}
		switch {
		case res.Signal != "":
			out.Status = StatusKilled
			out.Detail = "signal: " + res.Signal
		case res.ExitCode == 0:
			out.Status = StatusCompleted
		default:
			out.Status = StatusFailed
			out.Detail = "exit code: " + itoa(res.ExitCode)
		}
		ch <- out
	}()
	return ch
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}