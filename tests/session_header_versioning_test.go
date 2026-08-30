// 本文件对应任务 M06：SessionHeader 元数据。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// TestSessionHeaderRoundTrip 验证头部序列化/反序列化一致。
func TestSessionHeaderRoundTrip(t *testing.T) {
	h := session.NewSessionHeader(brand.NewSessionID("s1"), "/workspace")
	h.AgentPreset = "coder"

	data, err := h.Marshal()
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	restored, err := session.UnmarshalSessionHeader(data)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if restored.ID.Raw() != "s1" || restored.Cwd != "/workspace" || restored.AgentPreset != "coder" {
		t.Fatalf("字段还原不一致: %+v", restored)
	}
	if restored.Version != session.SessionFormatVersion {
		t.Fatalf("版本 = %d, want %d", restored.Version, session.SessionFormatVersion)
	}
	if restored.Origin != session.OriginCreated {
		t.Fatalf("origin = %q, want created", restored.Origin)
	}
	if restored.DelegationDepth != 0 {
		t.Fatalf("初始委派深度应为 0: %d", restored.DelegationDepth)
	}
}

// TestSessionHeaderUnknownVersionRejected 验证未知版本 fail-closed 拒绝读。
func TestSessionHeaderUnknownVersionRejected(t *testing.T) {
	// 构造一个版本为 999 的头部 JSON，模拟未来格式
	data := []byte(`{"version":999,"id":"s_x","createdAt":"2026-08-31T00:00:00Z"}`)

	_, err := session.UnmarshalSessionHeader(data)
	if err == nil {
		t.Fatal("未知版本头部应被拒绝读取")
	}
	// 应返回 SessionFormatUnsupportedError
	var unsupported *session.SessionFormatUnsupportedError
	if !asUnsupported(err, &unsupported) {
		t.Fatalf("应返回 SessionFormatUnsupportedError, 实际 %T", err)
	}
}

// asUnsupported 便捷类型断言。
func asUnsupported(err error, target **session.SessionFormatUnsupportedError) bool {
	e, ok := err.(*session.SessionFormatUnsupportedError)
	if ok {
		*target = e
	}
	return ok
}

// TestSessionHeaderForkLineage 验证 fork 后 ParentSession/DelegationDepth 正确写入。
func TestSessionHeaderForkLineage(t *testing.T) {
	parent := session.NewSessionHeader(brand.NewSessionID("parent_s"), "/ws")

	// 父会话回放了 42 条种子事件
	parent.SeedLength = 42

	child := parent.Fork(brand.NewSessionID("child_s"))
	if child.ParentSession.Raw() != "parent_s" {
		t.Fatalf("child.ParentSession = %q, want parent_s", child.ParentSession.Raw())
	}
	if child.Origin != session.OriginFork {
		t.Fatalf("child.Origin = %q, want fork", child.Origin)
	}
	if child.DelegationDepth != 1 {
		t.Fatalf("child.DelegationDepth = %d, want 1", child.DelegationDepth)
	}
	if child.SeedLength != 0 {
		t.Fatalf("fork 子会话 SeedLength 应为 0: %d", child.SeedLength)
	}
	if child.Cwd != "/ws" {
		t.Fatalf("child.Cwd 应继承父会话: %q", child.Cwd)
	}

	// 子会话回放后写入 SeedLength（cold-resume 语义）
	child.SeedLength = 7

	// 深度再次 +1
	grandchild := child.Fork(brand.NewSessionID("gc_s"))
	if grandchild.DelegationDepth != 2 {
		t.Fatalf("grandchild.DelegationDepth = %d, want 2", grandchild.DelegationDepth)
	}
	if grandchild.ParentSession.Raw() != "child_s" {
		t.Fatalf("grandchild.ParentSession = %q, want child_s", grandchild.ParentSession.Raw())
	}
}

// TestSessionHeaderValidate 验证缺失必填字段的头部校验失败。
func TestSessionHeaderValidate(t *testing.T) {
	bad := &session.SessionHeader{Version: session.SessionFormatVersion}
	if err := bad.Validate(); err == nil {
		t.Fatal("缺 ID 的头部校验应失败")
	}

	good := session.NewSessionHeader(brand.NewSessionID("s1"), "/ws")
	if err := good.Validate(); err != nil {
		t.Fatalf("合法头部校验应通过: %v", err)
	}
}
