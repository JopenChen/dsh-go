// 本文件对应任务 H06：Tool Pipeline 对象池（ExecContext / Meta map 热路径结构用
// sync.Pool 回收复用）。
//
// 设计原则：
//   - 仅在【显式开启 pooled 模式】（Pipeline.SetPooled(true)）后生效，默认 Run 行为不变；
//   - pool 只回收 ExecContext 及其内部的 Meta map（每次 Run 都会 new 一个 map，是最大
//     分配热点）；
//   - **ToolCallResult 不禁池**：Run 的返回 `*ToolCallResult` 会交由调用方持有，若从池中
//     取出复用，下一次 Run 会改写调用方正在引用的上一次结果 → 数据竞争/语义错误。
//     因此 pooled 模式跑完后把 Result 字段【拷贝到新对象】再返回（安全，调用方可自由持有）；
//   - 复用安全：Get 后必做的字段清零（Result/Signal/Denied/Meta/Ctx），杜绝脏复用。
package tools

import (
	"context"
	"sync"
)

// execContextPool H06：按需复用 ExecContext（含 Meta 空 map）。
var execContextPool = sync.Pool{
	New: func() any { return &ExecContext{Meta: map[string]any{}} },
}

// clearMeta 复用既有 map（删除全部键，保留底层 bucket，避免下次增长分配）。
func clearMeta(m map[string]any) {
	for k := range m {
		delete(m, k)
	}
}

// RunPooled 使用对象池路径执行一次工具调用，返回结果。
// 语义与 Run 完全一致，仅内部复用 ExecContext / Meta map，减少 GC 压力。
// Result 为新建对象，调用方可安全持有/修改。
func (p *Pipeline) RunPooled(ctx context.Context, req *ToolCallRequest, tool *Tool) *ToolCallResult {
	// 从池中取 ExecContext，并做完整字段复位（防脏复用）。
	ec := execContextPool.Get().(*ExecContext)
	ec.Request = req
	ec.Signal = SignalContinue
	ec.Denied = false
	ec.Ctx = ctx
	if ec.Result == nil {
		ec.Result = &ToolCallResult{}
	}
	if ec.Meta == nil {
		ec.Meta = map[string]any{}
	} else {
		clearMeta(ec.Meta)
	}
	// 分离的 Result 指针：直接复用 ec.Result，最后拷贝收尾。
	inner := ec.Result
	inner.CallID = req.CallID
	inner.IsError = false
	inner.Value = nil
	inner.Error = ""

	// 阶段 1：pre-execute
	_ = p.pre.Run(ctx, ec)
	if ec.Denied {
		inner.IsError = true
		inner.Error = "denied by pre-execute middleware"
		out := copyResult(inner)
		releaseExecContext(ec)
		return out
	}

	// 阶段 2：execute
	_ = p.execute.Run(ctx, ec)
	if ec.Signal == SignalCancel {
		inner.IsError = true
	}

	// 阶段 3：post-execute
	_ = p.post.Run(ctx, ec)
	if ec.Denied {
		inner.IsError = true
		if inner.Error == "" {
			inner.Error = "blocked by post-execute middleware"
		}
	}

	// 阶段 4：result
	_ = p.result.Run(ctx, ec)

	out := copyResult(inner)
	releaseExecContext(ec)
	return out
}

// copyResult 把池中 ExecContext 的结果拷贝到新对象并返回（调用方可安全持有）。
func copyResult(r *ToolCallResult) *ToolCallResult {
	if r == nil {
		return nil
	}
	return &ToolCallResult{
		CallID:  r.CallID,
		IsError: r.IsError,
		Value:   r.Value,
		Error:   r.Error,
	}
}

// releaseExecContext 归还 ExecContext 到池中。
func releaseExecContext(ec *ExecContext) {
	if ec == nil {
		return
	}
	// 归还前解引用，避免池中对象残留 Request/Result/Ctx 等大引用导致内存悬垂。
	ec.Request = nil
	ec.Result = nil
	ec.Ctx = nil
	execContextPool.Put(ec)
}

// PooledState 返回当前 pooled 模式状态（观测/测试用）。
func (p *Pipeline) PooledState() bool {
	if p == nil {
		return false
	}
	return p.pooled
}