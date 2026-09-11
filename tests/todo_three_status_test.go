// Package tests 的 todo 三态生命周期验收测试。
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/todo"
)

func newTodoLog(t *testing.T) *session.SessionLog {
	t.Helper()
	return session.NewSessionLog(brand.NewSessionID("todo_1"))
}

func TestTodoThreeStatusRoundTrip(t *testing.T) {
	sl := newTodoLog(t)
	tw := todo.NewTodoWriteTool(sl)
	_, err := tw.Execute(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"content": "a", "status": "in_progress"},
			map[string]any{"content": "b", "status": "pending"},
			map[string]any{"content": "c", "status": "completed"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cur := todo.Current(sl)
	if len(cur) != 3 {
		t.Fatalf("expected 3 items, got %d", len(cur))
	}
	if cur[0].Status != todo.StatusInProgress {
		t.Fatalf("first should be in_progress, got %s", cur[0].Status)
	}
}

func TestTodoRejectMultipleActiveSequential(t *testing.T) {
	sl := newTodoLog(t)
	tw := todo.NewTodoWriteTool(sl) // AllowParallel=false
	_, err := tw.Execute(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"content": "a", "status": "in_progress"},
			map[string]any{"content": "b", "status": "in_progress"},
		},
	})
	if err != todo.ErrMultipleActive {
		t.Fatalf("sequential mode must reject 2 active, got %v", err)
	}
}

func TestTodoAllowParallel(t *testing.T) {
	sl := newTodoLog(t)
	tw := todo.NewTodoWriteTool(sl)
	tw.AllowParallel = true
	_, err := tw.Execute(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"content": "a", "status": "in_progress"},
			map[string]any{"content": "b", "status": "in_progress"},
		},
	})
	if err != nil {
		t.Fatalf("parallel mode should allow 2 active, got %v", err)
	}
}

func TestTodoRejectEmptyAndDuplicate(t *testing.T) {
	items := []todo.TodoItem{{Content: "  "}}
	if _, err := todo.Normalize(items, false); err != todo.ErrEmptyContent {
		t.Fatalf("blank content rejected, got %v", err)
	}
	dup := []todo.TodoItem{{Content: "x"}, {Content: "x"}}
	if _, err := todo.Normalize(dup, false); err != todo.ErrDuplicateContent {
		t.Fatalf("duplicate rejected, got %v", err)
	}
}

func TestTodoDoneFallback(t *testing.T) {
	it := todo.TodoItem{Content: "x", Done: true}
	if it.ResolvedStatus() != todo.StatusCompleted {
		t.Fatal("Done=true should resolve to completed")
	}
}

func TestTodoCount(t *testing.T) {
	items := []todo.TodoItem{
		{Content: "a", Status: todo.StatusPending},
		{Content: "b", Status: todo.StatusInProgress},
		{Content: "c", Status: todo.StatusCompleted},
	}
	c := todo.Count(items)
	if c.Pending != 1 || c.InProgress != 1 || c.Completed != 1 {
		t.Fatalf("counts wrong: %+v", c)
	}
}
