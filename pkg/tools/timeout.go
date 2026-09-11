// 本文件复刻官方 guard/timeout-policy：协作式工具调用超时。工具声明 TimeoutMs 并
// 承诺尊重 ctx；包装器武装截止时间，自己的计时器触发时把结果映射为结构化
// TOOL_TIMEOUT，而不是与工具的执行相互竞争或丢弃。
//
// Go 的固有限制：无法强杀 goroutine。超时后包装器立即返回 TOOL_TIMEOUT，被调用的
// goroutine 只有在尊重 ctx 时才会退出；不尊重则会继续在后台运行（可能泄漏）。因此
// 这是"协作式"超时——工具实现必须监听 ctx.Done()。
package tools

import (
	"context"
	"errors"
	"time"
)

// ToolTimeoutCode 是超时的结构化错误码（对齐官方 TOOL_TIMEOUT）。
const ToolTimeoutCode = "TOOL_TIMEOUT"

// ErrToolTimeout 表示工具调用超过自身声明的预算。
var ErrToolTimeout = errors.New("tool call timed out")

// TimeoutError 是超时的结构化错误，携带 TOOL_TIMEOUT 码供重试/沙箱层路由。
type TimeoutError struct {
	Code  string
	Budget time.Duration
	Cause error
}

// Error 实现 error。
func (e *TimeoutError) Error() string {
	return "tool call timed out after " + e.Budget.String()
}

// Unwrap 支持 errors.Is/As。
func (e *TimeoutError) Unwrap() error { return e.Cause }

// WrapTimeout 用 timeoutMs 包装一个执行函数；timeoutMs<=0 时原样返回。
func WrapTimeout(exec ToolExecuteFunc, timeoutMs int) ToolExecuteFunc {
	if exec == nil || timeoutMs <= 0 {
		return exec
	}
	budget := time.Duration(timeoutMs) * time.Millisecond
	return func(ctx context.Context, input map[string]any) (any, error) {
		timerCtx, cancel := context.WithTimeout(ctx, budget)
		defer cancel()

		type outcome struct {
			value any
			err   error
		}
		done := make(chan outcome, 1)
		go func() {
			v, e := exec(timerCtx, input)
			done <- outcome{v, e}
		}()

		select {
		case o := <-done:
			return o.value, o.err
		case <-timerCtx.Done():
			// 仅当是【本层】计时器到期才映射为 TOOL_TIMEOUT；上游 ctx 取消
			// （父 deadline 先触发）按普通取消处理。
			if errors.Is(timerCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
				return nil, &TimeoutError{Code: ToolTimeoutCode, Budget: budget, Cause: ErrToolTimeout}
			}
			return nil, ctx.Err()
		}
	}
}
