// 本文件复刻官方 packages/core/agent/src/consumed-work.ts。
//
// 它回答 Turn/Step 词汇单独回答不了的问题：一份 agent 日志到底结算了哪些被消费的工作？
// 只看 turn/end 会把"在首个 step 前就中止的 turn"和"被拒绝/空 claim 产生的平衡 no-op turn"
// 混为一谈——要么把半途而废的工作误记为完成，要么冤枉每个 no-op。
//
// 缺失的事实来自收件箱自身的记录：Inbox 每次变更都带 removedCount，取消会标
// outcome='canceled'，据此区分"一个 claim 了输入的 turn"与"工作未运行即被丢弃"。
package agent

import (
	"github.com/JopenChen/dsh-go/pkg/session"
)

// ConsumedWork 是一份 agent 日志对其消费工作的结算。
type ConsumedWork struct {
	// End 是最后一个结算了已消费工作的已关闭 turn：它进入过模型 step，
	// 或 claim 了收件箱输入后失败/被中止/被拒绝。没有任何 turn 结算工作时为 nil。
	End *session.SessionEvent
	// DroppedUnrun 表示在上述 turn 之后，已被接受的工作在收件箱中被取消、从未运行。
	// 这是"任何 turn 都来不及打开就被取消的输入"唯一的账目——没有 turn/end 描述它。
	DroppedUnrun bool
}

// accountsForClaim 判断一个消费了输入却没走到 step 的 turn，其结束方式是否结算了该输入。
//
// 只有 completed 不结算：claim 被改写掉之后它已经没有东西可跑。
// blocked 也是这份输入的结局——产生它的 pre-step 拒绝丢弃了被 claim 的消息，
// 那些工作永远不会运行。
func accountsForClaim(reason session.TurnEndReason) bool {
	switch reason {
	case session.ReasonCompleted, session.ReasonFinished:
		return false
	case session.ReasonBlocked, session.ReasonAborted, session.ReasonInterrupted, session.ReasonError:
		return true
	default:
		// 未具名的结束方式（如 max-tokens 需 step，不会走到这；后端扩展的变体无法穷举）：
		// 消费了输入的不可名状结束，不能读作成功。
		return true
	}
}

// FoldConsumedWork 单遍折叠一份 agent 日志（或其拥有的后缀），得到已消费工作的结算。
// 每个输入都是日志本身，调用方无需在取消前采样实时状态，因此任何人发起的取消
// （持有者的 teardown、祖先的中断、卸载中的插件）都得到一致读法。
func FoldConsumedWork(events []session.SessionEvent) ConsumedWork {
	stepped := make(map[uint64]struct{})
	claimed := make(map[uint64]struct{})
	var open uint64
	hasOpen := false
	var end *session.SessionEvent
	droppedUnrun := false

	for i := range events {
		ev := &events[i]
		switch ev.Type {
		case session.EventTurnStart:
			if d, ok := ev.Data.(session.TurnStartData); ok {
				open = d.Turn
				hasOpen = true
			}
		case session.EventStepStart:
			if d, ok := ev.Data.(session.StepStartData); ok {
				stepped[d.Turn] = struct{}{}
			}
		case session.EventAgentInboxSpliced:
			d, ok := ev.Data.(session.InboxSplicedData)
			if !ok || d.RemovedCount == 0 {
				continue
			}
			if d.Outcome == "canceled" {
				// 替换会以新身份保留待办，因此只有什么都没留下的取消才算丢弃。
				droppedUnrun = droppedUnrun || len(d.Inserted) == 0
			} else if hasOpen {
				// claim 是循环自身在 step 边界的读取，总在某个 turn 内。
				claimed[open] = struct{}{}
			}
		case session.EventTurnEnd:
			d, ok := ev.Data.(session.TurnEndData)
			if !ok {
				continue
			}
			hasOpen = false
			_, didStep := stepped[d.Turn]
			_, didClaim := claimed[d.Turn]
			if didStep || (didClaim && accountsForClaim(d.Reason)) {
				cp := *ev
				end = &cp
				// 该 turn 关闭前丢弃的东西由它自己的结局报告；只有之后的丢弃仍未结算。
				droppedUnrun = false
			}
			delete(stepped, d.Turn)
			delete(claimed, d.Turn)
		}
	}

	return ConsumedWork{End: end, DroppedUnrun: droppedUnrun}
}
