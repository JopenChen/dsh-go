// Package brand 提供"品牌化（Branded）"类型封装，避免跨包契约中使用裸字符串 / []byte 混传。
//
// 设计动机（对齐 deepseek-harness 上游 packages/util/id-brand 与 packages/util/bytes）：
//   - 事件溯源 Session 中充斥着大量 ID（SessionID / ToolCallID / ApprovalRequestID ...），
//     若全部使用裸 string，编译器无法阻止「把 SessionID 传给期望 ToolCallID 的参数」这类低级错误；
//   - 通过泛型 Branded[T] + 空标签类型，将语义信息编码进类型系统，
//     任何误传在编译期即报错，运行时不可能出现。
//
// 使用方式：
//   - 每个具体品牌类型是 Branded[tag] 的类型别名（如 type SessionID = Branded[sessionIDTag]），
//     因此自动继承 IsZero/String/MarshalJSON/UnmarshalJSON/Value/Scan 等方法；
//   - 外部包通过「具体构造函数」构造（NewSessionID / ParseSessionID ...），
//     不可直接写 brand.New[brand.SessionID]（会导致 Branded 套 Branded 的双重包裹）。
//
// 该包是 dsh-go 全工程的类型地基：后续 session / agent / tools / persistence 等包
// 的跨包契约参数一律使用本包定义的具体品牌类型，不接受裸 string / []byte。
package brand

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// ============================================================================
// 品牌标签（Brand Tag）定义
// ============================================================================
//
// 每个标签是一个空结构体，不携带任何运行时数据，仅在类型系统中扮演"品牌标识"角色。
// 标签本身未导出，外部包只能通过具体品牌类型与构造函数使用，从根上杜绝误用。

type sessionIDTag struct{}
type toolCallIDTag struct{}
type approvalRequestIDTag struct{}
type jobIDTag struct{}
type skillIDTag struct{}
type attachmentIDTag struct{}
type credentialRefTag struct{}
type workspaceIDTag struct{}
type projectionIDTag struct{}

// ============================================================================
// Branded[T] 品牌化字符串载体（泛型核心）
// ============================================================================

// Branded 是品牌化字符串类型的通用载体。
//
// 类型参数 T 仅作为"品牌标签"，用于编译期区分不同种类的 ID：
//   - Branded[sessionIDTag] 与 Branded[toolCallIDTag] 是两种完全不同的类型；
//   - 把 SessionID 传给期望 ToolCallID 的参数会在编译期报错。
//
// 内部 value 字段未导出，外界只能通过各品牌的具体构造函数 / UnmarshalJSON 构造，
// 无法绕过品牌封装直接写入裸字符串。
type Branded[T comparable] struct {
	value string
}

// newBranded 内部通用构造：从原始字符串构造品牌化 ID（不校验内容）。
func newBranded[T comparable](raw string) Branded[T] {
	return Branded[T]{value: raw}
}

// parseBranded 内部通用解析：非空字符串才合法；空串返回错误。
func parseBranded[T comparable](raw string) (Branded[T], error) {
	if raw == "" {
		return Branded[T]{}, fmt.Errorf("brand: empty id for %T", *new(T))
	}
	return Branded[T]{value: raw}, nil
}

// IsZero 判断是否为零值（未赋值 / 空串）。
func (b Branded[T]) IsZero() bool {
	return b.value == ""
}

// String 返回底层字符串，实现 fmt.Stringer 接口，便于日志打印与模板渲染。
func (b Branded[T]) String() string {
	return b.value
}

// Raw 返回底层原始字符串。仅在需要与外部裸 string 边界交互时使用（如写日志/拼文件路径），
// 跨包契约参数仍应使用品牌类型本身。
func (b Branded[T]) Raw() string {
	return b.value
}

// Equal 比较两个同品牌 ID 是否相等。不同品牌类型之间不存在 Equal 方法（编译期隔离）。
func (b Branded[T]) Equal(other Branded[T]) bool {
	return b.value == other.value
}

// MarshalJSON 将品牌 ID 序列化为 JSON 字符串。
func (b Branded[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.value)
}

// UnmarshalJSON 从 JSON 字符串反序列化回品牌 ID。
func (b *Branded[T]) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("brand: invalid json for %T: %w", *new(T), err)
	}
	b.value = s
	return nil
}

// Value 实现 database/sql 的 driver.Valuer，便于持久化层（JSONL / SQLite）直接读写。
func (b Branded[T]) Value() (driver.Value, error) {
	return b.value, nil
}

// Scan 实现 database/sql 的 sql.Scanner，支持从 string / []byte / nil 扫描。
func (b *Branded[T]) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		b.value = ""
	case string:
		b.value = v
	case []byte:
		b.value = string(v)
	default:
		return fmt.Errorf("brand: cannot scan %T into %T", src, b)
	}
	return nil
}

// ============================================================================
// 具体品牌类型（类型别名 + 标签组合）
// ============================================================================

// SessionID 会话 ID（事件溯源 SessionLog 的唯一标识）。
type SessionID = Branded[sessionIDTag]

// ToolCallID 工具调用 ID（一条 tool/call 事件到其 tool/result 的配对键）。
type ToolCallID = Branded[toolCallIDTag]

// ApprovalRequestID 审批请求 ID（ask 类审批一次请求的唯一标识）。
type ApprovalRequestID = Branded[approvalRequestIDTag]

// JobID 后台任务 ID（Jobs Runtime 的 Job 唯一标识）。
type JobID = Branded[jobIDTag]

// SkillID 技能 ID（Skills 6 层注册表中某个技能的唯一标识）。
type SkillID = Branded[skillIDTag]

// AttachmentID 附件 ID（Attachment 存储中的图片/文件唯一标识）。
type AttachmentID = Branded[attachmentIDTag]

// CredentialRef 凭证引用（POSIX 变量名品牌，如 OPENAI_API_KEY）。
type CredentialRef = Branded[credentialRefTag]

// WorkspaceID 工作区 ID（Workspace Registry 中某个目录记录的标识）。
type WorkspaceID = Branded[workspaceIDTag]

// ProjectionID 投影 ID（Session Projections 注册中心中投影定义的标识）。
type ProjectionID = Branded[projectionIDTag]

// ============================================================================
// 具体构造函数（外部包唯一构造入口）
// ============================================================================

// NewSessionID 从原始字符串构造会话 ID。
func NewSessionID(raw string) SessionID { return newBranded[sessionIDTag](raw) }

// ParseSessionID 解析并校验会话 ID，空串返回错误。
func ParseSessionID(raw string) (SessionID, error) { return parseBranded[sessionIDTag](raw) }

// NewToolCallID 从原始字符串构造工具调用 ID。
func NewToolCallID(raw string) ToolCallID { return newBranded[toolCallIDTag](raw) }

// ParseToolCallID 解析并校验工具调用 ID。
func ParseToolCallID(raw string) (ToolCallID, error) { return parseBranded[toolCallIDTag](raw) }

// NewApprovalRequestID 从原始字符串构造审批请求 ID。
func NewApprovalRequestID(raw string) ApprovalRequestID { return newBranded[approvalRequestIDTag](raw) }

// ParseApprovalRequestID 解析并校验审批请求 ID。
func ParseApprovalRequestID(raw string) (ApprovalRequestID, error) {
	return parseBranded[approvalRequestIDTag](raw)
}

// NewJobID 从原始字符串构造后台任务 ID。
func NewJobID(raw string) JobID { return newBranded[jobIDTag](raw) }

// ParseJobID 解析并校验后台任务 ID。
func ParseJobID(raw string) (JobID, error) { return parseBranded[jobIDTag](raw) }

// NewSkillID 从原始字符串构造技能 ID。
func NewSkillID(raw string) SkillID { return newBranded[skillIDTag](raw) }

// ParseSkillID 解析并校验技能 ID。
func ParseSkillID(raw string) (SkillID, error) { return parseBranded[skillIDTag](raw) }

// NewAttachmentID 从原始字符串构造附件 ID。
func NewAttachmentID(raw string) AttachmentID { return newBranded[attachmentIDTag](raw) }

// ParseAttachmentID 解析并校验附件 ID。
func ParseAttachmentID(raw string) (AttachmentID, error) { return parseBranded[attachmentIDTag](raw) }

// NewCredentialRef 从原始字符串构造凭证引用。
func NewCredentialRef(raw string) CredentialRef { return newBranded[credentialRefTag](raw) }

// ParseCredentialRef 解析并校验凭证引用。
func ParseCredentialRef(raw string) (CredentialRef, error) { return parseBranded[credentialRefTag](raw) }

// NewWorkspaceID 从原始字符串构造工作区 ID。
func NewWorkspaceID(raw string) WorkspaceID { return newBranded[workspaceIDTag](raw) }

// ParseWorkspaceID 解析并校验工作区 ID。
func ParseWorkspaceID(raw string) (WorkspaceID, error) { return parseBranded[workspaceIDTag](raw) }

// NewProjectionID 从原始字符串构造投影 ID。
func NewProjectionID(raw string) ProjectionID { return newBranded[projectionIDTag](raw) }

// ParseProjectionID 解析并校验投影 ID。
func ParseProjectionID(raw string) (ProjectionID, error) { return parseBranded[projectionIDTag](raw) }

// ============================================================================
// Bytes[T] 品牌化字节切片载体
// ============================================================================

// Bytes 是品牌化字节切片类型的通用载体，用于附件、大块工具结果等二进制数据。
// 与 Branded[T] 一样通过空标签类型在编译期隔离不同语义的二进制数据。
type Bytes[T comparable] struct {
	value []byte
}

// NewBytes 从原始字节切片构造品牌字节（调用方保证不再变更原切片）。
func NewBytes[T comparable](raw []byte) Bytes[T] {
	return Bytes[T]{value: raw}
}

// ZeroBytes 返回零值品牌字节。
func ZeroBytes[T comparable]() Bytes[T] {
	return Bytes[T]{}
}

// IsZero 判断是否为零值（空切片 / 未赋值）。
func (b Bytes[T]) IsZero() bool {
	return len(b.value) == 0
}

// Bytes 返回底层字节切片视图。
func (b Bytes[T]) Bytes() []byte {
	return b.value
}

// String 返回以 UTF-8 解码的字符串视图，便于文本类内容（如工具结果）直接使用。
func (b Bytes[T]) String() string {
	return string(b.value)
}

// Len 返回底层字节长度。
func (b Bytes[T]) Len() int {
	return len(b.value)
}

// MarshalJSON 将品牌字节序列化为 Base64 编码的 JSON 字符串。
func (b Bytes[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.value)
}

// UnmarshalJSON 从 Base64 JSON 字符串反序列化回品牌字节。
func (b *Bytes[T]) UnmarshalJSON(data []byte) error {
	var raw []byte
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("brand: invalid bytes json for %T: %w", *new(T), err)
	}
	b.value = raw
	return nil
}
