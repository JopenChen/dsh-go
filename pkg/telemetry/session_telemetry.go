// Package telemetry 提供会话级可观测性挂钩（任务 S05：Session Telemetry hooks）。
//
// 对齐上游：packages/runtime-diagnostics/session-telemetry
//
// 设计要点：
//   - SessionTelemetry 是一个回调注册表，供上层（Agent Loop / Session）在三个
//     热路径埋点上报：
//       1) 事件追加（event）—— 每 append 一条 SessionEvent；
//       2) 流式分片（chunk）—— 每收到一个 assistant/chunk 文本分片；
//       3) 工具执行（tool）—— 每次工具调用结束（成功或失败）。
//   - 每个埋点都附「开始时间」，由 Record 方法自行计算 latency（毫秒），保证类似
//     钩子拿到的延迟口径一致；
//   - 全部回调以同步顺序调用；任何单个回调 panic 或拖慢都不影响主路径的其余
//     钩子与调用方（用 defer + recover 包裹），符合"埋点不能中断业务"的约定；
//   - latency 不强制非负，但自然时间流逝下恒为正；测试用可控 clock/时间戳断言
//     latency >= 0 且处于合理区间。
package telemetry

import (
	"context"
	"sync"
	"time"
)

// ============================================================================
// 钩子类型
// ============================================================================

// EventHookData 是事件追加事件的载荷（目标：累计每类事件数量与延迟）。
type EventHookData struct {
	EventType string        // 事件类型（如 "user/message"，来自 pkg/session）
	LatencyMS time.Duration // 单条事件追加耗时
}

// ChunkHookData 是流式分片事件的载荷。
type ChunkHookData struct {
	Seq      uint64        // 分片序号
	TextLen  int           // 文本长度
	LatencyMS time.Duration // 单分片耗时
}

// ToolHookData 是工具执行事件的载荷。
type ToolHookData struct {
	ToolName  string        // 工具名（如 "bash" / "todo_write"）
	Ok        bool          // 是否成功（err == nil）
	LatencyMS time.Duration // 工具执行耗时
}

// EventHook / ChunkHook / ToolHook 是三类钩子签名。
type EventHook func(ctx context.Context, d EventHookData)
type ChunkHook func(ctx context.Context, d ChunkHookData)
type ToolHook func(ctx context.Context, d ToolHookData)

// ============================================================================
// SessionTelemetry 注册表
// ============================================================================

// SessionTelemetry 是会话级遥测注册表（线程安全）。
type SessionTelemetry struct {
	mu          sync.RWMutex
	eventHooks  []EventHook
	chunkHooks  []ChunkHook
	toolHooks   []ToolHook
	now         func() time.Time // 可控时钟（测试注入）
}

// New 创建空遥测注册表。
func New() *SessionTelemetry {
	return &SessionTelemetry{
		eventHooks: []EventHook{},
		chunkHooks: []ChunkHook{},
		toolHooks:  []ToolHook{},
		now:        time.Now,
	}
}

// nowFn 返回当前时钟函数（供测试与实现使用）。
func (t *SessionTelemetry) nowFn() func() time.Time {
	if t.now == nil {
		return time.Now
	}
	return t.now
}

// ============================================================================
// 注册
// ============================================================================

// RegisterEventHook 注册事件追加钩子。
func (t *SessionTelemetry) RegisterEventHook(h EventHook) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.eventHooks = append(t.eventHooks, h)
}

// RegisterChunkHook 注册流式分片钩子。
func (t *SessionTelemetry) RegisterChunkHook(h ChunkHook) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.chunkHooks = append(t.chunkHooks, h)
}

// RegisterToolHook 注册工具执行钩子。
func (t *SessionTelemetry) RegisterToolHook(h ToolHook) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.toolHooks = append(t.toolHooks, h)
}

// ============================================================================
// 埋点（Record）
// ============================================================================

// RecordEvent 记录一次事件追加；startedAt 为事件开始时间（time.Time 或已算好的递减基准）。
func (t *SessionTelemetry) RecordEvent(ctx context.Context, eventType string, startedAt time.Time) {
	d := EventHookData{EventType: eventType, LatencyMS: t.since(startedAt)}
	t.dispatchEvent(ctx, d)
}

// RecordChunk 记录一次流式分片。
func (t *SessionTelemetry) RecordChunk(ctx context.Context, seq uint64, textLen int, startedAt time.Time) {
	d := ChunkHookData{Seq: seq, TextLen: textLen, LatencyMS: t.since(startedAt)}
	t.dispatchChunk(ctx, d)
}

// RecordTool 记录一次工具执行结束。
func (t *SessionTelemetry) RecordTool(ctx context.Context, toolName string, ok bool, startedAt time.Time) {
	d := ToolHookData{ToolName: toolName, Ok: ok, LatencyMS: t.since(startedAt)}
	t.dispatchTool(ctx, d)
}

// since 计算与 startedAt 的耗时（毫秒）。
func (t *SessionTelemetry) since(startedAt time.Time) time.Duration {
	if startedAt.IsZero() {
		return 0
	}
	return t.nowFn()().Sub(startedAt)
}

// ============================================================================
// 分发（每个钩子都做 panic 隔离，互不影响）
// ============================================================================

func (t *SessionTelemetry) dispatchEvent(ctx context.Context, d EventHookData) {
	t.mu.RLock()
	hooks := append([]EventHook(nil), t.eventHooks...)
	t.mu.RUnlock()
	for _, h := range hooks {
		callSafe(func() { h(ctx, d) })
	}
}

func (t *SessionTelemetry) dispatchChunk(ctx context.Context, d ChunkHookData) {
	t.mu.RLock()
	hooks := append([]ChunkHook(nil), t.chunkHooks...)
	t.mu.RUnlock()
	for _, h := range hooks {
		callSafe(func() { h(ctx, d) })
	}
}

func (t *SessionTelemetry) dispatchTool(ctx context.Context, d ToolHookData) {
	t.mu.RLock()
	hooks := append([]ToolHook(nil), t.toolHooks...)
	t.mu.RUnlock()
	for _, h := range hooks {
		callSafe(func() { h(ctx, d) })
	}
}

// callSafe 以 recover 包裹回调，防止单个埋点 panic 打断主流程。
func callSafe(fn func()) {
	defer func() { _ = recover() }()
	fn()
}