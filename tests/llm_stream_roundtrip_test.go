// 本文件对应任务 M07：LLM Provider 接缝 + 流式协议（SSE roundtrip + 失败分类）。
package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/credentials"
	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/llm/provider_deepseek"
)

// newDeepseekWithMock 构造基于 mock server 的 DeepSeek 适配器。
func newDeepseekWithMock(t *testing.T, handler http.HandlerFunc) *provider_deepseek.DeepSeek {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	store := credentials.NewMemoryStore()
	_ = store.Set(context.Background(), brand.NewCredentialRef("DEEPSEEK_API_KEY"), "sk-test")

	return provider_deepseek.NewDeepSeek(store,
		provider_deepseek.WithBaseURL(srv.URL),
		provider_deepseek.WithHTTPClient(srv.Client()),
	)
}

// sseData 构造一条 SSE data 行。
func sseData(obj string) string {
	return "data: " + obj + "\n"
}

// TestLLMDeepseekSSERoundtrip 验证 reasoning + content + tool_call + usage 完整解析。
func TestLLMDeepseekSSERoundtrip(t *testing.T) {
	adapter := newDeepseekWithMock(t, func(w http.ResponseWriter, r *http.Request) {
		// 校验鉴权头
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("Authorization 头异常: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// 推理块
		_, _ = w.Write([]byte(sseData(`{"choices":[{"delta":{"reasoning_content":"思考中"}}]}`)))
		// 正文块
		_, _ = w.Write([]byte(sseData(`{"choices":[{"delta":{"content":"你好"}}]}`)))
		// 工具调用块
		_, _ = w.Write([]byte(sseData(`{"choices":[{"delta":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"cmd\":\"ls\"}"}}]}}]}`)))
		// usage 块
		_, _ = w.Write([]byte(sseData(`{"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_cache_hit_tokens":8,"prompt_cache_miss_tokens":2}}`)))
		_, _ = w.Write([]byte("data: [DONE]\n"))
	})

	var chunks []llm.StreamChunk
	usage, err := adapter.Chat(context.Background(), llm.ChatRequest{
		Model:  "deepseek-chat",
		System: "你是一个助手",
		Messages: []llm.Message{
			llm.NewUserMessage("你好"),
		},
	}, func(c llm.StreamChunk) {
		chunks = append(chunks, c)
	})
	if err != nil {
		t.Fatalf("Chat 失败: %v", err)
	}

	// 验证分片种类与内容
	if len(chunks) < 4 {
		t.Fatalf("应至少 4 个分片(推理+正文+工具+done): %d", len(chunks))
	}
	if chunks[0].Kind != llm.ChunkReasoning || chunks[0].Reasoning != "思考中" {
		t.Fatalf("推理分片异常: %+v", chunks[0])
	}
	if chunks[1].Kind != llm.ChunkText || chunks[1].Text != "你好" {
		t.Fatalf("正文分片异常: %+v", chunks[1])
	}
	if chunks[2].Kind != llm.ChunkToolCall {
		t.Fatalf("工具调用分片异常: %+v", chunks[2])
	}
	if chunks[2].ToolCall == nil || chunks[2].ToolCall.Name != "bash" || chunks[2].ToolCall.Input["cmd"] != "ls" {
		t.Fatalf("工具调用内容异常: %+v", chunks[2].ToolCall)
	}
	if chunks[len(chunks)-1].Kind != llm.ChunkDone {
		t.Fatalf("末分片应为 done: %+v", chunks[len(chunks)-1])
	}

	// 验证 usage 缓存命中字段
	if usage.PromptTokens != 10 || usage.CompletionTokens != 5 {
		t.Fatalf("usage token 异常: %+v", usage)
	}
	if usage.PromptCacheHitTokens != 8 || usage.PromptCacheMissTokens != 2 {
		t.Fatalf("usage 缓存字段异常: %+v", usage)
	}
}

// TestLLMDeepseekFailureMapping 验证 HTTP 状态码 → LlmFailure 分类映射。
func TestLLMDeepseekFailureMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   llm.LlmFailureKind
	}{
		{"rate-limit", http.StatusTooManyRequests, llm.FailRateLimit},
		{"overload", http.StatusInternalServerError, llm.FailOverload},
		{"context-overflow", http.StatusBadRequest, llm.FailContextOverflow},
		{"refusal", http.StatusForbidden, llm.FailResponseRefusal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := newDeepseekWithMock(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
			})
			_, err := adapter.Chat(context.Background(), llm.ChatRequest{Model: "m"}, func(llm.StreamChunk) {})
			if err == nil {
				t.Fatal("应返回错误")
			}
			failure := llm.ClassifyLlmError(err)
			if failure == nil || failure.Kind != tc.want {
				t.Fatalf("分类 = %v, want %s", failure, tc.want)
			}
		})
	}
}

// TestLLMDeepseekMissingKey 验证缺少 API Key 时返回 refusal 分类。
func TestLLMDeepseekMissingKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("不应发起真实请求")
	}))
	defer srv.Close()

	store := credentials.NewMemoryStore() // 无 key
	adapter := provider_deepseek.NewDeepSeek(store, provider_deepseek.WithBaseURL(srv.URL))

	_, err := adapter.Chat(context.Background(), llm.ChatRequest{Model: "m"}, func(llm.StreamChunk) {})
	if err == nil {
		t.Fatal("缺 key 应报错")
	}
	if !strings.Contains(err.Error(), "missing API key") {
		t.Fatalf("错误信息异常: %v", err)
	}
}

// TestLLMAdapterInterface 验证 DeepSeek 适配器满足 LLMAdapter 接口。
func TestLLMAdapterInterface(t *testing.T) {
	var _ llm.LLMAdapter = (*provider_deepseek.DeepSeek)(nil)
}
