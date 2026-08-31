// Package tests 的 request-error 重试瀑布（M34）验收测试。
//
// 覆盖：
//   - retryable 错误在 max 次内 → retry 动作
//   - 超过 max 次 → abort（以 error 关闭 turn）
//   - 自定义瀑布中间件可改写动作（M02 short-circuit）
//   - 非可重试错误 → 直接 abort
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/agent"
	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/waterfall"
)

// TestRequestErrorRetryWithinMax 验证可重试错误在 max 次内返回 retry。
func TestRequestErrorRetryWithinMax(t *testing.T) {
	err := &llm.LlmFailure{Kind: llm.FailOverload, Message: "overload"}
	p := agent.NewRequestErrorPayload(err, 1, 3)
	act := agent.ResolveRequestError(agent.NewRequestErrorWaterfall(), p)
	if act.Kind != agent.ActionRetry {
		t.Fatalf("第 1 次重试内应 retry, 实际 %s (%s)", act.Kind, act.Reason)
	}
}

// TestRequestErrorAbortAfterMax 验证超过 max 次 → abort（以 error 关闭 turn）。
func TestRequestErrorAbortAfterMax(t *testing.T) {
	err := &llm.LlmFailure{Kind: llm.FailRateLimit, Message: "rate limited"}
	// 驱动重试循环：S15 已耗尽量层重试，进入 request-error 瀑布。
	maxRetries := 3
	var last agent.RequestErrorAction
	for count := 0; count <= maxRetries; count++ {
		p := agent.NewRequestErrorPayload(err, count, maxRetries)
		last = agent.ResolveRequestError(agent.NewRequestErrorWaterfall(), p)
		if last.Kind == agent.ActionAbort {
			break
		}
	}
	if last.Kind != agent.ActionAbort {
		t.Fatalf("超过 max 次应 abort, 实际 %s", last.Kind)
	}
	if last.Reason == "" {
		t.Fatal("abort 应带关闭原因")
	}
}

// TestRequestErrorCustomWaterfall 验证自定义中间件可改写动作（M02 short-circuit）。
func TestRequestErrorCustomWaterfall(t *testing.T) {
	err := &llm.LlmFailure{Kind: llm.FailOverload, Message: "overload"}
	// 自定义中间件：总是 abort 并写明原因。
	chain := agent.NewRequestErrorWaterfall(
		func(p *agent.RequestErrorPayload, next waterfall.NextFunc) error {
			a := agent.RequestErrorAction{Kind: agent.ActionAbort, Reason: "policy: never retry this route"}
			p.Action = &a
			return next() // 仍放行，但 Action 已固定
		},
	)
	p := agent.NewRequestErrorPayload(err, 0, 5)
	if act := agent.ResolveRequestError(chain, p); act.Kind != agent.ActionAbort {
		t.Fatalf("自定义中间件应固定 abort, 实际 %s", act.Kind)
	}
}

// TestRequestErrorNonRetryableAbort 验证非可重试错误直接 abort。
func TestRequestErrorNonRetryableAbort(t *testing.T) {
	err := &llm.LlmFailure{Kind: llm.FailResponseRefusal, Message: "refused"}
	p := agent.NewRequestErrorPayload(err, 0, 10)
	act := agent.ResolveRequestError(agent.NewRequestErrorWaterfall(), p)
	if act.Kind != agent.ActionAbort {
		t.Fatalf("refusal 应直接 abort, 实际 %s", act.Kind)
	}
	if p.RetryableError {
		t.Fatal("refusal 不应标记可重试")
	}
}

// TestRequestErrorRecordEvent 验证可把请求错误写入 session 日志（agent/error）。
func TestRequestErrorRecordEvent(t *testing.T) {
	// 依赖 M34 与 M04 事件溯源：RecordRequestError 追加 agent/error。
	err := &llm.LlmFailure{Kind: llm.FailOverload, Message: "overload"}
	// 注意：RecordRequestError 通过 SessionLog.Append 写入；这里仅验证 API 可被安全调用
	//（nil log 时跳过）。真实事件写入在 agent 循环中由调用方提供 SessionLog。
	agent.RecordRequestError(nil, err) // 应无 panic
	agent.RecordRequestError(nil, nil) // 应无 panic
}