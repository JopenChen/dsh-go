// Package todo 提供 Todo 系统：整体替换写入语义。
//
// 对齐上游：packages/core/todo
//
// 设计要点：
//   - todo/write 每次调用都整体替换当前待办列表（last-write-wins，不做增量 diff）；
//   - 工具入口 TodoWriteTool 将 LLM 传回的待办列表经 JSON 校验后写入 SessionLog；
//   - 读取一律通过 session.FoldTodoWrite 派生，保证热 append 与冷重放一致。
package todo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

// TodoItem 是单个待办项。
type TodoItem struct {
	// ID 稳定标识（整体替换时用于跨轮跟踪）。
	ID string `json:"id,omitempty"`
	// Content 待办内容。
	Content string `json:"content"`
	// Done 是否已完成。
	Done bool `json:"done,omitempty"`
}

// TodoWriteTool 是 todo_write 工具定义。
//
//   - 入参为 { items: [{id?, content, done?}] }；
//   - 调用即整体替换：当前列表被完全覆盖为本次传入的 items；
//   - 执行时向 SessionLog 写入一条 todo/write 事件。
type TodoWriteTool struct {
	// log 目标会话日志（写入 todo/write 事件）。
	log *session.SessionLog
}

// NewTodoWriteTool 创建 todo_write 工具。
func NewTodoWriteTool(sl *session.SessionLog) *TodoWriteTool {
	return &TodoWriteTool{log: sl}
}

// todoWriteInput 是工具入参结构。
type todoWriteInput struct {
	Items []TodoItem `json:"items"`
}

// Name 返回工具名。
func (t *TodoWriteTool) Name() string { return "todo_write" }

// Description 返回工具描述。
func (t *TodoWriteTool) Description() string { return "整体替换当前待办列表" }

// Execute 实现工具执行：整体替换待办列表并写入事件。
func (t *TodoWriteTool) Execute(ctx context.Context, input map[string]any) (any, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("todo: marshal input: %w", err)
	}
	var in todoWriteInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("todo: invalid input: %w", err)
	}

	// 提取内容列表
	contents := make([]string, 0, len(in.Items))
	for _, it := range in.Items {
		contents = append(contents, it.Content)
	}

	// 写入 todo/write 事件（整体替换，last-write-wins）
	if _, err := t.log.Append(session.TodoWriteData{Items: contents}); err != nil {
		return nil, fmt.Errorf("todo: append: %w", err)
	}
	return map[string]any{"ok": true, "count": len(contents)}, nil
}

// TodoTool returns a *tools.Tool wrapper for integration with M23 pipeline.
func (t *TodoWriteTool) Tool() *tools.Tool {
	return &tools.Tool{
		Name:        t.Name(),
		Description: t.Description(),
		Execute:     t.Execute,
	}
}

// Current 返回当前待办列表（通过 fold 派生，last-write-wins）。
func Current(log *session.SessionLog) []string {
	fold := session.FoldTodoWrite(log.Events())
	return fold.Items
}