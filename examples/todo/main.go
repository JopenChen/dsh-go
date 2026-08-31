// 教程：Todo 系统（整体替换写入）——规划原语之一（教学示例）。
//
// 在 Agent 的规划能力里，Goal 管"目标状态机"，Todo 管"待办清单"。本项目对
// Todo 采用官方一致的【整体替换】（last-write-wins）语义：每次写入都用一套
// 全新的待办列表整体覆盖旧列表，不做事后逐条 diff。
//
// 本示例演示：
//   1. 用 todo_write 工具写入一套待办（整体替换）；
//   2. 用派生函数读回当前待办（fold 自事件日志）；
//   3. 再写一次观察"整体替换"效果——旧列表被完全覆盖。
//
// 运行方式（仓库根目录）：
//   go run ./examples/todo
//
// 对照阅读：pkg/todo/todo.go（TodoWriteTool / Current / TodoItem）
package main

import (
	"context"
	"fmt"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/todo"
)

func main() {
	ctx := context.Background()
	// 1. 创建会话日志（Todo 状态由它派生）。
	sl := session.NewSessionLog(brand.NewSessionID("todo-demo"))
	// 2. 构造 todo_write 工具（绑定到日志）。
	tw := todo.NewTodoWriteTool(sl)

	// ------------------------------------------------------------------
	// 写入第一套待办。
	// 注意入参结构：tool 接收 { items: [{content, done?}] }，整体替换。
	// ------------------------------------------------------------------
	if _, err := tw.Execute(ctx, map[string]any{
		"items": []any{
			map[string]any{"content": "整理需求", "done": false},
			map[string]any{"content": "设计接口", "done": false},
			map[string]any{"content": "实现原型", "done": true},
		},
	}); err != nil {
		panic(err)
	}
	fmt.Println("— 第一次写入后（3 项）—")
	dump(todo.Current(sl))

	// ------------------------------------------------------------------
	// 再次写入：只传 2 项 → 旧 3 项被【整体覆盖】。
	// 这就是"整体替换"语义：不会保留/合并旧的已办项。
	// ------------------------------------------------------------------
	if _, err := tw.Execute(ctx, map[string]any{
		"items": []any{
			map[string]any{"content": "补充测试", "done": false},
			map[string]any{"content": "文档收尾", "done": false},
		},
	}); err != nil {
		panic(err)
	}
	fmt.Println("— 第二次写入后（2 项，旧列表被整体替换）—")
	dump(todo.Current(sl))

	fmt.Println("\n— 结论：Todo = 整体替换清单，读自事件日志(派生)，与 Goal 互补 —")
}

// dump 打印当前待办列表。
func dump(items []string) {
	for i, it := range items {
		fmt.Printf("  %d. %s\n", i+1, it)
	}
}