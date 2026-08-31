// 本文件对应任务 N01：DeepSeek Prefix Cache 探针埋点（阶段 0 前置基础设施）。
//
// 对齐上游：DeepSeek API docs（KV cache）+ 本地衍生。
//
// 设计要点：
//   - CacheStats 聚合每请求的 prompt_cache_hit_tokens / prompt_cache_miss_tokens；
//   - ComputeHitRatio() 计算命中率 = hit/(hit+miss)，在总量为 0 或全命中/全未命中时
//     都能给出确定值（防 0/0 → NaN）；
//   - CacheLogLine 生成每次 LLM 请求日志行所需字段 cache.hit_ratio=...，
//     供观测/告警（N08）与 N 簇全部验收作为数据基线。
package llm

import (
	"fmt"
)

// CacheStats 是一次或累计的 prefix cache 用量聚合。
type CacheStats struct {
	// HitTokens 命中缓存的 prompt token 数。
	HitTokens int `json:"hitTokens"`
	// MissTokens 未命中缓存的 prompt token 数。
	MissTokens int `json:"missTokens"`
}

// Add 累加一次用量。
func (c *CacheStats) Add(hit, miss int) {
	c.HitTokens += hit
	c.MissTokens += miss
}

// Total 返回总 prompt token（hit + miss）。
func (c *CacheStats) Total() int {
	return c.HitTokens + c.MissTokens
}

// ComputeHitRatio 计算命中率，防 0/0×NaN：
//   - 总量为 0 → 0.0；全命中 → 1.0；全未命中 → 0.0；部分命中 → hit/total。
func (c *CacheStats) ComputeHitRatio() float64 {
	total := c.Total()
	if total <= 0 {
		return 0.0
	}
	return float64(c.HitTokens) / float64(total)
}

// HitRatioOf 从一次 llm.Usage 直接求命中率（便捷方法）。
func HitRatioOf(u Usage) float64 {
	cs := CacheStats{HitTokens: u.PromptCacheHitTokens, MissTokens: u.PromptCacheMissTokens}
	return cs.ComputeHitRatio()
}

// CacheLogLine 生成一次请求日志行的命中率字段（"cache.hit_ratio=0.95"）。
// 用于每次 LLM 请求日志行；作为 N01 验收的采集基线。
func CacheLogLine(u Usage) string {
	return fmt.Sprintf("cache.hit_ratio=%.3f", HitRatioOf(u))
}

// RecordCacheUsage 简便地把一次用量记入累计 stats（供 tokenmeter/观测聚合）。
func (c *CacheStats) RecordUsage(u Usage) {
	c.Add(u.PromptCacheHitTokens, u.PromptCacheMissTokens)
}