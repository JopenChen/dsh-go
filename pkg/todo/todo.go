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
// TodoStatus 是待办三态生命周期（对齐官方 TodoItem.status）。
type TodoStatus string

const (
	StatusPending    TodoStatus = "pending"     // 未开始
	StatusInProgress TodoStatus = "in_progress" // 正在做
	StatusCompleted  TodoStatus = "completed"   // 已完成
)

// Valid 返回状态是否为三态之一。
func (s TodoStatus) Valid() bool {
	return s == StatusPending || s == StatusInProgress || s == StatusCompleted
}

type TodoItem struct {
	// ID 稳定标识（整体替换时用于跨轮跟踪，官方不设；dsh-go 保留可选）。
	ID string `json:"id,omitempty"`
	// Content 待办内容。
	Content string `json:"content"`
	// Status 三态状态。
	Status TodoStatus `json:"status,omitempty"`
	// Done 是否已完成（两态兼容字段；Status 为空时据此推导）。
	Done bool `json:"done,omitempty"`
}

// ResolvedStatus 返回有效状态：优先 Status，否则由 Done 推导。
func (it TodoItem) ResolvedStatus() TodoStatus {
	if it.Status.Valid() {
		return it.Status
	}
	if it.Done {
		return StatusCompleted
	}
	return StatusPending
}

// TodoWriteTool 是 todo_write 工具定义。
//
//   - 入参为 { items: [{id?, content, done?}] }；
//   - 调用即整体替换：当前列表被完全覆盖为本次传入的 items；
//   - 执行时向 SessionLog 写入一条 todo/write 事件。
type TodoWriteTool struct {
	// log 目标会话日志（写入 todo/write 事件）。
	log *session.SessionLog
	// AllowParallel 是否允许多个 in_progress（默认 false：顺序工作）。
	AllowParallel bool
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

	// 规范化：非空、去重、in_progress 数量约束。
	items, err := Normalize(in.Items, t.AllowParallel)
	if err != nil {
		return nil, err
	}

	// 构造持久化三态条目（同时回填 Items 兼容字段）。
	entries := make([]session.TodoEntry, len(items))
	contents := make([]string, len(items))
	for i, it := range items {
		entries[i] = session.TodoEntry{Content: it.Content, Status: string(it.ResolvedStatus())}
		contents[i] = it.Content
	}

	// 写入 todo/write 事件（整体替换，last-write-wins）
	if _, err := t.log.Append(session.TodoWriteData{Items: contents, Entries: entries}); err != nil {
		return nil, fmt.Errorf("todo: append: %w", err)
	}
	c := Count(items)
	return map[string]any{
		"ok": true,
		"counts": map[string]int{
			"pending": c.Pending, "inProgress": c.InProgress, "completed": c.Completed,
		},
	}, nil
}

// TodoTool returns a *tools.Tool wrapper for integration with M23 pipeline.
func (t *TodoWriteTool) Tool() *tools.Tool {
	return &tools.Tool{
		Name:        t.Name(),
		Description: t.Description(),
		Execute:     t.Execute,
	}
}

// Current 返回当前待办列表（通过 fold 派生，last-write-wins），优先三态。
func Current(log *session.SessionLog) []TodoItem {
	fold := session.FoldTodoWrite(log.Events())
	if len(fold.Entries) > 0 {
		out := make([]TodoItem, len(fold.Entries))
		for i, e := range fold.Entries {
			out[i] = TodoItem{Content: e.Content, Status: TodoStatus(e.Status)}
		}
		return out
	}
	out := make([]TodoItem, len(fold.Items))
	for i, c := range fold.Items {
		out[i] = TodoItem{Content: c, Status: StatusPending}
	}
	return out
}