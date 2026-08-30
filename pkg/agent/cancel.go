// 本文件对应任务 M18：Agent Cancel 原因分类（5 类）。
//
// 对齐上游：packages/core/agent/Agent.ts
//
// Cancel 的取消原因通过 turn-stopping 事件记录为 "cancel:<cause>"；
// 上层读取日志时用 ExtractCancelCause 还原分类，turn/end 以 aborted 关闭。
package agent

import (
	"strings"

	"github.com/JopenChen/dsh-go/pkg/session"
)

// cancelReasonPrefix 是取消原因写入 turn-stopping 的固定前缀。
const cancelReasonPrefix = "cancel:"

// RecordCancel 记录一次取消原因（写入 turn-stopping 事件）。
func RecordCancel(sl *session.SessionLog, cause CancelCause) error {
	_, err := sl.Append(session.TurnStoppingData{Reason: cancelReasonPrefix + string(cause)})
	return err
}

// ExtractCancelCause 从事件日志中提取最后一次取消原因分类。
// 返回 (cause, true) 表示存在取消记录；否则 (cause, false)。
func ExtractCancelCause(events []session.SessionEvent) (CancelCause, bool) {
	var found CancelCause
	ok := false
	for _, ev := range events {
		if ev.Type != session.EventTurnStopping {
			continue
		}
		d, isData := ev.Data.(session.TurnStoppingData)
		if !isData || !strings.HasPrefix(d.Reason, cancelReasonPrefix) {
			continue
		}
		found = CancelCause(strings.TrimPrefix(d.Reason, cancelReasonPrefix))
		ok = true
	}
	return found, ok
}

// AllCancelCauses 返回全部 5 种取消原因（测试与校验用）。
func AllCancelCauses() []CancelCause {
	return []CancelCause{CancelUser, CancelParent, CancelHook, CancelDisposed, CancelLegacy}
}
