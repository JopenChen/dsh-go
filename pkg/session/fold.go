// 本文件对应任务 M05：Session 派生投影函数族（fold 族）。
//
// 对齐上游：packages/core/session（deriveMessages / foldRequestHeader / foldEffectiveSandboxMode / ...）
//
// 设计原则：
//   - 所有 fold 函数都是「纯函数」：输入事件列表，输出派生状态，不修改任何事件；
//   - 状态完全由事件派生（Event Sourcing）：任何时刻重放全部事件即可恢复当前状态，
//     「热 append 维护」与「冷重放」两种路径结果必然一致；
//   - compaction 通过 surface replace 缩短历史：EffectiveEvents 在读时应用表面替换，
//     被替换范围的事件对 fold 不可见，但源事件永远不被修改。
package session

import (
	"sort"
)

// ============================================================================
// 有效事件序列（读时应用 surface replace）
// ============================================================================

// EffectiveEvents 返回应用表面替换后的有效事件列表（读时替换，源事件不变）。
// 规则：携带 SurfaceOp{op:replace, start, end} 的事件会将 [start, end] 范围
// 内的旧事件隐藏，并以自身替代；普通事件按原序保留。
func EffectiveEvents(events []SessionEvent) []SessionEvent {
	hidden := make(map[uint64]bool, len(events))
	for _, ev := range events {
		if ev.SurfaceOp != nil && ev.SurfaceOp.Op == SurfaceReplace {
			for s := ev.SurfaceOp.Start; s <= ev.SurfaceOp.End && s <= ev.Seq; s++ {
				hidden[s] = true
			}
		}
	}
	out := make([]SessionEvent, 0, len(events))
	for _, ev := range events {
		if hidden[ev.Seq] {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// ============================================================================
// 派生消息（deriveMessages）
// ============================================================================

// Message 是派生出的对话消息（供模型上下文组装使用）。
type Message struct {
	Role        string   `json:"role"` // "user" / "assistant"
	Content     string   `json:"content"`
	ToolCallIDs []string `json:"toolCallIds,omitempty"`
	Seq         uint64   `json:"seq"`
}

// deriveMessages 从有效事件序列派生对话消息列表。
// 规则：
//   - user/message → user 消息；
//   - assistant/message → assistant 消息（content + 其 toolCallIds）；
//   - assistant/chunk / assistant/reasoning 不单独成消息（由上层聚合）；
//   - 携带 surface replace 的事件以自身参与派生，被替换的旧消息不可见。
func deriveMessages(events []SessionEvent) []Message {
	effective := EffectiveEvents(events)
	var msgs []Message
	for _, ev := range effective {
		switch ev.Type {
		case EventUserMessage:
			if d, ok := ev.Data.(UserMessageData); ok {
				msgs = append(msgs, Message{Role: "user", Content: d.Content, Seq: ev.Seq})
			}
		case EventAssistantMessage:
			if d, ok := ev.Data.(AssistantMessageData); ok {
				msgs = append(msgs, Message{Role: "assistant", Content: d.Content, ToolCallIDs: d.ToolCallIDs, Seq: ev.Seq})
			}
		}
	}
	return msgs
}

// DeriveMessages 是 deriveMessages 的导出版本。
func DeriveMessages(events []SessionEvent) []Message {
	return deriveMessages(events)
}

// ============================================================================
// 派生投影：请求头 / 沙箱模式 / 审批策略 / 目标 / 待办 / 计划模式 / 权限预设 / 标题
// ============================================================================

// RequestHeaderFold 是 request/header 的最新快照投影。
type RequestHeaderFold struct {
	Present     bool   `json:"present"`
	ConfigEpoch uint64 `json:"configEpoch,omitempty"`
	SystemHash  string `json:"systemHash,omitempty"`
	ToolCount   int    `json:"toolCount,omitempty"`
}

// foldRequestHeader 折叠出最新的 request/header。
func foldRequestHeader(events []SessionEvent) RequestHeaderFold {
	var out RequestHeaderFold
	for _, ev := range events {
		if ev.Type != EventRequestHeader {
			continue
		}
		if d, ok := ev.Data.(RequestHeaderData); ok {
			out = RequestHeaderFold{Present: true, ConfigEpoch: d.ConfigEpoch, SystemHash: d.SystemHash, ToolCount: d.ToolCount}
		}
	}
	return out
}

// FoldRequestHeader 是 foldRequestHeader 的导出版本。
func FoldRequestHeader(events []SessionEvent) RequestHeaderFold {
	return foldRequestHeader(events)
}

// SandboxMode 沙箱模式。
type SandboxMode string

// 沙箱模式枚举。
const (
	SandboxReadOnly        SandboxMode = "read-only"
	SandboxWorkspaceWrite  SandboxMode = "workspace-write"
	SandboxDangerFullAccess SandboxMode = "danger-full-access"
)

// EffectiveSandboxMode 是生效沙箱模式投影（preset 映射 + 是否强制 + 根路径）。
type EffectiveSandboxMode struct {
	Present  bool       `json:"present"`
	Mode     SandboxMode `json:"mode,omitempty"`
	Enforced bool       `json:"enforced,omitempty"`
	Root     string     `json:"root,omitempty"`
}

// ApprovalPolicy 审批策略。
type ApprovalPolicy string

// 审批策略枚举。
const (
	ApprovalAllowAll           ApprovalPolicy = "allow-all"
	ApprovalDenyAll            ApprovalPolicy = "deny-all"
	ApprovalAskDangerous       ApprovalPolicy = "ask-dangerous"
	ApprovalAskDangerousEdit   ApprovalPolicy = "ask-dangerous-tool-edit"
)

// EffectiveApprovalPolicy 是生效审批策略投影。
type EffectiveApprovalPolicy struct {
	Present bool          `json:"present"`
	Policy  ApprovalPolicy `json:"policy,omitempty"`
	Source  string        `json:"source,omitempty"` // preset / user / session
}

// PermissionPresetFold 是权限预设投影。
type PermissionPresetFold struct {
	Present bool   `json:"present"`
	Preset  string `json:"preset,omitempty"`
}

// presetSandbox 是预设名 → 沙箱模式的映射表（与上游 permission-presets 对齐）。
var presetSandbox = map[string]SandboxMode{
	"safe":    SandboxReadOnly,
	"danger":  SandboxDangerFullAccess,
	"review":  SandboxReadOnly,
	"default": SandboxWorkspaceWrite,
}

// presetApproval 是预设名 → 审批策略的映射表。
var presetApproval = map[string]ApprovalPolicy{
	"safe":    ApprovalAskDangerous,
	"danger":  ApprovalAllowAll,
	"review":  ApprovalAskDangerousEdit,
	"default": ApprovalAskDangerous,
}

// foldPermissionPreset 折叠出最新的权限预设名。
func foldPermissionPreset(events []SessionEvent) PermissionPresetFold {
	var out PermissionPresetFold
	for _, ev := range events {
		if ev.Type != EventPresetChange {
			continue
		}
		if d, ok := ev.Data.(PresetChangeData); ok {
			out = PermissionPresetFold{Present: true, Preset: d.Preset}
		}
	}
	return out
}

// FoldPermissionPreset 是 foldPermissionPreset 的导出版本。
func FoldPermissionPreset(events []SessionEvent) PermissionPresetFold {
	return foldPermissionPreset(events)
}

// foldEffectiveSandboxMode 从最新权限预设推导生效沙箱模式。
func foldEffectiveSandboxMode(events []SessionEvent) EffectiveSandboxMode {
	preset := foldPermissionPreset(events)
	if !preset.Present {
		return EffectiveSandboxMode{}
	}
	mode, ok := presetSandbox[preset.Preset]
	if !ok {
		mode = SandboxWorkspaceWrite
	}
	return EffectiveSandboxMode{Present: true, Mode: mode, Enforced: true}
}

// FoldEffectiveSandboxMode 是 foldEffectiveSandboxMode 的导出版本。
func FoldEffectiveSandboxMode(events []SessionEvent) EffectiveSandboxMode {
	return foldEffectiveSandboxMode(events)
}

// foldEffectiveApprovalPolicy 从最新权限预设推导生效审批策略。
func foldEffectiveApprovalPolicy(events []SessionEvent) EffectiveApprovalPolicy {
	preset := foldPermissionPreset(events)
	if !preset.Present {
		return EffectiveApprovalPolicy{}
	}
	policy, ok := presetApproval[preset.Preset]
	if !ok {
		policy = ApprovalAskDangerous
	}
	return EffectiveApprovalPolicy{Present: true, Policy: policy, Source: "preset"}
}

// FoldEffectiveApprovalPolicy 是 foldEffectiveApprovalPolicy 的导出版本。
func FoldEffectiveApprovalPolicy(events []SessionEvent) EffectiveApprovalPolicy {
	return foldEffectiveApprovalPolicy(events)
}

// GoalFold 是目标状态投影（CAS revision 用于并发保护）。
type GoalFold struct {
	Present     bool            `json:"present"`
	GoalID      string          `json:"goalId,omitempty"`
	Phase       GoalPhase       `json:"phase,omitempty"`
	Description string          `json:"description,omitempty"`
	MaxRounds   int             `json:"maxRounds,omitempty"`
	Revision    uint64          `json:"revision,omitempty"`
	// BlockReason 最新阻塞原因（R06：随 goal/change 持久化，latest-wins）。
	BlockReason *GoalBlockReason `json:"blockedReason,omitempty"`
}

// foldGoalChange 折叠出最新目标状态（last-write-wins + revision 单调递增）。
func foldGoalChange(events []SessionEvent) GoalFold {
	var out GoalFold
	for _, ev := range events {
		if ev.Type != EventGoalChange {
			continue
		}
		if d, ok := ev.Data.(GoalChangeData); ok {
			// 只接受 revision 单调递增的更新（CAS 语义）
			if !out.Present || d.Revision > out.Revision {
				out = GoalFold{
					Present:     true,
					GoalID:      d.GoalID,
					Phase:       d.Phase,
					Description: d.Description,
					MaxRounds:   d.MaxRounds,
					Revision:    d.Revision,
					BlockReason: d.BlockReason,
				}
			}
		}
	}
	return out
}

// FoldGoalChange 是 foldGoalChange 的导出版本。
func FoldGoalChange(events []SessionEvent) GoalFold {
	return foldGoalChange(events)
}

// TodoFold 是待办列表投影（整体替换，last-write-wins）。
type TodoFold struct {
	Present bool     `json:"present"`
	Items   []string `json:"items,omitempty"`
}

// foldTodoWrite 折叠出最新待办列表（每次 todo/write 整体替换）。
func foldTodoWrite(events []SessionEvent) TodoFold {
	var out TodoFold
	for _, ev := range events {
		if ev.Type != EventTodoWrite {
			continue
		}
		if d, ok := ev.Data.(TodoWriteData); ok {
			out = TodoFold{Present: true, Items: append([]string(nil), d.Items...)}
		}
	}
	return out
}

// FoldTodoWrite 是 foldTodoWrite 的导出版本。
func FoldTodoWrite(events []SessionEvent) TodoFold {
	return foldTodoWrite(events)
}

// PlanModeFold 是计划模式投影。
type PlanModeFold struct {
	Present bool   `json:"present"`
	Mode    string `json:"mode,omitempty"` // "on" / "off"
}

// foldPlanMode 折叠出最新计划模式（on/off）。
func foldPlanMode(events []SessionEvent) PlanModeFold {
	var out PlanModeFold
	for _, ev := range events {
		if ev.Type != EventPlanMode {
			continue
		}
		if d, ok := ev.Data.(PlanModeData); ok {
			out = PlanModeFold{Present: true, Mode: d.Mode}
		}
	}
	return out
}

// FoldPlanMode 是 foldPlanMode 的导出版本。
func FoldPlanMode(events []SessionEvent) PlanModeFold {
	return foldPlanMode(events)
}

// SessionTitleFold 是会话标题投影（latest-wins）。
type SessionTitleFold struct {
	Present bool   `json:"present"`
	Title   string `json:"title,omitempty"`
}

// foldSessionTitle 折叠出最新标题。
func foldSessionTitle(events []SessionEvent) SessionTitleFold {
	var out SessionTitleFold
	for _, ev := range events {
		if ev.Type != EventSessionTitle {
			continue
		}
		if d, ok := ev.Data.(SessionTitleData); ok {
			out = SessionTitleFold{Present: true, Title: d.Title}
		}
	}
	return out
}

// FoldSessionTitle 是 foldSessionTitle 的导出版本。
func FoldSessionTitle(events []SessionEvent) SessionTitleFold {
	return foldSessionTitle(events)
}

// ============================================================================
// FoldAll：一次折叠全部派生状态
// ============================================================================

// SessionProjection 是全部派生投影的聚合视图。
type SessionProjection struct {
	Messages     []Message                 `json:"messages"`
	RequestHeader RequestHeaderFold         `json:"requestHeader"`
	SandboxMode  EffectiveSandboxMode      `json:"sandboxMode"`
	Approval     EffectiveApprovalPolicy   `json:"approvalPolicy"`
	Preset       PermissionPresetFold      `json:"preset"`
	Goal         GoalFold                  `json:"goal"`
	Todo         TodoFold                  `json:"todo"`
	PlanMode     PlanModeFold              `json:"planMode"`
	Title        SessionTitleFold          `json:"title"`
}

// FoldAll 对事件列表做一次全量折叠，返回全部派生状态。
func FoldAll(events []SessionEvent) SessionProjection {
	effective := EffectiveEvents(events)
	return SessionProjection{
		Messages:     deriveMessages(effective),
		RequestHeader: foldRequestHeader(effective),
		SandboxMode:  foldEffectiveSandboxMode(effective),
		Approval:     foldEffectiveApprovalPolicy(effective),
		Preset:       foldPermissionPreset(effective),
		Goal:         foldGoalChange(effective),
		Todo:         foldTodoWrite(effective),
		PlanMode:     foldPlanMode(effective),
		Title:        foldSessionTitle(effective),
	}
}

// FoldAllFromLog 从 SessionLog 直接折叠（便捷入口）。
func FoldAllFromLog(sl *SessionLog) SessionProjection {
	return FoldAll(sl.Events())
}

// sortMessagesBySeq 按 seq 排序消息（供测试断言与上层组装使用）。
func sortMessagesBySeq(msgs []Message) {
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].Seq < msgs[j].Seq })
}
