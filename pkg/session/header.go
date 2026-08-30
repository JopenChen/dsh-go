// 本文件对应任务 M06：SessionHeader 元数据。
//
// 对齐上游：packages/core/session（SessionHeader）
//
// SessionHeader 描述会话的静态元数据：格式版本 / ID / 创建时间 / CWD / 父会话
// （fork lineage）/ 种子长度（seed length）/ 来源 / 代理预设等。它是持久化键目录、
// fork 谱系追踪与 subagent 递归深度的唯一依据。
package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// SessionFormatVersion 是当前会话文件格式版本号。
// 任何对事件/头部结构不兼容的改动都必须递增该版本，并拒绝读取旧/新版本。
const SessionFormatVersion = 1

// HeaderOrigin 标识会话来源。
type HeaderOrigin string

// 会话来源枚举。
const (
	OriginCreated HeaderOrigin = "created" // 全新创建
	OriginFork    HeaderOrigin = "fork"    // 由父会话分叉
	OriginResume  HeaderOrigin = "resume"  // 冷恢复
)

// SessionHeader 是会话的静态元数据头部。
type SessionHeader struct {
	// Version 为格式版本号，序列化时写入、反序列化时校验（fail-closed）。
	Version int `json:"version"`
	// ID 为会话唯一标识。
	ID brand.SessionID `json:"id"`
	// CreatedAt 为会话创建时间。
	CreatedAt time.Time `json:"createdAt"`
	// Cwd 为会话工作目录（影响 FS 工具与相对路径解析）。
	Cwd string `json:"cwd,omitempty"`
	// ParentSession 为父会话 ID（fork 谱系；非空表示由某会话分叉而来）。
	ParentSession brand.SessionID `json:"parentSession,omitempty"`
	// SeedLength 为种子长度：Resume/Fork 时已回放（cold stored）的事件数，
	// 用于定位 session/end-seed 之后的 live work 分界。
	SeedLength uint64 `json:"seedLength,omitempty"`
	// Origin 为会话来源（created / fork / resume）。
	Origin HeaderOrigin `json:"origin,omitempty"`
	// DelegationDepth 为 subagent 递归委派深度（根为 0，每层 +1）。
	DelegationDepth int `json:"delegationDepth,omitempty"`
	// AgentPreset 为会话绑定的代理预设名。
	AgentPreset string `json:"agentPreset,omitempty"`
}

// NewSessionHeader 创建全新会话头部（版本当前值 / 来源 created / 深度 0）。
func NewSessionHeader(id brand.SessionID, cwd string) *SessionHeader {
	return &SessionHeader{
		Version:     SessionFormatVersion,
		ID:          id,
		CreatedAt:   time.Now().UTC(),
		Cwd:         cwd,
		Origin:      OriginCreated,
		DelegationDepth: 0,
	}
}

// Marshal 序列化头部为 JSON 字节。
func (h *SessionHeader) Marshal() ([]byte, error) {
	return json.Marshal(h)
}

// UnmarshalSessionHeader 反序列化头部并做 fail-closed 版本校验：
//   - 未知/不支持的版本号 → 返回错误，拒绝读（避免用错误的解析逻辑损坏数据）。
func UnmarshalSessionHeader(data []byte) (*SessionHeader, error) {
	var h SessionHeader
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, fmt.Errorf("session header: unmarshal: %w", err)
	}
	if h.Version != SessionFormatVersion {
		return nil, &SessionFormatUnsupportedError{
			Version: h.Version,
		}
	}
	return &h, nil
}

// Validate 校验头部字段完整性（读取后调用）。
func (h *SessionHeader) Validate() error {
	if h.Version != SessionFormatVersion {
		return &SessionFormatUnsupportedError{Version: h.Version}
	}
	if h.ID.IsZero() {
		return fmt.Errorf("session header: missing session id")
	}
	if h.CreatedAt.IsZero() {
		return fmt.Errorf("session header: missing created-at")
	}
	return nil
}

// Fork 基于当前头部派生一个子会话头部（fork lineage）：
//   - ParentSession = 当前会话 ID；
//   - Origin = fork；DelegationDepth = 父深度 + 1；
//   - SeedLength 由调用方在回放完种子事件后写入。
func (h *SessionHeader) Fork(childID brand.SessionID) *SessionHeader {
	return &SessionHeader{
		Version:         SessionFormatVersion,
		ID:              childID,
		CreatedAt:       time.Now().UTC(),
		Cwd:             h.Cwd,
		ParentSession:   h.ID,
		SeedLength:      0, // 分叉子会话从零开始 live
		Origin:          OriginFork,
		DelegationDepth: h.DelegationDepth + 1,
		AgentPreset:     h.AgentPreset,
	}
}

// SessionFormatUnsupportedError 表示会话格式版本不支持（fail-closed）。
type SessionFormatUnsupportedError struct {
	Version int
}

// Error 实现 error 接口。
func (e *SessionFormatUnsupportedError) Error() string {
	return fmt.Sprintf("session header: unsupported format version %d (want %d)", e.Version, SessionFormatVersion)
}

// AsHeaderOrigin 便捷类型转换（供测试与外部构造）。
func AsHeaderOrigin(s string) HeaderOrigin {
	return HeaderOrigin(s)
}
