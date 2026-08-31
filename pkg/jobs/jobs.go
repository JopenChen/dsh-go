// Package jobs 提供后台任务运行时（Jobs Runtime）与 owner 绑定。
//
// 对齐上游：packages/jobs/jobs（S11）+ owner binding（M46）
//
// 本文件对应任务 S11（Jobs Runtime）与 M46（Job 生命周期 owner 绑定）。
//
// 设计要点：
//   - Job 由生产者声明（kind/label/owner）+ 运行钩子（cancel/done/readOutput）；
//     runtime 拥有身份、访问与生命周期状态；
//   - owner 绑定（M46）：owner Agent 以 sessionId 上网，Agent dispose → 该 owner 名下
//     全部 Jobs 立刻 cancel hook（子进程树终止），孤儿进程彻底清理；
//   - JobSnapshot 是只读投影，供工具/监听消费；ownerSession 用于授权与关联；
//   - readOutput 是增量消费游标（每个 job 一个 consuming cursor）。
package jobs

import (
	"fmt"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// ============================================================================
// 类型
// ============================================================================

// JobKind 是生产者自定义的任务类型；registry 将每种视作不透明 id 命名空间。
type JobKind string

// 内置任务类型。
const (
	KindBash     JobKind = "bash"
	KindSubagent JobKind = "subagent"
)

// JobStatus 是任务生命周期状态。
type JobStatus string

// 状态枚举。
const (
	StatusRunning   JobStatus = "running"
	StatusStopping  JobStatus = "stopping"
	StatusCompleted JobStatus = "completed"
	StatusKilled    JobStatus = "killed"
	StatusFailed    JobStatus = "failed"
)

// JobOutcome 是生产者通过 done 上报的终止结果。
type JobOutcome struct {
	// Status 结束时状态：completed / killed / failed。
	Status JobStatus `json:"status"`
	// Detail 类型相关细节（如 "exit code: 3"）。
	Detail string `json:"detail,omitempty"`
	// Output 无 readOutput 任务的最终输出；流式任务留空。
	Output string `json:"output,omitempty"`
}

// JobSnapshot 是单任务只读投影。
type JobSnapshot struct {
	// ID registry 下发的 id（kind-N）。
	ID brand.JobID `json:"id"`
	// Kind 生产者类型。
	Kind JobKind `json:"kind"`
	// Label 一行模型向标签。
	Label string `json:"label"`
	// OwnerSession 用于授权与关联的 owner 会话 id；非空表示有主。
	OwnerSession brand.SessionID `json:"ownerSession,omitempty"`
	// Status 生命周期状态。
	Status JobStatus `json:"status"`
	// Detail 状态细节。
	Detail string `json:"detail,omitempty"`
	// Output 当前可读输出（消耗当前游标之前的内容）。
	Output string `json:"output,omitempty"`
}

// JobHooks 是 runtime 控制和观察生产者工作的钩子。
type JobHooks struct {
	// Cancel 请求终止；必须同步、幂等、最终结算 done。reason 将透传。
	Cancel func(reason string)
	// Done 在生产者在释放资源后结算（而非仅工作结束）。
	Done chan JobOutcome
	// ReadOutput 消费上次调用以来的输出；返回截断/溢出说明由生产者格式化。
	ReadOutput func() string
}

// JobStart 是生产者声明。
type JobStart struct {
	// Kind 类型（也是 id 前缀）。
	Kind JobKind
	// Label 一行模型向标签。
	Label string
	// OwnerSession owner 会话 id（用于授权与 dispose 清理）；零值表示无主任务。
	OwnerSession brand.SessionID
	// Run 在 preflight 后同步返回钩子；仅调用一次。返回 nil 表示启动失败且不留注册。
	Run func() *JobHooks
}

// ============================================================================
// Job 与 Runtime
// ============================================================================

// Job 是单个后台任务。
type Job struct {
	// ID 唯一任务 id。
	ID brand.JobID
	// Kind 类型。
	Kind JobKind
	// Label 标签。
	Label string
	// OwnerSession owner 会话 id。
	OwnerSession brand.SessionID

	mu     sync.Mutex
	status JobStatus
	detail string
	cancel func(reason string)
	done   chan JobOutcome
	read   func() string

	// finished 在完成结算且从 registry 移除后关闭（供 owner dispose 等待清理完成）。
	finished chan struct{}
}

// Snapshot 返回只读投影。
func (j *Job) Snapshot() JobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	output := ""
	if j.read != nil {
		output = j.read()
	}
	return JobSnapshot{
		ID:           j.ID,
		Kind:         j.Kind,
		Label:        j.Label,
		OwnerSession: j.OwnerSession,
		Status:       j.status,
		Detail:       j.detail,
		Output:       output,
	}
}

// ReadOutput 消费增量输出。
func (j *Job) ReadOutput() string {
	if j.read == nil {
		return ""
	}
	return j.read()
}

// Cancel 请求终止（幂等）。
func (j *Job) Cancel(reason string) {
	j.mu.Lock()
	done := j.cancel
	j.mu.Unlock()
	if done != nil {
		done(reason)
	}
}

// Done 返回任务完成通知。
func (j *Job) Done() <-chan JobOutcome { return j.done }

// resolveOutcome 结算任务最终状态。
func (j *Job) resolveOutcome(o JobOutcome) {
	j.mu.Lock()
	j.status = o.Status
	j.detail = o.Detail
	if o.Output != "" {
		// 无 readOutput 的任务最终输出。
		j.read = func() string { return o.Output }
	}
	j.mu.Unlock()
	select {
	case j.done <- o:
	default:
	}
}

// Runtime 是后台任务运行时（ctx.jobs）。
type Runtime struct {
	mu     sync.Mutex
	seq    int
	jobs   map[int]*Job
	byKind map[string]int // kmd-N
}

// NewRuntime 创建任务运行时。
func NewRuntime() *Runtime {
	return &Runtime{jobs: map[int]*Job{}, byKind: map[string]int{}}
}

// Start 启动一个任务。
func (r *Runtime) Start(start JobStart) (*Job, error) {
	if start.Run == nil {
		return nil, fmt.Errorf("jobs: Run hook required")
	}
	hooks := start.Run()
	if hooks == nil {
		return nil, fmt.Errorf("jobs: producer refused to start")
	}
	r.mu.Lock()
	r.seq++
	r.byKind[string(start.Kind)]++
	n := r.byKind[string(start.Kind)]
	id := brand.NewJobID(fmt.Sprintf("%s-%d", start.Kind, n))
	job := &Job{
		ID:           id,
		Kind:         start.Kind,
		Label:        start.Label,
		OwnerSession: start.OwnerSession,
		status:       StatusRunning,
		cancel:       hooks.Cancel,
		done:         hooks.Done,
		read:         hooks.ReadOutput,
		finished:     make(chan struct{}),
	}
	r.jobs[r.seq] = job
	r.mu.Unlock()

	// 异步结算：把钩子 done 映射到 job 状态并回收注册。
	go func(j *Job, h *JobHooks) {
		select {
		case o := <-h.Done:
			j.resolveOutcome(o)
		}
		r.unregister(j.ID)
		close(j.finished)
	}(job, hooks)
	return job, nil
}

func (r *Runtime) unregister(id brand.JobID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, j := range r.jobs {
		if j.ID == id {
			delete(r.jobs, k)
		}
	}
}

// JobByID 按 id 查任务。
func (r *Runtime) JobByID(id brand.JobID) *Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, j := range r.jobs {
		if j.ID == id {
			return j
		}
	}
	return nil
}

// ListByOwner 列出某 owner 下的全部任务（M46/S11）。
func (r *Runtime) ListByOwner(owner brand.SessionID) []*Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Job
	for _, j := range r.jobs {
		if j.OwnerSession == owner {
			out = append(out, j)
		}
	}
	return out
}

// All 返回全部任务。
func (r *Runtime) All() []*Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Job, 0, len(r.jobs))
	for _, j := range r.jobs {
		out = append(out, j)
	}
	return out
}

// DisposeOwner 在 owner Agent dispose 时调用：取消其名下全部任务并等待清理完成（M46）。
// 孤儿进程由各任务的 cancel hook（子进程树终止）彻底清理；等到 finished 保证已从
// 注册表移除、进程资源已释放。
func (r *Runtime) DisposeOwner(owner brand.SessionID, reason string) {
	for _, j := range r.ListByOwner(owner) {
		j.Cancel(reason)
		<-j.finished // 等待任务终止并完成清理
	}
}

// Finished 返回任务清理完成通知（外部等待用）。
func (j *Job) Finished() <-chan struct{} { return j.finished }