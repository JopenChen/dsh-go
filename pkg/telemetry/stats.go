// stats.go 复刻官方 session/session-stats 的纯 fold：从 step/tool 边界事件增量
// 统计回合数、步数以及模型耗时、首 token 延迟、工具耗时等墙钟时间。
package telemetry

// Stats 是整段日志的累计统计。
type Stats struct {
	Turns  int // 至少闭合一个 step 的不同 turn 数
	Steps  int // 闭合的 step 数
	LLMMs int64 // 模型墙钟耗时累计（step/start→assistant/message）
	TTFTMs int64 // 首 token 延迟累计
	ToolMs int64 // 工具 call→result 耗时累计
}

type openStepStat struct {
	turn       int
	start      int64
	firstToken int64 // 0 表示尚未出现
}

// StatsCollector 增量统计器（非并发安全，单线程 fold 使用）。
type StatsCollector struct {
	stats      Stats
	lastTurn   *int
	open       *openStepStat
	pending    map[string]int64
}

// NewStatsCollector 创建统计器。
func NewStatsCollector() *StatsCollector {
	return &StatsCollector{pending: map[string]int64{}}
}

// StepStart 记录 step 开始。
func (c *StatsCollector) StepStart(turn int, timeMs int64) {
	c.open = &openStepStat{turn: turn, start: timeMs}
}

// FirstToken 记录当前 step 的首个非空 token（重复调用只认第一次）。
func (c *StatsCollector) FirstToken(turn int, timeMs int64) {
	if c.open == nil || c.open.turn != turn || c.open.firstToken > 0 {
		return
	}
	c.open.firstToken = timeMs
}

// Message 在消息装配完成时结算模型耗时与首 token 延迟。
func (c *StatsCollector) Message(turn int, timeMs int64) {
	if c.open == nil || c.open.turn != turn {
		return
	}
	if d := timeMs - c.open.start; d > 0 {
		c.stats.LLMMs += d
	}
	if c.open.firstToken > 0 {
		if d := c.open.firstToken - c.open.start; d > 0 {
			c.stats.TTFTMs += d
		}
	}
	c.open = nil
}

// ToolCall 记录一次工具派发。
func (c *StatsCollector) ToolCall(callID string, timeMs int64) {
	c.pending[callID] = timeMs
}

// ToolResult 结算一次工具耗时（无对应 call 则忽略）。
func (c *StatsCollector) ToolResult(callID string, timeMs int64) {
	dispatched, ok := c.pending[callID]
	if !ok {
		return
	}
	delete(c.pending, callID)
	if d := timeMs - dispatched; d > 0 {
		c.stats.ToolMs += d
	}
}

// StepEnd 记录 step 闭合，并按 turn 去重计数。
func (c *StatsCollector) StepEnd(turn int) {
	if c.lastTurn == nil || *c.lastTurn != turn {
		c.stats.Turns++
		t := turn
		c.lastTurn = &t
	}
	c.stats.Steps++
	c.open = nil
}

// Snapshot 返回当前统计快照。
func (c *StatsCollector) Snapshot() Stats {
	return c.stats
}
