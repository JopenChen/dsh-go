// 本文件对应任务 S15：LLM Retry（指数退避 + 抖动 + 写 llm/retry 事件）。
//
// 对齐上游：packages/llm/llm retry middleware
//
// 设计要点：
//   - RetryMiddleware 包裹任意 LLMAdapter，仅在可重试失败（overload / rate-limit）上
//     做指数退避重试；response-refusal / context-overflow 不重试，直接上抛；
//   - 每次退避后写一条 llm/retry 事件（attempt / backoffMs / error），供审计与决策；
//   - 达到最大尝试次数仍未成功 → 携最后一次错误上抛，交由上层 M34 request-error
//     瀑布接续处理（本层不无限重试）。
package llm

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/JopenChen/dsh-go/pkg/session"
)

// RetryConfig 是重试参数。
type RetryConfig struct {
	// MaxAttempts 最大尝试次数（含首次）；<=0 时缺省为 3。
	MaxAttempts int
	// BaseBackoff 基础退避时长。
	BaseBackoff time.Duration
	// MaxBackoff 退避上限。
	MaxBackoff time.Duration
	// Jitter 抖动比例（0~1）；0 表示不加抖动（便于测试确定性）。
	Jitter float64
}

// DefaultRetryConfig 返回默认重试参数。
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		BaseBackoff: 100 * time.Millisecond,
		MaxBackoff:  4 * time.Second,
		Jitter:      0.1,
	}
}

// RetryWriter 是 llm/retry 事件写出器（典型实现为 session.SessionLog.Append）。
type RetryWriter func(data session.LLMRetryData)

// RetryMiddleware 是带重试的 LLM 适配器装饰器。
type RetryMiddleware struct {
	inner LLMAdapter
	cfg   RetryConfig
	write RetryWriter
}

// NewRetryMiddleware 创建重试装饰器。write 可在每次退避后回写 retry 事件。
func NewRetryMiddleware(inner LLMAdapter, cfg RetryConfig, write RetryWriter) *RetryMiddleware {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 100 * time.Millisecond
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 4 * time.Second
	}
	return &RetryMiddleware{inner: inner, cfg: cfg, write: write}
}

// Name 透传内部适配器名。
func (m *RetryMiddleware) Name() string { return m.inner.Name() }

// Chat 执行带重试的流式对话。
func (m *RetryMiddleware) Chat(ctx context.Context, req ChatRequest, cb func(StreamChunk)) (Usage, error) {
	var lastErr error
	for attempt := 1; attempt <= m.cfg.MaxAttempts; attempt++ {
		usage, err := m.inner.Chat(ctx, req, cb)
		if err == nil {
			return usage, nil
		}
		lastErr = err
		f := ClassifyLlmError(err)
		// 仅 overload / rate-limit 可重试；其余失败直接上抛。
		if f.Kind != FailOverload && f.Kind != FailRateLimit {
			return usage, err
		}
		if attempt == m.cfg.MaxAttempts {
			// 已达最大次数：不再退避，上抛走 request-error 瀑布。
			return usage, err
		}
		backoff := m.computeBackoff(attempt)
		if m.write != nil {
			m.write(session.LLMRetryData{
				Attempt:   attempt,
				BackoffMs: backoff.Milliseconds(),
				Error:     err.Error(),
			})
		}
		select {
		case <-ctx.Done():
			return usage, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return Usage{}, lastErr
}

// computeBackoff 计算第 attempt 次失败后的退避时长：base * 2^(attempt-1)，上限封顶，
// 再按 Jitter 比例加入随机抖动。
func (m *RetryMiddleware) computeBackoff(attempt int) time.Duration {
	base := float64(m.cfg.BaseBackoff) * math.Pow(2, float64(attempt-1))
	if base > float64(m.cfg.MaxBackoff) {
		base = float64(m.cfg.MaxBackoff)
	}
	ms := base
	if m.cfg.Jitter > 0 {
		span := m.cfg.Jitter * base
		ms = base - span + rand.Float64()*(2*span) // 在 [base-span, base+span] 内抖动
	}
	if ms < 0 {
		ms = 0
	}
	return time.Duration(ms)
}