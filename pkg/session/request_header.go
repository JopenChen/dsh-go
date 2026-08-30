// 本文件对应任务 M19：Request Header 快照 + request/context 路由。
//
// 对齐上游：packages/core/session/request-header
//
// 设计要点：
//   - EpochHeader 记录 config/system/tools 三个代际快照；
//   - RequestContext 记录 provider/model/contextWindow 与 reason(initial/resume/change/series)；
//   - RebuildFromHeader 从最新 request/header 快照重建 Prompt + Schema 输入面，
//     保证与实际发送 payload 逐字段一致（不依赖原始 events，compaction 后可重建）。
package session

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Reason 是请求重建原因。
type Reason string

// 请求重建原因枚举。
const (
	ReasonInitial Reason = "initial"
	ReasonResume  Reason = "resume"
	ReasonChange  Reason = "change"
	ReasonSeries  Reason = "series"
)

// EpochHeader 是 request/header 快照。
type EpochHeader struct {
	// ConfigEpoch 配置代际（settings 变化递增）。
	ConfigEpoch uint64 `json:"configEpoch"`
	// SystemHash system prompt 的稳定哈希。
	SystemHash string `json:"systemHash"`
	// ToolCount 工具数量（工具变化递增代际信号）。
	ToolCount int `json:"toolCount"`
}

// RequestContext 是 request/context 路由信息。
type RequestContext struct {
	// Provider 模型提供方。
	Provider string `json:"provider"`
	// Model 模型名。
	Model string `json:"model"`
	// ContextWindow 上下文窗口大小。
	ContextWindow int `json:"contextWindow,omitempty"`
	// Reason 重建原因。
	Reason Reason `json:"reason,omitempty"`
}

// RequestHeaderSnapshot 是重建请求所需的完整快照。
type RequestHeaderSnapshot struct {
	// Header EpochHeader 快照。
	Header EpochHeader `json:"header"`
	// Context RequestContext 路由信息。
	Context RequestContext `json:"context"`
	// SystemPrompt system prompt 文本（重建用）。
	SystemPrompt string `json:"systemPrompt,omitempty"`
	// ToolSchemas 工具 schema JSON（重建用）。
	ToolSchemas json.RawMessage `json:"toolSchemas,omitempty"`
}

// FoldRequestContext 折叠出最新 request/context（last-write-wins）。
func FoldRequestContext(events []SessionEvent) (RequestContext, bool) {
	var ctx RequestContext
	ok := false
	for _, ev := range events {
		if ev.Type != EventRequestContext {
			continue
		}
		if d, isData := ev.Data.(RequestContextData); isData {
			ctx = RequestContext{
				Provider:      d.Provider,
				Model:         d.Model,
				ContextWindow: d.Window,
				Reason:        Reason(d.Reason),
			}
			ok = true
		}
	}
	return ctx, ok
}

// FoldEpochHeader 折叠出最新 request/header 快照（last-write-wins）。
func FoldEpochHeader(events []SessionEvent) (EpochHeader, bool) {
	var h EpochHeader
	ok := false
	for _, ev := range events {
		if ev.Type != EventRequestHeader {
			continue
		}
		if d, isData := ev.Data.(RequestHeaderData); isData {
			h = EpochHeader{ConfigEpoch: d.ConfigEpoch, SystemHash: d.SystemHash, ToolCount: d.ToolCount}
			ok = true
		}
	}
	return h, ok
}

// RebuildFromHeader 从快照重建请求输入面（Prompt + Schema 文本）。
// 返回的 SystemPrompt 与 ToolSchemas 应与实际发送的 payload 逐字段一致。
func RebuildFromHeader(snap *RequestHeaderSnapshot) (systemPrompt string, toolSchemas json.RawMessage, err error) {
	if snap == nil {
		return "", nil, nil
	}
	return snap.SystemPrompt, snap.ToolSchemas, nil
}

// SnapshotHash 计算快照的稳定哈希（用于判断重建结果是否漂移）。
// 简化实现：拼接关键字段。
func (s *RequestHeaderSnapshot) SnapshotHash() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d", s.Header.ConfigEpoch))
	sb.WriteString("|")
	sb.WriteString(s.Header.SystemHash)
	sb.WriteString("|")
	sb.WriteString(fmt.Sprintf("%d", s.Header.ToolCount))
	sb.WriteString("|")
	sb.WriteString(s.Context.Provider)
	sb.WriteString("|")
	sb.WriteString(s.Context.Model)
	sb.WriteString("|")
	sb.WriteString(s.SystemPrompt)
	return sb.String()
}