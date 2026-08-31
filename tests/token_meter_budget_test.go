// Package tests 的 Token Meter（M31）验收测试。
//
// 覆盖：
//   - 每次请求计量 prompt/completion token，按会话累计
//   - 预算 10k tokens → 达到上限后下一轮请求被 budget deny 拒绝且不产生真实 LLM 调用
//   - 表面节点定价估算
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/tokenmeter"
)

// TestTokenMeterAccumulate 验证每次请求计量与累计。
func TestTokenMeterAccumulate(t *testing.T) {
	m := tokenmeter.New(100000)
	m.Record(tokenmeter.TokenUsage{PromptTokens: 1000, CompletionTokens: 500})
	m.Record(tokenmeter.TokenUsage{PromptTokens: 2000, CompletionTokens: 300})

	prompt, comp, total := m.Totals()
	if prompt != 3000 || comp != 800 || total != 3800 {
		t.Fatalf("累计异常: prompt=%d comp=%d total=%d", prompt, comp, total)
	}
	if len(m.Records()) != 2 {
		t.Fatalf("应有 2 条计量记录, 实际 %d", len(m.Records()))
	}
	if !m.HasBudget() {
		t.Fatal("未超预算应 HasBudget()=true")
	}
}

// TestTokenMeterBudgetDeny 验证预算 10k → 达到上限后下一轮请求被拒绝，不产生真实 LLM 调用。
func TestTokenMeterBudgetDeny(t *testing.T) {
	const budget = 10000
	m := tokenmeter.New(budget)

	// 逐步记录直至接近预算。
	llmCalled := 0
	for i := 0; i < 20; i++ {
		// 模拟请求层：先 Check 预算，再打 LLM。
		if err := m.Check(); err != nil {
			break // 拒绝，不产生真实调用
		}
		llmCalled++
		m.Record(tokenmeter.TokenUsage{PromptTokens: 600, CompletionTokens: 400}) // 每轮 1000
	}

	if !m.BudgetDenied() {
		t.Fatal("累计达预算后应 budget-deny")
	}
	// 20 轮每轮 1000 = 20000，但 Check 在累计 >=10000 后拒绝。第 10.0 轮后累计 10000 → deny。
	// 前 9 轮 Check 通过（9000），第 10 轮 Check 时累计 1000*9=9000<10000 通过记录到 10000，
	// 第 11 轮 Check 时已有 10000+delta → deny。因此 llmCalled 应为 10。
	if llmCalled != 10 {
		t.Fatalf("应恰好产生 10 次真实 LLM 调用后拒绝, 实际 %d", llmCalled)
	}
	if err := m.Check(); err == nil {
		t.Fatal("超预算后 Check 应返回 budget deny 错误")
	}
}

// TestTokenMeterNoBudget 验证 budget<=0 不限制。
func TestTokenMeterNoBudget(t *testing.T) {
	m := tokenmeter.New(0) // 不限制
	for i := 0; i < 30; i++ {
		m.Record(tokenmeter.TokenUsage{PromptTokens: 1000, CompletionTokens: 1000})
	}
	if m.BudgetDenied() {
		t.Fatal("无预算上限不应 deny")
	}
	if _, _, total := m.Totals(); total != 30*2000 {
		t.Fatalf("无限制应累计全部, 实际 %d", total)
	}
}

// TestTokenMeterSurfacePricing 验证表面节点定价估算。
func TestTokenMeterSurfacePricing(t *testing.T) {
	p := tokenmeter.SurfacePricing{"assistant/message": 4, "tool/result": 2, "user/message": 1}
	cost := p.CostOf([]tokenmeter.SurfaceNode{
		{Kind: "assistant/message", Count: 3},
		{Kind: "tool/result", Count: 5},
		{Kind: "user/message", Count: 2},
	})
	// 3*4 + 5*2 + 2*1 = 12 + 10 + 2 = 24
	if cost != 24 {
		t.Fatalf("表面定价估算错误: 期望 24, 实际 %d", cost)
	}
}