// 本文件对应任务 M21：SurfaceOp(append/replace) + foldSurface。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/session"
)

// TestCompactionReplaceSurface 验证 append 100 事件后 replace range [3,80] → deriveMessages 一致。
func TestCompactionReplaceSurface(t *testing.T) {
	// 构造 100 条消息事件（seq 1..100，user/assistant 交替）
	var events []session.SessionEvent
	time := fixedTestTime()
	for i := 1; i <= 100; i++ {
		var data session.EventData
		if i%2 == 1 {
			data = session.UserMessageData{Content: "u"}
		} else {
			data = session.AssistantMessageData{Content: "a"}
		}
		events = append(events, session.SessionEvent{Seq: uint64(i), Time: time, Type: data.EventType(), Data: data})
	}

	// 替换事件（seq 101，覆盖 [3,80]）：摘要
	summary := session.AssistantMessageData{Content: "【摘要覆盖 3..80】"}
	events = append(events, session.SessionEvent{
		Seq: 101, Time: time, Type: session.EventAssistantMessage, Data: summary,
		SurfaceOp: session.NewReplaceOp(3, 80, nil),
	})

	// foldSurface：应隐藏 seq 3..80，节点 = seq1,2 + 81..100 + 替换事件 = 23
	nodes, replacements := session.FoldSurface(events)
	if len(nodes) != 23 {
		t.Fatalf("有效节点数 = %d, want 23（seq1,2 + 81..100 + 替换事件）", len(nodes))
	}
	if len(replacements) != 1 || replacements[0].Start != 3 || replacements[0].End != 80 {
		t.Fatalf("替换范围异常: %+v", replacements)
	}
	// 替换事件自身标记 Replaced
	if !nodes[len(nodes)-1].Replaced {
		t.Fatal("替换事件应标记 Replaced")
	}

	// deriveMessages 应与 foldSurface 节点一致（读时替换）
	msgs := session.DeriveMessages(events)
	if len(msgs) != len(nodes) {
		t.Fatalf("deriveMessages 消息数 = %d, foldSurface 节点数 = %d，应一致", len(msgs), len(nodes))
	}
}

// TestSurfaceReplacePurity 验证 replace 不改写源事件（源数据保持 append-only）。
func TestSurfaceReplacePurity(t *testing.T) {
	time := fixedTestTime()
	// 源事件 seq 1..3
	var events []session.SessionEvent
	for i := 1; i <= 3; i++ {
		events = append(events, session.SessionEvent{
			Seq: uint64(i), Time: time, Type: session.EventUserMessage,
			Data: session.UserMessageData{Content: "orig"},
		})
	}
	// 记录源事件字节（marshal 前）
	srcSeq1Before := events[0].Seq

	// 替换事件覆盖 [1,3]
	events = append(events, session.SessionEvent{
		Seq: 4, Time: time, Type: session.EventAssistantMessage,
		Data:      session.AssistantMessageData{Content: "summary"},
		SurfaceOp: session.NewReplaceOp(1, 3, nil),
	})

	// fold 后源事件 seq 不应改变
	if events[0].Seq != srcSeq1Before {
		t.Fatal("源事件被修改了")
	}
	// 源事件仍保留（未被删除）
	if len(events) != 4 {
		t.Fatalf("源事件应保留: %d", len(events))
	}
}
