// Package tests 的 ConsumedWork（consumed.go）验收测试。
//
// 覆盖：
//   - 进入 step 的 turn 被结算为 End
//   - claim 输入后 blocked/error 的 turn 被结算；completed 的 no-op 不结算
//   - canceled 且无插入 → DroppedUnrun；canceled 但有插入（替换）→ 不丢弃
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/agent"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// mkEvent 构造一条带类型化 data 的事件。
func mkEvent(seq uint64, t session.EventType, data session.EventData) session.SessionEvent {
	return session.SessionEvent{Seq: seq, Type: t, Data: data}
}

func TestConsumedWorkSteppedTurnIsAccounted(t *testing.T) {
	events := []session.SessionEvent{
		mkEvent(1, session.EventTurnStart, session.TurnStartData{Turn: 0}),
		mkEvent(2, session.EventStepStart, session.StepStartData{Turn: 0, Step: 1}),
		mkEvent(3, session.EventStepEnd, session.StepEndData{Turn: 0, Step: 1}),
		mkEvent(4, session.EventTurnEnd, session.TurnEndData{Turn: 0, Reason: session.ReasonCompleted}),
	}
	cw := agent.FoldConsumedWork(events)
	if cw.End == nil {
		t.Fatal("a turn that entered a step must be accounted as End")
	}
	if cw.DroppedUnrun {
		t.Fatal("no work should be dropped")
	}
}

func TestConsumedWorkClaimedBlockedAccounted(t *testing.T) {
	events := []session.SessionEvent{
		mkEvent(1, session.EventTurnStart, session.TurnStartData{Turn: 0}),
		mkEvent(2, session.EventAgentInboxSpliced, session.InboxSplicedData{
			Target: session.InboxNextTurn, RemovedCount: 1, Inserted: []string{},
		}),
		mkEvent(3, session.EventTurnEnd, session.TurnEndData{Turn: 0, Reason: session.ReasonBlocked}),
	}
	cw := agent.FoldConsumedWork(events)
	if cw.End == nil {
		t.Fatal("a turn that claimed input and was blocked must be accounted")
	}
}

func TestConsumedWorkClaimedCompletedNotAccounted(t *testing.T) {
	events := []session.SessionEvent{
		mkEvent(1, session.EventTurnStart, session.TurnStartData{Turn: 0}),
		mkEvent(2, session.EventAgentInboxSpliced, session.InboxSplicedData{
			Target: session.InboxNextTurn, RemovedCount: 1, Inserted: []string{},
		}),
		mkEvent(3, session.EventTurnEnd, session.TurnEndData{Turn: 0, Reason: session.ReasonCompleted}),
	}
	cw := agent.FoldConsumedWork(events)
	if cw.End != nil {
		t.Fatal("a completed no-op turn that never reached a step must NOT be accounted")
	}
}

func TestConsumedWorkCanceledNoInsertDropped(t *testing.T) {
	events := []session.SessionEvent{
		mkEvent(1, session.EventAgentInboxSpliced, session.InboxSplicedData{
			Target: session.InboxNextTurn, RemovedCount: 2, Inserted: []string{}, Outcome: "canceled",
		}),
	}
	cw := agent.FoldConsumedWork(events)
	if !cw.DroppedUnrun {
		t.Fatal("canceled with nothing inserted must mark DroppedUnrun")
	}
}

func TestConsumedWorkCanceledWithInsertKept(t *testing.T) {
	events := []session.SessionEvent{
		mkEvent(1, session.EventAgentInboxSpliced, session.InboxSplicedData{
			Target: session.InboxNextTurn, RemovedCount: 1, Inserted: []string{"replacement"}, Outcome: "canceled",
		}),
	}
	cw := agent.FoldConsumedWork(events)
	if cw.DroppedUnrun {
		t.Fatal("a replacement keeps work pending, must not mark DroppedUnrun")
	}
}

func TestConsumedWorkEmptyLog(t *testing.T) {
	cw := agent.FoldConsumedWork(nil)
	if cw.End != nil || cw.DroppedUnrun {
		t.Fatal("empty log yields no End and no drop")
	}
}
