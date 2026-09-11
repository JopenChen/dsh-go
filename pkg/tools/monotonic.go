// 本文件实现工具流水线中的「单调守卫」（Monotonic Guard）。
//
// 上游语义：pre-execute 是可被多个策略插件重排的策略层，而单调守卫是夹在
// pre-execute 与 execute 之间的最终防线——决策只允许朝「更确定 / 更严格」方向
// 收敛，不允许被后续监听器撤销或放宽。
//
// 偏序关系（从严到宽）：deny > ask > allow
//   - 初始未定（ask：等待后续策略）；
//   - 一旦收敛为 deny，任何试图改回 allow/ask 的更新都被拒绝（fail closed）；
//   - allow 之后仍可被更靠后的安全策略升级为 deny（安全优先，可收紧不可放宽）。
package tools

import (
	"errors"
)

// ErrGuardRelaxed 表示单调守卫拒绝了一次"放宽已收敛决策"的更新。
var ErrGuardRelaxed = errors.New("tools monotonic guard: cannot relax an already stricter decision")

// guardRank 给三态决策一个严格度秩（越大越严格）。
func guardRank(d PreToolDecision) int {
	switch d {
	case PreDeny:
		return 2
	case PreAsk:
		return 1
	default: // PreAllow
		return 0
	}
}

// MonotonicGuard 是一次性的决策收敛器，服务于单次工具调用。
type MonotonicGuard struct {
	current PreToolDecision
	settled bool // 是否已被显式赋值
}

// NewMonotonicGuard 创建守卫，初始为未定（ask 语义）。
func NewMonotonicGuard() *MonotonicGuard {
	return &MonotonicGuard{current: PreAsk}
}

// Update 尝试用 next 收敛当前决策。
//
//   - 首次赋值总是接受；
//   - 后续仅当 next 不严格宽于当前（秩 >= 当前）时接受；
//   - 试图把已更严的决策放宽 → 返回 ErrGuardRelaxed，且保持原决策不变。
func (g *MonotonicGuard) Update(next PreToolDecision) error {
	if !g.settled {
		g.current = next
		g.settled = true
		return nil
	}
	if guardRank(next) < guardRank(g.current) {
		return ErrGuardRelaxed
	}
	g.current = next
	return nil
}

// Decision 返回当前收敛到的决策。
func (g *MonotonicGuard) Decision() PreToolDecision {
	if !g.settled {
		return PreAsk
	}
	return g.current
}

// IsDenied 是否已收敛为拒绝（fail-closed 判定）。
func (g *MonotonicGuard) IsDenied() bool {
	return g.settled && g.current == PreDeny
}
