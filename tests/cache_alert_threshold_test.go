// Package tests 的 N08（缓存破窗告警）验收测试。
//
// 覆盖：
//   - 单次命中率突降 > 30% → warn 日志（含 session.id / current / previous）
//   - 连续 5 次命中率 < 50% → error + webhook 触发
//   - 命中率恢复后 consecutiveFails 重置为 0
//   - 告警阈值可通过配置覆盖
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/llm"
)

// collectAlerts 收集告警的 sink。
type collectAlerts struct {
	alerts []llm.Alert
}

func (c *collectAlerts) sink(a llm.Alert) { c.alerts = append(c.alerts, a) }

// TestN08SuddenDropWarns 验证单次突降 > 30% → warn。
func TestN08SuddenDropWarns(t *testing.T) {
	col := &collectAlerts{}
	ca := llm.NewCacheAlert(col.sink)
	// 先建立正常基线。
	ca.Observe("s1", 0.95)
	// 突降到 0.3（降幅 0.65 > 0.3）→ warn。
	ca.Observe("s1", 0.30)
	var sawWarn bool
	for _, a := range col.alerts {
		if a.Level == llm.AlertWarn {
			sawWarn = true
			if !containsStr(a.Message, "s1") || !containsStr(a.Message, "current") || !containsStr(a.Message, "previous") {
				t.Fatalf("warn 应含 session/current/previous: %q", a.Message)
			}
		}
	}
	if !sawWarn {
		t.Fatal("突降应产生 warn 告警")
	}
}

// TestN08ConsecutiveFailsSignalsError 验证连续 5 次 < 50% → error + webhook。
func TestN08ConsecutiveFailsSignalsError(t *testing.T) {
	col := &collectAlerts{}
	ca := llm.NewCacheAlert(col.sink)
	for i := 0; i < 6; i++ {
		ca.Observe("s2", 0.10)
	}
	var sawError bool
	for _, a := range col.alerts {
		if a.Level == llm.AlertError {
			sawError = true
			if !a.Webhook {
				t.Fatal("连续破窗 error 应触发 webhook")
			}
		}
	}
	if !sawError {
		t.Fatal("连续 5 次低命中率应产生 error + webhook")
	}
}

// TestN08RecoveryResets 验证命中率恢复后 consecutiveFails 重置（不再立即 error）。
func TestN08RecoveryResets(t *testing.T) {
	col := &collectAlerts{}
	ca := llm.NewCacheAlert(col.sink)
	// 3 次低（未达 5 次阈值）。
	for i := 0; i < 3; i++ {
		ca.Observe("s3", 0.10)
	}
	ca.Observe("s3", 0.90) // 恢复 → 重置
	// 再来 4 次低（应累计到 4，未到 5 → 无 error）。
	col.alerts = nil
	errCount := 0
	for i := 0; i < 4; i++ {
		ca.Observe("s3", 0.10)
	}
	for _, a := range col.alerts {
		if a.Level == llm.AlertError {
			errCount++
		}
	}
	if errCount != 0 {
		t.Fatalf("恢复后 4 次低不应触发 error（consecutive 已重置）, 实际 %d", errCount)
	}
}

// TestN08ConfigOverridable 验证阈值可配置。
func TestN08ConfigOverridable(t *testing.T) {
	col := &collectAlerts{}
	ca := &llm.CacheAlert{Threshold: 0.8, DropWarn: 0.1, ConsecutiveFails: 2, Sink: col.sink}
	// 2 次命中率 0.75（<0.8）→ 触发 error。
	ca.Observe("s4", 0.75)
	ca.Observe("s4", 0.75)
	var sawError bool
	for _, a := range col.alerts {
		if a.Level == llm.AlertError {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("配置 threshold=0.8/consecutive=2 时 2 次 0.75 应触发 error")
	}
}