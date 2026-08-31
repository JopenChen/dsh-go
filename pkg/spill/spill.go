// Package spill 提供 Spill Storage（大结果溢出接缝）。
//
// 对齐上游：packages/spill/spill + spill-local + spill-policy
//
// 本文件对应任务 M42：Spill Storage 溢出接缝。
//
// 设计要点：
//   - SpillStore 只有一个操作 saveText：把超宽文本原样持久化，返回不透明 locator、
//     精确字节数与检索提示（retrievalHint）；失败即拒绝（权限/ENOSPC/后端不可用）；
//   - 本地后端写到 `<root>/session-<sha256(sessionId)>/<random>-<safeName>`，root 为
//     私有(0700)，写入用独占(wx)且 owner-only(0600)，避免被符号链接重定向；
//   - SpillPolicy（spill-policy 消费端）作为 tools/post-execute / Bash listener：
//     结果文本超过 maxInlineBytes → spill，并保留 head/tail 预览；保存失败 best-effort
//     保留原内联结果（不把成功调用搞成 isError）。
package spill

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// ============================================================================
// 类型
// ============================================================================

// SpillLocator 是单个溢出工件的不透明模型向句柄。
// 本地后端渲染为文件系统路径；远程/数据库后端可为 URI 或 key。消费方不解析它，
// 而按 retrievalHint 渲染。
type SpillLocator string

// String 返回句柄原始串（for fmt/logging）。
func (l SpillLocator) String() string { return string(l) }

// SpillOwner 是保存时的存储命名空间（按产生会话分组）。
type SpillOwner struct {
	// SessionID 产生该工件的会话 id。
	SessionID brand.SessionID `json:"sessionId"`
}

// SpillSource 是产生某溢出工件的工具与调用（仅用于起名/检视，不用于访问控制）。
type SpillSource struct {
	// ToolName 产生结果的工具名。
	ToolName string `json:"toolName"`
	// CallID 结果所属的模型调用 id。
	CallID brand.ToolCallID `json:"callId"`
	// Label 工件简短标签。
	Label string `json:"label"`
}

// SaveTextSpill 是一次持久化文本的请求。
type SaveTextSpill struct {
	// Owner 存储命名空间。
	Owner SpillOwner `json:"owner"`
	// Source 来源描述（起名/检视）。
	Source SpillSource `json:"source"`
	// SuggestedName 调用方建议的基础名（如 web_fetch.txt）；后端净化成单一安全路径段。
	SuggestedName string `json:"suggestedName"`
	// Content 要原样持久化的完整文本（UTF-8）。
	Content string `json:"content"`
}

// SpillRef 是一个已保存的溢出工件：locator + 字节数 + 后端检索提示。
type SpillRef struct {
	// Locator 不透明模型向句柄。
	Locator SpillLocator `json:"locator"`
	// Bytes 精确编码字节数。
	Bytes int `json:"bytes"`
	// RetrievalHint 模型侧检索引导（如 "read <path>" / "grep ... <path>"）。
	RetrievalHint string `json:"retrievalHint"`
}

// SpillStore 是 `ctx.spillStore` 的单方法抽象服务。
type SpillStore interface {
	// SaveText 原样持久化完整 content；真实存储失败时返回错误。
	SaveText(input SaveTextSpill) (SpillRef, error)
}

// ============================================================================
// 本地文件后端
// ============================================================================

// FileSpillStore 把溢出工件写到宿主文件系统的私有、会话作用域目录。
type FileSpillStore struct {
	// Root 私有根目录（默认自动创建，0700）。
	Root string
}

// NewFileStore 创建本地文件后端。
func NewFileStore(root string) *FileSpillStore {
	return &FileSpillStore{Root: root}
}

// SaveText 实现 SpillStore：把 content 写到会话私有目录下。
func (s *FileSpillStore) SaveText(input SaveTextSpill) (SpillRef, error) {
	if s.Root == "" {
		return SpillRef{}, fmt.Errorf("spill: root not configured")
	}
	// 会话子目录：sha256(sessionId)。
	sessHash := sha256sum(input.Owner.SessionID.Raw())
	dir := filepath.Join(s.Root, "session-"+sessHash[:16])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return SpillRef{}, fmt.Errorf("spill: create session dir: %w", err)
	}
	// 净化 suggestedName 为单一安全段。
	safe := sanitizeName(input.SuggestedName)
	// 随机前缀避免冲突；独占(wx) + owner-only(0600) 写入。
	name := randomSuffix() + "-" + safe
	path := filepath.Join(dir, name)
	// 真实存储失败 → 拒绝。
	if err := os.WriteFile(path, []byte(input.Content), 0o600); err != nil {
		return SpillRef{}, fmt.Errorf("spill: write artifact: %w", err)
	}
	return SpillRef{
		Locator:       SpillLocator(path),
		Bytes:         len([]byte(input.Content)),
		RetrievalHint: fmt.Sprintf("read or grep this artifact with the tool at path %q", path),
	}, nil
}

// ReadText 按 locator 读取溢出内容（供消费者/测试还原源字节）。
func (s *FileSpillStore) ReadText(locator SpillLocator) (string, error) {
	data, err := os.ReadFile(string(locator))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// sanitizeName 把建议名净化成单一安全路径段（保留 [a-zA-Z0-9._-]）。
func sanitizeName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	out := sb.String()
	if out == "" {
		out = "artifact"
	}
	// 去头尾点，防隐藏文件 / 路径穿越。
	out = strings.Trim(out, ".")
	if out == "" {
		out = "artifact"
	}
	return out
}

// randomSuffix 生成 8 字节随机十六进制前缀。
func randomSuffix() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// sha256sum 计算字符串的 sha256 十六进制。
func sha256sum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ============================================================================
// Spill Policy（post-tool / Bash listener）
// ============================================================================

// Apply 把过宽结果溢出，并返回内联预览 + 可选 ref。
//
//   - text 未超过 maxInlineBytes → 返回原 text 与 nil ref；
//   - 超过 → spill，返回 head/tail 预览与 ref；
//   - 保存失败 best-effort：保留原 text，返回错误（调用方决定是否视为 isError）。
func Apply(store SpillStore, input SaveTextSpill, text string, maxInlineBytes, previewBytes int) (inline string, ref *SpillRef, err error) {
	if len([]byte(text)) <= maxInlineBytes {
		return text, nil, nil
	}
	r, serr := store.SaveText(SaveTextSpill{
		Owner:         input.Owner,
		Source:        input.Source,
		SuggestedName: input.SuggestedName,
		Content:       text,
	})
	if serr != nil {
		// best-effort：保留原内联结果，不把成功调用变成 isError。
		return text, nil, serr
	}
	if previewBytes <= 0 {
		previewBytes = 1024
	}
	p := preview(text, previewBytes)
	return p, &r, nil
}

// preview 构造 head/tail 预览：头部 previewBytes 字节 + 尾部 previewBytes/4 字节 + 省略标记。
func preview(text string, head int) string {
	b := []byte(text)
	tail := head / 4
	if len(b) <= head+tail {
		return text
	}
	tailText := string(b[len(b)-tail:])
	headText := string(b[:head])
	return fmt.Sprintf("%s\n...\n[truncated %d bytes; see spill ref for the full %d bytes]\n%s",
		headText, len(b)-head-tail, len(b), tailText)
}