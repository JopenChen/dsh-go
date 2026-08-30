// 本文件对应任务 M05：Session 派生投影函数族（fold 一致性 + compaction replace）。
package tests

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// TestFoldHotVsReplay 验证 1k+ 事件下「热 append 维护」与「冷重放」fold 结果逐字段一致。
func TestFoldHotVsReplay(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("fold_1"))

	// 压入 1000+ 条混合事件（消息 + 状态变更）
	const turns = 520
	for i := 0; i < turns; i++ {
		_, _ = sl.Append(session.UserMessageData{Content: "user msg"})
		_, _ = sl.Append(session.AssistantMessageData{Content: "assistant msg"})
	}

	// 状态变更事件：确保各 fold 分支都被覆盖
	_, _ = sl.Append(session.PresetChangeData{Preset: "safe"})
	_, _ = sl.Append(session.GoalChangeData{GoalID: "g1", Phase: "active", Description: "do x", MaxRounds: 5, Revision: 1})
	_, _ = sl.Append(session.TodoWriteData{Items: []string{"a", "b", "c"}})
	_, _ = sl.Append(session.PlanModeData{Mode: "on"})
	_, _ = sl.Append(session.SessionTitleData{Title: "My Session"})
	_, _ = sl.Append(session.RequestHeaderData{ConfigEpoch: 2, SystemHash: "h", ToolCount: 3})
	_, _ = sl.Append(session.PresetChangeData{Preset: "danger"}) // 覆盖 preset

	if sl.Len() < 1000 {
		t.Fatalf("事件数不足 1000: %d", sl.Len())
	}

	// 热维护：随 append 逐步折叠，记录最终快照
	var hot session.SessionProjection
	events := sl.Events()
	for i := 1; i <= len(events); i++ {
		hot = session.FoldAll(events[:i])
	}

	// 冷重放：对完整事件列表一次性折叠
	replay := session.FoldAll(events)

	// 逐字段一致
	if !reflect.DeepEqual(hot, replay) {
		t.Fatalf("热/冷 fold 不一致\n热: %+v\n冷: %+v", hot, replay)
	}

	// 关键字段语义断言
	if len(replay.Messages) != turns*2 {
		t.Fatalf("消息数 = %d, want %d", len(replay.Messages), turns*2)
	}
	if replay.Preset.Preset != "danger" {
		t.Fatalf("preset 应最新为 danger: %+v", replay.Preset)
	}
	if replay.SandboxMode.Mode != session.SandboxDangerFullAccess {
		t.Fatalf("danger preset 应映射 full-access: %+v", replay.SandboxMode)
	}
	if replay.Approval.Policy != session.ApprovalAllowAll {
		t.Fatalf("danger preset 应映射 allow-all: %+v", replay.Approval)
	}
	if replay.Goal.GoalID != "g1" || replay.Goal.Revision != 1 {
		t.Fatalf("goal fold 异常: %+v", replay.Goal)
	}
	if !replay.Todo.Present || len(replay.Todo.Items) != 3 {
		t.Fatalf("todo fold 异常: %+v", replay.Todo)
	}
	if replay.PlanMode.Mode != "on" {
		t.Fatalf("plan mode fold 异常: %+v", replay.PlanMode)
	}
	if replay.Title.Title != "My Session" {
		t.Fatalf("title fold 异常: %+v", replay.Title)
	}
	if !replay.RequestHeader.Present || replay.RequestHeader.ConfigEpoch != 2 {
		t.Fatalf("request header fold 异常: %+v", replay.RequestHeader)
	}
}

// TestDeriveMessagesAfterCompactionReplace 验证 compaction 表面替换后消息列表缩短且新消息正确。
func TestDeriveMessagesAfterCompactionReplace(t *testing.T) {
	// 手工构造事件序列：10 条消息（seq 1..10）+ 1 条替换事件（seq 11，覆盖 1..8）+ 1 条新消息（seq 12）
	var events []session.SessionEvent
	time := fixedTestTime()
	for i := 1; i <= 10; i++ {
		role := "user"
		content := "old message"
		if i%2 == 0 {
			role = "assistant"
		}
		var data session.EventData
		if role == "user" {
			data = session.UserMessageData{Content: content}
		} else {
			data = session.AssistantMessageData{Content: content}
		}
		events = append(events, session.SessionEvent{Seq: uint64(i), Time: time, Type: data.EventType(), Data: data})
	}

	// 替换事件：assistant/message 摘要，surfaceOp=replace [1,10]（覆盖全部旧消息）
	summary := session.AssistantMessageData{Content: "【压缩摘要】前 10 条消息"}
	replaceEv := session.SessionEvent{
		Seq:       11,
		Time:      time,
		Type:      session.EventAssistantMessage,
		Data:      summary,
		SurfaceOp: session.NewReplaceOp(1, 10, nil),
	}
	events = append(events, replaceEv)

	// 新消息
	events = append(events, session.SessionEvent{
		Seq: 12, Time: time, Type: session.EventUserMessage,
		Data: session.UserMessageData{Content: "新问题"},
	})

	msgs := session.DeriveMessages(events)

	// 期望：摘要 + 新消息（旧消息 seq 1..10 被隐藏，输出缩短）
	if len(msgs) != 2 {
		t.Fatalf("压缩后消息数 = %d, want 2（摘要+新消息）", len(msgs))
	}
	if msgs[0].Content != "【压缩摘要】前 10 条消息" {
		t.Fatalf("首条应为压缩摘要: %q", msgs[0].Content)
	}
	if msgs[1].Content != "新问题" {
		t.Fatalf("末条应为新消息: %q", msgs[1].Content)
	}
	if msgs[0].Seq != 11 || msgs[1].Seq != 12 {
		t.Fatalf("seq 标记异常: %+v", msgs)
	}
}

// TestFoldDeterministicAcrossReplay 验证同一事件集多次 fold 输出完全一致（纯函数）。
func TestFoldDeterministicAcrossReplay(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("fold_2"))
	_, _ = sl.Append(session.PresetChangeData{Preset: "review"})
	_, _ = sl.Append(session.TodoWriteData{Items: []string{"x"}})
	_, _ = sl.Append(session.SessionTitleData{Title: "T"})
	events := sl.Events()

	p1 := session.FoldAll(events)
	p2 := session.FoldAll(events)

	b1, _ := json.Marshal(p1)
	b2, _ := json.Marshal(p2)
	if string(b1) != string(b2) {
		t.Fatalf("fold 输出不确定\n%s\n%s", b1, b2)
	}
}
