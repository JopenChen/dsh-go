// Package feedback 提供 Message Feedback 接缝（Storage Domain sidecar + 稳定 fail 分类）。
//
// 对齐上游：packages/feedback/feedback（S09）
//
// 设计要点：
//   - 每个 (sessionId, messageId) 一条 Feedback 记录：rating/note/version(CAS)/createdAt/updatedAt；
//   - 复用 M45 storage.Domain[T] 的 CAS 版本（Put 带 expectedVersion，冲突报 VERSION_CONFLICT）；
//   - 稳定错误分类：VERSION_CONFLICT / SESSION_NOT_FOUND / NOT_FOUND；
//   - list/put/delete。
package feedback

import (
	"context"
	"fmt"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/storage"
)

// ============================================================================
// 模型
// ============================================================================

// Rating 是反馈评分。
type Rating int

// 评分枚举。
const (
	RatingThumbsDown Rating = -1
	RatingNone       Rating = 0
	RatingThumbsUp   Rating = 1
)

// Feedback 是一条消息反馈。
type Feedback struct {
	// SessionID 所属会话 id。
	SessionID brand.SessionID `json:"sessionId"`
	// MessageID 消息标识。
	MessageID string `json:"messageId"`
	// Rating 评分（-1/0/1）。
	Rating Rating `json:"rating"`
	// Note 备注。
	Note string `json:"note,omitempty"`
	// Version CAS 版本（写时自增）。
	Version uint64 `json:"version"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt 最后更新时间。
	UpdatedAt time.Time `json:"updatedAt"`
}

// ============================================================================
// 稳定错误分类
// ============================================================================

// FailCode 是稳定失败分类码。
type FailCode string

const (
	CodeVersionConflict FailCode = "VERSION_CONFLICT"
	CodeSessionNotFound FailCode = "SESSION_NOT_FOUND"
	CodeNotFound        FailCode = "NOT_FOUND"
)

// FailError 是携带稳定码的反馈错误。
type FailError struct {
	Code FailCode
	Msg  string
}

func (e *FailError) Error() string { return string(e.Code) + ": " + e.Msg }

func fail(code FailCode, format string, args ...any) error {
	return &FailError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// CodeOf 提取稳定码。
func CodeOf(err error) FailCode {
	if err == nil {
		return ""
	}
	if fe, ok := err.(*FailError); ok {
		return fe.Code
	}
	return ""
}

// ============================================================================
// Store
// ============================================================================

// Store 是 Message Feedback 存储。
type Store struct {
	domain *storage.Domain[Feedback]
}

// New 创建反馈存储（backend 传 M45 storage backend）。
func New(backend storage.Backend) *Store {
	return &Store{domain: storage.NewDomain[Feedback](backend, "feedback")}
}

// keyOf 构造 (sessionId, messageId) 的存储键。
func keyOf(session brand.SessionID, messageID string) string {
	if session.IsZero() {
		return "_unowned_" + messageID
	}
	return session.Raw() + "::" + messageID
}

// Put 写入（或创建）一条反馈。Sign 若提供 expectedVersion，则做 CAS：版本不符 → VERSION_CONFLICT。
// 返回新版本号。
func (s *Store) Put(ctx context.Context, session brand.SessionID, messageID string, rating Rating, note string, expectedVersion *uint64) (uint64, error) {
	if session.IsZero() {
		return 0, fail(CodeSessionNotFound, "missing session id")
	}
	key := keyOf(session, messageID)
	now := time.Now()

	// 读取现有（判断 create vs update）。
	existing, ver, err := s.domain.Get(ctx, key)
	var expected uint64
	created := false
	if err != nil {
		if !isNotFound(err) {
			return 0, err
		}
		// 不存在 → 创建。
		created = true
		expected = 0
	} else {
		expected = ver
		_ = existing
	}

	// CAS：若调用方要求某版本，则校验。
	if expectedVersion != nil {
		if created {
			if *expectedVersion != 0 {
				return 0, fail(CodeVersionConflict, "feedback for message %q does not exist (expected v%d)", messageID, *expectedVersion)
			}
		} else if *expectedVersion != expected {
			return 0, fail(CodeVersionConflict, "feedback for message %q version mismatch (expected %d, now %d)", messageID, *expectedVersion, expected)
		}
	}

	fb := Feedback{
		SessionID: session,
		MessageID: messageID,
		Rating:    rating,
		Note:      note,
		Version:   expected + 1,
		CreatedAt: existing.CreatedAt,
		UpdatedAt: now,
	}
	if created {
		fb.CreatedAt = now
	}
	newVer, err := s.domain.Put(ctx, key, fb, expected)
	if err != nil {
		// Domain 层 CAS 失败也归类为 VERSION_CONFLICT。
		return 0, fail(CodeVersionConflict, "cas put failed: %v", err)
	}
	return newVer, nil
}

// Get 读取一条反馈；不存在 → NOT_FOUND。
func (s *Store) Get(ctx context.Context, session brand.SessionID, messageID string) (*Feedback, error) {
	fb, _, err := s.domain.Get(ctx, keyOf(session, messageID))
	if err != nil {
		if isNotFound(err) {
			return nil, fail(CodeNotFound, "feedback for message %q not found", messageID)
		}
		return nil, err
	}
	return &fb, nil
}

// Delete 删除一条反馈。
func (s *Store) Delete(ctx context.Context, session brand.SessionID, messageID string) error {
	return s.domain.Delete(ctx, keyOf(session, messageID))
}

// List 列出某会话下全部反馈（按 key 排序）。
func (s *Store) List(ctx context.Context, session brand.SessionID) ([]Feedback, error) {
	keys, err := s.domain.List(ctx)
	if err != nil {
		return nil, err
	}
	prefix := session.Raw() + "::"
	var out []Feedback
	for _, k := range keys {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			fb, _, err := s.domain.Get(ctx, k)
			if err == nil {
				out = append(out, fb)
			}
		}
	}
	return out, nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*storage.ErrKeyNotFound)
	return ok
}