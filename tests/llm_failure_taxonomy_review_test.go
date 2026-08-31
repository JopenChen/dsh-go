// 本文件对应 code-review 修复点 R04：对齐官方 LLM 稳定失败分类。
//
// 对照上游：D:\workspace\python_workspace\deepseek-harness\packages\llm\llm\src\error.ts
// 官方 HarnessError.code 稳定分类：CONTEXT_WINDOW_EXCEEDED / QUOTA / EMPTY_RESPONSE /
// INVALID_CREDENTIAL / RATE_LIMIT（另含 overload 等）。
//
// 验证目标：
//   1. 本项目 LlmFailureKind 已含官方三类缺失分类（quota / empty-response / invalid-credential）；
//   2. Retryable() 可重试集与官方默认 retryable 对齐（quota/invalid-credential 不可重试）；
//   3. ClassifyProviderDetail 对典型 provider 语料分类正确（上下文超限/配额耗尽/凭证非法/
//      限流/空响应/拒绝）；
//   4. NewProviderFailure 对未识别语料归为 unknown。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/llm"
)

// ============================================================================
// 1. 枚举覆盖
// ============================================================================

func TestR04KindEnumComplete(t *testing.T) {
	want := []struct {
		kind llm.LlmFailureKind
		name string
	}{
		{llm.FailOverload, "overload"},
		{llm.FailRateLimit, "rate-limit"},
		{llm.FailResponseRefusal, "response-refusal"},
		{llm.FailContextOverflow, "context-overflow"},
		{llm.FailQuota, "quota"},
		{llm.FailEmptyResponse, "empty-response"},
		{llm.FailInvalidCredential, "invalid-credential"},
	}
	for _, w := range want {
		if string(w.kind) != w.name {
			t.Fatalf("Kind 常量值异常: %q != %q", w.kind, w.name)
		}
	}
}

// ============================================================================
// 2. Retryable 语义（对齐官方默认 retryable 集）
// ============================================================================

func TestR04RetryableSemantics(t *testing.T) {
	cases := []struct {
		kind llm.LlmFailureKind
		want bool
	}{
		{llm.FailOverload, true},
		{llm.FailRateLimit, true},
		{llm.FailContextOverflow, true},
		{llm.FailEmptyResponse, true},
		{llm.FailResponseRefusal, false},
		{llm.FailQuota, false},
		{llm.FailInvalidCredential, false},
		{llm.LlmFailureKind("unknown"), false},
	}
	for _, c := range cases {
		if got := c.kind.Retryable(); got != c.want {
			t.Fatalf("Retryable(%q) = %v, want %v", c.kind, got, c.want)
		}
	}
}

// ============================================================================
// 3. Provider 语料分类器
// ============================================================================

func TestR04ClassifyProviderDetail(t *testing.T) {
	cases := []struct {
		detail string
		want   llm.LlmFailureKind
	}{
		// 上下文超限（官方 CONTEXT_WINDOW_EXCEEDED）
		{"This model's maximum context length is 128K tokens. However, your messages resulted in 200K tokens", llm.FailContextOverflow},
		{"context_length_exceeded", llm.FailContextOverflow},
		{"prompt is too long for this model's context window", llm.FailContextOverflow},
		// 配额耗尽（官方 QUOTA）
		{"Insufficient_quota", llm.FailQuota},
		{"quota exceeded for the current billing cycle", llm.FailQuota},
		{"You don't have enough API credits", llm.FailQuota},
		{"out of budget", llm.FailQuota},
		// 凭证非法（官方 INVALID_CREDENTIAL）
		{"invalid API key", llm.FailInvalidCredential},
		{"api_key_mismatch", llm.FailInvalidCredential},
		{"incorrect credential provided", llm.FailInvalidCredential},
		// 限流（RATE_LIMIT）
		{"Rate limit reached", llm.FailRateLimit},
		{"too many requests", llm.FailRateLimit},
		{"HTTP 429 Too Many Requests", llm.FailRateLimit},
		// 空响应（EMPTY_RESPONSE）
		{"empty response from model, no content blocks", llm.FailEmptyResponse},
		{"finish_reason stop with no content", llm.FailEmptyResponse},
		// 拒绝（response-refusal）
		{"The model refused to answer the request due to safety policy", llm.FailResponseRefusal},
		// 无法识别 → unknown
		{"Some unrelated network error: connection reset", llm.LlmFailureKind("unknown")},
	}
	for _, c := range cases {
		got := llm.ClassifyProviderDetail(c.detail)
		// 识别不到时函数返回 ""（unknown），统一比较语义。
		gotStr := string(got)
		if gotStr == "" {
			gotStr = "unknown"
		}
		if gotStr != string(c.want) {
			t.Fatalf("ClassifyProviderDetail(%q) = %q, want %q", c.detail, gotStr, c.want)
		}
	}
}

// TestR04EmptyDetail 空字符串不 panic 且返回 unknown。
func TestR04EmptyDetail(t *testing.T) {
	if got := llm.NewProviderFailure(""); got.Kind != "unknown" {
		t.Fatalf("空 detail 应归 unknown, got %q", got.Kind)
	}
}

// TestR04NewProviderFailure 构造失败：识别→稳定分类/未识别→unknown，Message 保留原文。
func TestR04NewProviderFailure(t *testing.T) {
	f := llm.NewProviderFailure("quota exhausted for the workspace")
	if f.Kind != llm.FailQuota {
		t.Fatalf("NewProviderFailure quota = %q", f.Kind)
	}
	if f.Message != "quota exhausted for the workspace" {
		t.Fatalf("Message 应保留原文, got %q", f.Message)
	}
	u := llm.NewProviderFailure("gibberish unknown backend hiccup")
	if u.Kind != "unknown" {
		t.Fatalf("未识别应归 unknown, got %q", u.Kind)
	}
}