// Package tests 的审批策略（M27）验收测试。
//
// 覆盖：
//   - 三层 override 顺序（preset < user < session）
//   - 决策路径：allow-all / deny-all / ask-dangerous
//   - ask→allowed-once 语义：通过一次仅放行当次，下一次同工具继续 ask
//   - ask 调用 User Questions 成功/失败链路完整
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/approval"
	"github.com/JopenChen/dsh-go/pkg/userq"
)

// fixedAnswerProvider 是可控答案的 userq provider（0 表示准许，1 表示拒绝）。
type fixedAnswerProvider struct{ ans int }

func (f *fixedAnswerProvider) Ask(_ context.Context, _ userq.QuestionOptions) (*userq.QuestionResult, error) {
	return &userq.QuestionResult{SelectedIndex: f.ans, SelectedIndices: []int{f.ans}}, nil
}

// failingAnswerProvider 是总是报错的 userq provider（模拟 UQ 失败）。
type failingAnswerProvider struct{}

func (*failingAnswerProvider) Ask(context.Context, userq.QuestionOptions) (*userq.QuestionResult, error) {
	return nil, context.Canceled
}

// presetPolicy 是测试用 preset → 策略映射。
func presetPolicy(preset string) (approval.Policy, bool) {
	switch preset {
	case "safe":
		return approval.PolicyDenyAll, true
	case "danger":
		return approval.PolicyAllowAll, true
	case "default":
		return approval.PolicyAskDangerous, true
	}
	return approval.PolicyDenyAll, false
}

// TestApprovalThreeLayerOverrideOrder 验证 预设<用户<会话 三层覆盖顺序。
func TestApprovalThreeLayerOverrideOrder(t *testing.T) {
	svc := approval.New(presetPolicy, userq.New(userq.NewStub()))

	// 预设层（danger → allow-all）。
	if e := svc.Resolve("danger", ""); e.Source != approval.SourcePreset || e.Policy != approval.PolicyAllowAll {
		t.Fatalf("预设层错误: %+v", e)
	}

	// 用户层 override（deny-all）优先于预设层。
	svc.SetUserPolicy(approval.PolicyDenyAll)
	if e := svc.Resolve("danger", ""); e.Source != approval.SourceUser || e.Policy != approval.PolicyDenyAll {
		t.Fatalf("用户层 override 错误: %+v", e)
	}

	// 会话层 override（allow-all）最高。
	svc.SetSessionPolicy("sess1", approval.PolicyAllowAll)
	if e := svc.Resolve("danger", "sess1"); e.Source != approval.SourceSession || e.Policy != approval.PolicyAllowAll {
		t.Fatalf("会话层 override 错误: %+v", e)
	}

	// 其他会话不回落到被覆盖会话的策略（各自独立）。
	if e := svc.Resolve("danger", "sess-other"); e.Source != approval.SourceUser {
		t.Fatalf("其他会话应命中用户层: %+v", e)
	}
}

// TestApprovalEvaluateAllowDeny 验证 allow/deny 直觉路径。
func TestApprovalEvaluateAllowDeny(t *testing.T) {
	svc := approval.New(presetPolicy, userq.New(userq.NewStub()))
	dec, _ := svc.Evaluate(approval.Request{Tool: "bash", Preset: "danger", CallID: "c1"})
	if dec != approval.DecideAllow {
		t.Fatalf("danger(allow-all) 应 allow, 实际 %s", dec)
	}
	dec, _ = svc.Evaluate(approval.Request{Tool: "bash", Preset: "safe", CallID: "c2"})
	if dec != approval.DecideDeny {
		t.Fatalf("safe(deny-all) 应 deny, 实际 %s", dec)
	}
}

// TestApprovalAskAllowedOnce 验证 ask→allowed-once：通过一次仅放行当次，下次继续 ask。
func TestApprovalAskAllowedOnce(t *testing.T) {
	// 答案 0=准许。
	svc := approval.New(presetPolicy, userq.New(&fixedAnswerProvider{ans: 0}))

	// default(ask-dangerous) + 危险工具 bash → ask → 准许 → allow。
	dec, err := svc.Evaluate(approval.Request{Tool: "bash", Preset: "default", CallID: "call-1"})
	if err != nil {
		t.Fatal(err)
	}
	if dec != approval.DecideAllow {
		t.Fatalf("ask 且用户准许应 allow, 实际 %s", dec)
	}
	if svc.AskCount() != 1 {
		t.Fatalf("应恰好 ask 1 次, 实际 %d", svc.AskCount())
	}

	// 下一次同工具调用：不被永久放行，继续 ask（次数增至 2）。
	dec, err = svc.Evaluate(approval.Request{Tool: "bash", Preset: "default", CallID: "call-2"})
	if err != nil {
		t.Fatal(err)
	}
	if svc.AskCount() != 2 {
		t.Fatalf("allowed-once 语义下同工具下一次应继续 ask, 实际次数 %d", svc.AskCount())
	}
	if dec != approval.DecideAllow {
		t.Fatalf("第二次用户仍准许应 allow, 实际 %s", dec)
	}
}

// TestApprovalAskRejected 验证 ask 被拒绝 → deny。
func TestApprovalAskRejected(t *testing.T) {
	// 答案 1=拒绝。
	svc := approval.New(presetPolicy, userq.New(&fixedAnswerProvider{ans: 1}))
	dec, err := svc.Evaluate(approval.Request{Tool: "bash", Preset: "default", CallID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if dec != approval.DecideDeny {
		t.Fatalf("ask 被拒绝应 deny, 实际 %s", dec)
	}
}

// TestApprovalUQFailureDeny 验证 UQ 失败 → fail-closed deny + 错误上抛。
func TestApprovalUQFailureDeny(t *testing.T) {
	svc := approval.New(presetPolicy, userq.New(&failingAnswerProvider{}))
	dec, err := svc.Evaluate(approval.Request{Tool: "bash", Preset: "default", CallID: "c1"})
	if err == nil {
		t.Fatal("UQ 失败应上抛 error")
	}
	if dec != approval.DecideDeny {
		t.Fatalf("UQ 失败应 fail-closed deny, 实际 %s", dec)
	}
}

// TestApprovalNonDangerousAllows 验证 ask-dangerous 下非危险工具直接 allow，不 ask。
func TestApprovalNonDangerousAllows(t *testing.T) {
	svc := approval.New(presetPolicy, userq.New(&fixedAnswerProvider{ans: 1})) // 即便会拒绝也不触发
	dec, err := svc.Evaluate(approval.Request{Tool: "goal_list", Preset: "default", CallID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if dec != approval.DecideAllow {
		t.Fatalf("非危险工具应直接 allow, 实际 %s", dec)
	}
	if svc.AskCount() != 0 {
		t.Fatalf("非危险工具不应 ask, 实际次数 %d", svc.AskCount())
	}
}