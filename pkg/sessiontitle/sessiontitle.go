// Package sessiontitle 提供会话标题生成能力（任务 S08：Session Title）。
//
// 对齐上游：packages/storage/session-title
//
// 会话标题的派生路径（latest-wins fold）已在 M05 中通过 session/session-title 事件
// 与 FoldSessionTitle 投影实现。本包补齐「标题怎么算出来」这部分：
//   - Fallback：从首条用户消息内容取前缀（默认前 30 个 Unicode 字符）作为标题；
//   - LLM Helper：可选地注入一个 LLMAdapter，让模型基于首条消息生成更精炼的标题；
//     LLM 失败或未启用时回退到 Fallback，保证标题永不缺失。
//
// 设计要点：
//   - 按 rune（Unicode 码点）截断而非按 byte，避免把多字节字符切断成乱码；
//   - 只依赖首条 user 消息，与事件折叠解耦，可独立单测；
//   - Generate 返回 {Title, Source}，Source 标明标题来自 "llm" 还是 "fallback"，
//     便于上层写入 session/title 事件时知晓来源。
package sessiontitle

import (
	"context"
	"strings"

	"github.com/JopenChen/dsh-go/pkg/llm"
)

// ============================================================================
// 常量与默认值
// ============================================================================

// defaultMaxRunes 是 fallback 标题的最大字符数（对齐上游 30 字）。
const defaultMaxRunes = 30

// Source 标题来源。
type Source string

// 标题来源枚举。
const (
	SourceLLM      Source = "llm"      // 由 LLM helper 生成
	SourceFallback Source = "fallback" // 由前缀截断生成
)

// ============================================================================
// 截断工具
// ============================================================================

// truncateRunes 将 s 按 Unicode 码点截断为至多 max runes；超出部分以「…」结尾。
// 截断在 rune 边界进行，保证不会把多字节 UTF-8 字符合并切坏。
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "…"
}

// Fallback 从首条用户消息内容生成前缀标题（默认 30 字）。
// 空内容返回空串，由调用方决定是否落 session/title。
func Fallback(firstUserMessage string) string {
	return FallbackMax(firstUserMessage, defaultMaxRunes)
}

// FallbackMax 按指定的最大字符数生成前缀标题。
func FallbackMax(firstUserMessage string, maxRunes int) string {
	return truncateRunes(firstUserMessage, maxRunes)
}

// ============================================================================
// Generator：标题生成器（LLM helper + fallback）
// ============================================================================

// Generator 依据是否启用 LLM helper 生成会话标题。
type Generator struct {
	// LLM 可选（nil 时强制走 fallback）。启用后模型基于首条用户消息提练标题。
	LLM llm.LLMAdapter
	// Model 显式指定用于标题生成的模型名；为空时由适配器默认决定。
	Model string
	// MaxRunes fallback 标题最大字符数；<=0 使用默认 30。
	MaxRunes int
	// System 传给 LLM 的系统提示词（可选，默认使用内置提练提示词）。
	System string
	// MaxTokens LLM 单次标题生成的最大 token 数。
	MaxTokens int
}

// Result 是一次标题生成的结果。
type Result struct {
	Title  string `json:"title"`
	Source Source `json:"source"`
}

// FallbackSystemPrompt 内置的标题提练系统提示词：要求模型给出一句话精炼标题。
var FallbackSystemPrompt = "你是一名标题提炼助手。根据用户的输入，用一句不超过 15 个字的中文概括其意图，只输出标题本身，不要引号、不要解释。"

// maxRunes 返回生效的 fallback 截断长度。
func (g *Generator) maxRunes() int {
	if g == nil || g.MaxRunes <= 0 {
		return defaultMaxRunes
	}
	return g.MaxRunes
}

// fallbackSystem 返回生效的 LLM 系统提示词。
func (g *Generator) fallbackSystem() string {
	if g == nil || g.System == "" {
		return FallbackSystemPrompt
	}
	return g.System
}

// Generate 生成标题。优先调用 LLM helper；LLM 不可用或失败时回退到前缀截断。
func (g *Generator) Generate(ctx context.Context, firstUserMessage string) Result {
	// 先算 fallback（无论 LLM 是否启用，都作为兜底标题）。
	fallback := FallbackMax(firstUserMessage, g.maxRunes())

	if g == nil || g.LLM == nil || strings.TrimSpace(firstUserMessage) == "" {
		return Result{Title: fallback, Source: SourceFallback}
	}

	title, err := g.generateFromLLM(ctx, firstUserMessage)
	if err != nil || strings.TrimSpace(title) == "" {
		// LLM 失败或产出为空 → 回退到前缀标题，保证标题永不缺失。
		return Result{Title: fallback, Source: SourceFallback}
	}
	return Result{Title: truncateRunes(title, g.maxRunes()), Source: SourceLLM}
}

// generateFromLLM 通过 LLM 生成标题；只聚合 text 分片并做基本清理。
func (g *Generator) generateFromLLM(ctx context.Context, firstUserMessage string) (string, error) {
	req := llm.ChatRequest{
		Model:       g.Model,
		System:      g.fallbackSystem(),
		Messages:    []llm.Message{llm.NewUserMessage(firstUserMessage)},
		MaxTokens:   g.MaxTokens,
		Temperature: 0.2, // 标题要求稳定，低温采样。
	}
	var sb strings.Builder
	_, err := g.LLM.Chat(ctx, req, func(c llm.StreamChunk) {
		if c.Kind == llm.ChunkText {
			sb.WriteString(c.Text)
		}
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(TrimTitle(sb.String())), nil
}

// TrimTitle 清理 LLM 产出的标题：去掉首尾引号、句点等（可选加固）。
func TrimTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'「」『』《》【】。，、；：！？. ")
	return s
}