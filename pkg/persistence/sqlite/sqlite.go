// Package sqlite 提供 SQLite 持久化后端 + FTS5 索引（任务 S03）。
//
// 对齐上游：packages/storage/sqlite-session
//
// 设计要点：
//   - 单 DB：一张 `session_events` 普通表存全部事件的 JSON 载荷（BLOB），并以
//     (session_id, seq) 为主键；
//   - 同库一张 FTS5 虚拟表 `session_events_fts` 索引每条的搜索文本，用于标题/消息
//     内容的全文检索（S04 的 Session Query 底层依托于此）；
//   - 追加（Append）与批量追加（AppendBatch）均走事务，原子写入：普通行与 FTS
//     行要么一起提交要么一起回滚，保证索引与正文一致性；
//   - 从事件 Data 的派生文本可索引：user/assistant 消息取 Content，session/title
//     取 Title，其余类型为空。
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// ============================================================================
// DDL
// ============================================================================

const (
	ddlEvents = `
		CREATE TABLE IF NOT EXISTS session_events (
			session_id   TEXT NOT NULL,
			seq          INTEGER NOT NULL,
			event_type   TEXT NOT NULL,
			ts           DATETIME NOT NULL,
			data         BLOB NOT NULL,
			text_content TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (session_id, seq)
		);
	`
	ddlFTS = `
		CREATE VIRTUAL TABLE IF NOT EXISTS session_events_fts
			USING fts5(session_id UNINDEXED, seq UNINDEXED, text_content,
				tokenize = 'trigram');
	`
)

// ============================================================================
// Store
// ============================================================================

// Store 是 SQLite 会话事件存储（含 FTS5 索引）。
type Store struct {
	db *sql.DB
	// fts 标记 FTS5 是否可用（不可用时 FTS 相关操作降级为 LIKE）。
	fts bool
}

// Open 打开（或创建）SQLite 数据库并初始化表与 FTS5 索引。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(ddlEvents); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("persistence/sqlite: init events table: %w", err)
	}
	st := &Store{db: db}
	// 尝试创建 FTS5 虚拟表；失败则降级（fts=false）。
	if _, err := db.Exec(ddlFTS); err == nil {
		st.fts = true
	} else {
		// FTS5 不可用时静默降级，LIKESearch 兜底仍可用。
		st.fts = false
	}
	return st, nil
}

// Close 关闭数据库连接。
func (s *Store) Close() error {
	return s.db.Close()
}

// FTSEnabled 返回 FTS5 是否可用（测试断言用）。
func (s *Store) FTSEnabled() bool { return s.fts }

// FTSUnavailable 表示当前驱动不支持 FTS5（供调用方提示）。
func (s *Store) FTSUnavailable() bool { return !s.fts }

// ============================================================================
// 文本抽取
// ============================================================================

// extractText 从事件派生用于 FTS 索引的搜索文本。
func extractText(ev session.SessionEvent) string {
	switch d := ev.Data.(type) {
	case session.UserMessageData:
		return d.Content
	case session.AssistantMessageData:
		return d.Content
	case session.SessionTitleData:
		return d.Title
	default:
		return ""
	}
}

// ============================================================================
// 写入（原子）
// ============================================================================

// Append 追加单条事件（普通行 + FTS 行，同一事务原子写入）。
func (s *Store) Append(ctx context.Context, sessionID brand.SessionID, ev session.SessionEvent) error {
	return s.AppendBatch(ctx, sessionID, []session.SessionEvent{ev})
}

// AppendBatch 在一笔事务内原子写入多条事件。
func (s *Store) AppendBatch(ctx context.Context, sessionID brand.SessionID, events []session.SessionEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO session_events(session_id, seq, event_type, ts, data, text_content)
		VALUES(?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	var ftsStmt *sql.Stmt
	if s.fts {
		ftsStmt, err = tx.PrepareContext(ctx, `
			INSERT INTO session_events_fts(session_id, seq, text_content)
			VALUES(?, ?, ?)`)
		if err != nil {
			return err
		}
	}

	for _, ev := range events {
		raw, err := ev.MarshalJSON()
		if err != nil {
			return err
		}
		txt := extractText(ev)
		if _, err := stmt.ExecContext(ctx, sessionID.Raw(), ev.Seq, string(ev.Type), ev.Time, raw, txt); err != nil {
			return err
		}
		if s.fts {
			if _, err := ftsStmt.ExecContext(ctx, sessionID.Raw(), ev.Seq, txt); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ============================================================================
// 读取
// ============================================================================

// Load 读取某会话的全部事件（按 seq 升序）。
func (s *Store) Load(ctx context.Context, sessionID brand.SessionID) ([]session.SessionEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT data FROM session_events WHERE session_id = ? ORDER BY seq`, sessionID.Raw())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []session.SessionEvent
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var ev session.SessionEvent
		if err := ev.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// Count 返回某会话事件条数。
func (s *Store) Count(ctx context.Context, sessionID brand.SessionID) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_events WHERE session_id = ?`, sessionID.Raw()).Scan(&n)
	return n, err
}

// ListSessions 返回全部出现过会话 ID（按字典序，确定性；供 Session Query 枚举）。
func (s *Store) ListSessions(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT session_id FROM session_events ORDER BY session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ============================================================================
// 搜索
// ============================================================================

// Match 是一次搜索命中（会话 + 序号 + 命中文本），按 session_id 排序。
type Match struct {
	SessionID string `json:"sessionId"`
	Seq       uint64 `json:"seq"`
	Text      string `json:"text,omitempty"`
}

// Search 用 FTS5 全文检索关键词；命中则返回匹配（session_id, seq）。
// FTS5 trigram 分词要求查询 ≥3 字符；短查询（<3 rune）自动降级 LIKE。
// FTS5 整体不可用时也降级 LIKE（语义一致，无词法倒排）。
func (s *Store) Search(ctx context.Context, query string) ([]Match, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if !s.fts || len([]rune(query)) < 3 {
		return s.searchLike(ctx, query)
	}
	return s.searchFTS(ctx, query)
}

// searchFTS 使用 FTS5 MATCH 检索。
func (s *Store) searchFTS(ctx context.Context, query string) ([]Match, error) {
	// CJK 无空格时各汉字分别是 token，用「短语查询」匹配连续相邻的字符序列最可靠。
	// 对含空白/拉丁词的查询，短语查询同样成立；对空串已在上层排除。
	term := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.session_id, f.seq, f.text_content
		FROM session_events_fts f
		WHERE session_events_fts MATCH ?
		ORDER BY f.session_id, f.seq`, term)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Match
	for rows.Next() {
		var m Match
		if err := rows.Scan(&m.SessionID, &m.Seq, &m.Text); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// searchLike 在 FTS5 不可用时用 LIKE 兜底（遍历 session_events 的 text_content）。
func (s *Store) searchLike(ctx context.Context, query string) ([]Match, error) {
	pattern := "%" + query + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, seq, text_content FROM session_events
		WHERE text_content LIKE ?
		ORDER BY session_id, seq`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Match
	for rows.Next() {
		var m Match
		if err := rows.Scan(&m.SessionID, &m.Seq, &m.Text); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}