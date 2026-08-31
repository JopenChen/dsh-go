// 本文件对应任务 M11：Plan Mode 软引导 + 审批退出。
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/plan"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/sysprompt"
)

// TestPlanModeInjectPolicy 验证进入 plan mode 后 plan:policy section 注入成功。
func TestPlanModeInjectPolicy(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("plan_1"))
	sys := sysprompt.New()

	if err := plan.Enter(sl, sys); err != nil {
		t.Fatalf("Enter 失败: %v", err)
	}
	if !plan.IsActive(sl) {
		t.Fatal("Enter 后应处于 plan mode")
	}
	if !sys.Has(plan.PlanPolicySectionName) {
		t.Fatal("plan:policy section 应注入")
	}
	if !plan.IsActive(sl) {
		t.Fatal("应处于 plan mode")
	}
}

// TestPlanModeApprovalPass 验证审批通过后移除 plan:policy 并退出。
func TestPlanModeApprovalPass(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("plan_2"))
	sys := sysprompt.New()
	_ = plan.Enter(sl, sys)

	tool := plan.NewExitPlanModeTool(sl, sys)
	// stub（无 asker）默认通过
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	m := res.(map[string]any)
	if m["exited"] != true || m["approved"] != true {
		t.Fatalf("审批通过应退出: %v", m)
	}

	// plan:policy section 应移除，下轮请求不出现
	if sys.Has(plan.PlanPolicySectionName) {
		t.Fatal("审批通过后 plan:policy 应被移除")
	}
	if plan.IsActive(sl) {
		t.Fatal("审批通过后应退出 plan mode")
	}
}

// TestPlanModeApprovalReject 验证审批拒绝后 plan:policy 仍保留。
func TestPlanModeApprovalReject(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("plan_3"))
	sys := sysprompt.New()
	_ = plan.Enter(sl, sys)

	tool := plan.NewExitPlanModeTool(sl, sys)
	// 自定义 asker：拒绝（返回 1 = 继续计划）
	tool.SetAsker(func(ctx context.Context, prompt string, choices []string) (int, error) {
		return 1, nil
	})
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	m := res.(map[string]any)
	if m["exited"] != false || m["approved"] != false {
		t.Fatalf("审批拒绝不应退出: %v", m)
	}

	// plan:policy 应保留（继续计划模式）
	if !sys.Has(plan.PlanPolicySectionName) {
		t.Fatal("审批拒绝后 plan:policy 应保留")
	}
	if !plan.IsActive(sl) {
		t.Fatal("审批拒绝后应仍处于 plan mode")
	}
}