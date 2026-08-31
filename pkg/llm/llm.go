// Package llm 提供 LLM Provider 接缝与流式协议类型。
//
// 对齐上游：packages/llm/llm + providers
//
// 设计要点：
//   - LLMAdapter 是统一的模型调用接口（Chat 流式 + 用量回调）；
//   - Message/ContentBlock 覆盖 text/tool_use/tool_result/image 四类内容块；
//   - StreamChunk 承载 text / reasoning / tool-call / done 流式分片；
//   - LlmFailure 对模型侧错误做稳定分类（overload / rate-limit / response-refusal /
//     context-overflow / quota / empty-response / invalid-credential），供重试（S15）与
//     request-error 瀑布（M34）消费；分类器对齐官方 error.ts 的 provider 文本判别语义。
package llm

import (
	"context"
	"fmt"
	"regexp"
	"strings"
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
//
// 对应官方 packages/llm/llm/src/error.ts 的稳定 HarnessError.code：
// CONTEXT_WINDOW_EXCEEDED / QUOTA / EMPTY_RESPONSE / INVALID_CREDENTIAL /
// RATE_LIMIT / (overload) 等。路由一律按 Kind（稳定串），绝不解析 message 文本。
const (
	// FailOverload 服务过载（建议重试）。
	FailOverload LlmFailureKind = "overload"
	// FailRateLimit 速率受限（建议退避重试）。
	FailRateLimit LlmFailureKind = "rate-limit"
	// FailResponseRefusal 模型拒绝响应（不可重试）。
	FailResponseRefusal LlmFailureKind = "response-refusal"
	// FailContextOverflow 上下文超长（需压缩）。
	FailContextOverflow LlmFailureKind = "context-overflow"
	// FailQuota 账户配额/余额耗尽（官方 code QUOTA）。终态，default retry 不重试。
	FailQuota LlmFailureKind = "quota"
	// FailEmptyResponse 响应正常结束但无任何内容块（官方 code EMPTY_RESPONSE）。
	// 退化完成（终态 stop 且 0 输出），不产生助手消息；可安全重试。
	FailEmptyResponse LlmFailureKind = "empty-response"
	// FailInvalidCredential 凭证格式非法（官方 code INVALID_CREDENTIAL）。
	// 区别于"缺失凭证"：修复方式为改贮存值而非补值；不在默认可重试集内。
	FailInvalidCredential LlmFailureKind = "invalid-credential"
)

// Retryable 返回该分类是否属于"可重试"（与官方默认 retryable 集对齐）：
//   - 可重试：overload / rate-limit / context-overflow（压缩后再试）/ empty-response；
//   - 不可重试：response-refusal / quota / invalid-credential 及未知。
func (f LlmFailureKind) Retryable() bool {
	switch f {
	case FailOverload, FailRateLimit, FailContextOverflow, FailEmptyResponse:
		return true
	default:
		return false
	}
}

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
// Provider 错误文本/明细分类器（对齐官方 packages/llm/llm/src/error.ts）
// ============================================================================

// providerDetail 匹配辅助：把 provider 的 error code/type/message/body 拼成的单串，
// 归一到小写后交给正则判别。官方 isContextWindowExceededError / isQuotaExceededError
// 即对 detail 做此类文本判别；Go 侧同样"分类靠判别器、路由靠稳定 Kind"。
var (
	// 上下文界限超限（对应官方 CONTEXT_WINDOW_EXCEEDED）。
	// 忠实复刻官方 error.ts：STRUCTURED_CONTEXT_OVERFLOW / maximum context (length|window) /
	// TOO_LARGE_FOR_CONTEXT / "too (long|large) for (this|the) model" 等模式族。
	reContextWindow = regexp.MustCompile(
		`(?:^|[^a-z0-9])context[\s_-](?:length|window)[\s_-](?:exceed(?:ed|s)?|overflow(?:ed)?|limit[\s_-]exceeded)(?:$|[^a-z0-9])` +
			`|(?:maximum|max)[\s_-]*(?:allowed|supported)?[\s_-]*context[\s_-]+(?:length|window)` +
			`|(?:^|[^a-z0-9])(?:prompt|input|request|messages?)[\s_-](?:too[\s_-](?:long|large)|exceed(?:ed)?)[\s_-]for[\s_-](?:the[\s_-])?(?:model)?(?:[^a-z0-9]|$)` +
			`|(?:^|[^a-z0-9])(?:input|prompt|request)[\s_-]+(?:is[\s_-]+)?too[\s_-](?:long|large)[\s_-]+for[\s_-]+(?:this|the)[\s_-]+model(?:[^a-z0-9]|$)`,
	)
	// 配额/余额/额度耗尽（对应官方 QUOTA）。
	reQuota = regexp.MustCompile(
		`insufficient[\s_-]+(?:quota|balance|credits?)` +
			`|(?:quota|usage[\s_-]+limit)[\s_-]+(?:exceeded|exhausted|reached)` +
			`|(?:out[\s_-]+of|not[\s_-]+enough)[\s_-]+(?:credits?|budget|quota|balance)` +
			`|don.t[\s_-]+have[\s_-]+enough[\s_-]*(?:api[\s_-]+)?credits?` +
			`|(?:balance|credits?)[\s_-]+(?:exhausted|depleted)`,
	)
	// 凭证非法/不匹配（对应官方 INVALID_CREDENTIAL）——区别于"缺失"。
	reInvalidCred = regexp.MustCompile(
		`(?:invalid|wrong|incorrect|bad|unauthorized|auth)[\s_-]+(?:api[\s_-]?key|credential|token|secret)` +
			`|(?:api[\s_-]?key|credential)[\s_-]+(?:invalid|revoked|mismatch)` +
			`|401[\s_-]*(?:invalid|authentication)`,
	)
	// 速率受限（对应官方 RATE_LIMIT）。
	reRateLimit = regexp.MustCompile(`rate[\s_-]*limit|too[\s_-]+many[\s_-]+requests|\b429\b`)
	// 空响应：正常终态但无任何内容（对应官方 EMPTY_RESPONSE）。
	reEmptyResponse = regexp.MustCompile(`empty[\s_-]+response|no[\s_-]+content|empty[\s_-]+completion|zero[\s_-]+output`)
	// 模型拒绝（对应响应拒绝）。
	reRefusal = regexp.MustCompile(`(?:refus|declin)[e\w]*|cannot[\s_-]+(?:answer|comply)|policy[\s_-]+(?:refus|declin)`)
)

// ClassifyProviderDetail 把 provider 提供的错误细节（code/type/message 拼接串，小写化前判别）
// 映射到稳定 LlmFailureKind；无法识别返回 ""（调用方可归为 unknown）。
// 语义忠实复刻官方 error.ts 的 isContextWindowExceededError / isQuotaExceededError 判别族。
func ClassifyProviderDetail(detail string) LlmFailureKind {
	d := strings.ToLower(strings.TrimSpace(detail))
	if d == "" {
		return ""
	}
	switch {
	case reContextWindow.MatchString(d):
		return FailContextOverflow
	case reQuota.MatchString(d):
		return FailQuota
	case reInvalidCred.MatchString(d):
		return FailInvalidCredential
	case reRateLimit.MatchString(d):
		return FailRateLimit
	case reEmptyResponse.MatchString(d):
		return FailEmptyResponse
	case reRefusal.MatchString(d):
		return FailResponseRefusal
	default:
		return ""
	}
}

// NewProviderFailure 根据 provider detail 构造 *LlmFailure：
//   - detail 无法识别 → Kind=unknown；
//   - 识别成功 → 对应稳定分类，Message 保留原始 detail 便于诊断。
func NewProviderFailure(detail string) *LlmFailure {
	kind := ClassifyProviderDetail(detail)
	if kind == "" {
		kind = "unknown"
	}
	return &LlmFailure{Kind: kind, Message: detail}
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
