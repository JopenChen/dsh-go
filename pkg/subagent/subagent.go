// Package subagent 提供子代理接缝（任务 S02：Subagent 3 后端）。
//
// 对齐上游：packages/subagent/subagent + providers
//
// 设计要点：
//   - Provider 抽象把「如何起一个子代理」与「子代理执行什么」解耦，支持三种后端：
//       * InProcess —— 进程内 fork（复用当前 Agent 上下文执行一个子任务，单测主用）；
//       * ACP / ForkCopy —— 原生协议/复制进程类后端（本版提供最小可运行桩，接口稳定）；
//   - ForkLineage 记录父子关系（parent 会话 → 子会话），供因果归因（与 M33 Initiator 一致）；
//   - Handle 是子代理的生命周期句柄：Drain 阻塞等子代理落盘完成；Dispose 释放并级联取消子代理
//     （父 dispose → 子自动 drain/清理）。
package subagent

import (
	"context"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// ============================================================================
// 类型
// ============================================================================

// ForkLineage 记录一次 fork 的家谱（父会话 → 子会话）。
type ForkLineage struct {
	Parent  *brand.SessionID `json:"parent,omitempty"`  // 父会话（nil=顶层）
	Session brand.SessionID  `json:"session"`            // 子会话
}

// SpawnRequest 是生成子代理的请求。
type SpawnRequest struct {
	Parent    *brand.SessionID `json:"parent,omitempty"`
	Input     string           `json:"input"`
	MaxRounds int              `json:"maxRounds,omitempty"`
}

// SpawnResult 是一次子代理运行的输出。
type SpawnResult struct {
	Lineage ForkLineage `json:"lineage"`
	Output  string      `json:"output"`
	Err     string      `json:"err,omitempty"`
}

// Provider 是子代理后端接缝。
type Provider interface {
	// Name 返回后端名（如 "in-process" / "acp" / "fork-copy"）。
	Name() string
	// Spawn 生成并运行一个子代理，返回可释放的句柄。
	Spawn(ctx context.Context, req SpawnRequest) (*Handle, error)
}

// Handle 是子代理生命周期句柄。
type Handle struct {
	Provider string   // 所属后端名
	Lineage  ForkLineage
	mu       sync.Mutex
	disposed bool
	drained  bool
	done     chan struct{}
}

// newHandle 构造句柄。
func newHandle(provider string, lineage ForkLineage) *Handle {
	return &Handle{Provider: provider, Lineage: lineage, done: make(chan struct{})}
}

// markDrained 标记子代理已落盘/结束（receiver 侧完成时调用）。
func (h *Handle) markDrained() {
	h.mu.Lock()
	if !h.drained {
		h.drained = true
		close(h.done)
	}
	h.mu.Unlock()
}

// Drain 阻塞直到子代理完成（或 ctx 取消）。
func (h *Handle) Drain(ctx context.Context) error {
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Dispose 释放子代理：标记 disposed（父 dispose 触发子级联清理）。
func (h *Handle) Dispose() {
	h.mu.Lock()
	h.disposed = true
	h.mu.Unlock()
	// 释放即视为接收方应尽快清理；此处标记 drained 以便 Drain 立即返回。
	h.markDrained()
}

// -------------------- ACP / ForkCopy 桩 Provider --------------------

// acpProvider 是 ACP child 后端的桩（真实接入原生 MCP 时替换 Spawn 实现）。
type acpProvider struct{}

func (acpProvider) Name() string { return "acp" }

func (acpProvider) Spawn(ctx context.Context, req SpawnRequest) (*Handle, error) {
	sid := brand.NewSessionID("acp-" + req.Input)
	h := newHandle("acp", ForkLineage{Parent: req.Parent, Session: sid})
	// 桩：同步标记完成。
	h.markDrained()
	return h, nil
}

// forkCopyProvider 是 fork-copy-process 后端的桩（复制进程执行）。
type forkCopyProvider struct{}

func (forkCopyProvider) Name() string { return "fork-copy" }

func (forkCopyProvider) Spawn(ctx context.Context, req SpawnRequest) (*Handle, error) {
	sid := brand.NewSessionID("fork-" + req.Input)
	h := newHandle("fork-copy", ForkLineage{Parent: req.Parent, Session: sid})
	h.markDrained()
	return h, nil
}

// ============================================================================
// Runtime
// ============================================================================

// Runtime 是子代理运行时：持有 Provider 注册表与活跃句柄。
type Runtime struct {
	mu       sync.RWMutex
	providers map[string]Provider
	handles  map[string]*Handle
}

// NewRuntime 创建运行时，并注册内置 ACP 与 ForkCopy 桩 Provider。
func NewRuntime() *Runtime {
	r := &Runtime{providers: map[string]Provider{}, handles: map[string]*Handle{}}
	// 内置可运行桩。
	_ = r.RegisterProvider(acpProvider{})
	_ = r.RegisterProvider(forkCopyProvider{})
	return r
}

// RegisterProvider 注册后端。
func (r *Runtime) RegisterProvider(p Provider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
	return nil
}

// Provider 获取指定后端。
func (r *Runtime) Provider(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// Spawn 通过指定后端生成子代理，并登记句柄。
func (r *Runtime) Spawn(ctx context.Context, providerName string, req SpawnRequest) (*Handle, error) {
	p, ok := r.Provider(providerName)
	if !ok {
		return nil, &ErrUnknownProvider{Name: providerName}
	}
	h, err := p.Spawn(ctx, req)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.handles[h.Lineage.Session.Raw()] = h
	r.mu.Unlock()
	return h, nil
}

// DisposeOwner 释放某父会话名下的全部子代理句柄（级联取消）。
func (r *Runtime) DisposeOwner(parent brand.SessionID) {
	r.mu.RLock()
	handles := make([]*Handle, 0, len(r.handles))
	for _, h := range r.handles {
		if h.Lineage.Parent != nil && *h.Lineage.Parent == parent {
			handles = append(handles, h)
		}
	}
	r.mu.RUnlock()
	for _, h := range handles {
		h.Dispose()
	}
}

// ActiveCount 返回当前登记句柄数（诊断用）。
func (r *Runtime) ActiveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handles)
}

// ============================================================================
// 错误
// ============================================================================

// ErrUnknownProvider 表示后端未注册。
type ErrUnknownProvider struct{ Name string }

func (e *ErrUnknownProvider) Error() string {
	return "subagent: unknown provider " + e.Name
}

// IsUnknownProvider 判断是否为未知后端错误。
func IsUnknownProvider(err error) bool {
	_, ok := err.(*ErrUnknownProvider)
	return ok
}