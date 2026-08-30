// 本文件对应任务 M44：SessionHeader 格式拒绝 & 版本号。
package tests

import (
	"encoding/json"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// TestSessionFormatRejectUnknownEvent 验证未知事件类型 fail-closed 拒绝。
func TestSessionFormatRejectUnknownEvent(t *testing.T) {
	header := session.NewSessionHeader(brand.NewSessionID("s1"), "/ws")
	headerBytes, err := header.Marshal()
	if err != nil {
		t.Fatalf("header marshal 失败: %v", err)
	}

	// 已知事件可通过
	known, _ := json.Marshal(session.SessionEvent{
		Seq: 1, Type: session.EventUserMessage, Data: session.UserMessageData{Content: "hi"},
	})
	_, events, err := session.LoadSession(headerBytes, known)
	if err != nil {
		t.Fatalf("已知事件应通过: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("应加载 1 条事件: %d", len(events))
	}

	// 未知事件类型（构造原始 JSON，type 字段为非法值）→ CorruptEventError
	unknown := []byte(`{"seq":2,"time":"2026-08-31T00:00:00Z","type":"bogus/event","data":{}}`)
	_, _, err = session.LoadSession(headerBytes, unknown)
	if err == nil {
		t.Fatal("未知事件类型应被拒绝")
	}
	if !session.IsCorruptEventError(err) {
		t.Fatalf("应为 CorruptEventError, 实际 %T", err)
	}
}

// TestSessionFormatCrossVersionReject 验证不同 VERSION 的会话文件互相拒绝。
func TestSessionFormatCrossVersionReject(t *testing.T) {
	// 构造版本 999 的 header（未来格式）
	headerBytes := []byte(`{"version":999,"id":"s_x","createdAt":"2026-08-31T00:00:00Z"}`)

	known, _ := json.Marshal(session.SessionEvent{
		Seq: 1, Type: session.EventUserMessage, Data: session.UserMessageData{Content: "hi"},
	})
	_, _, err := session.LoadSession(headerBytes, known)
	if err == nil {
		t.Fatal("不同版本的会话文件应互相拒绝")
	}
	var unsupported *session.SessionFormatUnsupportedError
	if !asUnsupported(err, &unsupported) {
		t.Fatalf("应为 SessionFormatUnsupportedError, 实际 %T", err)
	}
}

// TestSessionKnownEventTypesWhiteList 验证 KNOWN 集合覆盖 45+ 事件且 round-trip 可用。
func TestSessionKnownEventTypesWhiteList(t *testing.T) {
	if len(session.KNOWN_SESSION_EVENT_TYPES) < 45 {
		t.Fatalf("白名单事件类型数 = %d, want >= 45", len(session.KNOWN_SESSION_EVENT_TYPES))
	}
	// 每个已知类型都能反序列化（sampleDataFor 来自 M04 测试辅助）
	header := session.NewSessionHeader(brand.NewSessionID("s2"), "/ws")
	headerBytes, _ := header.Marshal()

	for _, et := range session.AllEventTypes {
		data := sampleDataFor(et)
		if data == nil {
			t.Fatalf("缺少 %q 的样本数据", et)
		}
		line, err := json.Marshal(session.SessionEvent{Seq: 1, Time: fixedTestTime(), Type: et, Data: data})
		if err != nil {
			t.Fatalf("%q marshal 失败: %v", et, err)
		}
		if _, _, err := session.LoadSession(headerBytes, line); err != nil {
			t.Fatalf("%q 应通过白名单校验: %v", et, err)
		}
	}
}
