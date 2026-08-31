// 本文件对应任务 M23：Tool Execution 四级 Waterfall 链。
//
// 对齐上游：packages/core/tools
//
// 一次工具调用按四级流水线执行，每一级都是一条可插拔中间件链（复用 pkg/waterfall）：
//   - pre-execute：进入前的拦截/改写（deny 拒绝、换参、注入上下文）；
//   - execute：真正调用工具实现（可中途换 signal 取消）；
//   - post-execute：结果后处理（accept/block、截断、附加 meta）；
//   - result：最终结果加工（统一包装、打点）。
//
// 与上游语义对齐：pre-execute 返回 deny → 该调用不执行且结果标记 isError；
// execute 中换 signal=cancel → post-execute 可 block → 结果带上 isError。
package tools

import (
	"context"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/waterfall"
)

// Signal 是工具执行的运行信号。
type Signal string

// 运行信号枚举。
const (
	// SignalContinue 继续执行。
	SignalContinue Signal = "continue"
	// SignalCancel 取消执行（execute 级中间件可替换为 cancel）。
	SignalCancel Signal = "cancel"
)

// ToolExecuteFunc 是工具实现的执行函数。
type ToolExecuteFunc func(ctx context.Context, input map[string]any) (any, error)

// Tool 是工具定义（name/schema/execute）。
type Tool struct {
	// Name 工具名（供 LLM 调用）。
	Name string
	// Description 工具描述（进 prompt）。
	Description string
	// Schema 参数 JSON Schema（进 prompt + 入参校验）。
	Schema *JsonSchemaNode
	// Execute 具体实现。
	Execute ToolExecuteFunc
}

// ToolCallRequest 是一次工具调用请求。
type ToolCallRequest struct {
	// CallID 调用 ID（与 tool/call → tool/result 配对）。
	CallID brand.ToolCallID
	// Tool 工具名。
	Tool string
	// Input 解析后的参数。
	Input map[string]any
}

// ToolCallResult 是一次工具调用的结果。
type ToolCallResult struct {
	// CallID 与请求配对。
	CallID brand.ToolCallID
	// IsError 是否视为失败（deny/block/cancel 或工具返回错误）。
	IsError bool
	// Value 结构化结果值。
	Value any
	// Error 错误信息（IsError 时填充）。
	Error string
}

// ExecContext 是贯穿四级链的共享上下文（payload）。
type ExecContext struct {
	// Request 原始请求。
	Request *ToolCallRequest
	// Result 结果（由各阶段填充）。
	Result *ToolCallResult
	// Signal 运行信号（execute 级可替换）。
	Signal Signal
	// Denied 是否被 pre/post 级拒绝。
	Denied bool
	// Meta 附加元数据（post 级可写入，供展示/审计）。
	Meta map[string]any
	// Ctx 携带进入本次执行的父 ctx（H01：取消/超时/追踪 propagate 到工具实现）。
	Ctx context.Context
}

// Pipeline 是四级工具执行流水线。
type Pipeline struct {
	pre     *waterfall.Chain[ExecContext]
	execute *waterfall.Chain[ExecContext]
	post    *waterfall.Chain[ExecContext]
	result  *waterfall.Chain[ExecContext]
}

// NewPipeline 构建空流水线（各阶段均可追加中间件）。
func NewPipeline() *Pipeline {
	return &Pipeline{
		pre:     waterfall.New[ExecContext](),
		execute: waterfall.New[ExecContext](),
		post:    waterfall.New[ExecContext](),
		result:  waterfall.New[ExecContext](),
	}
}

// UsePre 追加 pre-execute 中间件（拦截/改写入参，可 deny）。
func (p *Pipeline) UsePre(h ...waterfall.Handler[ExecContext]) *Pipeline {
	p.pre.Use(h...)
	return p
}

// UseExecute 追加 execute 中间件（包裹工具实现，可换 signal）。
func (p *Pipeline) UseExecute(h ...waterfall.Handler[ExecContext]) *Pipeline {
	p.execute.Use(h...)
	return p
}

// UsePost 追加 post-execute 中间件（accept/block/截断/加 meta）。
func (p *Pipeline) UsePost(h ...waterfall.Handler[ExecContext]) *Pipeline {
	p.post.Use(h...)
	return p
}

// UseResult 追加 result 中间件（最终加工）。
func (p *Pipeline) UseResult(h ...waterfall.Handler[ExecContext]) *Pipeline {
	p.result.Use(h...)
	return p
}

// Run 执行一次工具调用，走完四级流水线，返回结果。
func (p *Pipeline) Run(ctx context.Context, req *ToolCallRequest, tool *Tool) *ToolCallResult {
	ec := &ExecContext{
		Request: req,
		Result:  &ToolCallResult{CallID: req.CallID},
		Signal:  SignalContinue,
		Denied:  false,
		Meta:    map[string]any{},
		Ctx:     ctx,
	}

	// 阶段 1：pre-execute（拦截/改写入参）
	_ = p.pre.Run(ctx, ec)
	if ec.Denied {
		ec.Result.IsError = true
		ec.Result.Error = "denied by pre-execute middleware"
		return ec.Result
	}

	// 阶段 2：execute（调用工具实现；execute 链的最内层是真正的执行）
	_ = p.execute.Run(ctx, ec)
	if ec.Signal == SignalCancel {
		// 取消信号：post 阶段会 block，这里先标记
		ec.Result.IsError = true
	}

	// 阶段 3：post-execute（accept/block/截断/加 meta）
	_ = p.post.Run(ctx, ec)
	if ec.Denied {
		ec.Result.IsError = true
		if ec.Result.Error == "" {
			ec.Result.Error = "blocked by post-execute middleware"
		}
	}

	// 阶段 4：result（最终加工）
	_ = p.result.Run(ctx, ec)
	return ec.Result
}

// executeInner 构造 execute 阶段最内层的真实工具调用中间件。
func executeInner(tool *Tool) waterfall.Handler[ExecContext] {
	return func(ec *ExecContext, next waterfall.NextFunc) error {
		if tool == nil || tool.Execute == nil {
			ec.Result.IsError = true
			ec.Result.Error = "tool implementation missing"
			return next()
		}
		// H01：把本次执行的父 ctx（含取消/超时/追踪）透传给工具实现，而非重建 Background。
		val, err := tool.Execute(withExecContext(ec.Ctx, ec), ec.Request.Input)
		if err != nil {
			ec.Result.IsError = true
			ec.Result.Error = err.Error()
		} else {
			ec.Result.Value = val
		}
		return next()
	}
}

// WithTool 便捷方法：创建一条包含真实工具实现 execute 阶段的流水线。
func (p *Pipeline) WithTool(tool *Tool) *Pipeline {
	p.execute.Use(executeInner(tool))
	return p
}

// contextWithMeta 将 ExecContext 注入 ctx（供工具实现读取 Meta/信号）。
func contextWithMeta(ec *ExecContext) context.Context {
	return withExecContext(context.Background(), ec)
}

// execContextKey 是 context 中 ExecContext 的键类型。
type execContextKey struct{}

// withExecContext 返回携带 ExecContext 的 context。
func withExecContext(ctx context.Context, ec *ExecContext) context.Context {
	return context.WithValue(ctx, execContextKey{}, ec)
}

// ExecContextFrom 从 ctx 中取出 ExecContext（工具实现内可用）。
func ExecContextFrom(ctx context.Context) (*ExecContext, bool) {
	ec, ok := ctx.Value(execContextKey{}).(*ExecContext)
	return ec, ok
}
