// 本文件对应任务 M13：Todo 整体替换写入。
package tests

import (
	"context"
	"reflect"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/todo"
)

// TestTodoWriteReplace 验证多次 todo/write 每次整体替换（last-write-wins）。
func TestTodoWriteReplace(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("todo_1"))
	tool := todo.NewTodoWriteTool(sl)

	// 第一次整体替换
	_, err := tool.Execute(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"content": "a"},
			map[string]any{"content": "b"},
		},
	})
	if err != nil {
		t.Fatalf("第一次执行失败: %v", err)
	}
	if got := todo.Current(sl); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("第一次替换结果 = %v, want [a b]", got)
	}

	// 第二次整体替换（覆盖为仅一项）
	_, err = tool.Execute(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"content": "c"},
		},
	})
	if err != nil {
		t.Fatalf("第二次执行失败: %v", err)
	}
	if got := todo.Current(sl); !reflect.DeepEqual(got, []string{"c"}) {
		t.Fatalf("第二次应整体替换为 [c], got %v", got)
	}

	// 空列表替换：清空
	_, err = tool.Execute(context.Background(), map[string]any{"items": []any{}})
	if err != nil {
		t.Fatalf("清空执行失败: %v", err)
	}
	if got := todo.Current(sl); len(got) != 0 {
		t.Fatalf("清空后应为空, got %v", got)
	}

	// fold 语义为整体替换（last-write-wins）：只保留最新
	fold := session.FoldTodoWrite(sl.Events())
	if !fold.Present || len(fold.Items) != 0 {
		t.Fatalf("fold 应反映最新空列表: %+v", fold)
	}
}