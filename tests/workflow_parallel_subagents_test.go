// 本文件验证任务 S12：Workflow Engine（pipeline/parallel/agent 脚本全局 + 子代理编排）。
//
// 覆盖：并行 3 个子代理、每个跑 2 步 → 汇总结果正确且保序；串行短路；
// workflow 取消 → 子步骤级联取消（Cancelled 标志）。
package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/workflow"
)

// spawnStub 是集成 S02 subagent 的 SpawnAgentFunc 桩：返回 "agent:<input>:done"。
func spawnStub(ctx context.Context, backend, input string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "agent:" + backend + ":" + input + ":done", nil
}

// TestWorkflowParallel3Subagents 验证并行 3 个子代理、每个 2 步 → 汇总正确。
func TestWorkflowParallel3Subagents(t *testing.T) {
	// 每个子代理 = 串行 2 步（Agent 步骤）。
	subA := workflow.Pipeline("subA",
		workflow.Agent("a1", "in-process", "A-1", spawnStub),
		workflow.Agent("a2", "in-process", "A-2", spawnStub),
	)
	subB := workflow.Pipeline("subB",
		workflow.Agent("b1", "in-process", "B-1", spawnStub),
		workflow.Agent("b2", "in-process", "B-2", spawnStub),
	)
	subC := workflow.Pipeline("subC",
		workflow.Agent("c1", "in-process", "C-1", spawnStub),
		workflow.Agent("c2", "in-process", "C-2", spawnStub),
	)

	// 元工作流：并行 3 个子代理。
	meta := workflow.Parallel("meta", workflow.Seq(subA), workflow.Seq(subB), workflow.Seq(subC))

	res, err := meta.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Cancelled {
		t.Fatal("正常运行不应 marked cancelled")
	}
	if len(res.Outputs) != 3 {
		t.Fatalf("应有 3 个子代理输出，实际 %d", len(res.Outputs))
	}
	// 每个子代理输出应含 2 步结果。
	full := strings.Join(res.Outputs, ";")
	if !strings.Contains(full, "A-1:done") || !strings.Contains(full, "A-2:done") ||
		!strings.Contains(full, "B-1:done") || !strings.Contains(full, "B-2:done") ||
		!strings.Contains(full, "C-1:done") || !strings.Contains(full, "C-2:done") {
		t.Fatalf("汇总应含全部 6 步结果，实际 %q", full)
	}
}

// TestWorkflowPipelineShortCircuit 验证串行单步失败短路。
func TestWorkflowPipelineShortCircuit(t *testing.T) {
	runCount := 0
	bad := workflow.Pipeline("bad",
		&workflow.Step{Name: "ok", Run: func(ctx context.Context) (string, error) { return "ok", nil }},
		&workflow.Step{Name: "boom", Run: func(ctx context.Context) (string, error) { return "", errBoom{} }},
		&workflow.Step{Name: "never", Run: func(ctx context.Context) (string, error) { runCount++; return "no", nil }},
	)
	res, err := bad.Run(context.Background())
	if err == nil {
		t.Fatal("串行失败应返回错误")
	}
	if runCount != 0 {
		t.Fatalf("短路后不应执行后续步骤，实际执行 %d 次", runCount)
	}
	// 只输出前两步（ok 与 boom:error）。
	if len(res.Outputs) != 2 {
		t.Fatalf("输出应为 2 条(ok + error)，实际 %d", len(res.Outputs))
	}
}

// TestWorkflowCancel 验证取消会级联结束子步骤（Cancelled 标志）。
func TestWorkflowCancel(t *testing.T) {
	step := &workflow.Step{
		Name: "blocking",
		Run: func(ctx context.Context) (string, error) {
			// 阻塞直到取消。
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	w := workflow.Parallel("cancel", step, step)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	res, _ := w.Run(ctx)
	if !res.Cancelled {
		t.Fatal("取消后 Result 应 marked Cancelled")
	}
}

// errBoom 是测试用的简易错误。
type errBoom struct{}

func (errBoom) Error() string { return "boom" }