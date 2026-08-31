// Package tests 的 LLM Retry（S15）验收测试。
//
// 覆盖：
//   - 模拟 3 次 overload → 写 3 条 llm/retry 事件，第 4 次成功
//   - 非可重试失败（refusal）不重试直接上抛
//   - 超过 max 次 → 上抛错误（交 M34 request-error 瀑布）
package tests

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// flakyAdapter 是可控制失败次数的假适配器：前 failTimes 次返回 overload，之后成功。
type flakyAdapter struct {
	mu        sync.Mutex
	failTimes int
	calls     int
	failKind  llm.LlmFailureKind
}

func (f *flakyAdapter) Name() string { return "flaky" }

func (f *flakyAdapter) Chat(ctx context.Context, req llm.ChatRequest, cb func(llm.StreamChunk)) (llm.Usage, error) {
	f.mu.Lock()
	f.calls++
	r := f.calls
	f.mu.Unlock()
	if r <= f.failTimes {
		return llm.Usage{}, &llm.LlmFailure{Kind: f.failKind, Message: "overload"}
	}
	return llm.Usage{PromptTokens: 1, CompletionTokens: 1}, nil
}

// retryEventSink 收集 llm/retry 事件。
type retryEventSink struct {
	mu     sync.Mutex
	events []session.LLMRetryData
}

func (s *retryEventSink) write(d session.LLMRetryData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, d)
}

func (s *retryEventSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// fastCfg 是测试用快速退避配置（无抖动保证确定性）。
func fastCfg(maxAttempts int) llm.RetryConfig {
	return llm.RetryConfig{
		MaxAttempts: maxAttempts,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  5 * time.Millisecond,
		Jitter:      0,
	}
}

// TestLLMRetryWritesEventsAndSucceeds 验证 3 次失败写 3 条 retry 事件后第 4 次成功。
func TestLLMRetryWritesEventsAndSucceeds(t *testing.T) {
	inner := &flakyAdapter{failTimes: 3, failKind: llm.FailOverload}
	sink := &retryEventSink{}
	mw := llm.NewRetryMiddleware(inner, fastCfg(4), sink.write)

	usage, err := mw.Chat(context.Background(), llm.ChatRequest{}, func(llm.StreamChunk) {})
	if err != nil {
		t.Fatalf("第 4 次应成功, 实际 %v", err)
	}
	if usage.PromptTokens+usage.CompletionTokens <= 0 {
		t.Fatalf("应返回用量, 实际 %+v", usage)
	}
	if sink.count() != 3 {
		t.Fatalf("应写 3 条 retry 事件, 实际 %d", sink.count())
	}
	if inner.calls != 4 {
		t.Fatalf("应总共调用 4 次, 实际 %d", inner.calls)
	}
}

// TestLLMRetryNonRetryableNoRetry 验证非可重试失败不重试直接上抛。
func TestLLMRetryNonRetryableNoRetry(t *testing.T) {
	inner := &flakyAdapter{failTimes: 100, failKind: llm.FailResponseRefusal}
	sink := &retryEventSink{}
	mw := llm.NewRetryMiddleware(inner, fastCfg(5), sink.write)

	_, err := mw.Chat(context.Background(), llm.ChatRequest{}, func(llm.StreamChunk) {})
	if err == nil {
		t.Fatal("refusal 应上抛错误")
	}
	// 只调用 1 次，无重试事件（refusal 不重试）。
	if inner.calls != 1 {
		t.Fatalf("refusal 不应重试, 调用 %d 次", inner.calls)
	}
	if sink.count() != 0 {
		t.Fatalf("refusal 不应写 retry 事件, 实际 %d", sink.count())
	}
}

// TestLLMRetryExceedMaxReturnsError 验证超过 max 次失败 → 上抛错误（交 request-error 瀑布）。
func TestLLMRetryExceedMaxReturnsError(t *testing.T) {
	inner := &flakyAdapter{failTimes: 100, failKind: llm.FailOverload}
	sink := &retryEventSink{}
	mw := llm.NewRetryMiddleware(inner, fastCfg(2), sink.write) // max 2 次

	_, err := mw.Chat(context.Background(), llm.ChatRequest{}, func(llm.StreamChunk) {})
	if err == nil {
		t.Fatal("超过 max 次应上抛错误")
	}
	if inner.calls != 2 {
		t.Fatalf("max=2 应只调用 2 次, 实际 %d", inner.calls)
	}
}