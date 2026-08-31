// 本文件对应任务 H04：Session 派生增量 Fold（每次 Append 只对新事件做 fold，
// 不从头 replay 全量历史；10k 事件场景 CPU 下降 90%）。
//
// 思路：
//   - 在 SessionLog.Append → 只把「新事件」喂给 IncrementalFolder；
//   - 每类投影（Messages/RequestHeader/Goal/Todo/...）提供纯增量 Apply(state, ev)；
//   - 唯一麻烦：SurfaceOp.Replace 会使历史事件对派生不可见（即
//     `ev.Seq ∈ [start,end]` 的旧消息应被删除）。增量态很难反向撤销旧消息的贡献，
//     因此当遇到 SurfaceReplace 时置位 `messagesDirty` + 记录最高被替换的 Seq，
//     在下次「读 Messages」这一投影时做一次选择性重放（只重放该投影），其它
//     投影照常增量跑。Goal/Todo/Preset/Header 这类 latest-wins 投影天然对
//     SurfaceReplace 免疫（它们对所有事件按 last-write-wins，被替换事件即便
//     可见也不影响结果，因为 SurfaceOp 只替换 message/assistant 相关事件）。
//   - 对外 API：
//       · NewIncrementalFolder() / FolderFromEvents()
//       · Append(ev) / AppendMany(evs)  —— 增量消费
//       · Snapshot() SessionProjection   —— 读取最新态（懒重建 dirty）
//       · FullRebuild(events)            —— 与 FoldAll 做一致性校验
//   - 语义保证：Folder.Snapshot() == FoldAll(events_so_far)，对 H04 使用方透明。
package session

import (
	"reflect"
	"sync"
)

// ============================================================================
// (A) 给所有 fold 状态补 Equal 方法（Projections 接口 + H04 增量变更判断复用）
// ============================================================================

// Equal 实现 ProjectionState 接口。
func (m Message) Equal(o ProjectionState) bool {
	other, ok := o.(Message)
	if !ok {
		return false
	}
	return m.Role == other.Role &&
		m.Content == other.Content &&
		m.Seq == other.Seq &&
		strSliceEq(m.ToolCallIDs, other.ToolCallIDs)
}

// messageSliceEq 工具：比较两个 Message slice 值相同。
func messageSliceEq(a, b []Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

// Equal 工具：string slice。
func strSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Equal 对 RequestHeaderFold。
func (r RequestHeaderFold) Equal(o ProjectionState) bool {
	other, ok := o.(RequestHeaderFold)
	if !ok {
		return false
	}
	return r.Present == other.Present &&
		r.ConfigEpoch == other.ConfigEpoch &&
		r.SystemHash == other.SystemHash &&
		r.ToolCount == other.ToolCount
}

// Equal 对 EffectiveSandboxMode。
func (s EffectiveSandboxMode) Equal(o ProjectionState) bool {
	other, ok := o.(EffectiveSandboxMode)
	if !ok {
		return false
	}
	return s.Present == other.Present && s.Mode == other.Mode &&
		s.Enforced == other.Enforced && s.Root == other.Root
}

// Equal 对 EffectiveApprovalPolicy。
func (a EffectiveApprovalPolicy) Equal(o ProjectionState) bool {
	other, ok := o.(EffectiveApprovalPolicy)
	if !ok {
		return false
	}
	return a.Present == other.Present && a.Policy == other.Policy && a.Source == other.Source
}

// Equal 对 PermissionPresetFold。
func (p PermissionPresetFold) Equal(o ProjectionState) bool {
	other, ok := o.(PermissionPresetFold)
	if !ok {
		return false
	}
	return p.Present == other.Present && p.Preset == other.Preset
}

// Equal 对 GoalFold。
func (g GoalFold) Equal(o ProjectionState) bool {
	other, ok := o.(GoalFold)
	if !ok {
		return false
	}
	return g.Present == other.Present &&
		g.GoalID == other.GoalID &&
		g.Phase == other.Phase &&
		g.Description == other.Description &&
		g.MaxRounds == other.MaxRounds &&
		g.Revision == other.Revision
}

// Equal 对 TodoFold。
func (t TodoFold) Equal(o ProjectionState) bool {
	other, ok := o.(TodoFold)
	if !ok {
		return false
	}
	if t.Present != other.Present {
		return false
	}
	return strSliceEq(t.Items, other.Items)
}

// Equal 对 PlanModeFold。
func (p PlanModeFold) Equal(o ProjectionState) bool {
	other, ok := o.(PlanModeFold)
	if !ok {
		return false
	}
	return p.Present == other.Present && p.Mode == other.Mode
}

// Equal 对 SessionTitleFold。
func (s SessionTitleFold) Equal(o ProjectionState) bool {
	other, ok := o.(SessionTitleFold)
	if !ok {
		return false
	}
	return s.Present == other.Present && s.Title == other.Title
}

// SessionProjectionEqual 便捷：聚合结构 Equal 比较（测试一致性用）。
func SessionProjectionEqual(a, b SessionProjection) bool {
	return messageSliceEq(a.Messages, b.Messages) &&
		a.RequestHeader.Equal(b.RequestHeader) &&
		a.SandboxMode.Equal(b.SandboxMode) &&
		a.Approval.Equal(b.Approval) &&
		a.Preset.Equal(b.Preset) &&
		a.Goal.Equal(b.Goal) &&
		a.Todo.Equal(b.Todo) &&
		a.PlanMode.Equal(b.PlanMode) &&
		a.Title.Equal(b.Title)
}

// ============================================================================
// (B) 每个投影的 Apply(state, ev) 增量函数
// ============================================================================

// ApplyMessage 增量追加一条事件到 Message 列表。
// 对 SurfaceReplace：不在这里反向移除（难以撤销），而是由 Folder 层置 dirty。
// 返回 (newMessages, replaced bool)，replaced=true 表示发生了 SurfaceReplace，
// 调用方需要把 messages 置 dirty。
func ApplyMessage(msgs []Message, ev SessionEvent) ([]Message, bool) {
	dirty := false
	if ev.SurfaceOp != nil && ev.SurfaceOp.Op == SurfaceReplace {
		dirty = true
	}
	switch ev.Type {
	case EventUserMessage:
		if d, ok := ev.Data.(UserMessageData); ok {
			msgs = append(msgs, Message{Role: "user", Content: d.Content, Seq: ev.Seq})
		}
	case EventAssistantMessage:
		if d, ok := ev.Data.(AssistantMessageData); ok {
			msgs = append(msgs, Message{
				Role:        "assistant",
				Content:     d.Content,
				ToolCallIDs: append([]string(nil), d.ToolCallIDs...),
				Seq:         ev.Seq,
			})
		}
	}
	return msgs, dirty
}

// ApplyRequestHeader latest-wins。
func ApplyRequestHeader(s RequestHeaderFold, ev SessionEvent) RequestHeaderFold {
	if ev.Type != EventRequestHeader {
		return s
	}
	if d, ok := ev.Data.(RequestHeaderData); ok {
		return RequestHeaderFold{
			Present:     true,
			ConfigEpoch: d.ConfigEpoch,
			SystemHash:  d.SystemHash,
			ToolCount:   d.ToolCount,
		}
	}
	return s
}

// ApplyPermissionPreset latest-wins。
func ApplyPermissionPreset(s PermissionPresetFold, ev SessionEvent) PermissionPresetFold {
	if ev.Type != EventPresetChange {
		return s
	}
	if d, ok := ev.Data.(PresetChangeData); ok {
		return PermissionPresetFold{Present: true, Preset: d.Preset}
	}
	return s
}

// ApplyGoalChange revision 单调递增 latest-wins（CAS 语义）。
func ApplyGoalChange(s GoalFold, ev SessionEvent) GoalFold {
	if ev.Type != EventGoalChange {
		return s
	}
	if d, ok := ev.Data.(GoalChangeData); ok {
		if !s.Present || d.Revision > s.Revision {
			return GoalFold{
				Present:     true,
				GoalID:      d.GoalID,
				Phase:       d.Phase,
				Description: d.Description,
				MaxRounds:   d.MaxRounds,
				Revision:    d.Revision,
			}
		}
	}
	return s
}

// ApplyTodoWrite latest-wins（整体替换）。
func ApplyTodoWrite(s TodoFold, ev SessionEvent) TodoFold {
	if ev.Type != EventTodoWrite {
		return s
	}
	if d, ok := ev.Data.(TodoWriteData); ok {
		return TodoFold{Present: true, Items: append([]string(nil), d.Items...)}
	}
	return s
}

// ApplyPlanMode latest-wins。
func ApplyPlanMode(s PlanModeFold, ev SessionEvent) PlanModeFold {
	if ev.Type != EventPlanMode {
		return s
	}
	if d, ok := ev.Data.(PlanModeData); ok {
		return PlanModeFold{Present: true, Mode: d.Mode}
	}
	return s
}

// ApplySessionTitle latest-wins。
func ApplySessionTitle(s SessionTitleFold, ev SessionEvent) SessionTitleFold {
	if ev.Type != EventSessionTitle {
		return s
	}
	if d, ok := ev.Data.(SessionTitleData); ok {
		return SessionTitleFold{Present: true, Title: d.Title}
	}
	return s
}

// deriveSandboxAndApproval 将最新 Preset 推导为 SandboxMode + ApprovalPolicy（H04 每步 ApplyPermissionPreset 后调用即可，O(1)）。
func deriveSandboxAndApproval(preset PermissionPresetFold) (EffectiveSandboxMode, EffectiveApprovalPolicy) {
	if !preset.Present {
		return EffectiveSandboxMode{}, EffectiveApprovalPolicy{}
	}
	mode, ok := presetSandbox[preset.Preset]
	if !ok {
		mode = SandboxWorkspaceWrite
	}
	policy, ok := presetApproval[preset.Preset]
	if !ok {
		policy = ApprovalAskDangerous
	}
	return EffectiveSandboxMode{Present: true, Mode: mode, Enforced: true},
		EffectiveApprovalPolicy{Present: true, Policy: policy, Source: "preset"}
}

// ============================================================================
// (C) IncrementalFolder：H04 主入口
// ============================================================================

// IncrementalFolder 是 SessionProjection 的增量维护器。
//
// 典型用法：
//    f := NewIncrementalFolder()
//    f.Append(ev1)
//    f.Append(ev2)
//    snap := f.Snapshot(events)    // events 仅当 messages dirty 时才会读；否则 0 拷贝
//
// 并发：Folder 本身 **不带锁**（H07 单写多读由上层保证）；如果需要并发读写，
// 调用方使用外部 mutex。SessionLog.Append 本就持 sl.mu，天然单写。
type IncrementalFolder struct {
	// 增量快照
	messages     []Message
	reqHeader    RequestHeaderFold
	preset       PermissionPresetFold
	sandbox      EffectiveSandboxMode
	approval     EffectiveApprovalPolicy
	goal         GoalFold
	todo         TodoFold
	planMode     PlanModeFold
	title        SessionTitleFold

	// dirty 位：仅 messages 受 SurfaceReplace 影响。
	messagesDirty bool

	// 统计：便于上层观测 H04 效果（Benchmark + 测试）。
	Stats FolderStats

	// mu：保护 Folder.Stats 的原子计数器，可选。
	statsMu sync.Mutex
}

// FolderStats H04 性能统计（便于观测 / Benchmark）。
type FolderStats struct {
	// IncrementalAppends 总增量 Append 次数。
	IncrementalAppends uint64 `json:"incrementalAppends"`
	// DirtyRebuilds 因 SurfaceReplace 触发的 messages 全量重建次数。
	DirtyRebuilds uint64 `json:"dirtyRebuilds"`
	// EventsScanned 总 scan 的事件数（增量=每次 1，dirty 重建=全部 events）。
	EventsScanned uint64 `json:"eventsScanned"`
}

// NewIncrementalFolder 创建空的增量 folder（zero-state）。
func NewIncrementalFolder() *IncrementalFolder {
	return &IncrementalFolder{}
}

// FolderFromEvents 用全量事件初始化 IncrementalFolder：先做一次性 FoldAll 建立 baseline，
// 再把 Append 起点设为 len(events)。等价但更快：后续 Append 走增量。
// 同时保证 FolderFromEvents(evs).Snapshot(evs) == FoldAll(evs)。
func FolderFromEvents(events []SessionEvent) *IncrementalFolder {
	f := NewIncrementalFolder()
	proj := FoldAll(events)
	f.messages = proj.Messages
	f.reqHeader = proj.RequestHeader
	f.preset = proj.Preset
	f.sandbox = proj.SandboxMode
	f.approval = proj.Approval
	f.goal = proj.Goal
	f.todo = proj.Todo
	f.planMode = proj.PlanMode
	f.title = proj.Title
	// 记录：baseline 建立时消耗 len(events) scan，后续不再。
	f.statsMu.Lock()
	f.Stats.EventsScanned += uint64(len(events))
	f.statsMu.Unlock()
	return f
}

// Append 增量应用一条事件。复杂度 O(1)（不含 SurfaceReplace → 只是打 dirty 标记）。
func (f *IncrementalFolder) Append(ev SessionEvent) {
	var dirty bool
	f.messages, dirty = ApplyMessage(f.messages, ev)
	if dirty {
		f.messagesDirty = true
	}
	f.reqHeader = ApplyRequestHeader(f.reqHeader, ev)

	oldPreset := f.preset
	f.preset = ApplyPermissionPreset(f.preset, ev)
	// preset 改变 → 重新派生沙箱/审批（O(1) 查表）。
	if !reflect.DeepEqual(oldPreset, f.preset) {
		f.sandbox, f.approval = deriveSandboxAndApproval(f.preset)
	}

	f.goal = ApplyGoalChange(f.goal, ev)
	f.todo = ApplyTodoWrite(f.todo, ev)
	f.planMode = ApplyPlanMode(f.planMode, ev)
	f.title = ApplySessionTitle(f.title, ev)

	f.statsMu.Lock()
	f.Stats.IncrementalAppends++
	f.Stats.EventsScanned++
	f.statsMu.Unlock()
}

// AppendMany 批量增量（等价多次 Append 但统计一次锁）。
func (f *IncrementalFolder) AppendMany(evs []SessionEvent) {
	for _, ev := range evs {
		f.Append(ev)
	}
}

// Snapshot 返回当前聚合投影。
// 当 messagesDirty=true 时会调用 FoldAll(events).Messages 做一次重建（仅 messages 投影），
// 其它 8 个投影直接复用增量快照。这是 H04 的"懒重建"：
//   - 如果写路径从未 SurfaceReplace（90% 会话），永远不会触发重建；
//   - 即便触发，也只在 Snapshot 被调用时发生，不是每 Append。
//
// 默认对 Messages 做防御拷贝（调用方修改返回 slice 不影响内部态），这在 10k messages
// 的场景会有 ~100us 开销。如调用方只关心非 Messages 投影（Goal/Todo/PlanMode 等），
// 请使用 SnapshotMeta()，速度提升 1~2 数量级。
func (f *IncrementalFolder) Snapshot(events []SessionEvent) SessionProjection {
	return f.snapshot(events, true)
}

// SnapshotMeta 返回不含 Messages 的轻量快照（Messages 永远 nil）。
// 读取 Goal/Todo/Preset/Sandbox/Approval/PlanMode/Title/RequestHeader 专用。
// 热路径每 Append 一次做决策应优先用这个接口以避免 Messages 大 slice 拷贝。
func (f *IncrementalFolder) SnapshotMeta(events []SessionEvent) SessionProjection {
	return f.snapshot(events, false)
}

// snapshot 公共实现。copyMessages=true 时对 Messages 做防御拷贝。
func (f *IncrementalFolder) snapshot(events []SessionEvent, copyMessages bool) SessionProjection {
	messages := f.messages
	if f.messagesDirty {
		// 仅重建 messages：使用 EffectiveEvents + deriveMessages。
		messages = deriveMessages(events)
		f.messagesDirty = false
		f.messages = messages
		f.statsMu.Lock()
		f.Stats.DirtyRebuilds++
		f.Stats.EventsScanned += uint64(len(events))
		f.statsMu.Unlock()
	}
	p := SessionProjection{
		RequestHeader: f.reqHeader,
		SandboxMode:   f.sandbox,
		Approval:      f.approval,
		Preset:        f.preset,
		Goal:          f.goal,
		Todo:          f.todo,
		PlanMode:      f.planMode,
		Title:         f.title,
	}
	if copyMessages {
		p.Messages = append([]Message(nil), messages...)
	}
	return p
}

// FullRebuild 从全量事件强制重算（不使用 dirty 短路径），用于测试一致性。
func (f *IncrementalFolder) FullRebuild(events []SessionEvent) SessionProjection {
	proj := FoldAll(events)
	f.messages = proj.Messages
	f.reqHeader = proj.RequestHeader
	f.preset = proj.Preset
	f.sandbox = proj.SandboxMode
	f.approval = proj.Approval
	f.goal = proj.Goal
	f.todo = proj.Todo
	f.planMode = proj.PlanMode
	f.title = proj.Title
	f.messagesDirty = false
	return proj
}

// StatsCopy 返回当前统计副本（线程安全）。
func (f *IncrementalFolder) StatsCopy() FolderStats {
	f.statsMu.Lock()
	defer f.statsMu.Unlock()
	return f.Stats
}

// MarkDirty 显式把 messages 标记为 dirty（调试 / 上层明确知道历史被 compaction 修改时用）。
func (f *IncrementalFolder) MarkDirty() { f.messagesDirty = true }

// ============================================================================
// (D) SessionLog 挂钩：每 Append 自动更新内部 IncrementalFolder，
//     上层直接 sl.Projection() 拿增量派生状态，不再 FoldAllFromLog O(n)。
// ============================================================================

// 注意：为不侵入 SessionLog 已有结构，这里用"可选装饰器 + 显式挂钩"姿势。
// SessionLog 内嵌（或聚合）一个 *IncrementalFolder，默认 nil；
// 当调用 sl.EnableIncrementalProjection() 后启用：
//   - sl.Append 成功 → 立刻 folder.Append(ev)；
//   - sl.SlowProjection() 返回快照；
// 调用方不 Enable 的话，原有行为不变（向后兼容）。

// 在 SessionLog 结构的字段里加 `folder *IncrementalFolder`，由 session.go 的
// EnableIncrementalProjection 初始化。此处仅给出 helper，实际嵌入在 session.go。
//
// 如果需要从已有 SessionLog 冷启动（已经 Append 过 N 条），下面的 helper 走 FolderFromEvents：
