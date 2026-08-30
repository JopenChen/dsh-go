// 本文件对应任务 M45：Storage Domain KV 抽象（CAS 一致性 + 跨后端迁移）。
package tests

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/storage"
)

// makeBackends 构造 filekv 与 sqlitekv 两个真实后端 + memorykv，并返回清理函数（关闭 SQLite 连接）。
func makeBackends(t *testing.T) (fileKV, sqliteKV, memKV storage.Backend, cleanup func()) {
	t.Helper()
	dir := t.TempDir()

	fk, err := storage.NewFileKV(filepath.Join(dir, "filekv"))
	if err != nil {
		t.Fatalf("NewFileKV 失败: %v", err)
	}
	sk, err := storage.NewSQLiteKV(filepath.Join(dir, "sqlite.db"))
	if err != nil {
		t.Fatalf("NewSQLiteKV 失败: %v", err)
	}
	return fk, sk, storage.NewMemoryKV(), func() {
		_ = sk.Close()
	}
}

// TestStorageCASMismatchConsistent 验证两个后端 CAS 版本冲突错误行为一致。
func TestStorageCASMismatchConsistent(t *testing.T) {
	fileKV, sqliteKV, memKV, cleanup := makeBackends(t)
	defer cleanup()
	ctx := context.Background()

	for name, backend := range map[string]storage.Backend{
		"filekv":   fileKV,
		"sqlitekv": sqliteKV,
		"memorykv": memKV,
	} {
		t.Run(name, func(t *testing.T) {
			// 新建
			v1, err := backend.Put(ctx, "k1", []byte("a"), 0)
			if err != nil {
				t.Fatalf("新建失败: %v", err)
			}
			if v1 != 1 {
				t.Fatalf("首次版本应为 1: %d", v1)
			}

			// 用错误版本更新 → CAS 冲突
			_, err = backend.Put(ctx, "k1", []byte("b"), 99)
			if !storage.IsCASMismatch(err) {
				t.Fatalf("应返回 ErrCASMismatch, 实际 %v", err)
			}
			var mismatch *storage.ErrCASMismatch
			if !asCASMismatch(err, &mismatch) {
				t.Fatalf("错误类型应为 *ErrCASMismatch: %T", err)
			}
			if mismatch.ExpectedVersion != 99 || mismatch.ActualVersion != 1 {
				t.Fatalf("CAS 字段异常: %+v", mismatch)
			}

			// 用正确版本更新
			v2, err := backend.Put(ctx, "k1", []byte("b"), 1)
			if err != nil {
				t.Fatalf("正确版本更新失败: %v", err)
			}
			if v2 != 2 {
				t.Fatalf("第二次版本应为 2: %d", v2)
			}

			// 用 expectedVersion=0 更新已存在 key → 冲突（新建语义）
			if _, err := backend.Put(ctx, "k1", []byte("c"), 0); !storage.IsCASMismatch(err) {
				t.Fatalf("对已存在 key 用 0 新建应冲突: %v", err)
			}

			// 读取
			data, version, err := backend.Get(ctx, "k1")
			if err != nil {
				t.Fatalf("Get 失败: %v", err)
			}
			if string(data) != "b" || version != 2 {
				t.Fatalf("读取结果异常: %q v%d", data, version)
			}
		})
	}
}

// asCASMismatch 便捷类型断言。
func asCASMismatch(err error, target **storage.ErrCASMismatch) bool {
	e, ok := err.(*storage.ErrCASMismatch)
	if ok {
		*target = e
	}
	return ok
}

// TestStorageTypedDomain 验证 Domain[T] 类型化读写 + CAS。
func TestStorageTypedDomain(t *testing.T) {
	_, sqliteKV, _, cleanup := makeBackends(t)
	defer cleanup()
	ctx := context.Background()

	type feedback struct {
		Rating int    `json:"rating"`
		Note   string `json:"note"`
	}

	domain := storage.NewDomain[feedback](sqliteKV, "feedback")

	// 新建
	if _, err := domain.Put(ctx, "msg_1", feedback{Rating: 5, Note: "good"}, 0); err != nil {
		t.Fatalf("新建失败: %v", err)
	}
	// 读取
	fb, version, err := domain.Get(ctx, "msg_1")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if fb.Rating != 5 || fb.Note != "good" {
		t.Fatalf("读取类型化值异常: %+v", fb)
	}

	// CAS 更新
	if _, err := domain.Put(ctx, "msg_1", feedback{Rating: 4}, version); err != nil {
		t.Fatalf("CAS 更新失败: %v", err)
	}
	// CAS 冲突
	if _, err := domain.Put(ctx, "msg_1", feedback{Rating: 1}, version); !storage.IsCASMismatch(err) {
		t.Fatalf("过期版本更新应冲突: %v", err)
	}

	// List 域前缀隔离
	if _, err := domain.Put(ctx, "msg_2", feedback{Rating: 3}, 0); err != nil {
		t.Fatalf("msg_2 新建失败: %v", err)
	}
	keys, err := domain.List(ctx)
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("域内 key 数 = %d, want 2: %v", len(keys), keys)
	}
}

// TestStorageMigration 验证 filekv → sqlitekv 跨后端迁移脚本可跑且幂等。
func TestStorageMigration(t *testing.T) {
	fileKV, sqliteKV, _, cleanup := makeBackends(t)
	defer cleanup()
	ctx := context.Background()

	// 源端写入 3 个 key（含子前缀 key）
	_, _ = fileKV.Put(ctx, "a", []byte("1"), 0)
	_, _ = fileKV.Put(ctx, "b/x", []byte("2"), 0)
	_, _ = fileKV.Put(ctx, "c", []byte("3"), 0)

	migrated, skipped, err := storage.Migrate(ctx, fileKV, sqliteKV)
	if err != nil {
		t.Fatalf("Migrate 失败: %v", err)
	}
	if migrated != 3 {
		t.Fatalf("迁移数量 = %d, want 3", migrated)
	}
	if len(skipped) != 0 {
		t.Fatalf("首次迁移不应跳过: %v", skipped)
	}

	// 校验目标端数据一致
	for _, key := range []string{"a", "b/x", "c"} {
		data, _, err := sqliteKV.Get(ctx, key)
		if err != nil {
			t.Fatalf("目标端读取 %q 失败: %v", key, err)
		}
		if key == "a" && string(data) != "1" {
			t.Fatalf("迁移后数据不一致: %q", data)
		}
	}

	// 幂等：再次迁移应全部跳过（目标版本不低）
	migrated2, skipped2, err := storage.Migrate(ctx, fileKV, sqliteKV)
	if err != nil {
		t.Fatalf("二次迁移失败: %v", err)
	}
	if migrated2 != 0 || len(skipped2) != 3 {
		t.Fatalf("二次迁移应全部跳过: migrated=%d skipped=%v", migrated2, skipped2)
	}
}

// TestStorageKeyNotFound 验证不存在的 key 返回 ErrKeyNotFound。
func TestStorageKeyNotFound(t *testing.T) {
	_, _, memKV, cleanup := makeBackends(t)
	defer cleanup()
	ctx := context.Background()

	if _, _, err := memKV.Get(ctx, "missing"); !storage.IsKeyNotFound(err) {
		t.Fatalf("应返回 ErrKeyNotFound, 实际 %v", err)
	}
}
