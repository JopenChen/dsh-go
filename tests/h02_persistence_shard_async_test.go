// 本文件对应任务 H02：Persistence 锁分片 + 异步批量写入（JSONLBackend）。
//
// 验证目标：
//   1. 分片隔离：不同 SessionID 可落入不同 shard，ShardCount == 分片数组长度；
//   2. 异步批量落盘：Append 返回后，等待时间窗口或 Close 后数据出现在文件中；
//   3. 并发写入无丢失：多 goroutine 并发向同一会话/多会话 Append，Close 后 Load 数据完整；
//   4. 优雅 Close：Close 后 writer goroutine 退出 + 残留缓冲落盘不丢；
//   5. 显式 Flush：单会话显式 Flush 立即生效，无需等待窗口。
package tests

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/persistence"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// fixedTestTimeH02 返回固定时间戳（本地 helper，避免跨文件同名函数冲突）。
func fixedTestTimeH02() time.Time { return time.Unix(1700000000, 0).UTC() }

// newJSONLNoCheckH02 简化构造：用于只读 probe 的 backend（大 batch、默认窗口）。
func newJSONLNoCheckH02(t *testing.T, dir string) *persistence.JSONLBackend {
	t.Helper()
	b, err := persistence.NewJSONL(dir, 1024)
	if err != nil {
		t.Fatalf("NewJSONL 失败: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// TestH02ShardCountDefault 验证默认分片数 = DefaultShardCount（16），且为 2 幂。
func TestH02ShardCountDefault(t *testing.T) {
	dir := t.TempDir()
	b, err := persistence.NewJSONL(dir, 100)
	if err != nil {
		t.Fatalf("NewJSONL 失败: %v", err)
	}
	defer b.Close()

	if got := b.ShardCount(); got != persistence.DefaultShardCount {
		t.Fatalf("默认 ShardCount = %d, want %d", got, persistence.DefaultShardCount)
	}
	// 必须是 2 幂（否则位运算取模不对）。
	n := b.ShardCount()
	if n&(n-1) != 0 {
		t.Fatalf("ShardCount %d 不是 2 的幂", n)
	}
}

// TestH02WithShardCountRoundsUp 验证 WithShardCount(非 2 幂) 向上取整到最近 2 幂。
func TestH02WithShardCountRoundsUp(t *testing.T) {
	dir := t.TempDir()
	b, err := persistence.NewJSONL(dir, 100, persistence.WithShardCount(7))
	if err != nil {
		t.Fatalf("NewJSONL 失败: %v", err)
	}
	defer b.Close()
	if got := b.ShardCount(); got != 8 {
		t.Fatalf("WithShardCount(7) 应向上取整到 8, got %d", got)
	}
}

// TestH02AppendAsyncFlushByInterval 验证 Append 后未达 batchSize 也会因时间窗口落盘。
func TestH02AppendAsyncFlushByInterval(t *testing.T) {
	dir := t.TempDir()
	// 窗口 20ms，batch 10000：确保只由窗口触发 flush。
	interval := 20 * time.Millisecond
	b, err := persistence.NewJSONL(dir, 10000, persistence.WithFlushInterval(interval))
	if err != nil {
		t.Fatalf("NewJSONL 失败: %v", err)
	}
	defer b.Close()

	ctx := context.Background()
	id := brand.NewSessionID("h02_async_int")
	hdr := session.NewSessionHeader(id, "/ws")
	_ = b.SaveHeader(ctx, hdr)

	// 写入 2 条（远小于 batchSize 10000）。
	for i := uint64(1); i <= 2; i++ {
		_ = b.Append(ctx, id, session.SessionEvent{
			Seq:  i, Time: fixedTestTimeH02(),
			Type: session.EventUserMessage,
			Data: session.UserMessageData{Content: "msg"},
		})
	}

	// 立刻 Load：数据应尚未落盘（时间窗口未到）。
	probe := newJSONLNoCheckH02(t, dir)
	_, before, _ := probe.Load(ctx, id)
	if len(before) != 0 {
		t.Fatalf("Append 后立刻 Load 应看不到未落盘事件, got %d", len(before))
	}

	// 等待 >= 3 个窗口 + 余量，保证至少触发 1 次后台 flush。
	time.Sleep(interval*3 + 10*time.Millisecond)

	_, after, err := probe.Load(ctx, id)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("窗口 flush 后应有 2 条事件, got %d", len(after))
	}
}

// TestH02CloseFlushesEverything 验证 Close 把残留缓冲刷盘、writer goroutine 干净退出。
func TestH02CloseFlushesEverything(t *testing.T) {
	dir := t.TempDir()
	// 大 batch + 长窗口：保证 Append 不会自动 flush，只能靠 Close 触发。
	b, err := persistence.NewJSONL(dir, 100000,
		persistence.WithFlushInterval(5*time.Second),
		persistence.WithShardCount(4))
	if err != nil {
		t.Fatalf("NewJSONL 失败: %v", err)
	}

	ctx := context.Background()
	const N = 50
	ids := make([]brand.SessionID, N)
	for i := 0; i < N; i++ {
		id := brand.NewSessionID(fmt.Sprintf("h02_close_%d", i))
		ids[i] = id
		_ = b.SaveHeader(ctx, session.NewSessionHeader(id, "/ws"))
		_ = b.Append(ctx, id, session.SessionEvent{
			Seq: 1, Time: fixedTestTimeH02(),
			Type: session.EventTurnStart, Data: session.TurnStartData{},
		})
		_ = b.Append(ctx, id, session.SessionEvent{
			Seq: 2, Time: fixedTestTimeH02(),
			Type: session.EventTurnEnd,
			Data: session.TurnEndData{Reason: session.ReasonFinished},
		})
	}

	// 立即关闭：应把 50 会话 × 2 条 = 100 条事件 flush 完成。
	if err := b.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// 用新 backend 加载全部会话验证。
	probe, _ := persistence.NewJSONL(dir, 1)
	defer probe.Close()
	for i := 0; i < N; i++ {
		_, evs, err := probe.Load(ctx, ids[i])
		if err != nil {
			t.Fatalf("会话 %d Load 失败: %v", i, err)
		}
		if len(evs) != 2 {
			t.Fatalf("会话 %d 事件数 = %d, want 2", i, len(evs))
		}
	}
}

// TestH02ConcurrentAppendNoLoss 验证多 goroutine 对同一会话并发 Append：
// Close 后 Load 的事件数量 == 写入数量，无丢失。
func TestH02ConcurrentAppendNoLoss(t *testing.T) {
	dir := t.TempDir()
	b, err := persistence.NewJSONL(dir, 32,
		persistence.WithFlushInterval(30*time.Millisecond),
		persistence.WithShardCount(8))
	if err != nil {
		t.Fatalf("NewJSONL 失败: %v", err)
	}
	defer b.Close()

	ctx := context.Background()
	id := brand.NewSessionID("h02_concurrent")
	_ = b.SaveHeader(ctx, session.NewSessionHeader(id, "/ws"))

	const goroutines = 16
	const perRoutine = 200
	total := goroutines * perRoutine

	var seq int64 // 原子递增 seq
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for k := 0; k < perRoutine; k++ {
				s := atomic.AddInt64(&seq, 1)
				_ = b.Append(ctx, id, session.SessionEvent{
					Seq:  uint64(s),
					Time: fixedTestTimeH02(),
					Type: session.EventAssistantMessage,
					Data: session.AssistantMessageData{Content: "x"},
				})
			}
		}()
	}
	wg.Wait()

	// 同步 Flush 确保落盘（不依赖窗口）。
	if err := b.Flush(ctx, id); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
	_, evs, err := b.Load(ctx, id)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if len(evs) != total {
		t.Fatalf("并发写入后事件数 = %d, want %d（丢失 %d）",
			len(evs), total, total-len(evs))
	}
}

// TestH02ExplicitFlushImmediate 验证显式 Flush(id) 无需等待窗口即可落盘。
func TestH02ExplicitFlushImmediate(t *testing.T) {
	dir := t.TempDir()
	// 超长窗口（60s）：只有显式 Flush 能落盘。
	b, err := persistence.NewJSONL(dir, 10000,
		persistence.WithFlushInterval(60*time.Second))
	if err != nil {
		t.Fatalf("NewJSONL 失败: %v", err)
	}
	defer b.Close()

	ctx := context.Background()
	id := brand.NewSessionID("h02_exp_flush")
	_ = b.SaveHeader(ctx, session.NewSessionHeader(id, "/ws"))

	_ = b.Append(ctx, id, session.SessionEvent{
		Seq: 1, Time: fixedTestTimeH02(),
		Type: session.EventTurnStart, Data: session.TurnStartData{},
	})
	_ = b.Append(ctx, id, session.SessionEvent{
		Seq: 2, Time: fixedTestTimeH02(),
		Type: session.EventTurnEnd,
		Data: session.TurnEndData{Reason: session.ReasonFinished},
	})

	if err := b.Flush(ctx, id); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
	probe := newJSONLNoCheckH02(t, dir)
	_, evs, err := probe.Load(ctx, id)
	if err != nil {
		t.Fatalf("probe Load 失败: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("显式 Flush 后应有 2 条, got %d", len(evs))
	}
}

// TestH02BatchThresholdTrigger 验证会话缓冲达到 batchSize 立即触发 flush（信号通路）。
func TestH02BatchThresholdTrigger(t *testing.T) {
	dir := t.TempDir()
	// 超长窗口：只有 batchSize 阈值信号才能触发 flush。
	b, err := persistence.NewJSONL(dir, 5,
		persistence.WithFlushInterval(60*time.Second))
	if err != nil {
		t.Fatalf("NewJSONL 失败: %v", err)
	}
	defer b.Close()

	ctx := context.Background()
	id := brand.NewSessionID("h02_batch_trig")
	_ = b.SaveHeader(ctx, session.NewSessionHeader(id, "/ws"))

	// 写入 5 条（== batchSize）——应触发即时信号 flush。
	for i := uint64(1); i <= 5; i++ {
		_ = b.Append(ctx, id, session.SessionEvent{
			Seq:  i, Time: fixedTestTimeH02(),
			Type: session.EventAssistantChunk,
			Data: session.AssistantChunkData{Text: "x"},
		})
	}
	// 给信号通路一点处理时间（毫秒级）。
	time.Sleep(80 * time.Millisecond)

	probe := newJSONLNoCheckH02(t, dir)
	_, evs, err := probe.Load(ctx, id)
	if err != nil {
		t.Fatalf("probe Load 失败: %v", err)
	}
	if len(evs) != 5 {
		t.Fatalf("达到 batchSize 后信号 flush 应有 5 条事件, got %d", len(evs))
	}
}
