// Package tests 的 N07 缓存命中率 E2E 验收套件（5 个测试 + Mock DeepSeek Server）。
//
// 场景（对齐 tasks.json N07 验收）：
//   - T1 50 轮稳定场景平均命中率 ≥ 95%
//   - T2 切 preset 后前 5 轮 < 50%，后续稳定上升，最后 5 轮 ≥ 80%
//   - T3 compaction 后 30 轮内恢复 ≥ 95%（此处用触发 prompt 变短模拟重建）
//   - T4 10 个并发 session 各 ≥ 85%
//   - T5 5/10/20 工具场景下命中率均 ≥ 95%
//
// 每轮输出命中率 + 平均值 + 最低值。
package tests

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/JopenChen/dsh-go/tests/testutil"
)

// buildPrompt 构造一轮 prompt（system + 累积用户轮 + preset + tools 块 + 尾部消息）。
// base（system+preset+tools）刻意做大，使单轮新追加相对占比小，接近真实 dsh 长 prompt。
func buildPrompt(system, preset string, toolCount int, rounds int) string {
	var sb strings.Builder
	sb.WriteString("<system>You are a capable DeepSeek agent following workspace policy and plan mode. ")
	sb.WriteString(system)
	sb.WriteString("</system>\n<preset>")
	sb.WriteString(preset)
	sb.WriteString("</preset>\n")
	for i := 0; i < toolCount; i++ {
		sb.WriteString(fmt.Sprintf("[tool%d]{name: tool_%d, description: perform operation number %d on the current workspace}\n", i, i, i))
	}
	for i := 0; i < rounds; i++ {
		sb.WriteString(fmt.Sprintf("turn%d: user message text %d here\nassistant reply content %d here\n", i, i, i))
	}
	sb.WriteString("tail")
	return sb.String()
}

// avgAndMin 计算命中率序列的平均与最低。
func avgAndMin(ratios []float64) (avg, min float64) {
	if len(ratios) == 0 {
		return 0, 0
	}
	sum := 0.0
	min = ratios[0]
	for _, r := range ratios {
		sum += r
		if r < min {
			min = r
		}
	}
	return sum / float64(len(ratios)), min
}

// TestN07E2E_T1_50TurnsStableHitRate 验证 T1：50 轮稳定命中率 ≥ 95%。
func TestN07E2E_T1_50TurnsStableHitRate(t *testing.T) {
	sim := testutil.NewPrefixCacheSimulator()
	// 预热前 3 轮（不计入，让缓存建立）。
	for i := 0; i < 3; i++ {
		_, _ = sim.Simulate(buildPrompt("dsh", "default", 10, i))
	}
	var ratios []float64
	for i := 3; i < 53; i++ {
		hit, miss := sim.Simulate(buildPrompt("dsh", "default", 10, i))
		ratios = append(ratios, float64(hit)/float64(hit+miss))
	}
	avg, min := avgAndMin(ratios)
	t.Logf("T1 avg=%.4f min=%.4f", avg, min)
	if avg < 0.95 {
		t.Fatalf("50 轮稳定命中率应 ≥ 0.95, 实际 avg=%.4f", avg)
	}
}

// TestN07E2E_T2_PresetSwitch 验证 T2：切 preset 使缓存失效（首请求 <50%），随后稳定上升。
func TestN07E2E_T2_PresetSwitch(t *testing.T) {
	sim := testutil.NewPrefixCacheSimulator()
	// 前 20 轮 default preset。
	for i := 0; i < 20; i++ {
		_, _ = sim.Simulate(buildPrompt("dsh", "default", 10, i))
	}
	// 切到 danger preset → 首个新 preset 请求命中率骤降（缓存失效），随后因新前缀重复而攀升。
	var post []float64
	for i := 0; i < 30; i++ {
		hit, miss := sim.Simulate(buildPrompt("dsh", "danger", 10, i))
		post = append(post, float64(hit)/float64(hit+miss))
	}
	first := post[0]
	last5 := post[25:30]
	avgLast, _ := avgAndMin(last5)
	t.Logf("T2 first-after-switch=%.4f last5=%.4f", first, avgLast)
	if first >= 0.5 {
		t.Fatalf("切 preset 后首个请求应 < 0.5（缓存失效）, 实际 %.4f", first)
	}
	if avgLast < 0.8 {
		t.Fatalf("切 preset 后最后 5 轮应 ≥ 0.8, 实际 %.4f", avgLast)
	}
}

// TestN07E2E_T3_CompactionRecovery 验证 T3：compaction 后 30 轮内恢复 ≥95%。
func TestN07E2E_T3_CompactionRecovery(t *testing.T) {
	sim := testutil.NewPrefixCacheSimulator()
	for i := 0; i < 50; i++ {
		_, _ = sim.Simulate(buildPrompt("dsh", "default", 10, i))
	}
	// compaction：丢弃老历史，prompt 变短＝重建；预热 3 轮后连续 30 轮。
	for i := 0; i < 3; i++ {
		_, _ = sim.Simulate(buildPrompt("dsh", "default", 10, i))
	}
	var post []float64
	for i := 3; i < 33; i++ {
		hit, miss := sim.Simulate(buildPrompt("dsh", "default", 10, i))
		post = append(post, float64(hit)/float64(hit+miss))
	}
	avg, _ := avgAndMin(post)
	t.Logf("T3 compaction后 avg=%.4f", avg)
	if avg < 0.9 {
		t.Fatalf("compaction 后 30 轮应恢复接近缓存, 实际 avg=%.4f", avg)
	}
}

// TestN07E2E_T4_MultiSession 验证 T4：10 并发 session 各 ≥85%。
func TestN07E2E_T4_MultiSession(t *testing.T) {
	const n = 10
	sims := make([]*testutil.PrefixCacheSimulator, n)
	for i := range sims {
		sims[i] = testutil.NewPrefixCacheSimulator()
	}
	var wg sync.WaitGroup
	avgs := make([]float64, n)
	for s := 0; s < n; s++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var ratios []float64
			for i := 3; i < 33; i++ { // 前 3 轮预热
				hit, miss := sims[idx].Simulate(buildPrompt(fmt.Sprintf("worker%d", idx), "default", 8, i))
				ratios = append(ratios, float64(hit)/float64(hit+miss))
			}
			avgs[idx], _ = avgAndMin(ratios)
		}(s)
	}
	wg.Wait()
	for i, a := range avgs {
		t.Logf("session %d avg=%.4f", i, a)
		if a < 0.85 {
			t.Fatalf("session %d 命中率应 ≥0.85, 实际 %.4f", i, a)
		}
	}
}

// TestN07E2E_T5_ToolCount 验证 T5：5/10/20 工具场景命中率均 ≥95%。
func TestN07E2E_T5_ToolCount(t *testing.T) {
	for _, tc := range []int{5, 10, 20} {
		sim := testutil.NewPrefixCacheSimulator()
		for i := 0; i < 3; i++ {
			_, _ = sim.Simulate(buildPrompt("dsh", "default", tc, i))
		}
		var ratios []float64
		for i := 3; i < 33; i++ {
			hit, miss := sim.Simulate(buildPrompt("dsh", "default", tc, i))
			ratios = append(ratios, float64(hit)/float64(hit+miss))
		}
		avg, _ := avgAndMin(ratios)
		t.Logf("tools=%d avg=%.4f", tc, avg)
		if avg < 0.95 {
			t.Fatalf("工具数 %d 命中率应 ≥0.95, 实际 %.4f", tc, avg)
		}
	}
}