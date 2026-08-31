// 本文件对应任务 M34：agent/request-error 重试瀑布。
//
// 对齐上游：packages/core/agent-loop request-error waterfall
//
// 设计要点：
//   - RequestErrorAction{kind} 只有两种：'retry'（显式重试通道）与 'abort'（终止）；
//   - request-error waterfall 复用 M02 waterfall 洋葱链：各中间件按序判定动作，可短路；
//     未命中任何中间件时落到 TerminalRetryDecision 兜底；
//   - 与 S15（LLM Retry）协同：S15 在 LLM 层做一次退避重试并写 llm/retry 事件；若仍失败
//     （达 max 上抛），进入本 request-error 瀑布决定 retry 或 abort；
//   - 超过 max 次重试 → abort，并以 error 关闭当前 turn（写 agent/error 事件）。
package agent

import (
	"context"

	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/waterfall"
)

// RequestErrorActionKind 是 request-error 处理动作类型。
type RequestErrorActionKind string

// 动作枚举：仅 retry 与 abort 两种。
const (
	ActionRetry RequestErrorActionKind = "retry"
	ActionAbort RequestErrorActionKind = "abort"
)

// RequestErrorAction 是一次 request-error 的决策结果。
type RequestErrorAction struct {
	// Kind 动作类型。
	Kind RequestErrorActionKind `json:"kind"`
	// Reason 决策原因（审计）。
	Reason string `json:"reason,omitempty"`
}

// RequestErrorPayload 是 request-error 瀑布的共享载荷。
type RequestErrorPayload struct {
	// Err 触发请求错误的底层错误。
	Err error
	// RetryCount 已发生的重试次数。
	RetryCount int
	// MaxRetries 允许的最大重试次数。
	MaxRetries int
	// RetryableError 错误是否可重试（overload/rate-limit）。
	RetryableError bool
	// Action 由瀑布写入的最终动作。
	Action *RequestErrorAction
}

// ShouldRetryError 复用 llm 分类：仅 overload / rate-limit 可重试。
func ShouldRetryError(err error) bool {
	if err == nil {
		return false
	}
	f := llm.ClassifyLlmError(err)
	return f.Kind == llm.FailOverload || f.Kind == llm.FailRateLimit
}

// NewRequestErrorPayload 构造载荷并预计算可重试性。
func NewRequestErrorPayload(err error, retryCount, maxRetries int) *RequestErrorPayload {
	return &RequestErrorPayload{
		Err:            err,
		RetryCount:     retryCount,
		MaxRetries:     maxRetries,
		RetryableError: ShouldRetryError(err),
	}
}

// TerminalRetryDecision 是兜底终端决策：
// 错误可重试且未超过最大次数 → retry；否则 abort（并以 error 关闭 turn）。
func TerminalRetryDecision(p *RequestErrorPayload) RequestErrorAction {
	if p.RetryableError && p.RetryCount < p.MaxRetries {
		return RequestErrorAction{Kind: ActionRetry, Reason: "retryable llm failure within max retries"}
	}
	if p.RetryableError {
		return RequestErrorAction{Kind: ActionAbort, Reason: "max request-error retries exceeded"}
	}
	return RequestErrorAction{Kind: ActionAbort, Reason: "non-retryable request error"}
}

// RequestErrorWaterfall 是 request-error 处理瀑布（复用 M02 waterfall.Chain）。
// 中间件可通过写入 p.Action 并短路（不调用 next）终止链。
type RequestErrorWaterfall = waterfall.Chain[RequestErrorPayload]

// NewRequestErrorWaterfall 构建 request-error 瀑布链。
func NewRequestErrorWaterfall(handlers ...waterfall.Handler[RequestErrorPayload]) *RequestErrorWaterfall {
	return waterfall.New(handlers...)
}

// ResolveRequestError 运行 request-error 瀑布得到最终动作。
//
//   - 若某个中间件写入了 p.Action 则直接返回；
//   - 否则链放行到兜底：应用 TerminalRetryDecision。
func ResolveRequestError(chain *RequestErrorWaterfall, p *RequestErrorPayload) RequestErrorAction {
	if chain == nil {
		return TerminalRetryDecision(p)
	}
	_ = chain.Run(context.Background(), p) // 中间件逻辑不含强制 ctx；放行到兜底
	if p.Action != nil {
		return *p.Action
	}
	return TerminalRetryDecision(p)
}

// RecordRequestError 把一次请求错误写入会话日志（agent/error 事件），供审计。
// log 可为 nil（此时跳过）。
func RecordRequestError(log *session.SessionLog, err error) {
	if log == nil || err == nil {
		return
	}
	_, _ = log.Append(session.AgentErrorData{
		Message: err.Error(),
		Pkg:     "pkg/agent",
	})
}