// Package tests 的后台任务运行时（S11 + M46）验收测试。
//
// 覆盖：
//   - S11：任务启动 + 增量输出读取 + 完成结算 + cancel
//   - M46：owner 绑定，Agent dispose → 名下全部 Jobs 立刻 cancel（孤儿进程清理）
//   - 手动钩子任务与子进程后端任务
package tests

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/jobs"
)

// TestJobsIncrementalOutput 验证任务增量输出读取（S11）。
func TestJobsIncrementalOutput(t *testing.T) {
	var argv []string
	if runtime.GOOS == "windows" {
		argv = []string{"powershell.exe", "-NoProfile", "-Command", "1..10 | ForEach-Object { Write-Output $_ ; Start-Sleep -Milliseconds 100 }"}
	} else {
		argv = []string{"sh", "-c", "for i in 1 2 3 4 5 6 7 8 9 10; do echo $i; sleep 0.1; done"}
	}
	rt := jobs.NewRuntime()
	job, err := rt.StartSubprocess(jobs.SubprocessSpec{
		Kind:           jobs.KindBash,
		Label:          "seq 1 10",
		OwnerSession:   brand.NewSessionID("owner1"),
		Argv:           argv,
		Cwd:            t.TempDir(),
		StdoutMaxBytes: 65536,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for i := 0; i < 50; i++ {
		if t.Failed() {
			break
		}
		got.WriteString(job.ReadOutput())
		select {
		case out := <-job.Done():
			got.WriteString(job.ReadOutput())
			if out.Status != jobs.StatusCompleted {
				t.Fatalf("应 completed, 实际 %+v", out)
			}
		default:
		}
		if strings.Contains(got.String(), "10") {
			break
		}
		<-time.After(40 * time.Millisecond)
	}
	if !strings.Contains(got.String(), "10") {
		t.Fatalf("应能读到最终输出 10, 实际 %q", got.String())
	}
	if job.ID == (brand.JobID{}) {
		t.Fatal("应有任务 id")
	}
}

// TestJobsListByOwner 验证按 owner 列出任务。
func TestJobsListByOwner(t *testing.T) {
	rt := jobs.NewRuntime()
	for i := 0; i < 3; i++ {
		_, _ = rt.Start(manualStart(jobs.KindBash, "ownerX", func() {}))
	}
	_, _ = rt.Start(manualStart(jobs.KindBash, "ownerY", func() {}))
	if got := len(rt.ListByOwner(brand.NewSessionID("ownerX"))); got != 3 {
		t.Fatalf("ownerX 应有 3 个任务, 实际 %d", got)
	}
}

// TestJobsOwnerDisposeCancels 验证 M46：owner dispose → 名下全部 Jobs 立刻 cancel。
func TestJobsOwnerDisposeCancels(t *testing.T) {
	owner := brand.NewSessionID("owner-z")
	rt := jobs.NewRuntime()
	cancelled := make(chan struct{}, 2)

	// 两个常驻任务（cancel hook 计数）。
	_, _ = rt.Start(manualStart(jobs.KindBash, owner.Raw(), func() { cancelled <- struct{}{} }))
	_, _ = rt.Start(manualStart(jobs.KindBash, owner.Raw(), func() { cancelled <- struct{}{} }))

	// dispose owner → 全部 cancel 且等待 done。
	done := make(chan struct{})
	go func() {
		rt.DisposeOwner(owner, "owner disposed")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("DisposeOwner 未在超时内完成")
	}
	// 两个任务都收到 cancel。
	count := 0
	for i := 0; i < 2; i++ {
		select {
		case <-cancelled:
			count++
		case <-time.After(time.Second):
		}
	}
	if count != 2 {
		t.Fatalf("dispose 应 cancel 全部 2 个任务, 实际 %d", count)
	}
	// 完成后任务从 registry 移除。
	if jobs := rt.ListByOwner(owner); len(jobs) != 0 {
		t.Fatalf("dispose 后 owner 名下列表应为空, 实际 %d", len(jobs))
	}
}

// TestJobsOwnerDisposeSubprocessCleanup 验证 dispose 时子进程任务被树终止清理（M46 孤儿清理）。
func TestJobsOwnerDisposeSubprocessCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("依赖 sh")
	}
	owner := brand.NewSessionID("owner-sp")
	rt := jobs.NewRuntime()
	_, err := rt.StartSubprocess(jobs.SubprocessSpec{
		Kind:         jobs.KindBash,
		Label:        "sleep long",
		OwnerSession: owner,
		Argv:         []string{"sh", "-c", "sleep 1000"},
		Cwd:          t.TempDir(),
		GraceMs:      100,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { rt.DisposeOwner(owner, "dispose"); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("dispose 子进程任务超时")
	}
	if len(rt.ListByOwner(owner)) != 0 {
		t.Fatal("dispose 后子进程任务应从 registry 移除")
	}
}

// manualStart 构造一个带手动 cancel 钩子的任务启动函数。
func manualStart(kind jobs.JobKind, ownerID string, onCancel func()) jobs.JobStart {
	owner := brand.NewSessionID(ownerID)
	done := make(chan jobs.JobOutcome, 1)
	return jobs.JobStart{
		Kind:         kind,
		Label:        "manual",
		OwnerSession: owner,
		Run: func() *jobs.JobHooks {
			return &jobs.JobHooks{
				// Cancel：先回调计数，再结算 killed，使 DisposeOwner 的 <-Done() 能返回。
				Cancel: func(reason string) {
					onCancel()
					select {
					case done <- jobs.JobOutcome{Status: jobs.StatusKilled, Detail: "cancelled"}:
					default:
					}
				},
				Done:        done,
				ReadOutput: func() string { return "" },
			}
		},
	}
}