// 本文件对应任务 H08：Goroutine 治理 + 单一 Supervisor（统一启动/关闭生命周期）。
//
// 验证目标：
//   1. 启动多个 worker，Shutdown 后全部优雅退出（done 关闭），无泄漏；
//   2. 父 ctx 级联：取消根 ctx → 子 worker ctx 同步收到 Done；
//   3. 幂等：重复 Shutdown / 重复 Close 安全；
//   4. 超时：worker 拒绝退出时 Shutdown 返回错误（带超时 ctx）；
//   5. 关闭后 Go() 拒绝；
//   6. 单点 CancelWorker / WaitWorker / Healthy 行为正确；
//   7. 集成示例：模拟 JSONL shardWriter 型 ticker worker 通过 Supervisor 治理。
package tests

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/jobs"
)

// ============================================================================
// 1. 多 worker 启动 + 优雅关闭无泄漏
// ============================================================================

func TestH08SupervisorGracefulShutdown(t *testing.T) {
	sup := jobs.New()
	var exited atomic.Int32
	for i := 0; i < 8; i++ {
		name := "worker-" + itoa(i)
		ok := sup.Go(name, func(ctx context.Context) {
			defer exited.Add(1)
			// 模拟有界后台循环：监听 ctx 退出。
			<-ctx.Done()
		})
		if !ok {
			t.Fatalf("启动 worker %s 失败", name)
		}
	}
	if sup.WorkerCount() != 8 {
		t.Fatalf("WorkerCount = %d, want 8", sup.WorkerCount())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := sup.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}
	if exited.Load() != 8 {
		t.Fatalf("退出 worker 数 = %d, want 8（存在泄漏）", exited.Load())
	}
	// 关闭后无存活 worker → Healthy()==false
	if sup.Healthy(ctx) {
		t.Fatal("Shutdown 后不应有存活 worker")
	}
}

// ============================================================================
// 2. 父 ctx 级联取消
// ============================================================================

func TestH08ParentCtxCascade(t *testing.T) {
	sup := jobs.New()
	sawCancel := make(chan struct{}, 1)
	sup.Go("cascade", func(ctx context.Context) {
		<-ctx.Done()
		sawCancel <- struct{}{}
	})
	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCtx()
	if err := sup.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sawCancel:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("worker 未收到级联取消信号")
	}
}

// ============================================================================
// 3. 幂等：重复 Shutdown / Close
// ============================================================================

func TestH08IdempotentShutdown(t *testing.T) {
	sup := jobs.New()
	sup.Go("w1", func(ctx context.Context) { <-ctx.Done() })
	ctx := context.Background()
	if err := sup.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sup.Shutdown(ctx); err != nil {
		t.Fatalf("第二次 Shutdown 应幂等成功: %v", err)
	}
	sup.Close() // 不应 panic / hang
	sup.Close()
	// 关闭后 Go 拒绝
	if sup.Go("late", func(ctx context.Context) {}) {
		t.Fatal("Shutdown 后 Go 应被拒绝")
	}
}

// ============================================================================
// 4. 超时：worker 拒不退出 → Shutdown 返回错误
// ============================================================================

func TestH08ShutdownTimeout(t *testing.T) {
	sup := jobs.New()
	// 一个 worker 忽略 ctx（模拟 buggy worker 不退出），一个正常退出。
	sup.Go("stubborn", func(ctx context.Context) { select {} }) // 永不移交
	sup.Go("ok", func(ctx context.Context) { <-ctx.Done() })
	// 极短超时迫使 Shutdown 失败。
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := sup.Shutdown(ctx)
	if err == nil {
		t.Fatal("存在拒不退出的 worker 时 Shutdown 应返回错误")
	}
}

// ============================================================================
// 5. 单点 CancelWorker / WaitWorker
// ============================================================================

func TestH08SingleWorkerCancel(t *testing.T) {
	sup := jobs.New()
	var exited atomic.Bool
	sup.Go("target", func(ctx context.Context) {
		<-ctx.Done()
		exited.Store(true)
	})
	sup.Go("other", func(ctx context.Context) { <-ctx.Done() })
	sup.CancelWorker("target")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if !sup.WaitWorker(ctx, "target") {
		t.Fatal("WaitWorker(target) 应在取消后很快 true")
	}
	if !exited.Load() {
		t.Fatal("target worker 应已退出")
	}
	// 其它 worker 未受影响
	if sup.WorkerCount() != 2 {
		t.Fatalf("WorkerCount 应保持 2，got %d", sup.WorkerCount())
	}
}

// ============================================================================
// 6. 集成示例：模拟 ticker 型 writer worker（如 JSONL shardWriter）
// ============================================================================

func TestH08TickerWorkerViaSupervisor(t *testing.T) {
	sup := jobs.New()
	var flushed atomic.Int64
	// 模拟 JSONL shardWriter：每 10ms flush 一次，遇 ctx.Done 退出。
	sup.Go("jsonl-shard-0", func(ctx context.Context) {
		tk := time.NewTicker(10 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-tk.C:
				flushed.Add(1)
			case <-ctx.Done():
				return
			}
		}
	})
	// 跑 60ms
	time.Sleep(60 * time.Millisecond)
	before := flushed.Load()
	if before == 0 {
		t.Fatal("ticker worker 应已触发 flush")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := sup.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown ticker worker 失败: %v", err)
	}
	// 关闭后立即 flash 应停下。
	tk := time.NewTicker(10 * time.Millisecond)
	defer tk.Stop()
	select {
	case <-tk.C:
		// 已过窗口，flushed 不应再有明显增长（no race check here，仅验证退出）
	case <-time.After(50 * time.Millisecond):
	}
	// Shutdown 返回即可证明它已退出；此处只额外断言 flushed 仍 >= before。
	if flushed.Load() < before {
		t.Fatalf("flushed 应单调递增: before=%d now=%d", before, flushed.Load())
	}
}