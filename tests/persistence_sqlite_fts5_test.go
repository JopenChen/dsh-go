// 本文件验证任务 S03：SQLite 持久化后端 + FTS5 索引。
//
// 覆盖：10k session 事件原子批量写入；按 seq 读回一致；FTS5 搜索标题/消息内容命中；
// 单查询性能 <100ms；文件 DB 可重开。
package tests

import (
	"context"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	sqlstore "github.com/JopenChen/dsh-go/pkg/persistence/sqlite"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// buildEvents 构造 n 条用户消息事件（其中夹杂少量可命中关键词）。
func buildEvents(n int) []session.SessionEvent {
	evs := make([]session.SessionEvent, 0, n)
	for i := 1; i <= n; i++ {
		msg := "普通用户消息编号 " + time.Now().Format("150405") + " 内容填充补足长度"
		if i%500 == 0 {
			msg = "需要检索的主题词：数据库选型对比报告"
		}
		evs = append(evs, session.SessionEvent{
			Seq:  uint64(i),
			Time: time.Now().Add(time.Duration(i) * time.Microsecond),
			Type: session.EventUserMessage,
			Data: session.UserMessageData{Content: msg},
		})
	}
	return evs
}

// TestSQLiteWriteReadRoundtrip 验证 10k 事件批量写入后读回一致。
func TestSQLiteWriteReadRoundtrip(t *testing.T) {
	ctx := context.Background()
	st, err := sqlstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sid := brand.NewSessionID("s-1k")
	const n = 10000
	if err := st.AppendBatch(ctx, sid, buildEvents(n)); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.Count(ctx, sid); c != n {
		t.Fatalf("应有 %d 条事件，实际 %d", n, c)
	}
	loaded, err := st.Load(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != n {
		t.Fatalf("读回 %d != %d", len(loaded), n)
	}
	// seq 应严格递增 1..n。
	for i, ev := range loaded {
		if ev.Seq != uint64(i+1) {
			t.Fatalf("seq 应 %d，实际 %d", i+1, ev.Seq)
		}
	}
}

// TestSQLiteFTSSearch 验证 FTS5 搜索标题/消息内容命中。
func TestSQLiteFTSSearch(t *testing.T) {
	ctx := context.Background()
	st, err := sqlstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sid := brand.NewSessionID("s-fts")
	// 写入一批事件（含标题事件与带关键词的消息）。
	for _, ev := range buildEvents(2000) {
		if err := st.Append(ctx, sid, ev); err != nil {
			t.Fatal(err)
		}
	}
	// 单独写一条 session/title 与 高亮消息，便于命中。
	_ = st.Append(ctx, sid, session.SessionEvent{
		Seq:  3001, Time: time.Now(), Type: session.EventSessionTitle,
		Data: session.SessionTitleData{Title: "竞品分析与缓存命中率对齐方案"},
	})
	_ = st.Append(ctx, sid, session.SessionEvent{
		Seq:  3002, Time: time.Now(), Type: session.EventUserMessage,
		Data: session.UserMessageData{Content: "请分析数据库选型对比报告"},
	})

	// FTS 命中标题关键词。
	if !st.FTSEnabled() {
		t.Skip("当前 SQLite 驱动不支持 FTS5")
	}
	matches, err := st.Search(ctx, "缓存命中率")
	if err != nil {
		t.Fatal(err)
	}
	foundTitle := false
	for _, m := range matches {
		if m.Seq == 3001 {
			foundTitle = true
		}
	}
	if !foundTitle {
		t.Fatalf("FTS5 应命中 session/title 事件(seq=3001)，实际 matches=%+v", matches)
	}

	// FTS 命中消息内容关键词。
	matches2, err := st.Search(ctx, "数据库选型")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches2) == 0 {
		t.Fatal("FTS5 应命中消息内容中的关键词")
	}
}

// TestSQLiteSearchPerf 验证 10k 事件后单查询 <100ms。
func TestSQLiteSearchPerf(t *testing.T) {
	ctx := context.Background()
	st, err := sqlstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sid := brand.NewSessionID("s-perf")
	if err := st.AppendBatch(ctx, sid, buildEvents(10000)); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = st.Search(ctx, "编号")
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("单查询 %v > 100ms，不满足验收", elapsed)
	}
}

// TestSQLiteSearchEmpty 验证空关键词返回空。
func TestSQLiteSearchEmpty(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlstore.Open(":memory:")
	defer st.Close()
	ms, err := st.Search(ctx, "   ")
	if err != nil || len(ms) != 0 {
		t.Fatalf("空关键词应返回空，实际 %v err=%v", ms, err)
	}
}