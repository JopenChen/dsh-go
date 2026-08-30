// Package credentials 提供凭证（Credentials）与授权流（Authorization Flow）接缝。
//
// 对齐上游：packages/credentials/credentials + credentials-local + authorization
//
// 设计要点：
//   - CredentialRef 使用品牌化类型（POSIX 变量名，如 OPENAI_API_KEY），每轮 LLM 请求
//     调用 Resolve 一次读取当前值，修改 env 后下一轮请求即看到新值；
//   - 凭证记录通过 Storage Domain 持久化，修改携带 CAS 版本（record modify CAS），
//     并发写冲突会被拒绝；
//   - describe() 只暴露「是否有值 + 是否可写」，绝不输出明文；
//   - AuthorizationFlow 注册后可 list/begin/cancel（OAuth 等外部流程的 stub 载体，
//     S06 提供完整 stub 实现）。
package credentials

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/storage"
)

// ============================================================================
// Store：凭证存储
// ============================================================================

// envRecord 是持久化的凭证记录结构。
type envRecord struct {
	// Values 为 CredentialRef → 值 的映射。
	Values map[string]string `json:"values"`
}

// Store 是凭证存储：内存缓存 + Storage Domain CAS 持久化。
type Store struct {
	mu       sync.Mutex
	domain   *storage.Domain[envRecord]
	writable map[string]bool // 记录哪些 ref 可写
}

// NewStore 创建凭证存储，backend 用于持久化（CAS 语义）。
func NewStore(backend storage.Backend) *Store {
	return &Store{
		domain:   storage.NewDomain[envRecord](backend, "credentials"),
		writable: map[string]bool{},
	}
}

// NewMemoryStore 创建基于内存后端的凭证存储（测试/临时场景）。
func NewMemoryStore() *Store {
	return NewStore(storage.NewMemoryKV())
}

// load 读取当前记录（不存在则返回空记录）。
func (s *Store) load() (envRecord, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, version, err := s.domain.Get(context.Background(), "env")
	if err != nil {
		if storage.IsKeyNotFound(err) {
			return envRecord{Values: map[string]string{}}, 0
		}
		return envRecord{Values: map[string]string{}}, 0
	}
	if rec.Values == nil {
		rec.Values = map[string]string{}
	}
	return rec, version
}

// Resolve 每请求解析一次 CredentialRef 的当前值（LLM 请求前调用）。
func (s *Store) Resolve(ctx context.Context, ref brand.CredentialRef) (string, bool) {
	rec, _ := s.load()
	v, ok := rec.Values[ref.Raw()]
	return v, ok
}

// Set 设置凭证（CAS：expectedVersion 来自最近一次 load；不匹配则失败）。
func (s *Store) Set(ctx context.Context, ref brand.CredentialRef, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, version, err := s.domain.Get(ctx, "env")
	if err != nil && !storage.IsKeyNotFound(err) {
		return err
	}
	if rec.Values == nil {
		rec.Values = map[string]string{}
	}
	rec.Values[ref.Raw()] = value
	if _, err := s.domain.Put(ctx, "env", rec, version); err != nil {
		return err
	}
	s.writable[ref.Raw()] = true
	return nil
}

// Unset 删除凭证（CAS）。
func (s *Store) Unset(ctx context.Context, ref brand.CredentialRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, version, err := s.domain.Get(ctx, "env")
	if err != nil && !storage.IsKeyNotFound(err) {
		return err
	}
	if rec.Values == nil {
		return nil
	}
	delete(rec.Values, ref.Raw())
	delete(s.writable, ref.Raw())
	_, err = s.domain.Put(ctx, "env", rec, version)
	return err
}

// CredentialInfo 是 describe 输出的单条凭证描述（不含明文）。
type CredentialInfo struct {
	Ref      brand.CredentialRef `json:"ref"`
	Writable bool                `json:"writable"`
	HasValue bool                `json:"hasValue"`
}

// Describe 输出凭证清单：只暴露 ref + writable + 是否有值，绝不输出明文。
func (s *Store) Describe(ctx context.Context) []CredentialInfo {
	rec, _ := s.load()
	refs := make([]string, 0, len(rec.Values))
	for k := range rec.Values {
		refs = append(refs, k)
	}
	sort.Strings(refs)
	out := make([]CredentialInfo, 0, len(refs))
	for _, k := range refs {
		ref := brand.NewCredentialRef(k)
		_, has := rec.Values[k]
		out = append(out, CredentialInfo{Ref: ref, Writable: s.writable[k], HasValue: has})
	}
	return out
}

// ============================================================================
// AuthorizationFlow & AuthorizationService
// ============================================================================

// FlowID 是授权流的品牌化 ID。
type FlowID = brand.CredentialRef

// Flow 描述一个授权流（如 OAuth 授权码流）。
type Flow struct {
	// ID 唯一标识。
	ID FlowID
	// Name 人类可读名称。
	Name string
	// Begin 开始授权流，返回可用于完成授权的提示/URL（stub 可返回占位）。
	Begin func(ctx context.Context) (string, error)
	// Cancel 取消授权流。
	Cancel func(ctx context.Context) error
}

// AuthorizationService 是授权流注册中心。
type AuthorizationService struct {
	mu    sync.RWMutex
	flows map[string]*Flow
}

// NewAuthorizationService 创建授权服务。
func NewAuthorizationService() *AuthorizationService {
	return &AuthorizationService{flows: map[string]*Flow{}}
}

// Register 注册一个授权流。
func (a *AuthorizationService) Register(f *Flow) error {
	if f == nil || f.ID.Raw() == "" {
		return fmt.Errorf("credentials: flow with empty id cannot register")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.flows[f.ID.Raw()] = f
	return nil
}

// List 列出全部已注册授权流。
func (a *AuthorizationService) List() []*Flow {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*Flow, 0, len(a.flows))
	for _, f := range a.flows {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.Raw() < out[j].ID.Raw() })
	return out
}

// Begin 启动指定授权流。
func (a *AuthorizationService) Begin(ctx context.Context, id FlowID) (string, error) {
	a.mu.RLock()
	f, ok := a.flows[id.Raw()]
	a.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("credentials: flow %q not found", id.Raw())
	}
	if f.Begin == nil {
		return "", fmt.Errorf("credentials: flow %q has no begin handler", id.Raw())
	}
	return f.Begin(ctx)
}

// Cancel 取消指定授权流。
func (a *AuthorizationService) Cancel(ctx context.Context, id FlowID) error {
	a.mu.RLock()
	f, ok := a.flows[id.Raw()]
	a.mu.RUnlock()
	if !ok {
		return fmt.Errorf("credentials: flow %q not found", id.Raw())
	}
	if f.Cancel == nil {
		return nil
	}
	return f.Cancel(ctx)
}
