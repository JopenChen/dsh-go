// 本文件实现 subagent 的 in-process fork 后端（任务 S02）。
//
// InProcessProvider 在进程内 fork 一个子代理：复用当前进程的上下文（LLM 适配器 /
// 工具流水线），执行给定的子任务输入，返回父→子家谱与输出。它是 S02 验收测试
// （subagent_fork_inprocess_test.go）对应的可运行后端；ACP / fork-copy 提供桩以便接口留位。
package subagent

import (
	"context"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// InProcessRunner 是 in-process 后端的实际执行函数（由上层注入事务性实现）。
// 约定：返回子会话 ID、输出文本与错误。
type InProcessRunner func(ctx context.Context, req SpawnRequest) (sid brand.SessionID, output string, err error)

// InProcessProvider 是进程内 fork 后端。
type InProcessProvider struct {
	runner InProcessRunner
	mu     sync.Mutex
}

// NewInProcessProvider 创建 in-process 后端。runner 为 nil 时默认同步占位执行。
func NewInProcessProvider(runner InProcessRunner) *InProcessProvider {
	return &InProcessProvider{runner: runner}
}

// Name 实现 Provider。
func (p *InProcessProvider) Name() string { return "in-process" }

// Spawn 实现 Provider：调用 runner 执行子任务并返回句柄。
func (p *InProcessProvider) Spawn(ctx context.Context, req SpawnRequest) (*Handle, error) {
	runner := p.runner
	if runner == nil {
		// 默认占位：直接以输入为会话 ID，输出原样。
		defaultRunner := func(_ context.Context, r SpawnRequest) (brand.SessionID, string, error) {
			sid := brand.NewSessionID("inprocess-" + r.Input)
			return sid, "in-process:" + r.Input, nil
		}
		runner = defaultRunner
	}

	sid, output, err := runner(ctx, req)
	h := newHandle("in-process", ForkLineage{Parent: req.Parent, Session: sid})
	if err != nil {
		// 运行失败仍返回句柄，但带错误标记且立即 drained。
		h.markDrained()
		return h, err
	}
	_ = output
	// 同步执行完成后立即标记完成（进程内无后台协程）。
	h.markDrained()
	return h, nil
}