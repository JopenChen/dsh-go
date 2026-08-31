// 本文件验证任务 S05：Session Telemetry hooks。
//
// 覆盖三类钩子（事件追加 / 流式分片 / 工具执行）均触发 + latency 合理 + 单钩子
// panic 不影响其余钩子与调用方。
package tests

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/telemetry"
)

// TestTelemetryThreeHooksFire 验证事件/分片/工具三类钩子都触发且 latency 非负。
func TestTelemetryThreeHooksFire(t *testing.T) {
	tm := telemetry.New()
	ctx := context.Background()

	var (
		mu          sync.Mutex
		events, chunks, tools int
		latencies            []time.Duration
	)

	tm.RegisterEventHook(func(_ context.Context, d telemetry.EventHookData) {
		mu.Lock()
		defer mu.Unlock()
		events++
		latencies = append(latencies, d.LatencyMS)
	})
	tm.RegisterChunkHook(func(_ context.Context, d telemetry.ChunkHookData) {
		mu.Lock()
		defer mu.Unlock()
		chunks++
		latencies = append(latencies, d.LatencyMS)
	})
	tm.RegisterToolHook(func(_ context.Context, d telemetry.ToolHookData) {
		mu.Lock()
		defer mu.Unlock()
		tools++
		latencies = append(latencies, d.LatencyMS)
	})

	// 依次触发三类埋点。
	now := time.Now()
	tm.RecordEvent(ctx, "user/message", now.Add(-2*time.Millisecond))
	tm.RecordChunk(ctx, 1, 12, now.Add(-3*time.Millisecond))
	tm.RecordTool(ctx, "bash", true, now.Add(-5*time.Millisecond))

	mu.Lock()
	defer mu.Unlock()
	if events != 1 || chunks != 1 || tools != 1 {
		t.Fatalf("三类钩子应各触发 1 次，实际 event=%d chunk=%d tool=%d", events, chunks, tools)
	}
	if len(latencies) != 3 {
		t.Fatalf("应收集 3 条 latency，实际 %d", len(latencies))
	}
	// latency 应处于合理区间（约等于注入的 2/3/5ms）。
	for i, l := range latencies {
		if l < 0 {
			t.Fatalf("latency[%d]=%v 不应为负", i, l)
		}
		if l > 100*time.Millisecond {
			t.Fatalf("latency[%d]=%v 超出合理区间", i, l)
		}
	}
}

// TestTelemetryToolOkFlag 验证工具钩子正确携带成功/失败标记。
func TestTelemetryToolOkFlag(t *testing.T) {
	tm := telemetry.New()
	ctx := context.Background()
	var okResult bool
	tm.RegisterToolHook(func(_ context.Context, d telemetry.ToolHookData) {
		okResult = d.Ok
	})
	tm.RecordTool(ctx, "todo_write", false, time.Now())
	if okResult {
		t.Fatal("失败工具应 Ok=false")
	}
}

// TestTelemetryPanicIsolated 验证单个钩子 panic 不打断其余钩子与调用方。
func TestTelemetryPanicIsolated(t *testing.T) {
	tm := telemetry.New()
	ctx := context.Background()
	fired := 0

	tm.RegisterEventHook(func(_ context.Context, _ telemetry.EventHookData) {
		panic("boom") // 恶意钩子：直接 panic
	})
	tm.RegisterEventHook(func(_ context.Context, _ telemetry.EventHookData) {
		fired++
	})

	// 不应崩溃。
	tm.RecordEvent(ctx, "tool/bash", time.Now())
	if fired != 1 {
		t.Fatalf("panic 钩子后的正常钩子应仍被调用，实际 fired=%d", fired)
	}
}

// TestTelemetryNoHooksNoPanic 验证无钩子时埋点安全（空集分发）。
func TestTelemetryNoHooksNoPanic(t *testing.T) {
	tm := telemetry.New()
	ctx := context.Background()
	now := time.Now()
	tm.RecordEvent(ctx, "user/message", now)
	tm.RecordChunk(ctx, 1, 10, now)
	tm.RecordTool(ctx, "bash", true, now)
	// 无钩子不应 panic 即可。
}

// TestTelemetryConcurrent 验证并发埋点/注册线程安全（race 检测）。
func TestTelemetryConcurrent(t *testing.T) {
	tm := telemetry.New()
	ctx := context.Background()
	// 并发注册 + 埋点。
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tm.RegisterEventHook(func(_ context.Context, _ telemetry.EventHookData) {})
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tm.RecordEvent(ctx, "user/message", time.Now())
			tm.RecordChunk(ctx, 1, 5, time.Now())
			tm.RecordTool(ctx, "bash", true, time.Now())
		}()
	}
	wg.Wait()
}