// 本文件实现任务 S16：Output Retention（tool result canonical value 保留到 committed）。
//
// 对齐上游：packages/core/tools output retention gate
//
// 设计要点：
//   - 大工具结果（如 bash 输出 / 文件读取全文）可能在 post-execute 阶段仍被多个
//     消费者并发读取（模型上下文组装 / 会话展示 / 审计）。若结果值被就地改写或
//     提前释放，会导致读到缺失/截断字节。
//   - ResultRetention 是一个以 brand.ToolCallID 为键的只读保留仓库：
//       * Retain：在工具「提交(committed)」时把 canonical 字节做不可变副本入仓；
//       * Read：并发安全地返回**完整独立副本**，读方拿到的是可安全持有的字节切片，
//         多读者互不干扰（RWMutex 保护 + 每次 append 拷贝）；
//       * Remove：语义上标记逻辑删除（供会话清理），不影响已完成安全的读；
//   - Read 返回的是每个 reader 各自独立的切片拷贝，因此不存在"两个 reader 互相截断"
//     的可能，满足验收「并发 reader 各得完整字节」。
package tools

import (
	"bytes"
	"encoding/json"
	"sync"
)

// ============================================================================
// ResultRetention：并发安全的只读结果保留仓库
// ============================================================================

// RetainedEntry 是仓库中的一条保留记录。raw 为不可变字节；retained 标记逻辑存在。
type RetainedEntry struct {
	raw      []byte
	retained bool
}

// ResultRetention 保留工具结果 canonical value，供并发 reader 全量读取。
type ResultRetention struct {
	mu   sync.RWMutex
	data map[string]RetainedEntry
}

// NewResultRetention 创建空保留仓库。
func NewResultRetention() *ResultRetention {
	return &ResultRetention{data: map[string]RetainedEntry{}}
}

// Retain 在 committed 阶段保留某工具调用的 canonical 字节（不可变副本）。
// 相同的 callID 反复保留采用最后一次（last-write-wins）。
func (r *ResultRetention) Retain(callID string, value []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// 拷贝一份不可变快照，避免调用方后续改写影响仓库内数据。
	r.data[callID] = RetainedEntry{
		raw:      append([]byte(nil), value...),
		retained: true,
	}
}

// Read 返回指定调用 ID 的**完整独立副本**。不存在或未保留返回 (nil, false)。
func (r *ResultRetention) Read(callID string) ([]byte, bool) {
	r.mu.RLock()
	e, ok := r.data[callID]
	r.mu.RUnlock()
	if !ok || !e.retained {
		return nil, false
	}
	// 每个 reader 拿独立副本：即使其它 reader 拷贝/改写也不影响彼此。
	return append([]byte(nil), e.raw...), true
}

// Remove 逻辑删除一条保留记录（读已完成者可继续持有已返回的副本）。
func (r *ResultRetention) Remove(callID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.data[callID]; ok {
		e.retained = false
		r.data[callID] = e
	}
}

// Len 返回当前保留的记录条数（含逻辑删除前，用于诊断）。
func (r *ResultRetention) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.data)
}

// ============================================================================
// Canonical 值抽取
// ============================================================================

// Canonicalize 把一次工具结果 Value 归一化为 canonical 字节。
// 支持 []byte / string / 其余任意值 JSON 序列化；nil 返回空。
func Canonicalize(value any) []byte {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return v
	case string:
		return []byte(v)
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return []byte{}
		}
		return b
	}
}

// RetainResult 便捷版：把 ToolCallResult 的 canonical 值保留到仓库。
func (r *ResultRetention) RetainResult(res *ToolCallResult) {
	if res == nil {
		return
	}
	r.Retain(res.CallID.Raw(), Canonicalize(res.Value))
}

// Equal 判断字节是否一致（供测试原子性断言）。
func Equal(a, b []byte) bool {
	return bytes.Equal(a, b)
}