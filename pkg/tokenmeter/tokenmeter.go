// Package tokenmeter 提供 Token 计量与预算（Token Meter）。
//
// 对齐上游：packages/util/token-meter
//
// 本文件对应任务 M31：Token Meter 计量与预算。
//
// 设计要点：
//   - 计量每次模型请求的 prompt/completion token，按会话累计；
//   - session-level budget cap：一旦累计超过预算上限，在下一轮请求开始前以 budget deny
//     拒绝，且不产生真实的 LLM 调用（拦在请求层）；
//   - 表面节点定价：可对派生节点（如 assistant/message、tool/result 快照）按 token 计费，
//     供 UI/SDK 展示成本。
package tokenmeter

import (
	"fmt"
	"sync"
)

// TokenUsage 是一次模型请求的 token 用量。
type TokenUsage struct {
	// PromptTokens 输入 token。
	PromptTokens int `json:"promptTokens"`
	// CompletionTokens 输出 token。
	CompletionTokens int `json:"completionTokens"`
}

// Total 返回本次请求总 token。
func (u TokenUsage) Total() int {
	return u.PromptTokens + u.CompletionTokens
}

// RequestRecord 是一次计量记录。
type RequestRecord struct {
	Seq  int        `json:"seq"`
	Usage TokenUsage `json:"usage"`
}

// Meter 是会话级 token 计量器。
type Meter struct {
	mu        sync.Mutex
	budget    int          // 会话级预算上限；<=0 表示不限
	seq       int          // 请求序号
	records   []RequestRecord
	totalProm int
	totalComp int
	denied    bool // 是否已因超预算而进入 budget-deny 状态
}

// New 创建计量器。budget<=0 表示不限制。
func New(budget int) *Meter {
	return &Meter{budget: budget}
}

// Budget 返回预算上限。
func (m *Meter) Budget() int {
	return m.budget
}

// Record 记录一次模型请求的 token 用量，并更新 deny 状态。
func (m *Meter) Record(u TokenUsage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	m.totalProm += u.PromptTokens
	m.totalComp += u.CompletionTokens
	m.records = append(m.records, RequestRecord{Seq: m.seq, Usage: u})
	if m.budget > 0 && m.totalProm+m.totalComp >= m.budget {
		m.denied = true
	}
}

// HasBudget 是否还有预算签发下一轮请求（未被 deny）。
// 达到上限后返回 false；调用方据此在请求前拒绝，避免真实 LLM 调用。
func (m *Meter) HasBudget() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.denied
}

// BudgetDenied 是否已超预算被拒绝。
func (m *Meter) BudgetDenied() bool {
	return !m.HasBudget()
}

// Totals 返回累计 prompt/completion token 与总数。
func (m *Meter) Totals() (prompt, completion, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.totalProm, m.totalComp, m.totalProm + m.totalComp
}

// Records 返回全部计量记录快照。
func (m *Meter) Records() []RequestRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]RequestRecord, len(m.records))
	copy(out, m.records)
	return out
}

// BudgetDeniedError 是预算拒绝错误（稳定文案，便于上层审计）。
const BudgetDeniedError = "token-meter: session budget exceeded; request rejected before LLM call"

// Check 在发起新一轮请求前检查预算；超限返回错误（调用方应直接拒绝，不触发真实 LLM 调用）。
func (m *Meter) Check() error {
	if m.BudgetDenied() {
		return fmt.Errorf("%s (prompt+completion >= %d)", BudgetDeniedError, m.budget)
	}
	return nil
}

// SurfacePricing 是表面节点（派生节点）的定价映射。
// 每种节点类型给出每节点的 token 权重；成本 = weight * count。
type SurfacePricing map[string]int

var defaultSurfacePricing = SurfacePricing{
	"assistant/message": 4,
	"tool/result":       2,
	"user/message":      1,
}

// CostOf 计算一批表面节点的估算 token 成本。未知节点类型 → 无成本。
func (p SurfacePricing) CostOf(entries []SurfaceNode) int {
	total := 0
	for _, e := range entries {
		if w, ok := p[e.Kind]; ok {
			total += w * e.Count
		}
	}
	return total
}

// SurfaceNode 描述某类派生节点出现的次数。
type SurfaceNode struct {
	// Kind 节点类型（如 "assistant/message"）。
	Kind string
	// Count 出现次数。
	Count int
}