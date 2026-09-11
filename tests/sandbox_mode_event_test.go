// Package tests 的沙箱模式事件持久化验收测试。
//
// 覆盖：
//   - sandbox/mode 事件类型定义与 round-trip
//   - EffectiveSandboxMode fold：取最后一个 sandbox/mode 事件
//   - 无 sandbox/mode 事件时返回 ok=false
//   - SetSandboxMode 向会话 append 事件
//   - source: 'delegation' 子代理委派标记
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/sandbox"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// TestSandboxModeEventType 验证 sandbox/mode 事件类型定义。
func TestSandboxModeEventType(t *testing.T) {
	if session.EventSandboxMode != "sandbox/mode" {
		t.Fatalf("事件类型应为 sandbox/mode, 实际 %s", session.EventSandboxMode)
	}
	data := session.SandboxModeData{Mode: string(sandbox.ModeWorkspaceWrite)}
	if data.EventType() != session.EventSandboxMode {
		t.Fatal("SandboxModeData.EventType() 应返回 EventSandboxMode")
	}
}

// TestSandboxModeRoundTrip 验证 sandbox/mode 事件的 append + 读回 round-trip。
func TestSandboxModeRoundTrip(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("sbox-test"))
	_, err := sl.Append(session.SandboxModeData{Mode: string(sandbox.ModeReadOnly)})
	if err != nil {
		t.Fatalf("append sandbox/mode 失败: %v", err)
	}
	evs := sl.Events()
	if len(evs) != 1 {
		t.Fatalf("应有 1 条事件, 实际 %d", len(evs))
	}
	if evs[0].Type != session.EventSandboxMode {
		t.Fatalf("事件类型应为 sandbox/mode, 实际 %s", evs[0].Type)
	}
	data, ok := evs[0].Data.(session.SandboxModeData)
	if !ok {
		t.Fatal("事件数据应为 SandboxModeData")
	}
	if data.Mode != string(sandbox.ModeReadOnly) {
		t.Fatalf("mode 应为 read-only, 实际 %s", data.Mode)
	}
}

// TestEffectiveSandboxModeFold 验证 EffectiveSandboxMode 取最后一个事件。
func TestEffectiveSandboxModeFold(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("sbox-fold"))
	sl.Append(session.SandboxModeData{Mode: string(sandbox.ModeReadOnly)})
	sl.Append(session.UserMessageData{Content: "hello"})
	sl.Append(session.SandboxModeData{Mode: string(sandbox.ModeWorkspaceWrite)})
	sl.Append(session.AssistantMessageData{Content: "hi"})

	mode, ok := sandbox.EffectiveSandboxMode(sl.Events())
	if !ok {
		t.Fatal("应有 sandbox/mode 事件")
	}
	if mode != sandbox.ModeWorkspaceWrite {
		t.Fatalf("应取最后一个 workspace-write, 实际 %s", mode)
	}
}

// TestEffectiveSandboxModeNoEvent 验证无事件时返回 ok=false。
func TestEffectiveSandboxModeNoEvent(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("sbox-none"))
	sl.Append(session.UserMessageData{Content: "hello"})

	_, ok := sandbox.EffectiveSandboxMode(sl.Events())
	if ok {
		t.Fatal("无 sandbox/mode 事件时应返回 ok=false")
	}
}

// TestEffectiveSandboxModeEmpty 验证空事件列表。
func TestEffectiveSandboxModeEmpty(t *testing.T) {
	_, ok := sandbox.EffectiveSandboxMode(nil)
	if ok {
		t.Fatal("空事件列表应返回 ok=false")
	}
}

// TestSetSandboxMode 验证 SetSandboxMode 向会话 append 事件。
func TestSetSandboxMode(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("sbox-set"))
	err := sandbox.SetSandboxMode(sl, sandbox.ModeDangerFullAccess)
	if err != nil {
		t.Fatalf("SetSandboxMode 失败: %v", err)
	}
	evs := sl.Events()
	if len(evs) != 1 {
		t.Fatalf("应有 1 条事件, 实际 %d", len(evs))
	}
	if evs[0].Type != session.EventSandboxMode {
		t.Fatalf("事件类型应为 sandbox/mode, 实际 %s", evs[0].Type)
	}
	data := evs[0].Data.(session.SandboxModeData)
	if data.Mode != string(sandbox.ModeDangerFullAccess) {
		t.Fatalf("mode 应为 danger-full-access, 实际 %s", data.Mode)
	}
}

// TestSetSandboxModeDelegation 验证子代理委派标记 source='delegation'。
func TestSetSandboxModeDelegation(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("sbox-deleg"))
	err := sandbox.SetSandboxModeWithSource(sl, sandbox.ModeWorkspaceWrite, sandbox.ModeSourceDelegation)
	if err != nil {
		t.Fatalf("SetSandboxModeWithSource 失败: %v", err)
	}
	data := sl.Events()[0].Data.(session.SandboxModeData)
	if data.Source != string(sandbox.ModeSourceDelegation) {
		t.Fatalf("source 应为 delegation, 实际 %q", data.Source)
	}
}

// TestSandboxModeSessionInterface 验证 SessionLog 实现 SandboxMode 时从事件 fold。
func TestSandboxModeSessionInterface(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("sbox-iface"))
	sl.Append(session.SandboxModeData{Mode: string(sandbox.ModeReadOnly)})

	mode, ok := sl.SandboxMode()
	if !ok {
		t.Fatal("SessionLog.SandboxMode() 应从事件 fold 返回")
	}
	if string(mode) != string(sandbox.ModeReadOnly) {
		t.Fatalf("mode 应为 read-only, 实际 %s", mode)
	}
}
