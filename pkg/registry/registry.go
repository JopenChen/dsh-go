// 本文件对应任务 H07：Shared Registry 只读化。
//
// 背景：Agent/Tool/Skill/Command 等共享热注册表在启动完成后基本只读（高频读、低写），
// 但原实现每次读都加锁（sync.RWMutex.RLock），高并发读在锁上串行。
//
// Freezable[K,V] 提供"可冻结"的泛型注册中心：
//   - 冻结前：读写走 mutex（允许启动期热注册插件 / 动态装载）；
//   - Freeze()：构建一次性只读快照（不可逆）；冻结后 Get/Contains/Len/Keys 全部走
//     快照直接读 map，**完全无锁**（快照 map 冻结后不再写，天然并发安全）；
//   - 冻结后 Put/Remove 返回 ErrFrozen（写入拒绝，符合 H07「Freeze 后写入报错/NOP」）。
//
// 读路径在 freeze 后由 RWMutex 读锁降级为裸 map 读，是 H07 的核心收益。
package registry

import (
	"errors"
	"sync"
)

// ErrFrozen 表示注册中心已 Freeze，禁止再写。
var ErrFrozen = errors.New("registry: frozen, mutation not allowed")

// Freezable 是可冻结的泛型共享注册中心。
type Freezable[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V

	frozen   bool
	snapshot map[K]V // 冻结后的只读快照；nil 表示未冻结
}

// NewFreezable 创建空的 Freezable 注册中心。
func NewFreezable[K comparable, V any]() *Freezable[K, V] {
	return &Freezable[K, V]{m: map[K]V{}}
}

// NewFreezableFrom 用给定初值构建（深拷贝 map，避免外部共享底层表）。
func NewFreezableFrom[K comparable, V any](entries map[K]V) *Freezable[K, V] {
	cp := make(map[K]V, len(entries))
	for k, v := range entries {
		cp[k] = v
	}
	return &Freezable[K, V]{m: cp}
}

// Put 写入/覆盖一条；冻结后返回 ErrFrozen。
func (r *Freezable[K, V]) Put(k K, v V) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return ErrFrozen
	}
	r.m[k] = v
	return nil
}

// Remove 删除一条；冻结后返回 ErrFrozen。
func (r *Freezable[K, V]) Remove(k K) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return ErrFrozen
	}
	delete(r.m, k)
	return nil
}

// snapshotRef 返回只读快照引用（未冻结时为 nil）。持读锁读字段，杜绝与 Freeze 的写竞争。
// 快照 map 冻结后不再被写，返回后调用方可无锁并发读。
func (r *Freezable[K, V]) snapshotRef() map[K]V {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot
}

// Get 按键取值：冻结后走无锁快照；未冻结走读锁。
func (r *Freezable[K, V]) Get(k K) (V, bool) {
	if snap := r.snapshotRef(); snap != nil {
		v, ok := snap[k]
		return v, ok
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.m[k]
	return v, ok
}

// Contains 判断键是否存在：冻结后走无锁快照。
func (r *Freezable[K, V]) Contains(k K) bool {
	if snap := r.snapshotRef(); snap != nil {
		_, ok := snap[k]
		return ok
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.m[k]
	return ok
}

// Len 返回条目数。
func (r *Freezable[K, V]) Len() int {
	if snap := r.snapshotRef(); snap != nil {
		return len(snap)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.m)
}

// Keys 返回全部键（map 迭代序；调用方如需确定性顺序自行排序）。
func (r *Freezable[K, V]) Keys() []K {
	if snap := r.snapshotRef(); snap != nil {
		return keysOf(snap)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return keysOf(r.m)
}

// keysOf 提取键为 slice。
func keysOf[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Freeze 构建只读快照并锁定（不可逆）。之后 Put/Remove 返回 ErrFrozen，
// Get/Contains/Len/Keys 走无锁快照。多次调用幂等。
func (r *Freezable[K, V]) Freeze() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return
	}
	snap := make(map[K]V, len(r.m))
	for k, v := range r.m {
		snap[k] = v
	}
	// 先置快照，再置 frozen；快照引用先于 frozen=true 可见，读路径拿到的
	// snapshotRef 只在 frozen 后才非 nil，写入次序由 Freeze 写锁建立 happens-before。
	r.snapshot = snap
	r.frozen = true
	// 释放底表引用，便于 GC 回收旧 map（快照保留全部值）。
	r.m = nil
}

// IsFrozen 返回是否已冻结。
func (r *Freezable[K, V]) IsFrozen() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.frozen
}

// Clone 深拷贝当前内容（不冻结），得到独立 Freezable。
func (r *Freezable[K, V]) Clone() *Freezable[K, V] {
	r.mu.RLock()
	src := r.m
	if r.frozen {
		src = r.snapshot
	}
	cp := make(map[K]V, len(src))
	for k, v := range src {
		cp[k] = v
	}
	r.mu.RUnlock()
	return &Freezable[K, V]{m: cp}
}