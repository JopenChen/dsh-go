// Package tests 的 N01 探针单元测试。
//
// 覆盖 CacheStats.ComputeHitRatio() 在 0 / 正常 / 全命中 / 全未命中场景的正确性（防 NaN）。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/llm"
)

// TestCacheStatsHitRatioZero 验证总量 0 → 0.0（防 0/0 NaN）。
func TestCacheStatsHitRatioZero(t *testing.T) {
	cs := llm.CacheStats{}
	if r := cs.ComputeHitRatio(); r != 0.0 {
		t.Fatalf("总量 0 命中率应为 0.0, 实际 %v", r)
	}
}

// TestCacheStatsHitRatioPartial 验证部分命中（79/100 → 0.79）。
func TestCacheStatsHitRatioPartial(t *testing.T) {
	cs := llm.CacheStats{HitTokens: 79, MissTokens: 21}
	r := cs.ComputeHitRatio()
	if r < 0.789 || r > 0.791 {
		t.Fatalf("应约 0.79, 实际 %v", r)
	}
}

// TestCacheStatsHitRatioFull 验证全命中 → 1.0。
func TestCacheStatsHitRatioFull(t *testing.T) {
	cs := llm.CacheStats{HitTokens: 100, MissTokens: 0}
	if r := cs.ComputeHitRatio(); r != 1.0 {
		t.Fatalf("全命中应为 1.0, 实际 %v", r)
	}
}

// TestCacheStatsHitRatioNone 验证全未命中 → 0.0。
func TestCacheStatsHitRatioNone(t *testing.T) {
	cs := llm.CacheStats{HitTokens: 0, MissTokens: 50}
	if r := cs.ComputeHitRatio(); r != 0.0 {
		t.Fatalf("全未命中应为 0.0, 实际 %v", r)
	}
}

// TestCacheStatsAddRecord 验证累计聚合与 log 行。
func TestCacheStatsAddRecord(t *testing.T) {
	cs := llm.CacheStats{}
	cs.RecordUsage(llm.Usage{PromptTokens: 1000, PromptCacheHitTokens: 800, PromptCacheMissTokens: 200})
	cs.RecordUsage(llm.Usage{PromptTokens: 500, PromptCacheHitTokens: 500, PromptCacheMissTokens: 0})
	if cs.HitTokens != 1300 || cs.MissTokens != 200 {
		t.Fatalf("累计异常: %+v", cs)
	}
	r := cs.ComputeHitRatio() // 1300/1500 ≈ 0.8667
	if r < 0.86 || r > 0.87 {
		t.Fatalf("累计命中率应约 0.867, 实际 %v", r)
	}
	// 日志行字段。
	line := llm.CacheLogLine(llm.Usage{PromptCacheHitTokens: 900, PromptCacheMissTokens: 100})
	if line != "cache.hit_ratio=0.900" {
		t.Fatalf("日志行应 cache.hit_ratio=0.900, 实际 %q", line)
	}
}