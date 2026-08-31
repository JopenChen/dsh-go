// Package workspace 提供工作区注册表（任务 S14：Workspace Registry）。
//
// 对齐上游：packages/core/workspace
//
// 设计要点：
//   - Workspace 记录一条工作区信息：{ID / Root / SessionGroup / ResumeOnOpen}；
//   - 通过 storage.Domain（Storage Domain KV 抽象，M45）持久化，key=工作区 ID；
//   - ID 是「确定性」的：由规范化后的根路径经 sha256 派生而来，因此**同一 root
//     路径创建两次会返回同一 ID**（幂等），满足验收"相同 root 返回同一 id"；
//   - ResumeOnOpen 记录该工作区下次默认打开的会话（resume-on-open 语义）；
//   - 全部写操作走 CAS，调用方拿到 version 后可做并发保护。
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/storage"
)

// ============================================================================
// 类型
// ============================================================================

// Workspace 是一条工作区记录。
type Workspace struct {
	ID            brand.WorkspaceID `json:"id"`
	Root          string            `json:"root"`                    // 规范化绝对路径
	SessionGroup  string            `json:"sessionGroup,omitempty"`  // 会话分组标识（可选）
	ResumeOnOpen  *brand.SessionID  `json:"resumeOnOpen,omitempty"`  // 下次默认打开的会话（nil=不恢复）
}

// ============================================================================
// Registry
// ============================================================================

// Registry 是工作区注册表（基于 storage.Domain，线程安全由后端保证）。
type Registry struct {
	domain *storage.Domain[Workspace]
}

// New 创建工作区注册表，挂载到指定 KV 后端。
func New(backend storage.Backend) *Registry {
	return &Registry{domain: storage.NewDomain[Workspace](backend, "workspace/")}
}

// normalize 归一化根路径：去掉首尾空白并转绝对/清理格式。
func normalize(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("workspace: empty root path")
	}
	// 转绝对路径并清理冗余分隔符（相对路径基于 CWD）。
	abs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("workspace: resolve root %q: %w", root, err)
	}
	return abs, nil
}

// idFromRoot 由规范化根路径派生确定性工作区 ID（同一 root → 同一 id）。
func idFromRoot(normalizedRoot string) brand.WorkspaceID {
	sum := sha256.Sum256([]byte(normalizedRoot))
	return brand.NewWorkspaceID("ws_" + hex.EncodeToString(sum[:12]))
}

// Create 登记（或复用）一个工作区。相同 root 返回同一 ID。
// 返回 version（CAS 版本）与是否新建。
func (r *Registry) Create(ctx context.Context, root string) (brand.WorkspaceID, uint64, error) {
	normalized, err := normalize(root)
	if err != nil {
		return brand.WorkspaceID{}, 0, err
	}
	id := idFromRoot(normalized)

	// 已存在 → 直接复用（幂等）。
	if ws, ver, gerr := r.domain.Get(ctx, id.Raw()); gerr == nil {
		_ = ws
		return id, ver, nil
	} else if !storage.IsKeyNotFound(gerr) {
		return brand.WorkspaceID{}, 0, gerr
	}

	// 不存在 → CAS 新建。
	rec := Workspace{ID: id, Root: normalized}
	ver, err := r.domain.Put(ctx, id.Raw(), rec, 0)
	if err != nil {
		return brand.WorkspaceID{}, 0, fmt.Errorf("workspace: create %q: %w", normalized, err)
	}
	return id, ver, nil
}

// Get 按 ID 读取工作区。不存在返回 ErrKeyNotFound 包装错误。
func (r *Registry) Get(ctx context.Context, id brand.WorkspaceID) (Workspace, uint64, error) {
	if id.IsZero() {
		return Workspace{}, 0, fmt.Errorf("workspace: missing id")
	}
	return r.domain.Get(ctx, id.Raw())
}

// List 返回全部工作区（按 ID 字典序，确定性）。
func (r *Registry) List(ctx context.Context) ([]Workspace, error) {
	keys, err := r.domain.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []Workspace
	for _, k := range keys {
		ws, _, err := r.domain.Get(ctx, k)
		if err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, nil
}

// SetSessionGroup 设置工作区会话分组（整体替换当前分组）。
func (r *Registry) SetSessionGroup(ctx context.Context, id brand.WorkspaceID, group string) (uint64, error) {
	return r.update(ctx, id, func(ws *Workspace) {
		ws.SessionGroup = group
	})
}

// SetResumeOnOpen 记录该工作区默认打开的会话（resume-on-open）。
func (r *Registry) SetResumeOnOpen(ctx context.Context, id brand.WorkspaceID, sid brand.SessionID) (uint64, error) {
	return r.update(ctx, id, func(ws *Workspace) {
		cp := sid
		ws.ResumeOnOpen = &cp
	})
}

// ClearResumeOnOpen 清除 resume-on-open 记录。
func (r *Registry) ClearResumeOnOpen(ctx context.Context, id brand.WorkspaceID) (uint64, error) {
	return r.update(ctx, id, func(ws *Workspace) {
		ws.ResumeOnOpen = nil
	})
}

// update 读取→修改→CAS 写回；expectedVersion 用读到的 version，冲突即失败。
func (r *Registry) update(ctx context.Context, id brand.WorkspaceID, mutate func(*Workspace)) (uint64, error) {
	if id.IsZero() {
		return 0, fmt.Errorf("workspace: missing id")
	}
	ws, ver, err := r.domain.Get(ctx, id.Raw())
	if err != nil {
		return 0, err
	}
	mutate(&ws)
	return r.domain.Put(ctx, id.Raw(), ws, ver)
}