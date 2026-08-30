// Package waterfall 提供泛型中间件链（Waterfall）原语。
//
// 对齐上游：packages/core/waterfall
//
// 设计动机：
//   - dsh-go 的 agent/pre-step / agent/request / tools/pre-execute / tools/execute /
//     tools/post-execute / tools/result 以及 approval / plan / goal 注入等所有"多级拦截"场景
//     都遵循同一套洋葱模型：中间件按注册顺序依次进入，可改写 payload，可决定是否放行到下一级；
//   - 本包用一套泛型实现复用所有场景，避免为每种拦截点各写一套链式调用代码。
//
// 语义约定：
//   - Handler 收到共享 payload 指针与 next()；调用 next() 表示放行到下一级，
//     不调用 next() 即短路（链在此终止）；
//   - next() 返回下游错误，中间件可吞掉、改写或继续向上传播；
//   - next() 同一中间件内只允许调用一次（重复调用 panic，防止逻辑错误）；
//   - Handler 在调用 next() 之后仍可继续执行（洋葱后置处理），实现 pre/post 双层拦截。
package waterfall

import (
	"context"
	"fmt"
)

// NextFunc 指向链中下一级中间件的调用句柄。
// 返回值为下游处理结果（nil 表示下游通过）。
type NextFunc func() error

// Handler 单个中间件处理函数。
//   - payload：当前链的共享载荷指针，可被任意中间件读写并在各级间传递；
//   - next：放行到下一级；不调用即短路；
//   - 返回 error 非 nil 时，链立即终止并把该错误返回给 Run 调用方。
type Handler[T any] func(payload *T, next NextFunc) error

// Chain 是一条已排序的中间件链。
type Chain[T any] struct {
	handlers []Handler[T]
}

// New 构建一条中间件链，按给定顺序执行。
// 注意：空参数时也要保证内部 slice 非 nil，否则后续 Use() 会被误判为零值。
func New[T any](handlers ...Handler[T]) *Chain[T] {
	return &Chain[T]{handlers: append([]Handler[T]{}, handlers...)}
}

// Use 追加一个或多个中间件到链尾（构建期使用，运行期调用会 panic 以暴露误用）。
func (c *Chain[T]) Use(handlers ...Handler[T]) *Chain[T] {
	if c.handlers == nil {
		panic("waterfall: cannot Use() on a Chain built with zero value; use New()")
	}
	c.handlers = append(c.handlers, handlers...)
	return c
}

// Run 从链首开始执行整条链。
//   - 任一中间件返回 error → 立即终止并返回该错误；
//   - 任一中间件未调用 next() → 短路，返回 nil（或该中间件自己的返回值）。
func (c *Chain[T]) Run(ctx context.Context, payload *T) error {
	if ctx == nil {
		return fmt.Errorf("waterfall: nil context")
	}
	return c.runFrom(ctx, 0, payload)
}

// runFrom 从第 index 个中间件开始执行。
func (c *Chain[T]) runFrom(ctx context.Context, index int, payload *T) error {
	// 越界：链执行完毕
	if index >= len(c.handlers) {
		return nil
	}

	current := c.handlers[index]

	// 构造本级的 next()：指向下一级，且只允许调用一次
	called := false
	var nextErr error
	next := func() error {
		if called {
			panic("waterfall: next() called more than once in a single handler")
		}
		called = true
		nextErr = c.runFrom(ctx, index+1, payload)
		return nextErr
	}

	err := current(payload, next)
	if err != nil {
		return err
	}
	// 中间件返回 nil 但未调用 next() → 短路（链在此终止，视为通过）
	if !called {
		return nil
	}
	return nextErr
}

// RunSafe 同 Run，但将链执行过程中可能抛出的 panic 转为错误返回。
// 用于与外部回调（如用户注入的工具实现）交互的边界，避免单个 panic 拖垮整个 turn。
func (c *Chain[T]) RunSafe(ctx context.Context, payload *T) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("waterfall: panic in chain: %v", p)
		}
	}()
	return c.Run(ctx, payload)
}
