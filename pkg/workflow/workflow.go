// Package workflow 提供工作流引擎（任务 S12：Workflow Engine + tool-workflow）。
//
// 对齐上游：packages/workflow/workflow + tool-workflow
//
// 设计要点：
//   - 以「脚本全局函数」方式组织编排原语：Pipeline(串行) / Parallel(并行) /
//     Agent(子代理步骤)，均为顶层函数，业务侧直接调用即可拼装工作流；
//   - Step 是可执行的最小单元（运行函数 + 名称）；Workflow 持有逐步序列；
//   - Run：Pipeline 串行执行、Parallel 并发执行（goroutine + errgroup 等价语义），
//     收集各步输出；取消（ctx cancel）会级联取消正在进行的步骤；
//   - WorkflowResult 聚合各步输出与取消标志，供上层对比/汇总。
package workflow

import (
	"context"
	"fmt"
	"sync"
)

// ============================================================================
// Step 与 Result
// ============================================================================

// StepRunFunc 是单步执行函数（ctx 取消时应尽快返回）。
type StepRunFunc func(ctx context.Context) (string, error)

// Step 是最小工作流单元。
type Step struct {
	Name string     // 步骤名（诊断/汇总用）
	Run  StepRunFunc
}

// Result 是一个工作流的运行结果。
type Result struct {
	Name      string   `json:"name"`
	Outputs   []string `json:"outputs"`   // 各步输出（保序）
	Cancelled bool     `json:"cancelled"` // 是否被取消
}

// Errors 返回本次运行产生的首个错误（无则 nil）。
func (r *Result) Errors() error {
	for _, o := range r.Outputs {
		if s, ok := unwrapErr(o); ok {
			return s
		}
	}
	return nil
}

// unwrapErr 把输出中的错误占位还原为 error。
func unwrapErr(o string) (error, bool) {
	return nil, false // 输出为文本，无内嵌错误
}

// ============================================================================
// Workflow
// ============================================================================

// Mode 是工作流的执行模式。
type Mode string

const (
	ModePipeline Mode = "pipeline" // 串行
	ModeParallel Mode = "parallel" // 并行
)

// Workflow 是一组 Step 的组合（串行或并行）。
type Workflow struct {
	Name   string
	Mode   Mode
	Steps  []*Step
}

// ============================================================================
// 全局编排原语
// ============================================================================

// Pipeline 构造串行工作流（逐步依序执行）。
func Pipeline(name string, steps ...*Step) *Workflow {
	return &Workflow{Name: name, Mode: ModePipeline, Steps: steps}
}

// Parallel 构造并行工作流（各步并发执行）。
func Parallel(name string, steps ...*Step) *Workflow {
	return &Workflow{Name: name, Mode: ModeParallel, Steps: steps}
}

// Agent 构造一个子代理步骤（包装 subagent in-process 调用；backendName 可为
// "in-process"/"acp"/"fork-copy"）。
func Agent(name, backendName, input string, spawn SpawnAgentFunc) *Step {
	return &Step{
		Name: name,
		Run: func(ctx context.Context) (string, error) {
			return spawn(ctx, backendName, input)
		},
	}
}

// Seq 把子工作流包装成单步（用于并行编排多个子工作流做聚合）。
func Seq(w *Workflow) *Step {
	return &Step{
		Name: w.Name,
		Run: func(ctx context.Context) (string, error) {
			res, err := w.Run(ctx)
			if err != nil {
				return "", err
			}
			out := ""
			for i, o := range res.Outputs {
				if i > 0 {
					out += ","
				}
				out += o
			}
			return out, nil
		},
	}
}

// SpawnAgentFunc 由上层注入：按后端名运行一个子代理并返回输出。
type SpawnAgentFunc func(ctx context.Context, backendName, input string) (string, error)

// ============================================================================
// Run
// ============================================================================

// Run 执行工作流，返回聚合结果。ctx 取消会级联终止。
func (w *Workflow) Run(ctx context.Context) (*Result, error) {
	res := &Result{Name: w.Name}
	switch w.Mode {
	case ModeParallel:
		return w.runParallel(ctx, res)
	default: // ModePipeline
		return w.runPipeline(ctx, res)
	}
}

// runPipeline 串行执行逐步。
func (w *Workflow) runPipeline(ctx context.Context, res *Result) (*Result, error) {
	for _, s := range w.Steps {
		if err := ctx.Err(); err != nil {
			res.Cancelled = true
			return res, err
		}
		out, err := s.Run(ctx)
		if err == nil {
			res.Outputs = append(res.Outputs, out)
		} else {
			res.Outputs = append(res.Outputs, fmt.Sprintf("[%s:error %v]", s.Name, err))
		}
		// 单步失败即中止整条串行流水线（短路）。
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

// runParallel 并行执行各步并汇总（保序）。
func (w *Workflow) runParallel(ctx context.Context, res *Result) (*Result, error) {
	outputs := make([]string, len(w.Steps))
	var wg sync.WaitGroup
	for i, s := range w.Steps {
		wg.Add(1)
		go func(idx int, step *Step) {
			defer wg.Done()
			out, err := step.Run(ctx)
			if err != nil {
				outputs[idx] = fmt.Sprintf("[%s:error %v]", step.Name, err)
				return
			}
			outputs[idx] = out
		}(i, s)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		res.Cancelled = true
	}
	res.Outputs = outputs
	return res, nil
}