// 本文件验证任务 S07：OTel Telemetry 导出。
//
// 覆盖：S05 三类钩子经 OTelBridge 桥到 InMemoryExporter 各产出一条 span；每条 span
// 携带 session id + turn id + step id 三 baggage；事件/分片/工具 Kind 正确。
package tests

import (
	"context"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/telemetry"
)

// TestOTelBridgeSpansCarryBaggage 验证三类埋点都导出 span 且带三 id baggage。
func TestOTelBridgeSpansCarryBaggage(t *testing.T) {
	tm := telemetry.New()
	exporter := &telemetry.InMemoryExporter{}

	// 固定 baggage：session/turn/step。
	bag := telemetry.Baggage{SessionID: "s-1", TurnID: "t-1", StepID: "st-1"}
	bridge := telemetry.NewOTelBridge(exporter, func() telemetry.Baggage { return bag })
	bridge.Attach(tm)

	ctx := context.Background()
	now := time.Now()
	tm.RecordEvent(ctx, "user/message", now.Add(-time.Millisecond))
	tm.RecordChunk(ctx, 7, 42, now.Add(-time.Millisecond))
	tm.RecordTool(ctx, "bash", true, now.Add(-time.Millisecond))

	spans := exporter.Collect()
	if len(spans) != 3 {
		t.Fatalf("应导出 3 条 span，实际 %d", len(spans))
	}
	for i, sp := range spans {
		if sp.Baggage.SessionID != "s-1" || sp.Baggage.TurnID != "t-1" || sp.Baggage.StepID != "st-1" {
			t.Fatalf("span[%d] 应带 session/turn/step baggage，实际 %+v", i, sp.Baggage)
		}
	}
	// Kind 顺序：event(chunk/tool 顺序按注册顺序由遥测分发决定，这里只验各 Kind 出现。
	kinds := map[string]bool{}
	for _, sp := range spans {
		kinds[sp.Kind] = true
	}
	if !kinds["event"] || !kinds["chunk"] || !kinds["tool"] {
		t.Fatalf("应包含 event/chunk/tool 三类 Kind，实际 %v", kinds)
	}
}

// TestOTelSpanNameSuffix 验证工具 span 名带工具名（session.tool.<tool>）。
func TestOTelSpanNameSuffix(t *testing.T) {
	tm := telemetry.New()
	exporter := &telemetry.InMemoryExporter{}
	bridge := telemetry.NewOTelBridge(exporter, nil)
	bridge.Attach(tm)
	tm.RecordTool(context.Background(), "todo_write", false, time.Now())

	for _, sp := range exporter.Collect() {
		if sp.Kind == "tool" && sp.Name != "session.tool.todo_write" {
			t.Fatalf("工具 span 名应为 session.tool.todo_write，实际 %s", sp.Name)
		}
	}
}

// TestOTelNoBaggageProvider 验证无 baggage provider 也不 panic，span 空 baggage 可导出。
func TestOTelNoBaggageProvider(t *testing.T) {
	tm := telemetry.New()
	exporter := &telemetry.InMemoryExporter{}
	bridge := telemetry.NewOTelBridge(exporter, nil)
	bridge.Attach(tm)
	tm.RecordEvent(context.Background(), "user/message", time.Now())
	if n := len(exporter.Collect()); n != 1 {
		t.Fatalf("应导出 1 条 span，实际 %d", n)
	}
}