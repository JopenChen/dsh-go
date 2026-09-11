// Package agent 的配置与运行时类型。
//
// 对齐官方 packages/core/agent/src/runtime-types.ts 和 types.ts。
package agent

import (
	"time"
)

// AgentOptions 是 Agent 的创建配置（对齐官方 AgentOptions）。
//
// 这些选项在 Agent 创建时固定，运行时不可变。Persona 属于 system-prompt
// 部分，不在此配置中。
type AgentOptions struct {
	// Provider 路由名称（调用时必须有已注册的适配器）。
	// 为空时使用适配器的默认 provider。
	Provider string `json:"provider,omitempty"`

	// Model 是由选定 provider 适配器解释的模型 ID。
	// 为空时使用适配器的默认模型。
	Model string `json:"model,omitempty"`

	// ReasoningEffort 是适配器拥有的推理努力级别（provider/model 相关）。
	// 为空时使用适配器的默认设置。
	ReasoningEffort string `json:"reasoningEffort,omitempty"`

	// MaxTokens 是每次对话模型请求的最大输出 token 数。
	// 为 0 时使用适配器的默认限制。
	MaxTokens int `json:"maxTokens,omitempty"`

	// DefaultTimeout 是每次运行的默认超时（0 表示无超时）。
	DefaultTimeout time.Duration `json:"defaultTimeout,omitempty"`
}

// AgentStatus 是 Agent 的生命周期状态（对齐官方 AgentStatus）。
//
// 在每次状态转换时通过 agent/status 事件发出：
//   - idle：没有驱动程序活动
//   - running：当唤醒输入开始可取消的 pre-step 处理时开始，
//     持续到驱动程序耗尽、关闭或检查点 turns
//
// Disposal 将 Agent 从其注册表中移除；它不是第三个可观察状态。
type AgentStatus string

const (
	// StatusIdle 表示 Agent 当前空闲（没有活动的驱动程序）。
	StatusIdle AgentStatus = "idle"
	// StatusRunning 表示 Agent 正在运行（有活动的驱动程序）。
	StatusRunning AgentStatus = "running"
)

// CancelOptions 是取消选项（对齐官方 CancelOptions）。
type CancelOptions struct {
	// KeepInbox 保留排队和引导中的 inbox 项，而不是丢弃它们。
	// 活动的 turn 仍然被中止，但未开始和待处理的工作会保留供后续 turn 使用，
	// 并且不会记录 canceled inbox splice。
	KeepInbox bool `json:"keepInbox,omitempty"`
}

// PreStepDecision 是循环是否以及携带哪些消息进入提议的 step
// （对齐官方 PreStepDecision）。
type PreStepDecision struct {
	// Kind 是决策类型："reject" 或 "enter"。
	Kind string `json:"kind"`
	// Messages 是进入 step 时携带的用户消息（Kind=enter 时有效）。
	Messages []UserMessage `json:"messages,omitempty"`
	// StartsRequestSeries 表示在此 step 的已接纳消息之前启动一个不同的
	// model-message 系列（Kind=enter 时可选）。
	StartsRequestSeries bool `json:"startsRequestSeries,omitempty"`
}

// RejectPreStep 创建一个 reject 决策（不进入 step）。
func RejectPreStep() PreStepDecision {
	return PreStepDecision{Kind: "reject"}
}

// EnterPreStep 创建一个 enter 决策（携带指定消息进入 step）。
func EnterPreStep(messages ...UserMessage) PreStepDecision {
	return PreStepDecision{Kind: "enter", Messages: messages}
}

// SessionStartSource 是会话生命周期开始的原因
// （对齐官方 SessionStartSource）。
//
//   - startup：种子创建
//   - resume：持久化加载
//   - clear：清除后重新开始
//   - compact：压缩后重新开始
type SessionStartSource string

const (
	// StartSourceStartup 表示种子创建。
	StartSourceStartup SessionStartSource = "startup"
	// StartSourceResume 表示持久化加载。
	StartSourceResume SessionStartSource = "resume"
	// StartSourceClear 表示清除后重新开始。
	StartSourceClear SessionStartSource = "clear"
	// StartSourceCompact 表示压缩后重新开始。
	StartSourceCompact SessionStartSource = "compact"
)

// UserMessage 是用户消息（简化版，对齐官方 UserMessage）。
type UserMessage struct {
	// ID 是消息 ID。
	ID string `json:"id"`
	// Content 是消息内容。
	Content string `json:"content"`
	// Source 是消息来源（"run" / "followup" / "inbox" 等）。
	Source string `json:"source,omitempty"`
	// CreatedAt 是创建时间。
	CreatedAt time.Time `json:"createdAt,omitempty"`
}
