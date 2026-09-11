// 本文件复刻官方 packages/core/tools/src/ptc.ts 的核心桥接语义（非完整并发调度器）。
//
// PTC（Programmatic Tool Calling）是官方四种运行模式之一：模型不再逐个发出工具调用，
// 而是写一段程序（async 函数体），程序通过 `tools.name(args)` 组合多步调用。
//
// 核心链路：
//   1. run_code 工具入参 code（程序）+ description（摘要）；
//   2. 把当前工具集映射为一个 coderuntime 绑定命名空间 "tools"，每个工具成为一个
//      BindingFunction——函数内部走本项目的工具 Pipeline（因此权限/沙箱/单调守卫
//      对程序内调用同样生效）；
//   3. 委托 coderuntime.Runtime 执行程序，得到 logs + 完成值；
//   4. 整理为模型可见文本（logs 与 return 值拼接），只有此外层精选结果进入模型历史。
//
// 取舍：上游 createRunCodeTool 内含复杂的 parallel/exclusive 并发调度器，深度耦合
// ToolRuntime 分阶段接口。Go 侧采用同步桥接：程序内每次工具调用在对应 binding 内
// 同步走完流水线；需要并发时由具体 CodeRuntime 后端在其执行模型内安排。
package tools

import (
	"context"
	"fmt"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/coderuntime"
)

// RunCodeName 是 PTC 模式工具对模型暴露的名字。
const RunCodeName = "run_code"

// RunCodeInput 是 run_code 的入参。
type RunCodeInput struct {
	// Code 程序：async 函数体（顶层 await/return 可用）。
	Code string `json:"code"`
	// Description 程序作用的简短摘要（主动语态）。
	Description string `json:"description"`
}

// ToolLookup 按名查找已注册工具（桥接层用它把工具集暴露给程序）。
type ToolLookup func(name string) (*Tool, bool)

// NewRunCodeTool 构造 PTC 的 run_code 桥接工具。
//
//   - runtime：代码执行后端（实现 coderuntime.Runtime，由使用方注入）；
//   - lookup：按名查找工具；
//   - pipeline：每次程序内工具调用所走的流水线（nil 时用仅含工具实现的最小链）。
func NewRunCodeTool(runtime coderuntime.Runtime, lookup ToolLookup, pipeline *Pipeline) *Tool {
	if lookup == nil {
		lookup = func(string) (*Tool, bool) { return nil, false }
	}
	execute := func(ctx context.Context, input map[string]any) (any, error) {
		var in RunCodeInput
		in.Code, _ = input["code"].(string)
		in.Description, _ = input["description"].(string)
		if in.Code == "" {
			return nil, fmt.Errorf("run_code: empty code")
		}
		if in.Description == "" {
			return nil, fmt.Errorf("run_code: empty description")
		}

		// 把工具集映射为 "tools" 绑定命名空间。
		ns := coderuntime.BindingNamespace{
			Global:     "tools",
			ErrorClass: "ToolError",
			Functions: map[string]coderuntime.BindingFunction{
				// 通用入口：程序以 tools.<name>(args) 调用，名字经 binding 闭包外无法静态枚举，
				// 因此注入一个以成员名为线索的桥——由 runtime 的属性访问桥接转成 CallMember 调用。
			},
		}
		// 动态成员访问：用一个 Dispatch 绑定承载"按成员名调用"，具体后端把
		// tools.<name>(args) 映射为对 Dispatch 的调用并附带 name。
		ns.Functions["__dispatch__"] = func(c context.Context, args any) (any, error) {
			m, ok := args.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid dispatch args")
			}
			name, _ := m["name"].(string)
			toolArgs, _ := m["arguments"].(map[string]any)
			return invokeToolInProgram(c, lookup, pipeline, name, toolArgs)
		}

		res, err := runtime.Run(&coderuntime.RunRequest{
			Program:  in.Code,
			Bindings: []coderuntime.BindingNamespace{ns},
			Ctx:      ctx,
		})
		if err != nil {
			return nil, fmt.Errorf("run_code runtime error: %w", err)
		}
		if res.Failed() {
			// 程序失败：回喂失败类别与信息，让模型自我纠正。
			return nil, fmt.Errorf("run_code %s: %s", res.Failure.Kind, res.Failure.Message)
		}
		return PresentRunCodeResult(res), nil
	}

	return &Tool{
		Name:        RunCodeName,
		Description: "Execute a program against the available tools; body of an async function with top-level await/return.",
		Execute:     execute,
	}
}

// invokeToolInProgram 执行程序内的一次工具调用：走流水线（权限/沙箱/守卫同样生效）。
func invokeToolInProgram(ctx context.Context, lookup ToolLookup, pipeline *Pipeline, name string, args map[string]any) (any, error) {
	tool, ok := lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	req := &ToolCallRequest{
		CallID: brand.NewToolCallID(fmt.Sprintf("ptc-%s", name)),
		Tool:   name,
		Input:  args,
	}
	if pipeline == nil {
		pipeline = NewPipeline().WithTool(tool)
	} else {
		// 不污染调用方共享流水线：复制一条并挂上该工具实现。
		pipeline = pipeline.WithTool(tool)
	}
	result := pipeline.Run(ctx, req, tool)
	if result.IsError {
		return nil, fmt.Errorf("%s", result.Error)
	}
	return result.Value, nil
}

// RunCodeOutput 是 run_code 的结构化输出。
type RunCodeOutput struct {
	Logs   []string `json:"logs"`
	Result any      `json:"result,omitempty"`
}

// PresentRunCodeResult 把运行结果整理为模型可见文本：logs 按序拼接，再附完成值。
func PresentRunCodeResult(res *coderuntime.RunResult) RunCodeOutput {
	return RunCodeOutput{Logs: res.Logs, Result: res.Value}
}
