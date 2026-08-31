// 教程：工作流引擎（Workflow）——串行 / 并行编排（教学示例）。
//
// Agent 常需把多个步骤组合起来：要么一步步串着做，要么几个独立步骤并行跑。
// 本项目提供三个编排原语（顶层函数）：
//   workflow.Pipeline(...)   串行：按顺序逐步执行，某步失败即短路中止
//   workflow.Parallel(...)   并行：各步并发执行，最终保序汇总
//   workflow.Agent(...)      把一个"子代理调用"包装成一个步骤
//
// 本示例用纯函数步骤演示 Pipeline 与 Parallel，再用 workflow.Agent 演示
// 如何把子代理编排成一步，最后演示 context 取消如何级联终止。
//
// 运行方式（仓库根目录）：
//   go run ./examples/workflow
//
// 对照阅读：pkg/workflow/workflow.go（Pipeline / Parallel / Agent / Run）
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/JopenChen/dsh-go/pkg/workflow"
)

func main() {
	ctx := context.Background()

	// ------------------------------------------------------------------
	// 1. 串行工作流（Pipeline）：三步按顺序执行。
	//    每步返回一段文本；Result.Outputs 保序保存。
	// ------------------------------------------------------------------
	pipe := workflow.Pipeline("prepare",
		step("拉取代码", 50*time.Millisecond, "repo fetched"),
		step("构建", 60*time.Millisecond, "build ok"),
		step("跑测试", 30*time.Millisecond, "tests pass"),
	)
	res, err := pipe.Run(ctx)
	if err != nil {
		fmt.Printf("pipeline 失败: %v\n", err)
	} else {
		fmt.Println("— 串行 Pipeline 输出（按序）—")
		for _, o := range res.Outputs {
			fmt.Printf("  · %s\n", o)
		}
	}

	// ------------------------------------------------------------------
	// 2. 并行工作流（Parallel）：三个独立步骤并发执行。
	//    总耗时约等于最慢的一步（而非三步之和），最后仍保序汇总。
	// ------------------------------------------------------------------
	par := workflow.Parallel("fetch-3",
		step("请求A", 120*time.Millisecond, "data A"),
		step("请求B", 90*time.Millisecond, "data B"),
		step("请求C", 150*time.Millisecond, "data C"),
	)
	start := time.Now()
	pres, _ := par.Run(ctx)
	fmt.Printf("\n— 并行 Parallel 输出（并发，总耗时≈最慢一步）—\n")
	for _, o := range pres.Outputs {
		fmt.Printf("  · %s\n", o)
	}
	fmt.Printf("  (并行耗时: %v)\n", time.Since(start).Round(time.Millisecond))

	// ------------------------------------------------------------------
	// 3. 用 workflow.Agent 把"子代理调用"包装成工作流的一步。
	//    真实场景 SpawnAgentFunc 会去 run 一个子代理（in-process/ACP/fork-copy）；
	//    这里用闭包模拟子代理的输入→输出映射，便于看清编排形状。
	// ------------------------------------------------------------------
	agentFlow := workflow.Pipeline("with-subagent",
		step("准备输入", 20*time.Millisecond, "input-ready"),
		workflow.Agent("审阅", "in-process", "请审阅代码", func(_ context.Context, _ string, input string) (string, error) {
			// 模拟子代理输出
			return "subagent 审阅完成: " + input, nil
		}),
	)
	ares, _ := agentFlow.Run(ctx)
	fmt.Printf("\n— 含子代理步骤的工作流 —\n")
	for _, o := range ares.Outputs {
		fmt.Printf("  · %s\n", o)
	}

	// ------------------------------------------------------------------
	// 4. 教学点：context 取消会级联终止正在运行的工作流。
	//    这里用一个较长时间的任务：运行中途主动 cancel，
	//    并行模式会等所有 goroutine 退出并标记 Cancelled=true。
	// ------------------------------------------------------------------
	ctx2, cancel := context.WithCancel(context.Background())
	cancelAfter := workflow.Parallel("cancel-demo",
		step("短任务", 40*time.Millisecond, "short done"),
		step("长任务", 800*time.Millisecond, "long done"), // 运行中被取消
	)
	go func() { time.Sleep(60 * time.Millisecond); cancel() }()
	cres, cerr := cancelAfter.Run(ctx2)
	// 短任务完成；长任务因 ctx 取消被中断，但 Parallel 仍返回结构并标 Cancelled。
	fmt.Printf("\n— ctx 取消级联演示 —\n  cancelled=%v, cnt=%d, err=%v\n",
		cres.Cancelled, len(cres.Outputs), cerr)
}

// step 构造一个带名称与耗时的步骤，返回一段文本。
func step(name string, dur time.Duration, out string) *workflow.Step {
	return &workflow.Step{
		Name: name,
		Run: func(c context.Context) (string, error) {
			select {
			case <-time.After(dur):
				return out, nil
			case <-c.Done():
				return "", c.Err()
			}
		},
	}
}