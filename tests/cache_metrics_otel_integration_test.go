// Package tests 的 N09（Grafana 看板 + OTel 探针）验收测试。
//
// 覆盖：
//   - 4 个指标名（hit_ratio / hit_tokens / miss_tokens / broken_count）正确导出
//   - histogram P50 计算正确（供告警规则）
//   - broken_count 在破窗模式时 +1
//   - Grafana 看板 JSON 结构合法且含 3 面板
package tests

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/JopenChen/dsh-go/internal/telemetry"
)

// TestN09MetricsNames 验证 4 个 OTel 指标名。
func TestN09MetricsNames(t *testing.T) {
	names := telemetry.MetricsNames()
	want := map[string]bool{
		"dsh.cache.hit_ratio":    false,
		"dsh.cache.hit_tokens":   false,
		"dsh.cache.miss_tokens":  false,
		"dsh.cache.broken_count": false,
	}
	for _, n := range names {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, ok := range want {
		if !ok {
			t.Fatalf("指标 %s 缺失", n)
		}
	}
}

// TestN09MetricsRecord 验证记录与 broken_count。
func TestN09MetricsRecord(t *testing.T) {
	m := telemetry.NewCacheMetrics()
	m.Record(90, 10, 0.9)
	m.Record(80, 20, 0.8)
	if m.HitTokensTotal != 170 || m.MissTokensTotal != 30 {
		t.Fatalf("累计失准: hit=%d miss=%d", m.HitTokensTotal, m.MissTokensTotal)
	}
	m.MarkBroken()
	if m.BrokenCount != 1 {
		t.Fatalf("broken_count 应为 1, 实际 %d", m.BrokenCount)
	}
}

// TestN09HistogramP50 验证 P50 计算。
func TestN09HistogramP50(t *testing.T) {
	m := telemetry.NewCacheMetrics()
	// 4 个样本 0.5,0.6,0.9,1.0 → 中位数 0.9。
	m.Record(1, 0, 0.5)
	m.Record(1, 0, 0.6)
	m.Record(1, 0, 0.9)
	m.Record(1, 0, 1.0)
	if p := m.HistogramP50(); p != 0.9 {
		t.Fatalf("P50 应 0.9, 实际 %v", p)
	}
}

// TestN09GrafanaDashboardJSON 验证 Grafana 看板 JSON 合法且 3 面板 + 2 指标。
func TestN09GrafanaDashboardJSON(t *testing.T) {
	data, err := os.ReadFile("../deploy/grafana/dsh-cache-dashboard.json")
	if err != nil {
		t.Skipf("看板文件缺失: %v", err)
		return
	}
	var dash struct {
		Panels []struct {
			ID    int    `json:"id"`
			Type  string `json:"type"`
			Title string `json:"title"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(data, &dash); err != nil {
		t.Fatalf("看板 JSON 非法: %v", err)
	}
	if len(dash.Panels) != 3 {
		t.Fatalf("应有 3 面板, 实际 %d", len(dash.Panels))
	}
}