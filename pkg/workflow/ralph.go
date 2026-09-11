// ralph.go 复刻官方 workflow/tool-ralph 的纯编排：围绕一个固定目标，每轮让一个
// 全新子代理返回结构化报告，continue 则携带有界 handoff 进入下一轮，complete
// 成功收尾、blocked 需人工、达到 maxRounds 记为 budget-limited。
package workflow

import (
	"context"
	"encoding/json"
	"errors"
)

// RoundStatus 是单轮状态。
type RoundStatus string

const (
	StatusContinue RoundStatus = "continue"
	StatusComplete RoundStatus = "complete"
	StatusBlocked  RoundStatus = "blocked"
)

// RoundReport 是单轮子代理返回的结构化报告。
type RoundReport struct {
	Status    RoundStatus `json:"status"`
	Summary   string      `json:"summary"`
	Evidence  string      `json:"evidence"`
	NextSteps []string    `json:"nextSteps"`
	Blocker   string      `json:"blocker"`
}

// RunStatus 是整个 Ralph 循环的收尾状态。
type RunStatus string

const (
	RunCompleted     RunStatus = "complete"
	RunBlocked       RunStatus = "blocked"
	RunBudgetLimited RunStatus = "budget-limited"
)

// RalphResult 是循环结果。
type RalphResult struct {
	Status RunStatus
	Rounds int
	Final  RoundReport
}

// RoundRunner 运行一轮：传入目标与上一轮 handoff（首轮为空），返回本轮报告。
type RoundRunner func(ctx context.Context, objective, priorHandoff string) (RoundReport, error)

// DefaultRalphMaxRounds 是默认轮数上限。
const DefaultRalphMaxRounds = 256

// ValidateReport 校验报告与状态是否自洽。
func ValidateReport(r RoundReport) error {
	switch r.Status {
	case StatusComplete:
		if r.Evidence == "" || len(r.NextSteps) > 0 || r.Blocker != "" {
			return errors.New("complete 报告须有 evidence、无 nextSteps、blocker 为空")
		}
	case StatusContinue:
		if len(r.NextSteps) == 0 {
			return errors.New("continue 报告须至少含一条 nextSteps")
		}
	case StatusBlocked:
		if r.Blocker == "" {
			return errors.New("blocked 报告须给出 blocker")
		}
	default:
		return errors.New("无效的轮次状态")
	}
	return nil
}

// RunRalph 执行 Ralph 循环。maxRounds<=0 用默认值；maxHandoffChars>0 时校验 handoff 长度。
func RunRalph(ctx context.Context, objective string, runner RoundRunner, maxRounds, maxHandoffChars int) (*RalphResult, error) {
	if maxRounds <= 0 {
		maxRounds = DefaultRalphMaxRounds
	}
	prior := ""
	var last RoundReport
	for round := 1; round <= maxRounds; round++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		report, err := runner(ctx, objective, prior)
		if err != nil {
			return nil, err
		}
		if err := ValidateReport(report); err != nil {
			return nil, err
		}
		last = report
		switch report.Status {
		case StatusComplete:
			return &RalphResult{Status: RunCompleted, Rounds: round, Final: report}, nil
		case StatusBlocked:
			return &RalphResult{Status: RunBlocked, Rounds: round, Final: report}, nil
		}
		b, _ := json.Marshal(report)
		if maxHandoffChars > 0 && len(b) > maxHandoffChars {
			return nil, errors.New("handoff 超过 maxHandoffChars")
		}
		prior = string(b)
	}
	return &RalphResult{Status: RunBudgetLimited, Rounds: maxRounds, Final: last}, nil
}
