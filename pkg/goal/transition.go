// 本文件补充官方 goal 的阶段迁移合法性（index.ts 中 pause/resume/complete/block
// 的 source-phase 白名单）。只校验目标态是否为合法四态并不够：complete 是终态，
// paused/blocked 只能从 active 进入，迁移非法时返回 GOAL_INVALID_TRANSITION。
package goal

// CanTransition 判断从 from 迁移到 to 是否合法。
//
//	active   → active/paused/blocked/complete
//	paused   → active/complete
//	blocked  → active/complete
//	complete → （终态，不可再迁移）
func CanTransition(from, to Phase) bool {
	if !to.Valid() || !from.Valid() {
		return false
	}
	if from == PhaseComplete {
		return false
	}
	switch to {
	case PhaseActive:
		// resume：active/paused/blocked 均可（active 需额外未 armed，由调用方判定）。
		return from == PhaseActive || from == PhasePaused || from == PhaseBlocked
	case PhasePaused, PhaseBlocked:
		// pause / block：仅 active。
		return from == PhaseActive
	case PhaseComplete:
		// complete：active/paused/blocked 均可。
		return from == PhaseActive || from == PhasePaused || from == PhaseBlocked
	default:
		return false
	}
}
