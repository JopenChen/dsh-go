// 本文件复刻官方 token-meter/src/estimate.ts：固定密度启发式 token 估算。
// 在真正分词之前，用"每 4 字符 1 token + 每块结构开销 + 每条消息 role 开销"的
// 统一口径，让计量服务与上下文投影对同一份内容算出相同数字。
package tokenmeter

import (
	"encoding/json"
	"math"

	"github.com/JopenChen/dsh-go/pkg/llm"
)

const (
	charsPerToken = 4 // 固定文本密度
	blockOverhead = 4 // 每块 JSON 框架/类型标签开销
	// RoleOverhead 每条消息 role 字段框架开销。
	RoleOverhead = 4
)

// ceilDiv 向上取整的字符→token 换算。
func ceilDiv(n int) int {
	return int(math.Ceil(float64(n) / float64(charsPerToken)))
}

// EstimateContent 按固定密度估算一组内容块的 token（含每块结构开销）。
func EstimateContent(blocks []llm.ContentBlock) int {
	tokens := 0
	for _, b := range blocks {
		switch b.Kind {
		case llm.BlockText, llm.BlockReasoning:
			tokens += ceilDiv(len(b.Text)) + blockOverhead
		case llm.BlockToolUse:
			if b.ToolCall != nil {
				tokens += ceilDiv(len(b.ToolCall.Name))
				if raw, err := json.Marshal(b.ToolCall.Input); err == nil {
					tokens += ceilDiv(len(raw))
				}
			}
			tokens += blockOverhead
		case llm.BlockToolResult:
			if b.ToolResult != nil {
				// 结果内容是字符串，按文本密度估算。
				tokens += ceilDiv(len(b.ToolResult.Content))
			}
			tokens += blockOverhead
		default:
			// 图片等未知块：保守按 JSON 结构估算。
			if raw, err := json.Marshal(b); err == nil {
				tokens += blockOverhead + ceilDiv(len(raw))
			} else {
				tokens += blockOverhead
			}
		}
	}
	return tokens
}

// EstimateMessage 估算一条模型可见消息（内容 + role 框架）。
func EstimateMessage(msg llm.Message) int {
	return EstimateContent(msg.Content) + RoleOverhead
}

// EstimateMessages 估算整段对话历史。
func EstimateMessages(msgs []llm.Message) int {
	total := 0
	for _, m := range msgs {
		total += EstimateMessage(m)
	}
	return total
}
