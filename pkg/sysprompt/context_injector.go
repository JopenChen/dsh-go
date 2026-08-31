// 本文件对应任务 N05（D4 纪律）：PromptContext change-only 注入 + Compaction 保留 snapshot。
//
// 对齐上游：packages/core/system-prompt/context-prompt + compaction
//
// 设计要点：
//   - 复用 M10 ContextRegistry 的 Compute()（状态变化时 hash 变 / 无变化时稳定）；
//   - ChangeOnlyInjector：持久化"已注入的最后 hash"，仅在 hash 变化时注入为新 user-msg，
//     **不修改** system prompt（走 user-msg 追加）；
//   - 状态变更（plan mode 切换 / goal 设置 / approval policy 变更）都走 user-msg 追加；
//   - CompactPreserve：compaction 后仍保留最后一次 context snapshot，供 derived messages 重建。
package sysprompt

// ============================================================================
// 变更注入器
// ============================================================================

// ChangeOnlyInjector 是 PromptContext 的 change-only 注入器。
// 记录最近一次已注入的 hash；Compute() 的 hash 未变则不注入。
type ChangeOnlyInjector struct {
	reg      *ContextRegistry
	lastHash string
	injects  int
}

// NewChangeOnlyInjector 创建变更注入器（可传入持久化的 lastHash 恢复跨轮状态）。
func NewChangeOnlyInjector(reg *ContextRegistry, persistedHash string) *ChangeOnlyInjector {
	return &ChangeOnlyInjector{reg: reg, lastHash: persistedHash}
}

// MightInject 对比 Compute() 哈希：变化时注入并返回文本，否则不注入。
func (in *ChangeOnlyInjector) MightInject() (text string, injected bool) {
	text, hash := in.reg.Compute()
	if hash == in.lastHash {
		return "", false
	}
	in.lastHash = hash
	in.injects++
	// 不修改 system prompt：注入内容以 user-msg（text）交给上层，仅此一次。
	return text, true
}

// InjectCount 返回累计注入次数。
func (in *ChangeOnlyInjector) InjectCount() int { return in.injects }

// LastHash 返回已注入的 last hash（供持久化恢复）。
func (in *ChangeOnlyInjector) LastHash() string { return in.lastHash }

// ============================================================================
// Compaction 保留 snapshot
// ============================================================================

// contextSnapshot 是某时刻的上下文快照。
type contextSnapshot struct {
	// Text 拼接文本。
	Text string
	// Hash 稳定哈希。
	Hash string
}

// CompactPreserve 是"保留最后一次 context snapshot"的 compaction 辅助。
// compaction 删除老上下文节点后，仍可通过 last 恢复最新的 user-msg 追加。
type CompactPreserve struct {
	last contextSnapshot
}

// Record 记录一次计算出的 snapshot（覆盖为最新）。
func (c *CompactPreserve) Record(text, hash string) {
	c.last = contextSnapshot{Text: text, Hash: hash}
}

// Latest 返回保留的最后一次 snapshot（compaction 后仍可用）。
func (c *CompactPreserve) Latest() (text, hash string, ok bool) {
	if c.last.Hash == "" {
		return "", "", false
	}
	return c.last.Text, c.last.Hash, true
}