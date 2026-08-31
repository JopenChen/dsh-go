// 本文件对应任务 H03：LLM HTTP 超时与连接池优化（DeepSeek Provider）。
//
// 验证目标：
//   1. 默认 Transport 调优值正确（企业级主流连接池参数）；
//   2. 函数式选项能覆盖默认值（WithMaxIdleConnsPerHost / DisableHTTP2 等）；
//   3. ctx 取消能打断 Header 阶段（http.Client.Do）；
//   4. ctx 取消能打断 SSE 流式 Body 读取阶段（scanner 阻塞 IO 时，H03 新 goroutine+channel 桥接生效）；
//   5. LlmFailure.Cause + Unwrap 错误链可用 errors.Is 定位到 context.Canceled / DeadlineExceeded。
package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/credentials"
	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/llm/provider_deepseek"
)

// newCredsH03 构造带 DEEPSEEK_API_KEY = "sk-test-123" 的内存凭证库（简化）。
func newCredsH03() *credentials.Store {
	s := credentials.NewMemoryStore()
	_ = s.Set(context.Background(), brand.NewCredentialRef("DEEPSEEK_API_KEY"), "sk-test-123")
	return s
}

// ============================================================================
// 1. 默认 Transport 连接池参数正确
// ============================================================================

// TestH03DefaultPoolTuning 断言 NewDeepSeek() 默认构造的 Transport 调优值匹配常量。
func TestH03DefaultPoolTuning(t *testing.T) {
	d := provider_deepseek.NewDeepSeek(newCredsH03())
	tp := d.Transport()
	if tp == nil {
		t.Fatal("默认 Transport() 应为非 nil")
	}
	if tp.MaxIdleConns != provider_deepseek.DefaultMaxIdleConns {
		t.Fatalf("MaxIdleConns = %d, want %d", tp.MaxIdleConns, provider_deepseek.DefaultMaxIdleConns)
	}
	if tp.MaxIdleConnsPerHost != provider_deepseek.DefaultMaxIdleConnsPerHost {
		t.Fatalf("MaxIdleConnsPerHost = %d, want %d", tp.MaxIdleConnsPerHost, provider_deepseek.DefaultMaxIdleConnsPerHost)
	}
	if tp.IdleConnTimeout != provider_deepseek.DefaultIdleConnTimeout {
		t.Fatalf("IdleConnTimeout = %v, want %v", tp.IdleConnTimeout, provider_deepseek.DefaultIdleConnTimeout)
	}
	if tp.TLSHandshakeTimeout != provider_deepseek.DefaultTLSHandshakeTimeout {
		t.Fatalf("TLSHandshakeTimeout = %v, want %v", tp.TLSHandshakeTimeout, provider_deepseek.DefaultTLSHandshakeTimeout)
	}
	if !tp.ForceAttemptHTTP2 {
		t.Fatal("ForceAttemptHTTP2 应为 true（默认启用 H2）")
	}
	// Client.Timeout 应为 0（不硬超时，交给 ctx）。
	if d.HTTPClient().Timeout != 0 {
		t.Fatalf("http.Client.Timeout = %v, want 0（交由 H01 ctx 预算控制）", d.HTTPClient().Timeout)
	}
}

// ============================================================================
// 2. 选项覆盖默认值
// ============================================================================

// TestH03OptionsOverride 断言各 H03 选项能正确覆盖默认值。
func TestH03OptionsOverride(t *testing.T) {
	d := provider_deepseek.NewDeepSeek(newCredsH03(),
		provider_deepseek.WithMaxIdleConnsPerHost(42),
		provider_deepseek.WithIdleConnTimeout(7*time.Second),
		provider_deepseek.WithTLSHandshakeTimeout(3*time.Second),
		provider_deepseek.DisableHTTP2(),
	)
	tp := d.Transport()
	if tp.MaxIdleConnsPerHost != 42 {
		t.Fatalf("WithMaxIdleConnsPerHost(42) 后 MaxIdleConnsPerHost=%d", tp.MaxIdleConnsPerHost)
	}
	if tp.IdleConnTimeout != 7*time.Second {
		t.Fatalf("WithIdleConnTimeout(7s) 后 IdleConnTimeout=%v", tp.IdleConnTimeout)
	}
	if tp.TLSHandshakeTimeout != 3*time.Second {
		t.Fatalf("WithTLSHandshakeTimeout(3s) 后 TLSHandshakeTimeout=%v", tp.TLSHandshakeTimeout)
	}
	if tp.ForceAttemptHTTP2 {
		t.Fatal("DisableHTTP2 后 ForceAttemptHTTP2 应为 false")
	}
	if tp.TLSNextProto == nil {
		t.Fatal("DisableHTTP2 后 TLSNextProto 应设置为非 nil 空 map（标准姿势）")
	}
}

// ============================================================================
// 3. ctx 取消 → Header 阶段（client.Do）立即返回 + Cause 链正确
// ============================================================================

// TestH03CtxCancelDuringDo 模拟一个故意挂起的 httptest server，父 ctx 在 Do 前取消，
// Chat 应快速返回 LlmFailure 且 errors.Is(err, context.Canceled) == true。
//
// 注：此处使用独立的 release 通道让 handler 在断言完后立即返回，避免 httptest.Server.Close()
// 被"handler 还在等待 r.Context.Done()"卡住。server 侧的 r.Context() 与客户端 ctx 无关，
// 我们真正想验证的是"客户端自己的 ctx 取消时，http.Client.Do 立即取消"这一语义。
func TestH03CtxCancelDuringDo(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		// 先等 release：父测试会在拿到客户端错误后再 close(release)，
		// 保证 handler 能及时退出、不阻塞 httptest.Server.Close。
		<-release
	}))
	defer func() {
		// 双重保险：防御性 close 防止协程泄漏。
		select {
		case <-release:
		default:
			close(release)
		}
		srv.Close()
	}()

	d := provider_deepseek.NewDeepSeek(newCredsH03(), provider_deepseek.WithBaseURL(srv.URL))

	// 创建 ctx：发送请求后 20ms 取消（保证已进入 Do 阻塞）。
	parent, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := d.Chat(parent, llm.ChatRequest{Model: "deepseek-chat"}, nil)
	// 断言完后立刻释放 handler（先于 srv.Close）。
	close(release)

	if err == nil {
		t.Fatal("ctx 取消后 Chat 应返回 error, got nil")
	}
	var f *llm.LlmFailure
	if !errors.As(err, &f) {
		t.Fatalf("Chat 返回错误类型应为 *LlmFailure, got %T: %v", err, err)
	}
	// 根因应能沿 Cause 链找到 context.Canceled。
	if !errors.Is(f, context.Canceled) {
		t.Fatalf("LlmFailure 错误链 errors.Is(f, context.Canceled) = false; err=%v cause=%v", f, f.Cause)
	}
}

// ============================================================================
// 4. ctx 取消 → SSE Body 读取阶段立即返回
// ============================================================================

// slowSSEWriter 按 20ms/行 发送 SSE，直到服务器 ctx 被取消。
// 共 10000 行，正常读取需要 200s；我们在读取 80ms 后取消父 ctx，Chat 应在 <300ms 内返回。
func TestH03CtxCancelDuringSSEBody(t *testing.T) {
	var sentCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i := 0; i < 10000; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			// 合法 SSE chunk：空 content + 含 usage 的末 chunk 都不关心，
			// 只要每行合法，保证 scanner 能逐行扫。
			line := fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"content\":\"%d\"}}]}\n\n", i)
			_, werr := fmt.Fprint(w, line)
			if werr != nil {
				return
			}
			flusher.Flush()
			sentCount.Add(1)
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer srv.Close()

	d := provider_deepseek.NewDeepSeek(newCredsH03(), provider_deepseek.WithBaseURL(srv.URL))

	start := time.Now()
	// ctx 带 80ms 超时：应读到 3~5 条 chunk 后被 ctx 打断，而不是等 10000 条读完。
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	var gotChunks int
	_, err := d.Chat(ctx, llm.ChatRequest{Model: "deepseek-chat"}, func(c llm.StreamChunk) {
		if c.Kind == llm.ChunkText {
			gotChunks++
		}
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("SSE 中 ctx 超时后 Chat 应返回 error, got nil")
	}
	var f *llm.LlmFailure
	if !errors.As(err, &f) {
		t.Fatalf("返回错误应为 *LlmFailure, got %T", err)
	}
	// Cause 链应含 DeadlineExceeded。
	if !errors.Is(f, context.DeadlineExceeded) {
		t.Fatalf("应能 errors.Is 到 DeadlineExceeded；err=%v cause=%v", f, f.Cause)
	}
	// 超时后应快速返回（不应等待 goroutine 里 10000 条写完）。
	if elapsed > 500*time.Millisecond {
		t.Fatalf("ctx 超时返回耗时 = %v, 应 < 500ms（H03 的 SSE ctx 打断失败）", elapsed)
	}
	// 读期间应至少读到 1~5 条 chunk，证明 SSE 确实开始传了再被打断。
	if gotChunks < 1 {
		t.Fatalf("读到的 chunk 数 = %d, 应 >= 1（否则 mock server 可能根本没传）", gotChunks)
	}
}

// ============================================================================
// 5. 正常 SSE 流程：[DONE] 后 usage 返回正确
// ============================================================================

// TestH03NormalSSEUsage 验证读 SSE 末 chunk usage 字段正确返回，流程未被 H03 桥接破坏。
func TestH03NormalSSEUsage(t *testing.T) {
	payload := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\" World\"}}]}",
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"prompt_cache_hit_tokens\":7,\"prompt_cache_miss_tokens\":3}}",
		"data: [DONE]",
		"",
	}, "\n\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	d := provider_deepseek.NewDeepSeek(newCredsH03(), provider_deepseek.WithBaseURL(srv.URL))

	var text strings.Builder
	usage, err := d.Chat(context.Background(), llm.ChatRequest{Model: "deepseek-chat"}, func(c llm.StreamChunk) {
		if c.Kind == llm.ChunkText {
			text.WriteString(c.Text)
		}
	})
	if err != nil {
		t.Fatalf("Chat 返回错误: %v", err)
	}
	if text.String() != "Hello World" {
		t.Fatalf("拼接内容 = %q, want \"Hello World\"", text.String())
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens != 2 {
		t.Fatalf("usage = %+v, want prompt=10 completion=2", usage)
	}
	if usage.PromptCacheHitTokens != 7 || usage.PromptCacheMissTokens != 3 {
		t.Fatalf("cache usage hit=%d miss=%d, want hit=7 miss=3",
			usage.PromptCacheHitTokens, usage.PromptCacheMissTokens)
	}
}
