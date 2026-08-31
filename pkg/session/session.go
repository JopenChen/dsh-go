// Package session 提供事件溯源（Event Sourcing）会话日志核心。
//
// 对齐上游：packages/core/session
//
// 本文件对应任务 M04：Session 事件溯源 & 45+ 词汇表。
//
// 设计要点：
//   - 会话状态完全由追加式事件日志派生（append-only event log），任何时刻都可重放；
//   - 事件类型采用「Map → Derived Union」模式：EventData 接口 + 每种事件一个具体结构体，
//     序列化时通过 type 字段分发，反序列化时按 type 还原为具体类型，保证 round-trip 一致；
//   - SessionLog.Append() 是唯一的写入路径，严格维护时序不变量：
//     turn 开闭配对 / step 配对 / tool call↔result 匹配，违规立即被 invariant 拒绝。
package session

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/invariant"
)

// ============================================================================
// 事件类型词汇表（45+ 种，按功能簇分组）
// ============================================================================

// EventType 是事件类型的字符串标识。
type EventType string

// 簇 A：Turn / Step 双循环（8 种）
const (
	EventTurnStart    EventType = "turn/start"
	EventTurnEnd      EventType = "turn/end"
	EventTurnStopping EventType = "turn/stopping"
	EventStepStart    EventType = "step/start"
	EventStepEnd      EventType = "step/end"
	EventAgentError   EventType = "agent/error"
	EventAgentPreStep EventType = "agent/pre-step"
	EventAgentRequest EventType = "agent/request"
)

// 簇 B：消息（6 种）
const (
	EventUserMessage       EventType = "user/message"
	EventAssistantMessage  EventType = "assistant/message"
	EventAssistantChunk    EventType = "assistant/chunk"
	EventAssistantReasoning EventType = "assistant/reasoning"
	EventInjectionContext  EventType = "injection/context"
	EventSessionTitle      EventType = "session/title"
)

// 簇 C：工具（5 种）
const (
	EventToolCall         EventType = "tool/call"
	EventToolResult       EventType = "tool/result"
	EventToolError        EventType = "tool/error"
	EventToolObserved     EventType = "tool/observed"
	EventToolPresentation EventType = "tool/presentation"
)

// 簇 D：规划能力（7 种）
const (
	EventPlanMode      EventType = "plan/mode"
	EventPlanApproval  EventType = "plan/approval"
	EventGoalChange    EventType = "goal/change"
	EventGoalRound     EventType = "goal/round"
	EventTodoWrite     EventType = "todo/write"
	EventEndSeed       EventType = "session/end-seed"
	EventPlanUpdate    EventType = "project/plan-update"
)

// 簇 E：请求 & 审批（7 种）
const (
	EventRequestHeader    EventType = "request/header"
	EventRequestContext   EventType = "request/context"
	EventApprovalAsked    EventType = "approval/asked"
	EventApprovalDecided  EventType = "approval/decided"
	EventPresetChange     EventType = "permission/preset-change"
	EventAgentPresetChange EventType = "agent/preset-change"
	EventCommandRun       EventType = "command/run"
	EventCommandDone      EventType = "command/done"
)

// 簇 F：文件系统（6 种）
const (
	EventFSWriteIntent EventType = "fs/write-intent"
	EventFSEditIntent  EventType = "fs/edit-intent"
	EventFSObserved    EventType = "fs/observed"
	EventFSVersionBump EventType = "fs/version-bump"
	EventFSRead        EventType = "fs/read"
	EventFSDirList     EventType = "fs/dir-list"
)

// 簇 G：技能（3 种）
const (
	EventSkillsChange  EventType = "skills/change"
	EventSkillsCatalog EventType = "skills/catalog"
	EventSkillInject   EventType = "skill/inject"
)

// 簇 H：LLM（5 种）
const (
	EventLLMRequest EventType = "llm/request"
	EventLLMStream  EventType = "llm/stream"
	EventLLMRetry   EventType = "llm/retry"
	EventLLMError   EventType = "llm/error"
	EventLLMDone    EventType = "llm/done"
)

// 簇 I：表面替换 & 杂项（5 种）
const (
	EventSurfaceReplace          EventType = "surface/replace"
	EventAttachmentAdded         EventType = "attachment/added"
	EventSessionProjectionUpdate EventType = "session/projection-updated"
	EventWorkspaceChange         EventType = "workspace/change"
	EventUserQuestion            EventType = "user/question"
)

// 全部事件类型清单（用于词汇表遍历与 round-trip 测试）。
var AllEventTypes = []EventType{
	EventTurnStart, EventTurnEnd, EventTurnStopping, EventStepStart, EventStepEnd,
	EventAgentError, EventAgentPreStep, EventAgentRequest,
	EventUserMessage, EventAssistantMessage, EventAssistantChunk, EventAssistantReasoning,
	EventInjectionContext, EventSessionTitle,
	EventToolCall, EventToolResult, EventToolError, EventToolObserved, EventToolPresentation,
	EventPlanMode, EventPlanApproval, EventGoalChange, EventGoalRound, EventTodoWrite,
	EventEndSeed, EventPlanUpdate,
	EventRequestHeader, EventRequestContext, EventApprovalAsked, EventApprovalDecided,
	EventPresetChange, EventAgentPresetChange, EventCommandRun, EventCommandDone,
	EventFSWriteIntent, EventFSEditIntent, EventFSObserved, EventFSVersionBump, EventFSRead, EventFSDirList,
	EventSkillsChange, EventSkillsCatalog, EventSkillInject,
	EventLLMRequest, EventLLMStream, EventLLMRetry, EventLLMError, EventLLMDone,
	EventSurfaceReplace, EventAttachmentAdded, EventSessionProjectionUpdate, EventWorkspaceChange,
	EventUserQuestion,
}

// ============================================================================
// EventData：派生联合（Derived Union）接口
// ============================================================================

// EventData 是所有事件数据结构的统一接口。
// 每种事件一个具体结构体并实现 EventType()，构成"类型安全的事件联合"。
type EventData interface {
	EventType() EventType
}

// --- 簇 A：Turn / Step ---

// TurnStartData turn/start：一个 Turn 轮次开始。
type TurnStartData struct{}

func (TurnStartData) EventType() EventType { return EventTurnStart }

// TurnEndReason 是 turn/end 的关闭原因。
type TurnEndReason string

// turn/end 关闭原因枚举。
const (
	ReasonFinished    TurnEndReason = "finished"
	ReasonInterrupted TurnEndReason = "interrupted"
	ReasonAborted     TurnEndReason = "aborted"
)

// TurnEndData turn/end：一个 Turn 轮次结束。
type TurnEndData struct {
	Reason TurnEndReason `json:"reason"`
}

func (TurnEndData) EventType() EventType { return EventTurnEnd }

// TurnStoppingData turn/stopping：turn 进入停止流程（goal round driver 等监听）。
type TurnStoppingData struct {
	Reason string `json:"reason,omitempty"`
}

func (TurnStoppingData) EventType() EventType { return EventTurnStopping }

// StepStartData step/start：单步开始。
type StepStartData struct {
	StepSeq uint64 `json:"stepSeq"`
}

func (StepStartData) EventType() EventType { return EventStepStart }

// StepEndData step/end：单步结束。
type StepEndData struct {
	StepSeq uint64 `json:"stepSeq"`
}

func (StepEndData) EventType() EventType { return EventStepEnd }

// AgentErrorData agent/error：代理内部错误。
type AgentErrorData struct {
	Message string `json:"message"`
	Pkg     string `json:"pkg,omitempty"`
}

func (AgentErrorData) EventType() EventType { return EventAgentError }

// AgentPreStepData agent/pre-step：进入 step 前的预处理标记。
type AgentPreStepData struct {
	Blocked bool `json:"blocked,omitempty"`
}

func (AgentPreStepData) EventType() EventType { return EventAgentPreStep }

// AgentRequestData agent/request：组装后的模型请求快照。
type AgentRequestData struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
}

func (AgentRequestData) EventType() EventType { return EventAgentRequest }

// --- 簇 B：消息 ---

// UserMessageData user/message：用户消息。
type UserMessageData struct {
	Content string `json:"content"`
	Source  string `json:"source,omitempty"` // e.g. "user" / "reference" / "injected"
	Refs    []any  `json:"refs,omitempty"`   // 引用的 session/文件
}

func (UserMessageData) EventType() EventType { return EventUserMessage }

// AssistantMessageData assistant/message：助手最终消息。
type AssistantMessageData struct {
	Content string `json:"content"`
	ToolCallIDs []string `json:"toolCallIds,omitempty"`
}

func (AssistantMessageData) EventType() EventType { return EventAssistantMessage }

// AssistantChunkData assistant/chunk：流式内容分片。
type AssistantChunkData struct {
	Text string `json:"text,omitempty"`
}

func (AssistantChunkData) EventType() EventType { return EventAssistantChunk }

// AssistantReasoningData assistant/reasoning：推理内容分片。
type AssistantReasoningData struct {
	Text string `json:"text,omitempty"`
}

func (AssistantReasoningData) EventType() EventType { return EventAssistantReasoning }

// InjectionContextData injection/context：注入的动态上下文（如 skills catalog）。
type InjectionContextData struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Hash    string `json:"hash,omitempty"`
}

func (InjectionContextData) EventType() EventType { return EventInjectionContext }

// SessionTitleData session/title：会话标题（latest-wins）。
type SessionTitleData struct {
	Title string `json:"title"`
}

func (SessionTitleData) EventType() EventType { return EventSessionTitle }

// --- 簇 C：工具 ---

// ToolCallData tool/call：一次工具调用发起。
type ToolCallData struct {
	CallID brand.ToolCallID `json:"callId"`
	Tool   string           `json:"tool"`
	Input  json.RawMessage  `json:"input,omitempty"`
}

func (ToolCallData) EventType() EventType { return EventToolCall }

// ToolResultData tool/result：一次工具调用结果（与 tool/call 按 CallID 配对）。
type ToolResultData struct {
	CallID  brand.ToolCallID `json:"callId"`
	IsError bool             `json:"isError,omitempty"`
	Output  string           `json:"output,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
	SpillPath string         `json:"spillPath,omitempty"`
}

func (ToolResultData) EventType() EventType { return EventToolResult }

// ToolErrorData tool/error：工具执行错误。
type ToolErrorData struct {
	CallID brand.ToolCallID `json:"callId"`
	Error  string           `json:"error"`
}

func (ToolErrorData) EventType() EventType { return EventToolError }

// ToolObservedData tool/observed：工具产生的可观察事件（fs 先读后写等）。
type ToolObservedData struct {
	CallID brand.ToolCallID `json:"callId"`
	Kind   string           `json:"kind"`
}

func (ToolObservedData) EventType() EventType { return EventToolObserved }

// ToolPresentationData tool/presentation：工具展示卡片（中立 vocabulary）。
type ToolPresentationData struct {
	CallID brand.ToolCallID `json:"callId"`
	Card   string           `json:"card"`
}

func (ToolPresentationData) EventType() EventType { return EventToolPresentation }

// --- 簇 D：规划能力 ---

// PlanModeData plan/mode：计划模式切换（on/off）。
type PlanModeData struct {
	Mode string `json:"mode"` // "on" / "off"
}

func (PlanModeData) EventType() EventType { return EventPlanMode }

// PlanApprovalData plan/approval：计划审批结果。
type PlanApprovalData struct {
	Approved bool `json:"approved"`
}

func (PlanApprovalData) EventType() EventType { return EventPlanApproval }

// GoalPhase 目标阶段。
type GoalPhase string

// GoalChangeData goal/change：目标变更（CAS revision）。
type GoalChangeData struct {
	GoalID      string     `json:"goalId"`
	Phase       GoalPhase  `json:"phase"`
	Description string     `json:"description,omitempty"`
	MaxRounds   int        `json:"maxRounds,omitempty"`
	Revision    uint64     `json:"revision"`
}

func (GoalChangeData) EventType() EventType { return EventGoalChange }

// GoalRoundData goal/round：目标续轮标记。
type GoalRoundData struct {
	Round int `json:"round"`
}

func (GoalRoundData) EventType() EventType { return EventGoalRound }

// TodoWriteData todo/write：整体替换待办列表。
type TodoWriteData struct {
	Items []string `json:"items"`
}

func (TodoWriteData) EventType() EventType { return EventTodoWrite }

// EndSeedData session/end-seed：种子边界 marker（Resume/Fork 后第一条 live 写入）。
type EndSeedData struct {
	ParentSession brand.SessionID `json:"parentSession,omitempty"`
}

func (EndSeedData) EventType() EventType { return EventEndSeed }

// PlanUpdateData project/plan-update：计划内容更新。
type PlanUpdateData struct {
	Content string `json:"content,omitempty"`
}

func (PlanUpdateData) EventType() EventType { return EventPlanUpdate }

// --- 簇 E：请求 & 审批 ---

// RequestHeaderData request/header：请求头快照（config/system/tools epoch）。
type RequestHeaderData struct {
	ConfigEpoch uint64 `json:"configEpoch"`
	SystemHash  string `json:"systemHash,omitempty"`
	ToolCount   int    `json:"toolCount,omitempty"`
}

func (RequestHeaderData) EventType() EventType { return EventRequestHeader }

// RequestContextData request/context：请求上下文（provider/model/contextWindow）。
type RequestContextData struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Window   int    `json:"contextWindow,omitempty"`
	Reason   string `json:"reason,omitempty"` // initial/resume/change/series
}

func (RequestContextData) EventType() EventType { return EventRequestContext }

// ApprovalAskedData approval/asked：发起一次审批询问。
type ApprovalRequestIDData struct {
	RequestID brand.ApprovalRequestID `json:"requestId"`
	Tool      string                  `json:"tool,omitempty"`
	Summary   string                  `json:"summary,omitempty"`
}

func (ApprovalRequestIDData) EventType() EventType { return EventApprovalAsked }

// ApprovalDecidedData approval/decided：审批给出结论。
type ApprovalDecidedData struct {
	RequestID brand.ApprovalRequestID `json:"requestId"`
	Allowed   bool                    `json:"allowed"`
}

func (ApprovalDecidedData) EventType() EventType { return EventApprovalDecided }

// PresetChangeData permission/preset-change：权限预设切换。
type PresetChangeData struct {
	Preset string `json:"preset"`
}

func (PresetChangeData) EventType() EventType { return EventPresetChange }

// AgentPresetChangeData agent/preset-change：代理预设切换。
type AgentPresetChangeData struct {
	Preset string `json:"preset"`
}

func (AgentPresetChangeData) EventType() EventType { return EventAgentPresetChange }

// CommandRunData command/run：slash 命令触发。
type CommandRunData struct {
	Command string `json:"command"`
	Args    string `json:"args,omitempty"`
}

func (CommandRunData) EventType() EventType { return EventCommandRun }

// CommandDoneData command/done：slash 命令完成。
type CommandDoneData struct {
	Command string `json:"command"`
}

func (CommandDoneData) EventType() EventType { return EventCommandDone }

// --- 簇 F：文件系统 ---

// FsIntentData 是 fs 意图事件共用数据结构（write/edit/observed/version-bump/read/dir-list）。
type FsIntentData struct {
	Path string `json:"path"`
	Kind string `json:"kind,omitempty"`
	Version uint64 `json:"version,omitempty"`
	Observed bool `json:"observed,omitempty"`
}

func (FsIntentData) EventType() EventType { return "" }

// FsWriteIntentData fs/write-intent：写文件意图。
type FsWriteIntentData struct {
	Path string `json:"path"`
	Version uint64 `json:"version,omitempty"`
	Observed bool `json:"observed,omitempty"`
}

func (FsWriteIntentData) EventType() EventType { return EventFSWriteIntent }

// FsEditIntentData fs/edit-intent：编辑文件意图。
type FsEditIntentData struct {
	Path    string `json:"path"`
	Version uint64 `json:"version,omitempty"`
}

func (FsEditIntentData) EventType() EventType { return EventFSEditIntent }

// FsObservedData fs/observed：文件已被观察（先读后写政策）。
type FsObservedData struct {
	Path string `json:"path"`
}

func (FsObservedData) EventType() EventType { return EventFSObserved }

// FsVersionBumpData fs/version-bump：文件版本递增。
type FsVersionBumpData struct {
	Path    string `json:"path"`
	Version uint64 `json:"version"`
}

func (FsVersionBumpData) EventType() EventType { return EventFSVersionBump }

// FsReadData fs/read：读取文件。
type FsReadData struct {
	Path string `json:"path"`
}

func (FsReadData) EventType() EventType { return EventFSRead }

// FsDirListData fs/dir-list：列目录。
type FsDirListData struct {
	Path string `json:"path"`
}

func (FsDirListData) EventType() EventType { return EventFSDirList }

// --- 簇 G：技能 ---

// SkillsChangeData skills/change：技能目录变更。
type SkillsChangeData struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changed []string `json:"changed,omitempty"`
}

func (SkillsChangeData) EventType() EventType { return EventSkillsChange }

// SkillsCatalogData skills/catalog：技能目录全量快照（change-only 注入用）。
type SkillsCatalogData struct {
	Catalog string `json:"catalog"`
	Hash    string `json:"hash"`
}

func (SkillsCatalogData) EventType() EventType { return EventSkillsCatalog }

// SkillInjectData skill/inject：技能内容注入为 user message。
type SkillInjectData struct {
	SkillName string `json:"skillName"`
	Content   string `json:"content,omitempty"`
}

func (SkillInjectData) EventType() EventType { return EventSkillInject }

// --- 簇 H：LLM ---

// LLMRequestData llm/request：发起模型请求。
type LLMRequestData struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

func (LLMRequestData) EventType() EventType { return EventLLMRequest }

// LLMStreamData llm/stream：模型流式分片。
type LLMStreamData struct {
	Text string `json:"text,omitempty"`
}

func (LLMStreamData) EventType() EventType { return EventLLMStream }

// LLMRetryData llm/retry：模型请求重试（backoff 信息）。
type LLMRetryData struct {
	Attempt   int    `json:"attempt"`
	BackoffMs int64  `json:"backoffMs"`
	Error     string `json:"error"`
}

func (LLMRetryData) EventType() EventType { return EventLLMRetry }

// LLMErrorData llm/error：模型请求失败。
type LLMErrorData struct {
	Kind  string `json:"kind"` // overload/rate-limit/refusal/context-overflow
	Error string `json:"error"`
}

func (LLMErrorData) EventType() EventType { return EventLLMError }

// LLMDoneData llm/done：模型请求完成（含 token 用量）。
type LLMDoneData struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
}

func (LLMDoneData) EventType() EventType { return EventLLMDone }

// --- 簇 I：表面替换 & 杂项 ---

// SurfaceReplaceData surface/replace：表面替换（compaction 唯一合法回写方式）。
type SurfaceReplaceData struct {
	Start uint64          `json:"start"`
	End   uint64          `json:"end"`
	Data  json.RawMessage `json:"data,omitempty"`
}

func (SurfaceReplaceData) EventType() EventType { return EventSurfaceReplace }

// AttachmentAddedData attachment/added：附件新增。
type AttachmentAddedData struct {
	AttachmentID brand.AttachmentID `json:"attachmentId"`
	Name         string             `json:"name,omitempty"`
}

func (AttachmentAddedData) EventType() EventType { return EventAttachmentAdded }

// ProjectionUpdateData session/projection-updated：投影状态更新。
type ProjectionUpdateData struct {
	ProjectionID brand.ProjectionID `json:"projectionId"`
	Hash         string             `json:"hash,omitempty"`
}

func (ProjectionUpdateData) EventType() EventType { return EventSessionProjectionUpdate }

// WorkspaceChangeData workspace/change：工作区记录变更。
type WorkspaceChangeData struct {
	WorkspaceID brand.WorkspaceID `json:"workspaceId"`
	Root        string            `json:"root,omitempty"`
}

func (WorkspaceChangeData) EventType() EventType { return EventWorkspaceChange }

// UserQuestionData user/question：用户提问（UQ 接缝）。
type UserQuestionData struct {
	Question string `json:"question"`
	Answer   string `json:"answer,omitempty"`
}

func (UserQuestionData) EventType() EventType { return EventUserQuestion }

// ============================================================================
// 事件类型 → 数据实例工厂（反序列化分发用）
// ============================================================================

// newEventData 按类型创建空的 EventData 实例。
func newEventData(t EventType) (EventData, error) {
	switch t {
	case EventTurnStart:
		return TurnStartData{}, nil
	case EventTurnEnd:
		return TurnEndData{}, nil
	case EventTurnStopping:
		return TurnStoppingData{}, nil
	case EventStepStart:
		return StepStartData{}, nil
	case EventStepEnd:
		return StepEndData{}, nil
	case EventAgentError:
		return AgentErrorData{}, nil
	case EventAgentPreStep:
		return AgentPreStepData{}, nil
	case EventAgentRequest:
		return AgentRequestData{}, nil
	case EventUserMessage:
		return UserMessageData{}, nil
	case EventAssistantMessage:
		return AssistantMessageData{}, nil
	case EventAssistantChunk:
		return AssistantChunkData{}, nil
	case EventAssistantReasoning:
		return AssistantReasoningData{}, nil
	case EventInjectionContext:
		return InjectionContextData{}, nil
	case EventSessionTitle:
		return SessionTitleData{}, nil
	case EventToolCall:
		return ToolCallData{}, nil
	case EventToolResult:
		return ToolResultData{}, nil
	case EventToolError:
		return ToolErrorData{}, nil
	case EventToolObserved:
		return ToolObservedData{}, nil
	case EventToolPresentation:
		return ToolPresentationData{}, nil
	case EventPlanMode:
		return PlanModeData{}, nil
	case EventPlanApproval:
		return PlanApprovalData{}, nil
	case EventGoalChange:
		return GoalChangeData{}, nil
	case EventGoalRound:
		return GoalRoundData{}, nil
	case EventTodoWrite:
		return TodoWriteData{}, nil
	case EventEndSeed:
		return EndSeedData{}, nil
	case EventPlanUpdate:
		return PlanUpdateData{}, nil
	case EventRequestHeader:
		return RequestHeaderData{}, nil
	case EventRequestContext:
		return RequestContextData{}, nil
	case EventApprovalAsked:
		return ApprovalRequestIDData{}, nil
	case EventApprovalDecided:
		return ApprovalDecidedData{}, nil
	case EventPresetChange:
		return PresetChangeData{}, nil
	case EventAgentPresetChange:
		return AgentPresetChangeData{}, nil
	case EventCommandRun:
		return CommandRunData{}, nil
	case EventCommandDone:
		return CommandDoneData{}, nil
	case EventFSWriteIntent:
		return FsWriteIntentData{}, nil
	case EventFSEditIntent:
		return FsEditIntentData{}, nil
	case EventFSObserved:
		return FsObservedData{}, nil
	case EventFSVersionBump:
		return FsVersionBumpData{}, nil
	case EventFSRead:
		return FsReadData{}, nil
	case EventFSDirList:
		return FsDirListData{}, nil
	case EventSkillsChange:
		return SkillsChangeData{}, nil
	case EventSkillsCatalog:
		return SkillsCatalogData{}, nil
	case EventSkillInject:
		return SkillInjectData{}, nil
	case EventLLMRequest:
		return LLMRequestData{}, nil
	case EventLLMStream:
		return LLMStreamData{}, nil
	case EventLLMRetry:
		return LLMRetryData{}, nil
	case EventLLMError:
		return LLMErrorData{}, nil
	case EventLLMDone:
		return LLMDoneData{}, nil
	case EventSurfaceReplace:
		return SurfaceReplaceData{}, nil
	case EventAttachmentAdded:
		return AttachmentAddedData{}, nil
	case EventSessionProjectionUpdate:
		return ProjectionUpdateData{}, nil
	case EventWorkspaceChange:
		return WorkspaceChangeData{}, nil
	case EventUserQuestion:
		return UserQuestionData{}, nil
	default:
		return nil, fmt.Errorf("session: unknown event type %q", t)
	}
}

// EventTypeOf 返回任意 EventData 的类型（便捷方法，也用于测试断言）。
func EventTypeOf(d EventData) EventType {
	return d.EventType()
}

// ============================================================================
// SessionEvent：带序号/时间/类型/数据的事件记录
// ============================================================================

// SessionEvent 是日志中的单条事件记录。
type SessionEvent struct {
	// Seq 为严格递增的事件序号（1..N 连续）。
	Seq uint64 `json:"seq"`
	// Time 为事件写入时间（严格单调）。
	Time time.Time `json:"time"`
	// Type 为事件类型标识。
	Type EventType `json:"type"`
	// Data 为类型化载荷（Derived Union）。
	Data EventData `json:"data"`
	// SurfaceOp 存在表示该事件是表面替换操作（compaction 唯一合法回写方式）。
	SurfaceOp *SurfaceOp `json:"surfaceOp,omitempty"`
	// SourceEventSeqs 记录派生来源事件序号（compaction/派生消息用）。
	SourceEventSeqs []uint64 `json:"sourceEventSeqs,omitempty"`
}

// eventEnvelope 是 SessionEvent 的 JSON 传输结构（避免与自定义序列化循环递归）。
type eventEnvelope struct {
	Seq             uint64          `json:"seq"`
	Time            time.Time       `json:"time"`
	Type            EventType       `json:"type"`
	Data            json.RawMessage `json:"data"`
	SurfaceOp       *SurfaceOp      `json:"surfaceOp,omitempty"`
	SourceEventSeqs []uint64        `json:"sourceEventSeqs,omitempty"`
}

// MarshalJSON 将 SessionEvent 序列化为 {seq,time,type,data,surfaceOp?,sourceEventSeqs?}。
func (e SessionEvent) MarshalJSON() ([]byte, error) {
	dataBytes, err := json.Marshal(e.Data)
	if err != nil {
		return nil, fmt.Errorf("session: marshal data for %q: %w", e.Type, err)
	}
	env := eventEnvelope{
		Seq:             e.Seq,
		Time:            e.Time,
		Type:            e.Type,
		Data:            dataBytes,
		SurfaceOp:       e.SurfaceOp,
		SourceEventSeqs: e.SourceEventSeqs,
	}
	return json.Marshal(env)
}

// UnmarshalJSON 从 JSON 还原 SessionEvent，并按 type 分发 Data 为具体类型。
func (e *SessionEvent) UnmarshalJSON(data []byte) error {
	var env eventEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("session: unmarshal envelope: %w", err)
	}
	instance, err := newEventData(env.Type)
	if err != nil {
		return err
	}
	if len(env.Data) > 0 {
		// 通过反射创建指针再反序列化，最后解引用还原为值存储（Data 字段保持值语义，
		// 使 applyState 中的类型断言 (ToolCallData) 等仍按值匹配）。
		pv := reflect.New(reflect.TypeOf(instance))
		if err := json.Unmarshal(env.Data, pv.Interface()); err != nil {
			return fmt.Errorf("session: unmarshal data for %q: %w", env.Type, err)
		}
		instance = pv.Elem().Interface().(EventData)
	}
	e.Seq = env.Seq
	e.Time = env.Time
	e.Type = env.Type
	e.Data = instance
	e.SurfaceOp = env.SurfaceOp
	e.SourceEventSeqs = env.SourceEventSeqs
	return nil
}

// ============================================================================
// SessionLog：事件日志 + 严格不变量
// ============================================================================

// sessionState 跟踪当前开放结构，供时序不变量校验。
type sessionState struct {
	turnOpen    bool
	stepOpen    bool
	toolCalls   map[string]bool // CallID.Raw() -> 是否已配 result
	lastTime    time.Time
}

// SessionLog 是事件溯源日志的唯一写路径载体。
type SessionLog struct {
	mu         sync.Mutex
	sessionID  brand.SessionID
	events     []SessionEvent
	seq        uint64
	state      sessionState
	invariants *invariant.Registry
	// folder H04 增量 Fold 钩子（nil = 未启用，向后兼容）。
	// 启用后 Append 成功时自动 folder.Append(ev)，Projection() O(1) 返回派生快照。
	folder *IncrementalFolder
}

// EnableIncrementalProjection 启用 H04 增量派生。
//
// 若日志已有 N 条事件（重建场景），将用一次 FolderFromEvents 建立基线，
// 后续 Append 走纯增量 O(1)；未启用时上层仍可 FoldAllFromLog(sl) 走全量。
func (sl *SessionLog) EnableIncrementalProjection() {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.folder != nil {
		return
	}
	if len(sl.events) == 0 {
		sl.folder = NewIncrementalFolder()
	} else {
		sl.folder = FolderFromEvents(sl.events)
	}
}

// DisableIncrementalProjection 关闭增量派生（性能对比测试用）。
func (sl *SessionLog) DisableIncrementalProjection() {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.folder = nil
}

// IncrementalEnabled 返回是否启用了 H04 增量派生。
func (sl *SessionLog) IncrementalEnabled() bool {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.folder != nil
}

// Projection 返回最新派生投影。
// 启用 H04：走 IncrementalFolder.Snapshot（绝大多数情况 O(1)，
// 仅当 SurfaceReplace 触发 dirty 时懒重建 Messages 一条投影）；
// 未启用：回退 FoldAllFromLog O(n)。
func (sl *SessionLog) Projection() SessionProjection {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.folder != nil {
		return sl.folder.Snapshot(sl.events)
	}
	return FoldAll(sl.events)
}

// ProjectionMeta 返回不含 Messages 的轻量快照（热路径首选，不触发 Messages 大 slice 拷贝）。
// 仅填充 RequestHeader/SandboxMode/Approval/Preset/Goal/Todo/PlanMode/Title。
func (sl *SessionLog) ProjectionMeta() SessionProjection {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.folder != nil {
		return sl.folder.SnapshotMeta(sl.events)
	}
	// 未启用增量时：仍跑 FoldAll 但主动把 Messages 置 nil 保持签名契约。
	p := FoldAll(sl.events)
	p.Messages = nil
	return p
}

// IncrementalStats 返回 H04 统计快照；未启用时返回 zero FolderStats。
func (sl *SessionLog) IncrementalStats() FolderStats {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.folder == nil {
		return FolderStats{}
	}
	return sl.folder.StatsCopy()
}

// NewSessionLog 创建空日志，并注册内置时序不变量检查器。
func NewSessionLog(id brand.SessionID) *SessionLog {
	sl := &SessionLog{
		sessionID:  id,
		state:      sessionState{toolCalls: map[string]bool{}},
		invariants: invariant.NewRegistry(),
	}
	// 注册"时序不变量"：每一条违规都会带 pkg/session 前缀报错
	_ = sl.invariants.Register("pkg/session", sl.checkTurnPairing)
	_ = sl.invariants.Register("pkg/session", sl.checkStepPairing)
	_ = sl.invariants.Register("pkg/session", sl.checkToolCallResultPairing)
	return sl
}

// SessionID 返回日志所属会话 ID。
func (sl *SessionLog) SessionID() brand.SessionID {
	return sl.sessionID
}

// Events 返回全部事件的只读快照。
func (sl *SessionLog) Events() []SessionEvent {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	out := make([]SessionEvent, len(sl.events))
	copy(out, sl.events)
	return out
}

// Len 返回当前事件总数。
func (sl *SessionLog) Len() int {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return len(sl.events)
}

// LastSeq 返回最新事件序号（无事件时为 0）。
func (sl *SessionLog) LastSeq() uint64 {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.seq
}

// Append 是唯一写入路径：追加一条事件并执行时序不变量校验。
//   - 自动分配严格递增序号（1..N 连续）与单调时间；
//   - 违反 turn 开闭 / step 配对 / tool call↔result 匹配 → 拒绝写入并返回错误。
func (sl *SessionLog) Append(data EventData) (uint64, error) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	// 校验时间单调（时间由外部传入为可选，内部保证不早于上一条）
	now := time.Now()
	if !sl.state.lastTime.IsZero() && now.Before(sl.state.lastTime) {
		now = sl.state.lastTime
	}

	// 构造事件
	sl.seq++
	ev := SessionEvent{
		Seq:  sl.seq,
		Time: now,
		Type: data.EventType(),
		Data: data,
	}

	// 应用状态转移 + 时序校验
	if err := sl.applyState(ev); err != nil {
		sl.seq-- // 回退序号，保持连续
		return 0, err
	}

	sl.events = append(sl.events, ev)
	sl.state.lastTime = now
	// H04：如已启用增量投影，立即把这条事件喂给 IncrementalFolder（O(1) 不扫历史）。
	if sl.folder != nil {
		sl.folder.Append(ev)
	}
	return ev.Seq, nil
}

// Check 运行不变量注册中心（整条日志级校验：turn/step/tool 配对平衡）。
// 适合在持久化 / flush 前调用做一致性把关；Append 内部已做逐条状态级校验，
// 因此运行期不需要每 append 一次就跑整日志校验（中间态天然不平衡）。
func (sl *SessionLog) Check() []error {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.invariants.Run()
}

// applyState 根据事件类型更新开放结构状态并校验配对关系。
func (sl *SessionLog) applyState(ev SessionEvent) error {
	switch ev.Type {
	case EventTurnStart:
		if sl.state.turnOpen {
			return fmt.Errorf("session invariant: turn/start while turn already open")
		}
		sl.state.turnOpen = true
	case EventTurnEnd:
		if !sl.state.turnOpen {
			return fmt.Errorf("session invariant: turn/end without open turn/start")
		}
		if sl.state.stepOpen {
			return fmt.Errorf("session invariant: turn/end while step still open")
		}
		sl.state.turnOpen = false
	case EventTurnStopping:
		// 允许在 turn 开放时进入停止流程
		if !sl.state.turnOpen {
			return fmt.Errorf("session invariant: turn/stopping without open turn")
		}
	case EventStepStart:
		if !sl.state.turnOpen {
			return fmt.Errorf("session invariant: step/start without open turn")
		}
		if sl.state.stepOpen {
			return fmt.Errorf("session invariant: step/start while step already open")
		}
		sl.state.stepOpen = true
	case EventStepEnd:
		if !sl.state.stepOpen {
			return fmt.Errorf("session invariant: step/end without open step/start")
		}
		sl.state.stepOpen = false
	case EventToolCall:
		if !sl.state.stepOpen {
			return fmt.Errorf("session invariant: tool/call without open step")
		}
		td, ok := ev.Data.(ToolCallData)
		if !ok {
			return fmt.Errorf("session invariant: tool/call data type mismatch")
		}
		key := td.CallID.Raw()
		if _, exists := sl.state.toolCalls[key]; exists {
			return fmt.Errorf("session invariant: duplicate tool/call %q", key)
		}
		sl.state.toolCalls[key] = false
	case EventToolResult, EventToolError:
		td, ok := ev.Data.(ToolResultData)
		var callID brand.ToolCallID
		if ok {
			callID = td.CallID
		} else if ed, ok2 := ev.Data.(ToolErrorData); ok2 {
			callID = ed.CallID
		} else {
			return fmt.Errorf("session invariant: tool result data type mismatch")
		}
		key := callID.Raw()
		open, exists := sl.state.toolCalls[key]
		if !exists {
			return fmt.Errorf("session invariant: tool/result for unknown call %q", key)
		}
		if open {
			return fmt.Errorf("session invariant: duplicate tool/result for call %q", key)
		}
		sl.state.toolCalls[key] = true
	}
	return nil
}

// checkTurnPairing 校验日志级 turn 配对（供 invariant 注册中心使用）。
func (sl *SessionLog) checkTurnPairing() error {
	open := false
	for _, ev := range sl.events {
		switch ev.Type {
		case EventTurnStart:
			open = true
		case EventTurnEnd:
			open = false
		}
	}
	if open {
		return fmt.Errorf("unclosed turn at end of log")
	}
	return nil
}

// checkStepPairing 校验日志级 step 配对。
func (sl *SessionLog) checkStepPairing() error {
	turnOpen, stepOpen := false, false
	for _, ev := range sl.events {
		switch ev.Type {
		case EventTurnStart:
			turnOpen = true
		case EventTurnEnd:
			turnOpen = false
		case EventStepStart:
			stepOpen = turnOpen
		case EventStepEnd:
			stepOpen = false
		}
	}
	if stepOpen {
		return fmt.Errorf("unclosed step at end of log")
	}
	return nil
}

// checkToolCallResultPairing 校验日志级 tool call↔result 匹配。
func (sl *SessionLog) checkToolCallResultPairing() error {
	matched := map[string]bool{}
	for _, ev := range sl.events {
		switch ev.Type {
		case EventToolCall:
			if td, ok := ev.Data.(ToolCallData); ok {
				matched[td.CallID.Raw()] = false
			}
		case EventToolResult, EventToolError:
			var callID brand.ToolCallID
			if td, ok := ev.Data.(ToolResultData); ok {
				callID = td.CallID
			} else if ed, ok := ev.Data.(ToolErrorData); ok {
				callID = ed.CallID
			}
			if _, exists := matched[callID.Raw()]; !exists {
				return fmt.Errorf("tool/result for unknown call %q", callID.Raw())
			}
			matched[callID.Raw()] = true
		}
	}
	for k, done := range matched {
		if !done {
			return fmt.Errorf("unmatched tool/call %q", k)
		}
	}
	return nil
}
