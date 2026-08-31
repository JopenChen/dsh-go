// Package tests 的 N05（D4 纪律）验收测试。
//
// 覆盖：
//   - RuntimeContext.Compute() 在状态变化时 hash 变化、无变化时稳定
//   - GoalRoundContext 在 goal.complete 后停止注入
//   - change-only 注入：不修改 system 前缀，仅 user-msg 追加，且只注入 1 次
//   - compaction 后最后一次 context snapshot 仍可恢复（CompactPreserve）
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/sysprompt"
)

// TestN05ContextHashStableAndChanges 验证状态变化 hash 变、稳定 hash 稳。
func TestN05ContextHashStableAndChanges(t *testing.T) {
	reg := sysprompt.NewContextRegistry()
	reg.Register("plan", 1, "plan mode: on")
	text1, hash1 := reg.Compute()
	text1b, hash1b := reg.Compute()
	if text1 != text1b || hash1 != hash1b {
		t.Fatal("无变化时 hash 应稳定")
	}
	// 状态变化（plan mode 切换）→ hash 变。
	reg.Register("plan", 1, "plan mode: off")
	_, hash2 := reg.Compute()
	if hash2 == hash1 {
		t.Fatal("状态变化后 hash 应改变")
	}
	_ = text1
}

// TestN05ChangeOnlyInject 验证 change-only：不变不注入，变则注入，且不重复。
func TestN05ChangeOnlyInject(t *testing.T) {
	reg := sysprompt.NewContextRegistry()
	reg.Register("goal", 1, "goal: ship it")
	in := sysprompt.NewChangeOnlyInjector(reg, "")
	// 首次注入。
	if _, injected := in.MightInject(); !injected {
		t.Fatal("首次应注入")
	}
	// 不变 → 不重复注入。
	for i := 0; i < 50; i++ {
		if _, injected := in.MightInject(); injected {
			t.Fatalf("第 %d 轮不变不应注入", i)
		}
	}
	if in.InjectCount() != 1 {
		t.Fatalf("应只注入 1 次, 实际 %d", in.InjectCount())
	}
	// 状态变化 → 注入。
	reg.Register("goal", 1, "goal: ship it now")
	if _, injected := in.MightInject(); !injected {
		t.Fatal("状态变化后应注入")
	}
	if in.InjectCount() != 2 {
		t.Fatalf("应有两次注入, 实际 %d", in.InjectCount())
	}
}

// TestN05GoalRoundStopsOnComplete 验证 goal.complete 后 GoalRoundContext 停止注入。
func TestN05GoalRoundStopsOnComplete(t *testing.T) {
	active := sysprompt.GoalRoundContext{Active: true, Round: 3, GoalDesc: "build"}
	if active.Render() == "" {
		t.Fatal("active 时应渲染续轮提示")
	}
	complete := sysprompt.GoalRoundContext{Active: false, Round: 3, GoalDesc: "build"}
	if complete.Render() != "" {
		t.Fatal("complete 后应停止注入（空）")
	}
}

// TestN05CompactionPreserveSnapshot 验证 compaction 后最后一次 snapshot 保留。
func TestN05CompactionPreserveSnapshot(t *testing.T) {
	reg := sysprompt.NewContextRegistry()
	reg.Register("ctx1", 1, "snapshot-A")
	cp := &sysprompt.CompactPreserve{}

	// 记录快照 A（将被压缩掉）。
	textA, hashA := reg.Compute()
	cp.Record(textA, hashA)
	// 状态变化 → 新快照 B。
	reg.Register("ctx2", 2, "snapshot-B")
	textB, hashB := reg.Compute()
	cp.Record(textB, hashB)

	// compaction 丢弃老 A，仅保留最新 B。
	latest, hash, ok := cp.Latest()
	if !ok || hash != hashB || latest != textB {
		t.Fatalf("compaction 后应保留最新 snapshot B: ok=%v", ok)
	}
	if hash == hashA {
		t.Fatal("不应保留旧 snapshot A")
	}
}

// TestN05PersistedHashRecovery 验证持久化 hash 可恢复（跨轮不重复注入）。
func TestN05PersistedHashRecovery(t *testing.T) {
	reg := sysprompt.NewContextRegistry()
	reg.Register("p1", 1, "stable")
	reg.Register("p2", 2, "settings: x")
	// 第一轮注入并持久化 hash。
	in1 := sysprompt.NewChangeOnlyInjector(reg, "")
	_, _ = in1.MightInject()
	persisted := in1.LastHash()

	// 新一轮用持久化 hash 恢复 → 状态未变不重复注入。
	in2 := sysprompt.NewChangeOnlyInjector(reg, persisted)
	if _, injected := in2.MightInject(); injected {
		t.Fatal("持久化恢复后状态未变不应重复注入")
	}
	if in2.InjectCount() != 0 {
		t.Fatalf("应 0 次注入, 实际 %d", in2.InjectCount())
	}
}