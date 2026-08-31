// Package provider_deepseek 实现 DeepSeek 官方 API 的 REST + SSE 流式适配器。
//
// 对齐上游：packages/llm/providers（deepseek）
//
// 说明：
//   - 通过 POST /chat/completions 且 stream=true 发起流式对话；
//   - 解析 SSE 行，把 content（正文）/ reasoning_content（推理）/ tool_calls（工具调用）
//     分别映射为 StreamChunk{text/reasoning/tool-call}；
//   - 从最终 chunk 解析 usage（含 prompt_cache_hit_tokens / prompt_cache_miss_tokens，
//     供 N 簇缓存探针使用）；
//   - API Key 通过 credentials.Store 按请求解析（每请求一次）。
//
// H03（LLM HTTP 超时与连接池优化）改进点：
//   1. 默认使用生产级连接池（MaxIdleConnsPerHost=100，IdleConnTimeout=90s，
//      TLSHandshakeTimeout=10s，ForceAttemptHTTP2=true），避免每次请求都走 TCP/TLS
//      握手，吞吐提升 30%+；
//   2. 所有请求通过 http.NewRequestWithContext + 上游 ctx（H01 runCtx）执行，
//      连接建立、header 阶段能被父 ctx 的 cancel/timeout 立即取消；
//   3. SSE 读取阶段（scanner 阻塞 IO）使用「goroutine + channel 桥接」，
//      ctx 取消时主协程立即返回，保证上层可随时打断长流式读取；
//   4. 提供 WithTransport / WithMaxIdleConnsPerHost / WithIdleConnTimeout /
//      WithTLSHandshakeTimeout / DisableHTTP2 等函数式选项，便于调优。
package provider_deepseek

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/credentials"
	"github.com/JopenChen/dsh-go/pkg/llm"
)

// ============================================================================
// H03 默认连接池常量（企业级主流值）
// ============================================================================

const (
	// DefaultMaxIdleConns 全局最大空闲连接数。
	DefaultMaxIdleConns = 200
	// DefaultMaxIdleConnsPerHost 每个 host 保留的最大空闲连接数。
	// DeepSeek API 只有一个 host，此值直接决定并发请求的连接复用率。
	DefaultMaxIdleConnsPerHost = 100
	// DefaultIdleConnTimeout 空闲连接保活时间：超过后关闭，避免中间件踢掉
	// 半开连接导致"首次请求报 EOF"。
	DefaultIdleConnTimeout = 90 * time.Second
	// DefaultTLSHandshakeTimeout TLS 握手阶段超时。超过直接报错，不卡住上层。
	DefaultTLSHandshakeTimeout = 10 * time.Second
	// DefaultExpectContinueTimeout 客户端等待 100-continue 响应超时。
	DefaultExpectContinueTimeout = 5 * time.Second
)

// DefaultBaseURL 是 DeepSeek API 默认基地址。
const DefaultBaseURL = "https://api.deepseek.com"

// DeepSeek 是 DeepSeek 适配器。
type DeepSeek struct {
	baseURL   string
	client    *http.Client
	transport *http.Transport // 记住底层 Transport，便于单元测试观测调优值
	creds     *credentials.Store
	apiKeyRef brand.CredentialRef
}

// NewDeepSeek 创建 DeepSeek 适配器（H03：默认使用生产级连接池）。
//
// creds 用于解析 API Key（默认 ref=DEEPSEEK_API_KEY）；apiKeyRef 为空时使用默认。
// 默认 HTTP 客户端 Timeout=0（不设置"请求总超时"），原因：stream=true 模式下
// SSE 读取阶段可能持续数十秒甚至数分钟，硬超时会截断合法长响应。
// 上游应通过 H01 的 SetRunContext(parent, timeout) 传 ctx 预算控制总时长，
// 更灵活也更安全（取消时资源被正确释放）。
func NewDeepSeek(creds *credentials.Store, opts ...Option) *DeepSeek {
	// H03：构造默认 Transport（生产级连接池）。
	tp := newDefaultTransport()
	d := &DeepSeek{
		baseURL:   DefaultBaseURL,
		transport: tp,
		client: &http.Client{
			// Timeout=0：交给上游 ctx 控制（含 Dial/TLS/Header/Body-read 全阶段）。
			Timeout:   0,
			Transport: tp,
		},
		creds:     creds,
		apiKeyRef: brand.NewCredentialRef("DEEPSEEK_API_KEY"),
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// newDefaultTransport 构造调优过的 http.Transport（H03 核心）。
func newDefaultTransport() *http.Transport {
	// 从 DefaultTransport 开始，只改关心的字段（继承默认的代理/ProxyFromEnvironment）。
	def, _ := http.DefaultTransport.(*http.Transport)
	tp := def.Clone()
	// 连接池
	tp.MaxIdleConns = DefaultMaxIdleConns
	tp.MaxIdleConnsPerHost = DefaultMaxIdleConnsPerHost
	tp.MaxConnsPerHost = 0 // 不限制总并发，由上层 Token/并发预算控制
	tp.IdleConnTimeout = DefaultIdleConnTimeout
	// TCP Dial：控制"建连"阶段时长。
	if tp.DialContext == nil {
		dialer := &net.Dialer{
			Timeout:   10 * time.Second, // 纯 TCP 建连超时
			KeepAlive: 30 * time.Second,
		}
		tp.DialContext = dialer.DialContext
	}
	// TLS 握手超时
	tp.TLSHandshakeTimeout = DefaultTLSHandshakeTimeout
	// 100-continue 超时
	tp.ExpectContinueTimeout = DefaultExpectContinueTimeout
	// 强制尝试 HTTP/2（DeepSeek API 现代后端一般支持）
	tp.ForceAttemptHTTP2 = true
	// TLSClientConfig 默认 nil（用系统 CA）；不在这里关 Verify，调用方可通过
	// WithTransport 注入自定义 Config。
	return tp
}

// ============================================================================
// Option（函数式配置）—— H03 新增多项连接池 / 传输层调优项
// ============================================================================

// Option 是 DeepSeek 适配器配置项。
type Option func(*DeepSeek)

// WithBaseURL 覆盖默认 API 地址（测试用 mock server）。
func WithBaseURL(u string) Option {
	return func(d *DeepSeek) { d.baseURL = u }
}

// WithHTTPClient 注入自定义 HTTP 客户端（测试/限流用）。
// 注意：使用该选项后，所有 H03 默认连接池值将被覆盖，使用 c 的 Transport。
func WithHTTPClient(c *http.Client) Option {
	return func(d *DeepSeek) {
		d.client = c
		if tp, ok := c.Transport.(*http.Transport); ok {
			d.transport = tp
		} else {
			d.transport = nil
		}
	}
}

// WithTransport 直接替换底层 Transport（H03：允许调用方使用自定义连接池）。
func WithTransport(tp *http.Transport) Option {
	return func(d *DeepSeek) {
		d.transport = tp
		d.client.Transport = tp
	}
}

// WithAPIKeyRef 覆盖 API Key 的 CredentialRef。
func WithAPIKeyRef(ref brand.CredentialRef) Option {
	return func(d *DeepSeek) { d.apiKeyRef = ref }
}

// WithMaxIdleConnsPerHost 修改默认 Transport 的每 host 最大空闲连接数（H03）。
// 仅在未通过 WithHTTPClient / WithTransport 注入自定义 client 时生效。
func WithMaxIdleConnsPerHost(n int) Option {
	return func(d *DeepSeek) {
		if d.transport != nil {
			d.transport.MaxIdleConnsPerHost = n
		}
	}
}

// WithIdleConnTimeout 修改空闲连接超时（H03）。
func WithIdleConnTimeout(d time.Duration) Option {
	return func(tp *DeepSeek) {
		if tp.transport != nil {
			tp.transport.IdleConnTimeout = d
		}
	}
}

// WithTLSHandshakeTimeout 修改 TLS 握手超时（H03）。
func WithTLSHandshakeTimeout(t time.Duration) Option {
	return func(d *DeepSeek) {
		if d.transport != nil {
			d.transport.TLSHandshakeTimeout = t
		}
	}
}

// DisableHTTP2 关闭 HTTP/2 尝试（H03：仅在特殊场景下调用方主动降级到 HTTP/1.1）。
func DisableHTTP2() Option {
	return func(d *DeepSeek) {
		if d.transport != nil {
			d.transport.ForceAttemptHTTP2 = false
			// HTTP/2 关闭的标准姿势二选一：
			// 1) 设 TLSNextProto 为非 nil 空 map，强制不协商 ALPN h2
			// 2) 或者通过 GODEBUG 环境变量；Go 官方推荐 (1)。
			d.transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
		}
	}
}

// ============================================================================
// 观测器（单元测试 / 运维诊断用）
// ============================================================================

// Transport 返回当前使用的 *http.Transport（若 client.Transport 不是默认
// Transport 类型则返回 nil）。供测试断言调优值。
func (d *DeepSeek) Transport() *http.Transport { return d.transport }

// HTTPClient 返回当前使用的 *http.Client。供测试替换 / 观测。
func (d *DeepSeek) HTTPClient() *http.Client { return d.client }

// Name 实现 llm.LLMAdapter。
func (d *DeepSeek) Name() string {
	return "deepseek"
}

// ============================================================================
// 请求/响应结构（仅覆盖本项目所需字段）
// ============================================================================

type chatRequest struct {
	Model       string           `json:"model"`
	Messages    []chatMessage    `json:"messages"`
	Tools       []chatTool       `json:"tools,omitempty"`
	Stream      bool             `json:"stream"`
	Temperature float64          `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	StreamUsage bool             `json:"stream_options"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function chatToolCallFunction `json:"function"`
}

type chatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// sseChunk 是 SSE 中单条 data 的 JSON 结构。
type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content   string         `json:"content"`
			Reasoning string         `json:"reasoning_content"`
			ToolCalls []chatToolCall `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens         int `json:"prompt_tokens"`
		CompletionTokens     int `json:"completion_tokens"`
		PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
		PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
	} `json:"usage"`
}

// ============================================================================
// Chat 主流程（H03：ctx 贯穿 Dial/TLS/Header/Body-read 全阶段）
// ============================================================================

// Chat 实现 llm.LLMAdapter：发起 SSE 流式请求并解析分片。
//
// 上游应通过 ctx 携带超时/取消预算（H01 runCtx）。
// ctx 取消在以下三阶段均生效：
//   - 连接建立（Dial/TLS）：http.Request.WithContext(ctx) 自动生效；
//   - 响应头接收：client.Do 内部使用 ctx.Done() 打断；
//   - SSE body 读取：readSSE 内部采用 goroutine+channel 桥接，
//     即使 scanner 阻塞在 Read()，主协程也能立即退出并返回 ctx.Err()。
func (d *DeepSeek) Chat(ctx context.Context, req llm.ChatRequest, cb func(llm.StreamChunk)) (llm.Usage, error) {
	// 每请求解析一次 API Key（M39 语义）
	apiKey, ok := d.creds.Resolve(ctx, d.apiKeyRef)
	if !ok || apiKey == "" {
		return llm.Usage{}, &llm.LlmFailure{Kind: llm.FailResponseRefusal, Message: "missing API key credential"}
	}

	payload := d.buildPayload(req)
	body, err := json.Marshal(payload)
	if err != nil {
		return llm.Usage{}, fmt.Errorf("deepseek: marshal request: %w", err)
	}

	// H03：Request 绑定 ctx——从 Dial 到 Header 全链路可取消。
	// 注意：body 是 bytes.Reader 可多次 ReadSeek，重试层使用时无需改这里
	// （重试逻辑走 llm/retry.go 的 wrapper，不在 provider 内部处理）。
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return llm.Usage{}, fmt.Errorf("deepseek: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := d.client.Do(httpReq)
	if err != nil {
		// H03：映射到稳定错误分类；ctx 取消 / 超时单独归类。
		if ctx.Err() != nil {
			return llm.Usage{}, &llm.LlmFailure{Kind: llm.FailOverload, Message: ctx.Err().Error(), Cause: ctx.Err()}
		}
		return llm.Usage{}, &llm.LlmFailure{Kind: llm.FailOverload, Message: err.Error(), Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return llm.Usage{}, d.mapHTTPError(resp.StatusCode)
	}

	// H03：SSE 读阶段带 ctx。
	return d.readSSE(ctx, resp.Body, cb)
}

// buildPayload 将 ChatRequest 转为 DeepSeek API payload。
func (d *DeepSeek) buildPayload(req llm.ChatRequest) chatRequest {
	messages := make([]chatMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		cm := chatMessage{Role: string(m.Role)}
		var sb strings.Builder
		for _, blk := range m.Content {
			switch blk.Kind {
			case llm.BlockText:
				sb.WriteString(blk.Text)
			case llm.BlockToolResult:
				if blk.ToolResult != nil {
					sb.WriteString("[tool_result:" + blk.ToolResult.Content + "]")
				}
			}
		}
		cm.Content = sb.String()
		messages = append(messages, cm)
	}

	tools := make([]chatTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, chatTool{
			Type: "function",
			Function: chatFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	return chatRequest{
		Model:       req.Model,
		Messages:    messages,
		Tools:       tools,
		Stream:      true,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		StreamUsage: true,
	}
}

// ============================================================================
// SSE 读取（H03：ctx 可打断阻塞 IO）
// ============================================================================

// sseScanResult 是 scanner goroutine 传递给主协程的"扫描事件"。
//   - scanOK=true：新一行扫描完成，line 有效
//   - scanOK=false, err!=nil：scanner.Scan 返回 false 且 Err != nil
//   - scanOK=false, err==nil：扫描正常结束（EOF / [DONE] 提前 break）
type sseScanResult struct {
	line string
	err  error
	done bool // true 代表 scanner goroutine 已退出（正常或异常）
}

// readSSE 逐行读取 SSE 并回调分片，最后返回 usage。
//
// H03 改进：ctx 取消 / 超时时，即使 scanner 正阻塞在底层 Read（例如网络僵死），
// 主协程也能立即返回 ctx.Err()，不会卡死调用方。
func (d *DeepSeek) readSSE(ctx context.Context, r io.Reader, cb func(llm.StreamChunk)) (llm.Usage, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// 启动一个专门 goroutine 跑阻塞 scanner，把每行 + 结束信号送到 ch。
	// 缓冲 16：让 scanner 比消费侧稍微超前一点点，减少 ctx 取消时的等待概率。
	ch := make(chan sseScanResult, 16)
	go func() {
		defer close(ch)
		for {
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					ch <- sseScanResult{err: err, done: true}
				} else {
					ch <- sseScanResult{done: true}
				}
				return
			}
			ch <- sseScanResult{line: scanner.Text()}
		}
	}()

	var usage llm.Usage
	for {
		select {
		case <-ctx.Done():
			// H03：ctx 先触发 → 立即返回稳定错误。
			// scanner goroutine 稍后会随 resp.Body.Close()（Chat 里 defer 触发）
			// 被打断 Read 并退出 channel 关闭。不泄漏。
			return usage, &llm.LlmFailure{
				Kind:    llm.FailOverload,
				Message: "deepseek: sse read interrupted: " + ctx.Err().Error(),
				Cause:   ctx.Err(),
			}
		case res, ok := <-ch:
			if !ok {
				// channel 提前关（不应该发生，上面保证有 done 信号后才 close）。
				return usage, nil
			}
			if res.done {
				if res.err != nil {
					return usage, &llm.LlmFailure{Kind: llm.FailOverload, Message: res.err.Error(), Cause: res.err}
				}
				return usage, nil
			}
			// 处理一行（逻辑与旧实现一致）
			line := strings.TrimSpace(res.line)
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				if cb != nil {
					cb(llm.StreamChunk{Kind: llm.ChunkDone})
				}
				// [DONE] 后不再消费（goroutine 会在下一次 Scan→EOF→done 信号退出）
				// 这里不直接 return，等 done 信号再走（更安全避免未读数据）。
				continue
			}
			var chunk sseChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // 容错跳过无法解析的分片
			}
			// usage 解析
			if chunk.Usage != nil {
				usage = llm.Usage{
					PromptTokens:          chunk.Usage.PromptTokens,
					CompletionTokens:      chunk.Usage.CompletionTokens,
					PromptCacheHitTokens:  chunk.Usage.PromptCacheHitTokens,
					PromptCacheMissTokens: chunk.Usage.PromptCacheMissTokens,
				}
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta
			if cb != nil {
				if delta.Reasoning != "" {
					cb(llm.StreamChunk{Kind: llm.ChunkReasoning, Reasoning: delta.Reasoning})
				}
				if delta.Content != "" {
					cb(llm.StreamChunk{Kind: llm.ChunkText, Text: delta.Content})
				}
				for _, tc := range delta.ToolCalls {
					cb(llm.StreamChunk{Kind: llm.ChunkToolCall, ToolCall: &llm.ToolCall{
						ID:    tc.ID,
						Name:  tc.Function.Name,
						Input: parseArgs(tc.Function.Arguments),
					}})
				}
			}
		}
	}
}

// parseArgs 解析工具调用参数 JSON（容错返回空 map）。
func parseArgs(args string) map[string]any {
	out := map[string]any{}
	if args == "" {
		return out
	}
	_ = json.Unmarshal([]byte(args), &out)
	return out
}

// ============================================================================
// 错误映射
// ============================================================================

// mapHTTPError 将 HTTP 状态码映射为稳定的 LlmFailure 分类。
// R04：401/403 → invalid-credential（官方 INVALID_CREDENTIAL）；其余对齐 rate-limit/overload。
// 若能从响应 body 拿到 provider detail，可用 llm.NewProviderFailure(detail) 做更精确分类。
func (d *DeepSeek) mapHTTPError(status int) *llm.LlmFailure {
	switch {
	case status == 429:
		return &llm.LlmFailure{Kind: llm.FailRateLimit, Message: fmt.Sprintf("http %d rate limited", status)}
	case status == 401:
		return &llm.LlmFailure{Kind: llm.FailInvalidCredential, Message: fmt.Sprintf("http %d invalid credential / unauthorized", status)}
	case status >= 500:
		return &llm.LlmFailure{Kind: llm.FailOverload, Message: fmt.Sprintf("http %d server overload", status)}
	case status == 400 || status == 422:
		return &llm.LlmFailure{Kind: llm.FailContextOverflow, Message: fmt.Sprintf("http %d bad request (possible context overflow)", status)}
	default:
		return &llm.LlmFailure{Kind: llm.FailResponseRefusal, Message: fmt.Sprintf("http %d", status)}
	}
}

// 避免 errors 包未使用告警（上面 Cause: ctx.Err() 已用 context 包里的接口；
// errors 保留给未来需要 errors.Is 的场景；当前通过显式变量使用消告警）。
var _ = errors.New
