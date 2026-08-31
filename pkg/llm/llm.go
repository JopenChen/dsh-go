// Package llm 提供 LLM Provider 接缝与流式协议类型。
//
// 对齐上游：packages/llm/llm + providers
//
// 设计要点：
//   - LLMAdapter 是统一的模型调用接口（Chat 流式 + 用量回调）；
//   - Message/ContentBlock 覆盖 text/tool_use/tool_result/image 四类内容块；
//   - StreamChunk 承载 text / reasoning / tool-call / done 流式分片；
//   - LlmFailure 对模型侧错误做稳定分类（overload / rate-limit / response-refusal /
//     context-overflow），供重试（S15）与 request-error 瀑布（M34）消费。
package llm

import (
	"context"
	"fmt"
)

// ============================================================================
// 消息与内容块
// ============================================================================

// Role 是消息角色。
type Role string

// 消息角色枚举。
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ContentBlockKind 是内容块类型。
type ContentBlockKind string

// 内容块类型枚举。
const (
	BlockText    ContentBlockKind = "text"
	BlockToolUse ContentBlockKind = "tool_use"
	BlockToolResult ContentBlockKind = "tool_result"
	BlockImage   ContentBlockKind = "image"
)

// ToolCall 是一次工具调用声明（模型侧）。
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResultContent 是一次工具结果内容块。
type ToolResultContent struct {
	ToolUseID string `json:"toolUseId"`
	Content   string `json:"content"`
	IsError   bool   `json:"isError,omitempty"`
}

// ImageContent 是图片内容块（URL 或 Attachment 引用）。
type ImageContent struct {
	URL        string `json:"url,omitempty"`
	Attachment string `json:"attachment,omitempty"`
}

// ContentBlock 是一条消息内的内容块（Derived Union）。
type ContentBlock struct {
	Kind       ContentBlockKind   `json:"kind"`
	Text       string             `json:"text,omitempty"`
	ToolCall   *ToolCall          `json:"toolCall,omitempty"`
	ToolResult *ToolResultContent `json:"toolResult,omitempty"`
	Image      *ImageContent      `json:"image,omitempty"`
}

// Text 构造文本内容块。
func Text(s string) ContentBlock {
	return ContentBlock{Kind: BlockText, Text: s}
}

// ToolUse 构造工具调用内容块。
func ToolUse(tc *ToolCall) ContentBlock {
	return ContentBlock{Kind: BlockToolUse, ToolCall: tc}
}

// ToolResult 构造工具结果内容块。
func ToolResult(tr *ToolResultContent) ContentBlock {
	return ContentBlock{Kind: BlockToolResult, ToolResult: tr}
}

// Image 构造图片内容块。
func Image(img *ImageContent) ContentBlock {
	return ContentBlock{Kind: BlockImage, Image: img}
}

// Message 是一条对话消息（角色 + 内容块列表）。
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// NewUserMessage 便捷构造用户文本消息。
func NewUserMessage(text string) Message {
	return Message{Role: RoleUser, Content: []ContentBlock{Text(text)}}
}

// NewAssistantText 便捷构造助手文本消息。
func NewAssistantText(text string) Message {
	return Message{Role: RoleAssistant, Content: []ContentBlock{Text(text)}}
}

// ============================================================================
// 工具 Schema
// ============================================================================

// ToolSchema 是提供给模型的工具定义（OpenAI function 风格）。
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ============================================================================
// 流式分片与失败分类
// ============================================================================

// StreamChunkKind 是流式分片类型。
type StreamChunkKind string

// 流式分片类型枚举。
const (
	ChunkText      StreamChunkKind = "text"
	ChunkReasoning StreamChunkKind = "reasoning"
	ChunkToolCall  StreamChunkKind = "tool-call"
	ChunkDone      StreamChunkKind = "done"
)

// StreamChunk 是单次流式回调的分片。
type StreamChunk struct {
	Kind      StreamChunkKind `json:"kind"`
	Text      string          `json:"text,omitempty"`
	Reasoning string          `json:"reasoning,omitempty"`
	ToolCall  *ToolCall       `json:"toolCall,omitempty"`
}

// Usage 是模型请求的 token 用量（含缓存命中统计，供 N 簇探针使用）。
type Usage struct {
	PromptTokens         int `json:"promptTokens"`
	CompletionTokens     int `json:"completionTokens"`
	PromptCacheHitTokens int `json:"promptCacheHitTokens,omitempty"`
	PromptCacheMissTokens int `json:"promptCacheMissTokens,omitempty"`
}

// ============================================================================
// LlmFailure 分类错误
// ============================================================================

// LlmFailureKind 是模型失败的稳定分类。
type LlmFailureKind string

// 失败分类枚举。
const (
	// FailOverload 服务过载（建议重试）。
	FailOverload LlmFailureKind = "overload"
	// FailRateLimit 速率受限（建议退避重试）。
	FailRateLimit LlmFailureKind = "rate-limit"
	// FailResponseRefusal 模型拒绝响应（不可重试）。
	FailResponseRefusal LlmFailureKind = "response-refusal"
	// FailContextOverflow 上下文超长（需压缩）。
	FailContextOverflow LlmFailureKind = "context-overflow"
)

// LlmFailure 是携带稳定分类的模型调用失败。
type LlmFailure struct {
	Kind    LlmFailureKind `json:"kind"`
	Message string         `json:"message"`
	// Cause 携带底层根因（例如 context.DeadlineExceeded / *net.OpError 等），
	// 便于调用方 errors.Is 进行诊断；JSON 序列化时不输出（避免循环引用或敏感信息泄露）。
	// H03 新增：LLM provider 在 ctx 取消 / 网络错误时统一填 Cause，
	// 调用方 errors.Is(f, context.Canceled) 即可区分"用户取消"和"真正网络故障"。
	Cause error `json:"-"`
}

// Error 实现 error 接口。
func (e *LlmFailure) Error() string {
	return fmt.Sprintf("llm failure [%s]: %s", e.Kind, e.Message)
}

// Unwrap 支持 errors.Is / errors.As 沿 Cause 链下钻（H03 + Go 1.13+ 错误链）。
func (e *LlmFailure) Unwrap() error { return e.Cause }

// ClassifyLlmError 将任意 error 归类为 LlmFailure；无法归类时返回 unknown。
func ClassifyLlmError(err error) *LlmFailure {
	if err == nil {
		return nil
	}
	if f, ok := err.(*LlmFailure); ok {
		return f
	}
	// 兜底：归为 unknown 并保留原始消息
	return &LlmFailure{Kind: "unknown", Message: err.Error()}
}

// ============================================================================
// 请求与适配器接口
// ============================================================================

// ChatRequest 是一次模型对话请求。
type ChatRequest struct {
	// Model 模型名（如 deepseek-chat）。
	Model string `json:"model"`
	// Messages 对话消息。
	Messages []Message `json:"messages"`
	// System 系统提示词（独立字段便于 provider 组装）。
	System string `json:"system,omitempty"`
	// Tools 可用工具定义。
	Tools []ToolSchema `json:"tools,omitempty"`
	// Temperature 采样温度。
	Temperature float64 `json:"temperature,omitempty"`
	// MaxTokens 最大生成长度。
	MaxTokens int `json:"maxTokens,omitempty"`
}

// LLMAdapter 是统一的模型调用适配器接口。
type LLMAdapter interface {
	// Name 返回适配器名（如 "deepseek"）。
	Name() string
	// Chat 发起流式对话；每个分片通过 cb 回调，最终返回用量。
	Chat(ctx context.Context, req ChatRequest, cb func(StreamChunk)) (Usage, error)
}
