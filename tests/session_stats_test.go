// Package tests 的 session-stats 耗时统计验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/telemetry"
)

func TestStatsCollectorStep(t *testing.T) {
	c := telemetry.NewStatsCollector()
	c.StepStart(0, 0)
	c.FirstToken(0, 100)
	c.Message(0, 500)
	c.StepEnd(0)
	s := c.Snapshot()
	if s.Steps != 1 || s.Turns != 1 {
		t.Fatalf("counts = %+v", s)
	}
	if s.LLMMs != 500 || s.TTFTMs != 100 {
		t.Fatalf("timing = %+v", s)
	}
}

func TestStatsCollectorTurnDedup(t *testing.T) {
	c := telemetry.NewStatsCollector()
	// 同一 turn 两个 step，turns 只计一次。
	c.StepEnd(0)
	c.StepEnd(0)
	c.StepEnd(1)
	if s := c.Snapshot(); s.Steps != 3 || s.Turns != 2 {
		t.Fatalf("dedup = %+v", s)
	}
}

func TestStatsCollectorTool(t *testing.T) {
	c := telemetry.NewStatsCollector()
	c.ToolCall("id1", 0)
	c.ToolResult("id1", 250)
	// 无对应 call 的 result 忽略。
	c.ToolResult("nope", 999)
	if s := c.Snapshot(); s.ToolMs != 250 {
		t.Fatalf("tool = %+v", s)
	}
}
