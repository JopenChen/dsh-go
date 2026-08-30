// 本文件实现 memorykv 后端：内存键值存储（测试与临时场景用）。
package storage

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// memRecord 是内存中的单条记录。
type memRecord struct {
	data    []byte
	version uint64
}

// MemoryKV 是基于内存 map 的 Backend 实现。
type MemoryKV struct {
	mu sync.RWMutex
	m  map[string]*memRecord
}

// NewMemoryKV 创建空内存后端。
func NewMemoryKV() *MemoryKV {
	return &MemoryKV{m: map[string]*memRecord{}}
}

// Get 实现 Backend。
func (m *MemoryKV) Get(ctx context.Context, key string) ([]byte, uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.m[key]
	if !ok {
		return nil, 0, &ErrKeyNotFound{Key: key}
	}
	return append([]byte(nil), rec.data...), rec.version, nil
}

// Put 实现 Backend（CAS 语义）。
func (m *MemoryKV) Put(ctx context.Context, key string, data []byte, expectedVersion uint64) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.m[key]
	if !ok {
		if expectedVersion != 0 {
			return 0, &ErrCASMismatch{Key: key, ExpectedVersion: expectedVersion, ActualVersion: 0}
		}
		m.m[key] = &memRecord{data: append([]byte(nil), data...), version: 1}
		return 1, nil
	}
	if rec.version != expectedVersion {
		return 0, &ErrCASMismatch{Key: key, ExpectedVersion: expectedVersion, ActualVersion: rec.version}
	}
	rec.data = append([]byte(nil), data...)
	rec.version++
	return rec.version, nil
}

// Delete 实现 Backend。
func (m *MemoryKV) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.m, key)
	return nil
}

// List 实现 Backend（字典序输出保证确定性）。
func (m *MemoryKV) List(ctx context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for k := range m.m {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}
