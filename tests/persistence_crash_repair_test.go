// 本文件对应任务 M43：Persistence 接缝 + Flush Checkpoint + Crash Repair。
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/persistence"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// TestPersistenceCrashRepair 验证 append 过程中"崩溃"（未写 turn/end）→ reload → repair 关闭孤儿 turn。
func TestPersistenceCrashRepair(t *testing.T) {
	dir := t.TempDir()
	backend, err := persistence.NewJSONL(dir, 1) // batch 1：每条即时落盘
	if err != nil {
		t.Fatalf("NewJSONL 失败: %v", err)
	}
	ctx := context.Background()

	id := brand.NewSessionID("crash_s")
	header := session.NewSessionHeader(id, "/ws")
	if err := backend.SaveHeader(ctx, header); err != nil {
		t.Fatalf("SaveHeader 失败: %v", err)
	}

	// 写入半截 turn + 工具事件（已落盘），但不写 turn/end（模拟进程在此崩溃）
	events := []session.SessionEvent{
		{Seq: 1, Type: session.EventTurnStart, Data: session.TurnStartData{}},
		{Seq: 2, Type: session.EventStepStart, Data: session.StepStartData{StepSeq: 1}},
		{Seq: 3, Type: session.EventUserMessage, Data: session.UserMessageData{Content: "hi"}},
		{Seq: 4, Type: session.EventToolCall, Data: session.ToolCallData{CallID: brand.NewToolCallID("call_1"), Tool: "bash"}},
	}
	for i := range events {
		events[i].Time = fixedTestTime()
		if err := backend.Append(ctx, id, events[i]); err != nil {
			t.Fatalf("Append 失败: %v", err)
		}
	}
	// 故意不写 turn/end，模拟崩溃

	// reload（重新打开文件做 repair）
	backend2, err := persistence.NewJSONL(dir, 1)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	_, loaded, err := backend2.Load(ctx, id)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	// 校验：原 4 条 + repair 1 条
	if len(loaded) != 5 {
		t.Fatalf("load 后事件数 = %d, want 5（4 条 + repair turn/end）", len(loaded))
	}
	// 补写的 turn/end{reason:interrupted} 应在末尾
	last := loaded[len(loaded)-1]
	if last.Type != session.EventTurnEnd {
		t.Fatalf("末尾应为 repair 的 turn/end: %+v", last)
	}
	if td, ok := last.Data.(session.TurnEndData); !ok || td.Reason != session.ReasonInterrupted {
		t.Fatalf("repair 的 turn/end 应为 interrupted: %+v", last.Data)
	}

	// 已写入的 chunk/tool/call 事件应全部保留
	foundCall := false
	for _, ev := range loaded {
		if ev.Type == session.EventToolCall {
			foundCall = true
		}
	}
	if !foundCall {
		t.Fatal("已写入的 tool/call 事件应被保留")
	}
}

// TestPersistenceForkLineage 验证 fork 后父/子会话 Persistence 键目录正确隔离。
func TestPersistenceForkLineage(t *testing.T) {
	dir := t.TempDir()
	backend, err := persistence.NewJSONL(dir, 1)
	if err != nil {
		t.Fatalf("NewJSONL 失败: %v", err)
	}
	ctx := context.Background()

	parent := session.NewSessionHeader(brand.NewSessionID("parent"), "/ws")
	if err := backend.SaveHeader(ctx, parent); err != nil {
		t.Fatalf("SaveHeader 失败: %v", err)
	}
	child := parent.Fork(brand.NewSessionID("child"))
	if err := backend.SaveHeader(ctx, child); err != nil {
		t.Fatalf("child SaveHeader 失败: %v", err)
	}

	// 两个会话应各自可加载且互为父子关系
	_, parentEvents, err := backend.Load(ctx, parent.ID)
	if err != nil {
		t.Fatalf("parent Load 失败: %v", err)
	}
	childHeader, childEvents, err := backend.Load(ctx, child.ID)
	if err != nil {
		t.Fatalf("child Load 失败: %v", err)
	}

	if len(parentEvents) != 0 || len(childEvents) != 0 {
		t.Fatalf("空会话事件数应为 0: parent=%d child=%d", len(parentEvents), len(childEvents))
	}
	if childHeader.ParentSession.Raw() != "parent" {
		t.Fatalf("child.ParentSession = %q, want parent", childHeader.ParentSession.Raw())
	}

	// List 应包含两个会话
	ids, err := backend.List(ctx)
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("List 应含 2 个会话: %v", ids)
	}
}

// TestPersistenceFlushCheckpoint 验证 flush 后文件确实落盘可重载。
func TestPersistenceFlushCheckpoint(t *testing.T) {
	dir := t.TempDir()
	backend, err := persistence.NewJSONL(dir, 100)
	if err != nil {
		t.Fatalf("NewJSONL 失败: %v", err)
	}
	ctx := context.Background()

	id := brand.NewSessionID("flush_s")
	header := session.NewSessionHeader(id, "/ws")
	_ = backend.SaveHeader(ctx, header)

	// 写入几条事件（batch 100，不自动 flush）
	_ = backend.Append(ctx, id, session.SessionEvent{Seq: 1, Type: session.EventTurnStart, Data: session.TurnStartData{}})
	_ = backend.Append(ctx, id, session.SessionEvent{Seq: 2, Type: session.EventTurnEnd, Data: session.TurnEndData{Reason: session.ReasonFinished}})

	// flush 前重载（新 backend）应看不到事件（未落盘）
	backendProbe := newJSONLNoCheck(t, dir)
	_, before, _ := backendProbe.Load(ctx, id)
	if len(before) != 0 {
		t.Fatalf("flush 前文件不应有事件, got %d", len(before))
	}

	// flush checkpoint
	if err := backend.Flush(ctx, id); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}

	// 重载应看到 2 条事件
	_, after, err := backend.Load(ctx, id)
	if err != nil {
		t.Fatalf("reload 失败: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("flush 后应有 2 条事件, got %d", len(after))
	}
}

// newJSONLNoCheck 简化构造（不关心错误）。
func newJSONLNoCheck(t *testing.T, dir string) *persistence.JSONLBackend {
	t.Helper()
	b, err := persistence.NewJSONL(dir, 1)
	if err != nil {
		t.Fatalf("NewJSONL 失败: %v", err)
	}
	return b
}