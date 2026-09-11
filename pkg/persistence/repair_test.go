package persistence

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

func TestRepairOrphanTurnFull(t *testing.T) {
	events := []session.SessionEvent{
		{Seq: 1, Type: session.EventTurnStart, Data: session.TurnStartData{Turn: 0}},
		{Seq: 2, Type: session.EventStepStart, Data: session.StepStartData{Turn: 0, Step: 1}},
		{Seq: 3, Type: session.EventAssistantMessage,
			Data: session.AssistantMessageData{Content: "", ToolCallIDs: []string{"c1"}}},
		{Seq: 4, Type: session.EventToolCall,
			Data: session.ToolCallData{CallID: brand.NewToolCallID("c1"), Tool: "bash"}},
	}
	n := repairOrphanTurn(&events)
	// 应补 tool/result + step/end + turn/end 共 3 条。
	if n != 3 {
		t.Fatalf("repaired = %d want 3", n)
	}
	if len(events) != 7 {
		t.Fatalf("total events = %d want 7", len(events))
	}
	last := events[len(events)-1]
	if last.Type != session.EventTurnEnd {
		t.Fatalf("last must be turn/end, got %s", last.Type)
	}
}

func TestRepairBalancedNoop(t *testing.T) {
	events := []session.SessionEvent{
		{Seq: 1, Type: session.EventTurnStart, Data: session.TurnStartData{Turn: 0}},
		{Seq: 2, Type: session.EventTurnEnd, Data: session.TurnEndData{Turn: 0}},
	}
	if n := repairOrphanTurn(&events); n != 0 {
		t.Fatalf("balanced log must not repair, got %d", n)
	}
}
