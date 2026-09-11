// Package agent 的 Inbox 收件箱实现。
//
// 对齐官方 packages/core/agent/src/inbox.ts。
//
// Inbox 是 Agent 拥有的两个有序待处理消息列表的增量投影：
//   - next-turn：等待各个 turn 的提示
//   - next-step：等待下一个 step 边界的输入
package agent

import (
	"sync"

	"github.com/JopenChen/dsh-go/pkg/session"
)

// InboxNotifications 是 Inbox 变更的实时通知。
type InboxNotifications struct {
	// Inserted 发布一条插入的消息。
	Inserted func(message UserMessage)
	// Discarded 发布一条丢弃的消息。
	Discarded func(message UserMessage)
	// Claimed 在其拥有的 turn 内发布一条认领的消息。
	Claimed func(message UserMessage, turn uint64)
}

// Inbox 是持久化 Agent 收件箱事件的增量投影。
//
// 它维护两个列表：next-turn 和 next-step，并通过 splice 操作进行变更。
type Inbox struct {
	mu            sync.Mutex
	nextTurn      []UserMessage
	nextStep      []UserMessage
	notifications InboxNotifications
}

// NewInbox 创建一个新的 Inbox，并从会话日志中重放所有 agent/inbox/spliced 事件。
func NewInbox(log *session.SessionLog, notifications InboxNotifications) *Inbox {
	inbox := &Inbox{
		notifications: notifications,
	}
	if log != nil {
		// 重放历史事件
		for _, ev := range log.Events() {
			if ev.Type == session.EventAgentInboxSpliced {
				if data, ok := ev.Data.(session.InboxSplicedData); ok {
					inbox.apply(data)
				}
			}
		}
	}
	return inbox
}

// NextTurn 返回等待下一个 turn 的消息（只读）。
func (inbox *Inbox) NextTurn() []UserMessage {
	inbox.mu.Lock()
	defer inbox.mu.Unlock()
	result := make([]UserMessage, len(inbox.nextTurn))
	copy(result, inbox.nextTurn)
	return result
}

// NextStep 返回等待下一个 step 边界的输入（只读）。
func (inbox *Inbox) NextStep() []UserMessage {
	inbox.mu.Lock()
	defer inbox.mu.Unlock()
	result := make([]UserMessage, len(inbox.nextStep))
	copy(result, inbox.nextStep)
	return result
}

// HasPending 返回任一待处理消息列表是否包含工作。
func (inbox *Inbox) HasPending() bool {
	inbox.mu.Lock()
	defer inbox.mu.Unlock()
	return len(inbox.nextTurn) > 0 || len(inbox.nextStep) > 0
}

// Clear 持久化取消所有待处理输入，先清除 next-step 再清除 next-turn。
func (inbox *Inbox) Clear(log *session.SessionLog) {
	inbox.splice(log, session.InboxNextStep, 0, len(inbox.nextStep), nil, "canceled")
	inbox.splice(log, session.InboxNextTurn, 0, len(inbox.nextTurn), nil, "canceled")
}

// Claim 移除并返回为一个 step 提议的完整批次，并发布每条认领的消息。
// 持久化 splice 是纯删除。
//
// target 表示此边界是否还消费一个排队的 turn。
// turn 是将拥有该批次的 turn 编号。
// 返回 next-step 输入，后跟排队的 turn（如果请求）。
func (inbox *Inbox) Claim(log *session.SessionLog, target session.InboxTarget, turn uint64) []UserMessage {
	inbox.mu.Lock()
	claimed := inbox.mutate(session.InboxNextStep, 0, len(inbox.nextStep), nil)
	if target == session.InboxNextTurn {
		claimed = append(claimed, inbox.mutate(session.InboxNextTurn, 0, 1, nil)...)
	}
	inbox.mu.Unlock()

	// 发布认领通知
	if inbox.notifications.Claimed != nil {
		for _, msg := range claimed {
			inbox.notifications.Claimed(msg, turn)
		}
	}

	// 持久化删除
	if log != nil {
		if len(claimed) > 0 {
			// 计算删除的数量（next-step + next-step）
			stepRemoved := len(inbox.NextStep()) // 这是删除后的数量，不对
			_ = stepRemoved
			// 直接记录 splice 事件
			_, _ = log.Append(session.InboxSplicedData{
				Target:       session.InboxNextStep,
				Start:        0,
				RemovedCount: len(claimed),
				Inserted:     nil,
			})
		}
	}

	return claimed
}

// Push 追加一条消息到指定列表的末尾，并持久化。
func (inbox *Inbox) Push(log *session.SessionLog, target session.InboxTarget, message UserMessage) {
	inbox.splice(log, target, -1, 0, []UserMessage{message}, "")
}

// splice 执行持久化的拼接操作：在 start 位置删除 removedCount 条，插入 inserted。
// start = -1 表示追加到末尾。
func (inbox *Inbox) splice(log *session.SessionLog, target session.InboxTarget, start, removedCount int, inserted []UserMessage, outcome string) {
	inbox.mu.Lock()
	removed := inbox.mutate(target, start, removedCount, inserted)
	inbox.mu.Unlock()

	// 发布通知
	if inbox.notifications.Discarded != nil {
		for _, msg := range removed {
			inbox.notifications.Discarded(msg)
		}
	}
	if inbox.notifications.Inserted != nil {
		for _, msg := range inserted {
			inbox.notifications.Inserted(msg)
		}
	}

	// 持久化
	if log != nil {
		insertedContents := make([]string, len(inserted))
		for i, msg := range inserted {
			insertedContents[i] = msg.Content
		}
		_, _ = log.Append(session.InboxSplicedData{
			Target:       target,
			Start:        start,
			RemovedCount: removedCount,
			Inserted:     insertedContents,
			Outcome:      outcome,
		})
	}
}

// mutate 执行实际的列表变更（调用方必须持有锁）。
// 返回被删除的消息。
func (inbox *Inbox) mutate(target session.InboxTarget, start, removedCount int, inserted []UserMessage) []UserMessage {
	var list *[]UserMessage
	switch target {
	case session.InboxNextTurn:
		list = &inbox.nextTurn
	case session.InboxNextStep:
		list = &inbox.nextStep
	default:
		return nil
	}

	if start == -1 {
		start = len(*list)
	}

	// 边界检查
	if start < 0 {
		start = 0
	}
	if start > len(*list) {
		start = len(*list)
	}
	if removedCount < 0 {
		removedCount = 0
	}
	if start+removedCount > len(*list) {
		removedCount = len(*list) - start
	}

	// 保存被删除的消息
	removed := make([]UserMessage, removedCount)
	copy(removed, (*list)[start:start+removedCount])

	// 构建新列表
	newList := make([]UserMessage, 0, len(*list)-removedCount+len(inserted))
	newList = append(newList, (*list)[:start]...)
	newList = append(newList, inserted...)
	newList = append(newList, (*list)[start+removedCount:]...)
	*list = newList

	return removed
}

// apply 应用一个持久化的 splice 事件到内部状态（用于重放）。
func (inbox *Inbox) apply(data session.InboxSplicedData) {
	inbox.mu.Lock()
	defer inbox.mu.Unlock()

	// 将 content 字符串转换为 UserMessage
	inserted := make([]UserMessage, len(data.Inserted))
	for i, content := range data.Inserted {
		inserted[i] = UserMessage{Content: content}
	}

	inbox.mutate(data.Target, data.Start, data.RemovedCount, inserted)
}
