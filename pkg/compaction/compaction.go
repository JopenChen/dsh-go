// Package compaction 提供长对话压缩（任务 S01：Compaction LLM 摘要 + Surface Replace）。
//
// 对齐上游：packages/compaction/compaction + compaction-engine-basic
//
// 设计要点：
//   - 触发：当事件文本(token)预算超过阈值(config.MaxTokens)时，压缩最老的 surface；
//   - 替换：生成一条 assistant/message 摘要事件，携带 SurfaceOp{replace start..end}
//     （复用 M21 的读时表面替换，源事件保持 append-only，不修改历史）；
//   - 续航：压缩后事件列表仍可继续追加新事件（turn 继续走），且 request/header 重建
//     只需「有效事件」，无需原始被替换事件；
//   - 摘要引擎：Summarizer 接口抽象；内置 BasicEngine（无 LLM，做前缀归纳）与
//     LLMEngine（可选，注入 llm.LLMAdapter 生成更精炼摘要）。
package compaction

import (
	"context"
	"fmt"
	"time"

	"github.com/JopenChen/dsh-go/pkg/session"
)

// ============================================================================
// 常量与配置
// ============================================================================

// defaultMaxTokens 默认触发压缩的 token 阈值（估算）。
const defaultMaxTokens = 20000

// defaultKeepTail 默认保留最近的消息条数（不做压缩）。
const defaultKeepTail = 10

// defaultSummaryMaxRunes 默认单条摘要最大字符数。
const defaultSummaryMaxRunes = 800

// Config 是压缩器的配置。
type Config struct {
	MaxTokens       int // 事件文本 token 估算超此值触发压缩
	KeepTail        int // 保留最近消息条数（不被替换）
	SummaryMaxRunes int // 摘要最大字符数
}

// WithDefaults 填充默认值后返回副本。
func (c Config) WithDefaults() Config {
	if c.MaxTokens <= 0 {
		c.MaxTokens = defaultMaxTokens
	}
	if c.KeepTail <= 0 {
		c.KeepTail = defaultKeepTail
	}
	if c.SummaryMaxRunes <= 0 {
		c.SummaryMaxRunes = defaultSummaryMaxRunes
	}
	return c
}

// ============================================================================
// 摘要引擎抽象
// ============================================================================

// Summarizer 把一段「将被替换」的旧事件归纳为摘要文本。
type Summarizer interface {
	// Summarize 返回归纳后的摘要字符串。
	Summarize(ctx context.Context, events []session.SessionEvent, maxRunes int) (string, error)
}

// ============================================================================
// 工具：token 估算与触发判断
// ============================================================================

// tokenEstimate 粗略估算单条事件的 token 数（中文按字符/1.5、拉丁按 word 估算）。
func tokenEstimate(ev session.SessionEvent) int {
	var text string
	switch d := ev.Data.(type) {
	case session.UserMessageData:
		text = d.Content
	case session.AssistantMessageData:
		text = d.Content
	case session.SessionTitleData:
		text = d.Title
	case session.InjectionContextData:
		text = d.Content
	default:
		return 0
	}
	runes := len([]rune(text))
	return runes/2 + 1 // 中文近似
}

// EstimateTokens 估算事件列表的总 token 数（用于触发判断）。
func EstimateTokens(events []session.SessionEvent) int {
	total := 0
	for _, ev := range events {
		total += tokenEstimate(ev)
	}
	return total
}

// ShouldCompact 判断是否需要压缩。
func ShouldCompact(events []session.SessionEvent, maxTokens int) bool {
	if maxTokens <= 0 {
		return false
	}
	return EstimateTokens(events) > maxTokens
}

// ============================================================================
// Result
// ============================================================================

// Result 是一次压缩的输出。
type Result struct {
	Start     uint64                // 被替换范围起始 seq（含）
	End       uint64                // 被替换范围终止 seq（含）
	Summary   string                // 摘要文本
	Replacement session.SessionEvent // 新生成的摘要替换事件（含 SurfaceOp）
	// Effective 为压缩后的有效事件列表（= 原始列表 + 追加的 Replacement）
	Effective []session.SessionEvent
}

// ============================================================================
// Compactor
// ============================================================================

// Compactor 驱动 "触发判断 → 选取替换范围 → 摘要 → 生成替换事件" 全流程。
type Compactor struct {
	cfg       Config
	summarizer Summarizer
}

// New 创建压缩器。summarizer 为 nil 时使用 BasicEngine。
func New(cfg Config, summarizer Summarizer) *Compactor {
	cfg = cfg.WithDefaults()
	if summarizer == nil {
		summarizer = &BasicEngine{}
	}
	return &Compactor{cfg: cfg, summarizer: summarizer}
}

// ShouldCompact 便捷判断（用配置阈值）。
func (c *Compactor) ShouldCompact(events []session.SessionEvent) bool {
	return ShouldCompact(events, c.cfg.MaxTokens)
}

// Compact 对事件列表执行一次压缩：把最老的（保留最近 KeepTail 条消息）summarize 掉，
// 生成一条 assistant/message 替换事件（SurfaceOp replace 该范围），并返回追加后的有效列表。
func (c *Compactor) Compact(ctx context.Context, events []session.SessionEvent) (Result, error) {
	if len(events) == 0 {
		return Result{}, fmt.Errorf("compaction: empty events")
	}
	if !c.ShouldCompact(events) {
		return Result{}, fmt.Errorf("compaction: below token threshold, no need to compact")
	}

	// 计算最近保留的消息数 → 确定替换范围右边界（保留最近 KeepTail 条用户+助手消息）。
	keepTail := c.cfg.KeepTail * 2 // 近似：每条消息含 user(提问) + assistant(回答)
	if keepTail >= len(events) {
		keepTail = len(events) - 1
		if keepTail < 1 {
			keepTail = 0
		}
	}
	keepIndex := len(events) - keepTail // 保留 events[keepIndex...]，替换 [0, keepIndex-1]

	start := events[0].Seq
	end := events[keepIndex-1].Seq
	if keepIndex == 0 {
		// 边界：全部压缩（保留 0 条），令 end = 最后一个可替换 seq。
		end = events[len(events)-1].Seq
	}

	// 归纳摘要：压缩被替换的旧事件。
	oldForSummary := events[:keepIndex]
	summary, err := c.summarizer.Summarize(ctx, oldForSummary, c.cfg.SummaryMaxRunes)
	if err != nil {
		return Result{}, err
	}
	// 摘要长度受 maxRunes 兜底（过长截断）。
	runes := []rune(summary)
	if len(runes) > c.cfg.SummaryMaxRunes {
		summary = string(runes[:c.cfg.SummaryMaxRunes])
	}

	// 生成替换事件：assistant/message + SurfaceOp replace [start,end]
	lastSeq := events[len(events)-1].Seq
	replacement := session.SessionEvent{
		Seq:       lastSeq + 1,
		Time:      time.Now(),
		Type:      session.EventAssistantMessage,
		Data:      session.AssistantMessageData{Content: summary},
		SurfaceOp: session.NewReplaceOp(start, end, nil),
	}
	// 源事件不变（append-only）：追加替换事件即完成压缩。
	effective := append(append([]session.SessionEvent(nil), events...), replacement)

	return Result{
		Start:     start,
		End:       end,
		Summary:   summary,
		Replacement: replacement,
		Effective: effective,
	}, nil
}