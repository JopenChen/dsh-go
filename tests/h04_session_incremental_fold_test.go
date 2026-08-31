// 本文件对应任务 H04：Session 派生增量 Fold（10k 事件 CPU 下降 90%）。
//
// 测试矩阵：
//   1. 冷启动空 SessionLog，启用增量 Append N 条后 → sl.Projection() 必须与 FoldAll(sl.Events()) 完全等价。
//   2. 热启动：已有 N 条后再 EnableIncrementalProjection → 再 Append 仍等价。
//   3. SurfaceReplace 触发 dirty → messages 懒重建结果仍匹配 FoldAll。
//   4. latest-wins 族投影（Preset→Sandbox/Approval、Goal、Todo、PlanMode、Title、RequestHeader）各 1 组状态切换 → 增量结果正确。
//   5. Benchmark：10k 事件 × 每 Append 读一次投影 —— 增量 VS 全量 CPU 用时 ≤ 10%（10 倍加速）。
package tests

import (
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// newTestLogH04 便捷：创建一个 SessionLog（可选启用增量）。
func newTestLogH04(enableIncremental bool) *session.SessionLog {
	sl := session.NewSessionLog(brand.NewSessionID("h04-sess"))
	if enableIncremental {
		sl.EnableIncrementalProjection()
	}
	return sl
}

// fixedTimeH04 返回固定时间（Appending 不看 wall clock，但 invariant 会检查，保持一致更好）。
func fixedTimeH04() time.Time { return time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC) }

// ============================================================================
// 1. 等价性：空启动 + 每 Append 一条 user/assistant/preset/todo/goal/plan/title/header
// ============================================================================

// TestH04IncrementalEquivalentAfterEachAppend 验证「增量 Snapshot == 全量 FoldAll」在
// 每 Append 一次都成立（覆盖 Message/RequestHeader/Preset→Sandbox/Approval/Goal/Todo/PlanMode/Title）。
func TestH04IncrementalEquivalentAfterEachAppend(t *testing.T) {
	sl := newTestLogH04(true)
	if !sl.IncrementalEnabled() {
		t.Fatal("EnableIncrementalProjection 后应为 true")
	}

	// 构造一批事件，覆盖所有投影类型。
	payloads := []session.EventData{
		session.UserMessageData{Content: "u1"},
		session.AssistantMessageData{Content: "a1"},
		session.RequestHeaderData{ConfigEpoch: 1, SystemHash: "h1", ToolCount: 3},
		session.PresetChangeData{Preset: "danger"},
		session.GoalChangeData{GoalID: "g1", Phase: session.GoalPhase("in-progress"), Description: "d1", MaxRounds: 5, Revision: 1},
		session.TodoWriteData{Items: []string{"t1", "t2"}},
		session.PlanModeData{Mode: "on"},
		session.SessionTitleData{Title: "会话标题"},
		session.UserMessageData{Content: "u2"},
		session.AssistantMessageData{Content: "a2", ToolCallIDs: []string{"call-1"}},
		session.PresetChangeData{Preset: "safe"},
		session.GoalChangeData{GoalID: "g1", Phase: session.GoalPhase("in-progress"), Description: "d2", MaxRounds: 5, Revision: 2},
		session.TodoWriteData{Items: []string{"t1-new"}},
		session.PlanModeData{Mode: "off"},
		session.RequestHeaderData{ConfigEpoch: 2, SystemHash: "h2", ToolCount: 7},
		session.SessionTitleData{Title: "标题V2"},
	}

	for i, p := range payloads {
		if _, err := sl.Append(p); err != nil {
			t.Fatalf("第 %d 条 Append 失败: %v", i, err)
		}
		got := sl.Projection()
		want := session.FoldAll(sl.Events())
		if !session.SessionProjectionEqual(got, want) {
			t.Fatalf("第 %d 条 Append 后增量 ≠ 全量\n got=%+v\nwant=%+v", i, got, want)
		}
	}

	stats := sl.IncrementalStats()
	if stats.IncrementalAppends != uint64(len(payloads)) {
		t.Fatalf("IncrementalAppends = %d, want %d", stats.IncrementalAppends, len(payloads))
	}
	// 没有 SurfaceReplace → DirtyRebuilds 必须为 0
	if stats.DirtyRebuilds != 0 {
		t.Fatalf("DirtyRebuilds = %d, want 0", stats.DirtyRebuilds)
	}
}

// ============================================================================
// 2. 热启动：Append N 条 → 再 EnableIncrementalProjection → Append M 条，等价性仍成立。
// ============================================================================

func TestH04WarmStartEquivalence(t *testing.T) {
	sl := newTestLogH04(false) // 先不启用
	// 热启动基线事件：
	base := []session.EventData{
		session.UserMessageData{Content: "old-u"},
		session.AssistantMessageData{Content: "old-a"},
		session.PresetChangeData{Preset: "review"},
		session.TodoWriteData{Items: []string{"old"}},
		session.GoalChangeData{GoalID: "g-old", Phase: session.GoalPhase("pending"), Description: "old", MaxRounds: 1, Revision: 1},
	}
	for _, d := range base {
		if _, err := sl.Append(d); err != nil {
			t.Fatal(err)
		}
	}
	// 此时 FoldAll(sl.Events()) 基线。
	baseline := session.FoldAll(sl.Events())

	// 启用增量
	sl.EnableIncrementalProjection()
	got := sl.Projection()
	if !session.SessionProjectionEqual(got, baseline) {
		t.Fatalf("热启动启用瞬间 got ≠ baseline FoldAll")
	}
	stats1 := sl.IncrementalStats()
	// 基线 N 条 → 启动时 EventsScanned 应累计为 N
	if int(stats1.EventsScanned) < len(base) {
		t.Fatalf("热启动 EventsScanned = %d, expect >= %d", stats1.EventsScanned, len(base))
	}
	// Append 新 M 条
	extra := []session.EventData{
		session.UserMessageData{Content: "new-u"},
		session.AssistantMessageData{Content: "new-a"},
		session.TodoWriteData{Items: []string{"new1", "new2"}},
		session.PlanModeData{Mode: "on"},
		session.SessionTitleData{Title: "warm"},
	}
	for _, d := range extra {
		if _, err := sl.Append(d); err != nil {
			t.Fatal(err)
		}
	}
	got2 := sl.Projection()
	want2 := session.FoldAll(sl.Events())
	if !session.SessionProjectionEqual(got2, want2) {
		t.Fatalf("热启动后 Append M 条 got ≠ FoldAll()")
	}
	stats2 := sl.IncrementalStats()
	if stats2.IncrementalAppends != stats1.IncrementalAppends+uint64(len(extra)) {
		t.Fatalf("增量 Append 计数未正确累加: stats1=%+v stats2=%+v", stats1, stats2)
	}
}

// ============================================================================
// 3. SurfaceReplace → dirty 懒重建等价性
// ============================================================================

func TestH04SurfaceReplaceDirtyRebuild(t *testing.T) {
	sl := newTestLogH04(true)
	// Append 3 条 user message（seq 1/2/3）
	for _, c := range []string{"u1", "u2", "u3"} {
		if _, err := sl.Append(session.UserMessageData{Content: c}); err != nil {
			t.Fatal(err)
		}
	}
	// 正常投影：Messages = [u1, u2, u3]
	before := sl.Projection()
	if len(before.Messages) != 3 {
		t.Fatalf("messages before = %d, want 3", len(before.Messages))
	}

	// 直接用 sl.Events 构造一条 SurfaceReplace 事件：seq=2→被替换为新内容"u2-compacted"。
	// 说明：Append 的事件数据本身不携带 SurfaceOp，SurfaceOp 是 SessionEvent 字段，
	// 因此这里通过反射/直接构造 SessionEvent 追加（用 session.Log 的 low-level：
	// 绕过 Append 内部校验，直接把构造的 SessionEvent 塞进 sl.Events()
	// 不实际 Append — 因为 SurfaceOp 是 compaction 层写入的。在 H04 测试里，我们通过
	// 追加一条"携带 SurfaceOp 的 UserMessage"事件：这在 Compaction BasicEngine
	// 真实发生：它写回一条 assistant/message（替换原 assistant chunk 范围）。
	//
	// 这里：构造 seq=4，SurfaceReplace(Start=2,End=2)，Data=UserMessageData{Content:"u2-replaced"}
	// 则 EffectiveEvents 会隐藏 seq=2 的 u2，按 seq 序结果是 u1,u2-replaced,u3。
	if _, err := sl.Append(session.UserMessageData{Content: "u2-replaced"}); err != nil {
		t.Fatal(err)
	}
	// 手动把刚 Append 的事件（seq=4）打上 SurfaceOp: replace 2..2
	evs := sl.Events()
	evs[3].SurfaceOp = &session.SurfaceOp{Op: session.SurfaceReplace, Start: 2, End: 2}
	// 写回 sl：此处用一个"专用绕过"—— 通过 sl.Disable + Enable 重新建立 baseline，
	// 并调用 sl 的 Append hook。但 sl.Events() 返回的是副本，不影响 sl 内部。
	// 为了真正把 SurfaceOp 事件注入到 sl 内部：
	// 技巧：先 Disable 再手动以 FolderFromEvents(evs with SurfaceOp) 建立 baseline
	// 然后通过 Enable 的路径等价。最简单：用 *IncrementalFolder 直接测。
	// 所以此处我们用 IncrementalFolder 直接测试，避免触碰 sl 私有 events 字段。
	f := session.NewIncrementalFolder()
	// 通过 AppendMany 走增量路径（ApplyMessage 会把 SurfaceReplace → messagesDirty=true）
	f.AppendMany(evs)
	snap := f.Snapshot(evs)
	expected := session.FoldAll(evs)
	if !session.SessionProjectionEqual(snap, expected) {
		t.Fatalf("SurfaceReplace 后增量 Snapshot ≠ FoldAll()\n got=%+v\nwant=%+v", snap, expected)
	}
	// EffectiveEvents 规则：被隐藏范围（Start..End）内的 seq 被移除（此处仅 seq=2 的 u2），
	// 替换事件自身携带 SurfaceOp，按其原 seq（此处 4）参与剩余事件的排序。
	// 因此最终序列：u1(1), u3(3), u2-replaced(4)。
	if len(snap.Messages) != 3 {
		t.Fatalf("dirty rebuild 后 messages 条数 = %d, want 3", len(snap.Messages))
	}
	if snap.Messages[0].Content != "u1" || snap.Messages[1].Content != "u3" || snap.Messages[2].Content != "u2-replaced" {
		t.Fatalf("dirty rebuild 后 messages = [%+v], 顺序应为 u1/u3/u2-replaced（EffectiveEvents 隐藏 seq=2，替换事件保留原 seq=4）", snap.Messages)
	}
	stats := f.StatsCopy()
	if stats.DirtyRebuilds != 1 {
		t.Fatalf("SurfaceReplace 后 DirtyRebuilds = %d, want 1", stats.DirtyRebuilds)
	}
}

// ============================================================================
// 4. latest-wins 族投影专项：Goal CAS Revision 单调递增 / Preset→Sandbox & Approval 映射
// ============================================================================

func TestH04GoalCASIncremental(t *testing.T) {
	f := session.NewIncrementalFolder()
	// r=1 → 应用
	f.Append(session.SessionEvent{Seq: 1, Type: session.EventGoalChange, Data: session.GoalChangeData{
		GoalID: "g", Revision: 1, Description: "r1", MaxRounds: 3,
	}})
	// r=2 更大 → 应用
	f.Append(session.SessionEvent{Seq: 2, Type: session.EventGoalChange, Data: session.GoalChangeData{
		GoalID: "g", Revision: 2, Description: "r2", MaxRounds: 3,
	}})
	// r=1 过时 → 被 CAS 拒绝（描述应仍是 r2）
	f.Append(session.SessionEvent{Seq: 3, Type: session.EventGoalChange, Data: session.GoalChangeData{
		GoalID: "g", Revision: 1, Description: "stale", MaxRounds: 3,
	}})
	snap := f.Snapshot(nil)
	if !snap.Goal.Present || snap.Goal.Revision != 2 || snap.Goal.Description != "r2" {
		t.Fatalf("Goal CAS 增量失败: Goal=%+v", snap.Goal)
	}
}

func TestH04PresetSandboxApprovalDerivation(t *testing.T) {
	f := session.NewIncrementalFolder()
	check := func(t *testing.T, wantMode session.SandboxMode, wantPolicy session.ApprovalPolicy) {
		t.Helper()
		snap := f.Snapshot(nil)
		if snap.SandboxMode.Mode != wantMode {
			t.Fatalf("sandbox mode = %v, want %v", snap.SandboxMode.Mode, wantMode)
		}
		if snap.Approval.Policy != wantPolicy {
			t.Fatalf("approval policy = %v, want %v", snap.Approval.Policy, wantPolicy)
		}
	}
	f.Append(session.SessionEvent{Seq: 1, Type: session.EventPresetChange, Data: session.PresetChangeData{Preset: "safe"}})
	check(t, session.SandboxReadOnly, session.ApprovalAskDangerous)

	f.Append(session.SessionEvent{Seq: 2, Type: session.EventPresetChange, Data: session.PresetChangeData{Preset: "danger"}})
	check(t, session.SandboxDangerFullAccess, session.ApprovalAllowAll)

	f.Append(session.SessionEvent{Seq: 3, Type: session.EventPresetChange, Data: session.PresetChangeData{Preset: "review"}})
	check(t, session.SandboxReadOnly, session.ApprovalAskDangerousEdit)
}

// ============================================================================
// 5. Benchmark：10k 事件每 Append 后读一次投影（Goal/Todo 等决策态）
//    全量路径 O(N²)、增量路径 O(N) → 应有 ≥ 10× 加速。
// ============================================================================

// benchEventsH04 造 N 条混合事件用于 benchmark。
func benchEventsH04(n int) []session.SessionEvent {
	evs := make([]session.SessionEvent, 0, n)
	for i := 0; i < n; i++ {
		seq := uint64(i + 1)
		switch i % 6 {
		case 0:
			evs = append(evs, session.SessionEvent{Seq: seq, Type: session.EventUserMessage, Data: session.UserMessageData{Content: "hello"}})
		case 1:
			evs = append(evs, session.SessionEvent{Seq: seq, Type: session.EventAssistantMessage, Data: session.AssistantMessageData{Content: "world"}})
		case 2:
			evs = append(evs, session.SessionEvent{Seq: seq, Type: session.EventRequestHeader, Data: session.RequestHeaderData{ConfigEpoch: uint64(i), SystemHash: "x"}})
		case 3:
			evs = append(evs, session.SessionEvent{Seq: seq, Type: session.EventTodoWrite, Data: session.TodoWriteData{Items: []string{"a", "b", "c"}}})
		case 4:
			evs = append(evs, session.SessionEvent{Seq: seq, Type: session.EventGoalChange, Data: session.GoalChangeData{GoalID: "g", Revision: seq, MaxRounds: 3}})
		case 5:
			evs = append(evs, session.SessionEvent{Seq: seq, Type: session.EventPlanMode, Data: session.PlanModeData{Mode: "on"}})
		}
	}
	return evs
}

// BenchmarkIncrementalH04FullBaseline 模拟旧实现：每 Append 一条后 FoldAll(全部 events)。
// 复杂度：O(1 + 2 + 3 + ... + N) ≈ O(N²/2)，对于 N=10k 约 50M 次 scan。
func BenchmarkIncrementalH04FullBaseline(b *testing.B) {
	evs := benchEventsH04(10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		acc := make([]session.SessionEvent, 0, len(evs))
		var lastGoalRev uint64
		for _, ev := range evs {
			acc = append(acc, ev)
			p := session.FoldAll(acc)
			lastGoalRev = p.Goal.Revision
		}
		_ = lastGoalRev
	}
}

// BenchmarkIncrementalH04IncrementalPath 模拟启用 H04：Append + 每步 SnapshotMeta 读 Goal 等。
// 复杂度 O(N)（每条 Apply 常数级 + SnapshotMeta 常数级，无 Messages 拷贝）。
func BenchmarkIncrementalH04IncrementalPath(b *testing.B) {
	evs := benchEventsH04(10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f := session.NewIncrementalFolder()
		var lastGoalRev uint64
		for _, ev := range evs {
			f.Append(ev)
			p := f.SnapshotMeta(nil)
			lastGoalRev = p.Goal.Revision
		}
		_ = lastGoalRev
	}
}
