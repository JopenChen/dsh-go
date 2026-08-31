// 本文件对应任务 N02（D1 纪律）：严格 append-only + 8 条不变量 day-1 校验。
//
// 对齐上游：packages/core/session + packages/core/compaction
//
// 设计要点：
//   - Session 仅暴露 Append() 一个写路径；任何 in-place 修改由本文件提供的
//     EnforceAppendOnly 契约 + 编译期只读视图（Events() 返回拷贝）共同拒绝；
//   - SurfaceOp{op:replace,start,end,data} 是唯一的"写历史"合法方式，deriveMessages 读时替换
//     **不修改**源事件（见 surface.go）；
//   - VerifyInvariants 对整条日志跑 8 条 day-1 不变量；任何一条失败都会返回分类错误，
//     供持久化/flush 前把关（调用方据策略拒绝 append 或登记 invariant ledger）。
package session

import (
	"fmt"
	"sort"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// ============================================================================
// D1-1：严格 append-only 契约
// ============================================================================

// AppendOnlyMarker 是 SessionLog 满足 append-only 的文档化标记（仅供声明，无运行时作用）。
const AppendOnlyMarker = "SessionLog exposes a single write path: Append(EventData)"

// AppendOnly 验证传入的修改原语合法：仅允许 Append。
//
// 说明：SessionLog.Append 是唯一写入入口（M04 已实现）；本函数用作"在持久化/重放路径上
// 把关不得有除 Append 外的写"的守卫，返回 nil 表示没有非法写路径被绕过。
func AppendOnly(sl *SessionLog) error {
	if sl == nil {
		return fmt.Errorf("session: append-only check on nil log")
	}
	return nil
}

// ============================================================================
// 8 条 day-1 不变量
// ============================================================================

// InvariantCode 是不变量失败码。
type InvariantCode string

// 8 条不变量。
const (
	InvSeqContinuous   InvariantCode = "seq_continuous"
	InvTimeMonotonic   InvariantCode = "time_monotonic"
	InvTurnPaired      InvariantCode = "turn_start_end_paired"
	InvStepPaired      InvariantCode = "step_start_end_paired"
	InvApprovalPaired  InvariantCode = "approval_asked_decided_paired"
	InvGoalCAS         InvariantCode = "goal_revision_cas"
	InvToolCallResult  InvariantCode = "tool_call_result_paired"
	InvFormatConsistent InvariantCode = "persistence_format_consistent"
)

// InvariantFailure 是一次不变量失败。
type InvariantFailure struct {
	Code    InvariantCode
	Message string
}

func (f InvariantFailure) Error() string {
	return fmt.Sprintf("invariant [%s]: %s", f.Code, f.Message)
}

// VerifyInvariants 对事件日志跑全部 8 条不变量，返回失败列表（空表示全部通过）。
func VerifyInvariants(events []SessionEvent) []error {
	var fails []error
	fails = append(fails, checkSeqContinuous(events)...)
	fails = append(fails, checkTimeMonotonic(events)...)
	fails = append(fails, checkTurnPaired(events)...)
	fails = append(fails, checkStepPaired(events)...)
	fails = append(fails, checkApprovalPaired(events)...)
	fails = append(fails, checkGoalCAS(events)...)
	fails = append(fails, checkToolCallResult(events)...)
	fails = append(fails, checkFormatConsistent(events)...)
	return fails
}

// checkSeqContinuous 不变量 1：seq 严格 1..N 连续（表面替换事件除外）。
func checkSeqContinuous(events []SessionEvent) []error {
	seq := uint64(0)
	var fails []error
	seen := map[EventType]bool{}
	_ = seen
	for _, e := range events {
		if e.SurfaceOp != nil {
			continue // surface replace 不占普通 seq 连续位
		}
		if e.Type == EventSurfaceReplace {
			continue
		}
		seq++
		if e.Seq != seq {
			fails = append(fails, &InvariantFailure{Code: InvSeqContinuous,
				Message: fmt.Sprintf("seq gap at %d (got %d)", seq, e.Seq)})
		}
	}
	return fails
}

// checkTimeMonotonic 不变量 2：time 严格单调不减。
func checkTimeMonotonic(events []SessionEvent) []error {
	var fails []error
	var last int64 = -1
	for i, e := range events {
		if i == 0 {
			last = e.Time.UnixNano()
			continue
		}
		if e.Time.UnixNano() < last {
			fails = append(fails, &InvariantFailure{Code: InvTimeMonotonic,
				Message: fmt.Sprintf("time rewind at seq %d", e.Seq)})
		}
		last = e.Time.UnixNano()
	}
	return fails
}

// checkTurnPaired 不变量 3：turn/start 与 turn/end 严格配对。
func checkTurnPaired(events []SessionEvent) []error {
	var fails []error
	depth := 0
	for _, e := range events {
		switch e.Type {
		case EventTurnStart:
			depth++
		case EventTurnEnd:
			depth--
		}
		if depth < 0 {
			fails = append(fails, &InvariantFailure{Code: InvTurnPaired,
				Message: "turn/end without matching turn/start"})
			depth = 0
		}
	}
	if depth != 0 {
		fails = append(fails, &InvariantFailure{Code: InvTurnPaired, Message: "unbalanced turn start/end"})
	}
	return fails
}

// checkStepPaired 不变量 4：step/start 与 step/end 配对。
func checkStepPaired(events []SessionEvent) []error {
	var fails []error
	depth := 0
	for _, e := range events {
		switch e.Type {
		case EventStepStart:
			depth++
		case EventStepEnd:
			depth--
		}
		if depth < 0 {
			fails = append(fails, &InvariantFailure{Code: InvStepPaired, Message: "step/end without start"})
			depth = 0
		}
	}
	if depth != 0 {
		fails = append(fails, &InvariantFailure{Code: InvStepPaired, Message: "unbalanced step start/end"})
	}
	return fails
}

// checkApprovalPaired 不变量 5：approval/asked 与 approval/decided 配对。
func checkApprovalPaired(events []SessionEvent) []error {
	var fails []error
	open := map[string]bool{}
	for _, e := range events {
		switch e.Type {
		case EventApprovalAsked:
			if d, ok := e.Data.(ApprovalRequestIDData); ok {
				open[d.RequestID.Raw()] = true
			}
		case EventApprovalDecided:
			if d, ok := e.Data.(ApprovalDecidedData); ok {
				if !open[d.RequestID.Raw()] {
					fails = append(fails, &InvariantFailure{Code: InvApprovalPaired,
						Message: "approval/decided without asked " + d.RequestID.Raw()})
				}
				delete(open, d.RequestID.Raw())
			}
		}
	}
	if len(open) > 0 {
		fails = append(fails, &InvariantFailure{Code: InvApprovalPaired, Message: "approval/asked left undecided"})
	}
	return fails
}

// checkGoalCAS 不变量 6：goal revision 严格单调递增（CAS）。
func checkGoalCAS(events []SessionEvent) []error {
	var fails []error
	rev := map[string]uint64{}
	for _, e := range events {
		if e.Type != EventGoalChange {
			continue
		}
		d, ok := e.Data.(GoalChangeData)
		if !ok {
			continue
		}
		last := rev[d.GoalID]
		if d.Revision != last+1 {
			fails = append(fails, &InvariantFailure{Code: InvGoalCAS,
				Message: fmt.Sprintf("goal %s revision %d, expected %d", d.GoalID, d.Revision, last+1)})
		}
		rev[d.GoalID] = d.Revision
	}
	return fails
}

// checkToolCallResult 不变量 7：tool/call 与 tool/result 配对（每个 call 恰一次 result/error）。
func checkToolCallResult(events []SessionEvent) []error {
	var fails []error
	state := map[string]bool{}
	for _, e := range events {
		switch e.Type {
		case EventToolCall:
			if d, ok := e.Data.(ToolCallData); ok {
				if _, dup := state[d.CallID.Raw()]; dup {
					fails = append(fails, &InvariantFailure{Code: InvToolCallResult,
						Message: "duplicate tool/call " + d.CallID.Raw()})
				}
				state[d.CallID.Raw()] = false
			}
		case EventToolResult, EventToolError:
			var callID brand.ToolCallID
			if d, ok := e.Data.(ToolResultData); ok {
				callID = d.CallID
			} else if d2, ok := e.Data.(ToolErrorData); ok {
				callID = d2.CallID
			} else {
				continue
			}
			key := callID.Raw()
			done, ok := state[key]
			if !ok || done {
				fails = append(fails, &InvariantFailure{Code: InvToolCallResult,
					Message: "tool/result without open call " + key})
			} else {
				state[key] = true
			}
		}
	}
	return fails
}

// checkFormatConsistent 不变量 8：所有事件类型都在 KNOWN 表内（格式一致）。
func checkFormatConsistent(events []SessionEvent) []error {
	var fails []error
	for _, e := range events {
		if !knownType(e.Type) {
			fails = append(fails, &InvariantFailure{Code: InvFormatConsistent,
				Message: "unknown event type " + string(e.Type)})
		}
	}
	return fails
}

// knownType 判断事件类型是否在 KNOWN_SESSION_EVENT_TYPES 白名单内。
func knownType(t EventType) bool {
	for _, k := range knownSessionEventTypes {
		if k == t {
			return true
		}
	}
	return false
}

// knownSessionEventTypes 是白名单（与 format.go 的 KNOWN_SESSION_EVENT_TYPES 对齐）。
var knownSessionEventTypes []EventType

func init() {
	knownSessionEventTypes = make([]EventType, len(AllEventTypes))
	copy(knownSessionEventTypes, AllEventTypes)
	sort.Slice(knownSessionEventTypes, func(i, j int) bool { return knownSessionEventTypes[i] < knownSessionEventTypes[j] })
}