// 本文件对应任务 M16：Session Projections 投影注册中心。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// intState 是最小测试状态（基于 int 且可比较）。
type intState int

// Equal 实现 ProjectionState。
func (s intState) Equal(other session.ProjectionState) bool {
	o, ok := other.(intState)
	return ok && s == o
}

// TestProjectionRegisterAndSubscribe 验证注册 Goal/Todo/Plan 等投影后 subscribe changelog。
func TestProjectionRegisterAndSubscribe(t *testing.T) {
	reg := session.NewProjectionRegistry()

	// 注册 todo 投影（累计 todo 次数）
	todoDef := &session.ProjectionDefinition[intState]{
		ID:   brand.NewProjectionID("todo"),
		Init: 0,
		Apply: func(state intState, ev session.SessionEvent) intState {
			if ev.Type == session.EventTodoWrite {
				return state + 1
			}
			return state
		},
	}
	todoProj, err := session.RegisterProjection(reg, todoDef)
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}

	// 构造并应用事件
	reg.ApplyEvents([]session.SessionEvent{
		{Seq: 1, Type: session.EventTodoWrite, Data: session.TodoWriteData{Items: []string{"a"}}},
		{Seq: 2, Type: session.EventTodoWrite, Data: session.TodoWriteData{Items: []string{"a", "b"}}},
		{Seq: 3, Type: session.EventUserMessage, Data: session.UserMessageData{Content: "hi"}},
	})

	// 状态应累计为 2（两次 todo/write）
	if todoProj.State() != 2 {
		t.Fatalf("todo 投影状态 = %d, want 2", todoProj.State())
	}

	// changelog 应含 3 条，其中 todo 的 2 条 changed=true
	ch := reg.Changelog()
	if len(ch) != 3 {
		t.Fatalf("changelog 长度 = %d, want 3", len(ch))
	}
	changedCount := 0
	for _, c := range ch {
		if c.Changed {
			changedCount++
		}
	}
	if changedCount != 2 {
		t.Fatalf("changed 条数 = %d, want 2", changedCount)
	}
}

// TestProjectionSnapshotMatchesFold 验证 snapshot 与 fold* 结果一致。
func TestProjectionSnapshotMatchesFold(t *testing.T) {
	// 构造事件
	events := []session.SessionEvent{
		{Seq: 1, Type: session.EventTodoWrite, Data: session.TodoWriteData{Items: []string{"a"}}},
		{Seq: 2, Type: session.EventPlanMode, Data: session.PlanModeData{Mode: "on"}},
	}
	// 补 time/type 一致性（TCP 断言只用 type，这里补齐即可）
	events[0].Time = fixedTestTime()
	events[1].Time = fixedTestTime()

	reg := session.NewProjectionRegistry()
	// 用 fold 语义注册 todo 投影（直接复用 foldTodoWrite）
	todoDef := &session.ProjectionDefinition[intState]{
		ID:       brand.NewProjectionID("todo"),
		Init:     0,
		Merge: func(evs []session.SessionEvent) intState {
			if session.FoldTodoWrite(evs).Present {
				return intState(len(session.FoldTodoWrite(evs).Items))
			}
			return 0
		},
	}
	_, err := session.RegisterProjection(reg, todoDef)
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	reg.ApplyEvents(events)
	// 该投影只定义 Merge（无 Apply），用 Rebuild 走全量重放派生
	reg.Rebuild(events)

	// snapshot 与 foldTodoWrite 全量对比
	items := session.FoldTodoWrite(events).Items
	expectedState := intState(len(items))
	if got := reg.SnapshotAll()[brand.NewProjectionID("todo")]; got != expectedState {
		t.Fatalf("snapshot = %v, fold = %v（应一致）", got, expectedState)
	}
}