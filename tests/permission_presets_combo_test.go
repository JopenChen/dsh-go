// Package tests 的权限预设（M28）验收测试。
//
// 覆盖：
//   - 内置 4 预设（safe/danger/review/custom）的组合元组一一对应
//   - 未知预设回落 custom
//   - Derive 派生状态（含用户 override → IsCustom + 派生字段）
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/approval"
	"github.com/JopenChen/dsh-go/pkg/presets"
	"github.com/JopenChen/dsh-go/pkg/sandbox"
)

// TestPermissionPresetsCombo 验证 4 预设的 {sandboxMode, approvalPolicy} 组合。
func TestPermissionPresetsCombo(t *testing.T) {
	cases := []struct {
		name    string
		sandbox sandbox.SandboxMode
		approx  approval.Policy
	}{
		{presets.PresetSafe, sandbox.ModeReadOnly, approval.PolicyAskDangerous},
		{presets.PresetDanger, sandbox.ModeDangerFullAccess, approval.PolicyAllowAll},
		{presets.PresetReview, sandbox.ModeReadOnly, approval.PolicyAskDangerousEdit},
		{presets.PresetCustom, sandbox.ModeWorkspaceWrite, approval.PolicyAskDangerous},
	}
	for _, c := range cases {
		p, ok := presets.Resolve(c.name)
		if !ok {
			t.Fatalf("预设 %q Resolve 应成功", c.name)
		}
		if p.SandboxMode != c.sandbox || p.ApprovalPolicy != c.approx {
			t.Errorf("预设 %q 组合错误: got %+v, want sandbox=%s approval=%s", c.name, p, c.sandbox, c.approx)
		}
	}
	// 名称集合稳定。
	names := presets.PresetNames()
	if len(names) != 4 {
		t.Fatalf("应有 4 个预设, 实际 %v", names)
	}
}

// TestPermissionPresetResolveUnknownFallsBackToCustom 验证未知预设回落 custom。
func TestPermissionPresetResolveUnknownFallsBackToCustom(t *testing.T) {
	p, ok := presets.Resolve("nonexistent-preset")
	if !ok {
		t.Fatal("未知预设应回落 custom 且 ok=true")
	}
	if p.Name != presets.PresetCustom {
		t.Fatalf("应回落 custom, 实际 %s", p.Name)
	}
	if p.SandboxMode != sandbox.ModeWorkspaceWrite {
		t.Fatalf("custom sandbox 应为 workspace-write, 实际 %s", p.SandboxMode)
	}
}

// TestPermissionPresetMappers 验证 preset→approval/sandbox 映射函数（供 approval.Service 接线）。
func TestPermissionPresetMappers(t *testing.T) {
	am := presets.ApprovalMapper()
	sb := presets.SandboxMapper()
	pol, _ := am("danger")
	if pol != approval.PolicyAllowAll {
		t.Fatalf("danger→approval 应 allow-all, 实际 %s", pol)
	}
	scale, _ := sb("review")
	if scale != sandbox.ModeReadOnly {
		t.Fatalf("review→sandbox 应 read-only, 实际 %s", scale)
	}
}

// TestPermissionPresetDerive 验证 Derive 派生状态与 IsCustom 标记。
func TestPermissionPresetDerive(t *testing.T) {
	// 无 override：非 custom。
	st := presets.Derive(presets.PresetSafe, nil, nil)
	if st.IsCustom {
		t.Fatal("无 override 不应是 custom")
	}
	if st.ActivePreset != presets.PresetSafe || st.SandboxMode != sandbox.ModeReadOnly {
		t.Fatalf("safe 派生状态错误: %+v", st)
	}
	// 带 override：派生字段生效 + IsCustom=true。
	ws := sandbox.ModeDangerFullAccess
	allow := approval.PolicyAllowAll
	st = presets.Derive(presets.PresetCustom, &ws, &allow)
	if !st.IsCustom {
		t.Fatal("custom + override 应为 IsCustom=true")
	}
	if st.SandboxMode != sandbox.ModeDangerFullAccess || st.ApprovalPolicy != approval.PolicyAllowAll {
		t.Fatalf("override 派生字段错误: %+v", st)
	}
}