// 本文件实现任务 S07：OTel Telemetry 导出（OTLP exporter 门面 + S05 挂钩 + 三 id baggage）。
//
// 对齐上游：packages/runtime-diagnostics/telemetry-otel
//
// 设计要点：
//   - 不强制绑定第三方 SDK：定义 Exporter 抽象（ExportSpan），即可对接真实 OTLP 网关，
//     也可在测试/本地用 InMemoryExporter 收集断言（N09 同为自研探针，保持依赖可控）；
//   - Span 携带 Baggage（session/turn/step 三 id），保证下游可按事务链路聚合；
//   - OTelBridge 把 S05 的 SessionTelemetry 三类钩子（event/chunk/tool）接进来，每次
//     埋点 emit 一条带 baggage 的 span 到 Exporter。
package telemetry

import (
	"context"
	"sync"
	"time"
)

// ============================================================================
// Baggage & Span
// ============================================================================

// Baggage 是跨量纲传递的链路上下文（session/turn/step 三 id）。
type Baggage struct {
	SessionID string `json:"sessionId"`
	TurnID    string `json:"turnId"`
	StepID    string `json:"stepId"`
}

// Span 是导出的一条可观测 span。
type Span struct {
	Name     string            `json:"name"`
	Baggage  Baggage           `json:"baggage"`
	Started  time.Time         `json:"started"`
	Ended    time.Time         `json:"ended"`
	Kind     string            `json:"kind,omitempty"` // event/chunk/tool
	Attrs    map[string]string `json:"attrs,omitempty"`
}

// ============================================================================
// Exporter
// ============================================================================

// Exporter 是 span 的导出目标（对应 OTLP exporter）。
type Exporter interface {
	ExportSpan(ctx context.Context, span Span) error
}

// ============================================================================
// InMemoryExporter（测试/本地收集）
// ============================================================================

// InMemoryExporter 收集 span 到内存（测试断言用）。
type InMemoryExporter struct {
	mu    sync.Mutex
	Spans []Span
}

// ExportSpan 记录一条 span。
func (m *InMemoryExporter) ExportSpan(_ context.Context, span Span) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Spans = append(m.Spans, span)
	return nil
}

// Collect 返回已收集 span 的快照。
func (m *InMemoryExporter) Collect() []Span {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Span, len(m.Spans))
	copy(out, m.Spans)
	return out
}

// ============================================================================
// BaggageProvider & OTelBridge
// ============================================================================

// BaggageProvider 提供当前链路 baggage（由上层按当前 session/turn/step 注入）。
type BaggageProvider func() Baggage

// OTelBridge 把 S05 遥测钩子桥接到 Exporter。
type OTelBridge struct {
	exporter Exporter
	baggage  BaggageProvider
}

// NewOTelBridge 创建桥接器。
func NewOTelBridge(exporter Exporter, baggage BaggageProvider) *OTelBridge {
	return &OTelBridge{exporter: exporter, baggage: baggage}
}

// Attach 注册到 SessionTelemetry（S05），事件/分片/工具三类钩子都会 emit span。
func (b *OTelBridge) Attach(tm *SessionTelemetry) {
	bag := b.currentBaggage()
	tm.RegisterEventHook(func(ctx context.Context, d EventHookData) {
		_ = b.exporter.ExportSpan(ctx, Span{
			Name:    "session.event." + d.EventType,
			Baggage: bag,
			Kind:    "event",
			Started: time.Now(), Ended: time.Now(),
			Attrs: map[string]string{"latency_ms": d.LatencyMS.String()},
		})
	})
	tm.RegisterChunkHook(func(ctx context.Context, d ChunkHookData) {
		_ = b.exporter.ExportSpan(ctx, Span{
			Name:    "session.chunk",
			Baggage: bag,
			Kind:    "chunk",
			Started: time.Now(), Ended: time.Now(),
			Attrs: map[string]string{"seq": u64str(d.Seq), "len": itoa(d.TextLen)},
		})
	})
	tm.RegisterToolHook(func(ctx context.Context, d ToolHookData) {
		_ = b.exporter.ExportSpan(ctx, Span{
			Name:    "session.tool." + d.ToolName,
			Baggage: bag,
			Kind:    "tool",
			Started: time.Now(), Ended: time.Now(),
			Attrs: map[string]string{"ok": boolstr(d.Ok), "latency_ms": d.LatencyMS.String()},
		})
	})
}

// currentBaggage 取当前 baggage（仅评估一次，保证同一次埋点链路一致）。
func (b *OTelBridge) currentBaggage() Baggage {
	if b.baggage == nil {
		return Baggage{}
	}
	return b.baggage()
}

// ============================================================================
// 工具
// ============================================================================

func u64str(v uint64) string {
	return itoa(int(v))
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	var buf [20]byte
	i := len(buf)
	for v != 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func boolstr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}