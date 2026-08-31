// 本文件验证任务 S01：Compaction（LLM 摘要 + Surface Replace）。
//
// 覆盖：超阈值触发 → 生成 assistant/message 摘要事件带 SurfaceOp replace 范围；
// 压缩后有效消息数下降、turn 可继续追加新消息；FoldAll 只依赖有效事件即可重建
// （request/header 重建不需原始被替换事件）。
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/compaction"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// buildChatEvents 构造 n 轮 user/assistant 交替消息。
func buildChatEvents(n int) []session.SessionEvent {
	evs := make([]session.SessionEvent, 0, n*2)
	seq := uint64(1)
	for i := 0; i < n; i++ {
		evs = append(evs, session.SessionEvent{Seq: seq, Time: fixedTestTime(), Type: session.EventUserMessage,
			Data: session.UserMessageData{Content: "与用户对话的第 N 轮问题需要足够长以便计入 token 预算 QQQQ"}})
		seq++
		evs = append(evs, session.SessionEvent{Seq: seq, Time: fixedTestTime(), Type: session.EventAssistantMessage,
			Data: session.AssistantMessageData{Content: "助手回答：这是为支撑 token 预算触发的较长回复文本 AAAA"}})
		seq++
	}
	return evs
}

// TestCompactionTriggersAndReplaces 验证超阈值触发并生成替换事件，消息数下降。
func TestCompactionTriggersAndReplaces(t *testing.T) {
	ctx := context.Background()
	events := buildChatEvents(60) // 120 条事件
	cp := compaction.New(compaction.Config{MaxTokens: 40, KeepTail: 2, SummaryMaxRunes: 200}, nil)

	if !cp.ShouldCompact(events) {
		t.Fatal("事件应超过 token 阈值触发压缩")
	}
	res, err := cp.Compact(ctx, events)
	if err != nil {
		t.Fatal(err)
	}
	if res.Start > res.End {
		t.Fatalf("替换范围非法 start=%d end=%d", res.Start, res.End)
	}
	if res.Summary == "" {
		t.Fatal("摘要不应为空")
	}
	newSeq := events[len(events)-1].Seq + 1
	if res.Replacement.Seq != newSeq {
		t.Fatalf("替换事件 seq 应为 %d，实际 %d", newSeq, res.Replacement.Seq)
	}
	if res.Replacement.SurfaceOp == nil {
		t.Fatal("替换事件必须携带 SurfaceOp")
	}

	// 压缩后有效消息数：被替换大部分 + 保留最近 2 轮(4条) + 摘要1条 < 原 60 条消息。
	before := len(session.DeriveMessages(events))
	after := len(session.DeriveMessages(res.Effective))
	if after >= before {
		t.Fatalf("压缩后有效消息数应下降：before=%d after=%d", before, after)
	}
}

// TestCompactionTurnContinues 验证压缩后还能追加新消息（turn 继续走）。
func TestCompactionTurnContinues(t *testing.T) {
	ctx := context.Background()
	events := buildChatEvents(40)
	cp := compaction.New(compaction.Config{MaxTokens: 30, KeepTail: 2, SummaryMaxRunes: 200}, nil)
	res, err := cp.Compact(ctx, events)
	if err != nil {
		t.Fatal(err)
	}
	effective := res.Effective

	// 追加一条新用户消息（模拟压缩后继续对话）。
	nextSeq := effective[len(effective)-1].Seq + 1
	effective = append(effective, session.SessionEvent{Seq: nextSeq, Time: fixedTestTime(),
		Type: session.EventUserMessage, Data: session.UserMessageData{Content: "压缩之后的新一轮问题 PPPP"}})

	// 派生消息应包含最后这条新消息 → turn 正常延续。
	msgs := session.DeriveMessages(effective)
	last := msgs[len(msgs)-1]
	if last.Role != "user" || last.Content != "压缩之后的新一轮问题 PPPP" {
		t.Fatalf("压缩后新消息应可派生，实际最后一条=%+v", last)
	}
}

// TestCompactionHeaderRebuildNoRawEvents 验证 request/header 重建不需原始被替换事件。
func TestCompactionHeaderRebuildNoRawEvents(t *testing.T) {
	ctx := context.Background()
	events := buildChatEvents(40)
	cp := compaction.New(compaction.Config{MaxTokens: 30, KeepTail: 2, SummaryMaxRunes: 200}, nil)
	res, err := cp.Compact(ctx, events)
	if err != nil {
		t.Fatal(err)
	}
	effective := res.Effective

	// 只用有效事件(FoldAll internally 走 EffectiveEvents → 取代被替换范围)即可重建全部派生状态。
	proj := session.FoldAll(effective)
	if len(proj.Messages) == 0 {
		t.Fatal("压缩后应至少仍有派生消息(摘要+保留)")
	}
	// 验证没有把被替换的原始 seq 当成消息（即 msg 不含最老那一轮标记）。
	for _, m := range proj.Messages {
		if m.Seq <= res.Start {
			t.Fatalf("被替换范围 seq<=%d 的原始消息不应再出现在派生消息中，实际 seq=%d", res.Start, m.Seq)
		}
	}
}

// TestCompactionBelowThresholdNoOp 验证低于阈值时不压缩。
func TestCompactionBelowThresholdNoOp(t *testing.T) {
	ctx := context.Background()
	events := buildChatEvents(3) // 6 条事件，很轻
	cp := compaction.New(compaction.Config{MaxTokens: 10000, KeepTail: 2, SummaryMaxRunes: 200}, nil)
	if cp.ShouldCompact(events) {
		t.Fatal("低负载不应触发压缩")
	}
	if _, err := cp.Compact(ctx, events); err == nil {
		t.Fatal("低于阈值调用 Compact 应返回错误（无需压缩）")
	}
}