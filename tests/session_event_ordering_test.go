// 本文件对应任务 M04：Session 事件溯源（append 严格不变量校验）。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// appendTurn 便捷构造一段完整合法 Turn（start → step → step/end → end）。
func appendTurn(sl *session.SessionLog) error {
	if _, err := sl.Append(session.TurnStartData{}); err != nil {
		return err
	}
	if _, err := sl.Append(session.StepStartData{StepSeq: 1}); err != nil {
		return err
	}
	if _, err := sl.Append(session.UserMessageData{Content: "hi"}); err != nil {
		return err
	}
	if _, err := sl.Append(session.StepEndData{StepSeq: 1}); err != nil {
		return err
	}
	_, err := sl.Append(session.TurnEndData{Reason: session.ReasonFinished})
	return err
}

// TestSessionAppendSeqContinuous 验证 seq 严格 1..N 连续。
func TestSessionAppendSeqContinuous(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("s1"))

	seqs := []uint64{}
	for i := 0; i < 5; i++ {
		seq, err := sl.Append(session.UserMessageData{Content: "msg"})
		if err != nil {
			t.Fatalf("Append 失败: %v", err)
		}
		seqs = append(seqs, seq)
	}
	for i, seq := range seqs {
		if seq != uint64(i+1) {
			t.Fatalf("seq 应连续 1..N, 第 %d 条 = %d", i+1, seq)
		}
	}
	if sl.LastSeq() != 5 || sl.Len() != 5 {
		t.Fatalf("LastSeq=%d Len=%d, want 5/5", sl.LastSeq(), sl.Len())
	}
}

// TestSessionTurnPairing 验证 turn/end 无前 turn/start 被拒绝。
func TestSessionTurnPairing(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("s1"))

	// turn/end 没有前置 turn/start → 拒绝
	if _, err := sl.Append(session.TurnEndData{Reason: session.ReasonFinished}); err == nil {
		t.Fatal("无 turn/start 的 turn/end 应被拒绝")
	}

	// 合法 turn
	if err := appendTurn(sl); err != nil {
		t.Fatalf("合法 turn 应通过: %v", err)
	}
}

// TestSessionStepPairing 验证 step/start 必须处于开放 turn 中。
func TestSessionStepPairing(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("s1"))

	// 无开放 turn 的 step/start → 拒绝
	if _, err := sl.Append(session.StepStartData{StepSeq: 1}); err == nil {
		t.Fatal("无开放 turn 的 step/start 应被拒绝")
	}

	// turn 内合法 step
	_, _ = sl.Append(session.TurnStartData{})
	if _, err := sl.Append(session.StepStartData{StepSeq: 1}); err != nil {
		t.Fatalf("turn 内 step/start 应通过: %v", err)
	}
	// step/end 未配对 step/start 后再开 step → 拒绝
	if _, err := sl.Append(session.StepStartData{StepSeq: 2}); err == nil {
		t.Fatal("step 未关闭时再次 step/start 应被拒绝")
	}
}

// TestSessionToolCallResultPairing 验证 tool/call ↔ tool/result 缺失配对被拒绝。
func TestSessionToolCallResultPairing(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("s1"))
	_, _ = sl.Append(session.TurnStartData{})
	_, _ = sl.Append(session.StepStartData{StepSeq: 1})

	// 无匹配 tool/call 的 tool/result → 拒绝
	if _, err := sl.Append(session.ToolResultData{CallID: brand.NewToolCallID("ghost")}); err == nil {
		t.Fatal("无匹配 tool/call 的 tool/result 应被拒绝")
	}

	// 发起 tool/call
	callID := brand.NewToolCallID("call_x")
	if _, err := sl.Append(session.ToolCallData{CallID: callID, Tool: "bash"}); err != nil {
		t.Fatalf("tool/call 应通过: %v", err)
	}
	// 重复同一 callID → 拒绝
	if _, err := sl.Append(session.ToolCallData{CallID: callID, Tool: "bash"}); err == nil {
		t.Fatal("重复 tool/call 应被拒绝")
	}
	// 配对 result → 通过
	if _, err := sl.Append(session.ToolResultData{CallID: callID, Output: "ok"}); err != nil {
		t.Fatalf("tool/result 配对应通过: %v", err)
	}
	// 重复 result → 拒绝
	if _, err := sl.Append(session.ToolResultData{CallID: callID}); err == nil {
		t.Fatal("重复 tool/result 应被拒绝")
	}
}

// TestSessionValidTurnWithTools 验证一条完整合法 turn（含工具调用）整体通过。
func TestSessionValidTurnWithTools(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("s1"))

	_, _ = sl.Append(session.TurnStartData{})
	_, _ = sl.Append(session.StepStartData{StepSeq: 1})
	_, _ = sl.Append(session.UserMessageData{Content: "list files"})
	callID := brand.NewToolCallID("call_ls")
	_, _ = sl.Append(session.ToolCallData{CallID: callID, Tool: "bash"})
	_, _ = sl.Append(session.ToolResultData{CallID: callID, Output: "a.txt"})
	_, _ = sl.Append(session.StepEndData{StepSeq: 1})
	_, err := sl.Append(session.TurnEndData{Reason: session.ReasonFinished})
	if err != nil {
		t.Fatalf("完整合法 turn 应通过: %v", err)
	}

	// 未配对的 tool/call 在整个日志末尾不应存在
	events := sl.Events()
	if len(events) != 7 {
		t.Fatalf("事件数 = %d, want 7", len(events))
	}
}
