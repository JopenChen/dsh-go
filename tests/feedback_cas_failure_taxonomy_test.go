// Package tests 的 Message Feedback（S09）验收测试。
//
// 覆盖：
//   - write/read feedback（rating/note/CAS version/时间戳）
//   - CAS 冲突返回 VERSION_CONFLICT；不存在消息 NOT_FOUND；无会话 SESSION_NOT_FOUND
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/feedback"
	"github.com/JopenChen/dsh-go/pkg/storage"
)

// TestFeedbackWriteRead 验证写入与读取（CAS 版本自增）。
func TestFeedbackWriteRead(t *testing.T) {
	ctx := context.Background()
	store := feedback.New(storage.NewMemoryKV())
	sid := brand.NewSessionID("s1")

	v1, err := store.Put(ctx, sid, "msg1", feedback.RatingThumbsUp, "great", nil)
	if err != nil {
		t.Fatal(err)
	}
	if v1 != 1 {
		t.Fatalf("首版应为 1, 实际 %d", v1)
	}
	fb, err := store.Get(ctx, sid, "msg1")
	if err != nil {
		t.Fatal(err)
	}
	if fb.Rating != feedback.RatingThumbsUp || fb.Note != "great" {
		t.Fatalf("读取失准: %+v", fb)
	}
	if fb.Version != 1 {
		t.Fatalf("version 应 1, 实际 %d", fb.Version)
	}
	// 更新（无 CAS 约束）→ 版本升到 2。
	v2, _ := store.Put(ctx, sid, "msg1", feedback.RatingThumbsDown, "revised", nil)
	if v2 != 2 {
		t.Fatalf("更新后版本应 2, 实际 %d", v2)
	}
}

// TestFeedbackCASConflict 验证版本冲突 → VERSION_CONFLICT。
func TestFeedbackCASConflict(t *testing.T) {
	ctx := context.Background()
	store := feedback.New(storage.NewMemoryKV())
	sid := brand.NewSessionID("s1")
	if _, err := store.Put(ctx, sid, "m", feedback.RatingThumbsUp, "", nil); err != nil {
		t.Fatal(err)
	}
	// 用错误 expectedVersion（0=创建）去覆盖已存在记录 → 冲突。
	exp := uint64(0)
	_, err := store.Put(ctx, sid, "m", feedback.RatingThumbsDown, "", &exp)
	if feedback.CodeOf(err) != feedback.CodeVersionConflict {
		t.Fatalf("CAS 冲突应 VERSION_CONFLICT, 实际 %v", err)
	}
}

// TestFeedbackNotFound 验证不存在消息 → NOT_FOUND。
func TestFeedbackNotFound(t *testing.T) {
	ctx := context.Background()
	store := feedback.New(storage.NewMemoryKV())
	_, err := store.Get(ctx, brand.NewSessionID("s1"), "ghost")
	if feedback.CodeOf(err) != feedback.CodeNotFound {
		t.Fatalf("应 NOT_FOUND, 实际 %v", err)
	}
}

// TestFeedbackSessionNotFound 验证无会话 → SESSION_NOT_FOUND。
func TestFeedbackSessionNotFound(t *testing.T) {
	ctx := context.Background()
	store := feedback.New(storage.NewMemoryKV())
	_, err := store.Put(ctx, brand.SessionID{}, "m", feedback.RatingThumbsUp, "", nil)
	if feedback.CodeOf(err) != feedback.CodeSessionNotFound {
		t.Fatalf("应 SESSION_NOT_FOUND, 实际 %v", err)
	}
}

// TestFeedbackListAndDelete 验证列与会话删除。
func TestFeedbackListAndDelete(t *testing.T) {
	ctx := context.Background()
	sid := brand.NewSessionID("sX")
	other := brand.NewSessionID("sY")
	store := feedback.New(storage.NewMemoryKV())
	for _, m := range []string{"a", "b"} {
		_, _ = store.Put(ctx, sid, m, feedback.RatingNone, "", nil)
	}
	_, _ = store.Put(ctx, other, "c", feedback.RatingNone, "", nil)
	list, err := store.List(ctx, sid)
	if err != nil || len(list) != 2 {
		t.Fatalf("sX 应列出 2 条, 实际 %d (%v)", len(list), err)
	}
	if err := store.Delete(ctx, sid, "a"); err != nil {
		t.Fatal(err)
	}
	list, _ = store.List(ctx, sid)
	if len(list) != 1 {
		t.Fatalf("删除后应剩 1 条, 实际 %d", len(list))
	}
}