// 本文件补充官方 scope 的父作用域链（bindScopeParent / scopeParentOf / scopeChainOf）。
//
// 作用域除了"层栈叠加"，还可以显式声明自己的外层（父）作用域，从而在解析时沿
// 祖先链向上回溯。父关系是一张有向无环图：任何绑定都会做环检测，沿父链若能走回
// 起点，说明会成环，立即拒绝——因为所有链消费者都要沿父走到根。
package scope

import (
	"errors"
	"sync"
)

// ErrAlreadyBound 表示某 key 已绑定父，重复绑定被拒。
var ErrAlreadyBound = errors.New("scope: key already bound to a parent")

// ErrCycle 表示该父绑定会形成环。
var ErrCycle = errors.New("scope: parent link would form a cycle")

// ParentTree 是作用域父关系注册表，并发安全。
type ParentTree struct {
	mu      sync.RWMutex
	parents map[Key]Key
}

// NewParentTree 创建空父关系表。
func NewParentTree() *ParentTree {
	return &ParentTree{parents: map[Key]Key{}}
}

// Bind 把 parent 绑定为 key 的父作用域（一次性）。
//   - key 已有父 → ErrAlreadyBound；
//   - 沿 parent 向上若遇到 key（会成环）→ ErrCycle。
func (t *ParentTree) Bind(key, parent Key) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.parents[key]; exists {
		return ErrAlreadyBound
	}
	// 环检测：从 parent 沿父链走，遇到 key 即成环。
	for cursor := parent; ; {
		if cursor == key {
			return ErrCycle
		}
		next, ok := t.parents[cursor]
		if !ok {
			break
		}
		cursor = next
	}
	t.parents[key] = parent
	return nil
}

// Rebind 重新链接 key 的父（同样做环检测），不要求 key 已绑定。
func (t *ParentTree) Rebind(key, parent Key) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for cursor := parent; ; {
		if cursor == key {
			return ErrCycle
		}
		next, ok := t.parents[cursor]
		if !ok {
			break
		}
		cursor = next
	}
	t.parents[key] = parent
	return nil
}

// ParentOf 返回 key 的父；根作用域返回 false。
func (t *ParentTree) ParentOf(key Key) (Key, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.parents[key]
	return p, ok
}

// ChainOf 返回 key → 根祖先的链（nearest-first：[key, parent, grandparent, …]）。
func (t *ParentTree) ChainOf(key Key) []Key {
	t.mu.RLock()
	defer t.mu.RUnlock()
	chain := []Key{}
	visited := map[Key]struct{}{} // 防御性：即便数据异常也不死循环
	for cursor := key; ; {
		if _, seen := visited[cursor]; seen {
			break
		}
		visited[cursor] = struct{}{}
		chain = append(chain, cursor)
		next, ok := t.parents[cursor]
		if !ok {
			break
		}
		cursor = next
	}
	return chain
}
