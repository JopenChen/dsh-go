// 本文件对应 code-review 修复点 R05：对齐官方 @deepseek-ai/dsh-llm/error 的 errorChain()。
//
// 对照上游：D:\workspace\python_workspace\deepseek-harness\packages\llm\llm\src\error.ts
//   - `export function errorChain(value): string` —— 从最外层 message 开始，把完整 cause 链与
//     AggregateError 成员渲染成等价语义的字符串；wrapper 若与 cause 逐字相同则不重复渲染；
//     带循环防护（active path set，仅真环被折叠为 <circular cause>，共享 cause 仍完整展开）。
//
// Go 侧等价物（本项目此前无此公共工具）：
//   - RenderErrorChain(v any) string：把任意 error / 带 .message 的普通值渲染成可读错误链；
//   - 支持 Go 标准库的单一 cause（errors.Unwrap / interface{ Unwrap() error }）与
//     AggregateError 等价物（errors.Join → interface{ Unwrap() []error }，成员以 ` [a; b]` 括起）；
//   - 循环防护：渲染路径上重复的 error 折叠为 <circular cause>；共享 cause（菱形）仍完整展开。
package llm

import (
	"fmt"
	"reflect"
	"strings"
)

// RenderErrorChain 把任意抛出值渲染为带完整 cause 链的错误串。
//
// 语义对齐官方 error.ts 的 errorChain()：
//   - error：取 Error()，空则回落类型名；
//   - 非 error：取结构体的 message (Message) 字段，否则原样字符串化；
//   - AggregateError 成员：以 " [成员1; 成员2]" 括起追加；
//   - 单一 cause：以 ": " 追加，若与 wrapper message 逐字相同则跳过；
//   - 仅作为诊断展示用途（消息/日志/通知），绝不用其结果做业务路由——路由一律走稳定 Kind/code。
func RenderErrorChain(v any) string {
	path := make(map[error]bool)
	return renderChain(v, path)
}

// renderChain 递归渲染 error 链。path 追踪当前递归路径，仅真环被折叠。
func renderChain(cur any, path map[error]bool) string {
	err, ok := cur.(error)
	if !ok {
		// 非 error：优先取 message(Message) 字段，否则原样字符串化。
		if s, ok2 := messageOf(cur); ok2 {
			return s
		}
		return fmt.Sprint(cur)
	}
	if path[err] {
		return "<circular cause>"
	}
	path[err] = true
	defer delete(path, err)

	msg := err.Error()
	if msg == "" {
		msg = typeNameOf(err)
	}

	// AggregateError 等价物：errors.Join 的 Unwrap() []error。
	if mw, ok := err.(interface{ Unwrap() []error }); ok {
		members := mw.Unwrap()
		parts := make([]string, 0, len(members))
		for _, m := range members {
			if m != nil {
				parts = append(parts, renderChain(m, path))
			}
		}
		if len(parts) == 0 {
			return msg
		}
		return msg + " [" + strings.Join(parts, "; ") + "]"
	}

	// 单一 cause 链。
	if uw, ok := err.(interface{ Unwrap() error }); ok {
		cause := uw.Unwrap()
		if cause != nil {
			causeText := renderChain(cause, path)
			if causeText != "" && causeText != msg {
				return msg + ": " + causeText
			}
		}
	}

	return msg
}

// messageOf 尝试从普通结构体/指针提取 message 字段（非 error 但有语义性 message）。
func messageOf(v any) (string, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return "", false
	}
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return "", false
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return "", false
	}
	f := rv.FieldByName("Message")
	if f.IsValid() && f.Kind() == reflect.String {
		return f.String(), true
	}
	return "", false
}

// typeNameOf 返回 error 类型名（无 name 方法时回落）。
func typeNameOf(err error) string {
	t := reflect.TypeOf(err)
	if t == nil {
		return "error"
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Name() == "" {
		return "error"
	}
	return t.Name()
}