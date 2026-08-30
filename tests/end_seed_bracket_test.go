// 本文件对应任务 M20：session/end-seed 种子边界 marker。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// TestEndSeedBracket 验证 Resume/Fork 后必写 end-seed 且分界正确。
func TestEndSeedBracket(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("s1"))

	// 先写入种子事件（模拟 Resume 回放的存量）
	_, _ = sl.Append(session.UserMessageData{Content: "seed1"})
	_, _ = sl.Append(session.UserMessageData{Content: "seed2"})

	// 回放完成后写 end-seed（无父会话）
	if _, err := session.MarkEndSeed(sl, brand.SessionID{}); err != nil {
		t.Fatalf("MarkEndSeed 失败: %v", err)
	}

	// 之后是 live 工作
	_, _ = sl.Append(session.UserMessageData{Content: "live1"})

	seedSeq := session.SeedEndSeq(sl.Events())
	if seedSeq == 0 {
		t.Fatal("应存在 end-seed")
	}

	// seed 事件在 end-seed 之前，live 事件在之后
	if !session.IsAfterEndSeed(sl.Events(), seedSeq+1) {
		t.Fatal("live1 应在 end-seed 之后")
	}
	if session.IsAfterEndSeed(sl.Events(), 1) {
		t.Fatal("seed1 不应在 end-seed 之后")
	}
}

// TestEndSeedForkParent 验证 Fork 后 end-seed 携带父会话。
func TestEndSeedForkParent(t *testing.T) {
	parentID := brand.NewSessionID("parent_s")
	sl := session.NewSessionLog(brand.NewSessionID("child_s"))

	_, err := session.MarkEndSeed(sl, parentID)
	if err != nil {
		t.Fatalf("MarkEndSeed 失败: %v", err)
	}

	events := sl.Events()
	last := events[len(events)-1]
	if last.Type != session.EventEndSeed {
		t.Fatalf("最后一条应为 end-seed: %+v", last)
	}
	d, ok := last.Data.(session.EndSeedData)
	if !ok || d.ParentSession.Raw() != "parent_s" {
		t.Fatalf("end-seed 应携带父会话: %+v", last.Data)
	}
}

// TestEndSeedHeaderLength 验证会话头部种子长度分界。
func TestEndSeedHeaderLength(t *testing.T) {
	h := session.NewSessionHeader(brand.NewSessionID("s1"), "/ws")
	h.SeedLength = 42
	if session.EndSeedMarker(h) != 42 {
		t.Fatalf("SeedLength 分界 = %d, want 42", session.EndSeedMarker(h))
	}
}