// Package presets 提供 Agent Preset 与 Permission Presets 组合旋钮。
//
// 对齐上游：packages/interaction/permission-presets
//
// 本文件对应任务 M28：Permission Presets 组合旋钮。
//
// 设计要点：
//   - PermissionPreset 是 {sandboxMode, approvalPolicy} 的组合元组；
//   - 内置 4 个预设：safe / danger / review / custom（custom 为派生/自定义基线，
//     其 sandbox 与 approval 由用户 override 派生，其余取基线）；
//   - 提供 presetName → approvalPinkPolicy 的映射注入函数，供 approval.Service 使用，
//     从而避免 approval ↔ presets 的循环导入；
//   - 派生状态（DerivedState）暴露给 UI/SDK 读，反映预设 + 用户 override 后的最终形态。
package presets

import (
	"sort"

	"github.com/JopenChen/dsh-go/pkg/approval"
	"github.com/JopenChen/dsh-go/pkg/sandbox"
)

// PermissionPreset 是权限预设的组合元组。
type PermissionPreset struct {
	// Name 预设名。
	Name string `json:"name"`
	// SandboxMode 预设对应的沙箱模式。
	SandboxMode sandbox.SandboxMode `json:"sandboxMode"`
	// ApprovalPolicy 预设对应的审批策略。
	ApprovalPolicy approval.Policy `json:"approvalPolicy"`
}

// Preset 命名常量。
const (
	PresetSafe   = "safe"
	PresetDanger = "danger"
	PresetReview = "review"
	PresetCustom = "custom"
)

// presetTable 是内置预设定义（与 pkg/session 的 presetSandbox/presetApproval 对齐）。
var presetTable = map[string]PermissionPreset{
	PresetSafe: {
		Name:           PresetSafe,
		SandboxMode:    sandbox.ModeReadOnly,
		ApprovalPolicy: approval.PolicyAskDangerous,
	},
	PresetDanger: {
		Name:           PresetDanger,
		SandboxMode:    sandbox.ModeDangerFullAccess,
		ApprovalPolicy: approval.PolicyAllowAll,
	},
	PresetReview: {
		Name:           PresetReview,
		SandboxMode:    sandbox.ModeReadOnly,
		ApprovalPolicy: approval.PolicyAskDangerousEdit,
	},
	// custom 为派生基线：默认 workspace-write + ask-dangerous，可由用户 override。
	PresetCustom: {
		Name:           PresetCustom,
		SandboxMode:    sandbox.ModeWorkspaceWrite,
		ApprovalPolicy: approval.PolicyAskDangerous,
	},
}

// PresetNames 返回内置预设名（排序稳定，字典序）。
func PresetNames() []string {
	names := make([]string, 0, len(presetTable))
	for n := range presetTable {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Resolve 按预设名解析组合元组；未知预设返回 ok=false（调用方回落 custom）。
func Resolve(name string) (PermissionPreset, bool) {
	p, ok := presetTable[name]
	if !ok {
		// 未知预设 → 回落 custom 派生基线，ok=true 但带 (custom preset)
		return presetTable[PresetCustom], true
	}
	return p, true
}

// ApprovalMapper 返回 preset 名 → 审批策略的映射函数（供 approval.Service 注入）。
// 未知预设回落 custom 的审批策略（ask-dangerous）。
func ApprovalMapper() func(string) (approval.Policy, bool) {
	return func(name string) (approval.Policy, bool) {
		p, _ := Resolve(name)
		return p.ApprovalPolicy, true
	}
}

// SandboxMapper 返回 preset 名 → 沙箱模式的映射函数。
func SandboxMapper() func(string) (sandbox.SandboxMode, bool) {
	return func(name string) (sandbox.SandboxMode, bool) {
		p, _ := Resolve(name)
		return p.SandboxMode, true
	}
}

// DerivedState 是暴露给 UI/SDK 的派生权限状态（预设 + override 之后）。
type DerivedState struct {
	// ActivePreset 当前生效的预设名。
	ActivePreset string `json:"activePreset"`
	// SandboxMode 派生后的沙箱模式（含自定义 override）。
	SandboxMode sandbox.SandboxMode `json:"sandboxMode"`
	// ApprovalPolicy 派生后的审批策略。
	ApprovalPolicy approval.Policy `json:"approvalPolicy"`
	// IsCustom true 表示正使用自定义（非内置）派生状态。
	IsCustom bool `json:"isCustom"`
}

// Derive 基于指定预设 + 可选 override 派生最终状态。
//   - sandboxOverride / approvalOverride 非 nil 时覆盖对应字段（custom 派生）；
//   - 返回 IsCustom 提示 UI/SDK 是否为自定义形态。
func Derive(presetName string, sandboxOverride *sandbox.SandboxMode, approvalOverride *approval.Policy) DerivedState {
	base, _ := Resolve(presetName)
	isCustom := sandboxOverride != nil || approvalOverride != nil
	if presetName != PresetCustom {
		isCustom = false
	}
	if sandboxOverride != nil {
		base.SandboxMode = *sandboxOverride
	}
	if approvalOverride != nil {
		base.ApprovalPolicy = *approvalOverride
	}
	return DerivedState{
		ActivePreset:   base.Name,
		SandboxMode:    base.SandboxMode,
		ApprovalPolicy: base.ApprovalPolicy,
		IsCustom:       isCustom,
	}
}