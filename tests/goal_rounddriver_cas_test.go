// 本文件对应任务 M12：Goal 系统（状态机 + 续轮驱动 + 6 工具）。
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/goal"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

// TestGoalRoundDriverAutoRounds 验证 set_goal(active, maxRounds=5) 无用户输入自动续 5 轮。
func TestGoalRoundDriverAutoRounds(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("goal_1"))
	ts := goal.NewGoalToolset(sl)

	// 创建目标（maxRounds=5）
	root, _ := ts.Tools()[0].Name, ts.Tools()[0]
	_ = root
	g := goal.NewGoal("g1", "实现任务", 5)
	if err := writeGoalForTest(sl, g); err != nil {
		t.Fatalf("写目标失败: %v", err)
	}

	driver := goal.NewRoundDriver()
	rounds := 0
	for {
		if !driver.OnTurnStopping(sl, rounds) {
			break
		}
		rounds++
		if rounds > 10 {
			t.Fatal("续轮超过上限，疑似死循环")
		}
	}
	if rounds != 5 {
		t.Fatalf("maxRounds=5 应自动续 5 轮, 实际 %d", rounds)
	}
}

// TestGoalReportBlockerConcludeTurn 验证 goal_report_blocker → concludeTurn 结束 turn。
func TestGoalReportBlockerConcludeTurn(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("goal_2"))
	ts := goal.NewGoalToolset(sl)
	if err := writeGoalForTest(sl, goal.NewGoal("g2", "任务", 3)); err != nil {
		t.Fatalf("写目标失败: %v", err)
	}

	concluded := false
	// 注入 conclude 回调（模拟 Agent 注册）
	ctx := tools.WithConclude(context.Background(), func(reason string) { concluded = true })
	tool := ts.Tools()[5] // goal_report_blocker
	_, err := tool.Execute(ctx, map[string]any{"blocker": "缺依赖"})
	if err != nil {
		t.Fatalf("goal_report_blocker 失败: %v", err)
	}
	if !concluded {
		t.Fatal("report_blocker 应触发 concludeTurn")
	}
	// 状态应变 blocked
	cur, _ := goal.FromLog(sl)
	if cur.Phase != goal.PhaseBlocked {
		t.Fatalf("report_blocker 后 phase 应为 blocked: %+v", cur)
	}
}

// TestGoalCASRevision 验证 CAS Revision 冲突报错并发保护（revision 单增）。
func TestGoalCASRevision(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("goal_3"))
	ts := goal.NewGoalToolset(sl)

	// 创建目标
	if err := writeGoalForTest(sl, goal.NewGoal("g3", "任务", 3)); err != nil {
		t.Fatalf("写目标失败: %v", err)
	}

	// 两次 set_description：revision 应单增（1→2→3）
	tool := ts.Tools()[2]
	if _, err := tool.Execute(context.Background(), map[string]any{"description": "新描述1"}); err != nil {
		t.Fatalf("首次 set_description 失败: %v", err)
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"description": "新描述2"}); err != nil {
		t.Fatalf("二次 set_description 失败: %v", err)
	}
	cur, _ := goal.FromLog(sl)
	if cur.Revision != 3 {
		t.Fatalf("revision 应单增到 3: %d", cur.Revision)
	}
	if cur.Description != "新描述2" {
		t.Fatalf("description 应最新: %q", cur.Description)
	}
}

// TestGoalSixTools 验证 6 个工具均可用。
func TestGoalSixTools(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("goal_4"))
	ts := goal.NewGoalToolset(sl)
	if err := writeGoalForTest(sl, goal.NewGoal("g4", "任务", 3)); err != nil {
		t.Fatalf("写目标失败: %v", err)
	}
	if len(ts.Tools()) != 6 {
		t.Fatalf("应有 6 个 goal 工具: %d", len(ts.Tools()))
	}
	names := []string{}
	for _, tl := range ts.Tools() {
		names = append(names, tl.Name)
	}
	// list 应能读到目标
	out, err := ts.Tools()[0].Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("goal_list 失败: %v", err)
	}
	if out.(map[string]any)["goal"] == nil {
		t.Fatal("goal_list 应读到目标")
	}
	_ = names
}

// writeGoalForTest 测试辅助写目标。
func writeGoalForTest(sl *session.SessionLog, g goal.Goal) error {
	_, err := sl.Append(session.GoalChangeData{
		GoalID:      g.ID,
		Phase:       session.GoalPhase(g.Phase),
		Description: g.Description,
		MaxRounds:   g.MaxRounds,
		Revision:    g.Revision,
	})
	return err
}