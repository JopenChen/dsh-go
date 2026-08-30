// 本文件对应任务 M33：Agent Initiator 上下文（安全归因）。
//
// 对齐上游：packages/core/agent initiator module
//
// 设计要点：
//   - withInitiator 在 context 中注入当前发起操作的 Agent 身份 + 操作名；
//   - requireInitiator 在无 withInitiator 包裹的调用点返回 Unauthorized 结构化错误
//     （不允许静默放行，阻断"匿名发起"的安全隐患）；
//   - withoutInitiator 显式清除 initiator（进入不受控区域时隔离身份）。
package agent

import (
	"context"
	"fmt"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// initiatorKey 是 context 中 initiator 的键类型。
type initiatorKey struct{}

// Initiator 描述一次操作的发起者。
type Initiator struct {
	// AgentID 发起操作的 Agent 会话 ID。
	AgentID brand.SessionID
	// Op 操作名（如 "Run" / "Followup" / "executeTool"）。
	Op string
}

// UnauthorizedInitiatorError 表示在无 initiator 包裹时调用了 requireInitiator。
type UnauthorizedInitiatorError struct {
	Op string
}

// Error 实现 error 接口。
func (e *UnauthorizedInitiatorError) Error() string {
	return fmt.Sprintf("agent: unauthorized initiator: operation %q called without withInitiator context", e.Op)
}

// WithInitiator 返回携带 initiator 的 context。
func WithInitiator(ctx context.Context, agentID brand.SessionID, op string) context.Context {
	return context.WithValue(ctx, initiatorKey{}, &Initiator{AgentID: agentID, Op: op})
}

// RequireInitiator 取出 initiator；若不存在则返回 UnauthorizedInitiatorError。
// 用于所有需要"安全归因"的操作入口。
func RequireInitiator(ctx context.Context) (*Initiator, error) {
	ini, ok := ctx.Value(initiatorKey{}).(*Initiator)
	if !ok || ini == nil {
		return nil, &UnauthorizedInitiatorError{Op: "unknown"}
	}
	return ini, nil
}

// WithoutInitiator 返回不携带任何 initiator 的 context（身份隔离）。
func WithoutInitiator(ctx context.Context) context.Context {
	return context.WithValue(ctx, initiatorKey{}, nil)
}

// MustInitiator 便捷版本：无 initiator 时 panic（用于内部已保证有 initiator 的调用点）。
func MustInitiator(ctx context.Context) *Initiator {
	ini, err := RequireInitiator(ctx)
	if err != nil {
		panic(err)
	}
	return ini
}