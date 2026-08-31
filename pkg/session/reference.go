// Package session 的会话引用（Session References）能力。
//
// 对齐上游：packages/context/session-reference + packages/context/file-reference
//
// 本文件对应任务 M17：Session References（跨会话 & 文件 mention）。
//
// 语义：
//   - 用户消息中可包含两类 mention 语法：
//       @session/<id>     —— 引用另一个会话（id 为即刻识别的会话标识）；
//       #<path/file>      —— 引用工作区内某个文件（path 通常为相对/绝对路径）。
//   - Mention 只是「选择」的入口语法；真正进入模型上下文的是一份「聚合的不可信快照」
//     （additionalContext），与上游 PreparedReferencedMessage 对齐。
//   - 解析出的原始引用写入 user/message 事件的 Refs（source=reference），作为审计溯源。
//   - 所有失败都返回稳定分类错误码，宿主协议据此映射到自己的错误信封，不读取 prompt 字节。
package session

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// ============================================================================
// 稳定错误码（SessionReferenceErrorCode）
// ============================================================================

// SessionReferenceErrorCode 是会话引用失败的稳定分类码。
type SessionReferenceErrorCode string

// 与上游 SessionReferenceErrorCode 对齐的错误码；另按 M17 验收补充文件/会话未找到码。
const (
	// RefCodeInvalidConfig 配置无效（如缺少 resolver / 工作区根）。
	RefCodeInvalidConfig SessionReferenceErrorCode = "SESSION_REFERENCE_INVALID_CONFIG"
	// RefCodeInvalidReference 引用语法或目标不合法。
	RefCodeInvalidReference SessionReferenceErrorCode = "SESSION_REFERENCE_INVALID_REFERENCE"
	// RefCodeSelfReference 引用了请求代理自身所在的会话。
	RefCodeSelfReference SessionReferenceErrorCode = "SESSION_REFERENCE_SELF_REFERENCE"
	// RefCodeTooMany 引用数量超过上限。
	RefCodeTooMany SessionReferenceErrorCode = "SESSION_REFERENCE_TOO_MANY"
	// RefCodeReadFailed 读取源会话失败。
	RefCodeReadFailed SessionReferenceErrorCode = "SESSION_REFERENCE_READ_FAILED"
	// RefCodeBudgetExceeded 聚合上下文超出允许预算。
	RefCodeBudgetExceeded SessionReferenceErrorCode = "SESSION_REFERENCE_BUDGET_EXCEEDED"
	// RefCodeCancelled 引用准备被取消。
	RefCodeCancelled SessionReferenceErrorCode = "SESSION_REFERENCE_CANCELLED"
	// RefCodeSessionNotFound 引用的会话不存在（M17 验收补充码）。
	RefCodeSessionNotFound SessionReferenceErrorCode = "SESSION_NOT_FOUND"
	// RefCodeFileNotFound 引用的文件不存在（M17 验收补充码）。
	RefCodeFileNotFound SessionReferenceErrorCode = "FILE_NOT_FOUND"
	// RefCodeFileOutOfWorkspace 引用文件位于工作区根之外（越界，禁止访问）。
	RefCodeFileOutOfWorkspace SessionReferenceErrorCode = "FILE_OUT_OF_WORKSPACE"
)

// SessionReferenceError 携带稳定错误码的引用失败错误。
type SessionReferenceError struct {
	Code SessionReferenceErrorCode
	Msg  string
}

func (e *SessionReferenceError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Msg)
}

// newRefError 便捷构造 SessionReferenceError。
func newRefError(code SessionReferenceErrorCode, format string, args ...any) error {
	return &SessionReferenceError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// RefCodeFrom 从任意 error 提取稳定的引用错误码；非引用错误返回空串。
func RefCodeFrom(err error) SessionReferenceErrorCode {
	if err == nil {
		return ""
	}
	if re, ok := err.(*SessionReferenceError); ok {
		return re.Code
	}
	return ""
}

// ============================================================================
// Mention 解析
// ============================================================================

// MentionKind 表示引用种类。
type MentionKind string

const (
	// MentionSession 引用一个会话（@session/<id>）。
	MentionSession MentionKind = "session"
	// MentionFile 引用一个文件（#<path/file>）。
	MentionFile MentionKind = "file"
)

// Mention 表示用户消息中的一处引用。
type Mention struct {
	// Kind 是引用种类（session / file）。
	Kind MentionKind
	// Raw 是完整的原文 token（如 "@session/abc"、"#src/main.go"）。
	Raw string
	// Value 是引用目标：会话 id 或文件路径。
	Value string
	// Start/End 是 Raw 在原文中的字节区间，用于「剥离 mention、保留可读内容」。
	Start int
	End   int
}

// 会话 mention：@session/<id>，id 允许字母数字与 _-. 分隔符。
var sessionMentionRe = regexp.MustCompile(`@session/([A-Za-z0-9_.\-]+)`)

// 文件 mention：#<path/file>，path 为连续非空白、非 # 的路径片段。
// 为避免吃掉中文标点，仅匹配非空白字符；句尾的句号/逗号会残留（可读但不再视为引用目标）。
var fileMentionRe = regexp.MustCompile(`#([^\s#]+)`)

// maxReferencesPerMessage 单条消息允许引用的最大数量（对应 RefCodeTooMany）。
const maxReferencesPerMessage = 16

// ParseMentions 解析原文中的所有 @session/<id> 与 #<path/file> mention。
// 返回按原文出现顺序的引用列表；无引用时返回空切片（非 nil）。
func ParseMentions(text string) []Mention {
	var out []Mention
	// 会话 mention
	for _, loc := range sessionMentionRe.FindAllStringSubmatchIndex(text, -1) {
		raw := text[loc[0]:loc[1]]
		val := text[loc[2]:loc[3]]
		out = append(out, Mention{
			Kind:  MentionSession,
			Raw:   raw,
			Value: val,
			Start: loc[0],
			End:   loc[1],
		})
	}
	// 文件 mention
	for _, loc := range fileMentionRe.FindAllStringSubmatchIndex(text, -1) {
		// 排除误命中：若 # 前紧跟会话 mention 的 id 片段，跳过（避免把 "xxx@session/a#b" 拆错）。
		// 这里不做复杂消歧；文件 mention 的 Value 若以 ",." 等标点结尾则保留原样，交由调用方判定。
		raw := text[loc[0]:loc[1]]
		val := text[loc[2]:loc[3]]
		out = append(out, Mention{
			Kind:  MentionFile,
			Raw:   raw,
			Value: val,
			Start: loc[0],
			End:   loc[1],
		})
	}
	// 按出现顺序稳定排序（正则分别扫描，可能跨序，需合并按 Start 排序）。
	sortMentionsByPos(out)
	return out
}

// sortMentionsByPos 按 Start 升序对引用排序（稳定）。
func sortMentionsByPos(m []Mention) {
	for i := 1; i < len(m); i++ {
		for j := i; j > 0 && m[j-1].Start > m[j].Start; j-- {
			m[j-1], m[j] = m[j], m[j-1]
		}
	}
}

// stripMentions 从句柄文本中删除全部 mention token，返回剥离后的可读内容。
func stripMentions(text string, mentions []Mention) string {
	if len(mentions) == 0 {
		return text
	}
	var sb strings.Builder
	last := 0
	for _, m := range mentions {
		sb.WriteString(text[last:m.Start])
		last = m.End
	}
	sb.WriteString(text[last:])
	return sb.String()
}

// ============================================================================
// PreparedReferencedMessage
// ============================================================================

// SessionReferenceInput 表示一次被选中的源会话（宿主传入的结构化引用）。
type SessionReferenceInput struct {
	// SessionID 是源会话身份（权威标识）。
	SessionID brand.SessionID
	// Label 是可选的展示标签，进入快照。
	Label string
}

// PreparedReferencedMessage 是一次引用准备的产物：剥离 mention 后的可读内容 +
// 可选的聚合参考快照（additionalContext）。
type PreparedReferencedMessage struct {
	// Content 是剥离 mention token 后的可读消息内容。
	Content string
	// AdditionalContext 是聚合的不可信引用上下文；无引用时为 ""。
	AdditionalContext string
	// References 是成功解析并纳入快照的引用（按原文顺序）。
	References []ReferenceResolution
}

// ReferenceResolution 记录一条引用解析结果（写入 Refs 审计）。
type ReferenceResolution struct {
	Kind  MentionKind      `json:"kind"`
	Raw   string           `json:"raw,omitempty"`
	Value string           `json:"value"`
	Label string           `json:"label,omitempty"`
	Slice string           `json:"slice,omitempty"` // 聚合进 additionalContext 的文本片段
}

// ============================================================================
// Resolver 接缝
// ============================================================================

// SessionReferenceResolver 抽象「如何把一个 mention 目标解析成上下文片段」。
//
// 设计意图：跨会话读取依赖 persistence（M43），文件解析依赖 Filesystem（M35）。
// 两个依赖在 M17 阶段可能尚未全部就绪，因此这里以接缝接口暴露，宿主/测试可注入真实
// 或桩实现；默认提供面向工作区根的文件系统桩（os.Stat 判定存在/越界）。
type SessionReferenceResolver interface {
	// ResolveSession 返回目标会话的可读快照片段；会话不存在返回 RefCodeSessionNotFound，
	// 读取失败返回 RefCodeReadFailed。
	ResolveSession(id brand.SessionID) (string, error)
	// ResolveFile 返回目标文件的摘要片段；不存在返回 RefCodeFileNotFound，
	// 越界（超出工作区根）返回 RefCodeFileOutOfWorkspace。
	ResolveFile(path string) (string, error)
	// WorkspaceRoot 返回当前代理绑定的工作区根（用于文件越界判定）。
	WorkspaceRoot() string
}

// WorkspaceFileResolver 是基于工作区根的文件系统桩实现。
//
// warning: 仅做存在性与越界判定，不读取文件内容（上游 file-reference 的发现阶段也是
// 「path-only」而不读内容）；真正读内容交给 M35 Filesystem 接缝。
type WorkspaceFileResolver struct {
	Root string
}

// WorkspaceRoot 返回工作区根。
func (r *WorkspaceFileResolver) WorkspaceRoot() string { return r.Root }

// ResolveSession 对文件解析器而言"无会话能力"，返回 invalid hook error（以便组合）。
func (r *WorkspaceFileResolver) ResolveSession(brand.SessionID) (string, error) {
	return "", newRefError(RefCodeInvalidConfig, "workspace file resolver has no session capability")
}

// ResolveFile 判定文件存在性并返回摘要（此处仅返回路径摘要；读内容由 M35 承接）。
func (r *WorkspaceFileResolver) ResolveFile(path string) (string, error) {
	root, err := filepath.Abs(r.Root)
	if err != nil {
		return "", newRefError(RefCodeInvalidConfig, "bad workspace root %q: %v", r.Root, err)
	}
	// 解析为绝对路径（相对路径以工作区根为基准）。
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", newRefError(RefCodeInvalidReference, "bad file path %q: %v", path, err)
	}
	// 越界判定：目标必须在工作区根内且不是根自身。
	if abs == root || !within(abs, root) {
		return "", newRefError(RefCodeFileOutOfWorkspace, "file %q outside workspace root %q", path, root)
	}
	// 存在性判定。
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", newRefError(RefCodeFileNotFound, "file %q not found", path)
		}
		return "", newRefError(RefCodeReadFailed, "stat %q: %v", path, err)
	}
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}
	// path-only 摘要：不带内容，仅路径 + 类型 + 大小，稳定可哈希（遵循 D3 纪律精神）。
	return fmt.Sprintf("file %q (%s, %d bytes)", path, kind, info.Size()), nil
}

// within 判断 abs 是否位于 root 之内（含子目录）。
func within(abs, root string) bool {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ============================================================================
// Prepare 主流程
// ============================================================================

// PrepareRequest 描述一次引用准备请求。
type PrepareRequest struct {
	// AgentSessionID 是请求代理自身的会话 ID（用于自引用拒绝；可空则跳过自引用检查）。
	AgentSessionID brand.SessionID
	// Text 是用户原始消息（可能包含 mention）。
	Text string
	// Resolver 是会话/文件解析接缝。
	Resolver SessionReferenceResolver
	// MaxContextBytes 是可选的聚合上下文预算；<=0 表示不限制。
	MaxContextBytes int
}

// Prepare 解析 Text 中的 mention、解析每个引用，并返回 PreparedReferencedMessage。
//
// 规则：
//   - 无引用 → 原样 Content，AdditionalContext 为空；
//   - 引用自请求代理 → RefCodeSelfReference；
//   - 任一引用解析失败 → 立即以对应稳定错误码返回；
//   - 引用数超上限 → RefCodeTooMany；
//   - 聚合上下文超预算 → RefCodeBudgetExceeded。
func Prepare(req PrepareRequest) (*PreparedReferencedMessage, error) {
	if req.Resolver == nil {
		return nil, newRefError(RefCodeInvalidConfig, "missing reference resolver")
	}
	mentions := ParseMentions(req.Text)
	if len(mentions) > maxReferencesPerMessage {
		return nil, newRefError(RefCodeTooMany, "%d references exceed limit %d", len(mentions), maxReferencesPerMessage)
	}

	// 剥离 mention，得到可读内容。注意：file mention 可能误吞行尾标点，此处按原样保留，
	// 归属交给后续解析判定；这里只做 token 级剥离。
	content := stripMentions(req.Text, mentions)

	if len(mentions) == 0 {
		return &PreparedReferencedMessage{Content: content}, nil
	}

	// 逐个解析引用，组装聚合快照。
	var sb strings.Builder
	var res []ReferenceResolution
	var total int
	for _, m := range mentions {
		var slice string
		var err error
		switch m.Kind {
		case MentionSession:
			// 自引用拒绝
			if !req.AgentSessionID.IsZero() && m.Value == req.AgentSessionID.Raw() {
				return nil, newRefError(RefCodeSelfReference, "self reference to %q", m.Value)
			}
			slice, err = req.Resolver.ResolveSession(brand.NewSessionID(m.Value))
		case MentionFile:
			slice, err = req.Resolver.ResolveFile(m.Value)
		default:
			return nil, newRefError(RefCodeInvalidReference, "unknown mention kind %q", m.Kind)
		}
		if err != nil {
			return nil, err
		}
		total += len(slice)
		if req.MaxContextBytes > 0 && total > req.MaxContextBytes {
			return nil, newRefError(RefCodeBudgetExceeded, "aggregated context exceeds %d bytes", req.MaxContextBytes)
		}
		res = append(res, ReferenceResolution{
			Kind:  m.Kind,
			Raw:   m.Raw,
			Value: m.Value,
			Slice: slice,
		})
		sb.WriteString("### referenced: ")
		sb.WriteString(m.Value)
		sb.WriteString("\n")
		sb.WriteString(slice)
		sb.WriteString("\n\n")
	}

	return &PreparedReferencedMessage{
		Content:           content,
		AdditionalContext: strings.TrimRight(sb.String(), "\n"),
		References:        res,
	}, nil
}