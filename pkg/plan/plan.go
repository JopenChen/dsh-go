// Package plan 提供 Plan Mode（计划模式）软引导 + 审批退出。
//
// 对齐上游：packages/core/plan-mode
//
// 设计要点：
//   - plan:policy 是一个 Prompt Section（order 500），仅在 plan mode 开启时注入 system prompt；
//   - 进入 plan mode 写入 plan/mode(on) 事件；下一轮请求时 plan:policy section 注入成功；
//   - exit_plan_mode 工具触发 UserQuestion 审批（走 M14 UQ 接缝）；
//     - 审批通过 → 移除 plan:policy section（下轮请求不出现）+ 写 plan/mode(off)；
//     - 审批拒绝 → plan:policy section 仍保留（继续计划模式）。
package plan

import (
	"context"

	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/sysprompt"
)

// PlanPolicySectionName 是 plan:policy section 的名字。
const PlanPolicySectionName = "plan_policy"

// planPolicyText 是 plan:policy 的固定策略文本（与上游一致，D2 纪律要求稳定）。
const planPolicyText = "当前处于计划模式：只输出方案，不执行改动，直到用户批准。"

// Enter 进入 plan mode：写 plan/mode(on) 事件，并注入 plan:policy section。
func Enter(sl *session.SessionLog, sys *sysprompt.Assembler) error {
	if _, err := sl.Append(session.PlanModeData{Mode: "on"}); err != nil {
		return err
	}
	sys.Register(PlanPolicySectionName, sysprompt.SectionOrderPlanPolicy, planPolicyText)
	return nil
}

// Exit 退出 plan mode：写 plan/mode(off) 事件，并移除 plan:policy section。
func Exit(sl *session.SessionLog, sys *sysprompt.Assembler) error {
	if _, err := sl.Append(session.PlanModeData{Mode: "off"}); err != nil {
		return err
	}
	sys.Unregister(PlanPolicySectionName)
	return nil
}

// IsActive 判断当前是否处于 plan mode。
func IsActive(sl *session.SessionLog) bool {
	mode := session.FoldPlanMode(sl.Events())
	return mode.Present && mode.Mode == "on"
}

// ExitPlanModeResult 是 exit_plan_mode 审批的结果。
type ExitPlanModeResult struct {
	// Approved 审批是否通过。
	Approved bool
	// Exited 是否实际退出（通过才退出）。
	Exited bool
}

// ExitPlanModeTool 是 exit_plan_mode 工具：UserQuestion 审批通过才移除 plan:policy。
type ExitPlanModeTool struct {
	// ask 用户提问出口（M14 UQ 接缝；nil 时走 stub 默认通过）。
	ask func(ctx context.Context, prompt string, choices []string) (int, error)
	// sl 会话日志。
	sl *session.SessionLog
	// sys system prompt 组装器（移除 plan:policy 用）。
	sys *sysprompt.Assembler
}

// NewExitPlanModeTool 创建 exit_plan_mode 工具。
func NewExitPlanModeTool(sl *session.SessionLog, sys *sysprompt.Assembler) *ExitPlanModeTool {
	return &ExitPlanModeTool{sl: sl, sys: sys}
}

// SetAsker 注入用户提问回调（默认测试用 stub：通过）。
func (t *ExitPlanModeTool) SetAsker(f func(ctx context.Context, prompt string, choices []string) (int, error)) {
	t.ask = f
}

// Name 返回工具名。
func (t *ExitPlanModeTool) Name() string { return "exit_plan_mode" }

// Description 返回工具描述。
func (t *ExitPlanModeTool) Description() string { return "退出计划模式（需用户批准）" }

// Execute 实现工具：触发审批，通过则移除 plan:policy 并退出，拒绝则保留。
func (t *ExitPlanModeTool) Execute(ctx context.Context, input map[string]any) (any, error) {
	// 写 plan/approval 审批请求事件
	_, _ = t.sl.Append(session.PlanApprovalData{Approved: false})

	// 触发 UQ 审批
	approved := resolveApproval(t, ctx)

	// 写审批结果
	_, _ = t.sl.Append(session.PlanApprovalData{Approved: approved})

	if approved {
		// 通过 → 移除 plan:policy + 退出
		if err := Exit(t.sl, t.sys); err != nil {
			return nil, err
		}
		return map[string]any{"exited": true, "approved": true}, nil
	}
	// 拒绝 → plan:policy 保留
	return map[string]any{"exited": false, "approved": false}, nil
}

// resolveApproval 走用户提问（stub 默认通过）。
func resolveApproval(t *ExitPlanModeTool, ctx context.Context) bool {
	if t.ask != nil {
		idx, err := t.ask(ctx, "是否退出计划模式？", []string{"批准退出", "继续计划"})
		return err == nil && idx == 0
	}
	// stub 默认：通过
	return true
}