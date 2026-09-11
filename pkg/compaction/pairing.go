// 本文件复刻官方 compaction/tool-pairing 的核心：工具调用/结果的括号配平。
// 压缩会改变历史位置，安全切点必须从工具调用与结果的配平关系导出，而不能只看
// step 标记——若切点落在 tool_call 与对应 tool_result 之间，压缩后的历史就会出现
// "有调用没结果"的非法形态，模型会因此卡住。
package compaction

import (
	"errors"

	"github.com/JopenChen/dsh-go/pkg/session"
)

// ErrUnbalanced 表示历史中出现没有对应调用的工具结果（或整体无法配平）。
var ErrUnbalanced = errors.New("compaction: unbalanced tool pairing")

// toolCallDelta 返回单个事件对"进行中工具调用数"的改变量。
//   assistant/message：+该消息声明的工具调用数；tool/result：-1；其余 0。
func toolCallDelta(ev session.SessionEvent) int {
	switch ev.Type {
	case session.EventAssistantMessage:
		if d, ok := ev.Data.(session.AssistantMessageData); ok {
			return len(d.ToolCallIDs)
		}
	case session.EventToolResult:
		return -1
	}
	return 0
}

// BalancedCuts 返回每个切点是否配平。N 个事件有 N+1 个切点：cut[0] 在最前，
// cut[i] 在第 i 个事件之后，cut[N] 在末尾。配平 = 当前进行中调用数为 0。
func BalancedCuts(events []session.SessionEvent) ([]bool, error) {
	cuts := make([]bool, len(events)+1)
	inProgress := 0
	cuts[0] = true
	for i, ev := range events {
		inProgress += toolCallDelta(ev)
		if inProgress < 0 {
			return nil, ErrUnbalanced
		}
		cuts[i+1] = inProgress == 0
	}
	return cuts, nil
}

// IsBalancedAt 判断 cut 位置（0..len(events)）是否配平。
func IsBalancedAt(events []session.SessionEvent, cut int) (bool, error) {
	cuts, err := BalancedCuts(events)
	if err != nil {
		return false, err
	}
	if cut < 0 || cut >= len(cuts) {
		return false, ErrUnbalanced
	}
	return cuts[cut], nil
}

// NearestBalancedFrom 从 start 切点向后（含 start）找第一个配平切点；找不到返回 -1。
// 用于把一个期望的压缩边界安全地后移到不拆散工具对的位置。
func NearestBalancedFrom(events []session.SessionEvent, start int) int {
	cuts, err := BalancedCuts(events)
	if err != nil {
		return -1
	}
	for i := start; i < len(cuts); i++ {
		if cuts[i] {
			return i
		}
	}
	return -1
}
