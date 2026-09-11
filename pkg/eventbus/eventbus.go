// Package eventbus 提供类型化的进程内事件总线，对齐 Cordis 的四种同步分发模式。
//
// 上游 Cordis 用同一套 ctx.on/ctx.emit 承载插件间松耦合通信，每种事件有且只有
// 一种分发模式。Go 侧没有 Cordis 运行时，本包用泛型把四种契约固化为独立方法，
// 让"注册—分发—自动清理"在编译期就清晰可辨：
//
//   - Emit   广播：所有监听器顺序执行，返回值被忽略（通知语义）；
//   - Bail   保释：顺序执行，第一个返回非 error 零值/错误的监听器短路并回传；
//   - Serial 串行：顺序执行，收集每个监听器的返回值为切片（聚合语义）；
//   - Waterfall 归 pkg/waterfall（洋葱 next 委托），不在本包重复。
//
// 生命周期：On 返回 dispose 闭包，调用即摘除该监听器——对应 Cordis "插件卸载时
// 自动移除监听"，调用方应在自己的清理路径里保留并调用它。
package eventbus

import (
	"sync"
)

// Listener 是一个接收载荷并返回结果的监听器。
// Emit 场景下返回值被忽略；Bail/Serial 场景下参与短路或聚合。
type Listener[T any, R any] func(payload T) R

// Bus 是单个事件通道：维护一组有序监听器，并发安全。
// 零值不可直接使用，请用 New 构造。
type Bus[T any, R any] struct {
	mu        sync.RWMutex
	listeners []listenerSlot[T, R]
	nextID    uint64
}

// listenerSlot 给每个监听器一个稳定 ID，便于 O(1) 摘除。
type listenerSlot[T any, R any] struct {
	id uint64
	fn Listener[T, R]
}

// New 创建一个空事件总线。
func New[T any, R any]() *Bus[T, R] {
	return &Bus[T, R]{}
}

// On 注册一个监听器，返回 dispose 函数。
// 调用 dispose（幂等）会摘除该监听器；未调用时监听器一直有效。
func (b *Bus[T, R]) On(fn Listener[T, R]) func() {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	b.listeners = append(b.listeners, listenerSlot[T, R]{id: id, fn: fn})
	b.mu.Unlock()

	var removed bool
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if removed {
			return
		}
		removed = true
		for i := range b.listeners {
			if b.listeners[i].id == id {
				b.listeners = append(b.listeners[:i], b.listeners[i+1:]...)
				return
			}
		}
	}
}

// snapshot 返回当前监听器的浅拷贝（持锁时间最短，分发在锁外执行，
// 避免监听器内部再次 On 造成死锁）。
func (b *Bus[T, R]) snapshot() []Listener[T, R] {
	b.mu.RLock()
	out := make([]Listener[T, R], len(b.listeners))
	for i := range b.listeners {
		out[i] = b.listeners[i].fn
	}
	b.mu.RUnlock()
	return out
}

// Len 返回当前监听器数量。
func (b *Bus[T, R]) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.listeners)
}

// Emit 广播：顺序调用所有监听器，忽略返回值。
func (b *Bus[T, R]) Emit(payload T) {
	for _, fn := range b.snapshot() {
		fn(payload)
	}
}

// Bail 保释：顺序调用监听器，第一个 shouldShort(返回值)=true 的结果被回传，
// 后续监听器不再执行。所有监听器都未命中时返回零值与 false。
func (b *Bus[T, R]) Bail(payload T, shouldShort func(R) bool) (R, bool) {
	var zero R
	for _, fn := range b.snapshot() {
		r := fn(payload)
		if shouldShort != nil && shouldShort(r) {
			return r, true
		}
	}
	return zero, false
}

// Serial 串行：顺序调用所有监听器，收集返回值为切片（保持注册顺序）。
func (b *Bus[T, R]) Serial(payload T) []R {
	fns := b.snapshot()
	out := make([]R, 0, len(fns))
	for _, fn := range fns {
		out = append(out, fn(payload))
	}
	return out
}

// --- 常用特化：无返回值的通知总线（Emit-only 语义更清晰）---

// Signal 是不关心返回值的通知总线。
type Signal[T any] struct {
	inner *Bus[T, struct{}]
}

// NewSignal 创建通知总线。
func NewSignal[T any]() *Signal[T] {
	return &Signal[T]{inner: New[T, struct{}]()}
}

// On 注册通知监听器，返回 dispose。
func (s *Signal[T]) On(fn func(T)) func() {
	return s.inner.On(func(payload T) struct{} {
		fn(payload)
		return struct{}{}
	})
}

// Emit 广播通知。
func (s *Signal[T]) Emit(payload T) {
	s.inner.Emit(payload)
}

// Len 返回监听器数量。
func (s *Signal[T]) Len() int { return s.inner.Len() }
