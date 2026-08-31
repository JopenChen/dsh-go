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
	"sync"
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

// ============================================================================
// N08：缓存破窗告警（CacheAlert）
// ============================================================================

// AlertLevel 是告警级别。
type AlertLevel string

// 告警级别。
const (
	AlertWarn  AlertLevel = "warn"
	AlertError AlertLevel = "error"
)

// Alert 是一次缓存破窗告警。
type Alert struct {
	// Level 级别。
	Level AlertLevel `json:"level"`
	// Message 告警内容（含 session.id / current / previous）。
	Message string `json:"message"`
	// Webhook 是否应触发 webhook 通知（连续破窗时）。
	Webhook bool `json:"webhook,omitempty"`
}

// AlertSink 是告警输出回调（日志/webhook）。
type AlertSink func(a Alert)

// CacheAlert 检测缓存命中率破窗并分级告警。
//
//   - 单次命中率突降 > DropWarn 相比上一次 → warn（记录 session/current/previous）；
//   - 连续 consecutiveFails 次命中率 < Threshold → error + 可选 webhook；
//   - 命中率恢复（≥ Threshold）→ consecutiveFails 重置为 0。
type CacheAlert struct {
	// Threshold 低命中率阈值（默认 0.5）。
	Threshold float64
	// DropWarn 单次相对骤降阈值（默认 0.3）。
	DropWarn float64
	// ConsecutiveFails 连续低于阈值触发 error 的次数（默认 5）。
	ConsecutiveFails int
	// Sink 告警输出。
	Sink AlertSink

	mu               sync.Mutex
	prevRatio        float64
	consecutiveFails int
	hasPrev          bool
	alerts           []Alert
}

// NewCacheAlert 创建默认配置的破窗告警。
func NewCacheAlert(sink AlertSink) *CacheAlert {
	return &CacheAlert{
		Threshold:        0.5,
		DropWarn:         0.3,
		ConsecutiveFails: 5,
		Sink:             sink,
	}
}

// Observe 记录一次命中率并判定是否告警。
// sessionID 用于告警上下文。
func (a *CacheAlert) Observe(sessionID string, ratio float64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 单次骤降告警（相对上一次）。
	if a.hasPrev && a.prevRatio-ratio > a.DropWarn {
		alert := Alert{Level: AlertWarn,
			Message: "cache hit ratio dropped: session=" + sessionID +
				" current=" + ft(ratio) + " previous=" + ft(a.prevRatio)}
		a.emit(alert, false)
	}

	// 连续低命中率告警。
	if ratio < a.Threshold {
		a.consecutiveFails++
		if a.consecutiveFails >= a.ConsecutiveFails {
			alert := Alert{Level: AlertError,
				Message: "cache hit ratio persistently low: session=" + sessionID +
					" consecutive=" + itoa2(a.consecutiveFails) + " ratio=" + ft(ratio)}
			a.emit(alert, true)
			a.consecutiveFails = 0 // 每次 error 重置（或按需保持）
		}
	} else {
		a.consecutiveFails = 0
	}

	a.prevRatio = ratio
	a.hasPrev = true
}

func (a *CacheAlert) emit(al Alert, webhook bool) {
	al.Webhook = webhook
	a.alerts = append(a.alerts, al)
	if a.Sink != nil {
		a.Sink(al)
	}
}

// Alerts 返回累计告警快照。
func (a *CacheAlert) Alerts() []Alert {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Alert(nil), a.alerts...)
}

func ft(f float64) string {
	return fmt.Sprintf("%.3f", f)
}

func itoa2(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}