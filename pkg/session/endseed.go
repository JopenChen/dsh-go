// 本文件对应任务 M20：session/end-seed 种子边界 marker。
//
// 对齐上游：packages/core/session
//
// end-seed 标记 Resume/Fork 后第一条 live 写入的分界：
//   - 冷恢复（Resume）时先回放存量事件（seed），回放完成后写入一条 end-seed，
//     之后的 live 工作都在 end-seed 之后；
//   - Fork 子会话同样：回放父种子后、开始 live 前写入 end-seed；
//   - compaction 半开括号定位依赖它：只压缩 end-seed 之前（seed 区）或之后的 live 区。
package session

import (
	"github.com/JopenChen/dsh-go/pkg/brand"
)

// MarkEndSeed 写入 end-seed marker 事件。
//   - id 为当前会话 ID 或任何唯一标识；
//   - parentID 可空，标识 fork 来源；若携带则记录 EndSeedData.ParentSession。
//
// 调用时机：Resume/Fork 流程回放完种子事件后、开始 live 工作前。
func MarkEndSeed(sl *SessionLog, parentID brand.SessionID) (uint64, error) {
	return sl.Append(EndSeedData{ParentSession: parentID})
}

// SeedEndIndex 返回日志中最后一条 end-seed 事件的下标（不含则返回 -1）。
// SeedEndSeq 返回最后一条 end-seed 的 seq（不含则返回 0）。
func SeedEndSeq(events []SessionEvent) uint64 {
	var last uint64
	for _, ev := range events {
		if ev.Type == EventEndSeed {
			last = ev.Seq
		}
	}
	return last
}

// IsAfterEndSeed 判断某事件 seq 是否位于最后一次 end-seed 之后（live 区）。
func IsAfterEndSeed(events []SessionEvent, seq uint64) bool {
	seed := SeedEndSeq(events)
	if seed == 0 {
		// 从未写过 end-seed：全部视为 live
		return true
	}
	return seq > seed
}

// EndSeedMarker 返回记录在会话头部的种子长度分界（Resume/Fork 回放条数）。
// 该值由上层在回放结束时写入 SessionHeader.SeedLength。
func EndSeedMarker(header *SessionHeader) uint64 {
	if header == nil {
		return 0
	}
	return header.SeedLength
}