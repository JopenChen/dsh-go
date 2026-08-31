// 本文件对应 code-review 修复点 R05：对齐官方 errorChain() 错误链渲染。
//
// 对照上游：packages/llm/llm/src/error.ts 的 export function errorChain(value）。
//
// 验证目标：
//   1. 单一 cause 链：外层 + ": " + 内层；
//   2. wrapper 与 cause 逐字相同时不重复渲染（去噪）；
//   3. AggregateError 等价物（errors.Join）成员以 ` [a; b]` 括起；
//   4. 非 error 的 message 字段提取 / 原样字符串化；
//   5. 循环 cause → <circular cause>；
//   6. 空 Error() 回落类型名。
package tests

import (
	"errors"
	"fmt"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/llm"
)

// simpleErr 带类型名的 error，验证空 message 回落类型名。
type simpleErr struct{}

func (simpleErr) Error() string { return "" }

// llmFailureMessage 一个 message 与 cause 相同的 error（外层即 cause 文本）。
type llmFailureMessage struct{ msg string }

func (e *llmFailureMessage) Error() string { return e.msg }
func (e *llmFailureMessage) Unwrap() error { return errors.New(e.msg) } // cause 同文本

// circularA / circularB 构造互相包裹的循环 cause。
type circularA struct{ next error }

func (circularA) Error() string       { return "A" }
func (c circularA) Unwrap() error     { return c.next }
type circularB struct{ next error }

func (circularB) Error() string   { return "B" }
func (c circularB) Unwrap() error { return c.next }

func TestR05SingleCauseChain(t *testing.T) {
	base := errors.New("base failure")
	wrapped := fmt.Errorf("wrap: %w", base)
	got := llm.RenderErrorChain(wrapped)
	// 官方 errorChain 仅当 cause 与外层 message 【逐字相同】才跳过；此处 cause("base failure")
	// 是外层 message 的子串但非逐字相同，故照官方语义追加 ": base failure"。
	if got != "wrap: base failure: base failure" {
		t.Fatalf("single cause chain = %q, want %q", got, "wrap: base failure: base failure")
	}
}

func TestR05RepeatedCauseSkipped(t *testing.T) {
	e := &llmFailureMessage{msg: "dup"}
	got := llm.RenderErrorChain(e)
	// "dup: dup" 因重复而被抑制 → 仅 "dup"。
	if got != "dup" {
		t.Fatalf("repeated cause should be suppressed, got %q", got)
	}
}

func TestR05Aggregate(t *testing.T) {
	m := errors.Join(errors.New("a"), errors.New("b"), errors.New("c"))
	got := llm.RenderErrorChain(m)
	// Go errors.Join 会把成员合并进自身 Error()，且 Unwrap()[]error 再展开成员。
	// 结构上应体现带括号的成员展开。
	if !contains(got, "[a; b; c]") {
		t.Fatalf("aggregate 应含括号成员组, got %q", got)
	}
}

func TestR05MessageFieldAndStr(t *testing.T) {
	// 非 error 的 message 字段。
	if got := llm.RenderErrorChain(struct{ Message string }{Message: "hi msg"}); got != "hi msg" {
		t.Fatalf("message field = %q", got)
	}
	// 无 message 字段 → 原样字符串化。
	if got := llm.RenderErrorChain(42); got != "42" {
		t.Fatalf("plain value = %q", got)
	}
}

func TestR05CircularAndEmpty(t *testing.T) {
	// 循环：A→B→A。
	var a circularA
	var b circularB
	a.next = &b
	b.next = &a
	got := llm.RenderErrorChain(&a)
	if !contains(got, "<circular cause>") {
		t.Fatalf("circular 应含 <circular cause>, got %q", got)
	}
	// 空 message 回落类型名。
	if got := llm.RenderErrorChain(simpleErr{}); got != "simpleErr" {
		t.Fatalf("empty message should fall back to type name, got %q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}