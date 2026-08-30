// 本文件定义 SurfaceOp（表面操作）类型，供 SessionEvent 使用。
// 完整 foldSurface 实现见任务 M21。
package session

import "encoding/json"

// SurfaceOpKind 表面操作类型。
type SurfaceOpKind string

// 表面操作枚举。
const (
	// SurfaceAppend 普通追加（默认，无需显式声明）。
	SurfaceAppend SurfaceOpKind = "append"
	// SurfaceReplace 表面替换：将 [Start, End] 范围的旧事件在"读取时"替换为 Data，
	// 源事件本身不可变（compaction 唯一合法回写历史的方式）。
	SurfaceReplace SurfaceOpKind = "replace"
)

// SurfaceOp 描述一次表面操作（世代标记）。
//   - op=replace 时，Start/End 标记被替换的旧事件 seq 范围（闭区间），Data 为替换载荷；
//   - 该结构被持久化在 SessionEvent.surfaceOp 字段，读时应用，不改写源事件。
type SurfaceOp struct {
	Op    SurfaceOpKind  `json:"op"`
	Start uint64         `json:"start,omitempty"`
	End   uint64         `json:"end,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// NewAppendOp 构造一个追加标记（默认语义）。
func NewAppendOp() *SurfaceOp {
	return &SurfaceOp{Op: SurfaceAppend}
}

// NewReplaceOp 构造一个替换标记。
func NewReplaceOp(start, end uint64, data json.RawMessage) *SurfaceOp {
	return &SurfaceOp{Op: SurfaceReplace, Start: start, End: end, Data: data}
}

// ============================================================================
// FoldSurface：表面折叠（任务 M21）
// ============================================================================

// SurfaceNode 是表面折叠后的单个节点（源事件或替换事件）。
type SurfaceNode struct {
	// Seq 事件序号。
	Seq uint64
	// Replaced 是否为替换事件（携带 surfaceOp=replace）。
	Replaced bool
	// Event 该节点的完整事件。
	Event SessionEvent
}

// ReplaceRange 描述一次表面替换覆盖的 seq 范围。
type ReplaceRange struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

// FoldSurface 折叠表面：读时应用所有 surface replace，返回：
//   - nodes：有效节点列表（被替换范围内的旧事件被隐藏，替换事件自身保留）；
//   - replacements：全部替换范围（供 compaction 审计与测试断言）。
//
// 源事件永不被修改——替换是"读时"视角，持久化数据保持 append-only。
func FoldSurface(events []SessionEvent) (nodes []SurfaceNode, replacements []ReplaceRange) {
	hidden := map[uint64]bool{}
	for _, ev := range events {
		if ev.SurfaceOp != nil && ev.SurfaceOp.Op == SurfaceReplace {
			replacements = append(replacements, ReplaceRange{Start: ev.SurfaceOp.Start, End: ev.SurfaceOp.End})
			for s := ev.SurfaceOp.Start; s <= ev.SurfaceOp.End && s <= ev.Seq; s++ {
				hidden[s] = true
			}
		}
	}
	for _, ev := range events {
		if hidden[ev.Seq] {
			continue
		}
		replaced := ev.SurfaceOp != nil && ev.SurfaceOp.Op == SurfaceReplace
		nodes = append(nodes, SurfaceNode{Seq: ev.Seq, Replaced: replaced, Event: ev})
	}
	return nodes, replacements
}
