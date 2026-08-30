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
package provider_deepseek

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/credentials"
	"github.com/JopenChen/dsh-go/pkg/llm"
)

// DefaultBaseURL 是 DeepSeek API 默认基地址。
const DefaultBaseURL = "https://api.deepseek.com"

// DeepSeek 是 DeepSeek 适配器。
type DeepSeek struct {
	baseURL   string
	client    *http.Client
	creds     *credentials.Store
	apiKeyRef brand.CredentialRef
}

// NewDeepSeek 创建 DeepSeek 适配器。
// creds 用于解析 API Key（默认 ref=DEEPSEEK_API_KEY）；apiKeyRef 为空时使用默认。
func NewDeepSeek(creds *credentials.Store, opts ...Option) *DeepSeek {
	d := &DeepSeek{
		baseURL:   DefaultBaseURL,
		client:    &http.Client{},
		creds:     creds,
		apiKeyRef: brand.NewCredentialRef("DEEPSEEK_API_KEY"),
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Option 是 DeepSeek 适配器配置项。
type Option func(*DeepSeek)

// WithBaseURL 覆盖默认 API 地址（测试用 mock server）。
func WithBaseURL(u string) Option {
	return func(d *DeepSeek) { d.baseURL = u }
}

// WithHTTPClient 注入自定义 HTTP 客户端（测试/限流用）。
func WithHTTPClient(c *http.Client) Option {
	return func(d *DeepSeek) { d.client = c }
}

// WithAPIKeyRef 覆盖 API Key 的 CredentialRef。
func WithAPIKeyRef(ref brand.CredentialRef) Option {
	return func(d *DeepSeek) { d.apiKeyRef = ref }
}

// Name 实现 llm.LLMAdapter。
func (d *DeepSeek) Name() string {
	return "deepseek"
}

// 请求/响应结构（仅覆盖本项目所需字段）

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
	Role    string        `json:"role"`
	Content string        `json:"content"`
	ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string         `json:"type"`
	Function chatFunction   `json:"function"`
}

type chatFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
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
			Content      string        `json:"content"`
			Reasoning    string        `json:"reasoning_content"`
			ToolCalls    []chatToolCall `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
		PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
	} `json:"usage"`
}

// Chat 实现 llm.LLMAdapter：发起 SSE 流式请求并解析分片。
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return llm.Usage{}, fmt.Errorf("deepseek: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := d.client.Do(httpReq)
	if err != nil {
		return llm.Usage{}, &llm.LlmFailure{Kind: llm.FailOverload, Message: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return llm.Usage{}, d.mapHTTPError(resp.StatusCode)
	}

	return d.readSSE(resp.Body, cb)
}

// buildPayload 将 ChatRequest 转为 DeepSeek API payload。
func (d *DeepSeek) buildPayload(req llm.ChatRequest) chatRequest {
	messages := make([]chatMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		cm := chatMessage{Role: string(m.Role)}
		// 简单拼接内容块文本（工具调用块由上层展开；此处聚焦文本流）
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
		tools = append(tools, chatTool{Type: "function", Function: chatFunction{Name: t.Name, Description: t.Description, Parameters: t.Parameters}})
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

// readSSE 逐行读取 SSE 并回调分片，最后返回 usage。
func (d *DeepSeek) readSSE(r io.Reader, cb func(llm.StreamChunk)) (llm.Usage, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var usage llm.Usage
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
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
			break
		}
		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // 跳过无法解析的分片
		}
		// usage 解析（DeepSeek 在 stream_options.include_usage 时于末 chunk 返回）
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
	if err := scanner.Err(); err != nil {
		return usage, &llm.LlmFailure{Kind: llm.FailOverload, Message: err.Error()}
	}
	return usage, nil
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

// mapHTTPError 将 HTTP 状态码映射为稳定的 LlmFailure 分类。
func (d *DeepSeek) mapHTTPError(status int) *llm.LlmFailure {
	switch {
	case status == 429:
		return &llm.LlmFailure{Kind: llm.FailRateLimit, Message: fmt.Sprintf("http %d rate limited", status)}
	case status >= 500:
		return &llm.LlmFailure{Kind: llm.FailOverload, Message: fmt.Sprintf("http %d server overload", status)}
	case status == 400 || status == 422:
		return &llm.LlmFailure{Kind: llm.FailContextOverflow, Message: fmt.Sprintf("http %d bad request (possible context overflow)", status)}
	default:
		return &llm.LlmFailure{Kind: llm.FailResponseRefusal, Message: fmt.Sprintf("http %d", status)}
	}
}
