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
