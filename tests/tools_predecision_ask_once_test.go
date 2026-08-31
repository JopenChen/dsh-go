// Package tests 的 PreToolDecision（M22）验收测试。
//
// 覆盖：
//   - allow / deny / ask 三态在 tools/pre-execute 的接线
//   - deny → 工具不执行，结果 isError
//   - ask → 用户仅放行本次调用（allowed-once）：下一次同工具继续 ask，不被永久放行
//   - 与 M27 approval 决策的协作
package tests

import (
	"context"
	"sync"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/approval"
	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

// newToolCallRequest 构造一次工具调用请求。
func newToolCallRequest(callID, tool string, input map[string]any) *tools.ToolCallRequest {
	return &tools.ToolCallRequest{
		CallID: brand.NewToolCallID(callID),
		Tool:   tool,
		Input:  input,
	}
}

// countingAsk 是统计调用次数的 AskFunc；alwaysAllowed 控制是否放行。
type countingAsk struct {
	sync.Mutex
	calls   int
	allowed bool
}

func (c *countingAsk) Ask(req *tools.ToolCallRequest) (bool, error) {
	c.Lock()
	defer c.Unlock()
	c.calls++
	return c.allowed, nil
}

func (c *countingAsk) count() int {
	c.Lock()
	defer c.Unlock()
	return c.calls
}

// decisionByTool 根据工具名给出三态决策的 DecisionFunc。
func decisionByTool(allow map[string]bool, ask map[string]bool) tools.DecisionFunc {
	return func(req *tools.ToolCallRequest) (tools.PreToolDecision, error) {
		if ask[req.Tool] {
			return tools.PreAsk, nil
		}
		if allow[req.Tool] {
			return tools.PreAllow, nil
		}
		return tools.PreDeny, nil
	}
}

// TestPreToolDecisionAllowDenyAsk 验证三态接线：allow 执行、deny 短路、ask 放行当次。
func TestPreToolDecisionAllowDenyAsk(t *testing.T) {
	tool := &tools.Tool{Name: "test_tool", Execute: func(ctx context.Context, input map[string]any) (any, error) {
		return "executed", nil
	}}

	executed := false
	// 用闭包记录是否真正执行。
	inner := tool.Execute
	tool.Execute = func(ctx context.Context, input map[string]any) (any, error) {
		executed = true
		return inner(ctx, input)
	}

	ask := &countingAsk{allowed: true}
	pipeline := tools.NewPipeline()
	pipeline.UsePre(tools.PreDecisionMiddleware(
		decisionByTool(map[string]bool{"allow_tool": true}, map[string]bool{"ask_tool": true}),
		ask.Ask,
	))
	pipeline.WithTool(tool)

	// allow → 执行成功。
	res := pipeline.Run(context.Background(), newToolCallRequest("c-allow", "allow_tool", nil), tool)
	if res.IsError || res.Value != "executed" {
		t.Fatalf("allow 应执行成功: %+v", res)
	}

	// deny → 短路不执行，结果 isError。
	executed = false
	res = pipeline.Run(context.Background(), newToolCallRequest("c-deny", "deny_tool", nil), tool)
	if !res.IsError {
		t.Fatal("deny 应导致 isError")
	}
	if executed {
		t.Fatal("deny 的工具不应被执行")
	}

	// ask → 用户准许 → 放行本次执行。
	executed = false
	res = pipeline.Run(context.Background(), newToolCallRequest("c-ask-1", "ask_tool", nil), tool)
	if res.IsError || res.Value != "executed" {
		t.Fatalf("ask 且用户准许应执行: %+v", res)
	}
	if !executed {
		t.Fatal("ask 准许后工具应执行")
	}
	if ask.count() != 1 {
		t.Fatalf("应 ask 1 次, 实际 %d", ask.count())
	}
}

// TestPreToolDecisionAskOnceNotPermanent 验证 allowed-once：下一次同工具继续 ask，不永久放行。
func TestPreToolDecisionAskOnceNotPermanent(t *testing.T) {
	tool := &tools.Tool{Name: "t", Execute: func(ctx context.Context, input map[string]any) (any, error) {
		return "ok", nil
	}}
	ask := &countingAsk{allowed: true}
	pipeline := tools.NewPipeline()
	pipeline.UsePre(tools.PreDecisionMiddleware(
		decisionByTool(nil, map[string]bool{"ask_tool": true}),
		ask.Ask,
	))
	pipeline.WithTool(tool)

	// 两次相同工具调用：每次都触发 ask，绝不做按工具名的永久放行。
	pipeline.Run(context.Background(), newToolCallRequest("c1", "ask_tool", nil), tool)
	pipeline.Run(context.Background(), newToolCallRequest("c2", "ask_tool", nil), tool)

	if ask.count() != 2 {
		t.Fatalf("allowed-once 语义下两次调用应各自 ask（共 2 次）, 实际 %d", ask.count())
	}
}

// TestPreToolDecisionAskDeniedNotExecutes 验证 ask 被拒 → 短路 deny，工具不执行。
func TestPreToolDecisionAskDeniedNotExecutes(t *testing.T) {
	executed := false
	tool := &tools.Tool{Name: "t", Execute: func(ctx context.Context, input map[string]any) (any, error) {
		executed = true
		return "ok", nil
	}}
	ask := &countingAsk{allowed: false}
	pipeline := tools.NewPipeline()
	pipeline.UsePre(tools.PreDecisionMiddleware(
		decisionByTool(nil, map[string]bool{"ask_tool": true}),
		ask.Ask,
	))
	pipeline.WithTool(tool)

	res := pipeline.Run(context.Background(), newToolCallRequest("c1", "ask_tool", nil), tool)
	if !res.IsError {
		t.Fatal("ask 被拒应 isError")
	}
	if executed {
		t.Fatal("ask 被拒后工具不应执行")
	}
}

// TestPreDecisionFromApproval 验证 M22 与 M27 approval 协作（DecisionFromApproval 透传）。
func TestPreDecisionFromApproval(t *testing.T) {
	// 用一个固定返回 deny 的 approval evaluate 验证透传。
	evaluate := func(*tools.ToolCallRequest) (approval.Decision, error) {
		return approval.DecideDeny, nil
	}
	decide := tools.DecisionFromApproval(evaluate)
	req := newToolCallRequest("c1", "bash", nil)
	dec, err := decide(req)
	if err != nil {
		t.Fatal(err)
	}
	if dec != tools.PreDeny {
		t.Fatalf("应透传 deny, 实际 %d", dec)
	}
	// AskFromApproval: evaluate 返回 allow → true。
	evaluateAllow := func(*tools.ToolCallRequest) (approval.Decision, error) {
		return approval.DecideAllow, nil
	}
	if ok, _ := tools.AskFromApproval(evaluateAllow)(req); !ok {
		t.Fatal("AskFromApproval 在 allow 时应返回 true")
	}
}