// 本文件对应任务 M33：Agent Initiator 上下文（安全归因）。
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/agent"
	"github.com/JopenChen/dsh-go/pkg/brand"
)

// TestInitiatorCausalTrace 验证 initiator 随 context 传递并携带正确身份。
func TestInitiatorCausalTrace(t *testing.T) {
	ctx := agent.WithInitiator(context.Background(), brand.NewSessionID("agent_x"), "Run")

	// 模拟进入子调用后仍能取到
	weakCtx := context.WithValue(ctx, "somekey", "v") // 无关 value 不影响 initiator
	ini, err := agent.RequireInitiator(weakCtx)
	if err != nil {
		t.Fatalf("RequireInitiator 失败: %v", err)
	}
	if ini.AgentID.Raw() != "agent_x" {
		t.Fatalf("AgentID = %q, want agent_x", ini.AgentID.Raw())
	}
	if ini.Op != "Run" {
		t.Fatalf("Op = %q, want Run", ini.Op)
	}
}

// TestInitiatorUnauthorized 验证无 withInitiator 包裹时返回结构化错误。
func TestInitiatorUnauthorized(t *testing.T) {
	_, err := agent.RequireInitiator(context.Background())
	if err == nil {
		t.Fatal("无 initiator 调用应报错")
	}
	var unauthorized *agent.UnauthorizedInitiatorError
	if !asUnauthorizedInitiator(err, &unauthorized) {
		t.Fatalf("应为 UnauthorizedInitiatorError, 实际 %T", err)
	}
}

// TestInitiatorWithout 验证 withoutInitiator 隔离身份后 require 失败。
func TestInitiatorWithout(t *testing.T) {
	withIni := agent.WithInitiator(context.Background(), brand.NewSessionID("a"), "op")
	cleared := agent.WithoutInitiator(withIni)

	if _, err := agent.RequireInitiator(cleared); err == nil {
		t.Fatal("withoutInitiator 后 initiator 应被清除")
	}
}

// TestInitiatorMustPanic 验证无 initiator 时 MustInitiator panic。
func TestInitiatorMustPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustInitiator 无 initiator 应 panic")
		}
	}()
	_ = agent.MustInitiator(context.Background())
}

// asUnauthorizedInitiator 便捷类型断言。
func asUnauthorizedInitiator(err error, target **agent.UnauthorizedInitiatorError) bool {
	e, ok := err.(*agent.UnauthorizedInitiatorError)
	if ok {
		*target = e
	}
	return ok
}