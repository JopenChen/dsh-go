// Package telemetry 提供缓存命中率监控指标与告警协同。
//
// 对应任务 N09：Grafana 缓存命中率看板 + OTel 探针。
//
// 本实现提供 4 个"OTel 语义"的指标（HashAgg / Counter / Histogram 的轻量占位，
// 便于无 collector 环境测试；接入真实 OTel SDK 时替换为对应 Instrument 即可）：
//   - dsh.cache.hit_ratio      (Histogram, 按 session/preset/turn 维度)
//   - dsh.cache.hit_tokens     (Counter)
//   - dsh.cache.miss_tokens    (Counter)
//   - dsh.cache.broken_count   (Counter，检测到可缓存破窗模式 +1)
package telemetry

import "sync"

// CacheMetrics 是缓存命中率指标聚合器。
type CacheMetrics struct {
	mu             sync.Mutex
	HitRatios      []float64 // 命中率历史（Histogram 样本）
	HitTokensTotal int64
	MissTokensTotal int64
	BrokenCount    int64

	// Session 维度标签（当前局）。
	SessionID string
	Preset    string
	TurnSeq   int
}

// NewCacheMetrics 创建指标聚合器。
func NewCacheMetrics() *CacheMetrics {
	return &CacheMetrics{}
}

// Record 记录一次请求的缓存指标。
func (m *CacheMetrics) Record(hit, miss int, ratio float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.HitRatios = append(m.HitRatios, ratio)
	m.HitTokensTotal += int64(hit)
	m.MissTokensTotal += int64(miss)
}

// MarkBroken 标记一次破窗事件（broken_count +1）。
func (m *CacheMetrics) MarkBroken() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BrokenCount++
}

// HistogramP50 计算命中率样本的 P50（分位数），用于 Grafana 告警规则 hit_ratio_p50。
func (m *CacheMetrics) HistogramP50() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.HitRatios) == 0 {
		return 0
	}
	sorted := append([]float64(nil), m.HitRatios...)
	// 简单插入排序。
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1] > sorted[j]; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	idx := len(sorted) / 2
	return sorted[idx]
}

// MetricsNames 返回本实现导出的 4 个指标名（供 OTel 对接/看板引用）。
func MetricsNames() []string {
	return []string{
		"dsh.cache.hit_ratio",
		"dsh.cache.hit_tokens",
		"dsh.cache.miss_tokens",
		"dsh.cache.broken_count",
	}
}