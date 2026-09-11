// Package retain 复刻官方 util/output-retention 的逻辑单元有界保留：向保留器持续
// push 观察到的单元，只保留前 maxItems 个，其余精确计数为省略，finish 给出保留
// 子集与省略元数据。它只回答"保留了什么、省略了多少"，不含任何工具业务语义。
package retain

// Result 是保留结果。
type Result[T any] struct {
	Items     []T // 保留的子集
	Seen      int  // 观察到的总数
	Omitted   int  // 因预算省略的数量
	Truncated bool // 是否发生省略
}

// ItemRetainer 保留前 maxItems 个逻辑单元（head）。
type ItemRetainer[T any] struct {
	maxItems int
	items    []T
	seen     int
	omitted  int
}

// NewItemRetainer 创建 head 保留器；maxItems 为负按 0 处理。
func NewItemRetainer[T any](maxItems int) *ItemRetainer[T] {
	if maxItems < 0 {
		maxItems = 0
	}
	return &ItemRetainer[T]{maxItems: maxItems}
}

// Push 提供一个观察单元：未满则保留，已满则计入省略。返回该单元是否被保留。
func (r *ItemRetainer[T]) Push(item T) bool {
	r.seen++
	if len(r.items) < r.maxItems {
		r.items = append(r.items, item)
		return true
	}
	r.omitted++
	return false
}

// Finish 结算保留结果。
func (r *ItemRetainer[T]) Finish() Result[T] {
	return Result[T]{
		Items:     r.items,
		Seen:      r.seen,
		Omitted:   r.omitted,
		Truncated: r.omitted > 0,
	}
}
