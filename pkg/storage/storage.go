// Package storage 提供 Storage Domain KV 抽象（hub → backend → domain 三层）。
//
// 对齐上游：packages/core/storage-domain
//
// 分层设计：
//   - Backend：底层键值存储抽象（Get/Put/Delete/List + 版本号），支持
//     filekv（JSONL 文件）、sqlitekv（SQLite 表）、memorykv（测试用）多实现；
//   - Domain[T]：面向业务域的类型化读写层，通过编码序列化 T 落到 backend，
//     并对外暴露 CAS（Compare-And-Swap）语义：写时必须携带期望版本号，
//     版本冲突返回统一的 ErrCASMismatch，跨后端错误行为一致；
//   - 供 MessageFeedback / GoalRevisionViews / SessionSidecars / SkillsCatalog /
//     WorkspaceRegistry 等域复用同一套 CAS + 类型化读写。
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ============================================================================
// CAS 版本冲突错误
// ============================================================================

// ErrCASMismatch 表示乐观锁（CAS）版本冲突：写入时携带的期望版本与实际版本不符。
type ErrCASMismatch struct {
	Key             string
	ExpectedVersion uint64
	ActualVersion   uint64
}

// Error 实现 error 接口。
func (e *ErrCASMismatch) Error() string {
	return fmt.Sprintf("storage: CAS version mismatch for key %q: expected %d, actual %d",
		e.Key, e.ExpectedVersion, e.ActualVersion)
}

// ============================================================================
// Backend 抽象
// ============================================================================

// Backend 是底层键值存储后端接口。
// 约定：
//   - 版本号从 0 开始；首次写入（expectedVersion=0）创建记录，成功后版本变为 1；
//   - Put 携带的 expectedVersion 必须等于当前版本，否则返回 *ErrCASMismatch。
type Backend interface {
	// Get 读取 key 的数据与当前版本；key 不存在返回 ErrKeyNotFound。
	Get(ctx context.Context, key string) (data []byte, version uint64, err error)
	// Put 按 CAS 语义写入 key；expectedVersion=0 表示期望新建。
	Put(ctx context.Context, key string, data []byte, expectedVersion uint64) (version uint64, err error)
	// Delete 删除 key（不存在不报错）。
	Delete(ctx context.Context, key string) error
	// List 返回指定前缀下的所有 key（不含 prefix 前缀）。
	List(ctx context.Context, prefix string) (keys []string, err error)
}

// ErrKeyNotFound 表示 key 不存在。
type ErrKeyNotFound struct {
	Key string
}

// Error 实现 error 接口。
func (e *ErrKeyNotFound) Error() string {
	return fmt.Sprintf("storage: key not found: %q", e.Key)
}

// IsKeyNotFound 判断是否为 key 不存在错误。
func IsKeyNotFound(err error) bool {
	_, ok := err.(*ErrKeyNotFound)
	return ok
}

// IsCASMismatch 判断是否为 CAS 版本冲突错误。
func IsCASMismatch(err error) bool {
	_, ok := err.(*ErrCASMismatch)
	return ok
}

// ============================================================================
// Domain[T] 类型化读写层
// ============================================================================

// Domain 是一个业务域的类型化存储视图（key 带统一前缀，避免跨域碰撞）。
type Domain[T any] struct {
	backend Backend
	prefix  string
}

// NewDomain 创建类型化域存储，prefix 用于隔离不同业务域（如 "feedback/"）。
func NewDomain[T any](backend Backend, prefix string) *Domain[T] {
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return &Domain[T]{backend: backend, prefix: prefix}
}

// fullKey 拼接域前缀与业务 key。
func (d *Domain[T]) fullKey(key string) string {
	return d.prefix + key
}

// Get 读取并反序列化域值，返回其当前版本号。
func (d *Domain[T]) Get(ctx context.Context, key string) (T, uint64, error) {
	var zero T
	raw, version, err := d.backend.Get(ctx, d.fullKey(key))
	if err != nil {
		return zero, 0, err
	}
	if err := json.Unmarshal(raw, &zero); err != nil {
		return zero, version, fmt.Errorf("storage: unmarshal domain %q: %w", key, err)
	}
	return zero, version, nil
}

// Put 序列化并 CAS 写入域值。expectedVersion 来自 Get 的返回版本；0 表示新建。
func (d *Domain[T]) Put(ctx context.Context, key string, value T, expectedVersion uint64) (uint64, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, fmt.Errorf("storage: marshal domain %q: %w", key, err)
	}
	return d.backend.Put(ctx, d.fullKey(key), raw, expectedVersion)
}

// Delete 删除域值。
func (d *Domain[T]) Delete(ctx context.Context, key string) error {
	return d.backend.Delete(ctx, d.fullKey(key))
}

// List 列出该域下所有业务 key。
func (d *Domain[T]) List(ctx context.Context) ([]string, error) {
	keys, err := d.backend.List(ctx, d.prefix)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, strings.TrimPrefix(k, d.prefix))
	}
	return out, nil
}

// ============================================================================
// 跨后端迁移
// ============================================================================

// Migrate 将 src 后端全部数据拷贝到 dst 后端（跨后端迁移）。
// 仅迁移 src 中存在的 key；dest 已有更高版本时跳过冲突 key 并记录。
func Migrate(ctx context.Context, src, dst Backend) (migrated int, skipped []string, err error) {
	keys, err := src.List(ctx, "")
	if err != nil {
		return 0, nil, fmt.Errorf("storage: list src: %w", err)
	}
	for _, key := range keys {
		data, version, err := src.Get(ctx, key)
		if err != nil {
			if IsKeyNotFound(err) {
				continue
			}
			return migrated, skipped, fmt.Errorf("storage: get %q from src: %w", key, err)
		}
		// 目标端若已有不低版本则跳过（幂等迁移：dstVersion >= srcVersion 视为已同步）
		_, dstVersion, dstErr := dst.Get(ctx, key)
		if dstErr == nil && dstVersion >= version {
			skipped = append(skipped, key)
			continue
		}
		// 目标端已有同版本或更低版本：用源版本号+1 覆盖写入
		if _, err := dst.Put(ctx, key, data, dstVersion); err != nil {
			return migrated, skipped, fmt.Errorf("storage: put %q to dst: %w", key, err)
		}
		migrated++
	}
	return migrated, skipped, nil
}
