// Package tests 的 N01 DeepSeek 客户端探针 E2E 测试。
//
// 用 mock SSE 响应验证 parseUsage 正确解析 prompt_cache_hit_tokens /
// prompt_cache_miss_tokens 字段（DeepSeek 真实响应格式）。
package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/credentials"
	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/llm/provider_deepseek"
	"github.com/JopenChen/dsh-go/pkg/storage"
)

// deepseekSSEWithUsage 构造一段含 usage（含缓存字段）的 SSE 响应文本。
func deepseekSSEWithUsage() string {
	return "data: " + `{"choices":[{"delta":{"content":"hi"}}]}` + "\n\n" +
		"data: " + `{"choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":50,"prompt_cache_hit_tokens":900,"prompt_cache_miss_tokens":100}}` + "\n\n" +
		"data: [DONE]\n\n"
}

// TestDeepSeekParseUsageFromMockSSE 验证 DeepSeek 客户端从 SSE 正确解析缓存字段。
func TestDeepSeekParseUsageFromMockSSE(t *testing.T) {
	// 内存 credentials store 提供 API Key。
	store := credentials.NewStore(storage.NewMemoryKV())
	if err := store.Set(context.Background(), brand.NewCredentialRef("DEEPSEEK_API_KEY"), "sk-mock"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(deepseekSSEWithUsage()))
	}))
	defer srv.Close()

	adapter := provider_deepseek.NewDeepSeek(store, provider_deepseek.WithBaseURL(srv.URL))

	text := ""
	usage, err := adapter.Chat(context.Background(), llm.ChatRequest{Model: "deepseek-chat"}, func(c llm.StreamChunk) {
		if c.Kind == llm.ChunkText {
			text += c.Text
		}
	})
	if err != nil {
		t.Fatalf("Chat 失败: %v", err)
	}
	if text != "hi" {
		t.Fatalf("应收到流式文本 hi, 实际 %q", text)
	}
	if usage.PromptCacheHitTokens != 900 || usage.PromptCacheMissTokens != 100 {
		t.Fatalf("缓存字段解析错误: %+v", usage)
	}
	if usage.PromptTokens != 1000 || usage.CompletionTokens != 50 {
		t.Fatalf("普通 token 解析错误: %+v", usage)
	}
	// 命中率基线 correct。
	if r := llm.HitRatioOf(usage); r != 0.9 {
		t.Fatalf("命中率应 0.9, 实际 %v", r)
	}
}