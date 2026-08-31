// 本文件对应任务 M22：PreToolDecision 三态（allow/deny/ask）。
//
// 对齐上游：packages/core/tools/pre-execute
//
// 设计要点：
//   - PreToolDecision 是工具 pre-execute 阶段的三态决策（allow / deny / ask），
//     复用了 M27 pkg/approval.Decision；
//   - PreDecisionMiddleware 是一条 pre-execute 中间件：由 DecisionFunc 给出三态，
//     deny → 短路拒绝（结果 IsError、工具不执行）；ask → 交给 AskFunc 请求用户，
//     用户"准许"只放行本次调用（allowed-once 语义），下一次同工具调用继续 ask，
//     不做按工具名的永久放行；
//   - DecisionFunc 可由 pkg/approval.Service.Evaluate 映射而来（M27 已实现三层
//     override 与 ask→allowed-once 决策），M22 负责把它接入四级 Waterfall 的 pre 级。
package tools

import (
	"fmt"

	"github.com/JopenChen/dsh-go/pkg/approval"
	"github.com/JopenChen/dsh-go/pkg/waterfall"
)

// PreToolDecision 是一次工具调用的 pre-execute 决策（三态）。
type PreToolDecision = approval.Decision

// 决策常量（复用 approval 的语义）。
const (
	PreAllow = approval.DecideAllow
	PreDeny  = approval.DecideDeny
	PreAsk   = approval.DecideAsk
)

// DecisionFunc 对一次工具调用给出三态决策。
type DecisionFunc func(req *ToolCallRequest) (PreToolDecision, error)

// AskFunc 处理 ask 决策：向用户询问并返回是否放行本次调用。
// ASK 只对这一次调用放行（allowed-once），返回 true 即执行本次。
type AskFunc func(req *ToolCallRequest) (bool, error)

// denyReasonKey 是 Meta 中记录拒绝原因（含决策类别）的键。
const denyReasonKey = "preDecision.reason"

// DecisionFromApproval 把 M27 审批服务映射为 DecisionFunc。
// approval.Service.Evaluate 已自行处理 ask→allowed-once（内部调用 UQ），
// 因此这里直接透传它的 Decision，且不重复 ask。
func DecisionFromApproval(decide func(req *ToolCallRequest) (approval.Decision, error)) DecisionFunc {
	return func(req *ToolCallRequest) (PreToolDecision, error) {
		return decide(req)
	}
}

// PreDecisionMiddleware 构造一条 pre-execute 决策中间件。
//
// 行为：
//   - PreAllow：放行（不做任何改动）；
//   - PreDeny：写 ec.Denied=true 短路，结果标记 isError；
//   - PreAsk：调用 ask；准许 → 放行本次（可在 Meta 记录 allowedOnce 审计）；
//     拒绝/失败 → 短路 deny。
func PreDecisionMiddleware(decide DecisionFunc, ask AskFunc) waterfall.Handler[ExecContext] {
	return func(ec *ExecContext, next waterfall.NextFunc) error {
		if ec == nil || ec.Request == nil || decide == nil {
			return next()
		}
		decision, err := decide(ec.Request)
		if err != nil {
			ec.Denied = true
			ec.Meta[denyReasonKey] = fmt.Sprintf("decision error: %v", err)
			return next()
		}
		switch decision {
		case PreAllow:
			// 放行，进入下一级（execute）。
			return next()
		case PreDeny:
			ec.Denied = true
			ec.Meta[denyReasonKey] = "denied by pre-tool decision"
			return next()
		case PreAsk:
			if ask == nil {
				ec.Denied = true
				ec.Meta[denyReasonKey] = "ask requested but no ask handler"
				return next()
			}
			allowed, aerr := ask(ec.Request)
			if aerr != nil || !allowed {
				ec.Denied = true
				ec.Meta[denyReasonKey] = "ask denied by user"
				return next()
			}
			// allowed-once：仅放行本次调用，记录审计标记，不持久化放行。
			ec.Meta["allowedOnce"] = ec.Request.CallID.Raw()
			return next()
		default:
			ec.Denied = true
			ec.Meta[denyReasonKey] = fmt.Sprintf("unknown decision %d", decision)
			return next()
		}
	}
}

// AskFromApproval 把 M27 审批服务的 ask（UQ 询问）映射为 AskFunc。
// 传入的 evaluate 需返回已经 resolved 的 allow/deny；当它返回 Ask 时，
// 这里透传给内部 ask 解析。为简化接线，此处假定 evaluate 已把 ask 折叠为 allow/deny。
func AskFromApproval(evaluate func(req *ToolCallRequest) (approval.Decision, error)) AskFunc {
	return func(req *ToolCallRequest) (bool, error) {
		dec, err := evaluate(req)
		if err != nil {
			return false, err
		}
		return dec == approval.DecideAllow, nil
	}
}