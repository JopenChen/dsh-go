// Package sessionquery 提供会话查询与搜索（任务 S04：Session Query + FTS5 搜索）。
//
// 对齐上游：packages/storage/session-query
//
// 设计要点：
//   - 读层构建在 S03 的 SQLite 会话存储之上：枚举会话（ListSessions）、按会话读事件
//     （Load），并复用其 FTS5 全文检索（Search）；标题取 latest-wins 投影，缺省回退
//     首条用户消息前缀；
//   - ListSummaries 支持三元过滤：标题前缀（TitlePrefix）/ 创建时间范围
//     （CreatedFrom..CreatedTo）/ 数量上限（Limit），过滤结果按更新时间倒序；
//   - Search 先用 FTS5 在事件正文/标题里命中关键词，得到出现该词的会话集合，
//     再附加「标题含词」的会话；两者求并集后返回摘要（ctrl+f 定位语义）。
package sessionquery

import (
	"context"
	"strings"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	sqlstore "github.com/JopenChen/dsh-go/pkg/persistence/sqlite"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/sessiontitle"
)

// ============================================================================
// 类型
// ============================================================================

// SessionSummary 是一条会话的可查询摘要。
type SessionSummary struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	EventCount  int       `json:"eventCount"`
}

// ListRequest 是会话列表查询参数（三元过滤）。
type ListRequest struct {
	TitlePrefix string     // 标题前缀过滤（可选）
	CreatedFrom *time.Time // 创建时间下界（可选，含）
	CreatedTo   *time.Time // 创建时间上界（可选，含）
	Limit       int        // 返回上限（<=0 表示不限）
}

// SearchRequest 是全文搜索参数。
type SearchRequest struct {
	Keyword string // 关键词（非空）
	Limit   int    // 返回上限
}

// QueryService 提供按标题/时间/关键词检索会话的能力。
type QueryService struct {
	store *sqlstore.Store
}

// New 创建会话查询服务，绑定 S03 SQLite 会话存储。
func New(store *sqlstore.Store) *QueryService {
	return &QueryService{store: store}
}

// ============================================================================
// 会话摘要派生
// ============================================================================

// deriveSummary 从某会话的全部事件派生摘要（标题/创建/更新/数量）。
func deriveSummary(id string, events []session.SessionEvent) SessionSummary {
	sum := SessionSummary{ID: id, EventCount: len(events)}
	if len(events) == 0 {
		return sum
	}
	sum.CreatedAt = events[0].Time
	sum.UpdatedAt = events[len(events)-1].Time

	// 标题 latest-wins；空则回退首条用户消息前缀。
	title := session.FoldSessionTitle(events).Title
	if title == "" {
		title = firstUserMessage(events)
	}
	sum.Title = title
	return sum
}

// firstUserMessage 取首条用户消息内容（用于标题回退）。
func firstUserMessage(events []session.SessionEvent) string {
	for _, ev := range events {
		if ev.Type == session.EventUserMessage {
			if d, ok := ev.Data.(session.UserMessageData); ok {
				return sessiontitle.Fallback(d.Content)
			}
		}
	}
	return ""
}

// ============================================================================
// 列表（三元过滤）
// ============================================================================

// ListSummaries 按标题前缀 + 创建时间范围枚举会话，返回按更新时间倒序的摘要。
func (q *QueryService) ListSummaries(ctx context.Context, req ListRequest) ([]SessionSummary, error) {
	ids, err := q.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	var out []SessionSummary
	for _, id := range ids {
		events, err := q.store.Load(ctx, brand.NewSessionID(id))
		if err != nil {
			return nil, err
		}
		sum := deriveSummary(id, events)
		// 标题前缀过滤。
		if req.TitlePrefix != "" && !strings.HasPrefix(sum.Title, req.TitlePrefix) {
			continue
		}
		// 创建时间范围过滤。
		if req.CreatedFrom != nil && sum.CreatedAt.Before(*req.CreatedFrom) {
			continue
		}
		if req.CreatedTo != nil && sum.CreatedAt.After(*req.CreatedTo) {
			continue
		}
		out = append(out, sum)
	}
	// 按更新时间倒序（最新在前）。
	sortByUpdatedDesc(out)
	if req.Limit > 0 && len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return out, nil
}

// ============================================================================
// 搜索（FTS5）
// ============================================================================

// Search 用 FTS5 检索关键词，返回命中会话的摘要（内容命中 ∪ 标题含词）。
func (q *QueryService) Search(ctx context.Context, req SearchRequest) ([]SessionSummary, error) {
	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		return nil, nil
	}

	// 1) 事件正文/标题 FTS 命中 → 收集会话 id 集合。
	matches, err := q.store.Search(ctx, keyword)
	if err != nil {
		return nil, err
	}
	hitSessions := map[string]bool{}
	for _, m := range matches {
		hitSessions[m.SessionID] = true
	}

	// 2) 标题包含关键词的会话也纳入（即使 FTS 在别处未命中该词）。
	ids, err := q.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	var out []SessionSummary
	for _, id := range ids {
		events, err := q.store.Load(ctx, brand.NewSessionID(id))
		if err != nil {
			return nil, err
		}
		sum := deriveSummary(id, events)
		if hitSessions[id] || strings.Contains(sum.Title, keyword) {
			out = append(out, sum)
		}
	}
	sortByUpdatedDesc(out)
	if req.Limit > 0 && len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return out, nil
}

// ============================================================================
// 工具
// ============================================================================

// sortByUpdatedDesc 按 UpdatedAt 倒序排序（最新在前）。
func sortByUpdatedDesc(list []SessionSummary) {
	// 简单插入排序（列表规模通常很小）。
	for i := 1; i < len(list); i++ {
		j := i
		for j > 0 && list[j].UpdatedAt.After(list[j-1].UpdatedAt) {
			list[j], list[j-1] = list[j-1], list[j]
			j--
		}
	}
}