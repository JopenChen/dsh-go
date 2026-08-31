// Package goal 提供 Goal（目标）系统：状态机 + 续轮驱动 + 6 个 goal_* 工具。
//
// 对齐上游：packages/core/goal
//
// 设计要点：
//   - 目标状态经 goal/change 事件持久化（CAS revision 并发保护，单增）；
//   - RoundDriver 监听 turn-stopping：目标 active 时自动续轮，直到 maxRounds 达上限或完成；
//   - 6 个工具：goal_list / goal_set_phase / goal_set_description / goal_set_max_rounds /
//     goal_add_blocker / goal_report_blocker；其中 report_blocker 通过 concludeTurn 结束 turn。
package goal

import (
	"context"

	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

// ============================================================================
// 状态机
// ============================================================================

// Phase 是目标阶段。
type Phase string

// 阶段枚举（对齐官方 packages/goal/goal/src/types.ts 四态）。
const (
	PhaseActive   Phase = "active"
	PhasePaused   Phase = "paused"
	PhaseBlocked  Phase = "blocked"
	PhaseComplete Phase = "complete"
)

// Valid 返回该阶段是否为合法的四态之一（官方 GoalPhase）。
func (p Phase) Valid() bool {
	switch p {
	case PhaseActive, PhasePaused, PhaseBlocked, PhaseComplete:
		return true
	default:
		return false
	}
}

// Goal 是目标运行状态。
type Goal struct {
	ID          string            `json:"id"`
	Phase       Phase             `json:"phase"`
	Description string            `json:"description"`
	MaxRounds   int               `json:"maxRounds"`
	Revision    uint64            `json:"revision"`
	Blockers    []string          `json:"blockers,omitempty"`
	// BlockReason 仅在 phase=blocked 时存在（官方 GoalSnapshot.blockedReason）。
	BlockReason *GoalBlockReason  `json:"blockedReason,omitempty"`
}

// FromLog 从日志派生最新目标状态（CAS：revision 单增取最新）。
func FromLog(sl *session.SessionLog) (Goal, bool) {
	fold := session.FoldGoalChange(sl.Events())
	if !fold.Present {
		return Goal{}, false
	}
	return Goal{
		ID:          fold.GoalID,
		Phase:       phaseFrom(fold.Phase),
		Description: fold.Description,
		MaxRounds:   fold.MaxRounds,
		Revision:    fold.Revision,
	}, true
}

// phaseFrom 转换 fold 阶段。
func phaseFrom(p session.GoalPhase) Phase {
	return Phase(p)
}

// writeGoal 写一条 goal/change（CAS revision = 当前 + 1）。
func writeGoal(sl *session.SessionLog, g Goal) error {
	_, err := sl.Append(session.GoalChangeData{
		GoalID:      g.ID,
		Phase:       session.GoalPhase(g.Phase),
		Description: g.Description,
		MaxRounds:   g.MaxRounds,
		Revision:    g.Revision,
	})
	return err
}

// NewGoal 创建一个新目标（revision 从 1 开始）。
func NewGoal(id, desc string, maxRounds int) Goal {
	return Goal{ID: id, Phase: PhaseActive, Description: desc, MaxRounds: maxRounds, Revision: 1}
}

// ============================================================================
// RoundDriver 续轮驱动
// ============================================================================

// RoundDriver 监听 turn-stopping：目标 active 时决定是否续轮。
type RoundDriver struct {
	// roundCh 续轮信号（供 Agent 循环等待）。
	roundCh chan int
}

// NewRoundDriver 创建续轮驱动。
func NewRoundDriver() *RoundDriver {
	return &RoundDriver{roundCh: make(chan int, 1)}
}

// OnTurnStopping 在 turn 停止时被调用，决定是否启动下一轮。
// 返回是否需要续轮。
func (d *RoundDriver) OnTurnStopping(sl *session.SessionLog, round int) bool {
	g, ok := FromLog(sl)
	// 仅 active 目标自动续轮（官方 goal-round-driver 语义：
	// paused / blocked / complete 都不继续）。
	if !ok || g.Phase != PhaseActive {
		return false
	}
	// 轮次未超上限：继续
	if round < g.MaxRounds {
		// 写 goal/round 事件
		_, _ = sl.Append(session.GoalRoundData{Round: round + 1})
		select {
		case d.roundCh <- round + 1:
		default:
		}
		return true
	}
	return false
}

// WaitRound 阻塞等待下一次续轮信号（超时/取消用 ctx）。
func (d *RoundDriver) WaitRound(ctx context.Context) (int, error) {
	select {
	case r := <-d.roundCh:
		return r, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// ============================================================================
// 6 个 goal_* 工具
// ============================================================================

// GoalToolset 是一组 goal 工具。
type GoalToolset struct {
	sl *session.SessionLog
}

// NewGoalToolset 创建 goal 工具集。
func NewGoalToolset(sl *session.SessionLog) *GoalToolset {
	return &GoalToolset{sl: sl}
}

// Tools 返回全部 6 个工具的 *tools.Tool 切片（注册进 M23 pipeline）。
func (g *GoalToolset) Tools() []*tools.Tool {
	return []*tools.Tool{
		{Name: "goal_list", Execute: g.goalList},
		{Name: "goal_set_phase", Execute: g.goalSetPhase},
		{Name: "goal_set_description", Execute: g.goalSetDescription},
		{Name: "goal_set_max_rounds", Execute: g.goalSetMaxRounds},
		{Name: "goal_add_blocker", Execute: g.goalAddBlocker},
		{Name: "goal_report_blocker", Execute: g.goalReportBlocker},
	}
}

func (g *GoalToolset) current() (Goal, bool) {
	return FromLog(g.sl)
}

// goalList 列出目标状态。
func (g *GoalToolset) goalList(ctx context.Context, input map[string]any) (any, error) {
	if cur, ok := g.current(); ok {
		return map[string]any{"goal": cur}, nil
	}
	return map[string]any{"goal": nil}, nil
}

// goalSetPhase 设置阶段。
func (g *GoalToolset) goalSetPhase(ctx context.Context, input map[string]any) (any, error) {
	phase, _ := input["phase"].(string)
	cur, ok := g.current()
	if !ok {
		return nil, NewGoalError(ErrorNotFound, "no active goal", nil)
	}
	// 校验非法阶段（官方 GOAL_INVALID_TRANSITION）。
	if !Phase(phase).Valid() {
		return nil, NewGoalError(ErrorInvalidTransition, "invalid phase: "+phase, nil)
	}
	cur.Phase = Phase(phase)
	cur.Revision++
	if err := writeGoal(g.sl, cur); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "phase": phase}, nil
}

// goalSetDescription 设置目标描述。
func (g *GoalToolset) goalSetDescription(ctx context.Context, input map[string]any) (any, error) {
	desc, _ := input["description"].(string)
	cur, ok := g.current()
	if !ok {
		return nil, NewGoalError(ErrorNotFound, "no active goal", nil)
	}
	cur.Description = desc
	cur.Revision++
	if err := writeGoal(g.sl, cur); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "description": desc}, nil
}

// goalSetMaxRounds 设置最大轮次。
func (g *GoalToolset) goalSetMaxRounds(ctx context.Context, input map[string]any) (any, error) {
	rounds, _ := input["maxRounds"].(float64)
	cur, ok := g.current()
	if !ok {
		return nil, NewGoalError(ErrorNotFound, "no active goal", nil)
	}
	// 校验非正数（官方 GOAL_INVALID_MAX_ROUNDS）。
	if int(rounds) <= 0 {
		return nil, NewGoalError(ErrorInvalidMaxRounds, "maxRounds must be positive", nil)
	}
	cur.MaxRounds = int(rounds)
	cur.Revision++
	if err := writeGoal(g.sl, cur); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "maxRounds": cur.MaxRounds}, nil
}

// goalAddBlocker 增加阻塞项。
func (g *GoalToolset) goalAddBlocker(ctx context.Context, input map[string]any) (any, error) {
	blocker, _ := input["blocker"].(string)
	cur, ok := g.current()
	if !ok {
		return nil, NewGoalError(ErrorNotFound, "no active goal", nil)
	}
	// 校验阻塞原因非空（官方 GOAL_INVALID_BLOCK_REASON）。
	if blocker == "" {
		return nil, NewGoalError(ErrorInvalidBlockReason, "blocker required", nil)
	}
	cur.Blockers = append(cur.Blockers, blocker)
	cur.Revision++
	if err := writeGoal(g.sl, cur); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "blockers": cur.Blockers}, nil
}

// goalReportBlocker 报告阻塞并 concludeTurn 结束 turn。
func (g *GoalToolset) goalReportBlocker(ctx context.Context, input map[string]any) (any, error) {
	blocker, _ := input["blocker"].(string)
	phase := PhaseBlocked
	if ph, ok := input["phase"].(string); ok && ph != "" {
		phase = Phase(ph)
	}
	cur, ok := g.current()
	if !ok {
		return nil, NewGoalError(ErrorNotFound, "no active goal", nil)
	}
	// 阻塞必须说明原因（GOAL_INVALID_BLOCK_REASON）。
	if blocker == "" {
		return nil, NewGoalError(ErrorInvalidBlockReason, "blocker reason required", nil)
	}
	cur.Phase = phase
	cur.Blockers = append(cur.Blockers, blocker)
	// blocked 时带稳定 BlockReason。
	if phase == PhaseBlocked {
		cur.BlockReason = &GoalBlockReason{Code: "user-reported", Message: blocker}
	}
	cur.Revision++
	if err := writeGoal(g.sl, cur); err != nil {
		return nil, err
	}
	// concludeTurn 结束 turn（Agent 不再进行下一个 Step）
	if conclude := tools.ConcludeFrom(ctx); conclude != nil {
		conclude("goal-blocker")
	}
	out := map[string]any{"ok": true, "phase": phase, "blockers": cur.Blockers}
	if cur.BlockReason != nil {
		out["blockReason"] = cur.BlockReason
	}
	return out, nil
}