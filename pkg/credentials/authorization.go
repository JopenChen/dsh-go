// 本文件实现任务 S06：Authorization Service（OAuth 流 stub）。
//
// 对齐上游：packages/credentials/authorization
//
// 说明：M39 已提供 AuthorizationService 注册原语（Register/List/Begin/Cancel 的 flow
// 载体）。S06 在此基础上补齐「流程生命周期」的 stub 语义：
//   - AuthService 持有一次性回调 token：Begin → 创建 pending session 并返回 token；
//   - Complete(token, value) 模拟「OAuth 回调拿到凭据」：把 value 写入凭证 Store
//     （store.Set），session 进入 completed —— 之后 M39 的 Resolve 即可解析到该凭证；
//   - Cancel 取消 pending session（并调用 flow.Cancel）；
//
// 生产环境接入真实浏览器/SDK 时，只需把 Complete 换成真实回调端点即可，本 stub
// 保持接口稳定。
package credentials

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// ============================================================================
// 会话状态
// ============================================================================

// AuthStatus 是授权会话状态。
type AuthStatus string

// 授权会话状态枚举。
const (
	AuthPending   AuthStatus = "pending"   // 等待回调
	AuthCompleted AuthStatus = "completed" // 凭证已写入
	AuthCancelled AuthStatus = "cancelled" // 已取消
)

// AuthSession 是一条授权流程会话记录。
type AuthSession struct {
	ID        string       `json:"id"`
	FlowID    FlowID       `json:"flowId"`
	Status    AuthStatus   `json:"status"`
	StartedAt time.Time    `json:"startedAt"`
	CredRef   brand.CredentialRef `json:"credRef,omitempty"` // 回调写入的凭证引用
	HasValue  bool         `json:"hasValue"`
}

// ============================================================================
// AuthService：OAuth 流 stub
// ============================================================================

// AuthService 编排「begin → 回调 → resolved credential」的授权流程 stub。
type AuthService struct {
	auth     *AuthorizationService
	store    *Store
	mu       sync.Mutex
	sessions map[string]*AuthSession // sessionID → 状态
	next     int
}

// NewAuthService 创建授权服务 stub，绑定底层 flow 注册表与凭证存储。
func NewAuthService(auth *AuthorizationService, store *Store) *AuthService {
	return &AuthService{
		auth:     auth,
		store:    store,
		sessions: map[string]*AuthSession{},
	}
}

// newSessionID 生成自增会话 ID。
func (s *AuthService) newSessionID() string {
	s.next++
	return fmt.Sprintf("auth-%04d", s.next)
}

// Begin 启动指定授权流，返回一次性回调 token。
// 适用范围：list flows → begin(token) → 外部完成 → Complete(token, value)。
func (s *AuthService) Begin(ctx context.Context, flowID FlowID) (token string, err error) {
	// 触发 flow 的 Begin（得到授权提示/URL），失败则整体失败。
	hint, err := s.auth.Begin(ctx, flowID)
	if err != nil {
		return "", err
	}
	_ = hint

	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.newSessionID()
	sess := &AuthSession{ID: id, FlowID: flowID, Status: AuthPending, StartedAt: time.Now()}
	s.sessions[id] = sess
	return id, nil
}

// Complete 模拟 OAuth 回调：把 value 写入凭证 Store，会话进入 completed。
// 后续 M39 的 Resolve(flowID 对应的 ref) 即可取到 value。
func (s *AuthService) Complete(ctx context.Context, token string, value string) error {
	s.mu.Lock()
	sess, ok := s.sessions[token]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("credentials: auth session %q not found", token)
	}
	if sess.Status != AuthPending {
		s.mu.Unlock()
		return fmt.Errorf("credentials: auth session %q already %s", token, sess.Status)
	}
	// 凭证引用 = flow ID（约定）。
	ref := sess.FlowID
	s.mu.Unlock()

	// 写入凭证存储（CAS），成功后标记 completed。
	if err := s.store.Set(ctx, ref, value); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.Status = AuthCompleted
	sess.CredRef = ref
	sess.HasValue = true
	return nil
}

// Cancel 取消 pending 会话（并调用 flow.Cancel）。
func (s *AuthService) Cancel(ctx context.Context, token string) error {
	s.mu.Lock()
	sess, ok := s.sessions[token]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("credentials: auth session %q not found", token)
	}
	if sess.Status != AuthPending {
		s.mu.Unlock()
		return fmt.Errorf("credentials: auth session %q already %s", token, sess.Status)
	}
	flowID := sess.FlowID
	sess.Status = AuthCancelled
	s.mu.Unlock()

	// 调用底层 flow 的 Cancel（stub 可无操作）。
	return s.auth.Cancel(ctx, flowID)
}

// Status 返回指定会话状态。
func (s *AuthService) Status(token string) (AuthSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	if !ok {
		return AuthSession{}, false
	}
	cp := *sess
	return cp, true
}

// List 列出全部会话（按 ID 字典序，确定性）。
func (s *AuthService) List() []AuthSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AuthSession, 0, len(s.sessions))
	for _, v := range s.sessions {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}