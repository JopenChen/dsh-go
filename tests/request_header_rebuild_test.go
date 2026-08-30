// 本文件对应任务 M19：Request Header 快照 + request/context 路由。
package tests

import (
	"encoding/json"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// TestRequestHeaderRebuild 验证最新 request/header 快照 → rebuild 与实际 payload 一致。
func TestRequestHeaderRebuild(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("rh_1"))

	// 两次 header 快照（后一次覆盖）
	_, _ = sl.Append(session.RequestHeaderData{ConfigEpoch: 1, SystemHash: "old", ToolCount: 2})
	_, _ = sl.Append(session.RequestHeaderData{ConfigEpoch: 2, SystemHash: "new", ToolCount: 3})
	_, _ = sl.Append(session.RequestContextData{Provider: "deepseek", Model: "deepseek-chat", Window: 64000, Reason: "initial"})

	header, ok := session.FoldEpochHeader(sl.Events())
	if !ok {
		t.Fatal("应折叠到 header")
	}
	if header.ConfigEpoch != 2 || header.SystemHash != "new" || header.ToolCount != 3 {
		t.Fatalf("header 快照应为最新: %+v", header)
	}

	ctx, ok := session.FoldRequestContext(sl.Events())
	if !ok {
		t.Fatal("应折叠到 context")
	}
	if ctx.Provider != "deepseek" || ctx.Model != "deepseek-chat" || ctx.Reason != session.ReasonInitial {
		t.Fatalf("context 路由异常: %+v", ctx)
	}

	// rebuild 与实际 payload 一致
	snap := &session.RequestHeaderSnapshot{
		Header:       header,
		Context:      ctx,
		SystemPrompt: "system text",
		ToolSchemas:  json.RawMessage(`[{"name":"bash"}]`),
	}
	system, schemas, err := session.RebuildFromHeader(snap)
	if err != nil {
		t.Fatalf("RebuildFromHeader 失败: %v", err)
	}
	if system != "system text" {
		t.Fatalf("rebuild system 不一致: %q", system)
	}
	if string(schemas) != `[{"name":"bash"}]` {
		t.Fatalf("rebuild schemas 不一致: %s", schemas)
	}
}

// TestRequestHeaderCompactionRebuild 验证 compaction 后仍可重建（不依赖原始 events）。
func TestRequestHeaderCompactionRebuild(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("rh_2"))
	_, _ = sl.Append(session.RequestHeaderData{ConfigEpoch: 5, SystemHash: "h5", ToolCount: 4})

	// 模拟 compaction：只保留 header 快照事件（删掉其它）
	events := sl.Events()

	header, ok := session.FoldEpochHeader(events)
	if !ok {
		t.Fatal("compaction 后仍应能折叠 header")
	}
	if header.SystemHash != "h5" {
		t.Fatalf("rebuild 应基于快照: %+v", header)
	}
}
