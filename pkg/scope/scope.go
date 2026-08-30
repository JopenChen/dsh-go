// Package scope 提供分层作用域注册表（ScopedLayers）原语。
//
// 对齐上游：packages/core/scope
//
// 设计动机：
//   - dsh-go 的 Tools / Skills / Commands / Credentials 等注册表都存在"宿主级（host）定义 + 会话级 / 用户级 / 项目级覆盖"的需求，
//     例如：预设定义了一批工具，会话级可以隐藏其中某个；宿主级 deny 的凭据，作用域级可以 exempt；
//   - 本包用一套通用的「分层 + 合并 + 解析」实现复用于所有注册表，避免每个注册表各写一套作用域叠加逻辑。
//
// 解析规则（与上游一致）：
//   1. nearest-scope-wins：从最近的作用域层向 host 层逐层查找，先命中的层决定值；
//   2. rank：同一层内同 key 存在多个注册时，rank 大者胜；
//   3. 后注册覆盖先注册：rank 相同时，注册时间靠后者胜；
//   4. 匿名 entry（未命名）与 named entry（具名）互不冲突：匿名条目不参与具名查找，
//      仅在整体遍历时按注册顺序出现。
package scope

import (
	"sort"
	"sync"
)

// Key 标识一个作用域层（如 "host"、"user:xxx"、"session:xxx"、"project:xxx"）。
type Key string

// entry 是作用域层内的一条注册记录。
type entry[V any] struct {
	name  string // 具名 key；空串表示匿名 entry
	value V
	rank  int    // 同 key 冲突时的优先级，rank 大者胜
	seq   uint64 // 注册序号，用于 rank 相同时"后注册覆盖先注册"
}

// Layer 是单层作用域（同一 ScopeKey 下的条目集合）。
// 内部使用切片保留注册顺序，保证后注册覆盖先注册的确定性。
type Layer[V any] struct {
	key     Key
	entries []entry[V]
	mu      sync.RWMutex
	seq     uint64
}

// NewLayer 创建指定作用域键的空层。
func NewLayer[V any](key Key) *Layer[V] {
	return &Layer[V]{key: key}
}

// Key 返回该层的唯一标识。
func (l *Layer[V]) Key() Key {
	return l.key
}

// Register 向本层注册一条记录。
//   - name 为空表示匿名 entry；
//   - rank 越大优先级越高（同一层内同 key 冲突时）；
//   - 返回注册序号（供调用方构造依赖顺序时使用）。
func (l *Layer[V]) Register(name string, value V, rank int) uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	l.entries = append(l.entries, entry[V]{name: name, value: value, rank: rank, seq: l.seq})
	return l.seq
}

// get 在本层内按 name 查找（不跨层）：rank 大者胜，rank 相同则后注册胜。
func (l *Layer[V]) get(name string) (V, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var (
		found  bool
		winner entry[V]
	)
	for _, e := range l.entries {
		if e.name != name {
			continue
		}
		if !found || e.rank > winner.rank || (e.rank == winner.rank && e.seq > winner.seq) {
			found = true
			winner = e
		}
	}
	return winner.value, found
}

// Get 是本层内的具名查找（导出版本，供外部判断某值是否存在于本层）。
func (l *Layer[V]) Get(name string) (V, bool) {
	return l.get(name)
}

// Has 判断本层是否存在该具名条目。
func (l *Layer[V]) Has(name string) bool {
	_, ok := l.get(name)
	return ok
}

// Unregister 从本层移除所有匹配该 name 的具名条目（匿名条目不受影响）。
func (l *Layer[V]) Unregister(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.entries[:0]
	for _, e := range l.entries {
		if e.name == name {
			continue
		}
		out = append(out, e)
	}
	l.entries = out
}

// snapshot 返回本层全部条目的浅拷贝，供 ScopedLayers 合并使用。
func (l *Layer[V]) snapshot() []entry[V] {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]entry[V], len(l.entries))
	copy(out, l.entries)
	return out
}

// ScopedLayers 是「host → 最近作用域」按顺序叠加的多层注册表。
// 层顺序：索引 0 为 host（最外层），索引越靠后作用域越近（越优先）。
type ScopedLayers[V any] struct {
	mu     sync.RWMutex
	layers []*Layer[V]
}

// NewScopedLayers 创建空的分层注册表。
func NewScopedLayers[V any]() *ScopedLayers[V] {
	return &ScopedLayers[V]{}
}

// Push 追加一层作用域（追加后成为"最近的"作用域，优先于之前所有层）。
// host 层应最先 Push。
func (sl *ScopedLayers[V]) Push(layer *Layer[V]) *ScopedLayers[V] {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.layers = append(sl.layers, layer)
	return sl
}

// Get 按 nearest-scope-wins 规则解析具名 key 的值。
// 从最近层向 host 层逐层查找，任一命中即返回；全部未命中返回 false。
func (sl *ScopedLayers[V]) Get(name string) (V, bool) {
	if name == "" {
		var zero V
		return zero, false
	}
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	// 从最近的层（末尾）向 host 层（开头）查找
	for i := len(sl.layers) - 1; i >= 0; i-- {
		if v, ok := sl.layers[i].get(name); ok {
			return v, true
		}
	}
	var zero V
	return zero, false
}

// ResolvedEntry 是 Merge 后的单条解析结果。
type ResolvedEntry[V any] struct {
	Name  string // 具名 key；匿名条目为空
	Value V
	Rank  int
}

// Merge 将所有层合并为一个确定性视图：
//   - 具名条目：按 nearest-scope-wins 解析，同层内 rank/注册序决胜，输出唯一一份；
//   - 匿名条目：全部保留（来自不同层、按注册顺序）。
//
// 返回值按名称字典序 + 匿名条目在前的规则排序，保证多次调用输出逐字节一致。
func (sl *ScopedLayers[V]) Merge() []ResolvedEntry[V] {
	sl.mu.RLock()
	layers := make([]*Layer[V], len(sl.layers))
	copy(layers, sl.layers)
	sl.mu.RUnlock()

	// 第一遍：收集所有具名 key，逐层解析
	nameSet := map[string]struct{}{}
	var anonymous []ResolvedEntry[V]
	for _, layer := range layers {
		for _, e := range layer.snapshot() {
			if e.name == "" {
				anonymous = append(anonymous, ResolvedEntry[V]{Name: "", Value: e.value, Rank: e.rank})
				continue
			}
			nameSet[e.name] = struct{}{}
		}
	}

	names := make([]string, 0, len(nameSet))
	for n := range nameSet {
		names = append(names, n)
	}
	// 字典序排序保证确定性
	sort.Strings(names)

	resolved := make([]ResolvedEntry[V], 0, len(names)+len(anonymous))
	for _, n := range names {
		if v, ok := sl.Get(n); ok {
			resolved = append(resolved, ResolvedEntry[V]{Name: n, Value: v, Rank: 0})
		}
	}
	resolved = append(resolved, anonymous...)
	return resolved
}
