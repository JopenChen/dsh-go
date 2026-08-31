// 本文件对应任务 M10：PromptContext 动态注册与快照。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/sysprompt"
)

// TestPromptContextDynamic 验证动态添加/移除 PromptContext 下一轮 assemble 生效/失效。
func TestPromptContextDynamic(t *testing.T) {
	reg := sysprompt.NewContextRegistry()

	// 注册两个上下文
	reg.Register("runtime_ctx", 300, "cwd=/workspace")
	reg.Register("plan_policy", 500, "先计划再执行")

	text, hash := reg.Compute()
	if text == "" {
		t.Fatal("注册后应能 compute 出内容")
	}
	if hash == "" {
		t.Fatal("非空快照应有哈希")
	}
	// 确定性：再次 compute 哈希一致
	if _, h2 := reg.Compute(); h2 != hash {
		t.Fatal("hash 应稳定")
	}

	// 移除一个上下文 → 下一轮 compute 变化
	reg.Unregister("plan_policy")
	text2, hash2 := reg.Compute()
	if text2 == text || hash2 == hash {
		t.Fatal("移除后 compute 应变化")
	}
}

// TestPromptContextChangeDetection 验证内容变化时 hash 变化（change-only 基础）。
func TestPromptContextChangeDetection(t *testing.T) {
	reg := sysprompt.NewContextRegistry()
	changed := reg.Register("runtime_ctx", 300, "v1")
	if !changed {
		t.Fatal("首次注册应视为变化")
	}
	if !reg.Register("runtime_ctx", 300, "v2") {
		t.Fatal("内容变化后 register 应返回 changed=true")
	}
	if reg.Register("runtime_ctx", 300, "v2") {
		t.Fatal("内容未变时 register 应返回 changed=false（change-only）")
	}
}

// TestGoalRoundContext 验证 goal.active 注入、complete 后停止。
func TestGoalRoundContext(t *testing.T) {
	active := sysprompt.GoalRoundContext{Active: true, Round: 2, GoalDesc: "实现任务"}
	if active.Render() == "" {
		t.Fatal("active 时应渲染续轮提示")
	}

	// goal.complete 后不再注入
	inactive := sysprompt.GoalRoundContext{Active: false, Round: 2, GoalDesc: "实现任务"}
	if inactive.Render() != "" {
		t.Fatal("goal.complete 后应停止注入")
	}
}

// TestPromptContextSnapshotPersist 验证快照可被完整保留（compaction 后可重建）。
func TestPromptContextSnapshotPersist(t *testing.T) {
	reg := sysprompt.NewContextRegistry()
	reg.Register("runtime_ctx", 300, "snapshot-value")

	// 取快照（模拟 compaction 后保留）
	snap := reg.Snapshot()
	if snap["runtime_ctx"] != "snapshot-value" {
		t.Fatalf("快照内容异常: %v", snap)
	}
}