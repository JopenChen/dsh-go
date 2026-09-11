// 本文件复刻官方 toTodoList 的值约束（schema 无法表达的部分）：trim 后非空、
// content 去重、非并行模式下最多一个 in_progress；并提供三态计数。
package todo

import (
	"errors"
	"strings"
)

// ErrEmptyContent 表示存在空白内容条目。
var ErrEmptyContent = errors.New("todo: empty content")

// ErrDuplicateContent 表示存在重复内容。
var ErrDuplicateContent = errors.New("todo: duplicate content")

// ErrMultipleActive 表示非并行模式下出现多个 in_progress。
var ErrMultipleActive = errors.New("todo: more than one in_progress")

// Normalize 规范化待办列表。
//   - trim 后空白内容 → ErrEmptyContent；
//   - 重复 content → ErrDuplicateContent；
//   - allowParallel=false 时多于一个 in_progress → ErrMultipleActive。
func Normalize(items []TodoItem, allowParallel bool) ([]TodoItem, error) {
	out := make([]TodoItem, 0, len(items))
	seen := map[string]struct{}{}
	active := 0
	for _, it := range items {
		content := strings.TrimSpace(it.Content)
		if content == "" {
			return nil, ErrEmptyContent
		}
		if _, dup := seen[content]; dup {
			return nil, ErrDuplicateContent
		}
		seen[content] = struct{}{}
		status := it.ResolvedStatus()
		if status == StatusInProgress {
			active++
		}
		out = append(out, TodoItem{ID: it.ID, Content: content, Status: status})
	}
	if !allowParallel && active > 1 {
		return nil, ErrMultipleActive
	}
	return out, nil
}

// Counts 是三态计数。
type Counts struct {
	Pending    int
	InProgress int
	Completed  int
}

// Count 统计列表三态数量。
func Count(items []TodoItem) Counts {
	var c Counts
	for _, it := range items {
		switch it.ResolvedStatus() {
		case StatusInProgress:
			c.InProgress++
		case StatusCompleted:
			c.Completed++
		default:
			c.Pending++
		}
	}
	return c
}
