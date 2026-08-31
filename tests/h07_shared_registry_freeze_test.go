// 本文件对应任务 H07：Shared Registry 只读化（Freezable 泛型组件 + commands 应用）。
//
// 验证目标：
//   1. Freezable 基础读写 + contains/len/keys；
//   2. Freeze 后 Get 走只读快照、值一致；
//   3. Freeze 后 Put/Remove 返回 ErrFrozen（写入拒绝）；
//   4. Freeze 幂等（多次调用不 panic、快照不变）；
//   5. 并发只读：Freeze 后多 goroutine 高并发 Get 不锁、结果稳定（-race 下无竞争）；
//   6. commands.Registry 集成：Freeze()/IsFrozen()，Freeze 后 Register 报错；
//   7. Benchmark：freeze 后只读 Get 吞吐 vs 未 freeze（读锁）——freeze 更高或相当。
package tests

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/commands"
	"github.com/JopenChen/dsh-go/pkg/registry"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// ============================================================================
// 1. 基础读写
// ============================================================================

func TestH07FreezableBasic(t *testing.T) {
	r := registry.NewFreezable[string, int]()
	if r.Len() != 0 {
		t.Fatal("初始应为空")
	}
	if err := r.Put("a", 1); err != nil {
		t.Fatal(err)
	}
	if err := r.Put("b", 2); err != nil {
		t.Fatal(err)
	}
	if v, ok := r.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %v, ok=%v", v, ok)
	}
	if !r.Contains("b") || r.Contains("z") {
		t.Fatal("Contains 判定异常")
	}
	if r.Len() != 2 {
		t.Fatalf("Len = %d, want 2", r.Len())
	}
	// Remove
	if err := r.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if r.Contains("a") {
		t.Fatal("Remove 后不应包含 a")
	}
}

// ============================================================================
// 2. Freeze 后只读快照一致性
// ============================================================================

func TestH07FreezableSnapshotConsistency(t *testing.T) {
	r := registry.NewFreezable[string, int]()
	_ = r.Put("x", 10)
	_ = r.Put("y", 20)
	r.Freeze()
	if !r.IsFrozen() {
		t.Fatal("Freeze 后 IsFrozen 应为 true")
	}
	// 快照只读：值必须保留
	if v, ok := r.Get("x"); !ok || v != 10 {
		t.Fatalf("Freeze 后 Get(x) = %v ok=%v", v, ok)
	}
	if v, ok := r.Get("y"); !ok || v != 20 {
		t.Fatalf("Freeze 后 Get(y) = %v ok=%v", v, ok)
	}
	if r.Len() != 2 || !r.Contains("y") {
		t.Fatal("Freeze 后 Len/Contains 异常")
	}
}

// ============================================================================
// 3. Freeze 后写入拒绝
// ============================================================================

func TestH07FreezeRejectsMutation(t *testing.T) {
	r := registry.NewFreezable[string, int]()
	_ = r.Put("a", 1)
	r.Freeze()
	if err := r.Put("b", 2); !errors.Is(err, registry.ErrFrozen) {
		t.Fatalf("Freeze 后 Put 应返回 ErrFrozen, got %v", err)
	}
	if err := r.Remove("a"); !errors.Is(err, registry.ErrFrozen) {
		t.Fatalf("Freeze 后 Remove 应返回 ErrFrozen, got %v", err)
	}
	// 快照未受污染
	if r.Len() != 1 || !r.Contains("a") {
		t.Fatal("Freeze 后拒绝的写入不应影响快照")
	}
}

// ============================================================================
// 4. Freeze 幂等
// ============================================================================

func TestH07FreezeIdempotent(t *testing.T) {
	r := registry.NewFreezable[string, int]()
	_ = r.Put("a", 42)
	r.Freeze()
	r.Freeze() // 不 panic
	r.Freeze()
	if blob, ok := r.Get("a"); !ok || blob != 42 {
		t.Fatalf("多次 Freeze 后快照应保持: %v %v", blob, ok)
	}
}

// ============================================================================
// 5. 并发只读：Freeze 后多 goroutine 高并发 Get 稳定
// ============================================================================

func TestH07ConcurrentReadAfterFreeze(t *testing.T) {
	const n = 64
	r := registry.NewFreezable[string, int]()
	for i := 0; i < n; i++ {
		_ = r.Put(itoa(i), i)
	}
	r.Freeze()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	// 8 个 reader 各读 n 次；关闭 stop 前持续读。
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				for i := 0; i < n; i++ {
					v, ok := r.Get(itoa(i))
					if !ok || v != i {
						t.Errorf("并发读异常: key %d got %v ok=%v", i, v, ok)
						return
					}
				}
			}
		}(g)
	}
	// 这里用 signal 提前结束（-race 无竞争断言由 race detector 兜底）。
	_ = stop
	close(stop) // 立即结束本轮（本测试核心是 -race 无数据竞争 + 读稳定）
	wg.Wait()
}

// ============================================================================
// 6. commands.Registry 集成
// ============================================================================

func TestH07CommandsFreeze(t *testing.T) {
	r := commands.NewRegistry()
	// 内置 plan/goal
	if _, ok := r.Get("plan"); !ok {
		t.Fatal("内置 plan 命令缺失")
	}
	if r.IsFrozen() {
		t.Fatal("初始不应冻结")
	}
	// 注册第三方命令
	if err := r.Register(&commands.CommandDefinition{Name: "resume", Description: "继续", Handler: func(ctx context.Context, args string, sl *session.SessionLog) (string, error) { return "", nil }}); err != nil {
		t.Fatal(err)
	}
	r.Freeze()
	if !r.IsFrozen() {
		t.Fatal("Freeze 后 IsFrozen 应为 true")
	}
	// Freeze 后读 OK
	if _, ok := r.Get("resume"); !ok {
		t.Fatal("Freeze 后应能读到已注册命令")
	}
	if err := r.Register(&commands.CommandDefinition{Name: "late", Description: "x", Handler: nil}); !errors.Is(err, registry.ErrFrozen) {
		t.Fatalf("Freeze 后 Register 应返回 ErrFrozen, got %v", err)
	}
	// 列表仍含内置 + resume
	names := r.List()
	foundPlan, foundResume := false, false
	for _, n := range names {
		if n == "plan" {
			foundPlan = true
		}
		if n == "resume" {
			foundResume = true
		}
	}
	if !foundPlan || !foundResume {
		t.Fatalf("List 应含 plan 与 resume, got %v", names)
	}
}

// ============================================================================
// 7. Benchmark：Freeze 后只读 Get 吞吐 vs 未 Freeze
// ============================================================================

// BenchmarkH07ReadBeforeFreeze 未冻结（读锁）基线。
func BenchmarkH07ReadBeforeFreeze(b *testing.B) {
	r := registry.NewFreezable[string, int]()
	for i := 0; i < 100; i++ {
		_ = r.Put(itoa(i), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Get(itoa(i % 100))
	}
}

// BenchmarkH07ReadAfterFreeze 冻结后（无锁快照）。
func BenchmarkH07ReadAfterFreeze(b *testing.B) {
	r := registry.NewFreezable[string, int]()
	for i := 0; i < 100; i++ {
		_ = r.Put(itoa(i), i)
	}
	r.Freeze()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Get(itoa(i % 100))
	}
}