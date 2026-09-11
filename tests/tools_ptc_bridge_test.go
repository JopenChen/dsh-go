// Package tests 的 PTC run_code 桥接（tools/ptc.go + coderuntime）验收测试。
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/coderuntime"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

// stubRuntime 是可编程的代码执行后端桩。
type stubRuntime struct {
	lang     string
	isol     string
	result   *coderuntime.RunResult
	gotReq   *coderuntime.RunRequest
	bindings []coderuntime.BindingNamespace
}

func (s *stubRuntime) Language() string { return s.lang }
func (s *stubRuntime) Isolation() string { return s.isol }
func (s *stubRuntime) Run(req *coderuntime.RunRequest) (*coderuntime.RunResult, error) {
	s.gotReq = req
	s.bindings = req.Bindings
	return s.result, nil
}

func TestRunCodeDelegatesToRuntime(t *testing.T) {
	rt := &stubRuntime{
		lang: "typescript", isol: "process",
		result: &coderuntime.RunResult{Value: "ok", Logs: []string{"line1"}},
	}
	lookup := func(name string) (*tools.Tool, bool) {
		return &tools.Tool{Name: name, Execute: func(context.Context, map[string]any) (any, error) { return "v", nil }}, true
	}
	tool := tools.NewRunCodeTool(rt, lookup, nil)
	if tool.Name != tools.RunCodeName {
		t.Fatal("tool name should be run_code")
	}

	out, err := tool.Execute(context.Background(), map[string]any{"code": "return 1", "description": "test program"})
	if err != nil {
		t.Fatalf("execute should succeed: %v", err)
	}
	o, ok := out.(tools.RunCodeOutput)
	if !ok || o.Result != "ok" || len(o.Logs) != 1 {
		t.Fatalf("unexpected output %+v", out)
	}
	// 应把工具集映射为 "tools" 命名空间
	if len(rt.bindings) != 1 || rt.bindings[0].Global != "tools" {
		t.Fatal("a tools binding namespace must be passed to runtime")
	}
}

func TestRunCodeEmptyInputsRejected(t *testing.T) {
	rt := &stubRuntime{result: &coderuntime.RunResult{}}
	tool := tools.NewRunCodeTool(rt, nil, nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"code": "", "description": "x"}); err == nil {
		t.Fatal("empty code must be rejected")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"code": "x", "description": ""}); err == nil {
		t.Fatal("empty description must be rejected")
	}
}

func TestRunCodeProgramFailureSurfaced(t *testing.T) {
	rt := &stubRuntime{result: &coderuntime.RunResult{
		Failure: &coderuntime.RunFailure{Kind: coderuntime.FailException, Message: "syntax error"},
	}}
	tool := tools.NewRunCodeTool(rt, nil, nil)
	_, err := tool.Execute(context.Background(), map[string]any{"code": "x", "description": "y"})
	if err == nil {
		t.Fatal("a failed program must be surfaced as error for self-correction")
	}
}

func TestRunCodeBindingInvokesTool(t *testing.T) {
	called := false
	lookup := func(name string) (*tools.Tool, bool) {
		return &tools.Tool{Name: name, Execute: func(_ context.Context, in map[string]any) (any, error) {
			called = true
			return "result", nil
		}}, true
	}
	rt := &stubRuntime{result: &coderuntime.RunResult{}}
	tool := tools.NewRunCodeTool(rt, lookup, nil)

	// 手动取出绑定命名空间并调用 __dispatch__，验证程序内工具调用链路
	_, _ = tool.Execute(context.Background(), map[string]any{"code": "start", "description": "boot"})
	dispatch := rt.bindings[0].Functions["__dispatch__"]
	v, err := dispatch(context.Background(), map[string]any{
		"name": "echo", "arguments": map[string]any{"k": "v"},
	})
	if err != nil || v != "result" || !called {
		t.Fatalf("dispatch binding must run the looked-up tool, v=%v err=%v called=%v", v, err, called)
	}
}
