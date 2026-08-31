// 本文件实现 compaction 的 BasicEngine（任务 S01 引擎库）。
//
// BasicEngine 是不依赖 LLM 的确定性摘要引擎：从被压缩的旧事件中抽取用户消息，
// 拼接为一段「过去对话摘要」，并受 maxRunes 约束截断。用于无 LLM 键或离线场景，
// 保证压缩永远可用；有 LLM 时可用 LLMEngine 替换获得更精炼摘要。
package compaction

import (
	"context"
	"sort"
	"strings"

	"github.com/JopenChen/dsh-go/pkg/session"
)

// BasicEngineSumm 是 BasicEngine 生成的摘要标题/前缀。
const BasicEngineSumm = "【摘要】过去对话摘要如下："

// BasicEngine 是一个确定性的摘要引擎（无 LLM）。
type BasicEngine struct{}

// Summarize 抽取旧事件中的用户消息，前若干条作为摘要；超长截断到 maxRunes。
func (b *BasicEngine) Summarize(ctx context.Context, events []session.SessionEvent, maxRunes int) (string, error) {
	_ = ctx
	// 收集用户消息并保持原顺序（按 seq）。
	type pair struct {
		seq uint64
		msg string
	}
	msgs := []pair{}
	// 先复制避免改动输入。
	cp := append([]session.SessionEvent(nil), events...)
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].Seq < cp[j].Seq })
	for _, ev := range cp {
		if ev.Type == session.EventUserMessage {
			if d, ok := ev.Data.(session.UserMessageData); ok {
				msgs = append(msgs, pair{ev.Seq, d.Content})
			}
		}
	}
	// 最多取前 3 条作为代表。
	const maxItems = 3
	var sb strings.Builder
	sb.WriteString(BasicEngineSumm)
	for i, m := range msgs {
		if i >= maxItems {
			break
		}
		sb.WriteString(m.msg)
		if i < maxItems-1 && i < len(msgs)-1 {
			sb.WriteString("；")
		}
	}
	return truncateRunes(sb.String(), maxRunes), nil
}

// truncateRunes 按 rune 截断并加省略号。
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}