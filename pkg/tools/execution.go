// 本文件对应任务 M25：ToolRunContext deferContext + concludeTurn。
//
// 对齐上游：packages/core/tools/types ToolRunContext
//
// 设计要点：
//   - ToolRunContext 提供工具执行期间的运行时上下文（Meta + defer 回调注册）；
//   - concludeTurn 是终止当前 turn 的权威标记：被 goal report_blocker 等调用后，
//     Agent 循环在下一条是否需要继续时据此结束 turn；
//   - 通过上下文传播：pipeline 内的工具实现可调用 Agent 级 conclude 控制。
package tools

import (
	"context"
	"sync"
)

// concludeKey 是 context 中 conclude 回调的键。
type concludeKey struct{}

// ConcludeFunc 是结束当前 turn 的回调（由 Agent 注册）。
type ConcludeFunc func(reason string)

// WithConclude 将 conclude 回调注入 context。
func WithConclude(ctx context.Context, fn ConcludeFunc) context.Context {
	return context.WithValue(ctx, concludeKey{}, fn)
}

// ConcludeFrom 从 ctx 取 conclude 回调；不存在返回 nil。
func ConcludeFrom(ctx context.Context) ConcludeFunc {
	if fn, ok := ctx.Value(concludeKey{}).(ConcludeFunc); ok {
		return fn
	}
	return nil
}

// ToolRunContext 是工具执行期间的运行时上下文。
type ToolRunContext struct {
	mu        sync.Mutex
	meta      map[string]any
	deferred  []func()
	concluded bool
	conclude  ConcludeFunc
}

// NewToolRunContext 创建工具运行上下文。
func NewToolRunContext(conclude ConcludeFunc) *ToolRunContext {
	return &ToolRunContext{meta: map[string]any{}, conclude: conclude}
}

// SetMeta 设置运行元数据。
func (tc *ToolRunContext) SetMeta(k string, v any) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.meta[k] = v
}

// GetMeta 读取运行元数据。
func (tc *ToolRunContext) GetMeta(k string) (any, bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	v, ok := tc.meta[k]
	return v, ok
}

// DeferContext 注册一个在工具结束时执行的清理/收尾回调（类似 Go defer）。
func (tc *ToolRunContext) DeferContext(fn func()) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.deferred = append(tc.deferred, fn)
}

// runDeferred 按 LIFO 执行全部 deferred 回调。
func (tc *ToolRunContext) runDeferred() {
	tc.mu.Lock()
	defers := make([]func(), len(tc.deferred))
	copy(defers, tc.deferred)
	tc.deferred = nil
	tc.mu.Unlock()
	for i := len(defers) - 1; i >= 0; i-- {
		if defers[i] != nil {
			defers[i]()
		}
	}
}

// ConcludeTurn 结束当前 turn（权威标记），并执行 deferred 回调。
// 多次调用不会重复触发 conclude。
func (tc *ToolRunContext) ConcludeTurn(reason string) bool {
	tc.mu.Lock()
	if tc.concluded {
		tc.mu.Unlock()
		return false
	}
	tc.concluded = true
	conclude := tc.conclude
	tc.mu.Unlock()

	tc.runDeferred()
	if conclude != nil {
		conclude(reason)
	}
	return true
}

// IsConcluded 是否已 conclude（Agent 判续步用）。
func (tc *ToolRunContext) IsConcluded() bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.concluded
}

// Context 返回携带该 ToolRunContext 的 context（供工具实现读取）。
func (tc *ToolRunContext) Context() context.Context {
	return withRunContext(context.Background(), tc)
}

// runContextKey 是 context 中 ToolRunContext 的键。
type runContextKey struct{}

// withRunContext 注入 ToolRunContext 到 context。
func withRunContext(ctx context.Context, tc *ToolRunContext) context.Context {
	return context.WithValue(ctx, runContextKey{}, tc)
}

// ToolRunContextFrom 从 ctx 取 ToolRunContext。
func ToolRunContextFrom(ctx context.Context) (*ToolRunContext, bool) {
	tc, ok := ctx.Value(runContextKey{}).(*ToolRunContext)
	return tc, ok
}