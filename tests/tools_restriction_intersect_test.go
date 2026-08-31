// Package tests 的工具限制（M24）验收测试。
//
// 覆盖：
//   - host 层 deny + scope 层 exempt → 工具恢复可用
//   - 两层相交（host deny + scope deny）→ 拒绝
//   - nearest-scope-wins：最近层优先
//   - Filter 应用于 Preset 隐藏工具 / Subagent 限制子能力
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/tools"
)

// mkTool 构造一个可执行工具定义。
func mkTool(name string) *tools.Tool {
	return &tools.Tool{Name: name, Execute: func(ctx context.Context, input map[string]any) (any, error) {
		return "ok", nil
	}}
}

// TestToolRestrictionHostDenyScopeExempt 验证 host deny + scope exempt → 工具恢复可用。
func TestToolRestrictionHostDenyScopeExempt(t *testing.T) {
	rs := tools.NewRestrictionSet()
	// host 层 deny bash/rm。
	rs.Host(tools.RestrictionDeny("bash", "rm"))
	// scope 层 exempt rm。
	rs.Scope("session:main", tools.RestrictionAllow("rm"))

	if !rs.Allowed("rm") {
		t.Fatal("scope exempt 应覆盖 host deny, rm 应可用")
	}
	if rs.Allowed("bash") {
		t.Fatal("bash 无 scope exempt, 应仍被 host deny 拒绝")
	}
	if !rs.Allowed("goal_list") {
		t.Fatal("未提及的工具应默认放行")
	}
}

// TestToolRestrictionIntersect 验证两层相交（host deny + scope deny）→ 拒绝。
func TestToolRestrictionIntersect(t *testing.T) {
	rs := tools.NewRestrictionSet()
	rs.Host(tools.RestrictionDeny("rm", "bash"))
	rs.Scope("session:child", tools.RestrictionDeny("bash")) // 父限子：子层也 deny bash

	if rs.Allowed("bash") {
		t.Fatal("两层均 deny bash, 应拒绝")
	}
	if !rs.Allowed("ls") {
		t.Fatal("未提及的 ls 应放行")
	}
}

// TestToolRestrictionNearestScopeWins 验证最近层优先（子层重新拒绝父层放行的工具）。
func TestToolRestrictionNearestScopeWins(t *testing.T) {
	rs := tools.NewRestrictionSet()
	rs.Host(tools.RestrictionAllow("web_fetch")) // host 放行
	rs.Scope("session:a", tools.RestrictionDeny("web_fetch")) // 会话层拒绝

	if rs.Allowed("web_fetch") {
		t.Fatal("最近会话层 deny 应覆盖 host allow")
	}
}

// TestToolRestrictionFilterHideForPreset 验证 Filter 用于 Preset 隐藏工具。
func TestToolRestrictionFilterHideForPreset(t *testing.T) {
	defs := []*tools.Tool{mkTool("bash"), mkTool("rm"), mkTool("ls"), mkTool("goal_list")}
	rs := tools.NewRestrictionSet()
	rs.Host(tools.RestrictionDeny("bash", "rm")) // 预设隐藏危险工具

	kept := rs.Filter(defs)
	if len(kept) != 2 {
		t.Fatalf("应保留 2 个工具(lts/goal), 实际 %v", names(kept))
	}
	if kept[0].Name != "ls" || kept[1].Name != "goal_list" {
		t.Fatalf("保留顺序/内容错误: %v", names(kept))
	}
}

// TestToolRestrictionSubagentParentLimitsChild 验证父限子能力（Subagent 场景）。
func TestToolRestrictionSubagentParentLimitsChild(t *testing.T) {
	defs := []*tools.Tool{mkTool("bash"), mkTool("fs_edit"), mkTool("goal_list")}
	// 父代理：子代理默认无 bash/fs_edit。
	rs := tools.NewRestrictionSet()
	rs.Host(tools.RestrictionDeny("bash", "fs_edit"))
	// 子代理在更近层只放行 goal_list（父限子）。
	rs.Scope("subagent:child", tools.RestrictionAllow("goal_list"))

	kept := rs.Filter(defs)
	if len(kept) != 1 || kept[0].Name != "goal_list" {
		t.Fatalf("子代理应仅保留 goal_list, 实际 %v", names(kept))
	}
}

// names 提取工具名列表。
func names(ts []*tools.Tool) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		if t != nil {
			out = append(out, t.Name)
		}
	}
	return out
}