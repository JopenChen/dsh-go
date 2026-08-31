// 本文件验证任务 S04：Session Query + FTS5 搜索。
//
// 覆盖：标题前缀/创建时间范围/包含关键词三元过滤准确；FTS 内容搜索定位会话；
// 搜索结果与摘要字段正确。
package tests

import (
	"context"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	sqlstore "github.com/JopenChen/dsh-go/pkg/persistence/sqlite"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/sessionquery"
)

// seedSessions 往 sqlite 写入 3 个带标题与会话的会话（固定创建时间便于范围过滤）。
func seedSessions(t *testing.T, ctx context.Context, st *sqlstore.Store) {
	t.Helper()
	base := time.Now().Add(-24 * time.Hour)
	specs := []struct {
		id    string
		title string
		msg   string
		hour  int
	}{
		{"s1", "数据库选型对比报告", "分析 MySQL 与 PostgreSQL 的差异", 2},
		{"s2", "缓存命中率对齐方案", "如何达到 99% cache hit rate", 6},
		{"s3", "部署上线流程", "用 docker 部署服务", 10},
	}
	for _, sp := range specs {
		sid := brand.NewSessionID(sp.id)
		// 用户消息（seq=1，时间对应当前小时的起点）。
		msgEv := session.SessionEvent{
			Seq: 1, Time: base.Add(time.Duration(sp.hour) * time.Hour),
			Type: session.EventUserMessage, Data: session.UserMessageData{Content: sp.msg},
		}
		titleEv := session.SessionEvent{
			Seq: 2, Time: msgEv.Time.Add(time.Second),
			Type: session.EventSessionTitle, Data: session.SessionTitleData{Title: sp.title},
		}
		if err := st.Append(ctx, sid, msgEv); err != nil {
			t.Fatal(err)
		}
		if err := st.Append(ctx, sid, titleEv); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSessionQueryTripleFilter 验证标题前缀 + 创建时间范围 + Limit 三元过滤。
func TestSessionQueryTripleFilter(t *testing.T) {
	ctx := context.Background()
	st, err := sqlstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedSessions(t, ctx, st)
	q := sessionquery.New(st)

	from := time.Now().Add(-24 * time.Hour).Add(5 * time.Hour)
	to := time.Now().Add(-24 * time.Hour).Add(11 * time.Hour)

	// 标题前缀过滤：应只命中 s1（数据库…）与 s3（部署…），s2 标题"缓存…"不含"数据"。
	list := mustList(t, q, sessionquery.ListRequest{TitlePrefix: "数据"})
	if len(list) != 1 || list[0].ID != "s1" {
		t.Fatalf("标题前缀『数据』应仅命中 s1，实际 %+v", list)
	}

	// 创建时间范围 [5h,11h)：命中 s2(6h) 与 s3(10h)，排除 s1(2h)。
	list2 := mustList(t, q, sessionquery.ListRequest{CreatedFrom: &from, CreatedTo: &to})
	ids := idsOf(list2)
	if len(ids) != 2 || !has(ids, "s2") || !has(ids, "s3") {
		t.Fatalf("时间范围应命中 s2/s3，实际 %v", ids)
	}

	// 三元组合 + Limit=1（按更新时间倒序，最新为 s3）。
	list3 := mustList(t, q, sessionquery.ListRequest{TitlePrefix: "", Limit: 1})
	if len(list3) != 1 || list3[0].ID != "s3" {
		t.Fatalf("Limit=1 且倒序应命中最新 s3，实际 %+v", list3)
	}
}

// TestSessionQueryContentSearch 验证 FTS5 内容关键词定位会话。
func TestSessionQueryContentSearch(t *testing.T) {
	ctx := context.Background()
	st, err := sqlstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedSessions(t, ctx, st)
	q := sessionquery.New(st)

	if !st.FTSEnabled() {
		t.Skip("当前驱动不支持 FTS5")
	}
	// 内容含"PostgreSQL" → s1。
	res := mustSearch(t, q, sessionquery.SearchRequest{Keyword: "PostgreSQL"})
	if len(res) != 1 || res[0].ID != "s1" {
		t.Fatalf("关键词 PostgreSQL 应命中 s1，实际 %+v", res)
	}
	// 标题含"缓存" → s2（标题命中路径）。
	res2 := mustSearch(t, q, sessionquery.SearchRequest{Keyword: "缓存命中率"})
	if len(res2) != 1 || res2[0].ID != "s2" {
		t.Fatalf("标题含『缓存命中率』应命中 s2，实际 %+v", res2)
	}
}

// TestSessionQueryEmptyKeyword 验证空关键词返回空。
func TestSessionQueryEmptyKeyword(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlstore.Open(":memory:")
	defer st.Close()
	q := sessionquery.New(st)
	res, err := q.Search(ctx, sessionquery.SearchRequest{Keyword: "  "})
	if err != nil || len(res) != 0 {
		t.Fatalf("空关键词应返回空，实际 %v err=%v", res, err)
	}
}

// ============================================================================
// 工具
// ============================================================================

func mustList(t *testing.T, q *sessionquery.QueryService, req sessionquery.ListRequest) []sessionquery.SessionSummary {
	t.Helper()
	list, err := q.ListSummaries(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func mustSearch(t *testing.T, q *sessionquery.QueryService, req sessionquery.SearchRequest) []sessionquery.SessionSummary {
	t.Helper()
	res, err := q.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func idsOf(list []sessionquery.SessionSummary) map[string]bool {
	m := map[string]bool{}
	for _, s := range list {
		m[s.ID] = true
	}
	return m
}

func has(m map[string]bool, id string) bool { return m[id] }