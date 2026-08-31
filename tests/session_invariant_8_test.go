// Package tests 的 N02（D1 纪律）验收测试。
//
// 覆盖：
//   - 严格 append-only：SessionLog 仅暴露 Append() 一个写路径（Events 只读拷贝）
//   - 8 条不变量全部通过（正常日志）+ 人为注入反例被逐条捕获
//   - 50 轮正常对话后 seq 连续 + time 单调
package tests

import (
	"strings"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// buildHealthyLog 构造一段 8 条不变量全部通过的健康日志（含 turn/step/approval/goal/tool）。
func buildHealthyLog(t *testing.T) []session.SessionEvent {
	sl := session.NewSessionLog(brand.NewSessionID("n02"))
	_, _ = sl.Append(session.TurnStartData{})
	_, _ = sl.Append(session.StepStartData{StepSeq: 1})
	aid := brand.NewApprovalRequestID("ap1")
	_, _ = sl.Append(session.ApprovalRequestIDData{RequestID: aid, Tool: "bash"})
	_, _ = sl.Append(session.ApprovalDecidedData{RequestID: aid, Allowed: true})
	callID := brand.NewToolCallID("tc1")
	_, _ = sl.Append(session.ToolCallData{CallID: callID, Tool: "echo"})
	_, _ = sl.Append(session.ToolResultData{CallID: callID, Output: "hi"})
	_, _ = sl.Append(session.GoalChangeData{GoalID: "g1", Phase: session.GoalPhase("active"), Revision: 1})
	_, _ = sl.Append(session.GoalChangeData{GoalID: "g1", Revision: 2})
	_, _ = sl.Append(session.StepEndData{StepSeq: 1})
	_, _ = sl.Append(session.TurnEndData{Reason: session.ReasonFinished})
	return sl.Events()
}

// TestN02AllInvariantsPass 验证健康日志 8 条不变量全部通过。
func TestN02AllInvariantsPass(t *testing.T) {
	events := buildHealthyLog(t)
	if fails := session.VerifyInvariants(events); len(fails) != 0 {
		t.Fatalf("健康日志不应有不变量失败: %v", fails)
	}
}

// TestN02InvSeqGapCaptured 验证人为注入 seq 跳号被捕获。
func TestN02InvSeqGapCaptured(t *testing.T) {
	events := buildHealthyLog(t)
	// 改乱一个 seq：把 index 2 的 seq 改成 999（不重排，直接生成独立反例更直观）。
	corrupt := append([]session.SessionEvent{}, events...)
	corrupt[2].Seq = 999
	corrupt[2].Type = session.EventUserMessage // 避免类型与 seq 校验冲突；seq 校验只看非 surface 事件
	fails := session.VerifyInvariants(corrupt)
	if !hasInvariant(fails, session.InvSeqContinuous) {
		t.Fatalf("seq 跳号应被 seq_continuous 捕获: %v", fails)
	}
}

// TestN02InvTimeRewindCaptured 验证时间回退被捕获。
func TestN02InvTimeRewindCaptured(t *testing.T) {
	events := buildHealthyLog(t)
	corrupt := append([]session.SessionEvent{}, events...)
	corrupt[3].Time = corrupt[2].Time.Add(-time.Hour) // 回退
	fails := session.VerifyInvariants(corrupt)
	if !hasInvariant(fails, session.InvTimeMonotonic) {
		t.Fatalf("时间回退应被 time_monotonic 捕获: %v", fails)
	}
}

// TestN02InvUnpairedTurnCaptured 验证 turn 不配对被捕获。
func TestN02InvUnpairedTurnCaptured(t *testing.T) {
	// 只有一个 turn/start，无 turn/end。
	var events []session.SessionEvent
	ev := session.SessionEvent{Seq: 1, Time: time.Now(), Type: session.EventTurnStart, Data: session.TurnStartData{}}
	events = append(events, ev)
	fails := session.VerifyInvariants(events)
	if !hasInvariant(fails, session.InvTurnPaired) {
		t.Fatalf("未配对 turn 应被捕获: %v", fails)
	}
}

// TestN02InvGoalCASCaptured 验证 goal revision 跳号被捕获。
func TestN02InvGoalCASCaptured(t *testing.T) {
	events := buildHealthyLog(t)
	// 追加一个 revision 4 的 goal 变更（期望 3）。用一个全新日志追加会更清晰，这里直接扩 slice。
	extra := session.SessionEvent{Seq: 100, Time: time.Now().Add(time.Minute), Type: session.EventGoalChange,
		Data: session.GoalChangeData{GoalID: "g1", Revision: 4}}
	events = append(events, extra)
	fails := session.VerifyInvariants(events)
	if !hasInvariant(fails, session.InvGoalCAS) {
		t.Fatalf("goal revision 跳号应被 goal_revision_cas 捕获: %v", fails)
	}
}

// TestN02InvUnknownTypeCaptured 验证未知事件类型被格式不变量捕获。
func TestN02InvUnknownTypeCaptured(t *testing.T) {
	events := buildHealthyLog(t)
	corrupt := append([]session.SessionEvent{}, events...)
	corrupt[0].Type = session.EventType("bogus/type")
	fails := session.VerifyInvariants(corrupt)
	if !hasInvariant(fails, session.InvFormatConsistent) {
		t.Fatalf("未知类型应被 persistence_format_consistent 捕获: %v", fails)
	}
}

// TestN02FiftyRoundsSeqTime 验证 50 轮正常对话后 seq 连续 + time 单调。
func TestN02FiftyRoundsSeqTime(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("n02-50"))
	for i := 0; i < 50; i++ {
		_, _ = sl.Append(session.TurnStartData{})
		_, _ = sl.Append(session.StepStartData{StepSeq: 1})
		_, _ = sl.Append(session.UserMessageData{Content: "hi"})
		_, _ = sl.Append(session.AssistantMessageData{Content: "ok"})
		_, _ = sl.Append(session.StepEndData{StepSeq: 1})
		_, _ = sl.Append(session.TurnEndData{Reason: session.ReasonFinished})
	}
	events := sl.Events()
	if fails := session.VerifyInvariants(events); len(fails) != 0 {
		t.Fatalf("50 轮后不变量应通过: %v", fails)
	}
	// seq 1..300 连续。
	if len(events) != 300 || events[299].Seq != 300 {
		t.Fatalf("应 300 条事件且末 seq=300, 实际 %d/%d", len(events), events[len(events)-1].Seq)
	}
	// time 单调。
	for i := 1; i < len(events); i++ {
		if events[i].Time.Before(events[i-1].Time) {
			t.Fatalf("time 不单调 at %d", i)
		}
	}
}

// TestN02AppendOnlyContract 验证 SessionLog 只读视图不暴露写方法（Events 是拷贝）。
func TestN02AppendOnlyContract(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("ao"))
	_, _ = sl.Append(session.UserMessageData{Content: "a"})
	_, _ = sl.Append(session.UserMessageData{Content: "b"})
	// 取只读快照并尝试就地修改 → 不影响内部日志（拷贝）。
	events := sl.Events()
	events[0].Data = session.UserMessageData{Content: "HACK"}
	if sl.Len() != 2 {
		t.Fatal("操作只读拷贝不应改变 Len")
	}
	inner := sl.Events()
	if um, ok := inner[0].Data.(session.UserMessageData); !ok || um.Content != "a" {
		t.Fatal("就地改拷贝不应影响内部事件（严格 append-only)")
	}
}

func hasInvariant(fails []error, code session.InvariantCode) bool {
	for _, f := range fails {
		if strings.Contains(f.Error(), "["+string(code)+"]") {
			return true
		}
	}
	return false
}